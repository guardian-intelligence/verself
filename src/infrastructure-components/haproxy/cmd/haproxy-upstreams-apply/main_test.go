package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestApplyOnceOnlyWritesNomadUpstreams(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "rendered.cfg")
	dest := filepath.Join(dir, "nomad-upstreams.cfg")
	staticConfig := filepath.Join(dir, "haproxy.cfg")
	staticBefore := []byte("frontend fe_https\n  use_backend be_static\n")

	if err := os.WriteFile(source, []byte("global\n\nbackend be_app\n  server app 127.0.0.1:8080\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(staticConfig, staticBefore, 0o600); err != nil {
		t.Fatalf("write static config: %v", err)
	}

	changed, err := applyOnce(config{
		source:         source,
		dest:           dest,
		group:          "",
		haproxyUser:    "",
		haproxyBin:     "/bin/true",
		haproxyConfigs: stringList{staticConfig, dest},
		reloadUnit:     "",
	})
	if err != nil {
		t.Fatalf("apply once: %v", err)
	}
	if !changed {
		t.Fatal("applyOnce reported no change")
	}

	gotDest, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(gotDest) != "global\n\nbackend be_app\n  server app 127.0.0.1:8080\n" {
		t.Fatalf("dest content mismatch:\n%s", string(gotDest))
	}

	gotStatic, err := os.ReadFile(staticConfig)
	if err != nil {
		t.Fatalf("read static config: %v", err)
	}
	if string(gotStatic) != string(staticBefore) {
		t.Fatalf("static config changed:\n%s", string(gotStatic))
	}
}

func TestValidateHAProxyResolvesRelativeLibraryPathBeforeChangingDir(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	out := filepath.Join(root, "ld-library-path")
	t.Setenv("HAPROXY_LD_OUT", out)

	haproxyBin := filepath.Join(root, "local", "bin", "haproxy")
	if err := os.MkdirAll(filepath.Dir(haproxyBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(haproxyBin, []byte("#!/bin/sh\nprintf '%s' \"$LD_LIBRARY_PATH\" > \"$HAPROXY_LD_OUT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := validateHAProxy(config{
		haproxyBin:           filepath.Join("local", "bin", "haproxy"),
		haproxyConfigs:       stringList{"/etc/haproxy/haproxy.cfg"},
		haproxyLDLibraryPath: filepath.Join("local", "lib", "haproxy"),
		haproxyUser:          "",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "local", "lib", "haproxy")
	if !strings.Contains(string(got), want) {
		t.Fatalf("LD_LIBRARY_PATH = %q, want %q", string(got), want)
	}
}

func TestSignalPIDsFromFileAllowsMissingAndEmptyPidFiles(t *testing.T) {
	if err := signalPIDsFromFile(filepath.Join(t.TempDir(), "missing.pid"), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	empty := filepath.Join(t.TempDir(), "empty.pid")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signalPIDsFromFile(empty, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
}

func TestSignalPIDsFromFileRejectsInvalidPid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pid")
	if err := os.WriteFile(path, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signalPIDsFromFile(path, syscall.SIGTERM); err == nil {
		t.Fatal("expected invalid pid to fail")
	}
}
