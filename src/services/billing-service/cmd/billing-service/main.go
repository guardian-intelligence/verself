package main

import (
	"context"
	"errors"
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

	"github.com/verself/billing-service/internal/billing"
	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/billing-service/internal/billingapi"
	"github.com/verself/billing-service/migrations"
	iamclient "github.com/verself/iam-service/client"
	verselfotel "github.com/verself/observability/otel"
	secretsinternalclient "github.com/verself/secrets-service/internalclient"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName    = "billing-service"
	serviceVersion = "2.0.0"
)

func main() {
	ctx := context.Background()
	if handled, err := runRecoveryCLI(ctx, os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if handled, err := runMigrationCLI(ctx, os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMigrationCLI(ctx context.Context, args []string) (bool, error) {
	if len(args) < 1 || args[0] != "migrate" {
		return false, nil
	}
	opts, remaining, err := parseMigrationOptions(args[1:])
	if err != nil {
		return true, err
	}
	if len(remaining) != 1 || remaining[0] != "up" {
		return true, errors.New("usage: migrate [--resource-graph path] [--resource-name name] up")
	}
	runtimeCfg, err := loadBillingRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return true, err
	}
	return true, migrations.UpDSN(ctx, serviceName, runtimeCfg.PostgresDSN)
}

func run(args []string) error {
	opts, err := parseRunOptions(args)
	if err != nil {
		return err
	}
	runtimeCfg, err := loadBillingRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return err
	}
	cfg := envconfig.New()
	authAudience := cfg.RequireCredential(runtimeCfg.AuthAudienceName)
	if err := cfg.Err(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)
	logger.InfoContext(ctx, "billing-service deploy timing probe", "service_version", serviceVersion)

	spiffeSource, err := workloadauth.Source(ctx, runtimeCfg.SPIFFEEndpointSocket)
	if err != nil {
		return fmt.Errorf("billing spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "billing-service spiffe source close", "error", err)
		}
	}()
	iamHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceIAM, nil)
	if err != nil {
		return fmt.Errorf("billing iam mtls: %w", err)
	}
	iamClient, err := iamclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceIAM), iamclient.WithHTTPClient(iamHTTPClient))
	if err != nil {
		return fmt.Errorf("billing iam client: %w", err)
	}
	secretsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("billing secrets mtls: %w", err)
	}
	secretsClient, err := secretsinternalclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceSecrets), secretsinternalclient.WithHTTPClient(secretsHTTPClient))
	if err != nil {
		return fmt.Errorf("create secrets internal client: %w", err)
	}
	stripeKey, webhookSecret, err := loadStripeProviderSecrets(ctx, secretsClient, runtimeCfg.StripeSecretKeyName, runtimeCfg.StripeWebhookSecretName)
	if err != nil {
		return err
	}
	pgConfig, err := pgxpool.ParseConfig(runtimeCfg.PostgresDSN)
	if err != nil {
		return fmt.Errorf("parse postgres dsn: %w", err)
	}
	pgConfig.MaxConns = int32FromInt(runtimeCfg.PostgresMaxConns, "BillingService.spec.postgres.maxConns")
	pgConfig.MinConns = int32FromInt(runtimeCfg.PostgresMinConns, "BillingService.spec.postgres.minConns")
	pgConfig.MaxConnLifetime = time.Duration(runtimeCfg.PostgresMaxConnLifetimeSecs) * time.Second
	pgConfig.MaxConnIdleTime = time.Duration(runtimeCfg.PostgresMaxConnIdleSecs) * time.Second
	pgPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pgPool.Close()
	if err := pgPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, runtimeCfg.ClickHouseCACertPath)
	if err != nil {
		return fmt.Errorf("billing clickhouse tls: %w", err)
	}
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{runtimeCfg.ClickHouseAddress},
		Auth: clickhouse.Auth{
			Database: "verself",
			Username: runtimeCfg.ClickHouseUser,
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
	ledgerClient, err := ledger.NewClient(runtimeCfg.TigerBeetleClusterID, runtimeCfg.TigerBeetleAddresses)
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
		billingInternalPeerServices()...,
	)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("billing spiffe internal tls: %w", err)
	}

	privateMux := http.NewServeMux()
	billingapi.NewAPI(privateMux, billingapi.Config{Version: serviceVersion, ListenAddr: opts.ListenAddr, Client: billingClient, Logger: logger, Authorizer: iamclient.NewAuthorizer(iamClient), StripeWebhookSecret: webhookSecret, BillingReturnOrigins: runtimeCfg.BillingReturnOrigins, InstallationID: runtimeCfg.InstallationID})
	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	protected := auth.Middleware(auth.Config{IssuerURL: runtimeCfg.AuthIssuerURL, Audience: authAudience})(privateMux)
	rootMux.Handle("/", billingHandler(privateMux, protected))

	internalMux := http.NewServeMux()
	billingapi.NewInternalAPI(internalMux, billingapi.Config{Version: serviceVersion, ListenAddr: "https://" + opts.InternalListenAddr, Client: billingClient, Logger: logger, InternalPeers: internalPeerIDs, InstallationID: runtimeCfg.InstallationID})
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalMux)
	if err != nil {
		return fmt.Errorf("billing internal allowlist: %w", err)
	}

	public := httpserver.New(opts.ListenAddr, otelhttp.NewHandler(rootMux, serviceName))
	internal := httpserver.New(opts.InternalListenAddr, otelhttp.NewHandler(internalAllowlist, serviceName+"-internal"))
	internal.TLSConfig = internalTLSConfig

	return httpserver.RunPair(ctx, logger, public, internal)
}

func billingInternalPeerServices() []string {
	return []string{
		workloadauth.ServiceIAM,
		workloadauth.ServiceSandboxRental,
		workloadauth.ServiceSecrets,
	}
}

func loadStripeProviderSecrets(ctx context.Context, client *secretsinternalclient.Client, stripeSecretName string, webhookSecretName string) (string, string, error) {
	stripeKey, stripeKeyFound, err := loadOptionalRuntimeSecret(ctx, client, stripeSecretName)
	if err != nil {
		return "", "", err
	}
	webhookSecret, webhookSecretFound, err := loadOptionalRuntimeSecret(ctx, client, webhookSecretName)
	if err != nil {
		return "", "", err
	}
	if !stripeKeyFound && !webhookSecretFound {
		return "", "", nil
	}
	if !stripeKeyFound {
		return "", "", fmt.Errorf("billing stripe provider secret %s is required when %s exists", stripeSecretName, webhookSecretName)
	}
	if !webhookSecretFound {
		return "", "", fmt.Errorf("billing stripe provider secret %s is required when %s exists", webhookSecretName, stripeSecretName)
	}
	return stripeKey, webhookSecret, nil
}

func loadOptionalRuntimeSecret(ctx context.Context, client *secretsinternalclient.Client, name string) (string, bool, error) {
	resp, err := client.GetRuntimeSecret(ctx, secretsinternalclient.GetRuntimeSecretRequest{SecretName: secretsinternalclient.SecretName(name)})
	if err != nil {
		return "", false, fmt.Errorf("read runtime secret %s: %w", name, err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		if resp.Result == nil {
			return "", false, fmt.Errorf("read runtime secret %s: empty response", name)
		}
		value := strings.TrimSpace(string(resp.Result.Value))
		if value == "" {
			return "", false, fmt.Errorf("runtime secret %s is empty", name)
		}
		return value, true, nil
	case http.StatusNotFound:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("read runtime secret %s: status %d", name, resp.StatusCode)
	}
}

func int32FromInt(value int, field string) int32 {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	if value < minInt32 || value > maxInt32 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
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
	return false
}
