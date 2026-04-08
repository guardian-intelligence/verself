package main

import (
	"context"
	"database/sql"
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
	_ "github.com/lib/pq"
	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/billing-service/internal/billing"
	"github.com/forge-metal/billing-service/internal/billingapi"
	billingruntime "github.com/forge-metal/billing-service/internal/runtime"
	fmotel "github.com/forge-metal/otel"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	pgDSN := requireCredential("pg-dsn")
	stripeKey := requireCredential("stripe-secret-key")
	webhookSecret := requireCredential("stripe-webhook-secret")
	chPassword := credentialOr("ch-password", "")

	tbAddress := envOr("BILLING_TB_ADDRESS", "127.0.0.1:3320")
	tbClusterID := envUint64("BILLING_TB_CLUSTER_ID", 0)
	chAddress := envOr("BILLING_CH_ADDRESS", "127.0.0.1:9000")
	listenAddr := envOr("BILLING_LISTEN_ADDR", "127.0.0.1:4242")
	authIssuerURL := requireEnv("BILLING_AUTH_ISSUER_URL")
	authAudience := requireEnv("BILLING_AUTH_AUDIENCE")
	authJWKSURL := envOr("BILLING_AUTH_JWKS_URL", "")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: "billing-service", ServiceVersion: "1.1.0"})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()
	if err := pg.Ping(); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	tbAddresses := strings.Split(tbAddress, ",")
	tbClient, err := tb.NewClient(tbtypes.ToUint128(tbClusterID), tbAddresses)
	if err != nil {
		return fmt.Errorf("create tigerbeetle client: %w", err)
	}
	defer tbClient.Close()

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

	sc := stripe.NewClient(stripeKey)
	meteringSink := billing.NewClickHouseMeteringWriter(chConn, "forge_metal")
	meteringWriter := billing.NewAsyncMeteringWriter(meteringSink, billing.AsyncMeteringWriterConfig{})
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := meteringWriter.Close(flushCtx); err != nil {
			logger.ErrorContext(ctx, "billing: async metering shutdown", "error", err)
		}
	}()
	reconcileQuerier := billing.NewClickHouseReconcileQuerier(chConn, "forge_metal")

	cfg := billing.DefaultConfig()
	cfg.StripeSecretKey = stripeKey
	cfg.TigerBeetleAddresses = tbAddresses
	cfg.TigerBeetleClusterID = tbClusterID

	billingClient, err := billing.NewClient(tbClient, pg, sc, meteringWriter, cfg)
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}

	app := billingruntime.New(pg, tbClient, chConn, billingClient, reconcileQuerier, webhookSecret, logger)

	mux := http.NewServeMux()
	billingapi.NewAPI(mux, app)
	mux.Handle("POST /webhooks/stripe", app.WebhookHandler())

	authHandler := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
		JWKSURL:   authJWKSURL,
	})(mux)

	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	workerDone := make(chan error, 1)
	go func() {
		workerDone <- app.RunWorker(workerCtx, 100*time.Millisecond)
	}()

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(billingHandler(mux, authHandler), "billing-service"),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "billing: shutdown", "error", err)
		}
	}()

	logger.Info("billing: listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		workerCancel()
		return fmt.Errorf("billing: listen: %w", err)
	}

	workerCancel()
	if err := <-workerDone; err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("billing worker: %w", err)
	}
	return nil
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
	if err != nil {
		fmt.Fprintf(os.Stderr, "required credential %s: %v\n", name, err)
		os.Exit(1)
	}
	if value == "" {
		fmt.Fprintf(os.Stderr, "required credential %s is empty\n", name)
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
	if strings.HasPrefix(path, "/internal/billing/v1/ops/") {
		return true
	}
	// Org-scoped reads and Stripe session creation — loopback-only (nftables
	// blocks external access to port 4242). Called by sandbox-rental-service
	// which enforces its own auth.
	if strings.HasPrefix(path, "/internal/billing/v1/orgs/") {
		return true
	}
	switch path {
	case "/internal/billing/v1/check-quotas", "/internal/billing/v1/reserve", "/internal/billing/v1/settle", "/internal/billing/v1/void":
		return true
	case "/internal/billing/v1/checkout", "/internal/billing/v1/subscribe":
		return true
	default:
		return false
	}
}
