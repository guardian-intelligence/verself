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
	fmotel "github.com/forge-metal/otel"
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
	StalwartBaseURL       string
	PublicBaseURL         string
	OTLPEndpoint          string
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
		OTLPEndpoint:   cfg.OTLPEndpoint,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

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

	pgDSN, err := loadCredential("pg-dsn")
	if err != nil {
		return err
	}
	adminPassword, err := loadCredential("stalwart-admin-password")
	if err != nil {
		return err
	}
	agentPasswordSeed, err := loadCredential("stalwart-agent-password")
	if err != nil {
		return err
	}
	mailboxPassword, err := credentialOr("ceo-password", "")
	if err != nil {
		return err
	}
	forwardTo, err := credentialOr("forward-to", "")
	if err != nil {
		return err
	}
	resendAPIKey, err := credentialOr("resend-api-key", "")
	if err != nil {
		return err
	}

	pgConfig, err := pgxpool.ParseConfig(pgDSN)
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
		MailboxPassword: mailboxPassword,
		ForwardTo:       forwardTo,
		ResendAPIKey:    resendAPIKey,
	}, logger, httpClient)

	syncManager := mailboxsync.New(mailboxsync.Config{
		StalwartBaseURL:   cfg.StalwartBaseURL,
		AdminPassword:     adminPassword,
		AgentPasswordSeed: agentPasswordSeed,
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
		StalwartBaseURL:       envOr("MAILBOX_SERVICE_STALWART_BASE_URL", "http://127.0.0.1:8090"),
		PublicBaseURL:         publicBaseURL,
		OTLPEndpoint:          envOr("MAILBOX_SERVICE_OTLP_ENDPOINT", "127.0.0.1:4317"),
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

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
