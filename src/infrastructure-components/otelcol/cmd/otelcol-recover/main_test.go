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
					"name": "otelcol",
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

	cfg, err := loadConfig(options{repoRoot: dir, resourceGraph: graphPath, resourceName: "otelcol"})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.RuntimeArtifact != "bazel-bin/src/infrastructure-components/otelcol/otelcol-runtime.tar" {
		t.Fatalf("runtimeArtifact = %q", cfg.RuntimeArtifact)
	}
	if cfg.ConfigPath != "/etc/otelcol/current/config/config.yaml" {
		t.Fatalf("configPath = %q", cfg.ConfigPath)
	}
}

func TestExtractTarRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "otelcol.tar")
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
	err := extractTar(artifact, filepath.Join(t.TempDir(), "release"))
	if err == nil || !strings.Contains(err.Error(), "unsafe artifact tar entry") {
		t.Fatalf("extractTar error = %v", err)
	}
}

func TestInstallArtifactPromotesCurrentSymlink(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "otelcol-runtime.tar")
	if err := writeTar(artifact, map[string][]byte{
		"bin/otelcol-contrib": []byte("collector\n"),
		"bin/spiffe-helper":   []byte("helper\n"),
	}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "runtime")
	digest, err := installArtifact(dir, "otelcol-runtime.tar", root, []string{"bin/otelcol-contrib", "bin/spiffe-helper"})
	if err != nil {
		t.Fatalf("installArtifact: %v", err)
	}
	current, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("read current symlink: %v", err)
	}
	if !strings.Contains(current, strings.ReplaceAll(digest, ":", "-")) {
		t.Fatalf("current symlink = %q, want release for %s", current, digest)
	}
	if _, err := os.Stat(filepath.Join(current, "bin/otelcol-contrib")); err != nil {
		t.Fatalf("runtime omitted bin/otelcol-contrib: %v", err)
	}
}

func writeTar(path string, files map[string][]byte) error {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for name, body := range files {
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
		"runtimeArtifact":     "bazel-bin/src/infrastructure-components/otelcol/otelcol-runtime.tar",
		"configArtifact":      "bazel-bin/src/infrastructure-components/otelcol/otelcol-config.tar",
		"runtimeRoot":         "/var/lib/otelcol/runtime",
		"configRoot":          "/etc/otelcol",
		"configPath":          "/etc/otelcol/current/config/config.yaml",
		"helperConfigPath":    "/etc/otelcol/current/config/clickhouse-spiffe-helper.conf",
		"dataDir":             "/var/lib/otelcol",
		"storageDir":          "/var/lib/otelcol/storage",
		"clickhouseSpiffeDir": "/var/lib/otelcol/clickhouse-spiffe",
		"reportPath":          "/run/verself/recovery/otelcol/report.json",
		"user":                "otelcol",
		"group":               "otelcol",
		"supplementaryGroups": []string{
			"spire_workload",
			"adm",
			"systemd-journal",
		},
	}
}
