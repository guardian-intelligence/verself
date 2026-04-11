package main

import "testing"

func TestRepoUserEnvRemovesGuestEventFIFO(t *testing.T) {
	t.Parallel()

	got := repoUserEnv([]string{
		"PATH=/bin",
		"HOME=/root",
		"XDG_CACHE_HOME=/root/.cache",
		"NPM_CONFIG_CACHE=/root/.npm",
		"npm_config_cache=/root/.npm",
		"BUN_INSTALL_CACHE_DIR=/root/.bun",
		guestEventFIFOEnv + "=/run/forge-metal/guest-events.fifo",
		"CI=true",
	})

	want := []string{
		"PATH=/bin",
		"CI=true",
		"HOME=/home/runner",
		"XDG_CACHE_HOME=/workspace/.cache",
		"NPM_CONFIG_CACHE=/workspace/.cache/npm",
		"npm_config_cache=/workspace/.cache/npm",
		"BUN_INSTALL_CACHE_DIR=/workspace/.cache/bun",
	}
	if len(got) != len(want) {
		t.Fatalf("repo user env: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("repo user env[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWorkspaceFilePathRejectsEscapes(t *testing.T) {
	t.Parallel()

	for _, rel := range []string{"../etc/passwd", "/etc/passwd", "../../workspace/package.json"} {
		if _, err := workspaceFilePath(rel); err == nil {
			t.Fatalf("workspaceFilePath(%q) accepted escaping path", rel)
		}
	}
}

func TestWorkspaceFilePathResolvesInsideWorkspace(t *testing.T) {
	t.Parallel()

	got, err := workspaceFilePath("packages/app/package.json")
	if err != nil {
		t.Fatalf("workspaceFilePath: %v", err)
	}
	if got != "/workspace/packages/app/package.json" {
		t.Fatalf("workspace path: got %q", got)
	}
}
