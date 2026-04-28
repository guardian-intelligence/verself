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
	"github.com/verself/envconfig"
	"github.com/verself/httpserver"
	verselfotel "github.com/verself/otel"
	"github.com/verself/pgmigrate"
	sourceapi "github.com/verself/source-code-hosting-service/internal/api"
	"github.com/verself/source-code-hosting-service/internal/source"
	"github.com/verself/source-code-hosting-service/migrations"
)

const (
	serviceName      = "source-code-hosting-service"
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
	return true, pgmigrate.RunCLI(ctx, os.Args[2:], pgmigrate.Config{
		Service: serviceName,
		FS:      migrations.Files,
	})
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
			logger.ErrorContext(context.Background(), "source-code-hosting-service otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4261")
	internalListenAddr := cfg.String("VERSELF_INTERNAL_LISTEN_ADDR", "127.0.0.1:4262")
	authIssuerURL := cfg.RequireURL("VERSELF_AUTH_ISSUER_URL")
	authAudience := cfg.RequireString("VERSELF_AUTH_AUDIENCE")
	forgejoBaseURL := cfg.RequireURL("SOURCE_FORGEJO_BASE_URL")
	forgejoOwner := cfg.RequireString("SOURCE_FORGEJO_OWNER")
	forgejoToken := cfg.RequireCredential("forgejo-token")
	sandboxInternalURL := cfg.URL("SOURCE_SANDBOX_INTERNAL_URL", "https://127.0.0.1:4263")
	secretsInternalURL := cfg.URL("SOURCE_SECRETS_INTERNAL_URL", "https://127.0.0.1:4253")
	projectsInternalURL := cfg.URL("SOURCE_PROJECTS_INTERNAL_URL", "https://127.0.0.1:4265")
	identityInternalURL := cfg.URL("SOURCE_IDENTITY_INTERNAL_URL", "https://127.0.0.1:4241")
	publicBaseURL := cfg.RequireURL("SOURCE_PUBLIC_BASE_URL")
	webhookSecret := cfg.CredentialOr("webhook-secret", cfg.String("SOURCE_WEBHOOK_SECRET", ""))
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 8)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	if err := cfg.Err(); err != nil {
		return err
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("source spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "source-code-hosting-service spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceSourceCodeHosting); err != nil {
		return err
	}
	sandboxHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSandboxRental, nil)
	if err != nil {
		return fmt.Errorf("source sandbox-rental mtls: %w", err)
	}
	runnerClient, err := source.NewRunnerRepositoryClient(sandboxInternalURL, sandboxHTTPClient)
	if err != nil {
		return fmt.Errorf("create sandbox-rental internal client: %w", err)
	}
	secretsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("source secrets mtls: %w", err)
	}
	credentialClient, err := source.NewSecretsCredentialClient(secretsInternalURL, secretsHTTPClient)
	if err != nil {
		return fmt.Errorf("create secrets internal client: %w", err)
	}
	projectsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceProjects, nil)
	if err != nil {
		return fmt.Errorf("source projects mtls: %w", err)
	}
	projectsClient, err := source.NewProjectsClient(projectsInternalURL, projectsHTTPClient)
	if err != nil {
		return fmt.Errorf("create projects internal client: %w", err)
	}
	identityHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceIdentity, nil)
	if err != nil {
		return fmt.Errorf("source identity mtls: %w", err)
	}
	identityClient, err := source.NewIdentityClient(identityInternalURL, identityHTTPClient)
	if err != nil {
		return fmt.Errorf("create identity internal client: %w", err)
	}

	pg, err := openPool(ctx, pgDSN, pgMaxConns)
	if err != nil {
		return fmt.Errorf("open source postgres: %w", err)
	}
	defer pg.Close()

	svc := &source.Service{
		Store: source.Store{PG: pg},
		Forgejo: source.ForgejoClient{
			BaseURL: forgejoBaseURL,
			Token:   forgejoToken,
			Owner:   forgejoOwner,
			Client:  &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport), Timeout: 5 * time.Second},
		},
		Runner:        runnerClient,
		Organizations: identityClient,
		Projects:      projectsClient,
		Credentials:   credentialClient,
		CheckoutTTL:   5 * time.Minute,
		ForgejoPrefix: "verself",
	}
	if err := svc.Ready(ctx); err != nil {
		return fmt.Errorf("source readiness: %w", err)
	}

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := svc.Store.Ready(readyCtx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	rootMux.Handle("/webhooks/forgejo", sourceapi.WebhookHandler(svc, webhookSecret))

	privateMux := http.NewServeMux()
	sourceapi.NewAPI(privateMux, serviceVersion, publicBaseURL, sourceapi.Config{
		Service:       svc,
		PublicBaseURL: publicBaseURL,
		WebhookSecret: webhookSecret,
	})
	authenticated := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
	})(privateMux)
	gitHTTP := sourceapi.GitHTTPHandler(svc)
	rootMux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sourceapi.IsGitSmartHTTPRequest(r) {
			gitHTTP.ServeHTTP(w, r)
			return
		}
		authenticated.ServeHTTP(w, r)
	}))

	internalPeerIDs, err := workloadauth.PeerIDsForSource(spiffeSource, workloadauth.ServiceSandboxRental)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("source spiffe internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	sourceapi.NewInternalAPI(internalMux, serviceVersion, "https://"+internalListenAddr, sourceapi.Config{Service: svc})
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalMux)
	if err != nil {
		return fmt.Errorf("source internal allowlist: %w", err)
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
		return strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/") || strings.HasPrefix(r.URL.Path, "/webhooks/")
	default:
		return false
	}
}
