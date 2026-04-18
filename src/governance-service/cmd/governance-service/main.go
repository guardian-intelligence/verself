package main

import (
	"context"
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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	governanceapi "github.com/forge-metal/governance-service/internal/api"
	"github.com/forge-metal/governance-service/internal/governance"
	fmotel "github.com/forge-metal/otel"
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

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: "governance-service", ServiceVersion: "1.0.0"})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	pgDSN := requireCredential("pg-dsn")
	identityPGDSN := requireCredential("identity-pg-dsn")
	billingPGDSN := requireCredential("billing-pg-dsn")
	sandboxPGDSN := requireCredential("sandbox-pg-dsn")
	chPassword := credentialOr("ch-password", "")
	auditHMACKey := []byte(requireCredential("audit-hmac-key"))
	internalAuditToken := requireCredential("internal-audit-token")

	listenAddr := envOr("GOVERNANCE_LISTEN_ADDR", "127.0.0.1:4250")
	chAddress := envOr("GOVERNANCE_CH_ADDRESS", "127.0.0.1:9000")
	authIssuerURL := requireEnv("GOVERNANCE_AUTH_ISSUER_URL")
	authAudience := requireEnv("GOVERNANCE_AUTH_AUDIENCE")
	authJWKSURL := envOr("GOVERNANCE_AUTH_JWKS_URL", "")
	exportDir := envOr("GOVERNANCE_EXPORT_DIR", "/var/lib/governance-service/exports")
	publicBaseURL := envOr("GOVERNANCE_PUBLIC_BASE_URL", "")
	writerInstanceID := envOr("GOVERNANCE_WRITER_INSTANCE_ID", hostname())

	pg, err := openPool(ctx, pgDSN, envInt("GOVERNANCE_PG_MAX_CONNS", 8))
	if err != nil {
		return fmt.Errorf("open governance postgres: %w", err)
	}
	defer pg.Close()
	identityPG, err := openPool(ctx, identityPGDSN, envInt("GOVERNANCE_IDENTITY_PG_MAX_CONNS", 4))
	if err != nil {
		return fmt.Errorf("open identity postgres: %w", err)
	}
	defer identityPG.Close()
	billingPG, err := openPool(ctx, billingPGDSN, envInt("GOVERNANCE_BILLING_PG_MAX_CONNS", 4))
	if err != nil {
		return fmt.Errorf("open billing postgres: %w", err)
	}
	defer billingPG.Close()
	sandboxPG, err := openPool(ctx, sandboxPGDSN, envInt("GOVERNANCE_SANDBOX_PG_MAX_CONNS", 4))
	if err != nil {
		return fmt.Errorf("open sandbox postgres: %w", err)
	}
	defer sandboxPG.Close()

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

	svc := &governance.Service{
		PG:               pg,
		IdentityPG:       identityPG,
		BillingPG:        billingPG,
		SandboxPG:        sandboxPG,
		CH:               chConn,
		Logger:           logger,
		HMACKey:          auditHMACKey,
		HMACKeyID:        envOr("GOVERNANCE_AUDIT_HMAC_KEY_ID", "governance-service.v1"),
		ExportDir:        exportDir,
		ExportTTL:        time.Duration(envInt("GOVERNANCE_EXPORT_TTL_HOURS", 168)) * time.Hour,
		PublicBaseURL:    publicBaseURL,
		Environment:      envOr("GOVERNANCE_ENVIRONMENT", "single-node"),
		ServiceVersion:   "1.0.0",
		WriterInstanceID: writerInstanceID,
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("governance readiness: %w", err)
	}
	go runAuditProjector(ctx, logger, svc)

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
	governanceapi.RegisterInternalRoutes(rootMux, svc, internalAuditToken)

	privateMux := http.NewServeMux()
	governanceapi.NewAPI(privateMux, "1.0.0", "http://"+listenAddr, svc)
	authHandler := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
		ProjectID: authAudience,
		JWKSURL:   authJWKSURL,
	})(privateMux)
	rootMux.Handle("/", authHandler)

	handler := http.Handler(rootMux)
	handler = maxBody(handler, 1<<20)
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(handler, "governance-service"),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(context.Background(), "governance: shutdown", "error", err)
		}
	}()

	logger.InfoContext(ctx, "governance-service listening", "addr", listenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve governance-service: %w", err)
	}
	return nil
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
	config.MaxConns = int32(maxConns)
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

func maxBody(next http.Handler, limit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

func requireEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic("missing required env " + name)
	}
	return value
}

func envOr(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
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

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic("invalid integer env " + name + ": " + err.Error())
	}
	return parsed
}

func requireCredential(name string) string {
	value := credentialOr(name, "")
	if value == "" {
		panic("missing required credential " + name)
	}
	return value
}

func credentialOr(name, fallback string) string {
	base := os.Getenv("CREDENTIALS_DIRECTORY")
	if base == "" {
		return fallback
	}
	data, err := os.ReadFile(filepath.Join(base, name))
	if err != nil {
		return fallback
	}
	return strings.TrimSpace(string(data))
}
