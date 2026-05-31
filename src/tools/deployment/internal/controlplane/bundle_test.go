package controlplane

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBundleBuildsNomadRuntimeRoles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/billing-service/nomad.hcl", `job "billing" {}`)
	write(t, root, "src/services/billing-service/deploy/runtime-secrets.yml", `
openbao_runtime_secret_seed_declarations:
  - name: billing-service.stripe.secret_key
    site_secret: stripe_secret_key
`)
	write(t, root, "src/services/billing-service/deploy/postgres.yml", `
postgresql_service_databases:
  - { name: billing, owner: billing }
postgresql_peer_mappings:
  - { system_user: billing, pg_user: billing }
`)
	values := map[string]string{"stripe_secret_key": "sk_test_gamma"}
	body, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	writeBytes(t, root, ".verself/site-bootstrap/gamma/ansible-secrets.json", body)

	bundle, err := LoadBundle(root, "gamma", "sha", "run", []Component{{
		Component: "billing",
		JobID:     "billing",
		JobSpec:   filepath.Join(root, "src/services/billing-service/nomad.hcl"),
	}})
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(bundle.OpenBao.RuntimeSecrets) != 1 {
		t.Fatalf("runtime secrets = %d", len(bundle.OpenBao.RuntimeSecrets))
	}
	if got := bundle.OpenBao.RuntimeSecrets[0].SiteSecret.Value; got != "sk_test_gamma" {
		t.Fatalf("site secret value = %q", got)
	}
	if len(bundle.OpenBao.NomadRoles) != 1 || bundle.OpenBao.NomadRoles[0].Name != "billing-runtime" {
		t.Fatalf("roles = %#v", bundle.OpenBao.NomadRoles)
	}
	if len(bundle.Postgres.Databases) != 1 || bundle.Postgres.Databases[0].Name != "billing" {
		t.Fatalf("databases = %#v", bundle.Postgres.Databases)
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	writeBytes(t, root, rel, []byte(body))
}

func writeBytes(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}
