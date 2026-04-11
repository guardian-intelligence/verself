package main

import "testing"

func TestRepoUserEnvRemovesGuestEventFIFO(t *testing.T) {
	t.Parallel()

	got := repoUserEnv([]string{
		"PATH=/bin",
		guestEventFIFOEnv + "=/run/forge-metal/guest-events.fifo",
		"CI=true",
	})

	if len(got) != 2 || got[0] != "PATH=/bin" || got[1] != "CI=true" {
		t.Fatalf("repo user env: %#v", got)
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
