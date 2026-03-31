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

	"github.com/forge-metal/forge-metal/internal/firecracker"
)

const (
	lockfileHashRelPath = ".forge-metal/lockfile.sha256"
	repoGoldensDataset  = "repo-goldens"
)

var repoGoldenStateRoot = "/var/lib/ci/repo-goldens"

type Manager struct {
	firecrackerConfig firecracker.Config
	logger            *slog.Logger
}

type WarmRequest struct {
	Repo          string
	RepoURL       string
	DefaultBranch string
}

type ExecRequest struct {
	Repo    string
	RepoURL string
	Ref     string
}

func NewManager(cfg firecracker.Config, logger *slog.Logger) *Manager {
	return &Manager{
		firecrackerConfig: cfg,
		logger:            logger,
	}
}

func (m *Manager) Warm(ctx context.Context, req WarmRequest) error {
	if req.Repo == "" {
		return fmt.Errorf("repo is required")
	}
	if req.RepoURL == "" {
		return fmt.Errorf("repo_url is required")
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}

	repoKey := sanitizeRepoKey(req.Repo)
	targetDataset := m.nextRepoGoldenDataset(repoKey)
	previousDataset, err := m.activeRepoGoldenDataset(repoKey)
	if err != nil {
		return err
	}

	if err := ensureDataset(ctx, m.repoGoldensRootDataset()); err != nil {
		return err
	}
	if err := zfsClone(ctx, m.baseGoldenSnapshot(), targetDataset); err != nil {
		return err
	}

	mountDir, err := mountDataset(ctx, targetDataset)
	if err != nil {
		return err
	}

	workspace := filepath.Join(mountDir, "workspace")
	if err := os.RemoveAll(workspace); err != nil {
		_ = unmountDataset(context.Background(), mountDir)
		return fmt.Errorf("clear workspace: %w", err)
	}
	if err := runGit("", nil, "clone", "--depth", "1", "--branch", req.DefaultBranch, req.RepoURL, workspace); err != nil {
		_ = unmountDataset(context.Background(), mountDir)
		return err
	}

	manifest, err := LoadManifest(workspace)
	if err != nil {
		_ = unmountDataset(context.Background(), mountDir)
		return err
	}
	toolchain, err := DetectToolchain(workspace)
	if err != nil {
		_ = unmountDataset(context.Background(), mountDir)
		return err
	}
	if err := writeLockfileHash(workspace, toolchain); err != nil {
		_ = unmountDataset(context.Background(), mountDir)
		return err
	}

	job := firecracker.JobConfig{
		JobID:   uuid.NewString(),
		WorkDir: "/workspace",
		Command: buildGuestCommand(manifest, toolchain, true, true),
		Env: map[string]string{
			"CI": "true",
		},
	}
	if err := unmountDataset(ctx, mountDir); err != nil {
		return err
	}

	orch := firecracker.New(m.firecrackerConfig, m.logger)
	result, err := orch.RunDataset(ctx, job, targetDataset, false)
	if err != nil {
		return fmt.Errorf("warm run failed: %w", err)
	}
	if result.ExitCode != 0 {
		logs := strings.TrimSpace(result.Logs)
		if logs == "" {
			return fmt.Errorf("warm run exited with code %d", result.ExitCode)
		}
		return fmt.Errorf("warm run exited with code %d\n%s", result.ExitCode, logs)
	}
	if err := replaceReadySnapshot(ctx, targetDataset); err != nil {
		return err
	}
	if err := m.writeActiveRepoGoldenDataset(repoKey, targetDataset); err != nil {
		return err
	}
	if previousDataset != "" && previousDataset != targetDataset {
		if err := destroyDatasetRecursive(ctx, previousDataset); err != nil {
			m.logger.Warn("failed to destroy previous repo golden", "repo", req.Repo, "dataset", previousDataset, "err", err)
		}
	}
	return nil
}

func (m *Manager) Exec(ctx context.Context, req ExecRequest) (*firecracker.JobResult, error) {
	if req.Repo == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if req.RepoURL == "" {
		return nil, fmt.Errorf("repo_url is required")
	}
	if req.Ref == "" {
		return nil, fmt.Errorf("ref is required")
	}

	repoKey := sanitizeRepoKey(req.Repo)
	repoDataset, err := m.activeRepoGoldenDataset(repoKey)
	if err != nil {
		return nil, err
	}
	if repoDataset == "" {
		repoDataset = m.legacyRepoGoldenDataset(repoKey)
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
	if err := zfsClone(ctx, snapshot, jobDataset); err != nil {
		return nil, err
	}

	mountDir, err := mountDataset(ctx, jobDataset)
	if err != nil {
		_ = destroyDatasetRecursive(context.Background(), jobDataset)
		return nil, err
	}

	workspace := filepath.Join(mountDir, "workspace")
	manifest, err := LoadManifest(workspace)
	if err != nil {
		_ = unmountDataset(context.Background(), mountDir)
		_ = destroyDatasetRecursive(context.Background(), jobDataset)
		return nil, err
	}
	toolchain, err := DetectToolchain(workspace)
	if err != nil {
		_ = unmountDataset(context.Background(), mountDir)
		_ = destroyDatasetRecursive(context.Background(), jobDataset)
		return nil, err
	}
	if err := fetchRef(workspace, req.Ref); err != nil {
		_ = unmountDataset(context.Background(), mountDir)
		_ = destroyDatasetRecursive(context.Background(), jobDataset)
		return nil, err
	}
	installNeeded, err := lockfileChanged(workspace, toolchain)
	if err != nil {
		_ = unmountDataset(context.Background(), mountDir)
		_ = destroyDatasetRecursive(context.Background(), jobDataset)
		return nil, err
	}

	job := firecracker.JobConfig{
		JobID:   jobID,
		WorkDir: "/workspace",
		Command: buildGuestCommand(manifest, toolchain, installNeeded, false),
		Env: map[string]string{
			"CI": "true",
		},
	}
	if err := unmountDataset(ctx, mountDir); err != nil {
		_ = destroyDatasetRecursive(context.Background(), jobDataset)
		return nil, err
	}

	orch := firecracker.New(m.firecrackerConfig, m.logger)
	result, err := orch.RunDataset(ctx, job, jobDataset, true)
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

func (m *Manager) legacyRepoGoldenDataset(repoKey string) string {
	return fmt.Sprintf("%s/%s", m.repoGoldensRootDataset(), repoKey)
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

func buildGuestCommand(manifest *Manifest, toolchain *Toolchain, installNeeded bool, warm bool) []string {
	parts := make([]string, 0, 2)
	repoRoot := "/workspace"
	if installNeeded {
		parts = append(parts, fmt.Sprintf("cd %s && %s", shellQuote(repoRoot), shellJoin(toolchain.InstallCommand())))
	}

	command := manifest.CICommand
	if warm {
		command = manifest.WarmCommand
	}
	parts = append(parts, fmt.Sprintf("cd %s && %s", shellQuote(manifest.RepoWorkDir()), shellJoin(command)))

	services := strings.Join(manifest.Services, ",")
	return []string{
		"/bin/sh",
		"/usr/local/bin/forge-metal-ci-run",
		"--services", services,
		"--workdir", repoRoot,
		"--",
		"bash", "-lc", strings.Join(parts, " && "),
	}
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
