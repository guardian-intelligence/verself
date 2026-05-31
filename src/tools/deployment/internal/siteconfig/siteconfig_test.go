package siteconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvesSiteTokens(t *testing.T) {
	root := t.TempDir()
	siteDir := filepath.Join(root, "src", "host", "sites", "gamma")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	vars := []byte(`---
verself_site: gamma
verself_domain: gamma.verself.sh
company_domain: gamma.guardianintelligence.org
verself_installation_id: inst_gamma
spire_trust_domain: gamma.verself.sh
zitadel_domain: "{{ verself_domain }}"
billing_service_subdomain: billing.api
billing_service_domain: "{{ billing_service_subdomain }}.{{ verself_domain }}"
resend_subdomain: notify
resend_domain: "{{ resend_subdomain }}.{{ verself_domain }}"
`)
	if err := os.WriteFile(filepath.Join(siteDir, "vars.yml"), vars, 0o644); err != nil {
		t.Fatalf("write vars: %v", err)
	}
	model, err := Load(root, "gamma")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if model.Domains["billing_service"] != "billing.api.gamma.verself.sh" {
		t.Fatalf("billing domain = %q", model.Domains["billing_service"])
	}
	tokens := model.TokenMap()
	if tokens["__VERSELF_AUTH_ISSUER_URL__"] != "https://gamma.verself.sh" {
		t.Fatalf("issuer token = %q", tokens["__VERSELF_AUTH_ISSUER_URL__"])
	}
	if tokens["__VERSELF_BILLING_SERVICE_BASE_URL__"] != "https://billing.api.gamma.verself.sh" {
		t.Fatalf("billing base token = %q", tokens["__VERSELF_BILLING_SERVICE_BASE_URL__"])
	}
	if tokens["__VERSELF_EMAIL_FROM_ADDRESS__"] != "noreply@notify.gamma.verself.sh" {
		t.Fatalf("email token = %q", tokens["__VERSELF_EMAIL_FROM_ADDRESS__"])
	}
}
