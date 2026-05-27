package main

import (
	"os"
	"path/filepath"
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
