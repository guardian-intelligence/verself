package ci

import (
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
		t.Fatalf("ci script: got %q", script)
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
