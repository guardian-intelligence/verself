package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type config struct {
	name       string
	repo       string
	ref        string
	repoRoot   string
	createRepo bool
	allowDirty bool
	dryRun     bool
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "actions-publisher: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if !cfg.allowDirty {
		if err := ensureCleanSource(ctx, cfg.repoRoot); err != nil {
			return err
		}
	}
	if err := validateConfig(ctx, cfg); err != nil {
		return err
	}

	target := fmt.Sprintf("//src/actions/%s:github_action_dist", cfg.name)
	if err := runCommand(ctx, cfg.repoRoot, "bazelisk build "+target, "bazelisk", "build", target); err != nil {
		return err
	}
	tarPath, err := bazelOutputPath(ctx, cfg.repoRoot, target)
	if err != nil {
		return err
	}
	sourceSHA, err := gitOutput(ctx, cfg.repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return err
	}

	tempRoot, err := os.MkdirTemp("", "verself-action-publish-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempRoot)

	distDir := filepath.Join(tempRoot, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return fmt.Errorf("create dist dir: %w", err)
	}
	if err := extractTar(tarPath, distDir); err != nil {
		return err
	}

	repoExists := githubRepoExists(ctx, cfg)
	if cfg.dryRun {
		if !repoExists {
			if !cfg.createRepo {
				return fmt.Errorf("GitHub repository %s does not exist; pass --create-repo", cfg.repo)
			}
			fmt.Printf("dry run: would create public GitHub repository %s\n", cfg.repo)
		}
		fmt.Printf("dry run: would publish %s from %s to %s@%s\n", cfg.name, tarPath, cfg.repo, cfg.ref)
		return nil
	}
	if err := ensureGitHubRepo(ctx, cfg, repoExists); err != nil {
		return err
	}
	return publishBranch(ctx, cfg, distDir, strings.TrimSpace(sourceSHA))
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("actions-publisher", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := config{}
	fs.StringVar(&cfg.name, "name", "", "action name under src/actions")
	fs.StringVar(&cfg.repo, "repo", "", "GitHub repository owner/name")
	fs.StringVar(&cfg.ref, "ref", "", "branch ref to publish")
	fs.StringVar(&cfg.repoRoot, "repo-root", ".", "monorepo root")
	fs.BoolVar(&cfg.createRepo, "create-repo", false, "create the GitHub repository when missing")
	fs.BoolVar(&cfg.allowDirty, "allow-dirty", false, "allow publishing from a dirty monorepo")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "build and stage without pushing")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.name == "" {
		return config{}, errors.New("--name is required")
	}
	if cfg.repo == "" {
		return config{}, errors.New("--repo is required")
	}
	if cfg.ref == "" {
		return config{}, errors.New("--ref is required")
	}
	root, err := filepath.Abs(cfg.repoRoot)
	if err != nil {
		return config{}, fmt.Errorf("resolve repo root: %w", err)
	}
	cfg.repoRoot = root
	return cfg, nil
}

func validateConfig(ctx context.Context, cfg config) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(cfg.name) {
		return fmt.Errorf("--name must be a simple action directory name, got %q", cfg.name)
	}
	if !repoRE.MatchString(cfg.repo) {
		return fmt.Errorf("--repo must be owner/name, got %q", cfg.repo)
	}
	if _, err := captureCommand(ctx, cfg.repoRoot, "git", "check-ref-format", "--branch", cfg.ref); err != nil {
		return fmt.Errorf("--ref must be a valid branch name: %w", err)
	}
	actionDir := filepath.Join(cfg.repoRoot, "src", "actions", cfg.name)
	if st, err := os.Stat(actionDir); err != nil || !st.IsDir() {
		if err == nil {
			err = fmt.Errorf("%s is not a directory", actionDir)
		}
		return fmt.Errorf("action source missing: %w", err)
	}
	return nil
}

func ensureCleanSource(ctx context.Context, repoRoot string) error {
	out, err := gitOutput(ctx, repoRoot, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return errors.New("source repository is dirty; commit before publishing or pass --allow-dirty")
	}
	return nil
}

func bazelOutputPath(ctx context.Context, repoRoot, target string) (string, error) {
	out, err := captureCommand(ctx, repoRoot, "bazelisk", "cquery", "--output=files", target)
	if err != nil {
		return "", err
	}
	lines := strings.Fields(out)
	if len(lines) != 1 {
		return "", fmt.Errorf("expected one Bazel output for %s, got %d: %q", target, len(lines), out)
	}
	if filepath.IsAbs(lines[0]) {
		return lines[0], nil
	}
	return filepath.Join(repoRoot, lines[0]), nil
}

func githubRepoExists(ctx context.Context, cfg config) bool {
	if _, err := captureCommand(ctx, cfg.repoRoot, "gh", "repo", "view", cfg.repo); err == nil {
		return true
	}
	return false
}

func ensureGitHubRepo(ctx context.Context, cfg config, exists bool) error {
	if exists {
		return nil
	}
	if !cfg.createRepo {
		return fmt.Errorf("GitHub repository %s does not exist; pass --create-repo", cfg.repo)
	}
	description := fmt.Sprintf("Verself %s GitHub Action", cfg.name)
	return runCommand(ctx, cfg.repoRoot, "gh repo create "+cfg.repo, "gh", "repo", "create", cfg.repo, "--public", "--description", description, "--clone=false")
}

func publishBranch(ctx context.Context, cfg config, distDir, sourceSHA string) error {
	checkoutDir, err := os.MkdirTemp("", "verself-action-repo-*")
	if err != nil {
		return fmt.Errorf("create checkout dir: %w", err)
	}
	defer os.RemoveAll(checkoutDir)

	remote := "https://github.com/" + cfg.repo + ".git"
	commands := [][]string{
		{"git", "init"},
		{"git", "remote", "add", "origin", remote},
		{"git", "config", "user.name", "Verself Action Publisher"},
		{"git", "config", "user.email", "actions-publisher@verself.sh"},
	}
	for _, cmd := range commands {
		if err := runCommand(ctx, checkoutDir, strings.Join(cmd, " "), cmd[0], cmd[1:]...); err != nil {
			return err
		}
	}

	if _, err := captureCommand(ctx, checkoutDir, "git", "fetch", "--depth=1", "origin", "refs/heads/"+cfg.ref); err == nil {
		if err := runCommand(ctx, checkoutDir, "git checkout "+cfg.ref, "git", "checkout", "-B", cfg.ref, "FETCH_HEAD"); err != nil {
			return err
		}
	} else {
		if err := runCommand(ctx, checkoutDir, "git checkout --orphan "+cfg.ref, "git", "checkout", "--orphan", cfg.ref); err != nil {
			return err
		}
	}

	if err := clearWorktree(checkoutDir); err != nil {
		return err
	}
	if err := copyDir(distDir, checkoutDir); err != nil {
		return err
	}
	if err := runCommand(ctx, checkoutDir, "git add -A", "git", "add", "-A"); err != nil {
		return err
	}
	if err := runCommand(ctx, checkoutDir, "git diff --cached --quiet", "git", "diff", "--cached", "--quiet"); err == nil {
		fmt.Printf("%s@%s already matches %s\n", cfg.repo, cfg.ref, sourceSHA)
		return nil
	}
	message := fmt.Sprintf("Publish %s action from %s", cfg.name, sourceSHA)
	if err := runCommand(ctx, checkoutDir, "git commit", "git", "commit", "-m", message); err != nil {
		return err
	}
	if err := runCommand(ctx, checkoutDir, "git push origin HEAD:"+cfg.ref, "git", "push", "origin", "HEAD:refs/heads/"+cfg.ref); err != nil {
		return err
	}
	fmt.Printf("published %s@%s from %s\n", cfg.repo, cfg.ref, sourceSHA)
	return nil
}

func extractTar(tarPath, dst string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("open Bazel action tar: %w", err)
	}
	defer f.Close()

	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read Bazel action tar: %w", err)
		}
		target, err := safeJoin(dst, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("extract dir %s: %w", hdr.Name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("extract parent %s: %w", hdr.Name, err)
			}
			mode := hdr.FileInfo().Mode().Perm()
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("extract file %s: %w", hdr.Name, err)
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return fmt.Errorf("extract file %s: %w", hdr.Name, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close file %s: %w", hdr.Name, closeErr)
			}
		default:
			return fmt.Errorf("unsupported tar entry %s type %d", hdr.Name, hdr.Typeflag)
		}
	}
}

func safeJoin(root, name string) (string, error) {
	clean := filepath.Clean(name)
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("unsafe tar path %q", name)
	}
	return filepath.Join(root, clean), nil
}

func clearWorktree(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read checkout dir: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("clear checkout entry %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink %s", path)
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to copy non-regular file %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create parent %s: %w", target, err)
		}
		in, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("create %s: %w", target, err)
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return fmt.Errorf("copy %s: %w", path, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", target, closeErr)
		}
		return nil
	})
}

func gitOutput(ctx context.Context, cwd string, args ...string) (string, error) {
	return captureCommand(ctx, cwd, "git", args...)
}

func runCommand(ctx context.Context, cwd, label, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", label, err)
	}
	return nil
}

func captureCommand(ctx context.Context, cwd, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, msg)
	}
	return stdout.String(), nil
}
