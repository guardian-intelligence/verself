package jobs

import (
	"strings"
	"testing"

	"github.com/forge-metal/workload"
)

func TestNormalizeImportRepoRequestRejectsUnsafeCloneURLs(t *testing.T) {
	tests := []struct {
		name     string
		cloneURL string
	}{
		{name: "scp style ssh", cloneURL: "git@github.com:forge-metal/repo.git"},
		{name: "file scheme", cloneURL: "file:///tmp/repo.git"},
		{name: "git protocol", cloneURL: "git://github.com/forge-metal/repo.git"},
		{name: "loopback ipv4", cloneURL: "https://127.0.0.1/forge-metal/repo.git"},
		{name: "loopback ipv6", cloneURL: "https://[::1]/forge-metal/repo.git"},
		{name: "private ipv4", cloneURL: "https://10.0.0.1/forge-metal/repo.git"},
		{name: "link local metadata", cloneURL: "http://169.254.169.254/latest/meta-data"},
		{name: "localhost name", cloneURL: "https://localhost/forge-metal/repo.git"},
		{name: "userinfo", cloneURL: "https://token:secret@github.com/forge-metal/repo.git"},
		{name: "query", cloneURL: "https://github.com/forge-metal/repo.git?token=secret"},
		{name: "non default port", cloneURL: "https://github.com:8443/forge-metal/repo.git"},
		{name: "single label host", cloneURL: "https://forgejo/forge-metal/repo.git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeImportRepoRequest(ImportRepoRequest{
				CloneURL: tt.cloneURL,
			})
			if err == nil || !strings.Contains(err.Error(), "clone_url") {
				t.Fatalf("normalizeImportRepoRequest(%q) error = %v, want clone_url validation error", tt.cloneURL, err)
			}
		})
	}
}

func TestNormalizeImportRepoRequestAcceptsPublicHTTPCloneURL(t *testing.T) {
	req, err := normalizeImportRepoRequest(ImportRepoRequest{
		CloneURL: " https://github.com/forge-metal/repo.git ",
	})
	if err != nil {
		t.Fatalf("normalizeImportRepoRequest: %v", err)
	}
	if req.CloneURL != "https://github.com/forge-metal/repo.git" {
		t.Fatalf("clone_url: got %q", req.CloneURL)
	}
	if req.FullName != "forge-metal/repo" {
		t.Fatalf("full_name: got %q", req.FullName)
	}
}

func TestNormalizeSubmitRequestRejectsUnsafeRepoURL(t *testing.T) {
	_, err := normalizeSubmitRequest(SubmitRequest{
		Kind:    KindRepoExec,
		Repo:    "forge-metal/repo",
		RepoURL: "http://169.254.169.254/latest/meta-data",
		Ref:     "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "repo_url") {
		t.Fatalf("normalizeSubmitRequest error = %v, want repo_url validation error", err)
	}
}

func TestBuildRepoExecRequestRejectsUnsafeRepoURL(t *testing.T) {
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
		JobID: "55555555-5555-5555-5555-555555555555",
		RepoTarget: RepoTarget{
			Repo:    "forge-metal/repo",
			RepoURL: "file:///tmp/forge-metal-should-not-exist.git",
		},
		Ref: "refs/heads/main",
	}, inspection)
	if err == nil || !strings.Contains(err.Error(), "repo_url") {
		t.Fatalf("BuildRepoExecRequest error = %v, want repo_url validation error", err)
	}
}
