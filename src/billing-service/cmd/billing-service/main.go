package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v85"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/billing-service/internal/billing"
	"github.com/forge-metal/billing-service/internal/billing/ledger"
	"github.com/forge-metal/billing-service/internal/billingapi"
	fmotel "github.com/forge-metal/otel"
)

const serviceVersion = "2.0.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	pgDSN := requireCredential("pg-dsn")
	stripeKey := credentialOr("stripe-secret-key", "")
	webhookSecret := credentialOr("stripe-webhook-secret", "")
	chPassword := credentialOr("ch-password", "")

	listenAddr := envOr("BILLING_LISTEN_ADDR", "127.0.0.1:4242")
	chAddress := envOr("BILLING_CH_ADDRESS", "127.0.0.1:9000")
	tbAddress := envOr("BILLING_TB_ADDRESS", "127.0.0.1:3320")
	tbClusterID := envUint64Or("BILLING_TB_CLUSTER_ID", 0)
	authIssuerURL := requireEnv("BILLING_AUTH_ISSUER_URL")
	authAudience := requireEnv("BILLING_AUTH_AUDIENCE")
	authJWKSURL := envOr("BILLING_AUTH_JWKS_URL", "")
	internalRole := envOr("BILLING_INTERNAL_ROLE", "billing_internal")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: "billing-service", ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	pgConfig, err := pgxpool.ParseConfig(pgDSN)
	if err != nil {
		return fmt.Errorf("parse postgres dsn: %w", err)
	}
	pgConfig.MaxConns = int32(envInt("BILLING_PG_MAX_CONNS", 12))
	pgConfig.MinConns = int32(envInt("BILLING_PG_MIN_CONNS", 1))
	pgConfig.MaxConnLifetime = time.Duration(envInt("BILLING_PG_CONN_MAX_LIFETIME_SECONDS", 1800)) * time.Second
	pgConfig.MaxConnIdleTime = time.Duration(envInt("BILLING_PG_CONN_MAX_IDLE_SECONDS", 300)) * time.Second
	pgPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pgPool.Close()
	if err := pgPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	chConn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{chAddress}, Auth: clickhouse.Auth{Database: "forge_metal", Username: "default", Password: chPassword}})
	if err != nil {
		return fmt.Errorf("open clickhouse: %w", err)
	}
	defer func() { _ = chConn.Close() }()

	cfg := billing.DefaultConfig()
	cfg.StripeSecretKey = stripeKey
	cfg.UseStripe = stripeKey != ""
	var stripeClient *stripe.Client
	if stripeKey != "" {
		stripeClient = stripe.NewClient(stripeKey)
	}
	ledgerClient, err := ledger.NewClient(tbClusterID, strings.Split(tbAddress, ","))
	if err != nil {
		return fmt.Errorf("create tigerbeetle client: %w", err)
	}
	defer ledgerClient.Close()
	billingClient, err := billing.NewClient(pgPool, stripeClient, chConn, cfg, logger, ledgerClient)
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}
	if err := billingClient.EnsureLedgerBootstrapped(ctx); err != nil {
		return fmt.Errorf("bootstrap billing ledger: %w", err)
	}

	billingRuntime, err := billing.NewRuntime(pgPool, billingClient, logger)
	if err != nil {
		return fmt.Errorf("create billing river runtime: %w", err)
	}
	if err := billingRuntime.Start(ctx); err != nil {
		return err
	}
	if err := billingRuntime.EnqueueMaintenance(ctx, cfg.EventDeliveryProjectEvery); err != nil {
		return fmt.Errorf("enqueue initial billing maintenance: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := billingRuntime.Stop(stopCtx); err != nil {
			logger.ErrorContext(context.Background(), "billing river runtime stop", "error", err)
		}
	}()

	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()
	go runBackgroundLoop(bgCtx, logger, billingRuntime, cfg)

	privateMux := http.NewServeMux()
	billingapi.NewAPI(privateMux, billingapi.Config{Version: serviceVersion, ListenAddr: listenAddr, Client: billingClient, Logger: logger, InternalRole: internalRole, StripeWebhookSecret: webhookSecret})
	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	protected := auth.Middleware(auth.Config{IssuerURL: authIssuerURL, Audience: authAudience, ProjectID: authAudience, JWKSURL: authJWKSURL})(privateMux)
	rootMux.Handle("/", billingHandler(privateMux, protected))

	srv := &http.Server{Addr: listenAddr, Handler: otelhttp.NewHandler(fmotel.CorrelationMiddleware(rootMux), "billing-service"), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "billing shutdown", "error", err)
		}
	}()
	logger.Info("billing-service listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen billing-service: %w", err)
	}
	return nil
}

func runBackgroundLoop(ctx context.Context, logger *slog.Logger, runtime *billing.Runtime, cfg billing.Config) {
	ticker := time.NewTicker(cfg.EventDeliveryProjectEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := runtime.EnqueueMaintenance(ctx, cfg.EventDeliveryProjectEvery); err != nil {
				logger.WarnContext(ctx, "billing maintenance enqueue", "error", err)
			}
		}
	}
}

func billingHandler(public http.Handler, protected http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUnauthenticatedBillingPath(r.URL.Path) {
			public.ServeHTTP(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

func isUnauthenticatedBillingPath(path string) bool {
	if path == "/healthz" || path == "/readyz" || path == "/webhooks/stripe" {
		return true
	}
	if strings.HasPrefix(path, "/openapi") {
		return true
	}
	if strings.HasPrefix(path, "/internal/billing/v1/orgs/") || strings.HasPrefix(path, "/internal/billing/v1/products/") {
		return true
	}
	switch path {
	case "/internal/billing/v1/checkout", "/internal/billing/v1/contracts", "/internal/billing/v1/portal":
		return true
	default:
		return false
	}
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

func requireCredential(name string) string {
	value, err := loadCredential(name)
	if err != nil || value == "" {
		fmt.Fprintf(os.Stderr, "required credential %s: %v\n", name, err)
		os.Exit(1)
	}
	return value
}

func credentialOr(name, fallback string) string {
	value, err := loadCredential(name)
	if err != nil || value == "" {
		return fallback
	}
	return value
}

func requireEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		fmt.Fprintf(os.Stderr, "required env %s is empty\n", key)
		os.Exit(1)
	}
	return value
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s=%q: %v\n", key, value, err)
		os.Exit(1)
	}
	return parsed
}

func envUint64Or(key string, fallback uint64) uint64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s=%q: %v\n", key, value, err)
		os.Exit(1)
	}
	return parsed
}

func envUint64(key string, fallback uint64) uint64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s: %v\n", key, err)
		os.Exit(1)
	}
	return parsed
}
