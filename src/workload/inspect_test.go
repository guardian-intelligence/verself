package workload

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectRepoPathLoadsManifestToolchainAndCommit(t *testing.T) {
	root := filepath.Join("..", "platform", "test", "fixtures", "next-bun-monorepo")
	inspection, err := InspectRepoPath(root)
	if err != nil {
		t.Fatalf("InspectRepoPath: %v", err)
	}
	if inspection.Path != root {
		t.Fatalf("path: got %q want %q", inspection.Path, root)
	}
	if inspection.Manifest == nil {
		t.Fatal("expected manifest")
	}
	if inspection.Toolchain == nil {
		t.Fatal("expected toolchain")
	}
	if inspection.CommitSHA == "" {
		t.Fatal("expected commit sha")
	}
}

func TestInspectRepoRefChecksOutRequestedCommit(t *testing.T) {
	repoRoot := initGitFixtureRepo(t)
	headMain := gitRevParse(t, repoRoot, "HEAD")

	if err := os.WriteFile(filepath.Join(repoRoot, "package.json"), []byte(`{"name":"fixture","packageManager":"npm@10.9.0"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	writeManifestForTest(t, repoRoot, "version = 1\nrun = [\"npm\", \"test\"]\n")
	gitCommitAll(t, repoRoot, "add workload")
	headWorkload := gitRevParse(t, repoRoot, "HEAD")

	inspection, err := InspectRepoRef(repoRoot, headWorkload)
	if err != nil {
		t.Fatalf("InspectRepoRef: %v", err)
	}
	defer CleanupInspection(inspection.Path)

	if inspection.CommitSHA != headWorkload {
		t.Fatalf("commit sha: got %q want %q", inspection.CommitSHA, headWorkload)
	}
	if inspection.CommitSHA == headMain {
		t.Fatalf("expected fetched ref to differ from initial head")
	}
}

func initGitFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitFixture(t, "", "init", root)
	runGitFixture(t, root, "config", "user.email", "forge-metal-fixture@example.com")
	runGitFixture(t, root, "config", "user.name", "Forge Metal Fixture")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitFixture(t, root, "add", "README.md")
	runGitFixture(t, root, "commit", "-m", "initial")
	return root
}

func gitCommitAll(t *testing.T, root, message string) {
	t.Helper()
	runGitFixture(t, root, "add", ".")
	runGitFixture(t, root, "commit", "-m", message)
}

func gitRevParse(t *testing.T, root, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v: %s", ref, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

func runGitFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}
