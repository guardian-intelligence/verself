package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNPMPackageMetadataURLEscapesScopedPackage(t *testing.T) {
	got, err := npmPackageMetadataURL("https://registry.npmjs.org/", "@verself/sdk")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://registry.npmjs.org/@verself%2Fsdk"
	if got != want {
		t.Fatalf("metadata URL mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestBuildNPMProbeRowsCurrentSDKIsBlockedByPrivateFlag(t *testing.T) {
	pkg := packageJSON{
		Name:    "@verself/sdk",
		Version: "0.1.0",
		Private: true,
	}
	pkg.Repository = json.RawMessage(`{"url":"https://github.com/guardian-intelligence/verself-sh"}`)
	rows, blockers := buildNPMProbeRows(pkg, "package.json", defaultNPMRegistry, defaultGitHubRepository, npmRegistryProbe{
		PackageURL: "https://registry.npmjs.org/%40verself%2Fsdk",
	})
	if blockers != 1 {
		t.Fatalf("expected 1 blocker, got %d", blockers)
	}
	if !hasReleaseProbeRow(rows, "publishable_private_flag", "blocked") {
		t.Fatalf("missing private flag blocker: %#v", rows)
	}
	if !hasReleaseProbeRow(rows, "npm_registry_version", "ok") {
		t.Fatalf("missing unpublished version row: %#v", rows)
	}
}

func TestPackageRepositoryURLAcceptsStringOrObject(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "string", raw: `"https://github.com/guardian-intelligence/verself-sh"`, want: "https://github.com/guardian-intelligence/verself-sh"},
		{name: "object", raw: `{"type":"git","url":"https://github.com/guardian-intelligence/verself-sh.git"}`, want: "https://github.com/guardian-intelligence/verself-sh.git"},
		{name: "null", raw: `null`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := packageRepositoryURL(json.RawMessage(tt.raw))
			if got != tt.want {
				t.Fatalf("repository URL mismatch\nwant: %q\n got: %q", tt.want, got)
			}
		})
	}
}

func TestBuildNPMProbeRowsBlocksMissingTrustedPublisherRepository(t *testing.T) {
	pkg := packageJSON{
		Name:    "@verself/sdk",
		Version: "0.1.0",
	}
	rows, blockers := buildNPMProbeRows(pkg, "package.json", defaultNPMRegistry, defaultGitHubRepository, npmRegistryProbe{
		PackageURL: "https://registry.npmjs.org/%40verself%2Fsdk",
	})
	if blockers != 1 {
		t.Fatalf("expected 1 blocker, got %d: %#v", blockers, rows)
	}
	if !hasReleaseProbeRow(rows, "trusted_publisher_repository", "blocked") {
		t.Fatalf("missing trusted publisher repository blocker: %#v", rows)
	}
}

func TestNormalizeGitHubOwnerRepo(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "https", raw: "https://github.com/guardian-intelligence/verself-sh", want: "guardian-intelligence/verself-sh"},
		{name: "git https", raw: "git+https://github.com/guardian-intelligence/verself-sh.git", want: "guardian-intelligence/verself-sh"},
		{name: "ssh", raw: "git@github.com:guardian-intelligence/verself-sh.git", want: "guardian-intelligence/verself-sh"},
		{name: "not github", raw: "https://example.com/guardian-intelligence/verself-sh", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeGitHubOwnerRepo(tt.raw)
			if got != tt.want {
				t.Fatalf("normalized repo mismatch\nwant: %q\n got: %q", tt.want, got)
			}
		})
	}
}

func hasReleaseProbeRow(rows []releaseProbeRow, check, status string) bool {
	for _, row := range rows {
		if row.Check == check && row.Status == status {
			if strings.TrimSpace(row.Detail) == "" {
				return false
			}
			return true
		}
	}
	return false
}
