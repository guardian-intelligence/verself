package jobs

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	"github.com/forge-metal/workload"
)

func TestBuildRepoExecRequest_PNPMFixture(t *testing.T) {
	root := filepath.Join("..", "..", "..", "platform", "test", "fixtures", "next-pnpm-postgres")
	inspection, err := workload.InspectRepoPath(root)
	if err != nil {
		t.Fatalf("InspectRepoPath: %v", err)
	}

	request, err := BuildRepoExecRequest(RepoExecSpec{
		JobID: "11111111-1111-1111-1111-111111111111",
		RepoTarget: RepoTarget{
			Repo:    "fixtures/next-pnpm-postgres",
			RepoURL: "https://git.example.test/fixtures/next-pnpm-postgres.git",
		},
		Ref: "refs/pull/42/head",
	}, inspection)
	if err != nil {
		t.Fatalf("BuildRepoExecRequest: %v", err)
	}

	if request.Config != (vmorchestrator.Config{}) {
		t.Fatalf("runtime config should be zero-valued, got %+v", request.Config)
	}
	if request.Repo != "fixtures/next-pnpm-postgres" {
		t.Fatalf("repo: got %q", request.Repo)
	}
	if request.RepoURL != "https://git.example.test/fixtures/next-pnpm-postgres.git" {
		t.Fatalf("repo_url: got %q", request.RepoURL)
	}
	if request.Ref != "refs/pull/42/head" {
		t.Fatalf("ref: got %q", request.Ref)
	}
	if request.LockfileRelPath != "pnpm-lock.yaml" {
		t.Fatalf("lockfile: got %q", request.LockfileRelPath)
	}
	wantPrepare := []string{"npx", "--yes", "pnpm@9.15.0", "install", "--frozen-lockfile"}
	if !reflect.DeepEqual(request.JobTemplate.PrepareCommand, wantPrepare) {
		t.Fatalf("prepare command: got %+v want %+v", request.JobTemplate.PrepareCommand, wantPrepare)
	}
	wantRun := []string{"npx", "--yes", "pnpm@9.15.0", "run", "ci"}
	if !reflect.DeepEqual(request.JobTemplate.RunCommand, wantRun) {
		t.Fatalf("run command: got %+v want %+v", request.JobTemplate.RunCommand, wantRun)
	}
	if !reflect.DeepEqual(request.JobTemplate.Services, []string{"postgres"}) {
		t.Fatalf("services: got %+v", request.JobTemplate.Services)
	}
	if request.JobTemplate.Env["CI"] != "true" {
		t.Fatalf("CI env: got %q", request.JobTemplate.Env["CI"])
	}
}

func TestBuildWarmGoldenRequest_DefaultsBranchAndUsesPreparePhase(t *testing.T) {
	inspection := &workload.Inspection{
		Manifest: &workload.Manifest{
			Version: 1,
			WorkDir: ".",
			Prepare: []string{"npm", "run", "warm"},
			Run:     []string{"npm", "test"},
			Profile: workload.RuntimeProfileNode,
		},
		Toolchain: &workload.Toolchain{
			PackageManager: workload.PackageManagerNPM,
		},
	}

	request, err := BuildWarmGoldenRequest(WarmGoldenSpec{
		JobID: "22222222-2222-2222-2222-222222222222",
		RepoTarget: RepoTarget{
			Repo:    "fixtures/warm",
			RepoURL: "https://git.example.test/fixtures/warm.git",
		},
	}, inspection)
	if err != nil {
		t.Fatalf("BuildWarmGoldenRequest: %v", err)
	}

	if request.DefaultBranch != "main" {
		t.Fatalf("default branch: got %q", request.DefaultBranch)
	}
	if !reflect.DeepEqual(request.Job.RunCommand, []string{"npm", "run", "warm"}) {
		t.Fatalf("warm run command: got %+v", request.Job.RunCommand)
	}
	if !reflect.DeepEqual(request.Job.PrepareCommand, []string{"npm", "install"}) {
		t.Fatalf("warm prepare command: got %+v", request.Job.PrepareCommand)
	}
}

func TestPreparedRepoExec_CleansUpInspectionDir(t *testing.T) {
	root := t.TempDir()
	writeManifestForJobsTest(t, root, "version = 1\nrun = [\"npm\", \"test\"]\n")
	writePackageJSONForJobsTest(t, root, "{\n  \"name\": \"fixture\",\n  \"packageManager\": \"npm@10.0.0\"\n}\n")
	initGitRepoForJobsTest(t, root)

	inspection, err := workload.InspectRepoPath(root)
	if err != nil {
		t.Fatalf("InspectRepoPath: %v", err)
	}
	prepared := &PreparedRepoExec{Inspection: inspection}

	if prepared.Inspection == nil || prepared.Inspection.Path == "" {
		t.Fatalf("inspection path not populated")
	}
	if _, err := os.Stat(prepared.Inspection.Path); err != nil {
		t.Fatalf("stat inspection path: %v", err)
	}
	prepared.Cleanup()
	if _, err := os.Stat(prepared.Inspection.Path); !os.IsNotExist(err) {
		t.Fatalf("inspection path still exists after cleanup: %v", err)
	}
}

func TestBuildRepoExecRequest_RequiresRef(t *testing.T) {
	inspection := &workload.Inspection{
		Manifest: &workload.Manifest{
			Version: 1,
			Run:     []string{"npm", "test"},
		},
		Toolchain: &workload.Toolchain{
			PackageManager: workload.PackageManagerNPM,
		},
	}

	_, err := BuildRepoExecRequest(RepoExecSpec{
		JobID: "44444444-4444-4444-4444-444444444444",
		RepoTarget: RepoTarget{
			Repo:    "fixtures/local",
			RepoURL: "https://git.example.test/fixtures/local.git",
		},
	}, inspection)
	if err == nil || !strings.Contains(err.Error(), "repo exec ref is required") {
		t.Fatalf("BuildRepoExecRequest error: %v", err)
	}
}

func writeManifestForJobsTest(t *testing.T, root, contents string) {
	t.Helper()
	path := filepath.Join(root, ".forge-metal")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir manifest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "ci.toml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writePackageJSONForJobsTest(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
}

func initGitRepoForJobsTest(t *testing.T, root string) {
	t.Helper()
	runGitForJobsTest(t, root, "init", "-b", "main")
	runGitForJobsTest(t, root, "config", "user.name", "Forge Metal Tests")
	runGitForJobsTest(t, root, "config", "user.email", "tests@example.com")
	runGitForJobsTest(t, root, "add", ".")
	runGitForJobsTest(t, root, "commit", "-m", "initial")
}

func runGitForJobsTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
}
