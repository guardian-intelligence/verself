package main

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
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

func TestExtractToolsRequiresBazelisk(t *testing.T) {
	toolsTar := writeReleaseToolsTar(t, []string{"cargo-about", "cosign", "oras", "release-plz", "syft"})

	_, _, err := extractTools(toolsTar)
	if err == nil {
		t.Fatal("extractTools() error = nil, want missing bazelisk error")
	}
	if !strings.Contains(err.Error(), "bazelisk") {
		t.Fatalf("extractTools() error = %v, want bazelisk detail", err)
	}
}

func TestMergeCommandEnvOverridesHome(t *testing.T) {
	got := mergeCommandEnv([]string{"PATH=/bin", "HOME=/root", "KEEP=1"}, map[string]string{
		"HOME":           "/tmp/release-home",
		"XDG_CACHE_HOME": "/tmp/release-cache",
	})
	want := []string{"PATH=/bin", "KEEP=1", "HOME=/tmp/release-home", "XDG_CACHE_HOME=/tmp/release-cache"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("merged env = %#v, want %#v", got, want)
	}
}

func TestReleaseBazelOptionsUseReleaseCache(t *testing.T) {
	env := map[string]string{"XDG_CACHE_HOME": "/artifacts/releases/work/tool-env/mksk/cache"}
	gotStartup := releaseBazelStartupOptions(env)
	wantStartup := []string{
		"--output_user_root=/artifacts/releases/work/tool-env/mksk/cache/bazel-output",
		"--max_idle_secs=1",
	}
	if strings.Join(gotStartup, "\n") != strings.Join(wantStartup, "\n") {
		t.Fatalf("startup options = %#v", gotStartup)
	}
	got := releaseBazelCommandOptions(env)
	want := []string{
		"--disk_cache=/artifacts/releases/work/tool-env/mksk/cache/bazel-disk",
		"--repository_cache=/artifacts/releases/work/tool-env/mksk/cache/bazel-repo",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("command options = %#v, want %#v", got, want)
	}
}

func TestReleaseRustToolsBinDir(t *testing.T) {
	execroot := t.TempDir()
	bin := filepath.Join(execroot, "external", rulesRustToolsRepo, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"cargo", "rustc"} {
		if err := os.WriteFile(filepath.Join(bin, tool), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := releaseRustToolsBinDir(execroot)
	if err != nil {
		t.Fatalf("releaseRustToolsBinDir() error = %v", err)
	}
	if got != bin {
		t.Fatalf("releaseRustToolsBinDir() = %q, want %q", got, bin)
	}
}

func TestReleaseRustToolsBinDirRequiresRustc(t *testing.T) {
	execroot := t.TempDir()
	bin := filepath.Join(execroot, "external", rulesRustToolsRepo, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "cargo"), nil, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := releaseRustToolsBinDir(execroot)
	if err == nil {
		t.Fatal("releaseRustToolsBinDir() error = nil, want missing rustc error")
	}
	if !strings.Contains(err.Error(), "rustc") {
		t.Fatalf("releaseRustToolsBinDir() error = %v, want rustc detail", err)
	}
}

func TestCommandEnvWithPrependedPath(t *testing.T) {
	got := commandEnvWithPrependedPath(map[string]string{
		"HOME": "/tmp/home",
		"PATH": "/usr/bin",
	}, "/opt/release/bin", "/opt/rust/bin")
	wantPath := strings.Join([]string{"/opt/release/bin", "/opt/rust/bin", "/usr/bin"}, string(os.PathListSeparator))
	if got["PATH"] != wantPath {
		t.Fatalf("PATH = %q, want %q", got["PATH"], wantPath)
	}
	if got["HOME"] != "/tmp/home" {
		t.Fatalf("HOME = %q", got["HOME"])
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	for _, name := range []string{"..", "../x", "/tmp/x"} {
		if _, err := safeJoin("/tmp/root", name); err == nil {
			t.Fatalf("safeJoin accepted %q", name)
		}
	}
}

func writeReleaseToolsTar(t *testing.T, tools []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tools.tar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	if err := writer.WriteHeader(&tar.Header{Name: "bin", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if err := writer.WriteHeader(&tar.Header{Name: "bin/" + tool, Typeflag: tar.TypeReg, Mode: 0o755, Size: 0}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
