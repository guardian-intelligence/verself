package main

import (
	"bytes"
	"testing"
)

func TestNormalizeExternalDomain(t *testing.T) {
	input := []byte(`# Zitadel configuration.
# Managed by Ansible - do not edit manually.

Port: 8085
ExternalDomain: auth.verself.sh
ExternalPort: 443
`)
	got, err := normalizeExternalDomain(input, "verself.sh")
	if err != nil {
		t.Fatalf("normalize external domain: %v", err)
	}
	if !bytes.Contains(got, []byte("ExternalDomain: verself.sh\n")) {
		t.Fatalf("external domain was not rewritten:\n%s", string(got))
	}
	if bytes.Contains(got, []byte("auth.verself.sh")) {
		t.Fatalf("legacy auth domain remained:\n%s", string(got))
	}
	if !bytes.Contains(got, []byte("Managed by Nomad")) {
		t.Fatalf("management marker was not updated:\n%s", string(got))
	}
	again, err := normalizeExternalDomain(got, "verself.sh")
	if err != nil {
		t.Fatalf("normalize twice: %v", err)
	}
	if !bytes.Equal(again, got) {
		t.Fatalf("normalization is not idempotent:\nonce:\n%s\n\ntwice:\n%s", string(got), string(again))
	}
}

func TestNormalizeExternalDomainRequiresKey(t *testing.T) {
	if _, err := normalizeExternalDomain([]byte("Port: 8085\n"), "verself.sh"); err == nil {
		t.Fatal("expected missing ExternalDomain to fail")
	}
}

func TestRenderDiscoveryHosts(t *testing.T) {
	got := string(renderDiscoveryHosts("verself.sh"))
	want := "127.0.0.1 localhost\n::1 localhost ip6-localhost ip6-loopback\n127.0.0.1 verself.sh\n"
	if got != want {
		t.Fatalf("hosts file:\n%s", got)
	}
}
