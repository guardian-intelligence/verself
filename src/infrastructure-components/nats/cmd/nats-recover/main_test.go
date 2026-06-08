package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
					"name": "nats",
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

	cfg, err := loadConfig(options{repoRoot: dir, resourceGraph: graphPath, resourceName: "nats"})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.RuntimeArtifact != "bazel-bin/src/infrastructure-components/nats/nats-runtime.tar" {
		t.Fatalf("runtimeArtifact = %q", cfg.RuntimeArtifact)
	}
	if cfg.Authorization.Users[0].SPIFFEID != "spiffe://gamma.verself.sh/svc/notifications-service" {
		t.Fatalf("authorization = %#v", cfg.Authorization)
	}
}

func TestWriteConfigs(t *testing.T) {
	cfg := testConfig(t)
	if err := writeNATSConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeHelperConfig(cfg); err != nil {
		t.Fatal(err)
	}
	conf, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`server_name: "verself-nats"`,
		`store_dir: "` + cfg.JetStreamDir + `"`,
		`user: "spiffe://gamma.verself.sh/svc/notifications-service"`,
	} {
		if !strings.Contains(string(conf), want) {
			t.Fatalf("config omitted %q:\n%s", want, conf)
		}
	}
	helper, err := os.ReadFile(cfg.HelperConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(helper), `agent_address = "/run/spire-agent/sockets/agent.sock"`) {
		t.Fatalf("helper config omitted agent socket:\n%s", helper)
	}
}

func TestInstallRuntimeRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "nats-runtime.tar")
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
	if err == nil || !strings.Contains(err.Error(), "unsafe NATS runtime tar entry") {
		t.Fatalf("extractRuntimeTar error = %v", err)
	}
}

func TestInstallRuntimePromotesCurrentSymlink(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t)
	cfg.RuntimeArtifact = "nats-runtime.tar"
	cfg.RuntimeRoot = filepath.Join(dir, "runtime")
	artifact := filepath.Join(dir, cfg.RuntimeArtifact)
	if err := writeRuntimeTar(artifact); err != nil {
		t.Fatal(err)
	}
	digest, err := installRuntime(dir, cfg)
	if err != nil {
		t.Fatalf("installRuntime: %v", err)
	}
	current, err := os.Readlink(filepath.Join(cfg.RuntimeRoot, "current"))
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
	for _, rel := range []string{"bin/nats-server", "bin/spiffe-helper", "bin/nats-recover"} {
		if _, err := os.Stat(filepath.Join(current, rel)); err != nil {
			t.Fatalf("runtime omitted %s: %v", rel, err)
		}
	}
}

func testConfig(t *testing.T) config {
	t.Helper()
	dir := t.TempDir()
	cfg := config{}
	body, err := json.Marshal(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeRoot = filepath.Join(dir, "runtime")
	cfg.ConfigPath = filepath.Join(dir, "etc", "nats", "nats-server.conf")
	cfg.HelperConfig = filepath.Join(dir, "etc", "nats", "nats-spiffe-helper.conf")
	cfg.DataDir = filepath.Join(dir, "var", "lib", "nats")
	cfg.JetStreamDir = filepath.Join(dir, "var", "lib", "nats", "jetstream")
	cfg.PIDPath = filepath.Join(dir, "var", "lib", "nats", "nats.pid")
	cfg.ReportPath = filepath.Join(dir, "run", "nats", "report.json")
	cfg.SPIFFE.CertDir = filepath.Join(dir, "var", "lib", "nats", "spiffe")
	return cfg
}

func writeRuntimeTar(path string) error {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, name := range []string{"bin/nats-server", "bin/spiffe-helper", "bin/nats-recover"} {
		body := []byte(name + "\n")
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
			return err
		}
		if _, err := tw.Write(body); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, archive.Bytes(), 0o644)
}

func testSpec() map[string]any {
	return map[string]any{
		"runtimeArtifact":  "bazel-bin/src/infrastructure-components/nats/nats-runtime.tar",
		"runtimeRoot":      "/var/lib/nats/runtime",
		"configPath":       "/etc/nats/nats-server.conf",
		"helperConfigPath": "/etc/nats/nats-spiffe-helper.conf",
		"dataDir":          "/var/lib/nats",
		"jetStreamDir":     "/var/lib/nats/jetstream",
		"pidPath":          "/var/lib/nats/nats.pid",
		"reportPath":       "/run/verself/recovery/nats/report.json",
		"user":             "nats",
		"group":            "nats",
		"workloadGroup":    "spire_workload",
		"serverName":       "verself-nats",
		"host":             "127.0.0.1",
		"clientPort":       4222,
		"monitoringPort":   8222,
		"jetstream": map[string]any{
			"maxMemStore":  "256Mb",
			"maxFileStore": "4Gb",
		},
		"spiffe": map[string]any{
			"agentSocket": "/run/spire-agent/sockets/agent.sock",
			"certDir":     "/var/lib/nats/spiffe",
		},
		"authorization": map[string]any{
			"users": []map[string]any{
				{
					"spiffeID":       "spiffe://gamma.verself.sh/svc/notifications-service",
					"publishAllow":   []string{"events.>", "$JS.API.>", "$JS.ACK.>"},
					"subscribeAllow": []string{"_INBOX.>"},
				},
			},
		},
	}
}
