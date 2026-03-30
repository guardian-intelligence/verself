package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/forge-metal/forge-metal/internal/zfsharness"
)

// GoldenProject describes a repository to pre-warm in the golden image.
type GoldenProject struct {
	Name     string     // directory name under /workspaces/
	RepoURL  string     // git clone URL
	Branch   string     // branch to clone
	SubDir   string     // subdirectory for build commands (e.g. "dashboard/final-example")
	WarmCmds [][]string // commands to run after npm ci (e.g. [["npm","run","lint"]])
}

// DefaultGoldenProjects returns the projects to pre-warm in the golden image.
// Starts with next-learn only — small project, fast iteration.
func DefaultGoldenProjects() []GoldenProject {
	return []GoldenProject{
		{
			Name:    "next-learn",
			RepoURL: "https://github.com/vercel/next-learn.git",
			Branch:  "main",
			SubDir:  "dashboard/final-example",
			WarmCmds: [][]string{
				{"npm", "run", "lint"},
				{"npm", "run", "build"},
			},
		},
	}
}

// BuildGoldenImage creates and warms the benchmark golden image.
//
// Lifecycle: ensure dataset → clone repos → npm ci → warm caches → snapshot.
// If @ready already exists and force is false, the build is skipped.
func BuildGoldenImage(ctx context.Context, harness *zfsharness.Harness, projects []GoldenProject, force bool, logger *slog.Logger) error {
	if !force {
		ready, err := harness.GoldenReady(ctx)
		if err != nil {
			return fmt.Errorf("check golden ready: %w", err)
		}
		if ready {
			logger.Info("golden image already exists, skipping build (use --force to rebuild)")
			return nil
		}
	}

	logger.Info("building golden image", "projects", len(projects))

	if err := harness.EnsureGoldenDataset(ctx); err != nil {
		return fmt.Errorf("ensure golden dataset: %w", err)
	}

	mountpoint := harness.GoldenMountpoint()

	for _, proj := range projects {
		if err := buildProject(ctx, mountpoint, proj, logger); err != nil {
			return fmt.Errorf("build project %s: %w", proj.Name, err)
		}
	}

	if err := harness.SnapshotGoldenReady(ctx); err != nil {
		return fmt.Errorf("snapshot golden: %w", err)
	}

	logger.Info("golden image ready", "snapshot", harness.GoldenSnapshot())
	return nil
}

func buildProject(ctx context.Context, mountpoint string, proj GoldenProject, logger *slog.Logger) error {
	workspaceDir := filepath.Join(mountpoint, "workspaces", proj.Name)
	logger.Info("cloning project", "project", proj.Name, "target", workspaceDir)

	if err := os.MkdirAll(filepath.Dir(workspaceDir), 0755); err != nil {
		return fmt.Errorf("mkdir workspaces: %w", err)
	}

	// Remove stale workspace from previous failed build.
	if _, err := os.Stat(workspaceDir); err == nil {
		if err := os.RemoveAll(workspaceDir); err != nil {
			return fmt.Errorf("remove stale workspace: %w", err)
		}
	}

	if err := gitClone(ctx, proj.RepoURL, proj.Branch, workspaceDir); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	buildDir := workspaceDir
	if proj.SubDir != "" {
		buildDir = filepath.Join(workspaceDir, proj.SubDir)
	}

	// npm ci — required, fail if it fails.
	logger.Info("installing dependencies", "project", proj.Name)
	if err := runBuildCmd(ctx, buildDir, []string{"npm", "ci"}); err != nil {
		return fmt.Errorf("npm ci: %w", err)
	}

	// Warm commands — best-effort, log failures but continue.
	for _, cmd := range proj.WarmCmds {
		logger.Info("warming cache", "project", proj.Name, "cmd", strings.Join(cmd, " "))
		if err := runBuildCmd(ctx, buildDir, cmd); err != nil {
			logger.Warn("warm command failed (continuing)", "cmd", strings.Join(cmd, " "), "err", err)
		}
	}

	// Record lockfile hash for cache-hit detection at benchmark time.
	lockfilePath := filepath.Join(buildDir, "package-lock.json")
	if _, err := os.Stat(lockfilePath); err == nil {
		hashCmd := exec.CommandContext(ctx, "sha256sum", lockfilePath)
		hashCmd.Dir = buildDir
		if out, err := hashCmd.Output(); err == nil {
			hashFile := filepath.Join(buildDir, ".lockfile-hash")
			_ = os.WriteFile(hashFile, out, 0644)
		}
	}

	logger.Info("project ready", "project", proj.Name)
	return nil
}

func runBuildCmd(ctx context.Context, dir string, argv []string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CI=true",
		"NODE_ENV=production",
		"NEXT_TELEMETRY_DISABLED=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", strings.Join(argv, " "), truncateTail(string(out), 1024))
	}
	return nil
}

func truncateTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
