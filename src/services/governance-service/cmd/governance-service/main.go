package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	governanceapi "github.com/verself/governance-service/internal/api"
	"github.com/verself/governance-service/internal/governance"
	"github.com/verself/governance-service/migrations"
	iamclient "github.com/verself/iam-service/client"
	verselfotel "github.com/verself/observability/otel"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName    = "governance-service"
	serviceVersion = "1.0.0"
)

func main() {
	if handled, err := runRecoveryCLI(context.Background(), os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if handled, err := runMigrationCLI(context.Background(), os.Args[1:]); handled {
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
		return true, fmt.Errorf("usage: migrate [--resource-graph path] [--resource-name name] up")
	}
	runtimeCfg, err := loadGovernanceRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return true, err
	}
	return true, migrations.UpDSN(ctx, serviceName, runtimeCfg.PostgresDSN)
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opts, err := parseRunOptions(args)
	if err != nil {
		return err
	}
	runtimeCfg, err := loadGovernanceRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return err
	}
	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	apiActivityHMACKey := cfg.RequireCredential(runtimeCfg.APIActivityHMACKeyName)
	authAudience := cfg.RequireCredential(runtimeCfg.AuthAudienceName)
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, runtimeCfg.SPIFFEEndpointSocket)
	if err != nil {
		return fmt.Errorf("governance spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "governance-service spiffe source close", "error", err)
		}
	}()
	iamHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceIAM, nil)
	if err != nil {
		return fmt.Errorf("governance iam mtls: %w", err)
	}
	iamClient, err := iamclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceIAM), iamclient.WithHTTPClient(iamHTTPClient))
	if err != nil {
		return fmt.Errorf("governance iam client: %w", err)
	}
	pg, err := openPool(ctx, runtimeCfg.PostgresDSN, runtimeCfg.PostgresMaxConns)
	if err != nil {
		return fmt.Errorf("open governance postgres: %w", err)
	}
	defer pg.Close()
	identityPG, err := openPool(ctx, runtimeCfg.IdentityPostgresDSN, runtimeCfg.IdentityPostgresMaxConn)
	if err != nil {
		return fmt.Errorf("open identity postgres: %w", err)
	}
	defer identityPG.Close()
	billingPG, err := openPool(ctx, runtimeCfg.BillingPostgresDSN, runtimeCfg.BillingPostgresMaxConns)
	if err != nil {
		return fmt.Errorf("open billing postgres: %w", err)
	}
	defer billingPG.Close()
	sandboxPG, err := openPool(ctx, runtimeCfg.SandboxPostgresDSN, runtimeCfg.SandboxPostgresMaxConns)
	if err != nil {
		return fmt.Errorf("open sandbox postgres: %w", err)
	}
	defer sandboxPG.Close()

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, runtimeCfg.ClickHouseCACertPath)
	if err != nil {
		return fmt.Errorf("governance clickhouse tls: %w", err)
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

	svc := &governance.Service{
		PG:               pg,
		IdentityPG:       identityPG,
		BillingPG:        billingPG,
		SandboxPG:        sandboxPG,
		CH:               chConn,
		Logger:           logger,
		HMACKey:          []byte(apiActivityHMACKey),
		HMACKeyID:        runtimeCfg.APIActivityHMACKeyID,
		ExportDir:        runtimeCfg.ExportDir,
		ExportTTL:        time.Duration(runtimeCfg.ExportTTLHours) * time.Hour,
		PublicBaseURL:    runtimeCfg.PublicBaseURL,
		Environment:      runtimeCfg.Environment,
		ServiceVersion:   serviceVersion,
		WriterInstanceID: runtimeCfg.WriterInstanceID,
		InstallationID:   runtimeCfg.InstallationID,
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("governance readiness: %w", err)
	}
	go runAPIActivityProjector(ctx, logger, svc)

	apiActivityClientIDs, err := workloadauth.PeerIDsForSource(
		spiffeSource,
		workloadauth.ServiceIAM,
		workloadauth.ServiceProfile,
		workloadauth.ServiceSandboxRental,
		workloadauth.ServiceSecrets,
		workloadauth.ServiceObjectStorageAdmin,
	)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, apiActivityClientIDs...)
	if err != nil {
		return fmt.Errorf("governance spiffe internal tls: %w", err)
	}

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := svc.Ready(readyCtx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})

	privateMux := http.NewServeMux()
	governanceapi.NewAPI(privateMux, serviceVersion, "http://"+opts.ListenAddr, svc, iamclient.NewAuthorizer(iamClient))
	authHandler := auth.Middleware(auth.Config{
		IssuerURL: runtimeCfg.AuthIssuerURL,
		Audience:  authAudience,
	})(privateMux)
	rootMux.Handle("/", authHandler)

	internalMux := http.NewServeMux()
	governanceapi.NewInternalAPI(internalMux, serviceVersion, "https://"+opts.InternalListenAddr, svc)
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(apiActivityClientIDs, internalMux)
	if err != nil {
		return fmt.Errorf("governance internal allowlist: %w", err)
	}

	public := httpserver.New(opts.ListenAddr, otelhttp.NewHandler(maxBody(rootMux, 1<<20), serviceName))
	internal := httpserver.New(opts.InternalListenAddr, otelhttp.NewHandler(maxBody(internalAllowlist, 1<<20), serviceName+"-internal"))
	internal.TLSConfig = internalTLSConfig

	return httpserver.RunPair(ctx, logger, public, internal)
}

func runAPIActivityProjector(ctx context.Context, logger *slog.Logger, svc *governance.Service) {
	const batchLimit = 100

	project := func(ctx context.Context) {
		total := 0
		for {
			projectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			count, err := svc.ProjectPendingAPIActivities(projectCtx, batchLimit)
			cancel()
			if err != nil {
				logger.ErrorContext(ctx, "governance: project pending API activities", "error", err)
				return
			}
			total += count
			if count < batchLimit {
				break
			}
		}
		if total > 0 {
			logger.InfoContext(ctx, "governance: projected pending API activities", "count", total)
		}
	}
	project(ctx)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			project(ctx)
		}
	}
}

func openPool(ctx context.Context, dsn string, maxConns int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = int32FromInt(maxConns, "postgres.maxConns")
	config.MinConns = 1
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
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

func maxBody(next http.Handler, limit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}
