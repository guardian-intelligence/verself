package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildGuestCommand_InstallsFromRepoRootAndRunsFromWorkdir(t *testing.T) {
	root := filepath.Join("..", "..", "test", "fixtures", "next-bun-monorepo")
	manifest, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	toolchain, err := DetectToolchain(root)
	if err != nil {
		t.Fatalf("DetectToolchain: %v", err)
	}

	cmd := buildGuestCommand(manifest, toolchain, true, false)
	if len(cmd) != 10 {
		t.Fatalf("unexpected command length: got %d", len(cmd))
	}
	if cmd[0] != "/bin/sh" || cmd[1] != "/usr/local/bin/forge-metal-ci-run" {
		t.Fatalf("runner: got %q %q", cmd[0], cmd[1])
	}
	if cmd[5] != "/workspace" {
		t.Fatalf("wrapper workdir: got %q", cmd[5])
	}
	if cmd[7] != "bash" || cmd[8] != "-lc" {
		t.Fatalf("shell launcher: got %q %q", cmd[7], cmd[8])
	}

	script := cmd[9]
	if !strings.Contains(script, `cd '/workspace' && 'bash' '-lc' 'HOST_GATEWAY="$(ip route show default | awk '"'"'/default/ {print $3; exit}'"'"')" && test -n "$HOST_GATEWAY" && bun install --frozen-lockfile --registry "http://${HOST_GATEWAY}:4873"'`) {
		t.Fatalf("install script: got %q", script)
	}
	if !strings.Contains(script, "cd '/workspace/apps/web' && 'bun' 'run' 'ci'") {
		t.Fatalf("run script: got %q", script)
	}
}

func TestBuildGuestCommand_UsesPrepareDuringWarm(t *testing.T) {
	manifest := &Manifest{
		Version: 1,
		WorkDir: ".",
		Prepare: []string{"npm", "run", "warm"},
		Run:     []string{"npm", "test"},
		Profile: RuntimeProfileNode,
	}
	toolchain := &Toolchain{PackageManager: PackageManagerNPM}

	cmd := buildGuestCommand(manifest, toolchain, false, true)
	if !strings.Contains(cmd[9], "cd '/workspace' && 'npm' 'run' 'warm'") {
		t.Fatalf("warm script: got %q", cmd[9])
	}
	if strings.Contains(cmd[9], "'npm' 'test'") {
		t.Fatalf("warm script unexpectedly contains run command: %q", cmd[9])
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
	workflow := fixtureWorkflow("http://127.0.0.1:3000", "fixtures-e2e-20260401")
	if !strings.Contains(workflow, "--run-id 'fixtures-e2e-20260401'") {
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
