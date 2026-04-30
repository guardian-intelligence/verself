package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/apiwire"
	auth "github.com/verself/auth-middleware"
	workloadauth "github.com/verself/auth-middleware/workload"
	billingclient "github.com/verself/billing-service/client"
	"github.com/verself/envconfig"
	"github.com/verself/httpserver"
	verselfotel "github.com/verself/otel"
	secretsclient "github.com/verself/secrets-service/client"
	vmorchestrator "github.com/verself/vm-orchestrator"

	sandboxapi "github.com/verself/sandbox-rental-service/internal/api"
	"github.com/verself/sandbox-rental-service/internal/jobs"
	"github.com/verself/sandbox-rental-service/internal/recurring"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
	"github.com/verself/sandbox-rental-service/internal/sourceworkflow"
	"github.com/verself/sandbox-rental-service/migrations"
	"github.com/verself/temporal-platform/sdkclient"
)

const (
	correlationHeader = "X-Verself-Correlation-Id"
	correlationCookie = "verself_correlation_id"

	sandboxAPIRequestBodyLimit = 1 << 20
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
	return true, migrations.RunCLI(ctx, os.Args[2:], "sandbox-rental-service")
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: "sandbox-rental-service", ServiceVersion: "1.0.0"})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
	slog.SetDefault(logger)

	cfg := envconfig.New()
	// PostgreSQL runtime auth is local peer over the Unix socket. Runtime
	// provider credentials are fetched from secrets-service over SPIFFE mTLS.
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4243")
	internalListenAddr := cfg.String("VERSELF_INTERNAL_LISTEN_ADDR", "127.0.0.1:4263")
	chAddress := cfg.String("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("VERSELF_CLICKHOUSE_USER", "sandbox_rental")
	billingURL := cfg.URL("SANDBOX_BILLING_URL", "http://127.0.0.1:4242")
	governanceAuditURL := cfg.String("SANDBOX_GOVERNANCE_AUDIT_URL", "")
	secretsURL := cfg.URL("SANDBOX_SECRETS_URL", "https://127.0.0.1:4253")
	sourceInternalURL := cfg.URL("SANDBOX_SOURCE_INTERNAL_URL", "https://127.0.0.1:4262")
	billingReturnOriginsRaw := cfg.RequireString("SANDBOX_BILLING_RETURN_ORIGINS")
	publicBaseURL := cfg.RequireString("SANDBOX_PUBLIC_BASE_URL")
	authIssuerURL := cfg.RequireURL("VERSELF_AUTH_ISSUER_URL")
	authAudience := cfg.RequireString("VERSELF_AUTH_AUDIENCE")
	temporalFrontendAddress := cfg.String("SANDBOX_TEMPORAL_FRONTEND_ADDRESS", sdkclient.DefaultFrontendAddress)
	temporalNamespace := cfg.String("SANDBOX_TEMPORAL_NAMESPACE", recurring.DefaultNamespace)
	temporalRecurringTaskQueue := cfg.String("SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING", recurring.DefaultTaskQueue)
	vmOrchestratorSocket := cfg.String("SANDBOX_VM_ORCHESTRATOR_SOCKET", vmorchestrator.DefaultSocketPath)
	githubAppEnabled := cfg.Bool("SANDBOX_GITHUB_APP_ENABLED", false)
	githubAppID := cfg.Int64("SANDBOX_GITHUB_APP_ID", 0)
	githubAppSlug := cfg.String("SANDBOX_GITHUB_APP_SLUG", "")
	githubAppClientID := cfg.String("SANDBOX_GITHUB_APP_CLIENT_ID", "")
	githubAPIBaseURL := cfg.URL("SANDBOX_GITHUB_API_BASE_URL", "https://api.github.com")
	githubWebBaseURL := cfg.URL("SANDBOX_GITHUB_WEB_BASE_URL", "https://github.com")
	githubRunnerGroupID := cfg.Int64("SANDBOX_GITHUB_RUNNER_GROUP_ID", 1)
	checkoutCacheDir := cfg.String("SANDBOX_GITHUB_CHECKOUT_CACHE_DIR", "/var/lib/verself/sandbox-rental/github-checkout")
	forgejoAPIBaseURL := cfg.URL("SANDBOX_FORGEJO_API_BASE_URL", "")
	forgejoRunnerBaseURL := cfg.String("SANDBOX_FORGEJO_RUNNER_BASE_URL", "")
	forgejoWebhookBaseURL := cfg.String("SANDBOX_FORGEJO_WEBHOOK_BASE_URL", publicBaseURL)
	forgejoToken := cfg.CredentialOr("forgejo-token", "")
	forgejoWebhookSecret := cfg.CredentialOr("forgejo-webhook-secret", "")
	forgejoBootstrapSecret := cfg.CredentialOr("forgejo-bootstrap-secret", "")
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 16)
	pgMinConns := cfg.Int("VERSELF_PG_MIN_CONNS", 1)
	pgConnMaxLifetime := cfg.Int("VERSELF_PG_CONN_MAX_LIFETIME_SECONDS", 1800)
	pgConnMaxIdle := cfg.Int("VERSELF_PG_CONN_MAX_IDLE_SECONDS", 300)
	workloadTimeout := cfg.Int("SANDBOX_WORKLOAD_TIMEOUT_SECONDS", 7200)
	executionMaxWorkers := cfg.Int("SANDBOX_EXECUTION_MAX_WORKERS", scheduler.DefaultExecutionMaxWorkers)
	spiffeEndpoint := cfg.String(workloadauth.EndpointSocketEnv, "")
	chCACertPath := cfg.RequireCredentialPath("clickhouse-ca-cert")
	if err := cfg.Err(); err != nil {
		return err
	}
	billingReturnOrigins, err := sandboxapi.ParseBillingReturnOrigins(billingReturnOriginsRaw)
	if err != nil {
		return fmt.Errorf("SANDBOX_BILLING_RETURN_ORIGINS: %w", err)
	}
	if err := validatePublicBaseURL(publicBaseURL); err != nil {
		return fmt.Errorf("SANDBOX_PUBLIC_BASE_URL: %w", err)
	}

	spiffeSource, err := workloadauth.Source(ctx, spiffeEndpoint)
	if err != nil {
		return fmt.Errorf("spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "sandbox-rental: spiffe source close", "error", err)
		}
	}()
	if _, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceSandboxRental); err != nil {
		return err
	}
	// --- open connections ---

	pgxConfig, err := pgxpool.ParseConfig(pgDSN)
	if err != nil {
		return fmt.Errorf("parse postgres dsn: %w", err)
	}
	pgxConfig.MaxConns = int32(pgMaxConns)
	pgxConfig.MinConns = int32(pgMinConns)
	pgxConfig.MaxConnLifetime = time.Duration(pgConnMaxLifetime) * time.Second
	pgxConfig.MaxConnIdleTime = time.Duration(pgConnMaxIdle) * time.Second
	pgxPool, err := pgxpool.NewWithConfig(ctx, pgxConfig)
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pgxPool.Close()
	pgxPingCtx, cancelPGXPing := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPGXPing()
	if err := pgxPool.Ping(pgxPingCtx); err != nil {
		return fmt.Errorf("ping postgres pool: %w", err)
	}

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, chCACertPath)
	if err != nil {
		return fmt.Errorf("sandbox-rental clickhouse tls: %w", err)
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
	chPingCtx, cancelCHPing := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCHPing()
	var chProbe uint8
	if err := chConn.QueryRow(chPingCtx, "SELECT 1").Scan(&chProbe); err != nil {
		return fmt.Errorf("probe clickhouse: %w", err)
	}

	// --- vm-orchestrator client ---

	orchestrator, err := vmorchestrator.NewClient(ctx, vmOrchestratorSocket)
	if err != nil {
		return fmt.Errorf("connect vm-orchestrator: %w", err)
	}
	defer orchestrator.Close()

	// Probe the host pool ceilings once at startup. The pool ceiling is the
	// default VMResourceBounds applied to intake when an org has no explicit
	// row — apiwire.DefaultBounds clamped to what the host can actually
	// schedule. Org-specific overrides live in the vm_resource_bounds table.
	capacityCtx, cancelCapacity := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCapacity()
	capacity, err := orchestrator.GetCapacity(capacityCtx)
	if err != nil {
		return fmt.Errorf("query vm-orchestrator capacity: %w", err)
	}
	hostBounds := apiwire.DefaultBounds
	if capacity.MaxVCPUsPerLease > 0 {
		hostBounds.MaxVCPUs = capacity.MaxVCPUsPerLease
	}
	if capacity.MaxMemoryMiBPerLease > 0 {
		hostBounds.MaxMemoryMiB = capacity.MaxMemoryMiBPerLease
	}
	if capacity.MaxRootDiskGiBPerLease > 0 {
		hostBounds.MaxRootDiskGiB = capacity.MaxRootDiskGiBPerLease
	}

	// --- peer clients ---

	billingHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceBilling, nil)
	if err != nil {
		return fmt.Errorf("sandbox billing mtls: %w", err)
	}
	billingClient, err := billingclient.NewClientWithResponses(billingURL, billingclient.WithHTTPClient(billingHTTPClient))
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}
	secretsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("sandbox secrets mtls: %w", err)
	}
	secretsClient, err := secretsclient.NewClientWithResponses(secretsURL, secretsclient.WithHTTPClient(secretsHTTPClient))
	if err != nil {
		return fmt.Errorf("create secrets client: %w", err)
	}
	sourceHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSourceCodeHosting, nil)
	if err != nil {
		return fmt.Errorf("sandbox source-code-hosting mtls: %w", err)
	}
	temporalClient, err := sdkclient.NewWorkflowClient(sdkclient.Config{
		HostPort: temporalFrontendAddress,
	}, temporalNamespace, spiffeSource, "sandbox-rental-service-temporal-sdk")
	if err != nil {
		return fmt.Errorf("sandbox-rental temporal client: %w", err)
	}
	defer temporalClient.Close()

	var githubAppPrivateKey string
	var githubAppWebhookSecret string
	var githubAppClientSecret string
	if githubAppEnabled || githubAppID > 0 || githubAppSlug != "" || githubAppClientID != "" {
		githubSecrets, err := readRuntimeSecrets(ctx, secretsClient,
			secretsclient.SandboxGitHubPrivateKeyName,
			secretsclient.SandboxGitHubWebhookSecretName,
			secretsclient.SandboxGitHubClientSecretName,
		)
		if err != nil {
			return fmt.Errorf("sandbox-rental github provider secret: %w", err)
		}
		githubAppPrivateKey = requireSecretField(githubSecrets, secretsclient.SandboxGitHubPrivateKeyName, "sandbox-rental github provider secret")
		githubAppWebhookSecret = requireSecretField(githubSecrets, secretsclient.SandboxGitHubWebhookSecretName, "sandbox-rental github provider secret")
		githubAppClientSecret = requireSecretField(githubSecrets, secretsclient.SandboxGitHubClientSecretName, "sandbox-rental github provider secret")
	}

	// --- job service ---

	jobService := &jobs.Service{
		PGX:              pgxPool,
		CH:               chConn,
		CHDatabase:       "verself",
		Orchestrator:     orchestrator,
		Billing:          billingClient,
		Bounds:           hostBounds,
		Logger:           logger,
		WorkloadTimeout:  time.Duration(workloadTimeout) * time.Second,
		CheckoutCacheDir: checkoutCacheDir,
	}
	githubRunner, err := jobs.NewGitHubRunner(jobService, jobs.GitHubRunnerConfig{
		AppID:         githubAppID,
		AppSlug:       githubAppSlug,
		ClientID:      githubAppClientID,
		ClientSecret:  githubAppClientSecret,
		PrivateKeyPEM: githubAppPrivateKey,
		WebhookSecret: githubAppWebhookSecret,
		APIBaseURL:    githubAPIBaseURL,
		WebBaseURL:    githubWebBaseURL,
		RunnerGroupID: githubRunnerGroupID,
	}, &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   10 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("create github runner adapter: %w", err)
	}
	jobService.GitHubRunner = githubRunner
	forgejoRunner, err := jobs.NewForgejoRunner(jobService, jobs.ForgejoRunnerConfig{
		APIBaseURL:      forgejoAPIBaseURL,
		RunnerBaseURL:   forgejoRunnerBaseURL,
		WebhookBaseURL:  forgejoWebhookBaseURL,
		Token:           forgejoToken,
		WebhookSecret:   forgejoWebhookSecret,
		BootstrapSecret: forgejoBootstrapSecret,
	}, &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   10 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("create forgejo runner adapter: %w", err)
	}
	jobService.ForgejoRunner = forgejoRunner
	sourceDispatcher, err := sourceworkflow.NewDispatcher(sourceInternalURL, sourceHTTPClient)
	if err != nil {
		return fmt.Errorf("create source workflow dispatcher: %w", err)
	}
	sandboxapi.ConfigureAuditSink(governanceAuditURL, spiffeSource)
	recurringService, err := recurring.NewService(recurring.Config{
		PGX:            pgxPool,
		TemporalClient: temporalClient,
		Namespace:      temporalNamespace,
		TaskQueue:      temporalRecurringTaskQueue,
		Logger:         logger,
		Dispatcher:     sourceDispatcher,
	})
	if err != nil {
		return fmt.Errorf("create recurring service: %w", err)
	}

	schedulerRuntime, err := scheduler.NewRuntime(pgxPool, scheduler.Config{
		Logger:              logger,
		ExecutionMaxWorkers: executionMaxWorkers,
		RegisterWorkers: func(workers *river.Workers) error {
			return jobs.RegisterSchedulerWorkers(workers, jobService)
		},
	})
	if err != nil {
		return fmt.Errorf("create scheduler runtime: %w", err)
	}
	jobService.Scheduler = schedulerRuntime

	if err := schedulerRuntime.Start(ctx); err != nil {
		return fmt.Errorf("start scheduler runtime: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := schedulerRuntime.Stop(stopCtx); err != nil {
			logger.ErrorContext(context.Background(), "sandbox-rental: stop scheduler runtime", "error", err)
		}
	}()

	// --- Huma API ---

	rootMux := http.NewServeMux()
	privateMux := http.NewServeMux()
	sandboxapi.NewAPI(privateMux, "1.0.0", listenAddr, jobService, recurringService, billingClient, sandboxapi.PublicAPIConfig{
		BillingReturnOrigins: billingReturnOrigins,
		PublicBaseURL:        publicBaseURL,
	})
	sandboxapi.RegisterPublicRoutes(rootMux, jobService)

	authHandler := auth.Middleware(auth.Config{
		IssuerURL: authIssuerURL,
		Audience:  authAudience,
	})(privateMux)
	correlationForwarder := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := strings.TrimSpace(r.Header.Get(correlationHeader))
		if correlationID == "" {
			if cookie, err := r.Cookie(correlationCookie); err == nil {
				correlationID = strings.TrimSpace(cookie.Value)
			}
		}
		if correlationID != "" {
			ctx := jobs.WithCorrelationID(r.Context(), correlationID)
			// Mirror correlation_id into baggage so child spans pick it up
			// via verselfotel.baggageSpanProcessor. The otelhttp server span was
			// already started before this middleware ran, so set it directly
			// on the live span too. NewMemberRaw + SetMember are infallible
			// for our constant key and arbitrary value.
			attrKey := verselfotel.BaggageAttributePrefix + "correlation_id"
			member, _ := baggage.NewMemberRaw(attrKey, correlationID)
			bag, _ := baggage.FromContext(ctx).SetMember(member)
			ctx = baggage.ContextWithBaggage(ctx, bag)
			trace.SpanFromContext(ctx).SetAttributes(attribute.String(attrKey, correlationID))
			r = r.WithContext(ctx)
		}
		authHandler.ServeHTTP(w, r)
	})
	rootMux.Handle("/", correlationForwarder)
	rootHandler := limitPublicAPIRequestBodies(rootMux, sandboxAPIRequestBodyLimit)
	srv := httpserver.New(listenAddr, otelhttp.NewHandler(rootHandler, "sandbox-rental-service"))

	internalPeerIDs, err := workloadauth.PeerIDsForSource(spiffeSource, workloadauth.ServiceSourceCodeHosting)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("sandbox-rental spiffe internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	sandboxapi.NewInternalAPI(internalMux, "1.0.0", "https://"+internalListenAddr, jobService)
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalMux)
	if err != nil {
		return fmt.Errorf("sandbox-rental internal allowlist: %w", err)
	}
	internalHandler := limitInternalAPIRequestBodies(internalAllowlist, sandboxAPIRequestBodyLimit)
	internalSrv := httpserver.New(internalListenAddr, otelhttp.NewHandler(internalHandler, "sandbox-rental-service-internal"))
	internalSrv.TLSConfig = internalTLSConfig

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reconcileCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := jobService.Reconcile(reconcileCtx); err != nil {
					logger.ErrorContext(reconcileCtx, "sandbox-rental: reconcile", "error", err)
				}
				cancel()
			}
		}
	}()

	return httpserver.RunPair(ctx, logger, srv, internalSrv)
}

func requireSecretField(values map[string]string, field string, label string) string {
	value := strings.TrimSpace(values[field])
	if value == "" {
		fmt.Fprintf(os.Stderr, "%s missing required field %s\n", label, field)
		os.Exit(1)
	}
	return value
}

func readRuntimeSecrets(ctx context.Context, client *secretsclient.ClientWithResponses, secretNames ...string) (map[string]string, error) {
	ctx, span := otel.Tracer("runtime-secrets").Start(ctx, "secrets.runtime.resolve")
	defer span.End()
	span.SetAttributes(attribute.Int("verself.secret_count", len(secretNames)))

	if client == nil {
		err := fmt.Errorf("runtime secrets client is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	secretCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values := make(map[string]string, len(secretNames))
	for _, secretName := range secretNames {
		resp, err := client.ReadSecretWithResponse(secretCtx, secretName)
		if err != nil {
			err = fmt.Errorf("read runtime secret %s: %w", secretName, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		if resp.JSON200 == nil {
			err := fmt.Errorf("read runtime secret %s: unexpected status %d: %s", secretName, resp.StatusCode(), strings.TrimSpace(string(resp.Body)))
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		values[secretName] = resp.JSON200.Value
	}
	return values, nil
}

func validatePublicBaseURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if !parsed.IsAbs() || parsed.Hostname() == "" {
		return fmt.Errorf("must be an absolute URL with a host")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("scheme %q is not supported", parsed.Scheme)
	}
	if strings.Trim(parsed.EscapedPath(), "/") != "" {
		return fmt.Errorf("must not include a path")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("must not include query string or fragment")
	}
	return nil
}

func limitPublicAPIRequestBodies(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicAPIRequestWithBody(r) {
			if r.ContentLength > maxBytes {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicAPIRequestWithBody(r *http.Request) bool {
	if r == nil || !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

func limitInternalAPIRequestBodies(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isInternalAPIRequestWithBody(r) {
			if r.ContentLength > maxBytes {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func isInternalAPIRequestWithBody(r *http.Request) bool {
	if r == nil || !strings.HasPrefix(r.URL.Path, "/internal/") {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}
