package controlplane

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBundleBuildsNomadRuntimeRoles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/billing-service/nomad.hcl", `job "billing" {
  group "billing" {
    task "billing" {
      template {
        data = <<-EOT
STRIPE_SECRET_KEY={{ with secret "kv-runtime/data/secret/org/billing-service.stripe.secret_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }
    }
  }
}`)
	write(t, root, "src/infrastructure-components/webhook-proxy/nomad.hcl", `job "webhook-proxy" {
  group "webhook-proxy" {
    task "webhook-proxy" {
      template {
        data = <<-EOT
STRIPE_SECRET_KEY={{ with secret "kv-runtime/data/secret/org/billing-service.stripe.secret_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }
    }
  }
}`)
	write(t, root, "src/services/billing-service/deploy/runtime-secrets.yml", `
openbao_runtime_secret_seed_declarations:
  - name: billing-service.stripe.secret_key
    consumer_job_ids: [webhook-proxy]
    site_secret: stripe_secret_key
`)
	write(t, root, "src/services/billing-service/deploy/postgres.yml", `
postgresql_service_databases:
  - { name: billing, owner: billing }
postgresql_peer_mappings:
  - { system_user: billing, pg_user: billing }
`)
	bundle, err := LoadBundle(root, "gamma", []Component{{
		Component: "billing",
		JobID:     "billing",
		JobSpec:   filepath.Join(root, "src/services/billing-service/nomad.hcl"),
	}, {
		Component: "webhook_proxy",
		JobID:     "webhook-proxy",
		JobSpec:   filepath.Join(root, "src/infrastructure-components/webhook-proxy/nomad.hcl"),
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
	if len(bundle.OpenBao.NomadRoles) != 2 || bundle.OpenBao.NomadRoles[0].Name != "billing-runtime" || bundle.OpenBao.NomadRoles[1].Name != "webhook-proxy-runtime" {
		t.Fatalf("roles = %#v", bundle.OpenBao.NomadRoles)
	}
	if len(bundle.OpenBao.NomadRoles[1].Secrets) != 1 || bundle.OpenBao.NomadRoles[1].Secrets[0] != "billing-service.stripe.secret_key" {
		t.Fatalf("consumer role secrets = %#v", bundle.OpenBao.NomadRoles[1].Secrets)
	}
	if len(bundle.Postgres.Databases) != 1 || bundle.Postgres.Databases[0].Name != "billing" {
		t.Fatalf("databases = %#v", bundle.Postgres.Databases)
	}
}

func TestLoadBundleRejectsUndeclaredNomadRuntimeSecretReference(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/billing-service/nomad.hcl", `job "billing" {
  group "billing" {
    task "billing" {
      template {
        data = <<-EOT
STRIPE_SECRET_KEY={{ with secret "kv-runtime/data/secret/org/billing-service.stripe.secret_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }
    }
  }
}`)
	write(t, root, "src/services/billing-service/deploy/postgres.yml", `
postgresql_service_databases:
  - { name: billing, owner: billing }
postgresql_peer_mappings:
  - { system_user: billing, pg_user: billing }
`)
	_, err := LoadBundle(root, "gamma", []Component{{
		Component: "billing",
		JobID:     "billing",
		JobSpec:   filepath.Join(root, "src/services/billing-service/nomad.hcl"),
	}})
	if err == nil || !strings.Contains(err.Error(), `references undeclared OpenBao runtime secret "billing-service.stripe.secret_key"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadBundleRejectsUndeclaredNomadRuntimeSecretConsumer(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/billing-service/nomad.hcl", `job "billing" {}`)
	write(t, root, "src/infrastructure-components/webhook-proxy/nomad.hcl", `job "webhook-proxy" {
  group "webhook-proxy" {
    task "webhook-proxy" {
      template {
        data = <<-EOT
STRIPE_SECRET_KEY={{ with secret "kv-runtime/data/secret/org/billing-service.stripe.secret_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }
    }
  }
}`)
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
	_, err := LoadBundle(root, "gamma", []Component{{
		Component: "billing",
		JobID:     "billing",
		JobSpec:   filepath.Join(root, "src/services/billing-service/nomad.hcl"),
	}, {
		Component: "webhook_proxy",
		JobID:     "webhook-proxy",
		JobSpec:   filepath.Join(root, "src/infrastructure-components/webhook-proxy/nomad.hcl"),
	}})
	if err == nil || !strings.Contains(err.Error(), "nor declared in consumer_job_ids") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundleSHA256IgnoresDeployAttemptIdentity(t *testing.T) {
	bundle := Bundle{
		SchemaVersion: BundleSchemaVersion,
		Site:          "gamma",
		OpenBao: OpenBaoBundle{
			RuntimeSecrets: []RuntimeSecret{{
				Name:      "billing-service.stripe.secret_key",
				OwnerPath: "src/services/billing-service/deploy/runtime-secrets.yml",
				Component: "billing",
				JobID:     "billing",
				Source: RuntimeSecretSource{
					Kind:          RuntimeSecretSourceSiteSecret,
					SiteSecretKey: "stripe_secret_key",
				},
			}},
		},
	}
	first, err := BundleSHA256(bundle)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BundleSHA256(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("bundle digest changed without declaration changes: %s != %s", first, second)
	}
}

func TestProducedRuntimeSecretGrantsWriterAndStableMarker(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/deployment-service/nomad.hcl", `job "deployment-service" {}`)
	write(t, root, "src/infrastructure-components/substrate-control-plane/nomad.hcl", `job "substrate-control-plane" {}`)
	write(t, root, "src/services/deployment-service/deploy/runtime-secrets.yml", `
openbao_runtime_secret_seed_declarations:
  - name: deployment-service.site_seed_import_marker
    produced_by_job: substrate-control-plane
`)
	bundle, err := LoadBundle(root, "gamma", []Component{{
		Component: "deployment_service",
		JobID:     "deployment-service",
		JobSpec:   filepath.Join(root, "src/services/deployment-service/nomad.hcl"),
	}, {
		Component: "substrate_control_plane",
		JobID:     "substrate-control-plane",
		JobSpec:   filepath.Join(root, "src/infrastructure-components/substrate-control-plane/nomad.hcl"),
	}})
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(bundle.OpenBao.RuntimeSecrets) != 1 {
		t.Fatalf("runtime secrets = %#v", bundle.OpenBao.RuntimeSecrets)
	}
	secret := bundle.OpenBao.RuntimeSecrets[0]
	if secret.Source.Kind != RuntimeSecretSourceProduced || secret.Source.ProducedByJob != ControlPlaneJobID {
		t.Fatalf("source = %#v", secret.Source)
	}
	roles := map[string]NomadRole{}
	for _, role := range bundle.OpenBao.NomadRoles {
		roles[role.JobID] = role
	}
	if got := roles["deployment-service"].Secrets; len(got) != 1 || got[0] != "deployment-service.site_seed_import_marker" {
		t.Fatalf("deployment-service read secrets = %#v", got)
	}
	if got := roles["substrate-control-plane"].WriteSecrets; len(got) != 1 || got[0] != "deployment-service.site_seed_import_marker" {
		t.Fatalf("substrate-control-plane write secrets = %#v", got)
	}
	values := substrateControlPlaneProducedSecretValues(bundle)
	if values["deployment-service.site_seed_import_marker"] != "verself.substrate-control-plane.applied.v1:gamma" {
		t.Fatalf("marker values = %#v", values)
	}
}

func TestResendRuntimeSecretLifecycleIsEmailServiceOwned(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/email-service/nomad.hcl", `job "email-service" {
  group "email-service" {
    task "email-service" {
      template {
        data = <<-EOT
EMAIL_SERVICE_RESEND_API_KEY={{ with secret "kv-runtime/data/secret/org/email-service.resend.api_key" }}{{ .Data.data.value }}{{ end }}
EOT
      }
    }
  }
}`)
	write(t, root, "src/services/email-service/resend-keys.nomad.hcl", `job "email-service-resend-keys" {}`)
	write(t, root, "src/infrastructure-components/zitadel/nomad.hcl", `job "zitadel" {
  group "zitadel" {
    task "zitadel" {
      template {
        data = <<-EOT
SMTP_PASSWORD={{ with secret "kv-runtime/data/secret/org/zitadel.smtp.password" }}{{ .Data.data.value }}{{ end }}
EOT
      }
    }
  }
}`)
	write(t, root, "src/services/email-service/deploy/runtime-secrets.yml", `
openbao_runtime_secret_seed_declarations:
  - name: email-service.resend.full_access_api_key
    job_id: email-service-resend-keys
    external_openbao: true
  - name: email-service.resend.api_key
    job_id: email-service
    produced_by_job: email-service-resend-keys
  - name: email-service.resend.key_metadata
    job_id: email-service-resend-keys
    produced_by_job: email-service-resend-keys
  - name: zitadel.smtp.password
    job_id: email-service-resend-keys
    consumer_job_ids: [zitadel]
    produced_by_job: email-service-resend-keys
`)
	bundle, err := LoadBundle(root, "gamma", []Component{{
		Component: "email_service",
		JobID:     "email-service",
		JobSpec:   filepath.Join(root, "src/services/email-service/nomad.hcl"),
	}, {
		Component: "email_service_resend_keys",
		JobID:     "email-service-resend-keys",
		JobSpec:   filepath.Join(root, "src/services/email-service/resend-keys.nomad.hcl"),
	}, {
		Component: "zitadel",
		JobID:     "zitadel",
		JobSpec:   filepath.Join(root, "src/infrastructure-components/zitadel/nomad.hcl"),
	}})
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	secretByName := map[string]RuntimeSecret{}
	for _, secret := range bundle.OpenBao.RuntimeSecrets {
		secretByName[secret.Name] = secret
	}
	if secretByName["email-service.resend.full_access_api_key"].Source.Kind != RuntimeSecretSourceExternal {
		t.Fatalf("full-access source = %#v", secretByName["email-service.resend.full_access_api_key"].Source)
	}
	roles := map[string]NomadRole{}
	for _, role := range bundle.OpenBao.NomadRoles {
		roles[role.JobID] = role
	}
	if got := roles["email-service"].Secrets; len(got) != 1 || got[0] != "email-service.resend.api_key" {
		t.Fatalf("email-service read secrets = %#v", got)
	}
	if got := roles["zitadel"].Secrets; len(got) != 1 || got[0] != "zitadel.smtp.password" {
		t.Fatalf("zitadel read secrets = %#v", got)
	}
	keyManager := roles["email-service-resend-keys"]
	for _, want := range []string{
		"email-service.resend.full_access_api_key",
		"email-service.resend.key_metadata",
		"zitadel.smtp.password",
	} {
		if !containsString(keyManager.Secrets, want) {
			t.Fatalf("key-manager read secrets = %#v, missing %s", keyManager.Secrets, want)
		}
	}
	for _, want := range []string{
		"email-service.resend.api_key",
		"email-service.resend.key_metadata",
		"zitadel.smtp.password",
	} {
		if !containsString(keyManager.WriteSecrets, want) {
			t.Fatalf("key-manager write secrets = %#v, missing %s", keyManager.WriteSecrets, want)
		}
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

func TestOpenBaoClientIgnoresManualBootstrapTokenEnv(t *testing.T) {
	t.Setenv("BAO_TOKEN", "manual-token")
	t.Setenv("VAULT_TOKEN", "")
	cfg := ApplyConfig{}.withDefaults()
	cfg.OpenBaoCACert = ""

	if _, err := newBaoClient(cfg); err == nil || !strings.Contains(err.Error(), "VAULT_TOKEN") {
		t.Fatalf("expected VAULT_TOKEN requirement, got %v", err)
	}
}

func TestOpenBaoClientAcceptsNomadWorkloadToken(t *testing.T) {
	t.Setenv("BAO_TOKEN", "")
	t.Setenv("VAULT_TOKEN", "nomad-workload-token")
	cfg := ApplyConfig{}.withDefaults()
	cfg.OpenBaoCACert = ""

	client, err := newBaoClient(cfg)
	if err != nil {
		t.Fatalf("newBaoClient: %v", err)
	}
	if client.token != "nomad-workload-token" {
		t.Fatalf("token = %q", client.token)
	}
}

func TestReconcileRuntimeSecretSkipsMissingSiteSecret(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			writes++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	imported, err := reconcileRuntimeSecret(context.Background(), &baoClient{
		addr:  server.URL,
		token: "token",
		http:  server.Client(),
	}, RuntimeSecret{
		Name: "billing-service.stripe.secret_key",
		Source: RuntimeSecretSource{
			Kind:          RuntimeSecretSourceSiteSecret,
			SiteSecretKey: "stripe_secret_key",
		},
	}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if imported {
		t.Fatal("missing site secret was reported as imported")
	}
	if writes != 0 {
		t.Fatalf("writes = %d, want no write for absent site secret", writes)
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
