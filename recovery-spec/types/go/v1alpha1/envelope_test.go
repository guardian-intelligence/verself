package v1alpha1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeResourcesRejectsUnknownTopLevelFields(t *testing.T) {
	_, err := DecodeResources([]byte(`apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: gamma-public-dns
spec: {}
unexpected: true
`))
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeResourcesRejectsDuplicateIdentities(t *testing.T) {
	_, err := DecodeResources([]byte(`apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: gamma-public-dns
spec: {}
---
apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: gamma-public-dns
spec: {}
`))
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeResourcesRejectsInvalidName(t *testing.T) {
	_, err := DecodeResources([]byte(`apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: Gamma_Public_DNS
spec: {}
`))
	if err == nil || !strings.Contains(err.Error(), "metadata.name must be a DNS label") {
		t.Fatalf("error = %v", err)
	}
}

func TestResourceSpecHashIsStable(t *testing.T) {
	first, err := DecodeResources([]byte(validResourceYAML()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := DecodeResources([]byte(validResourceYAML()))
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := first[0].SpecHash()
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := second[0].SpecHash()
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("spec hashes differ: %s != %s", firstHash, secondHash)
	}
	if !strings.HasPrefix(firstHash, "sha256:") {
		t.Fatalf("spec hash = %q", firstHash)
	}
}

func TestLoadResourcesAcceptsCoreConformanceFixture(t *testing.T) {
	path := testRunfile("recovery-spec/conformance/core/valid/minimal-dns-resolution.yml")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	resources, err := LoadResources(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("resources = %d", len(resources))
	}
	if resources[0].Identity().Kind != "DNSResolution" {
		t.Fatalf("identity = %s", resources[0].Identity())
	}
}

func TestLoadResourcesRejectsCoreInvalidConformanceFixtures(t *testing.T) {
	pattern := testRunfile("recovery-spec/conformance/core/invalid/*.yml")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no invalid conformance fixtures matched %s", pattern)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := LoadResources(path); err == nil {
				t.Fatalf("%s loaded successfully", path)
			}
		})
	}
}

func validResourceYAML() string {
	return `apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: gamma-public-dns
spec:
  providerRef: cloudflare-main
  records:
    - domainRef: product
      name: "@"
      type: A
      ttl: 1
      valueFrom: environment.ingress.publicIPv4
`
}

func testRunfile(path string) string {
	root := os.Getenv("TEST_SRCDIR")
	if root == "" {
		return filepath.FromSlash(path)
	}
	workspace := os.Getenv("TEST_WORKSPACE")
	if workspace == "" {
		workspace = "_main"
	}
	return filepath.Join(root, workspace, filepath.FromSlash(path))
}
