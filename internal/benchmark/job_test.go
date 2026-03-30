package benchmark

import "testing"

func TestResolveSeededWorkspaceRejectsEscape(t *testing.T) {
	_, err := resolveSeededWorkspace("/benchpool/ci/job-123", "../outside")
	if err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestResolveSeededWorkspaceAcceptsCloneRelativePath(t *testing.T) {
	got, err := resolveSeededWorkspace("/benchpool/ci/job-123", "workspaces/taxonomy")
	if err != nil {
		t.Fatalf("resolveSeededWorkspace: %v", err)
	}
	want := "/benchpool/ci/job-123/workspaces/taxonomy"
	if got != want {
		t.Fatalf("path: got %q, want %q", got, want)
	}
}
