package main

import (
	"bytes"
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

func writeRuntimeArtifact(t *testing.T, artifact string) {
	t.Helper()
	mustWrite(t, filepath.Join(artifact, "etc", "nftables.conf"), managedHeader+"\ninclude \"/etc/nftables.d/*.nft\"\n")
	mustWrite(t, filepath.Join(artifact, "etc", "nftables.d", "host-firewall.nft"), managedHeader+"\ntable inet verself_host {}\n")
	mustWrite(t, filepath.Join(artifact, "systemd", "verself-firewall.target"), managedHeader+"\n[Unit]\nDescription=firewall\n")
	mustWrite(t, filepath.Join(artifact, "bin", "nftables-apply"), "#!/bin/sh\n")
	mustWrite(t, filepath.Join(artifact, "bin", "nft"), "#!/bin/sh\n")
	mustWrite(t, filepath.Join(artifact, "lib", "x86_64-linux-gnu", "libnftables.so.1"), "lib\n")
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
