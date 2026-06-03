package controlplane

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
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
openbao_runtime_secret_declarations:
  - name: billing-service.stripe.secret_key
    consumer_job_ids: [webhook-proxy]
    external_openbao: true
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
	if got := bundle.OpenBao.RuntimeSecrets[0].Source.Kind; got != RuntimeSecretSourceExternal {
		t.Fatalf("source kind = %q", got)
	}
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "sk_test_gamma") {
		t.Fatalf("bundle contains static runtime secret material: %s", body)
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
openbao_runtime_secret_declarations:
  - name: billing-service.stripe.secret_key
    external_openbao: true
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
					Kind: RuntimeSecretSourceExternal,
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
openbao_runtime_secret_declarations:
  - name: deployment-service.substrate_control_plane_marker
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
	if got := roles["deployment-service"].Secrets; len(got) != 1 || got[0] != "deployment-service.substrate_control_plane_marker" {
		t.Fatalf("deployment-service read secrets = %#v", got)
	}
	if got := roles["substrate-control-plane"].WriteSecrets; len(got) != 1 || got[0] != "deployment-service.substrate_control_plane_marker" {
		t.Fatalf("substrate-control-plane write secrets = %#v", got)
	}
	values := substrateControlPlaneProducedSecretValues(bundle)
	if values["deployment-service.substrate_control_plane_marker"] != "verself.substrate-control-plane.applied.v1:gamma" {
		t.Fatalf("marker values = %#v", values)
	}
}

func TestRuntimePolicyForSubstrateControlPlaneIncludesTransitRandom(t *testing.T) {
	policy := runtimePolicy(NomadRole{
		JobID:        ControlPlaneJobID,
		WriteSecrets: []string{"deployment-service.substrate_control_plane_marker"},
	})
	for _, want := range []string{
		`path "sys/policies/acl/*"`,
		`path "auth/jwt-nomad/role/*"`,
		`path "sys/mounts"`,
		`path "sys/mounts/transit"`,
		`path "transit/random/*"`,
		`kv-runtime/data/secret/org/deployment-service.substrate_control_plane_marker`,
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("policy missing %s:\n%s", want, policy)
		}
	}
}

func TestResendRuntimeSecretLifecycleIsEmailServiceOwned(t *testing.T) {
	root := t.TempDir()
	write(t, root, "src/services/email-service/nomad.hcl", `job "email-service" {
  group "email-service" {
    task "email-service" {
      template {
        destination = "secrets/resend-api-key"
        data = <<-EOT
{{ with secret "kv-runtime/data/secret/org/email-service.resend.api_key" }}{{ .Data.data.value }}{{ end }}
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
openbao_runtime_secret_declarations:
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

func TestOpenBaoClientAcceptsConfiguredWorkloadToken(t *testing.T) {
	cfg := ApplyConfig{}.withDefaults()
	cfg.OpenBaoCACert = ""
	cfg.OpenBaoToken = "nomad-workload-token"

	client, err := newBaoClient(cfg)
	if err != nil {
		t.Fatalf("newBaoClient: %v", err)
	}
	if client.token != "nomad-workload-token" {
		t.Fatalf("token = %q", client.token)
	}
}

func TestReconcileRuntimeSecretSkipsExternalOpenBao(t *testing.T) {
	writes := 0
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			reads++
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			writes++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	err := reconcileRuntimeSecret(context.Background(), &baoClient{
		addr:  server.URL,
		token: "token",
		http:  server.Client(),
	}, RuntimeSecret{
		Name: "billing-service.stripe.secret_key",
		Source: RuntimeSecretSource{
			Kind: RuntimeSecretSourceExternal,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("reads = %d, want one OpenBao existence check", reads)
	}
	if writes != 0 {
		t.Fatalf("writes = %d, want no write for external OpenBao source", writes)
	}
}

func TestReconcileGeneratedRuntimeSecretUsesOpenBaoTransit(t *testing.T) {
	random := []byte{0x01, 0x02, 0x03, 0x04}
	transitReads := 0
	kvWrites := 0
	written := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/kv-runtime/data/secret/org/deployment-service.r2_control_plane_token":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/transit/random/4":
			transitReads++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode transit body: %v", err)
			}
			if body["format"] != "base64" {
				t.Fatalf("transit format = %q", body["format"])
			}
			if err := json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]string{
					"random_bytes": base64.StdEncoding.EncodeToString(random),
				},
			}); err != nil {
				t.Fatalf("encode transit response: %v", err)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/kv-runtime/data/secret/org/deployment-service.r2_control_plane_token":
			kvWrites++
			var body struct {
				Data map[string]string `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode KV body: %v", err)
			}
			written = body.Data["value"]
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected OpenBao request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	err := reconcileRuntimeSecret(context.Background(), &baoClient{
		addr:  server.URL,
		token: "token",
		http:  server.Client(),
	}, RuntimeSecret{
		Name: "deployment-service.r2_control_plane_token",
		Source: RuntimeSecretSource{
			Kind:           RuntimeSecretSourceGenerated,
			GeneratedBytes: len(random),
			Encoding:       "base64url",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if transitReads != 1 {
		t.Fatalf("transit reads = %d, want one", transitReads)
	}
	if kvWrites != 1 {
		t.Fatalf("KV writes = %d, want one", kvWrites)
	}
	if want := base64.RawURLEncoding.EncodeToString(random); written != want {
		t.Fatalf("written = %q, want %q", written, want)
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
