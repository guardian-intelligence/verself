package deploycontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRepoAcceptsOwnerLocalContracts(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/billing-service/deploy/postgres.yml", `
postgresql_service_databases:
  - { name: billing, owner: billing }
postgresql_peer_mappings:
  - { system_user: billing, pg_user: billing }
`)
	write(t, root, "src/services/billing-service/deploy/runtime-secrets.yml", `
openbao_runtime_secret_seed_declarations:
  - name: billing-service.stripe.secret_key
    site_secret: stripe_secret_key
`)
	write(t, root, "src/services/billing-service/deploy/public-routes.yml", `
haproxy_public_routes:
  - host: '{{ billing_service_domain }}'
    backend: be_route_product_billing_api_billing_public_api
haproxy_public_apis:
  - key: billing
    host: '{{ billing_service_domain }}'
    path_prefix: /api/v1
`)
	write(t, root, "src/integrations/catalog/sites/gamma.yml", `
version: verself.integrations.v1
site: gamma
secret_store_policy: openbao_only
provider_project:
  engine: stripe_projects
  project_name: verself-gamma
  directory: src/integrations/stripe-projects/sites/gamma
  status: initialized
bootstrap_exceptions:
  - key: edge.cloudflare_parent_zone_dns
    provider: cloudflare
    isolation: bootstrap_shared
    credential_keys: [cloudflare_api_token]
    storage_targets: [controller_openbao, host_credstore]
    allowed_uses: [dns reconciliation]
    reason: Cloudflare DNS tokens are zone scoped.
integrations:
  - key: billing.stripe
    provider: stripe
    replacement: keep_provider_native
    owner: src/services/billing-service
    purpose: billing
    credentials:
      - key: billing-service.stripe.secret_key
        source: manual_provider_dashboard
        target: runtime_secret
        target_store: site_openbao
        openbao_name: billing-service.stripe.secret_key
`)

	report, err := ValidateRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.PostgresFiles != 1 || report.RuntimeSecrets != 1 || report.PublicRoutes != 1 || report.IntegrationFiles != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestValidateRepoRejectsDuplicateDatabaseClaims(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/a-service/deploy/postgres.yml", `
postgresql_service_databases:
  - { name: shared, owner: a_service }
`)
	write(t, root, "src/services/b-service/deploy/postgres.yml", `
postgresql_service_databases:
  - { name: shared, owner: b_service }
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `PostgreSQL database "shared"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoRejectsUnknownDeployShape(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/billing-service/deploy/runtime-secrets.yml", `
openbao_runtime_secret_seed_declarations:
  - name: billing-service.stripe.secret_key
    site_secret: stripe_secret_key
    fallback: ignored
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "field fallback not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoRejectsNonProdSOPSSecrets(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/host/sites/gamma/secrets/host.sops.yml", `cloudflare_api_token: ENC[...]`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "must not use SOPS secret files") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoRejectsBootstrapSharedRuntimeSecrets(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/integrations/catalog/sites/gamma.yml", `
version: verself.integrations.v1
site: gamma
secret_store_policy: openbao_only
integrations:
  - key: billing.stripe
    provider: stripe
    owner: src/services/billing-service
    purpose: billing
    credentials:
      - key: billing-service.stripe.secret_key
        source: bootstrap_session
        target: runtime_secret
        target_store: site_openbao
        openbao_name: billing-service.stripe.secret_key
        isolation: bootstrap_shared
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "bootstrap_shared cannot feed runtime secrets") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
