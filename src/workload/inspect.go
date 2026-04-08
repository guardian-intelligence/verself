package workload

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Inspection struct {
	Path      string
	Manifest  *Manifest
	Toolchain *Toolchain
	CommitSHA string
}

func InspectRepoDefaultBranch(repoURL, branch string) (*Inspection, error) {
	tmp, err := os.MkdirTemp("", "forge-metal-ci-warm-*")
	if err != nil {
		return nil, fmt.Errorf("create warm inspection dir: %w", err)
	}
	if err := runGit("", nil, "clone", "--depth", "1", "--branch", branch, repoURL, tmp); err != nil {
		CleanupInspection(tmp)
		return nil, err
	}
	return InspectRepoPath(tmp)
}

func InspectRepoRef(repoURL, ref string) (*Inspection, error) {
	tmp, err := os.MkdirTemp("", "forge-metal-ci-exec-*")
	if err != nil {
		return nil, fmt.Errorf("create exec inspection dir: %w", err)
	}
	if err := runGit("", nil, "clone", "--no-checkout", "--depth", "1", repoURL, tmp); err != nil {
		CleanupInspection(tmp)
		return nil, err
	}
	if err := fetchRef(tmp, ref); err != nil {
		CleanupInspection(tmp)
		return nil, err
	}
	return InspectRepoPath(tmp)
}

func InspectRepoPath(repoRoot string) (*Inspection, error) {
	manifest, err := LoadManifest(repoRoot)
	if err != nil {
		return nil, err
	}
	toolchain, err := DetectToolchain(repoRoot)
	if err != nil {
		return nil, err
	}
	commitSHA, err := gitHeadSHA(repoRoot)
	if err != nil {
		return nil, err
	}
	return &Inspection{
		Path:      repoRoot,
		Manifest:  manifest,
		Toolchain: toolchain,
		CommitSHA: commitSHA,
	}, nil
}

func CleanupInspection(path string) {
	if path == "" {
		return
	}
	_ = os.RemoveAll(path)
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
