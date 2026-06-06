package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/verself/integrations/cloudflare/control-plane/r2control"
)

func TestRetryableR2CredentialPropagationUsesTypedStatuses(t *testing.T) {
	if !retryableR2CredentialPropagation(r2control.StatusError{StatusCode: http.StatusUnauthorized}) {
		t.Fatal("401 provider status should be retryable")
	}
	if !retryableR2CredentialPropagation(r2VerificationStatusError{status: http.StatusForbidden}) {
		t.Fatal("403 verification status should be retryable")
	}
	if retryableR2CredentialPropagation(r2VerificationStatusError{status: http.StatusNotFound}) {
		t.Fatal("404 verification status should not be retryable")
	}
}

func TestAccountAdminR2AuthorityErrorExplainsPermission(t *testing.T) {
	err := accountAdminR2AuthorityError(validTestConfig(), r2control.APIStatusError{
		Method:     http.MethodGet,
		Path:       "/accounts/0123456789abcdef0123456789abcdef/r2/buckets/verself-deployment-artifacts",
		StatusCode: http.StatusForbidden,
		Body:       `{"success":false}`,
	})
	if err == nil || !strings.Contains(err.Error(), "requires Workers R2 Storage Read/Write") {
		t.Fatalf("error = %v", err)
	}
}

func TestAccountAdminR2AuthorityErrorLeavesNonAuthError(t *testing.T) {
	input := r2control.APIStatusError{
		Method:     http.MethodGet,
		Path:       "/accounts/0123456789abcdef0123456789abcdef/r2/buckets/missing",
		StatusCode: http.StatusNotFound,
		Body:       `{"success":false}`,
	}
	err := accountAdminR2AuthorityError(validTestConfig(), input)
	if err.Error() != input.Error() {
		t.Fatalf("error = %v, want original", err)
	}
}

func TestParentCredentialConfigUsesOpenBao(t *testing.T) {
	cfg := validTestConfig()
	cfg.openBaoAddr = "https://openbao.internal"
	cfg.openBaoPath = "kv-controller/data/integrations/cloudflare/r2/capabilities/object-storage-admin"
	cfg.openBaoCACertFile = "/openbao/ca.pem"
	cfg.openBaoTokenFile = "/run/openbao/token"
	cfg.openBaoToken = "openbao-token"

	got := cfg.parentCredentialConfig()
	if got.Source != r2control.ParentCredentialSourceOpenBao {
		t.Fatalf("source = %q", got.Source)
	}
	if got.OpenBaoAddr != cfg.openBaoAddr || got.OpenBaoPath != cfg.openBaoPath || got.OpenBaoCACertFile != cfg.openBaoCACertFile || got.OpenBaoTokenFile != cfg.openBaoTokenFile {
		t.Fatalf("OpenBao config = %+v", got)
	}
	if got.OpenBaoToken != "openbao-token" {
		t.Fatalf("OpenBao token was not carried in memory")
	}
}

func TestRuntimeSecretOpenBaoPathEscapesName(t *testing.T) {
	got := runtimeSecretOpenBaoPath("object-storage-service.r2.admin_access_key_id")

	if got != "kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id" {
		t.Fatalf("path = %q", got)
	}
}

func TestWriteRuntimeSecretsWritesKVValues(t *testing.T) {
	writes := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("X-Vault-Token") != "openbao-token" {
			t.Fatalf("token header = %q", r.Header.Get("X-Vault-Token"))
		}
		var body struct {
			Data map[string]string `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		writes[strings.TrimPrefix(r.URL.Path, "/v1/")] = body.Data["value"]
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "openbao-token")
	if err := os.WriteFile(tokenFile, []byte("openbao-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := writeRuntimeSecrets(context.Background(), config{
		openBaoAddr:             "http://controller-openbao.invalid",
		runtimeOpenBaoAddr:      server.URL,
		runtimeOpenBaoTokenFile: tokenFile,
		timeout:                 time.Second,
	}, map[string]string{
		"object-storage-service.r2.admin_access_key_id":     "admin-id",
		"object-storage-service.r2.proxy_secret_access_key": "proxy-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if writes["kv-runtime/data/secret/org/object-storage-service.r2.admin_access_key_id"] != "admin-id" {
		t.Fatalf("admin write = %#v", writes)
	}
	if writes["kv-runtime/data/secret/org/object-storage-service.r2.proxy_secret_access_key"] != "proxy-secret" {
		t.Fatalf("proxy write = %#v", writes)
	}
}

func TestValidateImportAccountAdminRequiresExactlyOneTokenSource(t *testing.T) {
	cfg := validTestConfig()
	cfg.action = "import-account-admin"
	cfg.openBaoAddr = "https://openbao.internal"

	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "--openbao-token-file and --account-admin-api-token-file are required") {
		t.Fatalf("validate error = %v", err)
	}

	cfg.openBaoTokenFile = "/run/openbao/token"
	cfg.accountAdminAPITokenFile = "/run/operator/cloudflare-token"
	if err := cfg.validate(); err != nil {
		t.Fatalf("file token source should validate: %v", err)
	}

	cfg.operatorImportStdin = true
	err = cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "--operator-import-stdin cannot be combined") {
		t.Fatalf("validate error with mixed token sources = %v", err)
	}

	cfg.openBaoTokenFile = ""
	cfg.accountAdminAPITokenFile = ""
	if err := cfg.validate(); err != nil {
		t.Fatalf("operator import stdin should validate: %v", err)
	}
}

func TestCloudflareRecoveryAuthorityUnavailableErrorRequestsRootTrustMaterial(t *testing.T) {
	err := cloudflareRecoveryAuthorityUnavailableError(
		"kv-controller/data/integrations/cloudflare/account-admin",
		errors.New("missing account-admin"),
		"kv-controller/data/integrations/cloudflare/r2/capabilities/recovery",
		errors.New("missing recovery credential"),
	)
	msg := err.Error()
	for _, want := range []string{
		"CloudflareRecoveryAuthorityAvailable=False",
		"RootTrustMaterialRequired",
		"--action=import-account-admin --operator-import-stdin",
		"encrypted OpenBao operator import token",
		"gitignored source such as secret.env",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q did not contain %q", msg, want)
		}
	}
}

func TestReadRequiredSecretFileRequiresOperatorOnlyMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("cloudflare-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := readRequiredSecretFile(path, "cloudflare token")
	if err == nil || !strings.Contains(err.Error(), "readable only by the operator") {
		t.Fatalf("read error = %v", err)
	}
}

func TestReadRequiredSecretFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("cloudflare-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "token")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, err := readRequiredSecretFile(link, "cloudflare token")
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("read error = %v", err)
	}
}

func TestReadRequiredSecretFileTrimsSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(" cloudflare-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readRequiredSecretFile(path, "cloudflare token")
	if err != nil {
		t.Fatal(err)
	}
	defer clearBytes(got)
	if string(got) != "cloudflare-token" {
		t.Fatalf("secret = %q", got)
	}
}

func TestReadRequiredSecretStdinTrimsSecret(t *testing.T) {
	got, err := readRequiredSecretStdin(strings.NewReader(" cloudflare-token\n"), "cloudflare token")
	if err != nil {
		t.Fatal(err)
	}
	defer clearBytes(got)
	if string(got) != "cloudflare-token" {
		t.Fatalf("secret = %q", got)
	}
}

func TestReadOperatorImportStdinRequiresBothTokens(t *testing.T) {
	material, err := readOperatorImportStdin(strings.NewReader(`{
  "openBaoToken": " openbao-token ",
  "cloudflareAccountAdminAPIToken": " cloudflare-token "
}`))
	if err != nil {
		t.Fatal(err)
	}
	if material.OpenBaoToken != "openbao-token" || material.CloudflareAccountAdminAPIToken != "cloudflare-token" {
		t.Fatalf("material = %#v", material)
	}

	_, err = readOperatorImportStdin(strings.NewReader(`{"openBaoToken":"openbao-token"}`))
	if err == nil || !strings.Contains(err.Error(), "requires openBaoToken and cloudflareAccountAdminAPIToken") {
		t.Fatalf("missing token error = %v", err)
	}
}

func TestSiteDNSZonesUsesHostedZoneForSubdomainSite(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/sites/gamma/vars.yml"), `
verself_domain: gamma.verself.sh
cloudflare_product_zone: verself.sh
company_domain: gamma.guardianintelligence.org
cloudflare_company_zone: guardianintelligence.org
cloudflare_dns_records:
  - { kind: public_api_origin, record: deployments.api, zone: product }
  - { kind: browser_origin, record: "@", zone: company }
`)
	zones, err := siteDNSZones(config{repoRoot: root, site: "gamma"})
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 2 || zones[0] != "guardianintelligence.org" || zones[1] != "verself.sh" {
		t.Fatalf("zones = %#v", zones)
	}
}

func TestLoadDNSDesiredStateUsesInventoryWhenSiteIPIsPlaceholder(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/sites/gamma/vars.yml"), `
verself_domain: gamma.verself.sh
company_domain: gamma.guardianintelligence.org
bare_metal_public_ipv4: 0.0.0.0
cloudflare_dns_records:
  - { kind: browser_origin, record: "@", zone: product }
`)
	writeTestFile(t, filepath.Join(root, "src/sites/gamma/inventory.ini"), `
[infra]
vs-gamma-w0 verself_ssh_host=203.0.113.10
`)

	desired, err := loadDNSDesiredState(config{repoRoot: root, site: "gamma"})
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.records) != 1 {
		t.Fatalf("unexpected desired records: %+v", desired.records)
	}
	if desired.records[0].targetIP != "203.0.113.10" {
		t.Fatalf("target IP came from vars placeholder: %+v", desired.records[0])
	}
}

func TestLoadDNSDesiredStateUsesHostedZoneForSubdomainSite(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/sites/gamma/vars.yml"), `
verself_domain: gamma.verself.sh
cloudflare_product_zone: verself.sh
company_domain: gamma.guardianintelligence.org
cloudflare_company_zone: guardianintelligence.org
bare_metal_public_ipv4: 203.0.113.10
cloudflare_dns_records:
  - { kind: public_api_origin, record: deployments.api, zone: product }
  - { kind: browser_origin, record: "@", zone: company }
`)

	desired, err := loadDNSDesiredState(config{repoRoot: root, site: "gamma"})
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.records) != 2 {
		t.Fatalf("unexpected desired records: %+v", desired.records)
	}
	byFQDN := map[string]dnsDesiredRecord{}
	for _, record := range desired.records {
		byFQDN[record.fqdn] = record
	}
	deployments := byFQDN["deployments.api.gamma.verself.sh"]
	if deployments.zoneName != "verself.sh" || deployments.record != "deployments.api.gamma" {
		t.Fatalf("deployment record = %+v", deployments)
	}
	company := byFQDN["gamma.guardianintelligence.org"]
	if company.zoneName != "guardianintelligence.org" || company.record != "gamma" {
		t.Fatalf("company record = %+v", company)
	}
}

func TestApplyRecoveryConfigMapsMinimalCloudflareControlPlane(t *testing.T) {
	t.Setenv("NOMAD_SECRETS_DIR", "/alloc/secrets")
	path := filepath.Join(t.TempDir(), "cloudflare-recovery.yml")
	writeTestFile(t, path, minimalRecoveryConfigYAML())

	cfg := validTestConfig()
	cfg.action = "recover"
	cfg.recoveryConfig = path
	if err := cfg.applyRecoveryConfig(); err != nil {
		t.Fatal(err)
	}
	if cfg.site != "gamma" || cfg.accountID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("site/account = %s/%s", cfg.site, cfg.accountID)
	}
	if cfg.openBaoTokenFile != "/alloc/secrets/vault_token" {
		t.Fatalf("openbao token file = %q", cfg.openBaoTokenFile)
	}
	if cfg.accountAdminOpenBaoPath != "kv-controller/data/integrations/cloudflare/account-admin" {
		t.Fatalf("account-admin OpenBao path = %q", cfg.accountAdminOpenBaoPath)
	}
	if cfg.objectStorageRuntimeSecretNames().AdminAccessKeyName() != "object-storage-service.r2.admin_access_key_id" {
		t.Fatalf("runtime secret names = %#v", cfg.objectStorageRuntimeSecretNames())
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDNSDesiredStateFromRecoveryUsesHostedZones(t *testing.T) {
	doc := CloudflareControlPlane{
		Spec: CloudflareControlPlaneSpec{
			TargetIPv4: "203.0.113.10",
			DNS: CloudflareDNS{
				Zones: []CloudflareDNSZone{
					{Name: "product", Zone: "verself.sh", Domain: "gamma.verself.sh"},
					{Name: "company", Zone: "guardianintelligence.org", Domain: "gamma.guardianintelligence.org"},
				},
				Records: []CloudflareDNSRecord{
					{Zone: "product", Record: "deployments.api", TTL: 1},
					{Zone: "company", Record: "@", TTL: 1},
				},
			},
		},
	}
	desired, err := dnsDesiredStateFromRecovery(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.records) != 2 {
		t.Fatalf("records = %#v", desired.records)
	}
	byFQDN := map[string]dnsDesiredRecord{}
	for _, record := range desired.records {
		byFQDN[record.fqdn] = record
	}
	if got := byFQDN["deployments.api.gamma.verself.sh"]; got.zoneName != "verself.sh" || got.record != "deployments.api.gamma" {
		t.Fatalf("product record = %+v", got)
	}
	if got := byFQDN["gamma.guardianintelligence.org"]; got.zoneName != "guardianintelligence.org" || got.record != "gamma" {
		t.Fatalf("company record = %+v", got)
	}
}

func TestLoadSiteConfigUsesGlobalCloudflareAccount(t *testing.T) {
	root := t.TempDir()
	writeCloudflareAccountConfig(t, root)

	cfg, err := loadSiteConfig(root, "gamma")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccountID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("account id = %q", cfg.AccountID)
	}
	if cfg.Bucket != "verself-deployment-artifacts" {
		t.Fatalf("bucket = %q", cfg.Bucket)
	}
	if cfg.SitePrefix != "gamma" {
		t.Fatalf("site prefix = %q", cfg.SitePrefix)
	}
}

func validTestConfig() config {
	return config{
		action:                 "verify-account-admin",
		site:                   "gamma",
		accountID:              "0123456789abcdef0123456789abcdef",
		bucket:                 "verself-deployment-artifacts",
		keyPrefix:              "sha256",
		region:                 "auto",
		tempTTL:                15 * time.Minute,
		childTokenTTL:          7 * 24 * time.Hour,
		inventoryDepth:         2,
		dnsConcurrency:         8,
		acmeDNSPropagationWait: 2 * time.Minute,
		certificateRenewBefore: 30 * 24 * time.Hour,
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCloudflareAccountConfig(t *testing.T, root string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, "src/integrations/cloudflare/account.json"), `{
  "version": "verself.cloudflare.account.v1",
  "control_plane_site": "prod",
  "account_id": "0123456789abcdef0123456789abcdef",
  "r2": {
    "deployment_artifacts_bucket": "verself-deployment-artifacts",
    "recovery_bucket": "verself-recovery"
  }
}`)
}

func minimalRecoveryConfigYAML() string {
	return `apiVersion: cloudflare.guardianintelligence.org/v1alpha1
kind: CloudflareControlPlane
metadata:
  name: gamma-cloudflare
spec:
  site: gamma
  accountID: 0123456789abcdef0123456789abcdef
  accountAdminOpenBaoPath: kv-controller/data/integrations/cloudflare/account-admin
  targetIPv4: 203.0.113.10
  openBao:
    address: https://127.0.0.1:8200
    tokenFile: ${NOMAD_SECRETS_DIR}/vault_token
    caCertFile: /etc/verself/openbao/ca.pem
  dns:
    zones:
      - name: product
        zone: verself.sh
        domain: gamma.verself.sh
    records:
      - zone: product
        record: "@"
        ttl: 1
        proxied: false
  tls:
    outputDir: /etc/haproxy/certs
    acme:
      directoryURL: https://acme-v02.api.letsencrypt.org/directory
      contactEmail: agents@guardianintelligence.org
      dnsPropagationWait: 2m
      renewBefore: 720h
    certificates:
      - name: gamma.verself.sh
        domains:
          - gamma.verself.sh
  objectStorage:
    bucket: verself-deployment-artifacts
    recoveryBucket: verself-recovery
    childTokenTTL: 168h
    runtimeSecrets:
      adminAccessKeyID: object-storage-service.r2.admin_access_key_id
      adminSecretAccessKey: object-storage-service.r2.admin_secret_access_key
      proxyAccessKeyID: object-storage-service.r2.proxy_access_key_id
      proxySecretAccessKey: object-storage-service.r2.proxy_secret_access_key
`
}
