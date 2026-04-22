package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	auth "github.com/forge-metal/auth-middleware"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	fmotel "github.com/forge-metal/otel"
	secretsclient "github.com/forge-metal/secrets-service/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/forge-metal/mailbox-service/internal/api"
	"github.com/forge-metal/mailbox-service/internal/app"
	"github.com/forge-metal/mailbox-service/internal/forwarder"
	"github.com/forge-metal/mailbox-service/internal/mailstore"
	"github.com/forge-metal/mailbox-service/internal/sessionproxy"
	mailboxsync "github.com/forge-metal/mailbox-service/internal/sync"
)

var version = "dev"

type config struct {
	ListenAddr            string
	PGDSN                 string
	StalwartBaseURL       string
	PublicBaseURL         string
	MailboxUser           string
	LocalDomain           string
	ForwarderFromAddress  string
	ForwarderFromName     string
	ForwarderStatePath    string
	ForwarderPollInterval time.Duration
	ForwarderQueryLimit   int
	ForwarderSeenLimit    int
	ForwarderBootstrapMax int
	SyncDiscoveryInterval time.Duration
	SyncReconcileInterval time.Duration
	AuthIssuerURL         string
	AuthAudience          string
	AuthJWKSURL           string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    "mailbox-service",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	spiffeSource, err := workloadauth.Source(ctx, envOr(workloadauth.EndpointSocketEnv, ""))
	if err != nil {
		return fmt.Errorf("mailbox-service spiffe source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "mailbox-service spiffe source close", "error", err)
		}
	}()
	secretsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("mailbox-service secrets mtls: %w", err)
	}
	secretsClient, err := secretsclient.NewClientWithResponses(requireEnvValue("MAILBOX_SERVICE_SECRETS_URL"), secretsclient.WithHTTPClient(secretsHTTPClient))
	if err != nil {
		return fmt.Errorf("mailbox-service secrets client: %w", err)
	}

	transport := otelhttp.NewTransport(
		http.DefaultTransport,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return "http " + r.Method + " " + r.URL.Host
		}),
	)
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
	streamClient := &http.Client{
		Transport: transport,
	}

	proxy, err := sessionproxy.New(sessionproxy.Config{
		StalwartBaseURL: cfg.StalwartBaseURL,
		PublicBaseURL:   cfg.PublicBaseURL,
	}, httpClient, logger)
	if err != nil {
		return fmt.Errorf("create session proxy: %w", err)
	}

	// ceo/agents passwords are mail-protocol credentials for human mailboxes
	// and stay as bootstrap material per docs/architecture/workload-identity.md
	// § Persistent bootstrap material. The Stalwart Management API admin
	// password is a workload secret and rides through secrets-service like
	// other provider secrets.
	ceoPassword, err := loadCredential("stalwart-ceo-password")
	if err != nil {
		return err
	}
	agentsPassword, err := loadCredential("stalwart-agents-password")
	if err != nil {
		return err
	}
	mailboxPasswords := map[string]string{
		"ceo":    ceoPassword,
		"agents": agentsPassword,
	}
	forwardTo, err := credentialOr("forward-to", "")
	if err != nil {
		return err
	}
	runtimeSecrets, err := readRuntimeSecrets(ctx, secretsClient,
		secretsclient.MailboxResendAPIKeyName,
		secretsclient.MailboxStalwartAdminPasswordName,
	)
	if err != nil {
		return fmt.Errorf("mailbox-service runtime provider secret: %w", err)
	}
	resendAPIKey := requireSecretField(runtimeSecrets, secretsclient.MailboxResendAPIKeyName, "mailbox-service resend provider secret")
	adminPassword := requireSecretField(runtimeSecrets, secretsclient.MailboxStalwartAdminPasswordName, "mailbox-service stalwart provider secret")

	pgConfig, err := pgxpool.ParseConfig(cfg.PGDSN)
	if err != nil {
		return fmt.Errorf("parse pg-dsn: %w", err)
	}
	pgConfig.MaxConns = 8
	pgPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("open pg pool: %w", err)
	}
	defer pgPool.Close()

	store := mailstore.New(pgPool)
	fwd := forwarder.New(forwarder.Config{
		StalwartBaseURL: cfg.StalwartBaseURL,
		MailboxUser:     cfg.MailboxUser,
		LocalDomain:     cfg.LocalDomain,
		FromAddress:     cfg.ForwarderFromAddress,
		FromName:        cfg.ForwarderFromName,
		StatePath:       cfg.ForwarderStatePath,
		PollInterval:    cfg.ForwarderPollInterval,
		QueryLimit:      cfg.ForwarderQueryLimit,
		SeenLimit:       cfg.ForwarderSeenLimit,
		BootstrapWindow: cfg.ForwarderBootstrapMax,
	}, forwarder.Credentials{
		MailboxPassword: ceoPassword,
		ForwardTo:       forwardTo,
		ResendAPIKey:    resendAPIKey,
	}, logger, httpClient)

	syncManager := mailboxsync.New(mailboxsync.Config{
		StalwartBaseURL:   cfg.StalwartBaseURL,
		AdminPassword:     adminPassword,
		MailboxPasswords:  mailboxPasswords,
		DiscoveryInterval: cfg.SyncDiscoveryInterval,
		ReconcileInterval: cfg.SyncReconcileInterval,
	}, store, logger, httpClient, streamClient)

	service := app.New(cfg.StalwartBaseURL, cfg.PublicBaseURL, proxy, fwd, store, syncManager)

	mux := http.NewServeMux()
	_, protectedAPI := api.NewAPI(mux, version, cfg.ListenAddr, service)
	authHandler := auth.Middleware(auth.Config{
		IssuerURL: cfg.AuthIssuerURL,
		Audience:  cfg.AuthAudience,
		JWKSURL:   cfg.AuthJWKSURL,
	})(protectedAPI)
	mux.Handle("/api/v1/mail/", authHandler)
	service.RegisterRoutes(mux)

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           otelhttp.NewHandler(mux, "mailbox-service"),
		ReadHeaderTimeout: 5 * time.Second,
	}

	service.StartBackground(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.InfoContext(ctx, "mailbox-service: started",
		"listen_addr", cfg.ListenAddr,
		"stalwart_base_url", cfg.StalwartBaseURL,
		"public_base_url", cfg.PublicBaseURL,
	)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

func loadConfig() (config, error) {
	publicBaseURL, err := requireEnv("MAILBOX_SERVICE_STALWART_PUBLIC_BASE_URL")
	if err != nil {
		return config{}, err
	}
	localDomain, err := requireEnv("MAILBOX_SERVICE_STALWART_LOCAL_DOMAIN")
	if err != nil {
		return config{}, err
	}
	forwarderFromAddress, err := requireEnv("MAILBOX_SERVICE_FORWARDER_FROM_ADDRESS")
	if err != nil {
		return config{}, err
	}

	pollInterval := 5 * time.Second
	if raw := os.Getenv("MAILBOX_SERVICE_FORWARDER_POLL_INTERVAL"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return config{}, fmt.Errorf("parse MAILBOX_SERVICE_FORWARDER_POLL_INTERVAL: %w", err)
		}
		pollInterval = value
	}
	discoveryInterval := 2 * time.Minute
	if raw := os.Getenv("MAILBOX_SERVICE_SYNC_DISCOVERY_INTERVAL"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return config{}, fmt.Errorf("parse MAILBOX_SERVICE_SYNC_DISCOVERY_INTERVAL: %w", err)
		}
		discoveryInterval = value
	}
	reconcileInterval := 10 * time.Minute
	if raw := os.Getenv("MAILBOX_SERVICE_SYNC_RECONCILE_INTERVAL"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return config{}, fmt.Errorf("parse MAILBOX_SERVICE_SYNC_RECONCILE_INTERVAL: %w", err)
		}
		reconcileInterval = value
	}
	authIssuerURL, err := requireEnv("MAILBOX_SERVICE_AUTH_ISSUER_URL")
	if err != nil {
		return config{}, err
	}
	authAudience, err := requireEnv("MAILBOX_SERVICE_AUTH_AUDIENCE")
	if err != nil {
		return config{}, err
	}
	authJWKSURL, err := requireEnv("MAILBOX_SERVICE_AUTH_JWKS_URL")
	if err != nil {
		return config{}, err
	}

	return config{
		ListenAddr:            envOr("MAILBOX_SERVICE_LISTEN_ADDR", "127.0.0.1:4246"),
		PGDSN:                 envOr("MAILBOX_SERVICE_PG_DSN", "postgres://mailbox_service@/mailbox_service?host=/var/run/postgresql&sslmode=disable"),
		StalwartBaseURL:       envOr("MAILBOX_SERVICE_STALWART_BASE_URL", "http://127.0.0.1:8090"),
		PublicBaseURL:         publicBaseURL,
		MailboxUser:           envOr("MAILBOX_SERVICE_STALWART_MAILBOX", "ceo"),
		LocalDomain:           localDomain,
		ForwarderFromAddress:  forwarderFromAddress,
		ForwarderFromName:     envOr("MAILBOX_SERVICE_FORWARDER_FROM_NAME", "forge-metal"),
		ForwarderStatePath:    envOr("MAILBOX_SERVICE_FORWARDER_STATE_PATH", "/var/lib/mailbox-service/forwarder-state.json"),
		ForwarderPollInterval: pollInterval,
		ForwarderQueryLimit:   100,
		ForwarderSeenLimit:    1024,
		ForwarderBootstrapMax: 100,
		SyncDiscoveryInterval: discoveryInterval,
		SyncReconcileInterval: reconcileInterval,
		AuthIssuerURL:         authIssuerURL,
		AuthAudience:          authAudience,
		AuthJWKSURL:           authJWKSURL,
	}, nil
}

func loadCredential(name string) (string, error) {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		return "", fmt.Errorf("CREDENTIALS_DIRECTORY not set")
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", fmt.Errorf("load credential %s: %w", name, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func requireSecretField(values map[string]string, field string, label string) string {
	value := strings.TrimSpace(values[field])
	if value == "" {
		fmt.Fprintf(os.Stderr, "%s missing required field %s\n", label, field)
		os.Exit(1)
	}
	return value
}

func readRuntimeSecrets(ctx context.Context, client *secretsclient.ClientWithResponses, secretNames ...string) (map[string]string, error) {
	if client == nil {
		return nil, fmt.Errorf("runtime secrets client is required")
	}
	secretCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values := make(map[string]string, len(secretNames))
	for _, secretName := range secretNames {
		resp, err := client.ReadSecretWithResponse(secretCtx, secretName)
		if err != nil {
			return nil, fmt.Errorf("read runtime secret %s: %w", secretName, err)
		}
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("read runtime secret %s: unexpected status %d: %s", secretName, resp.StatusCode(), strings.TrimSpace(string(resp.Body)))
		}
		values[secretName] = resp.JSON200.Value
	}
	return values, nil
}

func credentialOr(name, fallback string) (string, error) {
	value, err := loadCredential(name)
	if err != nil {
		return "", err
	}
	if value == "" {
		return fallback, nil
	}
	return value, nil
}

func requireEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("required env %s is empty", key)
	}
	return value, nil
}

func requireEnvValue(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		fmt.Fprintf(os.Stderr, "required env %s is empty\n", key)
		os.Exit(1)
	}
	return value
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
