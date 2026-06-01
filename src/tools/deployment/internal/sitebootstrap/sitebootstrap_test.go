package sitebootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeSeedBundleGeneratesMissingHostSecrets(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, validSeedBundle("gamma"))
	vars := filepath.Join(root, "ansible-secrets.json")
	evidence := filepath.Join(root, "seed-fingerprints.json")

	report, err := MaterializeSeedBundle(MaterializeOptions{
		Site:       "gamma",
		SeedPath:   seed,
		VarsPath:   vars,
		Evidence:   evidence,
		ForceWrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	readJSON(t, vars, &values)
	for key := range generatedSeedKeys {
		if values[key] == "" {
			t.Fatalf("%s was not generated", key)
		}
	}
	body, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "sk_test_gamma") {
		t.Fatalf("evidence leaked a raw secret: %s", body)
	}
	if len(report.Values) != len(fallbackProvidedSeedKeys)+len(generatedSeedKeys) {
		t.Fatalf("unexpected evidence values: %+v", report.Values)
	}
}

func TestValidateSeedBundleRejectsUnknownKeys(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, validSeedBundle("gamma")+"\n  typo_secret: value\n")
	_, err := ValidateSeedBundle("gamma", seed)
	if err == nil || !strings.Contains(err.Error(), "not a declared bootstrap seed key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSeedBundleRejectsLiveStripeForGamma(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, strings.Replace(validSeedBundle("gamma"), "sk_test_gamma", "sk_live_prod", 1))
	_, err := ValidateSeedBundle("gamma", seed)
	if err == nil || !strings.Contains(err.Error(), "live-mode Stripe secret key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMaterializeSeedBundlePreservesExistingGeneratedValues(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, validSeedBundle("gamma"))
	vars := filepath.Join(root, "ansible-secrets.json")
	writeTestFile(t, vars, `{"iam_service_email_identity_hmac_key":"existing-hmac"}`)

	if _, err := MaterializeSeedBundle(MaterializeOptions{
		Site:       "gamma",
		SeedPath:   seed,
		VarsPath:   vars,
		Evidence:   filepath.Join(root, "seed-fingerprints.json"),
		ForceWrite: true,
	}); err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	readJSON(t, vars, &values)
	if values["iam_service_email_identity_hmac_key"] != "existing-hmac" {
		t.Fatalf("expected existing generated value to be preserved, got %q", values["iam_service_email_identity_hmac_key"])
	}
}

func TestMaterializeSeedBundleWritesFilteredRuntimeSeed(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/services/billing-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: billing-service.stripe.secret_key
    site_secret: stripe_secret_key
`)
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, validSeedBundle("gamma"))
	runtimeSeed := filepath.Join(root, "openbao-runtime-seed.json")

	report, err := MaterializeSeedBundle(MaterializeOptions{
		Site:            "gamma",
		SeedPath:        seed,
		VarsPath:        filepath.Join(root, "ansible-secrets.json"),
		RuntimeSeedPath: runtimeSeed,
		Evidence:        filepath.Join(root, "seed-fingerprints.json"),
		RepoRoot:        root,
		ForceWrite:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var bundle RuntimeSeedBundle
	readJSON(t, runtimeSeed, &bundle)
	if bundle.Version != RuntimeSeedBundleVersion || bundle.Site != "gamma" {
		t.Fatalf("runtime seed header = %#v", bundle)
	}
	if got := bundle.Values["stripe_secret_key"]; got != "sk_test_gamma" {
		t.Fatalf("runtime seed stripe_secret_key = %q", got)
	}
	for _, forbidden := range []string{"cloudflare_api_token", "nomad_artifact_getter_s3_secret_access_key", "stripe_publishable_key"} {
		if _, ok := bundle.Values[forbidden]; ok {
			t.Fatalf("runtime seed included non-runtime key %s", forbidden)
		}
	}
	if report.Outputs["openbao_runtime_seed"] != runtimeSeed {
		t.Fatalf("runtime seed output missing from evidence: %#v", report.Outputs)
	}
}

func TestWriteInventory(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "inventory.ini")
	if err := WriteInventory(InventoryOptions{
		Site:       "gamma",
		Host:       "203.0.113.10",
		OutputPath: out,
		ForceWrite: true,
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"[workers]", "vs-gamma-w0 ansible_host=203.0.113.10", "ansible_user=ubuntu"} {
		if !strings.Contains(text, want) {
			t.Fatalf("inventory missing %q:\n%s", want, text)
		}
	}
}

func TestLoadSeedPolicyUsesCatalogAndOwnerLocalDeclarations(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/host/sites/gamma/vars.yml"), `
verself_site: gamma
github_integration_service_github_app_client_id: public-client-id
`)
	writeTestFile(t, filepath.Join(root, "src/services/billing-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: billing-service.stripe.secret_key
    site_secret: stripe_secret_key
`)
	writeTestFile(t, filepath.Join(root, "src/services/iam-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: iam-service.email_identity.hmac_key
    site_secret: iam_service_email_identity_hmac_key
`)
	writeTestFile(t, filepath.Join(root, "src/services/github-integration-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: github-integration-service.github.oauth_client_secret
    site_secret: github_integration_service_github_app_oauth_client_secret
`)
	writeTestFile(t, filepath.Join(root, "src/integrations/catalog/sites/gamma.yml"), `
version: verself.integrations.v1
site: gamma
secret_store_policy: openbao_only
bootstrap_exceptions:
  - key: edge.cloudflare_parent_zone_dns
    provider: cloudflare
    isolation: bootstrap_shared
    credential_keys: [cloudflare_api_token]
    storage_targets: [controller_openbao]
    allowed_uses: [dns]
    reason: parent-zone token
integrations:
  - key: billing.stripe
    provider: stripe
    owner: src/services/billing-service
    purpose: billing
    credentials:
      - key: billing-service.stripe.publishable_key
        source: provider_dashboard
        target: public_config
        site_var: stripe_publishable_key
      - key: billing-service.stripe.test_webhook_endpoint_id
        source: provider_webhook_endpoint
        target: provider_resource_id
        catalog_field: stripe_test_webhook_endpoint_id
`)

	policy, err := loadSeedPolicy(root, "gamma")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"cloudflare_api_token", "stripe_secret_key", "stripe_publishable_key", "stripe_test_webhook_endpoint_id", "github_integration_service_github_app_oauth_client_secret"} {
		if _, ok := policy.keys[key]; !ok {
			t.Fatalf("policy missing %s: %+v", key, policy.keys)
		}
	}
	if _, ok := policy.keys["github_integration_service_github_app_client_id"]; ok {
		t.Fatalf("public site var should not be requested in seed bundle")
	}
	if policy.keys["iam_service_email_identity_hmac_key"].Source != "generated_runtime" {
		t.Fatalf("iam hmac should be generated: %+v", policy.keys["iam_service_email_identity_hmac_key"])
	}
}

func validSeedBundle(site string) string {
	return `version: verself.site-bootstrap.seed.v1
site: ` + site + `
values:
  cloudflare_api_token: cf_gamma
  github_integration_service_github_app_oauth_client_secret: github_oauth_gamma
  github_integration_service_github_app_private_key: github_private_gamma
  github_integration_service_github_app_webhook_secret: github_webhook_gamma
  nomad_artifact_getter_s3_access_key_id: r2_getter_gamma
  nomad_artifact_getter_s3_secret_access_key: r2_getter_secret_gamma
  resend_api_key: re_gamma
  stripe_publishable_key: pk_test_gamma
  stripe_secret_key: sk_test_gamma
  stripe_test_webhook_endpoint_id: we_gamma
  stripe_webhook_secret: whsec_gamma
`
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatal(err)
	}
}
