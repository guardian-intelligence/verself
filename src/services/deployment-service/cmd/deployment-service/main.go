package main

import (
	"context"
	"errors"
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
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName    = deploymentapi.ServiceName
	serviceVersion = "0.1.0"
	githubIssuer   = "https://token.actions.githubusercontent.com"
)

func main() {
	if handled, err := runDeploymentRecoveryCLI(context.Background(), os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
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
	opts, remaining, err := parseDeploymentMigrationOptions(args[1:])
	if err != nil {
		return true, err
	}
	if len(remaining) != 1 || remaining[0] != "up" {
		return true, errors.New("usage: migrate [--resource-graph PATH] [--resource-name NAME] up")
	}
	runtimeCfg, err := loadDeploymentRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return true, err
	}
	return true, migrations.UpDSN(ctx, serviceName, runtimeCfg.PostgresDSN)
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	opts, err := parseDeploymentRunOptions(args)
	if err != nil {
		return err
	}
	runtimeCfg, err := loadDeploymentRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return err
	}

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

	nomadAllocID := strings.TrimSpace(os.Getenv("NOMAD_ALLOC_ID"))
	repoCtx, repoCancel := context.WithTimeout(ctx, runtimeCfg.SourceRepoInitTimeout)
	defer repoCancel()
	if err := deploymentapi.EnsureSourceRepository(repoCtx, deploymentapi.SourceRepositoryConfig{RepoRoot: runtimeCfg.SourceRepoRoot, RepoURL: runtimeCfg.SourceRepoURL}); err != nil {
		return fmt.Errorf("deployment source repository: %w", err)
	}

	pg, err := openPool(ctx, runtimeCfg.PostgresDSN, runtimeCfg.PostgresMaxConns)
	if err != nil {
		return fmt.Errorf("open deployment postgres: %w", err)
	}
	defer pg.Close()

	verifier, err := githubVerifier(ctx, runtimeCfg.PublicBaseURL, runtimeCfg.GitHubRepositories, runtimeCfg.GitHubRefs, runtimeCfg.GitHubWorkflowRefs)
	if err != nil {
		return err
	}
	if verifier == nil {
		return fmt.Errorf("deployment auth requires GitHub OIDC allow-lists")
	}
	spiffeSource, err := workloadauth.Source(ctx, runtimeCfg.SPIFFEEndpointSocket)
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
			Site:              runtimeCfg.Site,
			RepoRoot:          runtimeCfg.SourceRepoRoot,
			ObjectStorageAddr: runtimeCfg.ObjectStorageAddress,
			NomadAddr:         runtimeCfg.NomadAddress,
			NomadJobPaths:     runtimeCfg.NomadJobPaths,
			NomadAllocID:      nomadAllocID,
			RecoverySSHReady:  boolString(runtimeCfg.RecoverySSHReady),
		},
		ObjectStorageHTTPClient: objectStorageHTTPClient,
	}
	if err := svc.Store.Ready(ctx); err != nil {
		return fmt.Errorf("deployment postgres readiness: %w", err)
	}
	interrupted, err := svc.Store.MarkInterrupted(ctx, runtimeCfg.Site, "deployment-service restarted before completion; submit the same sha again to reconcile")
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
		SubmitAdmission: make(chan struct{}, runtimeCfg.AdmissionConcurrency),
	})
	server := httpserver.New(opts.ListenAddr, otelhttp.NewHandler(mux, serviceName))
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
		return nil, fmt.Errorf("DeploymentService.spec.postgres.maxConns must be positive")
	}
	pgMaxConns, err := int32FromInt(maxConns, "DeploymentService.spec.postgres.maxConns")
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
