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

	"github.com/verself/deployment-service/deployengine"
	deploymentapi "github.com/verself/deployment-service/internal/deployment"
	"github.com/verself/deployment-service/migrations"
	distributioninternalclient "github.com/verself/distribution-service/internalclient"
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
	r2ControlPlaneAddr := cfg.String("VERSELF_R2_CONTROL_PLANE_ADDR", workloadauth.InternalURL(workloadauth.ServiceCloudflareR2))
	distributionAddr := cfg.String("VERSELF_DISTRIBUTION_ADDR", workloadauth.InternalURL(workloadauth.ServiceDistribution))
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
	deploymentID, err := workloadauth.CurrentIDForService(spiffeSource, workloadauth.ServiceDeployment)
	if err != nil {
		return err
	}
	r2HTTPClient, err := workloadauth.MTLSClientForServiceWithTimeouts(spiffeSource, workloadauth.ServiceCloudflareR2, nil, workloadauth.ServiceClientTimeouts{
		Dial:           500 * time.Millisecond,
		TLSHandshake:   time.Second,
		ResponseHeader: 2 * time.Minute,
		Total:          2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("deployment-service R2 control-plane mtls: %w", err)
	}
	distributionHTTPClient, err := workloadauth.MTLSClientForServiceWithTimeouts(spiffeSource, workloadauth.ServiceDistribution, nil, workloadauth.ServiceClientTimeouts{
		Dial:           500 * time.Millisecond,
		TLSHandshake:   time.Second,
		ResponseHeader: 2 * time.Minute,
		Total:          2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("deployment-service distribution mtls: %w", err)
	}
	distributionClient, err := distributioninternalclient.NewClient(distributionAddr, distributioninternalclient.WithHTTPClient(distributionHTTPClient))
	if err != nil {
		return err
	}
	svc := &deploymentapi.Service{
		Store: deploymentapi.Store{PG: pg},
		Config: deploymentapi.Config{
			Site:               site,
			RepoRoot:           repoRoot,
			R2ControlPlaneAddr: r2ControlPlaneAddr,
			NomadAddr:          nomadAddr,
			NomadAllocID:       nomadAllocID,
			RecoverySSHReady:   recoverySSHReady,
			BazelJobs:          bazelJobs,
			BuilderID:          deploymentID.String(),
		},
		R2ControlPlaneHTTPClient: r2HTTPClient,
		DistributionAdmitter: deploymentDistributionAdmitter{
			client: distributionClient,
		},
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

type deploymentDistributionAdmitter struct {
	client *distributioninternalclient.Client
}

func (a deploymentDistributionAdmitter) AdmitDeploymentImage(ctx context.Context, req deployengine.DeploymentImageAdmissionRequest) (deployengine.DeploymentImageAdmissionResult, error) {
	if a.client == nil {
		return deployengine.DeploymentImageAdmissionResult{}, fmt.Errorf("distribution client is required")
	}
	evidence := make([]distributioninternalclient.Evidence, 0, len(req.Evidence))
	for _, item := range req.Evidence {
		evidence = append(evidence, distributioninternalclient.Evidence{
			EvidenceKind:      item.EvidenceKind,
			PredicateType:     item.PredicateType,
			SubjectDigest:     item.SubjectDigest,
			DocumentDigest:    item.DocumentDigest,
			OCIReferrerDigest: item.OCIReferrerDigest,
		})
	}
	resp, err := a.client.AdmitArtifact(ctx, distributioninternalclient.AdmitArtifactRequest{
		PackageName:       req.Output,
		PackageVersion:    req.SHA,
		ChannelName:       "deployment",
		PlatformOS:        "linux",
		PlatformArch:      "amd64",
		Flavor:            req.Site,
		OriginRegistryURL: req.OriginRegistryURL,
		PublicRegistryURL: req.PublicRegistryURL,
		OCIRepository:     req.Repository,
		OCIDigest:         req.Digest,
		OCIMediaType:      req.MediaType,
		OCISizeBytes:      req.SizeBytes,
		BuilderID:         req.BuilderID,
		SignerIdentity:    req.BuilderID,
		SourceRepository:  req.SourceRepository,
		SourceCommit:      req.SHA,
		SourceRef:         req.SourceRef,
		PolicyRef:         req.PolicyRef,
		Evidence:          evidence,
		SubmittedBy:       "deployment-service",
	}, "deployment-image-"+req.Site+"-"+req.Output+"-"+strings.TrimPrefix(req.Digest, "sha256:"))
	if err != nil {
		return deployengine.DeploymentImageAdmissionResult{}, err
	}
	if resp.Problem != nil {
		return deployengine.DeploymentImageAdmissionResult{}, fmt.Errorf("distribution admission rejected: %s", firstNonEmptyString(resp.Problem.Detail, resp.Problem.Title, resp.Problem.Code))
	}
	if resp.Result == nil {
		return deployengine.DeploymentImageAdmissionResult{}, fmt.Errorf("distribution admission response missing artifact")
	}
	return deployengine.DeploymentImageAdmissionResult{PublicOCIReference: resp.Result.Artifact.PublicOCIReference}, nil
}

func firstNonEmptyString(values ...*string) string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return strings.TrimSpace(*value)
		}
	}
	return "unknown distribution problem"
}
