package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	verselfotel "github.com/verself/observability/otel"
	secretsclient "github.com/verself/secrets-service/client"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/verself/mailbox-service/internal/api"
	"github.com/verself/mailbox-service/internal/app"
	"github.com/verself/mailbox-service/internal/forwarder"
	"github.com/verself/mailbox-service/internal/mailstore"
	"github.com/verself/mailbox-service/internal/sessionproxy"
	mailboxsync "github.com/verself/mailbox-service/internal/sync"
	"github.com/verself/mailbox-service/migrations"
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
	SecretsURL            string
	CEOPassword           string
	AgentsPassword        string
	ForwardTo             string
	SPIFFEEndpoint        string
}

func main() {
	if handled, err := runMigrationCLI(context.Background()); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMigrationCLI(ctx context.Context) (bool, error) {
	if len(os.Args) < 2 || os.Args[1] != "migrate" {
		return false, nil
	}
	return true, migrations.RunCLI(ctx, os.Args[2:], "mailbox-service")
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    "mailbox-service",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)

	spiffeSource, err := workloadauth.Source(ctx, cfg.SPIFFEEndpoint)
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
	secretsClient, err := secretsclient.NewClientWithResponses(cfg.SecretsURL, secretsclient.WithHTTPClient(secretsHTTPClient))
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
	mailboxPasswords := map[string]string{
		"ceo":    cfg.CEOPassword,
		"agents": cfg.AgentsPassword,
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
		MailboxPassword: cfg.CEOPassword,
		ForwardTo:       cfg.ForwardTo,
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
	})(protectedAPI)
	mux.Handle("/api/v1/mail/", authHandler)
	service.RegisterRoutes(mux)

	server := httpserver.New(cfg.ListenAddr, otelhttp.NewHandler(mux, "mailbox-service"))
	// The /jmap/* proxy streams arbitrarily large attachments from clients
	// into Stalwart; the standard 5s ReadTimeout/WriteTimeout would cut off
	// attachment uploads mid-stream. Keep slowloris protection via
	// ReadHeaderTimeout and let the body run uncapped.
	server.ReadTimeout = 0
	server.WriteTimeout = 0
	service.StartBackground(ctx)
	return httpserver.Run(ctx, logger, server)
}

func loadConfig() (config, error) {
	l := envconfig.New()
	cfg := config{
		ListenAddr:            l.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4246"),
		PGDSN:                 l.RequireString("VERSELF_PG_DSN"),
		StalwartBaseURL:       l.String("MAILBOX_SERVICE_STALWART_BASE_URL", "http://127.0.0.1:8090"),
		PublicBaseURL:         l.RequireURL("MAILBOX_SERVICE_STALWART_PUBLIC_BASE_URL"),
		MailboxUser:           l.String("MAILBOX_SERVICE_STALWART_MAILBOX", "ceo"),
		LocalDomain:           l.RequireString("MAILBOX_SERVICE_STALWART_LOCAL_DOMAIN"),
		ForwarderFromAddress:  l.RequireString("MAILBOX_SERVICE_FORWARDER_FROM_ADDRESS"),
		ForwarderFromName:     l.String("MAILBOX_SERVICE_FORWARDER_FROM_NAME", "verself"),
		ForwarderStatePath:    l.String("MAILBOX_SERVICE_FORWARDER_STATE_PATH", "/var/lib/mailbox-service/forwarder-state.json"),
		ForwarderPollInterval: l.Duration("MAILBOX_SERVICE_FORWARDER_POLL_INTERVAL", 5*time.Second),
		ForwarderQueryLimit:   100,
		ForwarderSeenLimit:    1024,
		ForwarderBootstrapMax: 100,
		SyncDiscoveryInterval: l.Duration("MAILBOX_SERVICE_SYNC_DISCOVERY_INTERVAL", 2*time.Minute),
		SyncReconcileInterval: l.Duration("MAILBOX_SERVICE_SYNC_RECONCILE_INTERVAL", 10*time.Minute),
		AuthIssuerURL:         l.RequireURL("VERSELF_AUTH_ISSUER_URL"),
		AuthAudience:          l.RequireCredential("auth-audience"),
		SecretsURL:            l.RequireURL("MAILBOX_SERVICE_SECRETS_URL"),
		CEOPassword:           l.RequireCredential("stalwart-ceo-password"),
		AgentsPassword:        l.RequireCredential("stalwart-agents-password"),
		ForwardTo:             l.CredentialOr("forward-to", ""),
		SPIFFEEndpoint:        l.String(workloadauth.EndpointSocketEnv, ""),
	}
	if err := l.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
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
	ctx, span := otel.Tracer("runtime-secrets").Start(ctx, "secrets.runtime.resolve")
	defer span.End()
	span.SetAttributes(attribute.Int("verself.secret_count", len(secretNames)))

	if client == nil {
		err := fmt.Errorf("runtime secrets client is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	secretCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values := make(map[string]string, len(secretNames))
	for _, secretName := range secretNames {
		resp, err := client.ReadSecretWithResponse(secretCtx, secretName)
		if err != nil {
			err = fmt.Errorf("read runtime secret %s: %w", secretName, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		if resp.JSON200 == nil {
			err := fmt.Errorf("read runtime secret %s: unexpected status %d: %s", secretName, resp.StatusCode(), strings.TrimSpace(string(resp.Body)))
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		values[secretName] = resp.JSON200.Value
	}
	return values, nil
}
