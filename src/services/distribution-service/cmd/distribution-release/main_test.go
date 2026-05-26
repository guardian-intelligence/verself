package main

import (
	"testing"
	"time"
)

func TestResolveMkskVersion(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		channel   string
		explicit  string
		workspace string
		want      string
		wantErr   bool
	}{
		{name: "nightly derived", channel: "nightly", workspace: "0.2.0", want: "0.2.0-nightly.20260526.1"},
		{name: "nightly explicit", channel: "nightly", explicit: "0.2.0-nightly.20260526.2", workspace: "0.1.0", want: "0.2.0-nightly.20260526.2"},
		{name: "rc explicit", channel: "rc", explicit: "0.2.0-rc.1", workspace: "0.1.0", want: "0.2.0-rc.1"},
		{name: "stable explicit", channel: "stable", explicit: "0.2.0", workspace: "0.1.0", want: "0.2.0"},
		{name: "rc rejects final", channel: "rc", explicit: "0.2.0", workspace: "0.1.0", wantErr: true},
		{name: "stable rejects prerelease", channel: "stable", explicit: "0.2.0-rc.1", workspace: "0.1.0", wantErr: true},
		{name: "unknown channel", channel: "beta", explicit: "0.2.0", workspace: "0.1.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveMkskVersion(tt.channel, tt.explicit, tt.workspace, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveMkskVersion() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveMkskVersion() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveMkskVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseWorkspaceVersionUsesWorkspacePackage(t *testing.T) {
	manifest := `
[package]
version = "9.9.9"

[workspace.package]
license = "MIT"
version = "0.2.0"
`
	version, ok := parseWorkspaceVersion(manifest)
	if !ok {
		t.Fatalf("parseWorkspaceVersion() ok = false")
	}
	if version != "0.2.0" {
		t.Fatalf("parseWorkspaceVersion() = %q, want %q", version, "0.2.0")
	}
}

func TestReleaseMetadataURL(t *testing.T) {
	got := releaseMetadataURL("0.2.0-rc.1")
	want := "https://oci.verself.sh/releases/mksk/0.2.0-rc.1"
	if got != want {
		t.Fatalf("releaseMetadataURL() = %q, want %q", got, want)
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../x", "/tmp/x"} {
		if _, err := safeJoin("/tmp/root", name); err == nil {
			t.Fatalf("safeJoin accepted %q", name)
		}
	}
}
