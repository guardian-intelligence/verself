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

func TestCompileResourcesProducesStableHashAcrossDocumentOrder(t *testing.T) {
	first, err := DecodeResources([]byte(twoResourceYAML("gamma-public-dns", "cloudflare-main")))
	if err != nil {
		t.Fatal(err)
	}
	second, err := DecodeResources([]byte(twoResourceYAML("cloudflare-main", "gamma-public-dns")))
	if err != nil {
		t.Fatal(err)
	}
	firstCompiled, err := CompileResources(first)
	if err != nil {
		t.Fatal(err)
	}
	secondCompiled, err := CompileResources(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstCompiled.SpecHash != secondCompiled.SpecHash {
		t.Fatalf("spec hashes differ: %s != %s", firstCompiled.SpecHash, secondCompiled.SpecHash)
	}
	if len(firstCompiled.Resources) != 2 {
		t.Fatalf("compiled resources = %d", len(firstCompiled.Resources))
	}
	if !strings.HasPrefix(firstCompiled.Resources[0].SpecHash, "sha256:") {
		t.Fatalf("resource spec hash = %q", firstCompiled.Resources[0].SpecHash)
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

func TestDecodeReportsValidatesReportEnvelope(t *testing.T) {
	reports, err := DecodeReports([]byte(`apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolutionReport
metadata:
  name: gamma-public-dns
status:
  observedGeneration: 1
  specHash: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
  conditions:
    - type: Accepted
      status: "True"
      reason: Valid
      observedGeneration: 1
      lastTransitionTime: "2026-06-05T00:00:00Z"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Identity().Kind != "DNSResolutionReport" {
		t.Fatalf("reports = %#v", reports)
	}
}

func TestReportStatusValidatesConditions(t *testing.T) {
	status := ReportStatus{
		ObservedGeneration: 1,
		SpecHash:           "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Conditions: []Condition{
			{
				Type:               "Accepted",
				Status:             ConditionStatusTrue,
				Reason:             "Valid",
				ObservedGeneration: 1,
				LastTransitionTime: "2026-06-05T00:00:00Z",
			},
		},
	}
	if err := status.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateLocalRefs(t *testing.T) {
	if err := ValidateProviderRef("cloudflare-main"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSecretRef("BadSecret"); err == nil {
		t.Fatal("uppercase secretRef accepted")
	}
}

func twoResourceYAML(first, second string) string {
	docs := map[string]string{
		"gamma-public-dns": `apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: gamma-public-dns
  generation: 1
spec:
  providerRef: cloudflare-main
  records:
    - domainRef: product
      name: "@"
      type: A
      ttl: 1
      valueFrom: environment.ingress.publicIPv4
`,
		"cloudflare-main": `apiVersion: guardian.verself.sh/v1alpha1
kind: ProviderBinding
metadata:
  name: cloudflare-main
spec:
  provider: cloudflare
  authorityRef: cloudflare-account-admin
  providerConfig:
    apiVersion: provider.cloudflare.guardian.verself.sh/v1alpha1
    kind: CloudflareProviderConfig
    spec:
      accountID: 0123456789abcdef0123456789abcdef
`,
	}
	return docs[first] + "---\n" + docs[second]
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
