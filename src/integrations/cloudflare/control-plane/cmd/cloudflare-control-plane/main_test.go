package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/verself/integrations/cloudflare/control-plane/internal/r2control"
)

func TestWriteBootstrapPublisherEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site-bootstrap", "gamma", "r2-publisher.env")
	publisher := r2control.CreatedAPIToken{
		ID:    "token-id",
		Value: "publisher-token",
	}

	if err := writeBootstrapPublisherEnvFile(path, publisher); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values, err := r2control.ParseEnvFile(body)
	if err != nil {
		t.Fatal(err)
	}
	wantSecret := r2control.SHA256Hex([]byte("publisher-token"))
	if values["CLOUDFLARE_R2_PUBLISHER_TOKEN_ID"] != "token-id" ||
		values["CLOUDFLARE_R2_PUBLISHER_API_TOKEN"] != "publisher-token" ||
		values["CLOUDFLARE_R2_PUBLISHER_SECRET_ACCESS_KEY"] != wantSecret {
		t.Fatalf("unexpected env values: %#v", values)
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

func TestValidateAllowsSecretEnvAccountAdminForBootstrapChildren(t *testing.T) {
	cfg := validTestConfig()
	cfg.action = "provision-site-bootstrap"
	cfg.accountAdminSource = accountAdminSourceSecretEnv

	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsSecretEnvAccountAdminRotation(t *testing.T) {
	cfg := validTestConfig()
	cfg.action = "rotate-admin-pair"
	cfg.accountAdminSource = accountAdminSourceSecretEnv

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsUnknownAccountAdminSource(t *testing.T) {
	cfg := validTestConfig()
	cfg.accountAdminSource = "legacy"

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error")
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
		action:                "verify-admin-pair",
		site:                  "gamma",
		accountID:             "0123456789abcdef0123456789abcdef",
		bucket:                "verself-deployment-artifacts",
		keyPrefix:             "sha256",
		region:                "auto",
		accountAdminSource:    accountAdminSourceOpenBao,
		tempTTL:               15 * time.Minute,
		uploadSessionTTL:      30 * time.Minute,
		ephemeralPublisherTTL: time.Hour,
		childTokenTTL:         7 * 24 * time.Hour,
		accountAdminTTL:       7 * 24 * time.Hour,
		inventoryDepth:        2,
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
