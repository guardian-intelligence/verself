package main

import (
	"archive/tar"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateSubject(t *testing.T) {
	commit := strings.Repeat("a", 40)
	platform := Platform{OS: "linux", Arch: "amd64"}
	tests := []struct {
		name    string
		subject ReleaseSubject
		wantErr bool
	}{
		{
			name: "nightly",
			subject: ReleaseSubject{
				Package:      packageName,
				Version:      "0.2.0-nightly.20260527.1",
				Channel:      ChannelNightly,
				SourceCommit: commit,
				Platform:     platform,
				Flavor:       defaultFlavor,
			},
		},
		{
			name: "rc",
			subject: ReleaseSubject{
				Package:      packageName,
				Version:      "0.2.0-rc.1",
				Channel:      ChannelRC,
				SourceCommit: commit,
				Platform:     platform,
				Flavor:       "glibc-default",
			},
		},
		{
			name: "stable",
			subject: ReleaseSubject{
				Package:      packageName,
				Version:      "0.2.0",
				Channel:      ChannelStable,
				SourceCommit: commit,
				Platform:     platform,
				Flavor:       defaultFlavor,
			},
		},
		{
			name: "stable rejects rc bytes",
			subject: ReleaseSubject{
				Package:      packageName,
				Version:      "0.2.0-rc.1",
				Channel:      ChannelStable,
				SourceCommit: commit,
				Platform:     platform,
				Flavor:       defaultFlavor,
			},
			wantErr: true,
		},
		{
			name: "rc rejects final",
			subject: ReleaseSubject{
				Package:      packageName,
				Version:      "0.2.0",
				Channel:      ChannelRC,
				SourceCommit: commit,
				Platform:     platform,
				Flavor:       defaultFlavor,
			},
			wantErr: true,
		},
		{
			name: "requires flavor",
			subject: ReleaseSubject{
				Package:      packageName,
				Version:      "0.2.0",
				Channel:      ChannelStable,
				SourceCommit: commit,
				Platform:     platform,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubject(tt.subject)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSubject() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestParsePlatform(t *testing.T) {
	got, err := parsePlatform("linux/amd64")
	if err != nil {
		t.Fatalf("parsePlatform() error = %v", err)
	}
	if got.String() != "linux/amd64" {
		t.Fatalf("parsePlatform() = %s", got.String())
	}
	if _, err := parsePlatform("darwin/arm64"); err == nil {
		t.Fatal("parsePlatform(darwin/arm64) error = nil")
	}
}

func TestReleaseMetadataURL(t *testing.T) {
	got := releaseMetadataURL("0.2.0-rc.1")
	want := "https://oci.verself.sh/releases/mksk/0.2.0-rc.1"
	if got != want {
		t.Fatalf("releaseMetadataURL() = %q, want %q", got, want)
	}
}

func TestExtractToolsRequiresBuildTools(t *testing.T) {
	toolsTar := writeReleaseToolsTar(t, []string{"bazelisk", "cargo-about"})
	_, cleanup, err := extractTools(toolsTar, []string{"bazelisk", "cargo-about", "syft"})
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil {
		t.Fatal("extractTools() error = nil, want missing syft error")
	}
	if !strings.Contains(err.Error(), "syft") {
		t.Fatalf("extractTools() error = %v, want syft detail", err)
	}
}

func TestMergeCommandEnvOverridesHome(t *testing.T) {
	got := mergeCommandEnv([]string{"PATH=/bin", "HOME=/home/user", "KEEP=1"}, map[string]string{
		"HOME":           "/tmp/release-home",
		"XDG_CACHE_HOME": "/tmp/release-cache",
	})
	want := []string{"PATH=/bin", "KEEP=1", "HOME=/tmp/release-home", "XDG_CACHE_HOME=/tmp/release-cache"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeCommandEnv() = %#v, want %#v", got, want)
	}
}

func TestReleaseBazelOptionsUseShortLivedServer(t *testing.T) {
	env := map[string]string{"XDG_CACHE_HOME": "/artifacts/releases/work/tool-env/mksk/cache"}
	gotStartup := releaseBazelStartupOptions(env)
	wantStartup := []string{
		"--output_user_root=/artifacts/releases/work/tool-env/mksk/cache/bazel-output",
		"--max_idle_secs=1",
	}
	if !reflect.DeepEqual(gotStartup, wantStartup) {
		t.Fatalf("releaseBazelStartupOptions() = %#v, want %#v", gotStartup, wantStartup)
	}
	gotCommand := releaseBazelCommandOptions(env)
	wantCommand := []string{
		"--disk_cache=/artifacts/releases/work/tool-env/mksk/cache/bazel-disk",
		"--repository_cache=/artifacts/releases/work/tool-env/mksk/cache/bazel-repo",
	}
	if !reflect.DeepEqual(gotCommand, wantCommand) {
		t.Fatalf("releaseBazelCommandOptions() = %#v, want %#v", gotCommand, wantCommand)
	}
}

func TestReleaseRustToolsBinDir(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "external", rulesRustToolsRepo, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "rustc"), []byte("rustc"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := releaseRustToolsBinDir(root)
	if err != nil {
		t.Fatalf("releaseRustToolsBinDir() error = %v", err)
	}
	if got != bin {
		t.Fatalf("releaseRustToolsBinDir() = %q, want %q", got, bin)
	}
}

func TestCommandEnvWithPrependedPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	got := commandEnvWithPrependedPath(map[string]string{
		"HOME": "/tmp/home",
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
	if _, err := safeJoin("/tmp/root", "../escape"); err == nil {
		t.Fatal("safeJoin() error = nil for traversal")
	}
	if _, err := safeJoin("/tmp/root", "/absolute"); err == nil {
		t.Fatal("safeJoin() error = nil for absolute path")
	}
}

func writeReleaseToolsTar(t *testing.T, tools []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tools.tar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(file)
	for _, name := range tools {
		body := []byte("#!/bin/sh\n")
		header := &tar.Header{
			Name: "bin/" + name,
			Mode: 0o755,
			Size: int64(len(body)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
