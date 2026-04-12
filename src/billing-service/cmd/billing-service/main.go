package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
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
	internalRole := envOr("BILLING_INTERNAL_ROLE", "billing_internal")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: "billing-service", ServiceVersion: "2.0.0"})
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

	meteringWriter := billing.NewClickHouseMeteringWriter(chConn, "forge_metal")
	sc := stripe.NewClient(stripeKey)
	cfg := billing.DefaultConfig()
	cfg.StripeSecretKey = stripeKey
	cfg.TigerBeetleAddresses = tbAddresses
	cfg.TigerBeetleClusterID = tbClusterID

	billingClient, err := billing.NewClient(tbClient, pg, sc, meteringWriter, cfg)
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	rootMux.HandleFunc("POST /webhooks/stripe", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read webhook body", http.StatusBadRequest)
			return
		}
		if err := billingClient.HandleStripeWebhook(r.Context(), body, r.Header.Get("Stripe-Signature"), webhookSecret); err != nil {
			logger.ErrorContext(r.Context(), "billing webhook", "error", err)
			http.Error(w, "webhook processing failed", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	privateMux := http.NewServeMux()
	billingapi.NewAPI(privateMux, billingapi.Config{
		Version:      "2.0.0",
		ListenAddr:   listenAddr,
		Client:       billingClient,
		Logger:       logger,
		InternalRole: internalRole,
	})
	protected := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
		ProjectID: authAudience,
		JWKSURL:   authJWKSURL,
	})(privateMux)
	rootMux.Handle("/", billingHandler(privateMux, protected))

	projectorCtx, cancelProjector := context.WithCancel(ctx)
	defer cancelProjector()
	projectorDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-projectorCtx.Done():
				projectorDone <- projectorCtx.Err()
				return
			case <-ticker.C:
				if _, err := billingClient.ProjectPendingWindows(projectorCtx, 100); err != nil && !errors.Is(err, context.Canceled) {
					logger.ErrorContext(projectorCtx, "billing projector", "error", err)
				}
			}
		}
	}()

	rootHandler := fmotel.CorrelationMiddleware(rootMux)
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(rootHandler, "billing-service"),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "billing shutdown", "error", err)
		}
	}()

	logger.Info("billing: listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		cancelProjector()
		return fmt.Errorf("billing listen: %w", err)
	}

	cancelProjector()
	if err := <-projectorDone; err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("billing projector: %w", err)
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
	if strings.HasPrefix(path, "/internal/billing/v1/orgs/") {
		return true
	}
	switch path {
	case "/internal/billing/v1/checkout", "/internal/billing/v1/subscribe", "/internal/billing/v1/portal":
		return true
	default:
		return false
	}
}
