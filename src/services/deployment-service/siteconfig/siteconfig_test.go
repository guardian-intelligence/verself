package siteconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvesSiteTokens(t *testing.T) {
	root := t.TempDir()
	siteDir := filepath.Join(root, "src", "sites", "gamma")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeCloudflareAccountConfig(t, root)
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
github_integration_service_github_app_id: 3918896
github_integration_service_github_app_slug: verself-runner-gamma
github_integration_service_github_app_client_id: Iv23lidNdRYiMW9kU27r
github_integration_service_github_runner_class_prefix: verself-gamma-
deployment_github_allowed_repositories: guardian-intelligence/verself
deployment_github_allowed_refs: refs/heads/main
deployment_github_allowed_workflow_refs: guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main
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
	if tokens["__VERSELF_GITHUB_APP_ID__"] != "3918896" {
		t.Fatalf("github app id token = %q", tokens["__VERSELF_GITHUB_APP_ID__"])
	}
	if tokens["__VERSELF_GITHUB_APP_SETUP_URL__"] != "https://github.com/apps/verself-runner-gamma/installations/new" {
		t.Fatalf("github setup token = %q", tokens["__VERSELF_GITHUB_APP_SETUP_URL__"])
	}
	if tokens["__VERSELF_GITHUB_OAUTH_CLIENT_ID__"] != "Iv23lidNdRYiMW9kU27r" {
		t.Fatalf("github oauth client token = %q", tokens["__VERSELF_GITHUB_OAUTH_CLIENT_ID__"])
	}
	if tokens["__VERSELF_GITHUB_RUNNER_CLASS_PREFIX__"] != "verself-gamma-" {
		t.Fatalf("github runner class prefix token = %q", tokens["__VERSELF_GITHUB_RUNNER_CLASS_PREFIX__"])
	}
	if tokens["__VERSELF_DEPLOY_GITHUB_ALLOWED_WORKFLOW_REFS__"] != "guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main" {
		t.Fatalf("deploy workflow refs token = %q", tokens["__VERSELF_DEPLOY_GITHUB_ALLOWED_WORKFLOW_REFS__"])
	}
	if tokens["__VERSELF_CLOUDFLARE_ACCOUNT_ID__"] != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("cloudflare account id token = %q", tokens["__VERSELF_CLOUDFLARE_ACCOUNT_ID__"])
	}
	if tokens["__VERSELF_CLOUDFLARE_R2_ENDPOINT__"] != "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com" {
		t.Fatalf("cloudflare r2 endpoint token = %q", tokens["__VERSELF_CLOUDFLARE_R2_ENDPOINT__"])
	}
	if tokens["__VERSELF_NOMAD_ARTIFACT_BUCKET__"] != "verself-deployment-artifacts" {
		t.Fatalf("artifact bucket token = %q", tokens["__VERSELF_NOMAD_ARTIFACT_BUCKET__"])
	}
}

func writeCloudflareAccountConfig(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "src", "integrations", "cloudflare", "account.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir cloudflare config: %v", err)
	}
	body := []byte(`{
  "version": "verself.cloudflare.account.v1",
  "control_plane_site": "prod",
  "account_id": "0123456789abcdef0123456789abcdef",
  "r2": {
    "deployment_artifacts_bucket": "verself-deployment-artifacts",
    "recovery_bucket": "verself-recovery"
  }
}
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write cloudflare config: %v", err)
	}
}
