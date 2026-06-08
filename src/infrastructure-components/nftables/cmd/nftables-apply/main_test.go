package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallConfigPreservesUnmanagedRules(t *testing.T) {
	artifact := t.TempDir()
	dest := t.TempDir()
	writeRuntimeArtifact(t, artifact)
	mustWrite(t, filepath.Join(dest, "etc", "nftables.d", "firecracker.nft"), "# unmanaged\n")
	mustWrite(t, filepath.Join(dest, "etc", "nftables.d", "old-managed.nft"), managedHeader+"\n")

	cfg := config{
		artifactRoot:    artifact,
		destRoot:        dest,
		hostRuntimeRoot: "/opt/verself/nftables",
		manageSystemd:   true,
		nftBin:          filepath.Join(artifact, "bin", "nft"),
		ldLibraryPath:   filepath.Join(artifact, "lib", "x86_64-linux-gnu"),
	}
	if err := installHostRuntime(&cfg); err != nil {
		t.Fatal(err)
	}
	if err := installConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "etc", "nftables.d", "firecracker.nft")); err != nil {
		t.Fatalf("unmanaged rule was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "etc", "nftables.d", "old-managed.nft")); !os.IsNotExist(err) {
		t.Fatalf("stale managed rule still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "etc", "nftables.d", "host-firewall.nft")); err != nil {
		t.Fatalf("managed rule was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "etc", "systemd", "system", "verself-nftables.service")); err != nil {
		t.Fatalf("boot service was not installed: %v", err)
	}
	unit, err := os.ReadFile(filepath.Join(dest, "etc", "systemd", "system", "verself-nftables.service"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(unit, []byte(artifact)) {
		t.Fatalf("boot service points at transient artifact root:\n%s", unit)
	}
	if !bytes.Contains(unit, []byte("/opt/verself/nftables/current/bin/nftables-apply")) {
		t.Fatalf("boot service does not point at durable runtime:\n%s", unit)
	}
}

func TestInstallConfigSkipsSystemdWhenDisabled(t *testing.T) {
	artifact := t.TempDir()
	dest := t.TempDir()
	mustWrite(t, filepath.Join(artifact, "etc", "nftables.conf"), managedHeader+"\ninclude \"/etc/nftables.d/*.nft\"\n")
	mustWrite(t, filepath.Join(artifact, "etc", "nftables.d", "host-firewall.nft"), managedHeader+"\ntable inet verself_host {}\n")
	mustWrite(t, filepath.Join(artifact, "systemd", "verself-firewall.target"), managedHeader+"\n[Unit]\nDescription=firewall\n")

	if err := installConfig(config{artifactRoot: artifact, destRoot: dest, manageSystemd: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "etc", "nftables.conf")); err != nil {
		t.Fatalf("nftables config was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "etc", "systemd", "system", "verself-nftables.service")); !os.IsNotExist(err) {
		t.Fatalf("boot service should not be installed when systemd management is disabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "etc", "systemd", "system", "verself-firewall.target")); !os.IsNotExist(err) {
		t.Fatalf("firewall target should not be installed when systemd management is disabled: %v", err)
	}
}

func TestValidateResolvesRelativeNftBinForNomadArtifact(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	mustWrite(t, filepath.Join(root, "local", "verself-artifacts", "nftables-runtime", "bin", "nft"), "#!/bin/sh\n")

	cfg := config{
		artifactRoot:    filepath.Join("local", "verself-artifacts", "nftables-runtime"),
		nftBin:          filepath.Join("local", "verself-artifacts", "nftables-runtime", "bin", "nft"),
		destRoot:        "/",
		hostRuntimeRoot: "/opt/verself/nftables",
		manageSystemd:   true,
	}

	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}

	wantNftBin := filepath.Join(root, "local", "verself-artifacts", "nftables-runtime", "bin", "nft")
	if cfg.nftBin != wantNftBin {
		t.Fatalf("nftBin = %s, want %s", cfg.nftBin, wantNftBin)
	}
}

func TestCopyManagedFileRequiresHeader(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	if err := os.WriteFile(src, []byte("not managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := copyManagedFile(fileInstall{Source: src, Dest: filepath.Join(dir, "dest"), Mode: 0o644})
	if err == nil {
		t.Fatal("expected missing managed header error")
	}
}

func TestInstallHostRuntimePublishesDurableCurrentSymlink(t *testing.T) {
	artifact := t.TempDir()
	dest := t.TempDir()
	writeRuntimeArtifact(t, artifact)

	cfg := config{
		artifactRoot:    artifact,
		destRoot:        dest,
		hostRuntimeRoot: "/opt/verself/nftables",
		manageSystemd:   true,
		nftBin:          filepath.Join(artifact, "bin", "nft"),
		ldLibraryPath:   filepath.Join(artifact, "lib", "x86_64-linux-gnu"),
	}
	if err := installHostRuntime(&cfg); err != nil {
		t.Fatal(err)
	}

	current := filepath.Join(dest, "opt", "verself", "nftables", "current")
	target, err := os.Readlink(current)
	if err != nil {
		t.Fatalf("current runtime is not a symlink: %v", err)
	}
	if filepath.IsAbs(target) {
		t.Fatalf("current symlink should be relative inside staged roots: %s", target)
	}
	if _, err := os.Stat(filepath.Join(current, "bin", "nftables-apply")); err != nil {
		t.Fatalf("durable runtime missing nftables-apply: %v", err)
	}
	if cfg.artifactRoot != current {
		t.Fatalf("artifactRoot = %s, want %s", cfg.artifactRoot, current)
	}
	if cfg.serviceArtifactRoot != "/opt/verself/nftables/current" {
		t.Fatalf("serviceArtifactRoot = %s", cfg.serviceArtifactRoot)
	}
}

func TestParseConfigLoadsNftablesFirewallFromGuardianGraph(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "document.json")
	manageSystemd := true
	doc := map[string]any{
		"resources": []map[string]any{
			{
				"apiVersion": "nftables.guardianintelligence.org/v1alpha1",
				"kind":       "NftablesFirewall",
				"metadata": map[string]any{
					"name": "nftables",
				},
				"spec": map[string]any{
					"runtimeArtifact": "bazel-bin/src/infrastructure-components/nftables/nftables-runtime.tar",
					"runtimeRoot":     "/opt/verself/nftables",
					"configPath":      "/etc/nftables.conf",
					"rulesDir":        "/etc/nftables.d",
					"manageSystemd":   manageSystemd,
					"systemd": map[string]any{
						"serviceUnitPath":    "/etc/systemd/system/verself-nftables.service",
						"firewallTargetPath": "/etc/systemd/system/verself-firewall.target",
					},
				},
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

	cfg, err := parseConfig([]string{
		"--repo-root=" + dir,
		"--resource-graph=" + graphPath,
		"--resource-name=nftables",
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.runtimeArtifact != "bazel-bin/src/infrastructure-components/nftables/nftables-runtime.tar" {
		t.Fatalf("runtimeArtifact = %q", cfg.runtimeArtifact)
	}
	if cfg.hostRuntimeRoot != "/opt/verself/nftables" || !cfg.manageSystemd {
		t.Fatalf("unexpected runtime/systemd config: %#v", cfg)
	}
}

func TestInstallRepoRuntimePublishesDurableCurrentSymlink(t *testing.T) {
	repo := t.TempDir()
	dest := t.TempDir()
	artifact := filepath.Join(repo, "bazel-bin/src/infrastructure-components/nftables/nftables-runtime.tar")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, runtimeTar(t), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config{
		repoRoot:        repo,
		runtimeArtifact: "bazel-bin/src/infrastructure-components/nftables/nftables-runtime.tar",
		destRoot:        dest,
		hostRuntimeRoot: "/opt/verself/nftables",
	}
	if err := installRepoRuntime(&cfg); err != nil {
		t.Fatal(err)
	}

	current := filepath.Join(dest, "opt", "verself", "nftables", "current")
	target, err := os.Readlink(current)
	if err != nil {
		t.Fatalf("current runtime is not a symlink: %v", err)
	}
	if filepath.IsAbs(target) {
		t.Fatalf("current symlink should be relative inside staged roots: %s", target)
	}
	if _, err := os.Stat(filepath.Join(current, "bin", "nftables-apply")); err != nil {
		t.Fatalf("durable runtime missing nftables-apply: %v", err)
	}
	if cfg.artifactRoot != current {
		t.Fatalf("artifactRoot = %s, want %s", cfg.artifactRoot, current)
	}
	if cfg.nftBin != filepath.Join(current, "bin", "nft") {
		t.Fatalf("nftBin = %s", cfg.nftBin)
	}
}

func writeRuntimeArtifact(t *testing.T, artifact string) {
	t.Helper()
	mustWrite(t, filepath.Join(artifact, "etc", "nftables.conf"), managedHeader+"\ninclude \"/etc/nftables.d/*.nft\"\n")
	mustWrite(t, filepath.Join(artifact, "etc", "nftables.d", "host-firewall.nft"), managedHeader+"\ntable inet verself_host {}\n")
	mustWrite(t, filepath.Join(artifact, "systemd", "verself-firewall.target"), managedHeader+"\n[Unit]\nDescription=firewall\n")
	mustWrite(t, filepath.Join(artifact, "bin", "nftables-apply"), "#!/bin/sh\n")
	mustWrite(t, filepath.Join(artifact, "bin", "nft"), "#!/bin/sh\n")
	mustWrite(t, filepath.Join(artifact, "lib", "x86_64-linux-gnu", "libnftables.so.1"), "lib\n")
}

func runtimeTar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files := map[string]string{
		"opt/verself/nftables/etc/nftables.conf":                     managedHeader + "\ninclude \"/etc/nftables.d/*.nft\"\n",
		"opt/verself/nftables/etc/nftables.d/host-firewall.nft":      managedHeader + "\ntable inet verself_host {}\n",
		"opt/verself/nftables/systemd/verself-firewall.target":       managedHeader + "\n[Unit]\nDescription=firewall\n",
		"opt/verself/nftables/bin/nftables-apply":                    "#!/bin/sh\n",
		"opt/verself/nftables/bin/nft":                               "#!/bin/sh\n",
		"opt/verself/nftables/lib/x86_64-linux-gnu/libnftables.so.1": "lib\n",
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustWrite(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
