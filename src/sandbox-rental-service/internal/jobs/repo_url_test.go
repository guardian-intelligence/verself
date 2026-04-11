package jobs

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
)

func TestNormalizeImportRepoRequestRejectsUnsafeCloneURLs(t *testing.T) {
	tests := []struct {
		name     string
		cloneURL string
	}{
		{name: "scp style ssh", cloneURL: "git@github.com:forge-metal/repo.git"},
		{name: "file scheme", cloneURL: "file:///tmp/repo.git"},
		{name: "git protocol", cloneURL: "git://github.com/forge-metal/repo.git"},
		{name: "http scheme", cloneURL: "http://github.com/forge-metal/repo.git"},
		{name: "loopback ipv4", cloneURL: "https://127.0.0.1/forge-metal/repo.git"},
		{name: "loopback ipv6", cloneURL: "https://[::1]/forge-metal/repo.git"},
		{name: "private ipv4", cloneURL: "https://10.0.0.1/forge-metal/repo.git"},
		{name: "link local metadata", cloneURL: "https://169.254.169.254/latest/meta-data"},
		{name: "localhost name", cloneURL: "https://localhost/forge-metal/repo.git"},
		{name: "userinfo", cloneURL: "https://token:secret@github.com/forge-metal/repo.git"},
		{name: "query", cloneURL: "https://93.184.216.34/forge-metal/repo.git?token=secret"},
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

func TestNormalizeImportRepoRequestAcceptsPublicHTTPSCloneURL(t *testing.T) {
	req, err := normalizeImportRepoRequest(ImportRepoRequest{
		CloneURL: " https://93.184.216.34/forge-metal/repo.git ",
	})
	if err != nil {
		t.Fatalf("normalizeImportRepoRequest: %v", err)
	}
	if req.CloneURL != "https://93.184.216.34/forge-metal/repo.git" {
		t.Fatalf("clone_url: got %q", req.CloneURL)
	}
	if req.FullName != "forge-metal/repo" {
		t.Fatalf("full_name: got %q", req.FullName)
	}
}

func TestNormalizeImportRepoRequestRejectsProviderHostMismatch(t *testing.T) {
	_, err := normalizeImportRepoRequest(ImportRepoRequest{
		ProviderHost: "github.com",
		CloneURL:     "https://93.184.216.34/forge-metal/repo.git",
	})
	if err == nil || !strings.Contains(err.Error(), "provider_host") {
		t.Fatalf("normalizeImportRepoRequest error = %v, want provider_host mismatch", err)
	}
}

func TestValidateGitCloneURLFieldRejectsPrivateDNSResolution(t *testing.T) {
	resolver := fakeGitDNSResolver{answers: map[string][]net.IPAddr{
		"github.com": {{IP: net.ParseIP("10.0.0.7")}},
	}}

	err := validateGitCloneURLFieldWithResolver(context.Background(), resolver, "clone_url", "https://github.com/forge-metal/repo.git")
	if err == nil || !strings.Contains(err.Error(), "resolved") {
		t.Fatalf("validateGitCloneURLFieldWithResolver error = %v, want resolved private IP rejection", err)
	}
}

func TestValidateGitCloneURLFieldAcceptsPublicDNSResolution(t *testing.T) {
	resolver := fakeGitDNSResolver{answers: map[string][]net.IPAddr{
		"github.com": {{IP: net.ParseIP("93.184.216.34")}},
	}}

	err := validateGitCloneURLFieldWithResolver(context.Background(), resolver, "clone_url", "https://github.com/forge-metal/repo.git")
	if err != nil {
		t.Fatalf("validateGitCloneURLFieldWithResolver: %v", err)
	}
}

func TestNormalizeSubmitRequestRejectsUnsafeRepoURL(t *testing.T) {
	_, err := normalizeSubmitRequest(SubmitRequest{
		Kind:    KindDirect,
		Repo:    "forge-metal/repo",
		RepoURL: "http://169.254.169.254/latest/meta-data",
		Ref:     "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "repo_url") {
		t.Fatalf("normalizeSubmitRequest error = %v, want repo_url validation error", err)
	}
}

type fakeGitDNSResolver struct {
	answers map[string][]net.IPAddr
}

func (r fakeGitDNSResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	return r.answers[host], nil
}

func TestRepoScanBudgetRejectsConcurrentScan(t *testing.T) {
	svc := &Service{RepoScanConcurrency: 1}
	release, err := svc.acquireRepoScanSlot(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	if _, err := svc.acquireRepoScanSlot(context.Background()); err == nil || !errors.Is(err, ErrRepoScanCapacity) {
		t.Fatalf("second acquire error = %v, want ErrRepoScanCapacity", err)
	}
}

func TestRepoScanGitCommandDisablesUnsafeProtocolSources(t *testing.T) {
	cmd, _, cancel := repoScanGitCommand(context.Background(), "--version")
	defer cancel()

	env := envMap(cmd.Env)
	for _, tc := range []struct {
		key  string
		want string
	}{
		{key: "GIT_CONFIG_NOSYSTEM", want: "1"},
		{key: "GIT_CONFIG_GLOBAL", want: "/dev/null"},
		{key: "GIT_TERMINAL_PROMPT", want: "0"},
		{key: "GIT_PROTOCOL_FROM_USER", want: "0"},
		{key: "GIT_ALLOW_PROTOCOL", want: "https"},
	} {
		if got := env[tc.key]; got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.key, got, tc.want)
		}
	}
}

func TestRepoScanGitCommandAllowsFileProtocolOnlyForE2EOverride(t *testing.T) {
	t.Setenv(repoScanAllowFileProtocolForE2E, "1")
	cmd, _, cancel := repoScanGitCommand(context.Background(), "--version")
	defer cancel()

	if got := envMap(cmd.Env)["GIT_ALLOW_PROTOCOL"]; got != "https:file" {
		t.Fatalf("GIT_ALLOW_PROTOCOL: got %q want %q", got, "https:file")
	}
}

func envMap(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if ok {
			out[key] = val
		}
	}
	for _, value := range os.Environ() {
		key, val, ok := strings.Cut(value, "=")
		if ok {
			if _, exists := out[key]; !exists {
				out[key] = val
			}
		}
	}
	return out
}
