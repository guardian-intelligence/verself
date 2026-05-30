package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSiteInventoryUsesInfraOutputsWhenPresent(t *testing.T) {
	got := renderSiteInventory(
		[]siteHost{{Alias: "vs-gamma-w0", Host: "203.0.113.10"}},
		[]siteHost{{Alias: "vs-gamma-i0", Host: "203.0.113.20"}},
	)
	for _, want := range []string{
		"[workers]\nvs-gamma-w0 ansible_host=203.0.113.10",
		"[infra]\nvs-gamma-i0 ansible_host=203.0.113.20",
		"ansible_user=ubuntu",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("inventory missing %q:\n%s", want, got)
		}
	}
}

func TestParseTofuOutputsRequiresAtLeastOneHost(t *testing.T) {
	_, err := parseTofuOutputs([]byte(`{
		"worker_ips": {"value": []},
		"infra_ips": {"value": []}
	}`))
	if err == nil {
		t.Fatal("expected empty host outputs to fail")
	}
}

func TestParseTofuOutputsReadsWorkerAndInfraIPs(t *testing.T) {
	got, err := parseTofuOutputs([]byte(`{
		"worker_ips": {"value": ["203.0.113.10"]},
		"infra_ips": {"value": ["203.0.113.20"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.WorkerIPs) != 1 || got.WorkerIPs[0] != "203.0.113.10" {
		t.Fatalf("unexpected worker IPs: %#v", got.WorkerIPs)
	}
	if len(got.InfraIPs) != 1 || got.InfraIPs[0] != "203.0.113.20" {
		t.Fatalf("unexpected infra IPs: %#v", got.InfraIPs)
	}
}

func TestNormalizeSiteSecretNameRejectsPathSeparators(t *testing.T) {
	if _, err := normalizeSiteSecretName("../postgresql_admin_password"); err == nil {
		t.Fatal("expected path separator secret name to fail")
	}
	if got, err := normalizeSiteSecretName("postgresql_admin_password"); err != nil || got != "postgresql_admin_password" {
		t.Fatalf("unexpected valid secret result: got=%q err=%v", got, err)
	}
}

func TestSiteValidateCanRequireInventory(t *testing.T) {
	root := t.TempDir()
	site := "gamma"
	siteRoot := filepath.Join(root, "src", "host", "sites", site)
	if err := os.MkdirAll(siteRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(siteRoot, "vars.yml"), "---\nverself_site: gamma\n")
	writeTestFile(t, filepath.Join(siteRoot, "site.json"), `{"artifact_delivery":{"bucket":"nomad-artifacts","key_prefix":"sha256","getter_source_prefix":"s3::https://example.invalid/nomad-artifacts","checksum_algorithm":"sha256","public":false},"nomad_addr":"http://127.0.0.1:4646"}`)
	writeTestFile(t, filepath.Join(siteRoot, "provisioning.tfvars.json"), `{"cluster_name":"gamma","worker_count":1,"infra_count":0}`)
	catalogRoot := filepath.Join(root, "src", "integrations", "catalog", "sites")
	if err := os.MkdirAll(catalogRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(catalogRoot, site+".yml"), "---\nintegrations: []\n")

	if err := cmdSiteValidate([]string{"--repo-root=" + root, "--site=" + site}); err != nil {
		t.Fatalf("validate without inventory: %v", err)
	}
	err := cmdSiteValidate([]string{"--repo-root=" + root, "--site=" + site, "--require-inventory"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("validate with missing inventory err = %v, want not found", err)
	}

	writeTestFile(t, filepath.Join(siteRoot, "inventory.ini"), "[infra]\nvs-gamma-w0 ansible_host=203.0.113.10\n")
	if err := cmdSiteValidate([]string{"--repo-root=" + root, "--site=" + site, "--require-inventory"}); err != nil {
		t.Fatalf("validate with inventory: %v", err)
	}
}

func TestResolveDiscoveryCanaryInputsUsesDogfoodOrgFallback(t *testing.T) {
	slug, orgID, err := resolveDiscoveryCanaryInputs("", "", discoveryCanarySiteVars{
		ServiceDiscoveryCanaryOrgSlug: " guardian-intelligence ",
		DogfoodOrgID:                  " org_B7HWGKW0SH7G4EXW9XT8TCT60C ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if slug != "guardian-intelligence" {
		t.Fatalf("slug = %q, want guardian-intelligence", slug)
	}
	if orgID != "org_B7HWGKW0SH7G4EXW9XT8TCT60C" {
		t.Fatalf("orgID = %q, want dogfood org", orgID)
	}
}

func TestResolveDiscoveryCanaryInputsRejectsInvalidDogfoodOrgFallback(t *testing.T) {
	_, _, err := resolveDiscoveryCanaryInputs("guardian-intelligence", "", discoveryCanarySiteVars{
		DogfoodOrgID: "org_invalid",
	})
	if err == nil || !strings.Contains(err.Error(), "authz org id must match") {
		t.Fatalf("err = %v, want invalid authz org id", err)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
