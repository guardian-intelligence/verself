package main

import (
	"context"
	"database/sql"
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
	_ "github.com/lib/pq"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	auth "github.com/forge-metal/auth-middleware"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/envconfig"
	"github.com/forge-metal/httpserver"
	"github.com/forge-metal/identity-service/internal/api"
	"github.com/forge-metal/identity-service/internal/identity"
	"github.com/forge-metal/identity-service/internal/zitadel"
	fmotel "github.com/forge-metal/otel"
)

const (
	serviceName      = "identity-service"
	serviceVersion   = "1.0.0"
	requestBodyLimit = 1 << 20
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

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.ErrorContext(context.Background(), "identity-service otel shutdown", "error", err)
		}
	}()

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("IDENTITY_PG_DSN")
	zitadelAdminToken := cfg.RequireCredential("zitadel-admin-token")
	zitadelActionSigningKey := cfg.RequireCredential("zitadel-action-signing-key")
	chAddress := cfg.String("IDENTITY_CH_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("IDENTITY_CH_USER", "identity_service")
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	listenAddr := cfg.String("IDENTITY_LISTEN_ADDR", "127.0.0.1:4248")
	internalListenAddr := cfg.String("IDENTITY_INTERNAL_LISTEN_ADDR", "127.0.0.1:4241")
	governanceAuditURL := cfg.String("IDENTITY_GOVERNANCE_AUDIT_URL", "")
	authIssuerURL := cfg.RequireURL("IDENTITY_AUTH_ISSUER_URL")
	authAudience := cfg.RequireString("IDENTITY_AUTH_AUDIENCE")
	authJWKSURL := cfg.String("IDENTITY_AUTH_JWKS_URL", "")
	zitadelBaseURL := cfg.RequireURL("IDENTITY_ZITADEL_BASE_URL")
	zitadelHostHeader := cfg.RequireString("IDENTITY_ZITADEL_HOST")
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	if err := cfg.Err(); err != nil {
		return err
	}

	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() {
		if err := pg.Close(); err != nil {
			logger.ErrorContext(context.Background(), "identity-service postgres close", "error", err)
		}
	}()
	if err := pg.Ping(); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	zitadelClient, err := zitadel.New(zitadel.Config{
		BaseURL:    zitadelBaseURL,
		HostHeader: zitadelHostHeader,
		AdminToken: zitadelAdminToken,
		HTTPClient: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   5 * time.Second,
		},
	})
	if err != nil {
		return err
	}
	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("identity spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "identity-service spiffe source close", "error", err)
		}
	}()
	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, chCACertPath)
	if err != nil {
		return fmt.Errorf("identity clickhouse tls: %w", err)
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
		return fmt.Errorf("open identity clickhouse: %w", err)
	}
	defer func() { _ = chConn.Close() }()
	chPingCtx, chPingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer chPingCancel()
	if err := chConn.Ping(chPingCtx); err != nil {
		return fmt.Errorf("ping identity clickhouse: %w", err)
	}
	store := identity.SQLStore{DB: pg, CH: chConn}
	identityService := &identity.Service{
		Store:     store,
		Directory: zitadelClient,
		ProjectID: authAudience,
	}
	api.ConfigureAuditSink(governanceAuditURL, spiffeSource)

	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()
	go runDomainLedgerProjectionLoop(bgCtx, logger, store)

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pg.PingContext(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		if err := chConn.Ping(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	api.RegisterZitadelActionRoutes(rootMux, identityService, zitadelActionSigningKey)

	privateMux := http.NewServeMux()
	api.NewAPI(privateMux, api.Config{Version: serviceVersion, ListenAddr: listenAddr, Service: identityService})
	authConfig := auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
		JWKSURL:   authJWKSURL,
	}
	protected := auth.Middleware(authConfig)(privateMux)
	rootMux.Handle("/", protected)

	profilePeerIDs, err := workloadauth.PeerIDsForSource(spiffeSource, workloadauth.ServiceProfile)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, profilePeerIDs...)
	if err != nil {
		return fmt.Errorf("identity spiffe internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	api.NewInternalAPI(internalMux, serviceVersion, "https://"+internalListenAddr, identityService)
	internalAuthenticated := auth.Middleware(authConfig)(internalMux)
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(profilePeerIDs, internalAuthenticated)
	if err != nil {
		return fmt.Errorf("identity internal allowlist: %w", err)
	}

	srv := httpserver.New(listenAddr, otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName))
	internal := httpserver.New(internalListenAddr, otelhttp.NewHandler(limitRequestBodies(internalAllowlist, requestBodyLimit), serviceName+"-internal"))
	internal.TLSConfig = internalTLSConfig
	return httpserver.RunPair(ctx, logger, srv, internal)
}

func runDomainLedgerProjectionLoop(ctx context.Context, logger *slog.Logger, store identity.SQLStore) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			projectCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			projected, err := store.ProjectPendingDomainLedger(projectCtx, 100)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.WarnContext(ctx, "identity domain ledger projection", "error", err)
				continue
			}
			if projected > 0 {
				logger.InfoContext(ctx, "identity domain ledger projected", "count", projected)
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
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/")
	default:
		return false
	}
}
