package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCloudflareTokenFromGeneratedVars(t *testing.T) {
	root := t.TempDir()
	vars := filepath.Join(root, "ansible-secrets.json")
	writeTestFile(t, vars, `{"cloudflare_api_token":"cf_token"}`)

	token, err := loadCloudflareToken(config{varsFile: vars})
	if err != nil {
		t.Fatal(err)
	}
	if token != "cf_token" {
		t.Fatalf("unexpected token %q", token)
	}
}

func TestLoadCloudflareTokenFromSeedBundle(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, `
version: verself.site-bootstrap.seed.v1
site: gamma
values:
  cloudflare_api_token: scoped_dns_token
`)

	token, err := loadCloudflareToken(config{varsFile: seed})
	if err != nil {
		t.Fatal(err)
	}
	if token != "scoped_dns_token" {
		t.Fatalf("unexpected token %q", token)
	}
}

func TestLoadCloudflareTokenRejectsAmbiguousSources(t *testing.T) {
	_, err := loadCloudflareToken(config{varsFile: "vars.json", tokenFile: "token"})
	if err == nil || !strings.Contains(err.Error(), "exactly one token source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadDesiredUsesInventoryWhenSiteIPIsPlaceholder(t *testing.T) {
	root := t.TempDir()
	ansibleDir := filepath.Join(root, "ansible")
	writeTestFile(t, filepath.Join(root, "sites/gamma/vars.yml"), `
verself_domain: gamma.verself.sh
company_domain: gamma.guardianintelligence.org
bare_metal_public_ipv4: 0.0.0.0
cloudflare_dns_records:
  - { kind: browser_origin, record: "@", zone: product }
`)
	inventory := filepath.Join(root, "sites/gamma/inventory.ini")
	writeTestFile(t, inventory, `
[infra]
vs-gamma-w0 ansible_host=203.0.113.10
`)

	desired, err := loadDesired(ansibleDir, "gamma", inventory)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.records) != 1 {
		t.Fatalf("unexpected desired records: %+v", desired.records)
	}
	if desired.records[0].targetIP != "203.0.113.10" {
		t.Fatalf("target IP came from vars placeholder: %+v", desired.records[0])
	}
}

func TestLoadDesiredUsesHostedZoneForSubdomainSite(t *testing.T) {
	root := t.TempDir()
	ansibleDir := filepath.Join(root, "ansible")
	writeTestFile(t, filepath.Join(root, "sites/gamma/vars.yml"), `
verself_domain: gamma.verself.sh
cloudflare_product_zone: verself.sh
company_domain: gamma.guardianintelligence.org
cloudflare_company_zone: guardianintelligence.org
bare_metal_public_ipv4: 203.0.113.10
cloudflare_dns_records:
  - { kind: public_api_origin, record: deployments.api, zone: product }
  - { kind: browser_origin, record: "@", zone: company }
`)

	desired, err := loadDesired(ansibleDir, "gamma", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.records) != 2 {
		t.Fatalf("unexpected desired records: %+v", desired.records)
	}
	byFQDN := map[string]desiredRecord{}
	for _, record := range desired.records {
		byFQDN[record.fqdn] = record
	}
	deployments := byFQDN["deployments.api.gamma.verself.sh"]
	if deployments.zoneName != "verself.sh" || deployments.record != "deployments.api.gamma" {
		t.Fatalf("deployment record = %+v", deployments)
	}
	company := byFQDN["gamma.guardianintelligence.org"]
	if company.zoneName != "guardianintelligence.org" || company.record != "gamma" {
		t.Fatalf("company record = %+v", company)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
