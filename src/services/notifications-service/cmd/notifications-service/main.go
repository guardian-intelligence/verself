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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	notificationsapi "github.com/verself/notifications-service/internal/api"
	"github.com/verself/notifications-service/internal/notifications"
	"github.com/verself/notifications-service/migrations"
	verselfotel "github.com/verself/observability/otel"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName      = notifications.ServiceName
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
			logger.ErrorContext(context.Background(), "notifications-service otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4260")
	authIssuerURL := cfg.RequireURL("VERSELF_AUTH_ISSUER_URL")
	authAudience := cfg.RequireString("VERSELF_AUTH_AUDIENCE")
	natsURL := cfg.String("NOTIFICATIONS_NATS_URL", notifications.NATSDefaultURL)
	chAddress := cfg.String("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("VERSELF_CLICKHOUSE_USER", "notifications_service")
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 8)
	pgMinConns := cfg.Int("VERSELF_PG_MIN_CONNS", 1)
	pgMaxLifetime := cfg.Int("VERSELF_PG_CONN_MAX_LIFETIME_SECONDS", 1800)
	pgMaxIdle := cfg.Int("VERSELF_PG_CONN_MAX_IDLE_SECONDS", 300)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("notifications spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "notifications-service spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceNotifications); err != nil {
		return err
	}

	pg, err := openPool(ctx, pgDSN, pgMaxConns, pgMinConns, pgMaxLifetime, pgMaxIdle)
	if err != nil {
		return fmt.Errorf("open notifications postgres: %w", err)
	}
	defer pg.Close()

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, chCACertPath)
	if err != nil {
		return fmt.Errorf("notifications clickhouse tls: %w", err)
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

	bus, err := notifications.NewNATSBus(ctx, natsURL, spiffeSource, logger)
	if err != nil {
		return fmt.Errorf("open notifications nats bus: %w", err)
	}
	defer bus.Close()

	svc := &notifications.Service{
		PG:        pg,
		CH:        chConn,
		Publisher: bus,
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("notifications readiness: %w", err)
	}
	runtime, err := notifications.NewRuntime(pg, svc, logger)
	if err != nil {
		return fmt.Errorf("create notifications river runtime: %w", err)
	}
	if err := runtime.Start(ctx); err != nil {
		return err
	}
	if err := runtime.EnqueueMaintenance(ctx, 5*time.Second); err != nil {
		return fmt.Errorf("enqueue initial notifications maintenance: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.Stop(stopCtx); err != nil {
			logger.ErrorContext(context.Background(), "notifications river runtime stop", "error", err)
		}
	}()

	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()
	go runBackgroundLoop(bgCtx, logger, runtime)
	go func() {
		if err := bus.RunConsumer(bgCtx, svc); err != nil && !errors.Is(err, context.Canceled) {
			logger.ErrorContext(context.Background(), "notifications nats consumer stopped", "error", err)
			stop()
		}
	}()

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

	privateMux := http.NewServeMux()
	notificationsapi.NewAPI(privateMux, notificationsapi.Config{Version: serviceVersion, ListenAddr: listenAddr, Service: svc})
	authenticated := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
	})(privateMux)
	rootMux.Handle("/", authenticated)

	public := httpserver.New(listenAddr, otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName))
	return httpserver.Run(ctx, logger, public)
}

func openPool(ctx context.Context, dsn string, maxConns, minConns, maxLifetimeSeconds, maxIdleSeconds int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = int32FromInt(maxConns, "NOTIFICATIONS_PG_MAX_CONNS")
	config.MinConns = int32FromInt(minConns, "NOTIFICATIONS_PG_MIN_CONNS")
	config.MaxConnLifetime = time.Duration(maxLifetimeSeconds) * time.Second
	config.MaxConnIdleTime = time.Duration(maxIdleSeconds) * time.Second
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

func runBackgroundLoop(ctx context.Context, logger *slog.Logger, runtime *notifications.Runtime) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := runtime.EnqueueMaintenance(ctx, 5*time.Second); err != nil {
				logger.WarnContext(ctx, "notifications maintenance enqueue", "error", err)
			}
		}
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
