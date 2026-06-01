package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeExternalDomain(t *testing.T) {
	input := []byte(`# Zitadel configuration.
# Managed by Ansible - do not edit manually.

Port: 8085
ExternalDomain: auth.example.test
ExternalPort: 443
`)
	got, err := normalizeExternalDomain(input, "example.test")
	if err != nil {
		t.Fatalf("normalize external domain: %v", err)
	}
	if !bytes.Contains(got, []byte("ExternalDomain: example.test\n")) {
		t.Fatalf("external domain was not rewritten:\n%s", string(got))
	}
	if bytes.Contains(got, []byte("auth.example.test")) {
		t.Fatalf("legacy auth domain remained:\n%s", string(got))
	}
	if !bytes.Contains(got, []byte("Managed by Nomad")) {
		t.Fatalf("management marker was not updated:\n%s", string(got))
	}
	again, err := normalizeExternalDomain(got, "example.test")
	if err != nil {
		t.Fatalf("normalize twice: %v", err)
	}
	if !bytes.Equal(again, got) {
		t.Fatalf("normalization is not idempotent:\nonce:\n%s\n\ntwice:\n%s", string(got), string(again))
	}
}

func TestNormalizeExternalDomainRequiresKey(t *testing.T) {
	if _, err := normalizeExternalDomain([]byte("Port: 8085\n"), "example.test"); err == nil {
		t.Fatal("expected missing ExternalDomain to fail")
	}
}

func TestRenderDiscoveryHosts(t *testing.T) {
	got := string(renderDiscoveryHosts("example.test"))
	want := "127.0.0.1 localhost\n::1 localhost ip6-localhost ip6-loopback\n127.0.0.1 example.test\n"
	if got != want {
		t.Fatalf("hosts file:\n%s", got)
	}
}

func TestNormalizeSecretFileTrimsTemplateNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "masterkey")
	if err := os.WriteFile(path, []byte("01234567890123456789012345678901\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := normalizeSecretFile(path, os.Getuid(), os.Getgid(), 0o600); err != nil {
		t.Fatalf("normalize secret: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "01234567890123456789012345678901" {
		t.Fatalf("secret file was not trimmed: %q", string(got))
	}
}
