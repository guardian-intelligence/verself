package main

import (
	"context"
	"encoding/json"
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

	verselfotel "github.com/verself/observability/otel"
	releaseapi "github.com/verself/release-service/internal/api"
	"github.com/verself/release-service/internal/release"
	"github.com/verself/release-service/migrations"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName      = release.ServiceName
	serviceVersion   = "1.0.0"
	requestBodyLimit = 1 << 20
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
	return true, migrations.RunCLI(ctx, os.Args[2:], serviceName)
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.ErrorContext(context.Background(), "release-service otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4274")
	serviceListenAddr := cfg.String("VERSELF_SERVICE_LISTEN_ADDR", "127.0.0.1:4275")
	installationID := cfg.RequireString("VERSELF_INSTALLATION_ID")
	chAddress := cfg.String("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("VERSELF_CLICKHOUSE_USER", "release_service")
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	platformOrgID := cfg.String("VERSELF_PLATFORM_ORG_ID", "")
	seedMakeSkill := cfg.Bool("VERSELF_RELEASE_SEED_MAKE_SKILL", false)
	makeSkillSourceCommit := cfg.String("VERSELF_RELEASE_MAKE_SKILL_SOURCE_COMMIT", "")
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 8)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	registryUsername := cfg.CredentialOr("zot-username", "")
	registryPassword := cfg.CredentialOr("zot-password", "")
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("release spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "release-service spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceRelease); err != nil {
		return err
	}

	pg, err := openPool(ctx, pgDSN, pgMaxConns)
	if err != nil {
		return fmt.Errorf("open release postgres: %w", err)
	}
	defer pg.Close()

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, chCACertPath)
	if err != nil {
		return fmt.Errorf("release clickhouse tls: %w", err)
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
	chPingCtx, chPingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer chPingCancel()
	if err := chConn.Ping(chPingCtx); err != nil {
		return fmt.Errorf("ping clickhouse: %w", err)
	}

	svc := &release.Service{
		Store: release.SQLStore{PG: pg},
		Registry: release.OCIRegistry{
			Username: registryUsername,
			Password: registryPassword,
		},
		CH:     chConn,
		Logger: logger,
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("release readiness: %w", err)
	}
	if seedMakeSkill {
		if err := svc.EnsureMakeSkillSeed(ctx, platformOrgID, makeSkillSourceCommit); err != nil {
			return fmt.Errorf("seed make-skill release metadata: %w", err)
		}
	}
	runtime, err := release.NewRuntime(pg, svc, logger)
	if err != nil {
		return err
	}
	if err := runtime.Start(ctx); err != nil {
		return err
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Stop(stopCtx); err != nil {
			logger.ErrorContext(context.Background(), "release runtime stop", "error", err)
		}
	}()
	go maintenanceLoop(ctx, logger, runtime)

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
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
	rootMux.HandleFunc("/recoveryz", recoveryz(svc))

	servicePeerIDs, err := workloadauth.PeerIDsForSource(
		spiffeSource,
		workloadauth.ServiceIAM,
		workloadauth.ServiceGovernance,
		workloadauth.ServiceGitHubIntegration,
		workloadauth.ServiceSourceCodeHosting,
		workloadauth.ServiceSandboxRental,
	)
	if err != nil {
		return err
	}
	serviceTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, servicePeerIDs...)
	if err != nil {
		return fmt.Errorf("release spiffe service tls: %w", err)
	}
	serviceMux := http.NewServeMux()
	releaseapi.NewInternalAPI(serviceMux, serviceVersion, "https://"+serviceListenAddr, svc, installationID)
	serviceAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(servicePeerIDs, serviceMux)
	if err != nil {
		return fmt.Errorf("release service allowlist: %w", err)
	}

	public := httpserver.New(listenAddr, otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName))
	serviceServer := httpserver.New(serviceListenAddr, otelhttp.NewHandler(limitRequestBodies(serviceAllowlist, requestBodyLimit), serviceName+"-service"))
	serviceServer.TLSConfig = serviceTLSConfig
	return httpserver.RunPair(ctx, logger, public, serviceServer)
}

func maintenanceLoop(ctx context.Context, logger *slog.Logger, runtime *release.Runtime) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := runtime.EnqueueMaintenance(ctx, time.Minute); err != nil {
				logger.WarnContext(ctx, "release maintenance enqueue failed", "error", err)
			}
		}
	}
}

func recoveryz(svc *release.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		status := map[string]string{"postgres": "ok", "clickhouse": "ok", "publication_workers": "ok"}
		code := http.StatusOK
		if err := svc.Store.Ready(ctx); err != nil {
			status["postgres"] = err.Error()
			code = http.StatusServiceUnavailable
		}
		if svc.CH == nil {
			status["clickhouse"] = "unavailable"
			code = http.StatusServiceUnavailable
		} else if err := svc.CH.Ping(ctx); err != nil {
			status["clickhouse"] = err.Error()
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(status)
	}
}

func openPool(ctx context.Context, dsn string, maxConns int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pgMaxConns, err := int32FromInt(maxConns, "VERSELF_PG_MAX_CONNS")
	if err != nil {
		return nil, err
	}
	config.MaxConns = pgMaxConns
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

func int32FromInt(value int, field string) (int32, error) {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	if value < minInt32 || value > maxInt32 {
		return 0, fmt.Errorf("%s exceeds int32 range: %d", field, value)
	}
	return int32(value), nil // #nosec G115 -- value is checked against the int32 range above.
}

func limitRequestBodies(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestMayHaveBody(r) {
			if r.ContentLength > maxBytes {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func requestMayHaveBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return strings.HasPrefix(r.URL.Path, "/internal/")
	default:
		return false
	}
}
