package sitebootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/verself/deployment-service/deployengine"
)

func TestBootstrapDeployReadsInventoryBeforeRemoteAccess(t *testing.T) {
	root := t.TempDir()
	err := RunBootstrapDeploy(context.Background(), BootstrapDeployOptions{
		Site:                     "gamma",
		SHA:                      "0123456789abcdef0123456789abcdef01234567",
		RepoRoot:                 root,
		InventoryPath:            filepath.Join(root, "missing-inventory.ini"),
		BootstrapCredentialsFile: filepath.Join(root, "bootstrap-credentials.yaml"),
	})
	if err == nil || !strings.Contains(err.Error(), "read inventory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBootstrapDeployRequiresBootstrapCredentialsFile(t *testing.T) {
	root := t.TempDir()
	err := RunBootstrapDeploy(context.Background(), BootstrapDeployOptions{
		Site:          "gamma",
		SHA:           "0123456789abcdef0123456789abcdef01234567",
		RepoRoot:      root,
		InventoryPath: filepath.Join(root, "missing-inventory.ini"),
	})
	if err == nil || err.Error() != "bootstrap credentials file is required" {
		t.Fatalf("error = %v, want bootstrap credentials file requirement", err)
	}
}

func TestVerifyArtifactCandidateChecksBodyDigest(t *testing.T) {
	body := []byte("artifact-bytes")
	err := verifyArtifactCandidate(deployengine.ArtifactPublishCandidate{
		Output: "runtime",
		SHA256: testSHA256(body),
		Body:   body,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = verifyArtifactCandidate(deployengine.ArtifactPublishCandidate{
		Output: "runtime",
		SHA256: strings.Repeat("0", 64),
		Body:   body,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match expected") {
		t.Fatalf("error = %v, want digest mismatch", err)
	}
}

func TestLocalArtifactUploadPathWritesBody(t *testing.T) {
	body := []byte("bundle")
	path, cleanup, err := localArtifactUploadPath(deployengine.ArtifactPublishCandidate{Output: "bundle", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if got := readFile(t, path); got != string(body) {
		t.Fatalf("temporary artifact body = %q", got)
	}
}

func TestValidateLocalOpenBaoSiteRootTokenFileRequiresPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site-root-token")
	if err := os.WriteFile(path, []byte("token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := validateLocalOpenBaoSiteRootTokenFile(path)
	if err == nil || !strings.Contains(err.Error(), "readable only by the operator") {
		t.Fatalf("error = %v, want private mode rejection", err)
	}
}

func TestOpenBaoSiteRootTokenInstallCommandUsesRunPath(t *testing.T) {
	command := openBaoSiteRootTokenInstallCommand("/tmp/root-token", openBaoSiteRootTokenPath)
	for _, want := range []string{"/run/verself/bootstrap/openbao-site-root.token", "install -d", "-m 0700", "-m 0600", "trap 'rm -f \"$tmp\"' EXIT"} {
		if !strings.Contains(command, want) {
			t.Fatalf("install command missing %q:\n%s", want, command)
		}
	}
}

func TestSCPCommandUsesUppercasePortFlag(t *testing.T) {
	cmd := scpCommand(context.Background(), inventoryTarget{Host: "2001:db8::1", User: "ubuntu", Port: 2222}, "/tmp/local", "/tmp/remote")
	args := strings.Join(cmd.Args, "\n")
	for _, want := range []string{"-P", "2222", "ubuntu@[2001:db8::1]:/tmp/remote"} {
		if !strings.Contains(args, want) {
			t.Fatalf("scp args missing %q:\n%s", want, args)
		}
	}
}

func TestRemoteArtifactFileUsesDigestAndSafeSegments(t *testing.T) {
	got := remoteArtifactFile(bootstrapArtifactRoot, "..", ".", deployengine.ArtifactPublishCandidate{
		Output: "../service/runtime",
		SHA256: strings.Repeat("a", 64),
	})

	if strings.Contains(got, "/../") || strings.Contains(got, "/./") {
		t.Fatalf("remote artifact path is not sanitized: %s", got)
	}
	if !strings.Contains(got, "/_/_/") || !strings.Contains(got, strings.Repeat("a", 64)+"-.._service_runtime.tar") {
		t.Fatalf("remote artifact path missing digest-safe output name: %s", got)
	}
}

func TestRemoteArtifactGetterSourceUsesLoopbackHTTP(t *testing.T) {
	got := remoteArtifactGetterSource("gamma", "sha", deployengine.ArtifactPublishCandidate{
		Output: "openbao-runtime",
		SHA256: strings.Repeat("b", 64),
	})

	if strings.HasPrefix(got, "file:") {
		t.Fatalf("getter source uses unsupported file scheme: %s", got)
	}
	if !strings.HasPrefix(got, "http://127.0.0.1:7380/gamma/sha/") {
		t.Fatalf("getter source = %q", got)
	}
}

func TestExternalRuntimeSecretImportErrorListsAllMissingSecrets(t *testing.T) {
	err := externalRuntimeSecretImportError([]string{"z.secret", "a.secret"})

	want := "external OpenBao runtime secrets are not imported: a.secret, z.secret; add them under openbao_runtime_secrets in --bootstrap-credentials-file or pre-seed OpenBao"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestLoadBootstrapCredentialImportsMapsDeclaredRuntimeSecrets(t *testing.T) {
	path := writeSecretInputFile(t, 0o600, `site: gamma
openbao_runtime_secrets:
  github-integration-service.github.private_key: private-key
  github-integration-service.github.webhook_secret: github-webhook
  github-integration-service.github.oauth_client_secret: github-oauth
  auth-control-plane.github_login.oauth_client_secret: github-login
  billing-service.stripe.secret_key: rk_test
  billing-service.stripe.webhook_secret: whsec_test
`)
	imports, err := loadBootstrapCredentialImports("gamma", path, testRuntimeSecretCatalog(
		"github-integration-service.github.private_key",
		"github-integration-service.github.webhook_secret",
		"github-integration-service.github.oauth_client_secret",
		"auth-control-plane.github_login.oauth_client_secret",
		"billing-service.stripe.secret_key",
		"billing-service.stripe.webhook_secret",
	))
	if err != nil {
		t.Fatal(err)
	}
	if imports["github-integration-service.github.private_key"] != "private-key" || imports["billing-service.stripe.webhook_secret"] != "whsec_test" {
		t.Fatalf("imports = %#v", imports)
	}
}

func TestLoadBootstrapCredentialImportsRequiresSiteMatch(t *testing.T) {
	path := writeSecretInputFile(t, 0o600, `site: prod
openbao_runtime_secrets:
  billing-service.stripe.secret_key: rk_test
  billing-service.stripe.webhook_secret: whsec_test
`)
	_, err := loadBootstrapCredentialImports("gamma", path, testRuntimeSecretCatalog(
		"billing-service.stripe.secret_key",
		"billing-service.stripe.webhook_secret",
	))
	if err == nil || !strings.Contains(err.Error(), `site "prod" does not match --site=gamma`) {
		t.Fatalf("error = %v, want site mismatch", err)
	}
}

func TestLoadBootstrapCredentialImportsRejectsLooseFileMode(t *testing.T) {
	path := writeSecretInputFile(t, 0o644, `site: gamma
openbao_runtime_secrets:
  billing-service.stripe.secret_key: rk_test
  billing-service.stripe.webhook_secret: whsec_test
`)
	_, err := loadBootstrapCredentialImports("gamma", path, testRuntimeSecretCatalog(
		"billing-service.stripe.secret_key",
		"billing-service.stripe.webhook_secret",
	))
	if err == nil || !strings.Contains(err.Error(), "mode 0600 or stricter") {
		t.Fatalf("error = %v, want mode rejection", err)
	}
}

func TestLoadBootstrapCredentialImportsRequiresDeclaredExternalSecret(t *testing.T) {
	path := writeSecretInputFile(t, 0o600, `site: gamma
openbao_runtime_secrets:
  billing-service.stripe.secret_key: rk_test
  billing-service.stripe.webhook_secret: whsec_test
`)
	catalog := testRuntimeSecretCatalog("billing-service.stripe.secret_key")
	catalog.ByName["billing-service.stripe.webhook_secret"] = runtimeSecretDeclaration{Name: "billing-service.stripe.webhook_secret"}
	_, err := loadBootstrapCredentialImports("gamma", path, catalog)
	if err == nil || !strings.Contains(err.Error(), "billing-service.stripe.webhook_secret is not marked external_openbao") {
		t.Fatalf("error = %v, want external_openbao rejection", err)
	}
}

func TestLoadBootstrapCredentialImportsRequiresCompleteGroups(t *testing.T) {
	path := writeSecretInputFile(t, 0o600, `site: gamma
openbao_runtime_secrets:
  billing-service.stripe.secret_key: rk_test
`)
	_, err := loadBootstrapCredentialImports("gamma", path, testRuntimeSecretCatalog(
		"billing-service.stripe.secret_key",
		"billing-service.stripe.webhook_secret",
	))
	if err == nil || !strings.Contains(err.Error(), "incomplete Stripe billing credential group") {
		t.Fatalf("error = %v, want incomplete Stripe group", err)
	}
}

func TestLoadBootstrapCredentialInputAcceptsCloudflareBlock(t *testing.T) {
	path := writeSecretInputFile(t, 0o600, `site: gamma
cloudflare:
  account_admin_api_token: token
  account_id: c3eaeffaadf7d4847684d4775c16d598
  object_storage_bucket: verself-deployment-artifacts
  child_token_ttl: 168h
`)
	input, err := loadBootstrapCredentialInput("gamma", path, testRuntimeSecretCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if input.Cloudflare == nil || input.Cloudflare.AccountID != "c3eaeffaadf7d4847684d4775c16d598" || input.Cloudflare.ChildTokenTTL != 168*time.Hour {
		t.Fatalf("cloudflare input = %#v", input.Cloudflare)
	}
}

func TestLoadBootstrapCredentialInputRequiresCompleteCloudflareBlock(t *testing.T) {
	path := writeSecretInputFile(t, 0o600, `site: gamma
cloudflare:
  account_admin_api_token: token
`)
	_, err := loadBootstrapCredentialInput("gamma", path, testRuntimeSecretCatalog())
	if err == nil || !strings.Contains(err.Error(), "missing cloudflare.account_id, cloudflare.object_storage_bucket, cloudflare.child_token_ttl") {
		t.Fatalf("error = %v, want incomplete Cloudflare block", err)
	}
}

func TestLoadBootstrapCredentialInputRejectsUnknownYAMLFields(t *testing.T) {
	path := writeSecretInputFile(t, 0o600, `site: gamma
cloudflare:
  account_admin_api_key: token
`)
	_, err := loadBootstrapCredentialInput("gamma", path, testRuntimeSecretCatalog())
	if err == nil || !strings.Contains(err.Error(), "field account_admin_api_key not found") {
		t.Fatalf("error = %v, want unknown field rejection", err)
	}
}

func TestSortedRuntimeRolesExcludesVaultOnlyRoles(t *testing.T) {
	got := sortedRuntimeRoles(runtimeAccessCatalog{
		Roles:     map[string]struct{}{"deployment-service-runtime": {}},
		RoleReads: map[string]map[string]struct{}{},
		RoleWrites: map[string]map[string]struct{}{
			"object-storage-runtime": {"object-storage-service.r2.proxy_access_key_id": {}},
		},
	})

	want := []string{"object-storage-runtime"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roles = %#v, want %#v", got, want)
	}
}

func TestOpenBaoRolePolicyAllowsNomadTokenRenewal(t *testing.T) {
	policy := openBaoRolePolicy("object-storage-runtime", runtimeAccessCatalog{
		RoleReads: map[string]map[string]struct{}{
			"object-storage-runtime": {"object-storage-service.r2.proxy_access_key_id": {}},
		},
	})

	for _, want := range []string{
		`path "auth/token/renew-self"`,
		`capabilities = ["update"]`,
		`path "kv-runtime/data/secret/org/object-storage-service.r2.proxy_access_key_id"`,
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("policy missing %q:\n%s", want, policy)
		}
	}
}

func TestMissingProducedRuntimeSecretsBlocksUntilProducerRuns(t *testing.T) {
	secrets := testRuntimeSecretCatalog("ready")
	secrets.Declarations = append(secrets.Declarations, runtimeSecretDeclaration{
		Name:          "later",
		ProducedByJob: "producer",
	})
	secrets.ByName["later"] = secrets.Declarations[len(secrets.Declarations)-1]
	access := runtimeAccessCatalog{
		JobReads: map[string]map[string]struct{}{
			"consumer": {
				"ready": {},
				"later": {},
			},
		},
	}

	available := bootstrapAvailableRuntimeSecrets(secrets)
	got := missingProducedRuntimeSecrets("consumer", access, secrets, available)
	if strings.Join(got, ",") != "later" {
		t.Fatalf("missing = %#v, want later", got)
	}
	available["later"] = struct{}{}
	got = missingProducedRuntimeSecrets("consumer", access, secrets, available)
	if len(got) != 0 {
		t.Fatalf("missing after producer = %#v, want none", got)
	}
}

func TestProducedRuntimeSecretsForJob(t *testing.T) {
	secrets := testRuntimeSecretCatalog("ready")
	secrets.Declarations = append(secrets.Declarations,
		runtimeSecretDeclaration{Name: "a", ProducedByJob: "producer"},
		runtimeSecretDeclaration{Name: "b", ProducedByJob: "other"},
		runtimeSecretDeclaration{Name: "c", ProducedByJob: "producer"},
	)
	for _, declaration := range secrets.Declarations {
		secrets.ByName[declaration.Name] = declaration
	}

	got := producedRuntimeSecretsForJob(secrets, "producer")
	want := []string{"a", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("produced = %#v, want %#v", got, want)
	}
}

func TestParseRemotePasswdEntry(t *testing.T) {
	identity, err := parseRemotePasswdEntry("deployment_service", "deployment_service:x:990:984::/home/deployment_service:/usr/sbin/nologin\n")
	if err != nil {
		t.Fatal(err)
	}
	if identity.UID != 990 || identity.GID != 984 {
		t.Fatalf("identity = uid:%d gid:%d, want uid:990 gid:984", identity.UID, identity.GID)
	}
}

func TestParseRemotePasswdEntryRejectsMalformedOutput(t *testing.T) {
	err := func() error {
		_, err := parseRemotePasswdEntry("deployment_service", "deployment_service:x:not-a-uid:984\n")
		return err
	}()
	if err == nil || !strings.Contains(err.Error(), "parse remote uid") {
		t.Fatalf("error = %v, want parse remote uid error", err)
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func testSHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func writeSecretInputFile(t *testing.T, mode os.FileMode, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bootstrap-credentials.yaml")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func testRuntimeSecretCatalog(names ...string) runtimeSecretCatalog {
	catalog := runtimeSecretCatalog{ByName: map[string]runtimeSecretDeclaration{}}
	for _, name := range names {
		declaration := runtimeSecretDeclaration{Name: name, ExternalOpenBao: true}
		catalog.Declarations = append(catalog.Declarations, declaration)
		catalog.ByName[name] = declaration
	}
	return catalog
}
