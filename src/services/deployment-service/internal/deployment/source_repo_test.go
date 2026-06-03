package deployment

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureSourceRepositoryClonesEmptyRoot(t *testing.T) {
	origin := initGitOrigin(t)
	dest := filepath.Join(t.TempDir(), "repo")
	if err := EnsureSourceRepository(context.Background(), SourceRepositoryConfig{RepoRoot: dest, RepoURL: origin}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatalf("cloned repo .git missing: %v", err)
	}
}

func TestEnsureSourceRepositoryRejectsMismatchedOrigin(t *testing.T) {
	origin := initGitOrigin(t)
	dest := filepath.Join(t.TempDir(), "repo")
	runGit(t, "", "clone", "--no-tags", origin, dest)
	err := EnsureSourceRepository(context.Background(), SourceRepositoryConfig{RepoRoot: dest, RepoURL: origin + "-other"})
	if err == nil {
		t.Fatal("mismatched origin accepted")
	}
}

func initGitOrigin(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "origin")
	runGit(t, "", "init", repo)
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial")
	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmdArgs := args
	if dir != "" {
		cmdArgs = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", cmdArgs, err, out)
	}
}
