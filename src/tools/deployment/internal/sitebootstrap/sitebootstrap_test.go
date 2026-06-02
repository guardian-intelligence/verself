package sitebootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeSeedBundleWritesExplicitBootstrapVars(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, validSeedBundle("gamma"))
	vars := filepath.Join(root, "bootstrap-vars.json")
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
	for key := range machineProvisionedSeedKeys {
		if values[key] == "" {
			t.Fatalf("%s was not materialized", key)
		}
	}
	body, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "sk_test_gamma") {
		t.Fatalf("evidence leaked a raw secret: %s", body)
	}
	if len(report.Values) != len(machineProvisionedSeedKeys) {
		t.Fatalf("unexpected evidence values: %+v", report.Values)
	}
}

func TestValidateSeedBundleDoesNotRequireMachineProvisionedKeys(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, operatorSeedBundle("gamma"))
	if _, err := ValidateSeedBundle("gamma", seed); err != nil {
		t.Fatal(err)
	}
}

func TestWriteSeedTemplateDoesNotRequestResendRuntimeKey(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "seed.yml")
	if err := WriteSeedTemplate(SeedTemplateOptions{
		Site:       "gamma",
		OutputPath: out,
		ForceWrite: true,
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "resend_api_key") {
		t.Fatalf("seed template includes Resend runtime material:\n%s", body)
	}
}

func TestMaterializeSeedBundleAllowsMissingMachineProvisionedKeys(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, operatorSeedBundle("gamma"))
	report, err := MaterializeSeedBundle(MaterializeOptions{
		Site:       "gamma",
		SeedPath:   seed,
		VarsPath:   filepath.Join(root, "bootstrap-vars.json"),
		Evidence:   filepath.Join(root, "seed-fingerprints.json"),
		RepoRoot:   root,
		ForceWrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, evidence := range report.Values {
		if strings.HasPrefix(evidence.Key, "nomad_artifact_getter_") {
			t.Fatalf("machine-provisioned key should not be materialized before the site control plane exists: %+v", evidence)
		}
	}
}

func TestMaterializeSeedBundleDoesNotRequireExternalProviderRuntimeSecrets(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/services/example-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_declarations:
  - name: example-service.provider.api_key
    external_openbao: true
`)
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, bootstrapOnlySeedBundle("gamma"))

	report, err := MaterializeSeedBundle(MaterializeOptions{
		Site:       "gamma",
		SeedPath:   seed,
		VarsPath:   filepath.Join(root, "bootstrap-vars.json"),
		Evidence:   filepath.Join(root, "seed-fingerprints.json"),
		RepoRoot:   root,
		ForceWrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, evidence := range report.Values {
		if evidence.Key == "example_provider_api_key" || evidence.Key == "example-service.provider.api_key" {
			t.Fatalf("external provider secret should not be materialized into bootstrap seed: %+v", evidence)
		}
	}
}

func TestBootstrapDeployChecksLocalMaterialBeforeRemoteAccess(t *testing.T) {
	root := t.TempDir()
	err := RunBootstrapDeploy(context.Background(), BootstrapDeployOptions{
		Site:          "gamma",
		SHA:           "0123456789abcdef0123456789abcdef01234567",
		RepoRoot:      root,
		InventoryPath: filepath.Join(root, "inventory.ini"),
	})
	if err == nil {
		t.Fatal("expected missing local bootstrap material error")
	}
	if !strings.Contains(err.Error(), filepath.Join(".verself", "site-bootstrap", "gamma", "bootstrap-vars.json")) {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "Cloudflare") || strings.Contains(err.Error(), "R2") {
		t.Fatalf("bootstrap deploy should not require provider authority before the site control plane exists: %v", err)
	}
}

func TestBootstrapDeployChecksR2PublisherBeforeRemoteAccess(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, defaultLocalBootstrapVarsPath(root, "gamma"), `{"host_generated":"value"}`)
	err := RunBootstrapDeploy(context.Background(), BootstrapDeployOptions{
		Site:          "gamma",
		SHA:           "0123456789abcdef0123456789abcdef01234567",
		RepoRoot:      root,
		InventoryPath: filepath.Join(root, "missing-inventory.ini"),
	})
	if err == nil || !strings.Contains(err.Error(), "--cloudflare-control-plane-binary") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "inventory") || strings.Contains(err.Error(), "OpenBao site root key") {
		t.Fatalf("bootstrap deploy reached remote-facing checks before local artifact publishing validation: %v", err)
	}
}

func TestBootstrapR2ControlPlaneCommandUsesInMemoryPublisher(t *testing.T) {
	root := t.TempDir()
	r2Binary := filepath.Join(root, "cloudflare-r2-control-plane")
	if err := os.WriteFile(r2Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	cloudflareBinary := filepath.Join(root, "cloudflare-control-plane")
	if err := os.WriteFile(cloudflareBinary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	opts := normalizeBootstrapDeployOptions(BootstrapDeployOptions{
		Site:                 "gamma",
		RepoRoot:             root,
		R2ControlPlaneBinary: r2Binary,
		CloudflareBinary:     cloudflareBinary,
	})
	publisher := bootstrapPublisherCredential{
		AccessKeyID:     "publisher-token-id",
		SecretAccessKey: "publisher-secret",
		TokenID:         "publisher-token-id",
	}
	cmd, err := startBootstrapR2ControlPlane(context.Background(), opts, "127.0.0.1:18732", "r2bootstrap_token", publisher)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, "\n")
	for _, want := range []string{
		"--action=serve",
		"--site=gamma",
		"--repo-root=" + root,
		"--listen=127.0.0.1:18732",
		"--auth-token-env=" + bootstrapR2AuthTokenEnv,
		"--credential-source=env",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("R2 control-plane args missing %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "--credentials-file") {
		t.Fatalf("R2 bootstrap command still depends on a credential file:\n%s", args)
	}
	for _, forbidden := range []string{"account-admin", "openbao", "BAO_", "VAULT_"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("R2 bootstrap command contains forbidden authority detail %q:\n%s", forbidden, args)
		}
	}
	if strings.Contains(args, "-R") {
		t.Fatalf("R2 bootstrap command still contains temporary artifact tunnel details:\n%s", args)
	}
	if !envContains(cmd.Env, bootstrapR2AuthTokenEnv+"=r2bootstrap_token") {
		t.Fatalf("R2 control-plane command did not carry bootstrap auth token env: %v", cmd.Env)
	}
	if !envContains(cmd.Env, bootstrapR2AccessKeyIDEnv+"=publisher-token-id") {
		t.Fatalf("R2 control-plane command did not carry publisher token id env: %v", cmd.Env)
	}
	if !envContains(cmd.Env, bootstrapR2SecretAccessEnv+"=publisher-secret") {
		t.Fatalf("R2 control-plane command did not carry publisher secret env: %v", cmd.Env)
	}
}

func TestLocalBootstrapMaterialAcceptsMaterializedSeedOutputs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, defaultLocalBootstrapVarsPath(root, "gamma"), `{"host_generated":"value"}`)
	err := checkLocalBootstrapMaterial(normalizeBootstrapDeployOptions(BootstrapDeployOptions{
		Site:     "gamma",
		RepoRoot: root,
	}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapOpenBaoRootKeyPreflightChecksPresenceWithoutPrintingSecrets(t *testing.T) {
	cmd := sshCommand(context.Background(), inventoryTarget{
		Host: "gamma.example.test",
		User: "ubuntu",
		Port: 2222,
	}, openBaoRootKeyPreflightCommand())
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "test -s '/etc/verself/bootstrap/openbao-root.key'") {
		t.Fatalf("preflight command = %q, want root key presence check", args)
	}
	if !strings.Contains(args, "stat -c '%a' '/etc/verself/bootstrap/openbao-root.key'") {
		t.Fatalf("preflight command = %q, want mode check", args)
	}
	if strings.Contains(args, "cat /etc/verself/bootstrap/openbao-root.key") {
		t.Fatalf("preflight command prints root key: %q", args)
	}
	if strings.Contains(args, "set -x") {
		t.Fatalf("preflight command enables shell tracing: %q", args)
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

func TestValidateSeedBundleRejectsControllerOnlyKeys(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/integrations/catalog/sites/gamma.yml"), `
version: verself.integrations.v1
site: gamma
secret_store_policy: openbao_only
bootstrap_exceptions:
  - key: cloudflare.account_admin
    provider: cloudflare
    isolation: controller_only
    credential_keys: [cloudflare_account_admin_api_token_a, cloudflare_account_admin_api_token_b]
    storage_targets: [controller_openbao]
    allowed_uses: [global DNS and R2 control-plane provisioning]
    reason: account authority
integrations: []
`)
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, `version: verself.site-bootstrap.seed.v1
site: gamma
values:
  cloudflare_account_admin_api_token_a: cf_admin_a
`)
	_, err := ValidateSeedBundle("gamma", seed, root)
	if err == nil || !strings.Contains(err.Error(), "controller-only bootstrap authority") {
		t.Fatalf("unexpected error: %v", err)
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
	writeTestFile(t, filepath.Join(root, "src/services/github-integration-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_declarations:
  - name: github-integration-service.github.oauth_client_secret
    external_openbao: true
`)
	writeTestFile(t, filepath.Join(root, "src/integrations/catalog/sites/gamma.yml"), `
version: verself.integrations.v1
site: gamma
secret_store_policy: openbao_only
bootstrap_exceptions:
  - key: cloudflare.account_admin
    provider: cloudflare
    isolation: controller_only
    credential_keys: [cloudflare_account_admin_api_token_a, cloudflare_account_admin_api_token_b]
    storage_targets: [controller_openbao]
    allowed_uses: [global DNS and R2 control-plane provisioning]
    reason: account authority
integrations:
  - key: example.provider
    provider: example
    owner: src/services/example-service
    purpose: example
    credentials:
      - key: example-service.provider.publishable_key
        source: provider_dashboard
        target: public_config
        site_var: example_provider_publishable_key
      - key: example-service.provider.webhook_endpoint_id
        source: provider_webhook_endpoint
        target: provider_resource_id
        catalog_field: example_provider_webhook_endpoint_id
`)

	policy, err := loadSeedPolicy(root, "gamma")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"example_provider_publishable_key", "example_provider_webhook_endpoint_id", "github_integration_service_github_app_oauth_client_secret"} {
		if _, ok := policy.keys[key]; ok {
			t.Fatalf("policy should not require product provider key %s during bootstrap: %+v", key, policy.keys)
		}
	}
	if _, ok := policy.controllerOnly["cloudflare_account_admin_api_token_a"]; !ok {
		t.Fatalf("Cloudflare account admin token A should be controller-only: %+v", policy.controllerOnly)
	}
	if _, ok := policy.controllerOnly["cloudflare_account_admin_api_token_b"]; !ok {
		t.Fatalf("Cloudflare account admin token should be controller-only: %+v", policy.controllerOnly)
	}
	if _, ok := policy.keys["github_integration_service_github_app_client_id"]; ok {
		t.Fatalf("public site var should not be requested in seed bundle")
	}
	if _, ok := policy.keys["iam_service_email_identity_hmac_key"]; ok {
		t.Fatalf("iam hmac should be generated by OpenBao runtime reconciliation: %+v", policy.keys)
	}
	if _, ok := policy.keys["object_storage_service_r2_admin_access_key_id"]; ok {
		t.Fatalf("object storage R2 credential should be projected through OpenBao runtime secrets: %+v", policy.keys)
	}
}

func TestValidateSeedBundleRejectsRuntimeProviderSecrets(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, staleRuntimeProviderSeedBundle("gamma"))

	_, err := ValidateSeedBundle("gamma", seed)
	if err == nil {
		t.Fatal("expected stale runtime provider seed key rejection")
	}
	if !strings.Contains(err.Error(), "is not a declared bootstrap seed key") {
		t.Fatalf("error = %v", err)
	}
}

func operatorSeedBundle(site string) string {
	return `version: verself.site-bootstrap.seed.v1
site: ` + site + `
values: {}
`
}

func validSeedBundle(site string) string {
	return `version: verself.site-bootstrap.seed.v1
site: ` + site + `
values:
  nomad_artifact_getter_s3_access_key_id: r2_getter_gamma
  nomad_artifact_getter_s3_secret_access_key: r2_getter_secret_gamma
`
}

func bootstrapOnlySeedBundle(site string) string {
	return `version: verself.site-bootstrap.seed.v1
site: ` + site + `
values:
  nomad_artifact_getter_s3_access_key_id: r2_getter_gamma
  nomad_artifact_getter_s3_secret_access_key: r2_getter_secret_gamma
`
}

func staleRuntimeProviderSeedBundle(site string) string {
	return `version: verself.site-bootstrap.seed.v1
site: ` + site + `
values:
  object_storage_service_r2_admin_access_key_id: r2_admin_gamma
  object_storage_service_r2_admin_secret_access_key: r2_admin_secret_gamma
  object_storage_service_r2_proxy_access_key_id: r2_proxy_gamma
  object_storage_service_r2_proxy_secret_access_key: r2_proxy_secret_gamma
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

func envContains(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
