package jobs

import "testing"

func TestResolveStickyDiskPathUsesGitHubWorkspace(t *testing.T) {
	got, err := resolveStickyDiskPath("src/viteplus-monorepo/node_modules", "guardian-intelligence/forge-metal")
	if err != nil {
		t.Fatalf("resolveStickyDiskPath returned error: %v", err)
	}
	want := "/workspace/forge-metal/forge-metal/src/viteplus-monorepo/node_modules"
	if got != want {
		t.Fatalf("resolveStickyDiskPath = %q, want %q", got, want)
	}
}

func TestResolveStickyDiskPathKeepsRunnerHome(t *testing.T) {
	got, err := resolveStickyDiskPath("~/.npm", "guardian-intelligence/forge-metal")
	if err != nil {
		t.Fatalf("resolveStickyDiskPath returned error: %v", err)
	}
	want := "/home/runner/.npm"
	if got != want {
		t.Fatalf("resolveStickyDiskPath = %q, want %q", got, want)
	}
}

func TestResolveStickyDiskPathRejectsRelativePathWithoutRepository(t *testing.T) {
	if _, err := resolveStickyDiskPath("node_modules", "forge-metal"); err == nil {
		t.Fatal("resolveStickyDiskPath returned nil error")
	}
}
