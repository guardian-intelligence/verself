package vmorchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const lockfileHashRelPath = ".forge-metal/lockfile.sha256"

func sanitizeRepoKey(repo string) string {
	repo = strings.TrimSpace(strings.ToLower(repo))
	replacer := strings.NewReplacer("/", "-", "_", "-", " ", "-", ".", "-")
	repo = replacer.Replace(repo)
	repo = strings.Trim(repo, "-")
	if repo == "" {
		return "repo"
	}
	return repo
}

func repoGoldensRootDataset(cfg Config) string {
	return fmt.Sprintf("%s/%s", cfg.Pool, "repo-goldens")
}

func baseGoldenSnapshot(cfg Config) string {
	return fmt.Sprintf("%s/%s@ready", cfg.Pool, cfg.GoldenZvol)
}

func nextRepoGoldenDataset(cfg Config, repoKey string, now time.Time) string {
	return fmt.Sprintf("%s/%s-%d", repoGoldensRootDataset(cfg), repoKey, now.UTC().UnixNano())
}

func repoGoldenStatePath(rootDir, repoKey string) string {
	return filepath.Join(rootDir, repoKey+".dataset")
}

func activeRepoGoldenDataset(rootDir, repoKey string) (string, error) {
	data, err := os.ReadFile(repoGoldenStatePath(rootDir, repoKey))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read repo golden state: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func writeActiveRepoGoldenDataset(rootDir, repoKey, dataset string) error {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return fmt.Errorf("mkdir repo golden state root: %w", err)
	}
	if err := os.WriteFile(repoGoldenStatePath(rootDir, repoKey), []byte(dataset+"\n"), 0o644); err != nil {
		return fmt.Errorf("write repo golden state: %w", err)
	}
	return nil
}

func ensureDataset(ctx context.Context, dataset string) error {
	return runZFS(ctx, "create", "-p", dataset)
}

func destroyDatasetRecursive(ctx context.Context, dataset string) error {
	exists, err := zfsExists(ctx, dataset)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return runZFS(ctx, "destroy", "-R", "-f", dataset)
}

func zfsSnapshot(ctx context.Context, snapshot string) error {
	return runZFS(ctx, "snapshot", snapshot)
}

func zfsDestroy(ctx context.Context, target string, recursive bool) error {
	args := []string{"destroy"}
	if recursive {
		args = append(args, "-R", "-f")
	}
	args = append(args, target)
	return runZFS(ctx, args...)
}

func replaceReadySnapshot(ctx context.Context, dataset string) error {
	snapshot := dataset + "@ready"
	exists, err := zfsExists(ctx, snapshot)
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

func zfsExists(ctx context.Context, target string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "list", "-H", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "does not exist") {
			return false, nil
		}
		return false, fmt.Errorf("zfs list %s: %s: %w", target, strings.TrimSpace(string(out)), err)
	}
	return true, nil
}

func runZFS(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func mountDataset(ctx context.Context, dataset string) (string, error) {
	return mountZvol(ctx, zvolDevicePath(dataset))
}

func checkFilesystem(ctx context.Context, dataset string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	devPath := zvolDevicePath(dataset)
	cmd := exec.CommandContext(ctx, "fsck.ext4", "-n", devPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fsck.ext4 %s: %s: %w", devPath, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func runGit(dir string, env []string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func gitHeadSHA(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
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

func clearWorkspace(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("clear workspace %s: %w", path, err)
	}
	return nil
}

func writeLockfileHash(repoRoot, lockfileRelPath string) error {
	lockfileRelPath = strings.TrimSpace(lockfileRelPath)
	if lockfileRelPath == "" {
		return nil
	}
	hash, err := computeFileSHA256(filepath.Join(repoRoot, lockfileRelPath))
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(repoRoot, lockfileHashRelPath), hash+"\n", 0o644)
}

func lockfileChanged(repoRoot, lockfileRelPath string) (bool, error) {
	lockfileRelPath = strings.TrimSpace(lockfileRelPath)
	if lockfileRelPath == "" {
		return true, nil
	}

	current, err := computeFileSHA256(filepath.Join(repoRoot, lockfileRelPath))
	if err != nil {
		return false, err
	}
	recordedPath := filepath.Join(repoRoot, lockfileHashRelPath)
	recorded, err := os.ReadFile(recordedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("read lockfile hash record: %w", err)
	}
	return strings.TrimSpace(string(recorded)) != current, nil
}

func computeFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func writeFile(path, contents string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
