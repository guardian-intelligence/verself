package main

import (
	"context"
	"errors"
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
	"go.opentelemetry.io/otel/trace"

	billingclient "github.com/verself/billing-service/client"
	iamclient "github.com/verself/iam-service/client"
	verselfotel "github.com/verself/observability/otel"
	secretsclient "github.com/verself/secrets-service/client"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
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

	sandboxAPIRequestBodyLimit  = 1 << 20
	sandboxInternalWriteTimeout = 60 * time.Second
	schedulerShutdownTimeout    = 10 * time.Second
	siteDomainPath              = "/etc/verself/domain"
	installationIDPath          = "/etc/verself/installation-id"
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
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := verselfotel.Init(signalCtx, verselfotel.Config{ServiceName: "sandbox-rental-service", ServiceVersion: "1.0.0"})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	// PostgreSQL runtime auth is local peer over the Unix socket. Runtime
	// provider credentials are fetched from secrets-service over SPIFFE mTLS.
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4243")
	internalListenAddr := cfg.String("VERSELF_INTERNAL_LISTEN_ADDR", "127.0.0.1:4263")
	chAddress := cfg.String("VERSELF_CLICKHOUSE_ADDRESS", "127.0.0.1:9440")
	chUser := cfg.String("VERSELF_CLICKHOUSE_USER", "sandbox_rental")
	authAudience := cfg.RequireCredential("auth-audience")
	siteDomain := cfg.RequireFile(siteDomainPath)
	publicBaseURL := cfg.String("SANDBOX_PUBLIC_BASE_URL", "https://sandbox.api."+siteDomain)
	authIssuerURL := cfg.URL("VERSELF_AUTH_ISSUER_URL", "https://"+siteDomain)
	installationID := cfg.String("VERSELF_INSTALLATION_ID", cfg.RequireFile(installationIDPath))
	temporalFrontendAddress := cfg.RequireString("SANDBOX_TEMPORAL_FRONTEND_ADDRESS")
	temporalNamespace := cfg.String("SANDBOX_TEMPORAL_NAMESPACE", recurring.DefaultNamespace)
	temporalRecurringTaskQueue := cfg.String("SANDBOX_TEMPORAL_TASK_QUEUE_RECURRING", recurring.DefaultTaskQueue)
	vmOrchestratorSocket := cfg.String("SANDBOX_VM_ORCHESTRATOR_SOCKET", vmorchestrator.DefaultSocketPath)
	githubWebBaseURL := cfg.URL("SANDBOX_GITHUB_WEB_BASE_URL", "https://github.com")
	checkoutBundleStoreDir := cfg.String("SANDBOX_GITHUB_CHECKOUT_BUNDLE_STORE_DIR", "/var/lib/verself/sandbox-rental/github-checkout-bundles")
	runnerBootstrapSecret := cfg.RequireCredential("runner-bootstrap-secret")
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
	if err := validatePublicBaseURL(publicBaseURL); err != nil {
		return fmt.Errorf("SANDBOX_PUBLIC_BASE_URL: %w", err)
	}

	spiffeSource, err := workloadauth.Source(signalCtx, spiffeEndpoint)
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
	pgxConfig.MaxConns = int32FromInt(pgMaxConns, "SANDBOX_PG_MAX_CONNS")
	pgxConfig.MinConns = int32FromInt(pgMinConns, "SANDBOX_PG_MIN_CONNS")
	pgxConfig.MaxConnLifetime = time.Duration(pgConnMaxLifetime) * time.Second
	pgxConfig.MaxConnIdleTime = time.Duration(pgConnMaxIdle) * time.Second
	pgxPool, err := pgxpool.NewWithConfig(signalCtx, pgxConfig)
	if err != nil {
		return fmt.Errorf("open postgres pool: %w", err)
	}
	defer pgxPool.Close()
	pgxPingCtx, cancelPGXPing := context.WithTimeout(signalCtx, 5*time.Second)
	defer cancelPGXPing()
	if err := pgxPool.Ping(pgxPingCtx); err != nil {
		return fmt.Errorf("ping postgres pool: %w", err)
	}

	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(signalCtx, spiffeSource, chCACertPath)
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
	chPingCtx, cancelCHPing := context.WithTimeout(signalCtx, 5*time.Second)
	defer cancelCHPing()
	var chProbe uint8
	if err := chConn.QueryRow(chPingCtx, "SELECT 1").Scan(&chProbe); err != nil {
		return fmt.Errorf("probe clickhouse: %w", err)
	}

	// --- vm-orchestrator client ---

	orchestrator, err := vmorchestrator.NewClient(signalCtx, vmOrchestratorSocket)
	if err != nil {
		return fmt.Errorf("connect vm-orchestrator: %w", err)
	}
	defer func() { _ = orchestrator.Close() }()

	// Probe the host pool ceilings once at startup. The pool ceiling is the
	// default VMResourceBounds applied to intake when an org has no explicit
	// row, clamped to what the host can actually
	// schedule. Org-specific overrides live in the vm_resource_bounds table.
	capacityCtx, cancelCapacity := context.WithTimeout(signalCtx, 5*time.Second)
	defer cancelCapacity()
	capacity, err := orchestrator.GetCapacity(capacityCtx)
	if err != nil {
		return fmt.Errorf("query vm-orchestrator capacity: %w", err)
	}
	hostBounds := jobs.DefaultBounds
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
	billingClient, err := billingclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceBilling), billingclient.WithHTTPClient(billingHTTPClient))
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}
	iamHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceIAM, nil)
	if err != nil {
		return fmt.Errorf("sandbox iam mtls: %w", err)
	}
	iamClient, err := iamclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceIAM), iamclient.WithHTTPClient(iamHTTPClient))
	if err != nil {
		return fmt.Errorf("create iam client: %w", err)
	}
	secretsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceSecrets, nil)
	if err != nil {
		return fmt.Errorf("sandbox secrets mtls: %w", err)
	}
	secretsClient, err := secretsclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceSecrets), secretsclient.WithHTTPClient(secretsHTTPClient))
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

	// --- job service ---

	jobService := &jobs.Service{
		PGX:                    pgxPool,
		CH:                     chConn,
		CHDatabase:             "verself",
		Orchestrator:           orchestrator,
		Billing:                billingClient,
		Secrets:                secretsClient,
		Bounds:                 hostBounds,
		Logger:                 logger,
		WorkloadTimeout:        time.Duration(workloadTimeout) * time.Second,
		CheckoutBundleStoreDir: checkoutBundleStoreDir,
		RunnerBootstrapSecret:  runnerBootstrapSecret,
		GitHubWebBaseURL:       githubWebBaseURL,
	}
	sourceDispatcher, err := sourceworkflow.NewDispatcher(workloadauth.InternalURL(workloadauth.ServiceSourceCodeHosting), sourceHTTPClient)
	if err != nil {
		return fmt.Errorf("create source workflow dispatcher: %w", err)
	}
	sandboxapi.ConfigureAPIActivitySink(workloadauth.InternalURL(workloadauth.ServiceGovernance), spiffeSource)
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

	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	if err := schedulerRuntime.Start(workerCtx); err != nil {
		return fmt.Errorf("start scheduler runtime: %w", err)
	}

	// --- Huma API ---

	rootMux := http.NewServeMux()
	registerStatusRoutes(rootMux)
	privateMux := http.NewServeMux()
	sandboxapi.NewAPI(privateMux, "1.0.0", listenAddr, jobService, recurringService, sandboxapi.PublicAPIConfig{
		PublicBaseURL:  publicBaseURL,
		Authorizer:     iamclient.NewAuthorizer(iamClient),
		InstallationID: installationID,
	})
	sandboxapi.RegisterPublicRoutes(rootMux, jobService, installationID)

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

	internalPeerIDs, err := workloadauth.PeerIDsForSource(spiffeSource, workloadauth.ServiceSourceCodeHosting, workloadauth.ServiceGitHubIntegration)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("sandbox-rental spiffe internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	registerStatusRoutes(internalMux)
	sandboxapi.NewInternalAPI(internalMux, "1.0.0", "https://"+internalListenAddr, jobService)
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalMux)
	if err != nil {
		return fmt.Errorf("sandbox-rental internal allowlist: %w", err)
	}
	internalHandler := limitInternalAPIRequestBodies(internalAllowlist, sandboxAPIRequestBodyLimit)
	internalSrv := httpserver.NewWithTimeouts(internalListenAddr, otelhttp.NewHandler(internalHandler, "sandbox-rental-service-internal"), httpserver.Timeouts{
		Write: sandboxInternalWriteTimeout,
	})
	internalSrv.TLSConfig = internalTLSConfig

	reconcileCtx, stopReconcile := context.WithCancel(context.Background())
	defer stopReconcile()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-reconcileCtx.Done():
				return
			case <-ticker.C:
				runCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := jobService.Reconcile(runCtx); err != nil {
					logger.ErrorContext(runCtx, "sandbox-rental: reconcile", "error", err)
				}
				cancel()
			}
		}
	}()

	serverCtx, stopServers := context.WithCancel(context.Background())
	defer stopServers()
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- httpserver.RunPair(serverCtx, logger, srv, internalSrv)
	}()

	select {
	case err := <-serverErrCh:
		stopReconcile()
		cancelWorkers()
		stopCtx, cancel := context.WithTimeout(context.Background(), schedulerShutdownTimeout)
		defer cancel()
		stopErr := schedulerRuntime.StopAndCancel(stopCtx)
		if err != nil {
			return err
		}
		return stopErr
	case <-signalCtx.Done():
	}

	stop()
	stopReconcile()
	stopServers()
	waitForHTTPShutdown(logger, serverErrCh)

	stopCtx, span := otel.Tracer("sandbox-rental-service").Start(context.Background(), "sandbox.shutdown")
	span.SetAttributes(attribute.String("sandbox.scheduler_shutdown.timeout", schedulerShutdownTimeout.String()))
	stopCtx, cancelScheduler := context.WithTimeout(stopCtx, schedulerShutdownTimeout)
	defer cancelScheduler()
	err = schedulerRuntime.StopAndCancel(stopCtx)
	if err != nil {
		span.RecordError(err)
		cancelWorkers()
		span.End()
		return fmt.Errorf("stop scheduler runtime: %w", err)
	}
	span.SetAttributes(attribute.String("sandbox.scheduler_shutdown.result", "succeeded"))
	span.End()
	logger.InfoContext(context.Background(), "sandbox-rental: shutdown completed")
	return nil
}

func registerStatusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
}

func waitForHTTPShutdown(logger *slog.Logger, serverErrCh <-chan error) {
	select {
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(context.Background(), "sandbox-rental: http shutdown", "error", err)
		}
	case <-time.After(httpserver.ShutdownTimeout + time.Second):
		logger.WarnContext(context.Background(), "sandbox-rental: http shutdown wait timed out")
	}
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
