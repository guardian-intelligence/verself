package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/verself/integrations/cloudflare/control-plane/internal/r2control"
)

func TestWriteBootstrapPublisherCredentialUsesFixedFD(t *testing.T) {
	publisher := r2control.CreatedAPIToken{
		ID:            "token-id",
		S3AccessKeyID: "access-key-id",
		S3SecretKey:   "publisher-secret",
		ExpiresOn:     "2026-06-02T20:00:00Z",
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	originalOutput := openBootstrapPublisherOutput
	openBootstrapPublisherOutput = func() (*os.File, error) { return writer, nil }
	defer func() {
		openBootstrapPublisherOutput = originalOutput
	}()

	if err := writeBootstrapPublisherCredential(publisher); err != nil {
		t.Fatal(err)
	}

	var got bootstrapPublisherCredential
	err = json.NewDecoder(reader).Decode(&got)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessKeyID != publisher.S3AccessKeyID || got.SecretAccessKey != publisher.S3SecretKey || got.TokenID != publisher.ID || got.ExpiresOn != publisher.ExpiresOn {
		t.Fatalf("credential = %#v", got)
	}
}

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

func TestParentCredentialConfigUsesOpenBao(t *testing.T) {
	cfg := validTestConfig()
	cfg.openBaoAddr = "https://openbao.internal"
	cfg.openBaoPath = "kv-controller/data/integrations/cloudflare/r2/capabilities/deployment-publisher"
	cfg.openBaoCACertFile = "/openbao/ca.pem"
	cfg.openBaoTokenFile = "/run/openbao/token"

	got := cfg.parentCredentialConfig()
	if got.Source != r2control.ParentCredentialSourceOpenBao {
		t.Fatalf("source = %q", got.Source)
	}
	if got.OpenBaoAddr != cfg.openBaoAddr || got.OpenBaoPath != cfg.openBaoPath || got.OpenBaoCACertFile != cfg.openBaoCACertFile || got.OpenBaoTokenFile != cfg.openBaoTokenFile {
		t.Fatalf("OpenBao config = %+v", got)
	}
}

func TestNomadArtifactGetterBootstrapVarsOnlyIncludeGetter(t *testing.T) {
	getter := r2control.CreatedAPIToken{
		S3AccessKeyID: "getter-access-key",
		S3SecretKey:   "getter-secret-key",
	}

	got := nomadArtifactGetterBootstrapVars(getter)
	if len(got) != 2 {
		t.Fatalf("updates = %#v", got)
	}
	if got["nomad_artifact_getter_s3_access_key_id"] != getter.S3AccessKeyID || got["nomad_artifact_getter_s3_secret_access_key"] != getter.S3SecretKey {
		t.Fatalf("updates = %#v", got)
	}
}

func TestMergeBootstrapVarsWritesJSONOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bootstrap-vars.json")
	writeTestFile(t, path, `{"host_generated":"value"}`)

	if err := mergeBootstrapVars(path, map[string]string{
		"nomad_artifact_getter_s3_access_key_id":     "getter-access-key",
		"nomad_artifact_getter_s3_secret_access_key": "getter-secret-key",
	}); err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("bootstrap vars should be JSON, got %s: %v", body, err)
	}
	if got["host_generated"] != "value" || got["nomad_artifact_getter_s3_access_key_id"] != "getter-access-key" || got["nomad_artifact_getter_s3_secret_access_key"] != "getter-secret-key" {
		t.Fatalf("bootstrap vars = %#v", got)
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
		"cloudflare-r2-control-plane.publisher_token_id":    "publisher-id",
		"object-storage-service.r2.proxy_secret_access_key": "proxy-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if writes["kv-runtime/data/secret/org/cloudflare-r2-control-plane.publisher_token_id"] != "publisher-id" {
		t.Fatalf("publisher write = %#v", writes)
	}
	if writes["kv-runtime/data/secret/org/object-storage-service.r2.proxy_secret_access_key"] != "proxy-secret" {
		t.Fatalf("proxy write = %#v", writes)
	}
}

func TestSiteDNSZonesUsesHostedZoneForSubdomainSite(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src/host/sites/gamma/vars.yml"), `
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
	writeTestFile(t, filepath.Join(root, "src/host/sites/gamma/vars.yml"), `
verself_domain: gamma.verself.sh
company_domain: gamma.guardianintelligence.org
bare_metal_public_ipv4: 0.0.0.0
cloudflare_dns_records:
  - { kind: browser_origin, record: "@", zone: product }
`)
	writeTestFile(t, filepath.Join(root, "src/host/sites/gamma/inventory.ini"), `
[infra]
vs-gamma-w0 ansible_host=203.0.113.10
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
	writeTestFile(t, filepath.Join(root, "src/host/sites/gamma/vars.yml"), `
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

func TestLoadSiteConfigUsesGlobalCloudflareAccount(t *testing.T) {
	root := t.TempDir()
	writeCloudflareAccountConfig(t, root)
	writeTestFile(t, filepath.Join(root, "src/host/sites/gamma/site.json"), `{
  "artifact_delivery": {
    "kind": "cloudflare_r2_control_plane",
    "key_prefix": "sha256",
    "getter_options": {"region": "auto"},
    "checksum_algorithm": "sha256",
    "public": false
  }
}`)

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

func TestLoadSiteConfigRejectsSiteCloudflareGlobals(t *testing.T) {
	root := t.TempDir()
	writeCloudflareAccountConfig(t, root)
	writeTestFile(t, filepath.Join(root, "src/host/sites/gamma/site.json"), `{
  "artifact_delivery": {
    "kind": "cloudflare_r2_control_plane",
    "bucket": "verself-deployment-artifacts",
    "key_prefix": "sha256",
    "getter_options": {"region": "auto"},
    "checksum_algorithm": "sha256",
    "public": false
  }
}`)

	err := validLoadSiteConfigError(root, "gamma")
	if err == nil {
		t.Fatal("expected site Cloudflare global rejection")
	}
	if !strings.Contains(err.Error(), "artifact_delivery.bucket belongs to src/integrations/cloudflare/account.json") {
		t.Fatalf("error = %v", err)
	}
}

func validTestConfig() config {
	return config{
		action:                 "verify-admin-pair",
		site:                   "gamma",
		accountID:              "0123456789abcdef0123456789abcdef",
		bucket:                 "verself-deployment-artifacts",
		keyPrefix:              "sha256",
		region:                 "auto",
		tempTTL:                15 * time.Minute,
		uploadSessionTTL:       30 * time.Minute,
		childTokenTTL:          7 * 24 * time.Hour,
		accountAdminTTL:        7 * 24 * time.Hour,
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

func validLoadSiteConfigError(root, site string) error {
	_, err := loadSiteConfig(root, site)
	return err
}
