package deployment

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const sourceCheckoutTimeout = 2 * time.Minute

type SourceRepositoryConfig struct {
	RepoRoot string
	RepoURL  string
}

func EnsureSourceRepository(ctx context.Context, cfg SourceRepositoryConfig) error {
	repoRoot := strings.TrimSpace(cfg.RepoRoot)
	repoURL := strings.TrimSpace(cfg.RepoURL)
	if repoRoot == "" {
		return fmt.Errorf("deployment source repo root is required")
	}
	if repoURL == "" {
		return fmt.Errorf("deployment source repo URL is required")
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".git")); err == nil {
		return ensureOrigin(ctx, repoRoot, repoURL)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat deployment source repo: %w", err)
	}
	if err := ensureEmptyOrMissing(repoRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(repoRoot), 0o750); err != nil {
		return fmt.Errorf("create deployment source parent: %w", err)
	}
	if err := gitRun(ctx, "", "clone", "--no-tags", repoURL, repoRoot); err != nil {
		return err
	}
	return ensureOrigin(ctx, repoRoot, repoURL)
}

func CheckoutSourceSHA(ctx context.Context, repoRoot, sha string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	sha = strings.ToLower(strings.TrimSpace(sha))
	if repoRoot == "" {
		return fmt.Errorf("deployment source repo root is required")
	}
	if sha == "" {
		return fmt.Errorf("deployment source sha is required")
	}
	if err := gitRun(ctx, repoRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
		return err
	}
	if err := ensureCleanSourceWorktree(ctx, repoRoot); err != nil {
		return err
	}
	current, err := gitOutput(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.EqualFold(current, sha) {
		return nil
	}
	checkoutCtx, cancel := context.WithTimeout(ctx, sourceCheckoutTimeout)
	defer cancel()
	if err := gitRun(checkoutCtx, repoRoot, "fetch", "--no-tags", "--depth=1", "origin", sha); err != nil {
		if fallbackErr := gitRun(checkoutCtx, repoRoot, "fetch", "--no-tags", "origin"); fallbackErr != nil {
			return fmt.Errorf("%w; fallback fetch failed: %v", err, fallbackErr)
		}
	}
	if err := gitRun(checkoutCtx, repoRoot, "checkout", "--detach", sha); err != nil {
		return err
	}
	current, err = gitOutput(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if !strings.EqualFold(current, sha) {
		return fmt.Errorf("deployment source checkout resolved %s, expected %s", current, sha)
	}
	return nil
}

func ensureEmptyOrMissing(path string) error {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read deployment source repo root: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("deployment source repo root %s exists but is not a git repo and is not empty", path)
	}
	return nil
}

func ensureOrigin(ctx context.Context, repoRoot, repoURL string) error {
	got, err := gitOutput(ctx, repoRoot, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if strings.TrimSpace(got) != repoURL {
		return fmt.Errorf("deployment source repo origin is %q, expected %q", strings.TrimSpace(got), repoURL)
	}
	if err := gitRun(ctx, repoRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
		return err
	}
	return ensureCleanSourceWorktree(ctx, repoRoot)
}

func ensureCleanSourceWorktree(ctx context.Context, repoRoot string) error {
	status, err := gitOutput(ctx, repoRoot, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("deployment source repo root %s is dirty", repoRoot)
	}
	return nil
}

func gitRun(ctx context.Context, repoRoot string, args ...string) error {
	_, err := gitOutput(ctx, repoRoot, args...)
	return err
}

func gitOutput(ctx context.Context, repoRoot string, args ...string) (string, error) {
	cmdArgs := append([]string{}, args...)
	if repoRoot != "" {
		cmdArgs = append([]string{"-C", repoRoot}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(cmdArgs, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
