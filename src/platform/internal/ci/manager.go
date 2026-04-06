package ci

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	fastsandbox "github.com/forge-metal/fast-sandbox"
)

const (
	lockfileHashRelPath = ".forge-metal/lockfile.sha256"
	repoGoldensDataset  = "repo-goldens"
)

var repoGoldenStateRoot = "/var/lib/ci/repo-goldens"

type Manager struct {
	firecrackerConfig fastsandbox.Config
	logger            *slog.Logger
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

func NewManager(cfg fastsandbox.Config, logger *slog.Logger) *Manager {
	return &Manager{
		firecrackerConfig: cfg,
		logger:            logger,
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
	repoKey := sanitizeRepoKey(req.Repo)
	targetDataset := m.nextRepoGoldenDataset(repoKey)
	previousDataset, err := m.activeRepoGoldenDataset(repoKey)
	if err != nil {
		return err
	}
	logger := m.logger.With("repo", req.Repo, "run_id", runID, "dataset", targetDataset)

	var (
		manifest                  *Manifest
		toolchain                 *Toolchain
		job                       fastsandbox.JobConfig
		result                    fastsandbox.JobResult
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

	if err := ensureDataset(ctx, m.repoGoldensRootDataset()); err != nil {
		return err
	}
	cloneStart := time.Now()
	if err := zfsClone(ctx, m.baseGoldenSnapshot(), targetDataset); err != nil {
		return err
	}
	cloneDuration = time.Since(cloneStart)
	logger.Info("repo golden clone created", "clone_ms", cloneDuration.Milliseconds(), "base_snapshot", m.baseGoldenSnapshot(), "previous_dataset", previousDataset)

	// Track mount state so the cleanup defer can unmount before destroying.
	// ZFS destroy -f only force-unmounts ZFS-native mounts, not ext4-over-zvol
	// VFS mounts, so we must unmount explicitly or the dataset leaks.
	cleanupTargetDataset := true
	var cleanupMountDir string
	defer func() {
		if !cleanupTargetDataset {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), zfsTimeout)
		defer cleanupCancel()
		if cleanupMountDir != "" {
			if umountErr := unmountDataset(cleanupCtx, cleanupMountDir); umountErr != nil {
				logger.Warn("cleanup unmount failed", "mount", cleanupMountDir, "err", umountErr)
			}
			cleanupMountDir = ""
		}
		cleanupStart := time.Now()
		if destroyErr := destroyDatasetRecursive(cleanupCtx, targetDataset); destroyErr != nil {
			logger.Warn("failed to destroy warm target dataset", "err", destroyErr)
			return
		}
		logger.Warn("destroyed warm target dataset after failed promotion", "cleanup_ms", time.Since(cleanupStart).Milliseconds())
	}()

	mountDir, err := mountDataset(ctx, targetDataset)
	if err != nil {
		return err
	}
	cleanupMountDir = mountDir

	workspace := filepath.Join(mountDir, "workspace")
	if err := os.RemoveAll(workspace); err != nil {
		return fmt.Errorf("clear workspace: %w", err)
	}
	if err := runGit("", nil, "clone", "--depth", "1", "--branch", req.DefaultBranch, req.RepoURL, workspace); err != nil {
		return err
	}

	manifest, err = LoadManifest(workspace)
	if err != nil {
		return err
	}
	toolchain, err = DetectToolchain(workspace)
	if err != nil {
		return err
	}
	jobEnv, err := buildJobEnv(manifest)
	if err != nil {
		return err
	}
	if err := writeLockfileHash(workspace, toolchain); err != nil {
		return err
	}
	commitSHA, err = gitHeadSHA(workspace)
	if err != nil {
		return err
	}

	job = buildGuestJob(uuid.NewString(), manifest, toolchain, true, true, jobEnv)
	if err := unmountDataset(ctx, mountDir); err != nil {
		return err
	}
	cleanupMountDir = ""

	orch := fastsandbox.New(m.firecrackerConfig, m.logger)
	startedAt = time.Now().UTC()
	result, err = orch.RunDataset(ctx, job, targetDataset, false)
	if err != nil {
		return fmt.Errorf("warm run failed: %w", err)
	}
	logger.Info("warm run finished", "exit_code", result.ExitCode, "duration_ms", result.Duration.Milliseconds(), "commit_sha", commitSHA)
	if result.ExitCode != 0 {
		logs := strings.TrimSpace(formatJobLogs(result))
		if logs == "" {
			return fmt.Errorf("warm run exited with code %d", result.ExitCode)
		}
		return fmt.Errorf("warm run exited with code %d\n%s", result.ExitCode, logs)
	}

	logger.Info("warm filesystem check starting", "dataset", targetDataset)
	filesystemCheckStart := time.Now()
	if err := checkFilesystem(ctx, targetDataset); err != nil {
		filesystemCheckDuration = time.Since(filesystemCheckStart)
		logger.Error("warm filesystem check failed", "dataset", targetDataset, "duration_ms", filesystemCheckDuration.Milliseconds(), "err", err)
		return fmt.Errorf("warm filesystem check failed: %w", err)
	}
	filesystemCheckDuration = time.Since(filesystemCheckStart)
	filesystemCheckOK = true
	logger.Info("warm filesystem check passed", "dataset", targetDataset, "duration_ms", filesystemCheckDuration.Milliseconds())

	logger.Info("promoting repo golden snapshot", "dataset", targetDataset)
	snapshotPromotionStart := time.Now()
	if err := replaceReadySnapshot(ctx, targetDataset); err != nil {
		snapshotPromotionDuration = time.Since(snapshotPromotionStart)
		return err
	}
	snapshotPromotionDuration = time.Since(snapshotPromotionStart)
	if err := m.writeActiveRepoGoldenDataset(repoKey, targetDataset); err != nil {
		return err
	}
	cleanupTargetDataset = false
	promoted = true
	logger.Info("repo golden promoted", "dataset", targetDataset, "promotion_ms", snapshotPromotionDuration.Milliseconds(), "previous_dataset", previousDataset)
	if previousDataset != "" && previousDataset != targetDataset {
		previousDestroyStart := time.Now()
		if err := destroyDatasetRecursive(ctx, previousDataset); err != nil {
			m.logger.Warn("failed to destroy previous repo golden", "repo", req.Repo, "dataset", previousDataset, "err", err)
		} else {
			previousDestroyDuration = time.Since(previousDestroyStart)
			logger.Info("destroyed previous repo golden", "dataset", previousDataset, "duration_ms", previousDestroyDuration.Milliseconds())
		}
	}
	return nil
}

func (m *Manager) Exec(ctx context.Context, req ExecRequest) (*fastsandbox.JobResult, error) {
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

	repoKey := sanitizeRepoKey(req.Repo)
	repoDataset, err := m.activeRepoGoldenDataset(repoKey)
	if err != nil {
		return nil, err
	}
	if repoDataset == "" {
		return nil, fmt.Errorf("repo golden for %s does not exist; run warm first", req.Repo)
	}
	snapshot := repoDataset + "@ready"
	exists, err := snapshotExists(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("repo golden %s does not exist; run warm first", snapshot)
	}

	jobID := uuid.NewString()
	jobDataset := fmt.Sprintf("%s/%s/%s", m.firecrackerConfig.Pool, m.firecrackerConfig.CIDataset, jobID)
	cloneStart := time.Now()
	if err := zfsClone(ctx, snapshot, jobDataset); err != nil {
		return nil, err
	}
	cloneDuration := time.Since(cloneStart)

	mountDir, err := mountDataset(ctx, jobDataset)
	if err != nil {
		_ = destroyDatasetRecursive(context.Background(), jobDataset)
		return nil, err
	}

	// Unmount-then-destroy cleanup; same pattern as Warm().
	execMounted := true
	execCleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), zfsTimeout)
		defer cancel()
		if execMounted {
			if umountErr := unmountDataset(cleanupCtx, mountDir); umountErr != nil {
				m.logger.Warn("exec cleanup unmount failed", "mount", mountDir, "err", umountErr)
			}
			execMounted = false
		}
		_ = destroyDatasetRecursive(cleanupCtx, jobDataset)
	}

	workspace := filepath.Join(mountDir, "workspace")
	if err := fetchRef(workspace, req.Ref); err != nil {
		execCleanup()
		return nil, err
	}
	manifest, err := LoadManifest(workspace)
	if err != nil {
		execCleanup()
		return nil, err
	}
	toolchain, err := DetectToolchain(workspace)
	if err != nil {
		execCleanup()
		return nil, err
	}
	jobEnv, err := buildJobEnv(manifest)
	if err != nil {
		execCleanup()
		return nil, err
	}
	commitSHA, err := gitHeadSHA(workspace)
	if err != nil {
		execCleanup()
		return nil, err
	}
	installNeeded, err := lockfileChanged(workspace, toolchain)
	if err != nil {
		execCleanup()
		return nil, err
	}

	job := buildGuestJob(jobID, manifest, toolchain, installNeeded, false, jobEnv)
	if err := unmountDataset(ctx, mountDir); err != nil {
		_ = destroyDatasetRecursive(context.Background(), jobDataset)
		return nil, err
	}
	execMounted = false

	orch := fastsandbox.New(m.firecrackerConfig, m.logger)
	startedAt := time.Now().UTC()
	result, err := orch.RunDataset(ctx, job, jobDataset, true)
	completedAt := time.Now().UTC()
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
		RunErr:            err,
	}); telemetryErr != nil {
		m.logger.Warn("emit ci exec telemetry failed", "repo", req.Repo, "run_id", runID, "err", telemetryErr)
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (m *Manager) baseGoldenSnapshot() string {
	return fmt.Sprintf("%s/%s@ready", m.firecrackerConfig.Pool, m.firecrackerConfig.GoldenZvol)
}

func (m *Manager) repoGoldensRootDataset() string {
	return fmt.Sprintf("%s/%s", m.firecrackerConfig.Pool, repoGoldensDataset)
}

func (m *Manager) nextRepoGoldenDataset(repoKey string) string {
	return fmt.Sprintf("%s/%s-%d", m.repoGoldensRootDataset(), repoKey, time.Now().UTC().UnixNano())
}

func (m *Manager) repoGoldenStatePath(repoKey string) string {
	return filepath.Join(repoGoldenStateRoot, repoKey+".dataset")
}

func (m *Manager) activeRepoGoldenDataset(repoKey string) (string, error) {
	data, err := os.ReadFile(m.repoGoldenStatePath(repoKey))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read repo golden state: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func (m *Manager) writeActiveRepoGoldenDataset(repoKey, dataset string) error {
	if err := os.MkdirAll(repoGoldenStateRoot, 0o755); err != nil {
		return fmt.Errorf("mkdir repo golden state root: %w", err)
	}
	return os.WriteFile(m.repoGoldenStatePath(repoKey), []byte(dataset+"\n"), 0o644)
}

func buildGuestJob(jobID string, manifest *Manifest, toolchain *Toolchain, installNeeded bool, warm bool, env map[string]string) fastsandbox.JobConfig {
	repoRoot := "/workspace"

	runCommand := manifest.Run
	if warm {
		runCommand = manifest.ResolvedPrepare()
	}
	if toolchain != nil {
		runCommand = toolchain.ResolveCommand(runCommand)
	}

	job := fastsandbox.JobConfig{
		JobID:      jobID,
		RunCommand: cloneStringSlice(runCommand),
		RunWorkDir: manifest.RepoWorkDir(),
		Services:   cloneStringSlice(manifest.Services),
		Env:        cloneStringMap(env),
	}
	switch resolvedProfile(manifest) {
	case RuntimeProfileNode:
		if installNeeded {
			job.PrepareCommand = toolchain.InstallCommand()
			job.PrepareWorkDir = repoRoot
		}
	}
	return job
}

func buildJobEnv(manifest *Manifest) (map[string]string, error) {
	env := map[string]string{
		"CI": "true",
	}
	for _, name := range manifest.Env {
		value, ok := os.LookupEnv(name)
		if !ok {
			return nil, fmt.Errorf("required env %s is not set", name)
		}
		env[name] = value
	}
	return env, nil
}

func resolvedProfile(manifest *Manifest) RuntimeProfile {
	if manifest == nil || manifest.Profile == "" || manifest.Profile == RuntimeProfileAuto {
		return RuntimeProfileNode
	}
	return manifest.Profile
}

func fetchRef(repoRoot, ref string) error {
	if err := runGit(repoRoot, []string{"GIT_TERMINAL_PROMPT=0"}, "fetch", "--depth", "1", "origin", ref); err != nil {
		return err
	}
	if err := runGit(repoRoot, nil, "checkout", "--force", "FETCH_HEAD"); err != nil {
		return err
	}
	return nil
}

func lockfileChanged(repoRoot string, toolchain *Toolchain) (bool, error) {
	lockfile := toolchain.LockfilePath(repoRoot)
	if lockfile == "" {
		return true, nil
	}
	current, err := ComputeFileSHA256(lockfile)
	if err != nil {
		return false, err
	}
	recordedPath := filepath.Join(repoRoot, lockfileHashRelPath)
	recorded, err := os.ReadFile(recordedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(recorded)) != current, nil
}

func writeLockfileHash(repoRoot string, toolchain *Toolchain) error {
	lockfile := toolchain.LockfilePath(repoRoot)
	if lockfile == "" {
		return nil
	}
	hash, err := ComputeFileSHA256(lockfile)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(repoRoot, lockfileHashRelPath), hash+"\n", 0o644)
}

func replaceReadySnapshot(ctx context.Context, dataset string) error {
	snapshot := dataset + "@ready"
	exists, err := snapshotExists(ctx, snapshot)
	if err != nil {
		return err
	}
	if exists {
		if err := zfsDestroy(ctx, snapshot, false); err != nil {
			return err
		}
	}
	return zfsSnapshot(ctx, snapshot)
}

func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func formatJobLogs(result fastsandbox.JobResult) string {
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
