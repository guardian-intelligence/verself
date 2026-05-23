package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallConfigPreservesUnmanagedRules(t *testing.T) {
	artifact := t.TempDir()
	dest := t.TempDir()
	mustWrite(t, filepath.Join(artifact, "etc", "nftables.conf"), managedHeader+"\ninclude \"/etc/nftables.d/*.nft\"\n")
	mustWrite(t, filepath.Join(artifact, "etc", "nftables.d", "host-firewall.nft"), managedHeader+"\ntable inet verself_host {}\n")
	mustWrite(t, filepath.Join(artifact, "systemd", "verself-firewall.target"), managedHeader+"\n[Unit]\nDescription=firewall\n")
	mustWrite(t, filepath.Join(dest, "etc", "nftables.d", "firecracker.nft"), "# unmanaged\n")
	mustWrite(t, filepath.Join(dest, "etc", "nftables.d", "old-managed.nft"), managedHeader+"\n")

	if err := installConfig(config{artifactRoot: artifact, destRoot: dest, manageSystemd: true}); err != nil {
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

func mustWrite(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
