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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	governanceapi "github.com/verself/governance-service/internal/api"
	"github.com/verself/governance-service/internal/governance"
	"github.com/verself/governance-service/migrations"
	verselfotel "github.com/verself/observability/otel"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

func main() {
	if handled, err := runMigrationCLI(context.Background()); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMigrationCLI(ctx context.Context) (bool, error) {
	if len(os.Args) < 2 || os.Args[1] != "migrate" {
		return false, nil
	}
	return true, migrations.RunCLI(ctx, os.Args[2:], "governance-service")
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: "governance-service", ServiceVersion: "1.0.0"})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	identityPGDSN := cfg.RequireString("GOVERNANCE_IAM_PG_DSN")
	billingPGDSN := cfg.RequireString("GOVERNANCE_BILLING_PG_DSN")
	sandboxPGDSN := cfg.RequireString("GOVERNANCE_SANDBOX_PG_DSN")
	auditHMACKey := cfg.RequireCredential("audit-hmac-key")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4250")
	internalListenAddr := cfg.String("VERSELF_INTERNAL_LISTEN_ADDR", "127.0.0.1:4254")
	chAddress := cfg.String("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("VERSELF_CLICKHOUSE_USER", "governance_service")
	authIssuerURL := cfg.RequireURL("VERSELF_AUTH_ISSUER_URL")
	authAudience := cfg.RequireString("VERSELF_AUTH_AUDIENCE")
	exportDir := cfg.String("GOVERNANCE_EXPORT_DIR", "/var/lib/governance-service/exports")
	publicBaseURL := cfg.String("GOVERNANCE_PUBLIC_BASE_URL", "")
	writerInstanceID := cfg.String("GOVERNANCE_WRITER_INSTANCE_ID", hostname())
	hmacKeyID := cfg.String("GOVERNANCE_AUDIT_HMAC_KEY_ID", "governance-service.v1")
	exportTTLHours := cfg.Int("GOVERNANCE_EXPORT_TTL_HOURS", 168)
	environment := cfg.String("GOVERNANCE_ENVIRONMENT", "single-node")
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 8)
	identityPGMaxConns := cfg.Int("GOVERNANCE_IAM_PG_MAX_CONNS", 4)
	billingPGMaxConns := cfg.Int("GOVERNANCE_BILLING_PG_MAX_CONNS", 4)
	sandboxPGMaxConns := cfg.Int("GOVERNANCE_SANDBOX_PG_MAX_CONNS", 4)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("governance spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "governance-service spiffe source close", "error", err)
		}
	}()
	pg, err := openPool(ctx, pgDSN, pgMaxConns)
	if err != nil {
		return fmt.Errorf("open governance postgres: %w", err)
	}
	defer pg.Close()
	identityPG, err := openPool(ctx, identityPGDSN, identityPGMaxConns)
	if err != nil {
		return fmt.Errorf("open identity postgres: %w", err)
	}
	defer identityPG.Close()
	billingPG, err := openPool(ctx, billingPGDSN, billingPGMaxConns)
	if err != nil {
		return fmt.Errorf("open billing postgres: %w", err)
	}
	defer billingPG.Close()
	sandboxPG, err := openPool(ctx, sandboxPGDSN, sandboxPGMaxConns)
	if err != nil {
		return fmt.Errorf("open sandbox postgres: %w", err)
	}
	defer sandboxPG.Close()

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, chCACertPath)
	if err != nil {
		return fmt.Errorf("governance clickhouse tls: %w", err)
	}
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddress},
		Auth: clickhouse.Auth{
			Database: "verself",
			Username: chUser,
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
		HMACKey:          []byte(auditHMACKey),
		HMACKeyID:        hmacKeyID,
		ExportDir:        exportDir,
		ExportTTL:        time.Duration(exportTTLHours) * time.Hour,
		PublicBaseURL:    publicBaseURL,
		Environment:      environment,
		ServiceVersion:   "1.0.0",
		WriterInstanceID: writerInstanceID,
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("governance readiness: %w", err)
	}
	go runAuditProjector(ctx, logger, svc)

	auditClientIDs, err := workloadauth.PeerIDsForSource(
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
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, auditClientIDs...)
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
	governanceapi.NewAPI(privateMux, "1.0.0", "http://"+listenAddr, svc)
	authHandler := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
	})(privateMux)
	rootMux.Handle("/", authHandler)

	internalMux := http.NewServeMux()
	governanceapi.NewInternalAPI(internalMux, "1.0.0", "https://"+internalListenAddr, svc)
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(auditClientIDs, internalMux)
	if err != nil {
		return fmt.Errorf("governance internal allowlist: %w", err)
	}

	public := httpserver.New(listenAddr, otelhttp.NewHandler(maxBody(rootMux, 1<<20), "governance-service"))
	internal := httpserver.New(internalListenAddr, otelhttp.NewHandler(maxBody(internalAllowlist, 1<<20), "governance-service-internal"))
	internal.TLSConfig = internalTLSConfig

	return httpserver.RunPair(ctx, logger, public, internal)
}

func runAuditProjector(ctx context.Context, logger *slog.Logger, svc *governance.Service) {
	project := func(ctx context.Context) {
		projectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		count, err := svc.ProjectPendingAuditEvents(projectCtx, 100)
		if err != nil {
			logger.ErrorContext(ctx, "governance: project pending audit events", "error", err)
			return
		}
		if count > 0 {
			logger.InfoContext(ctx, "governance: projected pending audit events", "count", count)
		}
	}
	project(ctx)
	ticker := time.NewTicker(5 * time.Second)
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
	config.MaxConns = int32FromInt(maxConns, "GOVERNANCE_PG_MAX_CONNS")
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

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}
