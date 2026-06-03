package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	deploymentapi "github.com/verself/deployment-service/internal/deployment"
	"github.com/verself/deployment-service/migrations"
	verselfotel "github.com/verself/observability/otel"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName    = deploymentapi.ServiceName
	serviceVersion = "0.1.0"
	githubIssuer   = "https://token.actions.githubusercontent.com"
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
			logger.ErrorContext(context.Background(), "deployment-service otel shutdown", "error", err)
		}
	}()
	slog.SetDefault(logger)

	cfg := envconfig.New()
	pgDSN := cfg.RequireString("VERSELF_PG_DSN")
	listenAddr := cfg.String("VERSELF_LISTEN_ADDR", "127.0.0.1:4294")
	site := cfg.RequireString("VERSELF_SITE")
	repoRoot := cfg.RequireString("VERSELF_DEPLOY_REPO_ROOT")
	repoURL := cfg.RequireURL("VERSELF_DEPLOY_REPO_URL")
	repoInitTimeout := cfg.Duration("VERSELF_DEPLOY_REPO_INIT_TIMEOUT", 2*time.Minute)
	nomadAddr := cfg.String("VERSELF_NOMAD_ADDR", "http://127.0.0.1:4646")
	nomadAllocID := cfg.String("NOMAD_ALLOC_ID", "")
	recoverySSHReady := cfg.String("VERSELF_RECOVERY_SSH_READY", "")
	objectStorageAddr := cfg.String("VERSELF_OBJECT_STORAGE_ADDR", workloadauth.InternalURL(workloadauth.ServiceObjectStorageAdmin))
	bazelJobsRaw := cfg.RequireString("VERSELF_DEPLOY_BAZEL_JOBS")
	githubAudience := cfg.URL("VERSELF_DEPLOY_GITHUB_OIDC_AUDIENCE", "")
	githubRepositories := cfg.String("VERSELF_DEPLOY_GITHUB_ALLOWED_REPOSITORIES", "")
	githubRefs := cfg.String("VERSELF_DEPLOY_GITHUB_ALLOWED_REFS", "")
	githubWorkflowRefs := cfg.String("VERSELF_DEPLOY_GITHUB_ALLOWED_WORKFLOW_REFS", "")
	pgMaxConns := cfg.Int("VERSELF_PG_MAX_CONNS", 4)
	admissionConcurrency := cfg.Int("VERSELF_DEPLOY_ADMISSION_CONCURRENCY", 4)
	if err := cfg.Err(); err != nil {
		return err
	}
	bazelJobs, err := parsePositiveInt("VERSELF_DEPLOY_BAZEL_JOBS", bazelJobsRaw)
	if err != nil {
		return err
	}
	if admissionConcurrency <= 0 {
		return fmt.Errorf("VERSELF_DEPLOY_ADMISSION_CONCURRENCY must be positive")
	}
	repoCtx, repoCancel := context.WithTimeout(ctx, repoInitTimeout)
	defer repoCancel()
	if err := deploymentapi.EnsureSourceRepository(repoCtx, deploymentapi.SourceRepositoryConfig{RepoRoot: repoRoot, RepoURL: repoURL}); err != nil {
		return fmt.Errorf("deployment source repository: %w", err)
	}

	pg, err := openPool(ctx, pgDSN, pgMaxConns)
	if err != nil {
		return fmt.Errorf("open deployment postgres: %w", err)
	}
	defer pg.Close()

	verifier, err := githubVerifier(ctx, githubAudience, githubRepositories, githubRefs, githubWorkflowRefs)
	if err != nil {
		return err
	}
	if verifier == nil {
		return fmt.Errorf("deployment auth requires GitHub OIDC allow-lists")
	}
	spiffeSource, err := workloadauth.Source(ctx, "")
	if err != nil {
		return fmt.Errorf("deployment-service spiffe source: %w", err)
	}
	defer func() { _ = spiffeSource.Close() }()
	objectStorageHTTPClient, err := workloadauth.MTLSClientForServiceWithTimeouts(spiffeSource, workloadauth.ServiceObjectStorageAdmin, nil, workloadauth.ServiceClientTimeouts{
		Dial:           500 * time.Millisecond,
		TLSHandshake:   time.Second,
		ResponseHeader: 2 * time.Minute,
		Total:          2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("deployment-service object-storage mtls: %w", err)
	}
	svc := &deploymentapi.Service{
		Store: deploymentapi.Store{PG: pg},
		Config: deploymentapi.Config{
			Site:              site,
			RepoRoot:          repoRoot,
			ObjectStorageAddr: objectStorageAddr,
			NomadAddr:         nomadAddr,
			NomadAllocID:      nomadAllocID,
			RecoverySSHReady:  recoverySSHReady,
			BazelJobs:         bazelJobs,
		},
		ObjectStorageHTTPClient: objectStorageHTTPClient,
	}
	if err := svc.Store.Ready(ctx); err != nil {
		return fmt.Errorf("deployment postgres readiness: %w", err)
	}
	interrupted, err := svc.Store.MarkInterrupted(ctx, site, "deployment-service restarted before completion; submit the same sha again to reconcile")
	if err != nil {
		return fmt.Errorf("deployment interrupted recovery: %w", err)
	}
	if interrupted > 0 {
		logger.WarnContext(ctx, "deployment-service marked interrupted deployments failed", "count", interrupted)
	}

	mux := http.NewServeMux()
	deploymentapi.RegisterRoutes(mux, deploymentapi.API{
		Service:         svc,
		GitHub:          verifier,
		SubmitAdmission: make(chan struct{}, admissionConcurrency),
	})
	server := httpserver.New(listenAddr, otelhttp.NewHandler(mux, serviceName))
	return httpserver.Run(ctx, logger, server)
}

func githubVerifier(ctx context.Context, audience, repositories, refs, workflowRefs string) (*deploymentapi.GitHubOIDCVerifier, error) {
	audience = strings.TrimSpace(audience)
	repositories = strings.TrimSpace(repositories)
	refs = strings.TrimSpace(refs)
	workflowRefs = strings.TrimSpace(workflowRefs)
	if repositories == "" && refs == "" && workflowRefs == "" {
		return nil, nil
	}
	if audience == "" || repositories == "" || refs == "" || workflowRefs == "" {
		return nil, fmt.Errorf("GitHub OIDC deployment auth requires audience, allowed repositories, allowed refs, and allowed workflow refs when enabled")
	}
	return deploymentapi.NewGitHubOIDCVerifier(ctx, githubIssuer, audience, repositories, refs, workflowRefs)
}

func parsePositiveInt(name, raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}

func openPool(ctx context.Context, dsn string, maxConns int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if maxConns <= 0 {
		return nil, fmt.Errorf("VERSELF_PG_MAX_CONNS must be positive")
	}
	pgMaxConns, err := int32FromInt(maxConns, "VERSELF_PG_MAX_CONNS")
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
