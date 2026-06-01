package controlplane

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	if got := bundle.OpenBao.RuntimeSecrets[0].Source.SiteSecretKey; got != "stripe_secret_key" {
		t.Fatalf("site secret key = %q", got)
	}
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "sk_test_gamma") {
		t.Fatalf("bundle contains site secret material: %s", body)
	}
	if len(bundle.OpenBao.NomadRoles) != 1 || bundle.OpenBao.NomadRoles[0].Name != "billing-runtime" {
		t.Fatalf("roles = %#v", bundle.OpenBao.NomadRoles)
	}
	if len(bundle.Postgres.Databases) != 1 || bundle.Postgres.Databases[0].Name != "billing" {
		t.Fatalf("databases = %#v", bundle.Postgres.Databases)
	}
}

func TestLoadRuntimeSeedRejectsWrongSite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "openbao-runtime-seed.json")
	writeBytes(t, root, "openbao-runtime-seed.json", []byte(`{
  "version": "verself.openbao-runtime-seed.v1",
  "site": "prod",
  "values": {"stripe_secret_key": "sk_test"}
}
`))
	_, err := loadRuntimeSeed(ApplyConfig{RuntimeSeedPath: path}, Bundle{Site: "gamma"})
	if err == nil || !strings.Contains(err.Error(), "does not match bundle site") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeSeedAcceptsMissingFile(t *testing.T) {
	seed, err := loadRuntimeSeed(ApplyConfig{RuntimeSeedPath: filepath.Join(t.TempDir(), "missing.json")}, Bundle{Site: "gamma"})
	if err != nil {
		t.Fatal(err)
	}
	if seed.Loaded || len(seed.Values) != 0 {
		t.Fatalf("missing seed = %#v", seed)
	}
}

func TestReadBundleFileAcceptsGzipJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bundle.json.gz")
	var body bytes.Buffer
	zw := gzip.NewWriter(&body)
	if _, err := zw.Write([]byte(`{"schema_version":"verself.substrate-control-plane.v1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readBundleFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"schema_version":"verself.substrate-control-plane.v1"}` {
		t.Fatalf("decoded = %s", got)
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
