package ci

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/forge-metal/workload"
	"github.com/google/uuid"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

type Manager struct {
	firecrackerConfig vmorchestrator.Config
	logger            *slog.Logger
	socketPath        string
}

type WarmRequest struct {
	Repo          string
	RepoURL       string
	DefaultBranch string
	RunID         string
}

type ExecRequest struct {
	Repo    string
	RepoURL string
	Ref     string
	RunID   string
}

func NewManager(cfg vmorchestrator.Config, logger *slog.Logger) *Manager {
	return &Manager{
		firecrackerConfig: cfg,
		logger:            logger,
		socketPath:        vmorchestrator.DefaultSocketPath,
	}
}

func (m *Manager) Warm(ctx context.Context, req WarmRequest) (err error) {
	if req.Repo == "" {
		return fmt.Errorf("repo is required")
	}
	if req.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}

	createdAt := time.Now().UTC()
	runID, parentRunID := warmRunIDs(req.RunID)
	logger := m.logger.With("repo", req.Repo, "run_id", runID)

	var (
		manifest                  *Manifest
		toolchain                 *Toolchain
		job                       vmorchestrator.JobConfig
		result                    vmorchestrator.JobResult
		targetDataset             string
		previousDataset           string
		cloneDuration             time.Duration
		filesystemCheckDuration   time.Duration
		snapshotPromotionDuration time.Duration
		previousDestroyDuration   time.Duration
		commitSHA                 string
		filesystemCheckOK         bool
		promoted                  bool
		startedAt                 time.Time
		completedAt               time.Time
	)
	defer func() {
		completedAt = time.Now().UTC()
		if telemetryErr := emitWarmTelemetry(logger, emitWarmTelemetryInput{
			FirecrackerConfig:         m.firecrackerConfig,
			Request:                   req,
			RunID:                     runID,
			ParentRunID:               parentRunID,
			Manifest:                  manifest,
			Toolchain:                 toolchain,
			TargetDataset:             targetDataset,
			PreviousDataset:           previousDataset,
			Job:                       job,
			JobResult:                 result,
			CloneDuration:             cloneDuration,
			FilesystemCheckDuration:   filesystemCheckDuration,
			SnapshotPromotionDuration: snapshotPromotionDuration,
			PreviousDestroyDuration:   previousDestroyDuration,
			FilesystemCheckOK:         filesystemCheckOK,
			Promoted:                  promoted,
			CreatedAt:                 createdAt,
			StartedAt:                 startedAt,
			CompletedAt:               completedAt,
			CommitSHA:                 commitSHA,
			RunErr:                    err,
		}); telemetryErr != nil {
			logger.Warn("emit ci warm telemetry failed", "err", telemetryErr)
		}
	}()

	inspection, err := inspectRepoDefaultBranch(req.RepoURL, req.DefaultBranch)
	if err != nil {
		return err
	}
	defer cleanupInspection(inspection.Path)

	manifest = inspection.Manifest
	toolchain = inspection.Toolchain
	jobEnv, err := buildJobEnv(manifest)
	if err != nil {
		return err
	}
	commitSHA = inspection.CommitSHA
	job = buildGuestJob(uuid.NewString(), manifest, toolchain, true, true, jobEnv)

	client, err := m.newClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	startedAt = time.Now().UTC()
	warmResult, err := client.WarmGolden(ctx, vmorchestrator.WarmGoldenRequest{
		Config:          m.firecrackerConfig,
		Repo:            req.Repo,
		RepoURL:         req.RepoURL,
		DefaultBranch:   req.DefaultBranch,
		Job:             job,
		LockfileRelPath: toolchain.LockfileRelPath,
	})
	if err != nil {
		return err
	}
	result = warmResult.JobResult
	targetDataset = warmResult.TargetDataset
	previousDataset = warmResult.PreviousDataset
	cloneDuration = warmResult.CloneDuration
	filesystemCheckDuration = warmResult.FilesystemCheckDuration
	snapshotPromotionDuration = warmResult.SnapshotPromotionDuration
	previousDestroyDuration = warmResult.PreviousDestroyDuration
	filesystemCheckOK = warmResult.FilesystemCheckOK
	promoted = warmResult.Promoted
	if strings.TrimSpace(warmResult.CommitSHA) != "" {
		commitSHA = warmResult.CommitSHA
	}
	logger.Info("warm run finished", "exit_code", result.ExitCode, "duration_ms", result.Duration.Milliseconds(), "commit_sha", commitSHA)
	if strings.TrimSpace(warmResult.ErrorMessage) != "" {
		logs := strings.TrimSpace(formatJobLogs(result))
		if logs == "" {
			return fmt.Errorf("%s", warmResult.ErrorMessage)
		}
		return fmt.Errorf("%s\n%s", warmResult.ErrorMessage, logs)
	}
	return nil
}

func (m *Manager) Exec(ctx context.Context, req ExecRequest) (*vmorchestrator.JobResult, error) {
	if req.Repo == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if req.RepoURL == "" {
		return nil, fmt.Errorf("repo_url is required")
	}
	if req.Ref == "" {
		return nil, fmt.Errorf("ref is required")
	}
	createdAt := time.Now().UTC()
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = "ci-exec-" + uuid.NewString()
	}

	inspection, err := inspectRepoRef(req.RepoURL, req.Ref)
	if err != nil {
		return nil, err
	}
	defer cleanupInspection(inspection.Path)

	manifest := inspection.Manifest
	toolchain := inspection.Toolchain
	jobEnv, err := buildJobEnv(manifest)
	if err != nil {
		return nil, err
	}
	commitSHA := inspection.CommitSHA
	jobID := uuid.NewString()
	jobTemplate := buildGuestJob(jobID, manifest, toolchain, true, false, jobEnv)
	var cloneDuration time.Duration

	client, err := m.newClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	startedAt := time.Now().UTC()
	status, err := client.ExecRepo(ctx, vmorchestrator.RepoExecRequest{
		Config:          m.firecrackerConfig,
		Repo:            req.Repo,
		RepoURL:         req.RepoURL,
		Ref:             req.Ref,
		JobTemplate:     jobTemplate,
		LockfileRelPath: toolchain.LockfileRelPath,
	})
	completedAt := time.Now().UTC()
	if err != nil {
		return nil, err
	}
	if status.Result == nil {
		if status.ErrorMessage == "" {
			return nil, fmt.Errorf("repo exec %s returned no result", req.Repo)
		}
		return nil, fmt.Errorf("%s", status.ErrorMessage)
	}
	result := *status.Result
	installNeeded := true
	snapshot := ""
	if status.RepoExec != nil {
		installNeeded = status.RepoExec.InstallNeeded
		snapshot = status.RepoExec.GoldenSnapshot
		cloneDuration = status.RepoExec.CloneDuration
		if strings.TrimSpace(status.RepoExec.CommitSHA) != "" {
			commitSHA = status.RepoExec.CommitSHA
		}
	}
	job := jobTemplate
	if !installNeeded {
		job.PrepareCommand = nil
		job.PrepareWorkDir = ""
	}
	var runErr error
	if status.ErrorMessage != "" {
		runErr = fmt.Errorf("%s", status.ErrorMessage)
	}
	prNumber := prNumberFromRef(req.Ref)
	if telemetryErr := emitExecTelemetry(m.logger, emitExecTelemetryInput{
		FirecrackerConfig: m.firecrackerConfig,
		Request:           req,
		RunID:             runID,
		Manifest:          manifest,
		Toolchain:         toolchain,
		InstallNeeded:     installNeeded,
		GoldenSnapshot:    snapshot,
		Job:               job,
		JobResult:         result,
		CloneDuration:     cloneDuration,
		CreatedAt:         createdAt,
		StartedAt:         startedAt,
		CompletedAt:       completedAt,
		CommitSHA:         commitSHA,
		PRNumber:          prNumber,
		RunErr:            runErr,
	}); telemetryErr != nil {
		m.logger.Warn("emit ci exec telemetry failed", "repo", req.Repo, "run_id", runID, "err", telemetryErr)
	}
	if status.ErrorMessage != "" {
		return &result, runErr
	}
	return &result, nil
}

func buildGuestJob(jobID string, manifest *Manifest, toolchain *Toolchain, installNeeded bool, warm bool, env map[string]string) vmorchestrator.JobConfig {
	return workload.BuildGuestJob(jobID, manifest, toolchain, installNeeded, warm, env)
}

func buildJobEnv(manifest *Manifest) (map[string]string, error) {
	return workload.BuildJobEnv(manifest)
}

func resolvedProfile(manifest *Manifest) RuntimeProfile {
	if manifest == nil || manifest.Profile == "" || manifest.Profile == RuntimeProfileAuto {
		return RuntimeProfileNode
	}
	return manifest.Profile
}

type inspectedRepo = workload.Inspection

func (m *Manager) newClient(ctx context.Context) (*vmorchestrator.Client, error) {
	return vmorchestrator.NewClient(ctx, m.socketPath)
}

func inspectRepoDefaultBranch(repoURL, branch string) (*inspectedRepo, error) {
	return workload.InspectRepoDefaultBranch(repoURL, branch)
}

func inspectRepoRef(repoURL, ref string) (*inspectedRepo, error) {
	return workload.InspectRepoRef(repoURL, ref)
}

func inspectRepoPath(repoRoot string) (*inspectedRepo, error) {
	return workload.InspectRepoPath(repoRoot)
}

func cleanupInspection(path string) {
	workload.CleanupInspection(path)
}

func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func formatJobLogs(result vmorchestrator.JobResult) string {
	guestLogs := strings.TrimSpace(result.Logs)
	serialLogs := strings.TrimSpace(result.SerialLogs)
	switch {
	case guestLogs == "" && serialLogs == "":
		return ""
	case guestLogs == "":
		return "[serial diagnostics]\n" + serialLogs
	case serialLogs == "":
		return guestLogs
	default:
		return guestLogs + "\n\n[serial diagnostics]\n" + serialLogs
	}
}
