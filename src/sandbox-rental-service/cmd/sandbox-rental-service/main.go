package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	billingclient "github.com/forge-metal/billing-service/client"
	fmotel "github.com/forge-metal/otel"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"

	sandboxapi "github.com/forge-metal/sandbox-rental-service/internal/api"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
	"github.com/forge-metal/sandbox-rental-service/internal/serviceauth"
)

const (
	verificationRunHeader = "X-Forge-Metal-Verification-Run"
	correlationHeader     = "X-Forge-Metal-Correlation-Id"
	correlationCookie     = "fm_correlation_id"

	sandboxAPIRequestBodyLimit = 1 << 20
	sandboxMaxHeaderBytes      = 16 << 10
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
	billingClientSecret := requireCredential("billing-client-secret")
	webhookSecretKEK := requireCredential("webhook-secret-kek")

	// Non-secret config via Environment=.
	listenAddr := envOr("SANDBOX_LISTEN_ADDR", "127.0.0.1:4243")
	chAddress := envOr("SANDBOX_CH_ADDRESS", "127.0.0.1:9000")
	billingURL := envOr("SANDBOX_BILLING_URL", "http://127.0.0.1:4242")
	billingClientID := requireEnv("SANDBOX_BILLING_CLIENT_ID")
	billingTokenURL := requireEnv("SANDBOX_BILLING_TOKEN_URL")
	billingAuthAudience := requireEnv("SANDBOX_BILLING_AUTH_AUDIENCE")
	billingReturnOrigins, err := sandboxapi.ParseBillingReturnOrigins(requireEnv("SANDBOX_BILLING_RETURN_ORIGINS"))
	if err != nil {
		return fmt.Errorf("SANDBOX_BILLING_RETURN_ORIGINS: %w", err)
	}
	publicBaseURL := requireEnv("SANDBOX_PUBLIC_BASE_URL")
	if err := validatePublicBaseURL(publicBaseURL); err != nil {
		return fmt.Errorf("SANDBOX_PUBLIC_BASE_URL: %w", err)
	}
	authIssuerURL := requireEnv("SANDBOX_AUTH_ISSUER_URL")
	authAudience := requireEnv("SANDBOX_AUTH_AUDIENCE")
	authJWKSURL := envOr("SANDBOX_AUTH_JWKS_URL", "")
	vmOrchestratorSocket := envOr("SANDBOX_VM_ORCHESTRATOR_SOCKET", vmorchestrator.DefaultSocketPath)

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

	billingAuthEditor, err := serviceauth.NewBearerTokenRequestEditor(serviceauth.ClientCredentialsConfig{
		IssuerURL:    authIssuerURL,
		TokenURL:     billingTokenURL,
		ClientID:     billingClientID,
		ClientSecret: billingClientSecret,
		Audience:     billingAuthAudience,
		Transport:    otelhttp.NewTransport(http.DefaultTransport),
	})
	if err != nil {
		return fmt.Errorf("create billing auth editor: %w", err)
	}
	billingClient, err := billingclient.New(billingURL, billingclient.WithHTTPClient(&http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   10 * time.Second,
	}), billingclient.WithRequestEditorFn(billingAuthEditor))
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}

	// --- job service ---

	webhookSecretCodec, err := jobs.NewSecretCodec(webhookSecretKEK)
	if err != nil {
		return fmt.Errorf("create webhook secret codec: %w", err)
	}

	jobService := &jobs.Service{
		PG:                            pg,
		CH:                            chConn,
		CHDatabase:                    "forge_metal",
		Orchestrator:                  orchestrator,
		Billing:                       billingClient,
		BillingVCPUs:                  int(capacity.VCPUsPerVM),
		BillingMemMiB:                 int(capacity.MemoryMiBPerVM),
		BillingRootfsProvisionedBytes: capacity.RootfsProvisionedBytes,
		WebhookSecretCodec:            webhookSecretCodec,
		Logger:                        logger,
	}

	// --- Huma API ---

	rootMux := http.NewServeMux()
	privateMux := http.NewServeMux()
	sandboxapi.NewAPI(privateMux, "1.0.0", listenAddr, jobService, billingClient, sandboxapi.PublicAPIConfig{
		BillingReturnOrigins: billingReturnOrigins,
		PublicBaseURL:        publicBaseURL,
	})
	sandboxapi.RegisterPublicRoutes(rootMux, jobService)

	authHandler := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
		ProjectID: authAudience,
		JWKSURL:   authJWKSURL,
	})(privateMux)
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
	rootMux.Handle("/", verificationHandler)
	rootHandler := http.Handler(rootMux)
	rootHandler = limitPublicAPIRequestBodies(rootHandler, sandboxAPIRequestBodyLimit)
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(rootHandler, "sandbox-rental-service"),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    sandboxMaxHeaderBytes,
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
	sandboxapi.StartWebhookDeliveryWorker(ctx, jobService)

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

func validatePublicBaseURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if !parsed.IsAbs() || parsed.Hostname() == "" {
		return fmt.Errorf("must be an absolute URL with a host")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("scheme %q is not supported", parsed.Scheme)
	}
	if strings.Trim(parsed.EscapedPath(), "/") != "" {
		return fmt.Errorf("must not include a path")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("must not include query string or fragment")
	}
	return nil
}

func limitPublicAPIRequestBodies(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicAPIRequestWithBody(r) {
			if r.ContentLength > maxBytes {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicAPIRequestWithBody(r *http.Request) bool {
	if r == nil || !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}
