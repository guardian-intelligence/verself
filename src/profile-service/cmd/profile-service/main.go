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

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/verself/auth-middleware"
	workloadauth "github.com/verself/auth-middleware/workload"
	iaminternalclient "github.com/verself/iam-service/internalclient"
	verselfotel "github.com/verself/observability/otel"
	profileapi "github.com/verself/profile-service/internal/api"
	"github.com/verself/profile-service/internal/profile"
	"github.com/verself/profile-service/migrations"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
)

const (
	serviceName      = "profile-service"
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
			logger.ErrorContext(context.Background(), "profile-service otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4258")
	internalListenAddr := cfg.String("VERSELF_INTERNAL_LISTEN_ADDR", "127.0.0.1:4259")
	authIssuerURL := cfg.RequireURL("VERSELF_AUTH_ISSUER_URL")
	authAudience := cfg.RequireString("VERSELF_AUTH_AUDIENCE")
	iamInternalURL := cfg.RequireURL("PROFILE_IAM_INTERNAL_URL")
	governanceAuditURL := cfg.String("PROFILE_GOVERNANCE_AUDIT_URL", "")
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 8)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("profile spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "profile-service spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceProfile); err != nil {
		return err
	}

	pg, err := openPool(ctx, pgDSN, pgMaxConns)
	if err != nil {
		return fmt.Errorf("open profile postgres: %w", err)
	}
	defer pg.Close()

	iamHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceIAM, nil)
	if err != nil {
		return fmt.Errorf("profile identity mtls: %w", err)
	}
	iamClient, err := iaminternalclient.NewClientWithResponses(iamInternalURL, iaminternalclient.WithHTTPClient(iamHTTPClient))
	if err != nil {
		return fmt.Errorf("identity internal client: %w", err)
	}
	svc := &profile.Service{
		Store:    profile.SQLStore{PG: pg},
		Identity: profile.IAMInternalClient{Client: iamClient},
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("profile readiness: %w", err)
	}
	profileapi.ConfigureAuditSink(governanceAuditURL, spiffeSource)

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
	profileapi.NewAPI(privateMux, profileapi.Config{Version: serviceVersion, ListenAddr: listenAddr, Service: svc})
	authenticated := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
	})(privateMux)
	rootMux.Handle("/", profileapi.CaptureRawBearerToken(authenticated))

	internalPeerIDs, err := workloadauth.PeerIDsForSource(spiffeSource, workloadauth.ServiceGovernance)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("profile spiffe internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	profileapi.NewInternalAPI(internalMux, serviceVersion, "https://"+internalListenAddr, svc)
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalMux)
	if err != nil {
		return fmt.Errorf("profile internal allowlist: %w", err)
	}

	public := httpserver.New(listenAddr, otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName))
	internal := httpserver.New(internalListenAddr, otelhttp.NewHandler(limitRequestBodies(internalAllowlist, requestBodyLimit), serviceName+"-internal"))
	internal.TLSConfig = internalTLSConfig
	return httpserver.RunPair(ctx, logger, public, internal)
}

func openPool(ctx context.Context, dsn string, maxConns int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = int32FromInt(maxConns, "PROFILE_PG_MAX_CONNS")
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
