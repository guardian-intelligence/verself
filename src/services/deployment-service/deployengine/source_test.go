package deployengine

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareSourceChecksOutRequestedSHA(t *testing.T) {
	ctx := context.Background()
	origin := initRepo(t, filepath.Join(t.TempDir(), "origin"))
	first := commitFile(t, origin, "deploy.txt", "first\n")
	second := commitFile(t, origin, "deploy.txt", "second\n")
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, "", "clone", origin, clone)
	runGit(t, clone, "checkout", "--detach", second)

	engine, err := newExecution(Options{
		Site:         "gamma",
		SHA:          first,
		DeployRunKey: "deploy_test",
		RepoRoot:     clone,
	})
	if err != nil {
		t.Fatalf("newExecution: %v", err)
	}
	err = prepareSource(ctx, engine)
	if err != nil {
		t.Fatalf("prepareSource: %v", err)
	}
	got := strings.TrimSpace(runGitOutput(t, clone, "rev-parse", "HEAD"))
	if got != first {
		t.Fatalf("HEAD = %s, want %s", got, first)
	}
}

func TestPrepareSourceRejectsDirtyWorktree(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"))
	sha := commitFile(t, repo, "deploy.txt", "first\n")
	if err := os.WriteFile(filepath.Join(repo, "deploy.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	engine, err := newExecution(Options{
		Site:         "gamma",
		SHA:          sha,
		DeployRunKey: "deploy_test",
		RepoRoot:     repo,
	})
	if err != nil {
		t.Fatalf("newExecution: %v", err)
	}
	err = prepareSource(ctx, engine)
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("prepareSource error = %v, want dirty worktree refusal", err)
	}
}

func TestNewExecutionRejectsSymbolicSHA(t *testing.T) {
	_, err := newExecution(Options{
		Site:         "gamma",
		SHA:          "HEAD",
		DeployRunKey: "deploy_test",
		RepoRoot:     t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "40-character git commit sha") {
		t.Fatalf("newExecution error = %v, want sha validation", err)
	}
}

func TestNewExecutionNormalizesUppercaseSHA(t *testing.T) {
	exec, err := newExecution(Options{
		Site:         "gamma",
		SHA:          "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
		DeployRunKey: "deploy_test",
		RepoRoot:     t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if exec.SHA != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("sha = %q, want lowercase", exec.SHA)
	}
}

func initRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, "", "init", dir)
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Verself Test")
	return dir
}

func commitFile(t *testing.T, repo, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", name)
	runGit(t, repo, "commit", "-m", "commit "+name)
	return strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = runGitOutput(t, dir, args...)
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String()
}
