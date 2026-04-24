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

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v85"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	auth "github.com/forge-metal/auth-middleware"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/billing-service/internal/billing"
	"github.com/forge-metal/billing-service/internal/billing/ledger"
	"github.com/forge-metal/billing-service/internal/billingapi"
	"github.com/forge-metal/envconfig"
	"github.com/forge-metal/httpserver"
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
	cfg := envconfig.New()
	pgDSN := cfg.RequireString("BILLING_PG_DSN")
	listenAddr := cfg.String("BILLING_LISTEN_ADDR", "127.0.0.1:4242")
	internalListenAddr := cfg.String("BILLING_INTERNAL_LISTEN_ADDR", "127.0.0.1:4255")
	chAddress := cfg.String("BILLING_CH_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("BILLING_CH_USER", "billing_service")
	tbAddress := cfg.String("BILLING_TB_ADDRESS", "127.0.0.1:3320")
	tbClusterID := cfg.Uint64("BILLING_TB_CLUSTER_ID", 0)
	secretsURL := cfg.RequireURL("BILLING_SECRETS_URL")
	authIssuerURL := cfg.RequireURL("BILLING_AUTH_ISSUER_URL")
	authAudience := cfg.RequireString("BILLING_AUTH_AUDIENCE")
	authJWKSURL := cfg.String("BILLING_AUTH_JWKS_URL", "")
	pgMaxConns := cfg.Int("BILLING_PG_MAX_CONNS", 12)
	pgMinConns := cfg.Int("BILLING_PG_MIN_CONNS", 1)
	pgMaxLifetime := cfg.Int("BILLING_PG_CONN_MAX_LIFETIME_SECONDS", 1800)
	pgMaxIdle := cfg.Int("BILLING_PG_CONN_MAX_IDLE_SECONDS", 300)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	if err := cfg.Err(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: "billing-service", ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
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
	pgConfig.MaxConns = int32(pgMaxConns)
	pgConfig.MinConns = int32(pgMinConns)
	pgConfig.MaxConnLifetime = time.Duration(pgMaxLifetime) * time.Second
	pgConfig.MaxConnIdleTime = time.Duration(pgMaxIdle) * time.Second
	pgPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pgPool.Close()
	if err := pgPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, chCACertPath)
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

	billingCfg := billing.DefaultConfig()
	billingCfg.StripeSecretKey = stripeKey
	billingCfg.UseStripe = stripeKey != ""
	var stripeClient *stripe.Client
	if stripeKey != "" {
		stripeClient = stripe.NewClient(stripeKey)
	}
	ledgerClient, err := ledger.NewClient(tbClusterID, strings.Split(tbAddress, ","))
	if err != nil {
		return fmt.Errorf("create tigerbeetle client: %w", err)
	}
	defer ledgerClient.Close()
	billingClient, err := billing.NewClient(pgPool, stripeClient, chConn, billingCfg, logger, ledgerClient)
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
	if err := billingRuntime.EnqueueMaintenance(ctx, billingCfg.EventDeliveryProjectEvery); err != nil {
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
	go runBackgroundLoop(bgCtx, logger, billingRuntime, billingCfg)

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

	public := httpserver.New(listenAddr, otelhttp.NewHandler(rootMux, "billing-service"))
	internal := httpserver.New(internalListenAddr, otelhttp.NewHandler(internalAllowlist, "billing-service-internal"))
	internal.TLSConfig = internalTLSConfig

	return httpserver.RunPair(ctx, logger, public, internal)
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

func readRuntimeSecrets(ctx context.Context, client *secretsclient.ClientWithResponses, secretNames ...string) (map[string]string, error) {
	ctx, span := otel.Tracer("runtime-secrets").Start(ctx, "secrets.runtime.resolve")
	defer span.End()
	span.SetAttributes(attribute.Int("forge_metal.secret_count", len(secretNames)))

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
