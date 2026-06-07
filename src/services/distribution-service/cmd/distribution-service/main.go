package main

import (
	"context"
	"encoding/json"
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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	distributionapi "github.com/verself/distribution-service/internal/api"
	"github.com/verself/distribution-service/internal/distribution"
	distributionreleaseattest "github.com/verself/distribution-service/internal/releaseattest"
	"github.com/verself/distribution-service/migrations"
	verselfotel "github.com/verself/observability/otel"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName      = distribution.ServiceName
	serviceVersion   = "1.0.0"
	requestBodyLimit = 4 << 20
)

func main() {
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
		return true, errors.New("usage: migrate [--resource-graph path] [--resource-name name] up")
	}
	runtimeCfg, err := loadDistributionRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
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
	runtimeCfg, err := loadDistributionRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return err
	}

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.ErrorContext(context.Background(), "distribution-service otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	spiffeSource, err := workloadauth.Source(ctx, runtimeCfg.SPIFFEEndpointSocket)
	if err != nil {
		return fmt.Errorf("distribution spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "distribution-service spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceDistribution); err != nil {
		return err
	}

	pg, err := openPool(ctx, runtimeCfg.PostgresDSN, runtimeCfg.PostgresMaxConns)
	if err != nil {
		return fmt.Errorf("open distribution postgres: %w", err)
	}
	defer pg.Close()

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, runtimeCfg.ClickHouseCACertPath)
	if err != nil {
		return fmt.Errorf("distribution clickhouse tls: %w", err)
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
	chPingCtx, chPingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer chPingCancel()
	if err := chConn.Ping(chPingCtx); err != nil {
		return fmt.Errorf("ping clickhouse: %w", err)
	}

	svc := &distribution.Service{
		Store:           distribution.SQLStore{PG: pg},
		CH:              chConn,
		Logger:          logger,
		TrustedBuilders: setFromList(runtimeCfg.TrustedBuilders),
		TrustedSigners:  setFromList(runtimeCfg.TrustedSigners),
		ReleaseVerifier: distributionreleaseattest.Verifier{
			TrustedAKs:  map[string]distributionreleaseattest.TrustedAK{},
			PCRPolicies: map[string]distributionreleaseattest.PCRPolicy{},
		},
		InstallationID: runtimeCfg.InstallationID,
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("distribution readiness: %w", err)
	}

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

	publicMux := http.NewServeMux()
	distributionapi.RegisterPublicRoutes(publicMux, distributionapi.Config{Service: svc, InstallationID: runtimeCfg.InstallationID})
	authenticated := auth.Middleware(auth.Config{IssuerURL: runtimeCfg.AuthIssuerURL, Audience: runtimeCfg.AuthAudience})(publicMux)
	rootMux.Handle("/api/v1/distribution/", publicMux)
	rootMux.Handle("/api/", authenticated)
	rootMux.Handle("/v2/", publicMux)

	internalPeerIDs, err := workloadauth.PeerIDsForSource(
		spiffeSource,
		workloadauth.ServiceGovernance,
	)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("distribution internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	distributionapi.RegisterInternalRoutes(internalMux, distributionapi.Config{Service: svc, InstallationID: runtimeCfg.InstallationID})
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalMux)
	if err != nil {
		return fmt.Errorf("distribution internal allowlist: %w", err)
	}

	public := httpserver.New(opts.ListenAddr, otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName))
	internal := httpserver.New(opts.InternalListenAddr, otelhttp.NewHandler(limitRequestBodies(internalAllowlist, requestBodyLimit), serviceName+"-internal"))
	internal.TLSConfig = internalTLSConfig
	return httpserver.RunPair(ctx, logger, public, internal)
}

func openPool(ctx context.Context, dsn string, maxConns int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pgMaxConns, err := int32FromInt(maxConns, "DistributionService.spec.postgres.maxConns")
	if err != nil {
		return nil, err
	}
	config.MaxConns = pgMaxConns
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

func setFromList(values []string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item != "" {
			out[item] = struct{}{}
		}
	}
	return out
}

func recoveryz(svc *distribution.Service) http.HandlerFunc {
	type status struct {
		PostgreSQL string `json:"postgresql"`
		ClickHouse string `json:"clickhouse"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		out := status{PostgreSQL: "ok", ClickHouse: "ok"}
		code := http.StatusOK
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := svc.Store.Ready(ctx); err != nil {
			out.PostgreSQL = err.Error()
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(out)
	}
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
		return strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/")
	default:
		return false
	}
}
