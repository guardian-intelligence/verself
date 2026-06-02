package sitebootstrap

import (
	"context"
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
	if len(report.Values) != len(fallbackProvidedSeedKeys)+len(machineProvisionedSeedKeys)+len(generatedSeedKeys) {
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
		Site:            "gamma",
		SeedPath:        seed,
		VarsPath:        filepath.Join(root, "ansible-secrets.json"),
		RuntimeSeedPath: filepath.Join(root, "openbao-runtime-seed.json"),
		Evidence:        filepath.Join(root, "seed-fingerprints.json"),
		RepoRoot:        root,
		ForceWrite:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, evidence := range report.Values {
		if strings.HasPrefix(evidence.Key, "nomad_artifact_getter_") || strings.HasPrefix(evidence.Key, "cloudflare_r2_control_plane_") {
			t.Fatalf("machine-provisioned key should not be materialized before the site control plane exists: %+v", evidence)
		}
	}
}

func TestMaterializeSeedBundleDoesNotRequireExternalProviderRuntimeSecrets(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/services/billing-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: billing-service.stripe.secret_key
    external_openbao: true
`)
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, bootstrapOnlySeedBundle("gamma"))
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
	for _, evidence := range report.Values {
		if evidence.Key == "stripe_secret_key" || evidence.Key == "billing-service.stripe.secret_key" {
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
	if !strings.Contains(err.Error(), filepath.Join(".verself", "site-bootstrap", "gamma", "ansible-secrets.json")) {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "Cloudflare") || strings.Contains(err.Error(), "R2") {
		t.Fatalf("bootstrap deploy should not require provider authority before the site control plane exists: %v", err)
	}
}

func TestBootstrapDeployChecksR2PublisherBeforeRemoteAccess(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, defaultLocalSecretVarsPath(root, "gamma"), `{"host_generated":"value"}`)
	writeRuntimeSeedFile(t, defaultLocalRuntimeSeedPath(root, "gamma"), RuntimeSeedBundle{
		Version: RuntimeSeedBundleVersion,
		Site:    "gamma",
		Values:  map[string]string{"runtime_secret": "raw_secret"},
	})
	err := RunBootstrapDeploy(context.Background(), BootstrapDeployOptions{
		Site:          "gamma",
		SHA:           "0123456789abcdef0123456789abcdef01234567",
		RepoRoot:      root,
		InventoryPath: filepath.Join(root, "missing-inventory.ini"),
	})
	if err == nil || !strings.Contains(err.Error(), "--r2-control-plane-binary") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "inventory") || strings.Contains(err.Error(), "OpenBao site root key") {
		t.Fatalf("bootstrap deploy reached remote-facing checks before local R2 publisher validation: %v", err)
	}
}

func TestBootstrapR2ControlPlaneCommandUsesR2Publisher(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "cloudflare-r2-control-plane")
	if err := os.WriteFile(binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	publisherEnv := filepath.Join(root, "r2-publisher.env")
	writeTestFile(t, publisherEnv, `CLOUDFLARE_R2_PUBLISHER_TOKEN_ID=token-id
CLOUDFLARE_R2_PUBLISHER_SECRET_ACCESS_KEY=secret
`)
	opts := normalizeBootstrapDeployOptions(BootstrapDeployOptions{
		Site:                 "gamma",
		RepoRoot:             root,
		R2ControlPlaneBinary: binary,
		R2CredentialSource:   "env-file",
		R2CredentialsFile:    publisherEnv,
	})
	cmd, err := startBootstrapR2ControlPlane(context.Background(), opts, "127.0.0.1:18732", "r2bootstrap_token")
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
		"--credential-source=env-file",
		"--credentials-file=" + publisherEnv,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("R2 control-plane args missing %q:\n%s", want, args)
		}
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
}

func TestLocalBootstrapMaterialAcceptsMaterializedSeedOutputs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, defaultLocalSecretVarsPath(root, "gamma"), `{"host_generated":"value"}`)
	writeRuntimeSeedFile(t, defaultLocalRuntimeSeedPath(root, "gamma"), RuntimeSeedBundle{
		Version: RuntimeSeedBundleVersion,
		Site:    "gamma",
		Values:  map[string]string{"runtime_secret": "raw_secret"},
	})
	err := checkLocalBootstrapMaterial(normalizeBootstrapDeployOptions(BootstrapDeployOptions{
		Site:     "gamma",
		RepoRoot: root,
	}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestLocalBootstrapMaterialRejectsWrongSiteWithoutLeakingValues(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, defaultLocalSecretVarsPath(root, "gamma"), `{"host_generated":"value"}`)
	writeRuntimeSeedFile(t, defaultLocalRuntimeSeedPath(root, "gamma"), RuntimeSeedBundle{
		Version: RuntimeSeedBundleVersion,
		Site:    "prod",
		Values:  map[string]string{"runtime_secret": "raw_secret"},
	})
	err := checkLocalBootstrapMaterial(normalizeBootstrapDeployOptions(BootstrapDeployOptions{
		Site:     "gamma",
		RepoRoot: root,
	}))
	if err == nil || !strings.Contains(err.Error(), `not "gamma"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "raw_secret") {
		t.Fatalf("local preflight leaked a runtime seed value: %v", err)
	}
}

func TestLocalBootstrapMaterialRejectsIncompleteRuntimeSeed(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/services/email-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: email-service.provider.api_key
    site_secret: email_provider_api_key
`)
	writeTestFile(t, defaultLocalSecretVarsPath(root, "gamma"), `{"host_generated":"value"}`)
	writeRuntimeSeedFile(t, defaultLocalRuntimeSeedPath(root, "gamma"), RuntimeSeedBundle{
		Version: RuntimeSeedBundleVersion,
		Site:    "gamma",
		Values:  map[string]string{"unrelated_runtime_value": "raw_secret"},
	})
	err := checkLocalBootstrapMaterial(normalizeBootstrapDeployOptions(BootstrapDeployOptions{
		Site:     "gamma",
		RepoRoot: root,
	}))
	if err == nil || !strings.Contains(err.Error(), "email_provider_api_key") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "raw_secret") {
		t.Fatalf("local preflight leaked a runtime seed value: %v", err)
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
    allowed_uses: [child token provisioning]
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
	writeTestFile(t, filepath.Join(root, "src/services/email-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: email-service.provider.api_key
    site_secret: email_provider_api_key
`)
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, validSeedBundle("gamma")+`  email_provider_api_key: provider_gamma
`)
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
	if got := bundle.Values["email_provider_api_key"]; got != "provider_gamma" {
		t.Fatalf("runtime seed email_provider_api_key = %q", got)
	}
	for _, forbidden := range []string{"cloudflare_api_token", "nomad_artifact_getter_s3_secret_access_key"} {
		if _, ok := bundle.Values[forbidden]; ok {
			t.Fatalf("runtime seed included non-runtime key %s", forbidden)
		}
	}
	if report.Outputs["openbao_runtime_seed"] != runtimeSeed {
		t.Fatalf("runtime seed output missing from evidence: %#v", report.Outputs)
	}
}

func TestMaterializeSeedBundleWritesInfrastructureRuntimeSeed(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/infrastructure-components/zitadel/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: infrastructure.provider.api_key
    job_id: zitadel
    site_secret: infrastructure_provider_api_key
`)
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, `version: verself.site-bootstrap.seed.v1
site: gamma
values:
  infrastructure_provider_api_key: provider_gamma
`)
	runtimeSeed := filepath.Join(root, "openbao-runtime-seed.json")

	if _, err := MaterializeSeedBundle(MaterializeOptions{
		Site:            "gamma",
		SeedPath:        seed,
		VarsPath:        filepath.Join(root, "ansible-secrets.json"),
		RuntimeSeedPath: runtimeSeed,
		Evidence:        filepath.Join(root, "seed-fingerprints.json"),
		RepoRoot:        root,
		ForceWrite:      true,
	}); err != nil {
		t.Fatal(err)
	}
	var bundle RuntimeSeedBundle
	readJSON(t, runtimeSeed, &bundle)
	if got := bundle.Values["infrastructure_provider_api_key"]; got != "provider_gamma" {
		t.Fatalf("runtime seed infrastructure_provider_api_key = %q", got)
	}
}

func TestLoadRuntimeSiteSecretKeysScansAllOwnerRoots(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/services/billing-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: billing-service.stripe.secret_key
    site_secret: stripe_secret_key
  - name: billing-service.stripe.webhook_secret
    site_secret: stripe_webhook_secret
`)
	writeTestFile(t, filepath.Join(root, "src/services/email-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: email-service.provider.api_key
    site_secret: email_provider_api_key
`)
	writeTestFile(t, filepath.Join(root, "src/services/github-integration-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: github-integration-service.github.private_key
    site_secret: github_integration_service_github_app_private_key
  - name: github-integration-service.github.webhook_secret
    site_secret: github_integration_service_github_app_webhook_secret
  - name: github-integration-service.github.oauth_client_secret
    site_secret: github_integration_service_github_app_oauth_client_secret
`)
	writeTestFile(t, filepath.Join(root, "src/services/iam-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: iam-service.email_identity.hmac_key
    site_secret: iam_service_email_identity_hmac_key
`)
	writeTestFile(t, filepath.Join(root, "src/infrastructure-components/zitadel/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: auth-control-plane.github_login.oauth_client_secret
    job_id: auth-control-plane
    site_secret: github_integration_service_github_app_oauth_client_secret
  - name: infrastructure.provider.api_key
    job_id: zitadel
    site_secret: infrastructure_provider_api_key
`)

	keys, err := loadRuntimeSiteSecretKeys(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, key := range keys {
		got[key] = true
	}
	for _, key := range []string{
		"github_integration_service_github_app_oauth_client_secret",
		"github_integration_service_github_app_private_key",
		"github_integration_service_github_app_webhook_secret",
		"email_provider_api_key",
		"iam_service_email_identity_hmac_key",
		"infrastructure_provider_api_key",
		"stripe_secret_key",
		"stripe_webhook_secret",
	} {
		if !got[key] {
			t.Fatalf("runtime site-secret keys missing %s from %v", key, keys)
		}
	}
}

func TestMaterializeSeedBundlePreservesExistingRuntimeProviderValues(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/services/email-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: email-service.provider.api_key
    site_secret: email_provider_api_key
`)
	seed := filepath.Join(root, "seed.yml")
	writeTestFile(t, seed, bootstrapOnlySeedBundle("gamma"))
	vars := filepath.Join(root, "ansible-secrets.json")
	writeTestFile(t, vars, `{"email_provider_api_key":"existing-provider-key"}`)
	runtimeSeed := filepath.Join(root, "openbao-runtime-seed.json")

	if _, err := MaterializeSeedBundle(MaterializeOptions{
		Site:            "gamma",
		SeedPath:        seed,
		VarsPath:        vars,
		RuntimeSeedPath: runtimeSeed,
		Evidence:        filepath.Join(root, "seed-fingerprints.json"),
		RepoRoot:        root,
		ForceWrite:      true,
	}); err != nil {
		t.Fatal(err)
	}
	var bundle RuntimeSeedBundle
	readJSON(t, runtimeSeed, &bundle)
	if got := bundle.Values["email_provider_api_key"]; got != "existing-provider-key" {
		t.Fatalf("runtime seed did not preserve existing provider value, got %q", got)
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
    external_openbao: true
`)
	writeTestFile(t, filepath.Join(root, "src/services/iam-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
  - name: iam-service.email_identity.hmac_key
    site_secret: iam_service_email_identity_hmac_key
`)
	writeTestFile(t, filepath.Join(root, "src/services/github-integration-service/deploy/runtime-secrets.yml"), `
openbao_runtime_secret_seed_declarations:
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
    allowed_uses: [child token provisioning]
    reason: account authority
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
	for _, key := range []string{"stripe_secret_key", "stripe_publishable_key", "stripe_test_webhook_endpoint_id", "github_integration_service_github_app_oauth_client_secret"} {
		if _, ok := policy.keys[key]; ok {
			t.Fatalf("policy should not require product provider key %s during bootstrap: %+v", key, policy.keys)
		}
	}
	if policy.keys["cloudflare_api_token"].Source != "machine_provisioned_cloudflare_control_plane" {
		t.Fatalf("Cloudflare DNS child token should be machine provisioned: %+v", policy.keys["cloudflare_api_token"])
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
	if policy.keys["iam_service_email_identity_hmac_key"].Source != "generated_runtime" {
		t.Fatalf("iam hmac should be generated: %+v", policy.keys["iam_service_email_identity_hmac_key"])
	}
	if policy.keys["object_storage_service_r2_admin_access_key_id"].Source != "machine_provisioned_cloudflare_r2_control_plane" {
		t.Fatalf("object storage R2 credential should be machine provisioned: %+v", policy.keys["object_storage_service_r2_admin_access_key_id"])
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
  cloudflare_api_token: cf_dns_gamma
  cloudflare_r2_control_plane_publisher_api_token: cf_r2_publisher_api_gamma
  cloudflare_r2_control_plane_publisher_token_id: cf_r2_publisher_token_gamma
  nomad_artifact_getter_s3_access_key_id: r2_getter_gamma
  nomad_artifact_getter_s3_secret_access_key: r2_getter_secret_gamma
  object_storage_service_r2_admin_access_key_id: r2_admin_gamma
  object_storage_service_r2_admin_secret_access_key: r2_admin_secret_gamma
  object_storage_service_r2_proxy_access_key_id: r2_proxy_gamma
  object_storage_service_r2_proxy_secret_access_key: r2_proxy_secret_gamma
`
}

func bootstrapOnlySeedBundle(site string) string {
	return `version: verself.site-bootstrap.seed.v1
site: ` + site + `
values:
  cloudflare_api_token: cf_dns_gamma
  cloudflare_r2_control_plane_publisher_api_token: cf_r2_publisher_api_gamma
  cloudflare_r2_control_plane_publisher_token_id: cf_r2_publisher_token_gamma
  nomad_artifact_getter_s3_access_key_id: r2_getter_gamma
  nomad_artifact_getter_s3_secret_access_key: r2_getter_secret_gamma
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

func writeRuntimeSeedFile(t *testing.T, path string, seed RuntimeSeedBundle) {
	t.Helper()
	body, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, string(body))
}
