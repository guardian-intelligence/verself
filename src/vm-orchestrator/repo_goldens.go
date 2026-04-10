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

const (
	repoWarmCommitEvent       = "repo_warm.commit"
	repoExecCheckoutEvent     = "repo_exec.checkout"
	repoExecInstallDecision   = "repo_exec.install_decision"
	repoExecInstallNeededAttr = "install_needed"
	repoCommitSHAAttr         = "commit_sha"
)

// buildInVMWarmJob wraps the caller's install commands in a script that
// runs entirely inside the Firecracker VM: fetch the repo, install deps,
// and write lockfile hashes. The host never mounts the zvol.
func buildInVMWarmJob(originalJob JobConfig, repoURL, defaultBranch, lockfileRelPath, hostServiceIP string, hostServicePort int) (JobConfig, error) {
	guestRepoURL, err := repoURLForGuest(repoURL, hostServiceIP, hostServicePort)
	if err != nil {
		return JobConfig{}, err
	}
	guestRepoURLNoCredentials := repoURLWithoutCredentials(guestRepoURL)

	var script strings.Builder
	script.WriteString("set -eu\n")
	writeGuestEventFunction(&script)
	script.WriteString("REPO_URL=" + shellQuoteArg(guestRepoURL) + "\n")
	script.WriteString("REPO_URL_NO_CREDENTIALS=" + shellQuoteArg(guestRepoURLNoCredentials) + "\n")
	script.WriteString("rm -rf /workspace\n")
	script.WriteString("mkdir -p /workspace\n")
	script.WriteString("cd /workspace\n")
	script.WriteString("git init\n")
	script.WriteString("git remote add origin \"$REPO_URL_NO_CREDENTIALS\"\n")
	script.WriteString("git fetch --depth 1 \"$REPO_URL\" " + shellQuoteArg(defaultBranch) + "\n")
	script.WriteString("git checkout --force FETCH_HEAD\n")
	script.WriteString("rm -f .git/FETCH_HEAD\n")
	script.WriteString("unset REPO_URL\n")
	script.WriteString("COMMIT_SHA=$(git rev-parse HEAD)\n")
	writeGuestEventShellAttr(&script, repoWarmCommitEvent, repoCommitSHAAttr, "$COMMIT_SHA")

	if len(originalJob.PrepareCommand) > 0 {
		wd := originalJob.PrepareWorkDir
		if wd == "" {
			wd = "/workspace"
		}
		script.WriteString(fmt.Sprintf("cd %s\n", shellQuoteArg(wd)))
		script.WriteString(shellJoinCmd(originalJob.PrepareCommand) + "\n")
	}

	if len(originalJob.RunCommand) > 0 {
		wd := originalJob.RunWorkDir
		if wd == "" {
			wd = "/workspace"
		}
		script.WriteString(fmt.Sprintf("cd %s\n", shellQuoteArg(wd)))
		script.WriteString(shellJoinCmd(originalJob.RunCommand) + "\n")
	}

	// Write lockfile hash for exec-time change detection.
	// The exec path reads this to decide whether to skip dependency install.
	lockfileRelPath = strings.TrimSpace(lockfileRelPath)
	if lockfileRelPath != "" {
		script.WriteString("mkdir -p /workspace/.forge-metal\n")
		script.WriteString(fmt.Sprintf("sha256sum %s | cut -d' ' -f1 > %s\n",
			shellQuoteArg(workspacePath(lockfileRelPath)), shellQuoteArg(workspacePath(lockfileHashRelPath))))
	}

	return JobConfig{
		JobID:      originalJob.JobID,
		RunCommand: []string{"sh", "-c", script.String()},
		RunWorkDir: "/",
		Services:   originalJob.Services,
		Env:        originalJob.Env,
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

	var prepare strings.Builder
	prepare.WriteString("set -eu\n")
	writeGuestEventFunction(&prepare)
	prepare.WriteString("REPO_URL=" + shellQuoteArg(guestRepoURL) + "\n")
	prepare.WriteString("REPO_URL_NO_CREDENTIALS=" + shellQuoteArg(guestRepoURLNoCredentials) + "\n")
	prepare.WriteString("cd /workspace\n")
	prepare.WriteString("git remote set-url origin \"$REPO_URL_NO_CREDENTIALS\"\n")
	prepare.WriteString("git fetch --depth 1 \"$REPO_URL\" " + shellQuoteArg(ref) + "\n")
	prepare.WriteString("git checkout --force FETCH_HEAD\n")
	prepare.WriteString("rm -f .git/FETCH_HEAD\n")
	prepare.WriteString("unset REPO_URL\n")
	prepare.WriteString("COMMIT_SHA=$(git rev-parse HEAD)\n")
	writeGuestEventShellAttr(&prepare, repoExecCheckoutEvent, repoCommitSHAAttr, "$COMMIT_SHA")
	prepare.WriteString("INSTALL_NEEDED=1\n")
	if strings.TrimSpace(lockfileRelPath) != "" {
		currentLockfile := workspacePath(lockfileRelPath)
		recordedLockfile := workspacePath(lockfileHashRelPath)
		prepare.WriteString(fmt.Sprintf("if [ -f %s ] && [ -f %s ]; then\n", shellQuoteArg(currentLockfile), shellQuoteArg(recordedLockfile)))
		prepare.WriteString(fmt.Sprintf("  CURRENT_LOCKFILE_SHA=$(sha256sum %s | cut -d' ' -f1)\n", shellQuoteArg(currentLockfile)))
		prepare.WriteString(fmt.Sprintf("  RECORDED_LOCKFILE_SHA=$(cat %s)\n", shellQuoteArg(recordedLockfile)))
		prepare.WriteString("  if [ \"$CURRENT_LOCKFILE_SHA\" = \"$RECORDED_LOCKFILE_SHA\" ]; then\n")
		prepare.WriteString("    INSTALL_NEEDED=0\n")
		prepare.WriteString("  fi\n")
		prepare.WriteString("fi\n")
	}
	writeGuestEventShellAttr(&prepare, repoExecInstallDecision, repoExecInstallNeededAttr, "$INSTALL_NEEDED")
	if len(originalJob.PrepareCommand) > 0 {
		prepare.WriteString("if [ \"$INSTALL_NEEDED\" = \"1\" ]; then\n")
		wd := originalJob.PrepareWorkDir
		if wd == "" {
			wd = "/workspace"
		}
		prepare.WriteString(fmt.Sprintf("  cd %s\n", shellQuoteArg(wd)))
		prepare.WriteString("  " + shellJoinCmd(originalJob.PrepareCommand) + "\n")
		prepare.WriteString("fi\n")
	}

	return JobConfig{
		JobID:          originalJob.JobID,
		PrepareCommand: []string{"sh", "-c", prepare.String()},
		PrepareWorkDir: "/",
		RunCommand:     originalJob.RunCommand,
		RunWorkDir:     originalJob.RunWorkDir,
		Services:       originalJob.Services,
		Env:            originalJob.Env,
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

func workspacePath(relPath string) string {
	relPath = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(relPath)), "/")
	return filepath.ToSlash(filepath.Join("/workspace", relPath))
}

func writeGuestEventFunction(script *strings.Builder) {
	script.WriteString("emit_guest_event() {\n")
	script.WriteString("  if [ -n \"${FORGE_METAL_GUEST_EVENT_FIFO:-}\" ]; then\n")
	script.WriteString("    printf '%s\\n' \"$1\" > \"$FORGE_METAL_GUEST_EVENT_FIFO\" || true\n")
	script.WriteString("  fi\n")
	script.WriteString("}\n")
}

func writeGuestEventShellAttr(script *strings.Builder, kind, attr, shellValue string) {
	script.WriteString("emit_guest_event ")
	script.WriteString(shellQuoteArg(fmt.Sprintf(`{"kind":"%s","attrs":{"%s":"`, kind, attr)))
	script.WriteString(`"`)
	script.WriteString(shellValue)
	script.WriteString(`"`)
	script.WriteString(shellQuoteArg(`"}}`))
	script.WriteString("\n")
}

func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func shellJoinCmd(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuoteArg(arg)
	}
	return strings.Join(quoted, " ")
}
