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
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/billing-service/internal/billing"
	"github.com/forge-metal/billing-service/internal/billing/ledger"
	"github.com/forge-metal/billing-service/internal/billingapi"
	fmotel "github.com/forge-metal/otel"
	secretsclient "github.com/forge-metal/secrets-service/client"
)

const serviceVersion = "2.0.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	pgDSN := requireEnv("BILLING_PG_DSN")

	listenAddr := envOr("BILLING_LISTEN_ADDR", "127.0.0.1:4242")
	internalListenAddr := envOr("BILLING_INTERNAL_LISTEN_ADDR", "127.0.0.1:4255")
	chAddress := envOr("BILLING_CH_ADDRESS", "127.0.0.1:9440")
	chUser := envOr("BILLING_CH_USER", "billing_service")
	tbAddress := envOr("BILLING_TB_ADDRESS", "127.0.0.1:3320")
	tbClusterID := envUint64Or("BILLING_TB_CLUSTER_ID", 0)
	secretsURL := requireEnv("BILLING_SECRETS_URL")
	authIssuerURL := requireEnv("BILLING_AUTH_ISSUER_URL")
	authAudience := requireEnv("BILLING_AUTH_AUDIENCE")
	authJWKSURL := envOr("BILLING_AUTH_JWKS_URL", "")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: "billing-service", ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	spiffeSource, err := workloadauth.Source(ctx, envOr(workloadauth.EndpointSocketEnv, ""))
	if err != nil {
		return fmt.Errorf("billing spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "billing-service spiffe source close", "error", err)
		}
	}()
	secretsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("billing secrets mtls: %w", err)
	}
	secretsClient, err := secretsclient.NewClientWithResponses(secretsURL, secretsclient.WithHTTPClient(secretsHTTPClient))
	if err != nil {
		return fmt.Errorf("billing secrets client: %w", err)
	}
	stripeSecrets, err := readRuntimeSecrets(ctx, secretsClient,
		secretsclient.BillingStripeSecretKeyName,
		secretsclient.BillingStripeWebhookSecretName,
	)
	if err != nil {
		return fmt.Errorf("billing stripe provider secret: %w", err)
	}
	stripeKey := strings.TrimSpace(stripeSecrets[secretsclient.BillingStripeSecretKeyName])
	webhookSecret := strings.TrimSpace(stripeSecrets[secretsclient.BillingStripeWebhookSecretName])
	if stripeKey != "" && webhookSecret == "" {
		return fmt.Errorf("billing stripe provider secret missing required field webhook_secret")
	}
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

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, credentialPath("clickhouse-ca-cert"))
	if err != nil {
		return fmt.Errorf("billing clickhouse tls: %w", err)
	}
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddress},
		Auth: clickhouse.Auth{
			Database: "forge_metal",
			Username: chUser,
		},
		TLS: chTLSConfig,
	})
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

	internalPeerIDs, err := workloadauth.PeerIDsForSource(
		spiffeSource,
		workloadauth.ServiceSandboxRental,
		workloadauth.ServiceSecrets,
	)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("billing spiffe internal tls: %w", err)
	}

	privateMux := http.NewServeMux()
	billingapi.NewAPI(privateMux, billingapi.Config{Version: serviceVersion, ListenAddr: listenAddr, Client: billingClient, Logger: logger, InternalPeers: internalPeerIDs, StripeWebhookSecret: webhookSecret})
	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	protected := auth.Middleware(auth.Config{IssuerURL: authIssuerURL, Audience: authAudience, JWKSURL: authJWKSURL})(privateMux)
	rootMux.Handle("/", billingHandler(privateMux, protected))

	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, privateMux)
	if err != nil {
		return fmt.Errorf("billing internal allowlist: %w", err)
	}
	srv := &http.Server{Addr: listenAddr, Handler: otelhttp.NewHandler(rootMux, "billing-service"), ReadHeaderTimeout: 10 * time.Second}
	internalSrv := &http.Server{
		Addr:              internalListenAddr,
		Handler:           otelhttp.NewHandler(internalAllowlist, "billing-service-internal"),
		TLSConfig:         internalTLSConfig,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "billing shutdown", "error", err)
		}
		if err := internalSrv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "billing internal shutdown", "error", err)
		}
	}()
	logger.Info("billing-service listening", "addr", listenAddr)
	logger.Info("billing-service internal listening", "addr", internalListenAddr)
	errCh := make(chan error, 2)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen billing-service: %w", err)
			return
		}
		errCh <- nil
	}()
	go func() {
		if err := internalSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen billing-service internal: %w", err)
			return
		}
		errCh <- nil
	}()
	var firstErr error
	for range 2 {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
			stop()
		}
	}
	if firstErr != nil {
		return firstErr
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

func credentialPath(name string) string {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		fmt.Fprintf(os.Stderr, "CREDENTIALS_DIRECTORY not set for credential %s\n", name)
		os.Exit(1)
	}
	return filepath.Join(dir, name)
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
