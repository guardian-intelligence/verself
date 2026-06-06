package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigFromGuardianGraph(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "document.json")
	doc := map[string]any{
		"resources": []map[string]any{
			{
				"apiVersion": apiVersion,
				"kind":       kind,
				"metadata": map[string]any{
					"name": "nomad-observer",
				},
				"spec": testSpec(),
			},
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(graphPath, body, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(graphOptions{repoRoot: dir, resourceGraph: graphPath, resourceName: "nomad-observer"})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.runtimeArtifact != "bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer.tar" {
		t.Fatalf("runtimeArtifact = %q", cfg.runtimeArtifact)
	}
	if cfg.clickhouseUser != "nomad_observer" {
		t.Fatalf("clickhouseUser = %q", cfg.clickhouseUser)
	}
}

func TestInstallRuntimeRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "nomad-observer.tar")
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o755, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("nope")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, archive.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	err := extractRuntimeTar(artifact, filepath.Join(t.TempDir(), "release"))
	if err == nil || !strings.Contains(err.Error(), "unsafe Nomad Observer runtime tar entry") {
		t.Fatalf("extractRuntimeTar error = %v", err)
	}
}

func TestInstallRuntimePromotesCurrentSymlink(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t)
	cfg.runtimeArtifact = "nomad-observer.tar"
	cfg.runtimeRoot = filepath.Join(dir, "runtime")
	artifact := filepath.Join(dir, cfg.runtimeArtifact)
	if err := writeRuntimeTar(artifact); err != nil {
		t.Fatal(err)
	}
	digest, err := installRuntime(dir, cfg)
	if err != nil {
		t.Fatalf("installRuntime: %v", err)
	}
	current, err := os.Readlink(filepath.Join(cfg.runtimeRoot, "current"))
	if err != nil {
		t.Fatalf("read current symlink: %v", err)
	}
	if !strings.Contains(current, strings.ReplaceAll(digest, ":", "-")) {
		t.Fatalf("current symlink = %q, want release for %s", current, digest)
	}
	info, err := os.Stat(current)
	if err != nil {
		t.Fatalf("stat current release: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("current release mode = %#o, want 0755", got)
	}
	if _, err := os.Stat(filepath.Join(current, "bin/nomad-observer")); err != nil {
		t.Fatalf("runtime omitted bin/nomad-observer: %v", err)
	}
}

func testConfig(t *testing.T) config {
	t.Helper()
	body, err := json.Marshal(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	var spec crdSpec
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}
	cfg := config{
		runtimeArtifact:       spec.RuntimeArtifact,
		runtimeRoot:           spec.RuntimeRoot,
		dataDir:               spec.DataDir,
		reportPath:            spec.ReportPath,
		user:                  spec.User,
		group:                 spec.Group,
		supplementaryGroups:   spec.SupplementaryGroups,
		nomadAddr:             spec.Nomad.Address,
		namespace:             spec.Nomad.Namespace,
		otlpEndpoint:          spec.OTel.ExporterEndpoint,
		clickhouseAddr:        spec.ClickHouse.Address,
		clickhouseUser:        spec.ClickHouse.User,
		clickhouseCACertPath:  spec.ClickHouse.CACertPath,
		spiffeSocket:          spec.SPIFFE.EndpointSocket,
		fleetSnapshotInterval: time.Duration(spec.Fleet.SnapshotIntervalSeconds) * time.Second,
	}
	return cfg
}

func writeRuntimeTar(path string) error {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	body := []byte("nomad observer\n")
	if err := tw.WriteHeader(&tar.Header{Name: "bin/nomad-observer", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
		return err
	}
	if _, err := tw.Write(body); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, archive.Bytes(), 0o644)
}

func testSpec() map[string]any {
	return map[string]any{
		"runtimeArtifact": "bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer.tar",
		"runtimeRoot":     "/var/lib/nomad-observer/runtime",
		"dataDir":         "/var/lib/nomad-observer",
		"graphPath":       "/run/verself/recovery/nomad-observer/document.json",
		"reportPath":      "/run/verself/recovery/nomad-observer/report.json",
		"user":            "nomad_observer",
		"group":           "nomad_observer",
		"supplementaryGroups": []string{
			"spire_workload",
		},
		"nomad": map[string]any{
			"address":   "http://127.0.0.1:4646",
			"namespace": "default",
		},
		"otel": map[string]any{
			"exporterEndpoint": "http://127.0.0.1:4317",
		},
		"fleet": map[string]any{
			"snapshotIntervalSeconds": 30,
		},
		"clickhouse": map[string]any{
			"address":    "127.0.0.1:9440",
			"user":       "nomad_observer",
			"caCertPath": "/etc/verself/clickhouse/server-ca.pem",
		},
		"spiffe": map[string]any{
			"endpointSocket": "unix:///run/spire-agent/sockets/agent.sock",
		},
	}
}
