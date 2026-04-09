package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	billingclient "github.com/forge-metal/billing-service/client"
	fmotel "github.com/forge-metal/otel"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"

	sandboxapi "github.com/forge-metal/sandbox-rental-service/internal/api"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

const (
	verificationRunHeader = "X-Forge-Metal-Verification-Run"
	correlationHeader     = "X-Forge-Metal-Correlation-Id"
	correlationCookie     = "fm_correlation_id"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: "sandbox-rental-service", ServiceVersion: "1.0.0"})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	// Secrets via systemd LoadCredential=.
	pgDSN := requireCredential("pg-dsn")
	chPassword := credentialOr("ch-password", "")
	forgejoRunnerToken := credentialOr("forgejo-runner-token", "")

	// Non-secret config via Environment=.
	listenAddr := envOr("SANDBOX_LISTEN_ADDR", "127.0.0.1:4243")
	chAddress := envOr("SANDBOX_CH_ADDRESS", "127.0.0.1:9000")
	billingURL := envOr("SANDBOX_BILLING_URL", "http://127.0.0.1:4242")
	authIssuerURL := requireEnv("SANDBOX_AUTH_ISSUER_URL")
	authAudience := requireEnv("SANDBOX_AUTH_AUDIENCE")
	authJWKSURL := envOr("SANDBOX_AUTH_JWKS_URL", "")
	vmOrchestratorSocket := envOr("SANDBOX_VM_ORCHESTRATOR_SOCKET", vmorchestrator.DefaultSocketPath)
	forgejoURL := envOr("SANDBOX_FORGEJO_URL", "")
	forgejoRunnerLabel := envOr("SANDBOX_FORGEJO_RUNNER_LABEL", jobs.RunnerProfileForgeMetal)
	forgejoRunnerBinaryURL := envOr("SANDBOX_FORGEJO_RUNNER_BINARY_URL", "")
	forgejoRunnerBinarySHA256 := envOr("SANDBOX_FORGEJO_RUNNER_BINARY_SHA256", "")

	// --- open connections ---

	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()
	if err := pg.Ping(); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddress},
		Auth: clickhouse.Auth{
			Database: "forge_metal",
			Username: "default",
			Password: chPassword,
		},
	})
	if err != nil {
		return fmt.Errorf("open clickhouse: %w", err)
	}
	defer func() { _ = chConn.Close() }()

	// --- vm-orchestrator client ---

	orchestrator, err := vmorchestrator.NewClient(ctx, vmOrchestratorSocket)
	if err != nil {
		return fmt.Errorf("connect vm-orchestrator: %w", err)
	}
	defer orchestrator.Close()

	capacityCtx, cancelCapacity := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCapacity()
	capacity, err := orchestrator.GetCapacity(capacityCtx)
	if err != nil {
		return fmt.Errorf("query vm-orchestrator capacity: %w", err)
	}

	// --- billing client ---

	billingClient, err := billingclient.New(billingURL, billingclient.WithHTTPClient(&http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   10 * time.Second,
	}))
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}

	// --- job service ---

	jobService := &jobs.Service{
		PG:                        pg,
		CH:                        chConn,
		CHDatabase:                "forge_metal",
		Orchestrator:              orchestrator,
		Billing:                   billingClient,
		BillingVCPUs:              int(capacity.VCPUsPerVM),
		BillingMemMiB:             int(capacity.MemoryMiBPerVM),
		ForgejoURL:                forgejoURL,
		ForgejoRunnerLabel:        forgejoRunnerLabel,
		ForgejoRunnerToken:        forgejoRunnerToken,
		ForgejoRunnerBinaryURL:    forgejoRunnerBinaryURL,
		ForgejoRunnerBinarySHA256: forgejoRunnerBinarySHA256,
		Logger:                    logger,
	}

	// --- Huma API ---

	mux := http.NewServeMux()
	humaAPI := humago.New(mux, huma.DefaultConfig("Sandbox Rental Service", "1.0.0"))

	sandboxapi.RegisterRoutes(humaAPI, jobService, billingClient)

	authHandler := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
		JWKSURL:   authJWKSURL,
	})(mux)
	verificationHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runID := strings.TrimSpace(r.Header.Get(verificationRunHeader))
		if runID != "" {
			r = r.WithContext(jobs.WithVerificationRunID(r.Context(), runID))
		}
		correlationID := strings.TrimSpace(r.Header.Get(correlationHeader))
		if correlationID == "" {
			if cookie, err := r.Cookie(correlationCookie); err == nil {
				correlationID = strings.TrimSpace(cookie.Value)
			}
		}
		if correlationID != "" {
			r = r.WithContext(jobs.WithCorrelationID(r.Context(), correlationID))
		}
		authHandler.ServeHTTP(w, r)
	})

	// All routes require auth (no webhooks or public ops endpoints).
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(verificationHandler, "sandbox-rental-service"),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// --- server lifecycle ---

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "sandbox-rental: shutdown", "error", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reconcileCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := jobService.Reconcile(reconcileCtx); err != nil {
					logger.ErrorContext(reconcileCtx, "sandbox-rental: reconcile", "error", err)
				}
				cancel()
			}
		}
	}()

	logger.Info("sandbox-rental: listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("sandbox-rental: listen: %w", err)
	}

	return nil
}

// --- credential helpers (systemd LoadCredential=) ---

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

func requireCredential(name string) string {
	v, err := loadCredential(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "required credential %s: %v\n", name, err)
		os.Exit(1)
	}
	if v == "" {
		fmt.Fprintf(os.Stderr, "required credential %s is empty\n", name)
		os.Exit(1)
	}
	return v
}

func credentialOr(name, fallback string) string {
	v, err := loadCredential(name)
	if err != nil || v == "" {
		return fallback
	}
	return v
}

// --- env helpers ---

func requireEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		fmt.Fprintf(os.Stderr, "required env %s is empty\n", key)
		os.Exit(1)
	}
	return value
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

