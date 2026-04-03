package ci

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildGuestJob_NodeExecInstallsFromRepoRootAndRunsFromWorkdir(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixtures", "next-bun-monorepo")
	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	toolchain, err := DetectToolchain(root)
	if err != nil {
		t.Fatalf("DetectToolchain: %v", err)
	}

	job := buildGuestJob("job-1", manifest, toolchain, true, false, map[string]string{"CI": "true"})
	if job.JobID != "job-1" {
		t.Fatalf("job id: got %q", job.JobID)
	}
	if !reflect.DeepEqual(job.PrepareCommand, []string{"bun", "install", "--frozen-lockfile"}) {
		t.Fatalf("prepare command: got %+v", job.PrepareCommand)
	}
	if job.PrepareWorkDir != "/workspace" {
		t.Fatalf("prepare workdir: got %q", job.PrepareWorkDir)
	}
	if !reflect.DeepEqual(job.RunCommand, []string{"bun", "run", "ci"}) {
		t.Fatalf("run command: got %+v", job.RunCommand)
	}
	if job.RunWorkDir != "/workspace/apps/web" {
		t.Fatalf("run workdir: got %q", job.RunWorkDir)
	}
	if len(job.Services) != 0 {
		t.Fatalf("services: got %+v", job.Services)
	}
}

func TestBuildGuestJob_WarmUsesManifestPrepareAsRunPhase(t *testing.T) {
	manifest := &Manifest{
		Version: 1,
		WorkDir: ".",
		Prepare: []string{"npm", "run", "warm"},
		Run:     []string{"npm", "test"},
		Profile: RuntimeProfileNode,
	}
	toolchain := &Toolchain{PackageManager: PackageManagerNPM}

	job := buildGuestJob("job-2", manifest, toolchain, false, true, map[string]string{"CI": "true"})
	if len(job.PrepareCommand) != 0 {
		t.Fatalf("unexpected prepare command: %+v", job.PrepareCommand)
	}
	if !reflect.DeepEqual(job.RunCommand, []string{"npm", "run", "warm"}) {
		t.Fatalf("warm run command: got %+v", job.RunCommand)
	}
}

func TestBuildGuestJob_PNPMUsesNPX(t *testing.T) {
	manifest := &Manifest{
		Version: 1,
		WorkDir: ".",
		Run:     []string{"pnpm", "run", "ci"},
		Profile: RuntimeProfileNode,
	}
	toolchain := &Toolchain{
		PackageManager:        PackageManagerPNPM,
		PackageManagerVersion: "9.15.0",
	}

	job := buildGuestJob("job-3", manifest, toolchain, true, false, map[string]string{"CI": "true"})
	want := []string{"npx", "--yes", "pnpm@9.15.0", "install", "--frozen-lockfile"}
	if !reflect.DeepEqual(job.PrepareCommand, want) {
		t.Fatalf("prepare command: got %+v want %+v", job.PrepareCommand, want)
	}
	runWant := []string{"npx", "--yes", "pnpm@9.15.0", "run", "ci"}
	if !reflect.DeepEqual(job.RunCommand, runWant) {
		t.Fatalf("run command: got %+v want %+v", job.RunCommand, runWant)
	}
}

func TestBuildJobEnv_IncludesCIAndConfiguredVars(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://fixture")
	manifest := &Manifest{
		Version: 1,
		Run:     []string{"npm", "test"},
		Env:     []string{"DATABASE_URL"},
	}

	env, err := buildJobEnv(manifest)
	if err != nil {
		t.Fatalf("buildJobEnv: %v", err)
	}
	if env["CI"] != "true" {
		t.Fatalf("CI env: got %q", env["CI"])
	}
	if env["DATABASE_URL"] != "postgres://fixture" {
		t.Fatalf("DATABASE_URL env: got %q", env["DATABASE_URL"])
	}
}

func TestBuildJobEnv_RequiresConfiguredVars(t *testing.T) {
	manifest := &Manifest{
		Version: 1,
		Run:     []string{"npm", "test"},
		Env:     []string{"MISSING_SECRET"},
	}

	_, err := buildJobEnv(manifest)
	if err == nil || !strings.Contains(err.Error(), "required env MISSING_SECRET is not set") {
		t.Fatalf("buildJobEnv error: %v", err)
	}
}

func TestUniquePRBranch_IsRepoScopedAndStable(t *testing.T) {
	branch := uniquePRBranch("test/forge-metal-warm-path", "next-bun-monorepo", "20260331-120000")
	want := "test/forge-metal-warm-path-next-bun-monorepo-20260331-120000"
	if branch != want {
		t.Fatalf("branch: got %q want %q", branch, want)
	}
}

func TestFixtureWorkflow_IncludesRunID(t *testing.T) {
	workflow := fixtureWorkflow("http://127.0.0.1:3000", "fixtures-pass-20260401")
	if !strings.Contains(workflow, "--run-id 'fixtures-pass-20260401'") {
		t.Fatalf("workflow missing run-id flag: %q", workflow)
	}
}

func TestPRNumberFromRef(t *testing.T) {
	if got := prNumberFromRef("refs/pull/42/head"); got != 42 {
		t.Fatalf("prNumberFromRef: got %d want 42", got)
	}
	if got := prNumberFromRef("refs/heads/main"); got != 0 {
		t.Fatalf("prNumberFromRef non-PR ref: got %d want 0", got)
	}
}

func TestSortedEnvKeys_RedactsValues(t *testing.T) {
	keys := sortedEnvKeys(map[string]string{
		"B": "two",
		"A": "one",
	})
	if got := strings.Join(keys, ","); got != "A,B" {
		t.Fatalf("sortedEnvKeys: got %q", got)
	}
}

func TestWriteManifestForTestHelperUsesExpectedPath(t *testing.T) {
	root := t.TempDir()
	writeManifestForTest(t, root, "version = 1\nrun = [\"npm\", \"test\"]\n")
	if _, err := os.Stat(filepath.Join(root, ".forge-metal", "ci.toml")); err != nil {
		t.Fatalf("stat manifest: %v", err)
	}
}
