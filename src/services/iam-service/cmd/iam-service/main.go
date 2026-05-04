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

	"github.com/verself/iam-service/internal/api"
	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/iam-service/internal/spicedb"
	"github.com/verself/iam-service/internal/zitadel"
	"github.com/verself/iam-service/migrations"
	iamschema "github.com/verself/iam-service/schema"
	verselfotel "github.com/verself/observability/otel"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName      = "iam-service"
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
			logger.ErrorContext(context.Background(), "iam-service otel shutdown", "error", err)
		}
	}()

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 8)
	zitadelAdminToken := cfg.RequireCredential("zitadel-admin-token")
	zitadelActionSigningKey := cfg.RequireCredential("zitadel-action-signing-key")
	browserOIDCClientID := cfg.RequireCredential("oidc-client-id")
	browserOIDCClientSecret := cfg.RequireCredential("oidc-client-secret")
	chAddress := cfg.String("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("VERSELF_CLICKHOUSE_USER", "iam_service")
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4248")
	internalListenAddr := cfg.String("VERSELF_INTERNAL_LISTEN_ADDR", "127.0.0.1:4241")
	governanceAuditURL := cfg.String("IAM_GOVERNANCE_AUDIT_URL", "")
	authIssuerURL := cfg.RequireURL("VERSELF_AUTH_ISSUER_URL")
	authAudience := cfg.RequireString("VERSELF_AUTH_AUDIENCE")
	browserAuthPublicBaseURL := cfg.RequireURL("IAM_BROWSER_AUTH_PUBLIC_BASE_URL")
	browserAuthLoginAudiencesRaw := cfg.RequireString("IAM_BROWSER_AUTH_LOGIN_AUDIENCES")
	zitadelBaseURL := cfg.RequireURL("IAM_ZITADEL_BASE_URL")
	zitadelHostHeader := cfg.RequireString("IAM_ZITADEL_HOST")
	spiceDBEndpoint := cfg.RequireString("IAM_SPICEDB_GRPC_ENDPOINT")
	spiceDBPresharedKey := cfg.RequireCredential("spicedb-grpc-preshared-key")
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	if err := cfg.Err(); err != nil {
		return err
	}
	browserAuthLoginAudiences, err := splitRequiredCSV("IAM_BROWSER_AUTH_LOGIN_AUDIENCES", browserAuthLoginAudiencesRaw)
	if err != nil {
		return err
	}

	pg, err := openPool(ctx, pgDSN, pgMaxConns)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()

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
		return fmt.Errorf("iam spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "iam-service spiffe source close", "error", err)
		}
	}()
	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, chCACertPath)
	if err != nil {
		return fmt.Errorf("iam clickhouse tls: %w", err)
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
		return fmt.Errorf("open iam clickhouse: %w", err)
	}
	defer func() { _ = chConn.Close() }()
	chPingCtx, chPingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer chPingCancel()
	if err := chConn.Ping(chPingCtx); err != nil {
		return fmt.Errorf("ping iam clickhouse: %w", err)
	}
	spice, err := spicedb.New(ctx, spicedb.Config{
		Endpoint:     spiceDBEndpoint,
		PresharedKey: spiceDBPresharedKey,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := spice.Close(); err != nil {
			logger.ErrorContext(context.Background(), "iam-service spicedb client close", "error", err)
		}
	}()
	schemaCtx, schemaCancel := context.WithTimeout(ctx, 2*time.Second)
	schemaToken, err := spice.WriteSchema(schemaCtx, iamschema.Verself)
	schemaCancel()
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "iam-service spicedb schema reconciled", "zed_token", schemaToken)
	authzService := authz.New(spice)
	store := identity.SQLStore{PG: pg, CH: chConn}
	identityService := &identity.Service{
		Store:              store,
		Directory:          zitadelClient,
		AuthorizationGraph: authzService,
		ProjectID:          authAudience,
	}
	api.ConfigureAuditSink(governanceAuditURL, spiffeSource)
	browserAuth, err := api.NewBrowserAuth(ctx, api.BrowserAuthConfig{
		PG:             pg,
		Logger:         logger,
		IssuerURL:      authIssuerURL,
		ClientID:       browserOIDCClientID,
		ClientSecret:   browserOIDCClientSecret,
		PublicBaseURL:  browserAuthPublicBaseURL,
		LoginAudiences: browserAuthLoginAudiences,
		HTTPClient: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   5 * time.Second,
		},
	})
	if err != nil {
		return err
	}

	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()
	go runDomainLedgerProjectionLoop(bgCtx, logger, store)

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pg.Ping(r.Context()); err != nil {
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
	api.RegisterBrowserAuthRoutes(rootMux, browserAuth)

	privateMux := http.NewServeMux()
	api.NewAPI(privateMux, api.Config{Version: serviceVersion, ListenAddr: listenAddr, Service: identityService, Authz: authzService})
	authConfig := auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
	}
	protected := auth.Middleware(authConfig)(privateMux)
	rootMux.Handle("/", protected)

	internalPeerIDs, err := workloadauth.PeerIDsForSource(spiffeSource, workloadauth.ServiceProfile, workloadauth.ServiceSourceCodeHosting)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("iam spiffe internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	api.NewInternalAPI(internalMux, serviceVersion, "https://"+internalListenAddr, identityService)
	profileAuthenticated := auth.Middleware(authConfig)(internalMux)
	internalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/v1/subjects/") {
			profileAuthenticated.ServeHTTP(w, r)
			return
		}
		internalMux.ServeHTTP(w, r)
	})
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalHandler)
	if err != nil {
		return fmt.Errorf("iam internal allowlist: %w", err)
	}

	srv := httpserver.New(listenAddr, otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName))
	internal := httpserver.New(internalListenAddr, otelhttp.NewHandler(limitRequestBodies(internalAllowlist, requestBodyLimit), serviceName+"-internal"))
	internal.TLSConfig = internalTLSConfig
	return httpserver.RunPair(ctx, logger, srv, internal)
}

func openPool(ctx context.Context, dsn string, maxConns int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = int32FromInt(maxConns, "IAM_PG_MAX_CONNS")
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
				logger.WarnContext(ctx, "iam domain ledger projection", "error", err)
				continue
			}
			if projected > 0 {
				logger.InfoContext(ctx, "iam domain ledger projected", "count", projected)
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

func splitRequiredCSV(name, raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%s must contain at least one value", name)
	}
	return values, nil
}

func requestMayHaveBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/")
	default:
		return false
	}
}
