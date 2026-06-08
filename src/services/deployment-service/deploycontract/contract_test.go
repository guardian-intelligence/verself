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
  - key: cloudflare.account_admin
    provider: cloudflare
    isolation: controller_only
    credential_keys: [cloudflare_account_admin_api_token_a, cloudflare_account_admin_api_token_b]
    storage_targets: [controller_openbao]
    allowed_uses: [global DNS and R2 control-plane provisioning]
    reason: Cloudflare account authority stays controller-only.
integrations:
  - key: edge.cloudflare_dns
    provider: cloudflare
    controller: prod
    owner: src/integrations/cloudflare/control-plane
    purpose: global_dns_reconciliation
  - key: billing.stripe
    provider: stripe
    replacement: keep_provider_native
    owner: src/services/billing-service
    purpose: billing
`)

	report, err := ValidateRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.PostgresFiles != 1 || report.RuntimeSecrets != 0 || report.PublicRoutes != 1 || report.IntegrationFiles != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestValidateRepoRejectsCloudflareDNSControllerOutsideProd(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/integrations/catalog/sites/gamma.yml", `
version: verself.integrations.v1
site: gamma
secret_store_policy: openbao_only
integrations:
  - key: edge.cloudflare_dns
    provider: cloudflare
    controller: gamma
    owner: src/integrations/cloudflare/control-plane
    purpose: global_dns_reconciliation
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "controller must be prod for Cloudflare DNS") {
		t.Fatalf("unexpected error: %v", err)
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
	write(t, root, "src/services/example-service/deploy/runtime-secrets.yml", `
openbao_runtime_secret_declarations:
  - name: example-service.provider.api_key
    external_openbao: true
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

func TestValidateRepoRejectsMissingFirstPartyDeployWorkflow(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/sites/gamma/vars.yml", `
deployment_github_allowed_repositories: guardian-intelligence/verself
deployment_github_allowed_refs: refs/heads/main
deployment_github_allowed_workflow_refs: guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "missing first-party workflow file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoAcceptsExistingFirstPartyDeployWorkflow(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".github/workflows/gamma-deploy.yml", gammaDeployWorkflow())
	write(t, root, "src/sites/gamma/vars.yml", `
deployment_github_allowed_repositories: guardian-intelligence/verself
deployment_github_allowed_refs: refs/heads/main
deployment_github_allowed_workflow_refs: guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main
`)

	if _, err := ValidateRepo(root); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRepoRejectsExternalActionInDeployWorkflow(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".github/workflows/gamma-deploy.yml", strings.Replace(gammaDeployWorkflow(), "guardian-intelligence/verself/.github/actions/checkout@main", "actions/checkout@v4", 1))
	write(t, root, "src/sites/gamma/vars.yml", `
deployment_github_allowed_repositories: guardian-intelligence/verself
deployment_github_allowed_refs: refs/heads/main
deployment_github_allowed_workflow_refs: guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "must not use external action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoRejectsBroadDeployWorkflowPermissions(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".github/workflows/gamma-deploy.yml", strings.Replace(gammaDeployWorkflow(), "contents: read", "contents: write", 1))
	write(t, root, "src/sites/gamma/vars.yml", `
deployment_github_allowed_repositories: guardian-intelligence/verself
deployment_github_allowed_refs: refs/heads/main
deployment_github_allowed_workflow_refs: guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"permissions must grant contents: read", "permissions must not grant contents: write"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in error: %v", want, err)
		}
	}
}

func TestValidateRepoRejectsDeployWorkflowWithoutRepoRefGuard(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".github/workflows/gamma-deploy.yml", strings.Replace(gammaDeployWorkflow(), "    if: github.repository == 'guardian-intelligence/verself' && github.ref == 'refs/heads/main'\n", "", 1))
	write(t, root, "src/sites/gamma/vars.yml", `
deployment_github_allowed_repositories: guardian-intelligence/verself
deployment_github_allowed_refs: refs/heads/main
deployment_github_allowed_workflow_refs: guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "must guard github.repository") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoRejectsPartialGitHubDeployAuth(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/sites/gamma/vars.yml", `
deployment_github_allowed_repositories: guardian-intelligence/verself
deployment_github_allowed_refs: refs/heads/main
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "requires deployment_github_allowed_repositories, deployment_github_allowed_refs, and deployment_github_allowed_workflow_refs together") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoRejectsGitHubDeployWorkflowOutsideAllowedRepoAndRef(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/sites/gamma/vars.yml", `
deployment_github_allowed_repositories: guardian-intelligence/verself
deployment_github_allowed_refs: refs/heads/main
deployment_github_allowed_workflow_refs: attacker/verself/.github/workflows/gamma-deploy.yml@refs/heads/dev
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"repository is not listed", "ref is not listed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in error: %v", want, err)
		}
	}
}

func TestValidateRepoRejectsNestedGitHubDeployWorkflowPath(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/sites/gamma/vars.yml", `
deployment_github_allowed_repositories: guardian-intelligence/verself
deployment_github_allowed_refs: refs/heads/main
deployment_github_allowed_workflow_refs: guardian-intelligence/verself/.github/workflows/nested/gamma-deploy.yml@refs/heads/main
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "directly under .github/workflows") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoAcceptsBootstrapRuntimeSecretContracts(t *testing.T) {
	root := t.TempDir()
	writeBootstrapRuntimeContracts(t, root)

	if _, err := ValidateRepo(root); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRepoRejectsBootstrapNomadJobWithoutRuntimeSecretRender(t *testing.T) {
	root := t.TempDir()
	writeBootstrapRuntimeContracts(t, root)
	write(t, root, "src/integrations/cloudflare/r2-control-plane/nomad.hcl", `job "cloudflare-r2-control-plane" {}`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "must render OpenBao runtime secret cloudflare-r2-control-plane.publisher_token_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoRejectsToolLayerDeploymentEngine(t *testing.T) {
	root := t.TempDir()
	writeBootstrapRuntimeContracts(t, root)
	write(t, root, "src/tools/deployment/deployengine/engine.go", `package deployengine`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "normal deployment runtime code must live under src/services/deployment-service") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoRejectsNomadRuntimeUserMissingFromPreflight(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/example-service/nomad.hcl", `
job "example-service" {
  group "example-service" {
    task "server" {
      user = "example_service"
    }
  }
}
`)
	write(t, root, "src/tools/site-preflight/ansible/roles/base/defaults/main.yml", `
base_nomad_runtime_users: []
`)

	_, err := ValidateRepo(root)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), `missing Nomad runtime user "example_service" required by src/services/example-service/nomad.hcl`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepoAcceptsNomadRuntimeUsersDeclaredInPreflight(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/example-service/nomad.hcl", `
job "example-service" {
  group "example-service" {
    task "setup" {
      user = "root"
    }
    task "server" {
      user = "example_service"
    }
    task "postgres" {
      user = "postgres"
    }
	}
}
`)
	write(t, root, "src/tools/site-preflight/ansible/roles/base/defaults/main.yml", `
base_nomad_runtime_users:
  - name: example_service
    home: /var/lib/example_service
`)

	if _, err := ValidateRepo(root); err != nil {
		t.Fatal(err)
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

func writeBootstrapRuntimeContracts(t *testing.T, root string) {
	t.Helper()
	write(t, root, "src/integrations/cloudflare/r2-control-plane/deploy/runtime-secrets.yml", `
openbao_runtime_secret_declarations:
  - name: cloudflare-r2-control-plane.publisher_token_id
    external_openbao: true
  - name: cloudflare-r2-control-plane.publisher_secret_access_key
    external_openbao: true
`)
	write(t, root, "src/integrations/cloudflare/r2-control-plane/nomad.hcl", `
job "cloudflare-r2-control-plane" {
  group "cloudflare-r2-control-plane" {
    network {
      port "internal_https" {
        host_network = "loopback"
      }
    }
    task "cloudflare-r2-control-plane" {
      config {
        args = [
          "--action=serve",
          "--credential-source=files",
          "--parent-access-key-id-file=$${NOMAD_SECRETS_DIR}/r2-publisher-token-id",
          "--parent-secret-access-key-file=$${NOMAD_SECRETS_DIR}/r2-publisher-secret-access-key",
          "--listen=127.0.0.1:$${NOMAD_PORT_internal_https}",
        ]
      }
      env {
        SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"
      }
      template {
        destination = "secrets/r2-publisher-token-id"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/cloudflare-r2-control-plane.publisher_token_id" }}{{ .Data.data.value }}{{ end }}
		EOT
		      }
      template {
        destination = "secrets/r2-publisher-secret-access-key"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/cloudflare-r2-control-plane.publisher_secret_access_key" }}{{ .Data.data.value }}{{ end }}
		EOT
		      }
      service {
        name = "cloudflare-r2-control-plane-internal-https"
        port = "internal_https"
      }
		    }
		  }
		}
	`)
}

func gammaDeployWorkflow() string {
	return `
name: Gamma Deploy

on:
  push:
    branches:
      - main
  workflow_dispatch:

permissions:
  contents: read
  id-token: write

concurrency:
  group: gamma-deploy
  cancel-in-progress: true

jobs:
  deploy:
    name: Deploy main to gamma
    if: github.repository == 'guardian-intelligence/verself' && github.ref == 'refs/heads/main'
    runs-on: verself-4vcpu-ubuntu-2404
    timeout-minutes: 30
    steps:
      - name: Check out repository
        uses: guardian-intelligence/verself/.github/actions/checkout@main
        with:
          persist-credentials: false

      - name: Bootstrap toolchain
        run: src/tools/dev/bootstrap/bootstrap-linux-amd64

      - name: Deploy
        run: aspect deploy --site=gamma --sha="$GITHUB_SHA"
`
}
