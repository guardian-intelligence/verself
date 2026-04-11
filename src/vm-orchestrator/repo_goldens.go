package vmorchestrator

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

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

// buildInVMWarmJob asks vm-init's trusted guest supervisor to fetch the repo,
// run user commands as the guest runner user, and return a typed manifest.
func buildInVMWarmJob(originalJob JobConfig, repoURL, defaultBranch, lockfileRelPath, hostServiceIP string, hostServicePort int) (JobConfig, error) {
	guestRepoURL, err := repoURLForGuest(repoURL, hostServiceIP, hostServicePort)
	if err != nil {
		return JobConfig{}, err
	}
	guestRepoURLNoCredentials := repoURLWithoutCredentials(guestRepoURL)

	return JobConfig{
		JobID:    originalJob.JobID,
		Services: originalJob.Services,
		Env:      originalJob.Env,
		RepoOperation: &vmproto.RepoOperation{
			Kind:               vmproto.RepoOperationWarm,
			RepoURL:            guestRepoURL,
			OriginURL:          guestRepoURLNoCredentials,
			Ref:                defaultBranch,
			LockfileRelPath:    strings.TrimSpace(lockfileRelPath),
			UserPrepareCommand: cloneStringSlice(originalJob.PrepareCommand),
			UserPrepareWorkDir: originalJob.PrepareWorkDir,
			UserRunCommand:     cloneStringSlice(originalJob.RunCommand),
			UserRunWorkDir:     originalJob.RunWorkDir,
		},
	}, nil
}

func buildInVMRepoExecJob(originalJob JobConfig, repoURL, ref, lockfileRelPath, hostServiceIP string, hostServicePort int) (JobConfig, error) {
	if len(originalJob.RunCommand) == 0 {
		return JobConfig{}, fmt.Errorf("repo exec run command is required")
	}

	guestRepoURL, err := repoURLForGuest(repoURL, hostServiceIP, hostServicePort)
	if err != nil {
		return JobConfig{}, err
	}
	guestRepoURLNoCredentials := repoURLWithoutCredentials(guestRepoURL)

	return JobConfig{
		JobID:    originalJob.JobID,
		Services: originalJob.Services,
		Env:      originalJob.Env,
		RepoOperation: &vmproto.RepoOperation{
			Kind:               vmproto.RepoOperationExec,
			RepoURL:            guestRepoURL,
			OriginURL:          guestRepoURLNoCredentials,
			Ref:                ref,
			LockfileRelPath:    strings.TrimSpace(lockfileRelPath),
			UserPrepareCommand: cloneStringSlice(originalJob.PrepareCommand),
			UserPrepareWorkDir: originalJob.PrepareWorkDir,
			UserRunCommand:     cloneStringSlice(originalJob.RunCommand),
			UserRunWorkDir:     originalJob.RunWorkDir,
		},
	}, nil
}

func repoURLForGuest(repoURL, hostServiceIP string, hostServicePort int) (string, error) {
	if hostServiceIP == "" {
		hostServiceIP = defaultHostServiceIP
	}
	if hostServicePort == 0 {
		hostServicePort = defaultHostServicePort
	}
	parsed, err := url.Parse(strings.TrimSpace(repoURL))
	if err != nil {
		return "", fmt.Errorf("parse repo_url for guest: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("repo_url scheme %q is not supported for guest host-service fetch; use http or https", parsed.Scheme)
	}
	parsed.Scheme = "http"
	parsed.Host = net.JoinHostPort(hostServiceIP, strconv.Itoa(hostServicePort))
	return parsed.String(), nil
}

func repoURLWithoutCredentials(repoURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(repoURL))
	if err != nil {
		return repoURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return repoURL
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}
