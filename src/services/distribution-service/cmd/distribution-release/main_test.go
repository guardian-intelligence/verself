package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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

func TestWriteReleaseErrorReport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(errorReportDirEnv, dir)
	writeReleaseErrorReport(os.ErrNotExist)
	body, err := os.ReadFile(filepath.Join(dir, "distribution-release-error.txt"))
	if err != nil {
		t.Fatalf("read error report: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"distribution-release failed\n",
		"occurred_at=",
		"error=file does not exist",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("error report missing %q in:\n%s", want, text)
		}
	}
}

func TestAbsolutePath(t *testing.T) {
	got, err := absolutePath("local")
	if err != nil {
		t.Fatalf("absolutePath() error = %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("absolutePath() = %q, want absolute path", got)
	}
	got, err = absolutePath("/tmp/../tmp")
	if err != nil {
		t.Fatalf("absolutePath(abs) error = %v", err)
	}
	if got != "/tmp" {
		t.Fatalf("absolutePath(abs) = %q, want /tmp", got)
	}
}

func TestBazelCommandArgsUseAllocationCacheHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/release-home")
	t.Setenv("XDG_CACHE_HOME", "/tmp/release-home/.cache")
	args := bazelCommandArgs("test", "//src/make-skill:cli_test")
	want := []string{
		"--nohome_rc",
		"--output_user_root=/tmp/release-home/.cache/bazel",
		"--host_jvm_args=-Duser.home=/tmp/release-home",
		"test",
		"--disk_cache=/tmp/release-home/.cache/bazel-disk",
		"--repository_cache=/tmp/release-home/.cache/bazel-repo",
		"//src/make-skill:cli_test",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("bazelCommandArgs() = %#v, want %#v", args, want)
	}
}

func TestReleaseToolEnvIncludesToolBinAndCargoHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/release-home")
	t.Setenv("LD_LIBRARY_PATH", "/host/lib")
	t.Setenv("PATH", "/usr/bin")
	env := releaseToolEnv("/tmp/tools")
	if env["PATH"] != "/tmp/tools/bin:/usr/bin" {
		t.Fatalf("PATH = %q", env["PATH"])
	}
	if env["CARGO"] != "/tmp/tools/bin/cargo" {
		t.Fatalf("CARGO = %q", env["CARGO"])
	}
	if env["RUSTC"] != "/tmp/tools/bin/rustc" {
		t.Fatalf("RUSTC = %q", env["RUSTC"])
	}
	if env["LD_LIBRARY_PATH"] != "/tmp/tools:/host/lib" {
		t.Fatalf("LD_LIBRARY_PATH = %q", env["LD_LIBRARY_PATH"])
	}
	if env["CARGO_HOME"] != "/tmp/release-home/.cargo" {
		t.Fatalf("CARGO_HOME = %q", env["CARGO_HOME"])
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../x", "/tmp/x"} {
		if _, err := safeJoin("/tmp/root", name); err == nil {
			t.Fatalf("safeJoin accepted %q", name)
		}
	}
}

func TestCleanRootUnder(t *testing.T) {
	got, err := cleanRootUnder("/artifacts/releases/mksk/alloc-1", "/artifacts/releases/mksk")
	if err != nil {
		t.Fatalf("cleanRootUnder() error = %v", err)
	}
	if got != "/artifacts/releases/mksk/alloc-1" {
		t.Fatalf("cleanRootUnder() = %q", got)
	}
	for _, path := range []string{
		"artifacts/releases/mksk/alloc-1",
		"/artifacts/releases/mksk",
		"/artifacts/releases/mksk-other/alloc-1",
	} {
		if _, err := cleanRootUnder(path, "/artifacts/releases/mksk"); err == nil {
			t.Fatalf("cleanRootUnder accepted %q", path)
		}
	}
}

func TestValidateDispatchVersion(t *testing.T) {
	if err := validateDispatchVersion("nightly", ""); err != nil {
		t.Fatalf("nightly derived version error = %v", err)
	}
	if err := validateDispatchVersion("rc", "0.2.0-rc.1"); err != nil {
		t.Fatalf("rc version error = %v", err)
	}
	if err := validateDispatchVersion("stable", "0.2.0-rc.1"); err == nil {
		t.Fatalf("stable accepted prerelease")
	}
}

func TestResolveSiteSSHAccess(t *testing.T) {
	root := t.TempDir()
	siteDir := filepath.Join(root, "src", "host", "sites", "prod")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inventory := `[infra]
vs-dev-w0 ansible_host=access.verself.sh verself_recovery_ssh_host=10.66.66.1 verself_recovery_ssh_port=2222 verself_recovery_ssh_user=ubuntu

[all:vars]
ansible_user=ubuntu@prod
`
	if err := os.WriteFile(filepath.Join(siteDir, "inventory.ini"), []byte(inventory), 0o644); err != nil {
		t.Fatal(err)
	}
	access, err := resolveSiteSSHAccess(root, "prod")
	if err != nil {
		t.Fatalf("resolveSiteSSHAccess() error = %v", err)
	}
	if len(access.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(access.Targets))
	}
	if access.Targets[0].Host != "access.verself.sh" || access.Targets[0].User != "ubuntu@prod" || access.Targets[0].Port != 22 {
		t.Fatalf("primary target = %+v", access.Targets[0])
	}
	if access.Targets[1].Host != "10.66.66.1" || access.Targets[1].User != "ubuntu" || access.Targets[1].Port != 2222 {
		t.Fatalf("recovery target = %+v", access.Targets[1])
	}
}

func TestRemoteRefCandidates(t *testing.T) {
	got := remoteRefCandidates("main")
	want := []string{"refs/heads/main", "refs/tags/main^{}", "refs/tags/main"}
	if len(got) != len(want) {
		t.Fatalf("remoteRefCandidates length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("remoteRefCandidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	got = remoteRefCandidates("refs/tags/mksk-v0.2.0")
	if len(got) != 1 || got[0] != "refs/tags/mksk-v0.2.0" {
		t.Fatalf("remoteRefCandidates refs value = %#v", got)
	}
}

func TestDispatchNomadPostsPayload(t *testing.T) {
	var got nomadDispatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/job/distribution-release-mksk/dispatch" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"EvalID":"eval-1","DispatchedJobID":"distribution-release-mksk/dispatch-1"}`))
	}))
	defer server.Close()
	resp, err := dispatchNomad(t.Context(), server.URL, "distribution-release-mksk", []byte(`{"package":"mksk"}`), map[string]string{"channel": "nightly"}, "mksk-nightly")
	if err != nil {
		t.Fatalf("dispatchNomad() error = %v", err)
	}
	if resp.DispatchedJobID != "distribution-release-mksk/dispatch-1" {
		t.Fatalf("dispatched job id = %s", resp.DispatchedJobID)
	}
	if string(got.Payload) != `{"package":"mksk"}` {
		t.Fatalf("payload = %s base64=%s", string(got.Payload), base64.StdEncoding.EncodeToString(got.Payload))
	}
	if got.Meta["channel"] != "nightly" || got.IdPrefixTemplate != "mksk-nightly" {
		t.Fatalf("dispatch request = %+v", got)
	}
}
