package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/verself/integrations/cloudflare/r2-control-plane/internal/r2control"
)

func TestLoadServerAuthTokenPrefersEnv(t *testing.T) {
	t.Setenv("TEST_R2_CONTROL_PLANE_TOKEN", "from-env")

	token, err := loadServerAuthToken(config{authTokenEnv: "TEST_R2_CONTROL_PLANE_TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "from-env" {
		t.Fatalf("token = %q, want env value", token)
	}
}

func TestLoadServerAuthTokenRejectsMissingToken(t *testing.T) {
	_, err := loadServerAuthToken(config{authTokenEnv: "TEST_R2_CONTROL_PLANE_TOKEN"})
	if err == nil || !strings.Contains(err.Error(), "auth token is required") {
		t.Fatalf("error = %v, want missing token refusal", err)
	}
}

func TestUploadServerRequiresBearerToken(t *testing.T) {
	server := uploadServer{authToken: "expected"}
	if server.authorized(httptest.NewRequest("POST", "/", nil)) {
		t.Fatal("request without bearer token was authorized")
	}
	rawTokenReq := httptest.NewRequest("POST", "/", nil)
	rawTokenReq.Header.Set("Authorization", "expected")
	if server.authorized(rawTokenReq) {
		t.Fatal("request without bearer scheme was authorized")
	}
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer expected")
	if !server.authorized(req) {
		t.Fatal("request with bearer token was not authorized")
	}
}

func TestReadParentAPITokenRejectsWhitespaceSeparatedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("one two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAPITokenFile(path, "api token"); err == nil {
		t.Fatal("expected whitespace-separated token to fail")
	}
}

func TestValidateChildCredentialPersistence(t *testing.T) {
	cfg := validTestConfig()
	cfg.action = "ensure-publisher"
	cfg.childCredentialPersistence = childPersistenceSiteSeed
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	cfg.action = "ensure-recovery"
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "not valid for recovery credentials") {
		t.Fatalf("error = %v, want recovery persistence refusal", err)
	}
	cfg.action = "provision-site-bootstrap"
	cfg.childCredentialPersistence = childPersistenceControllerOpenBao
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "requires --child-credential-persistence=site-seed") {
		t.Fatalf("error = %v, want site-seed persistence refusal", err)
	}
}

func TestChildCredentialPersistenceDefaultsByAction(t *testing.T) {
	cfg := validTestConfig()
	cfg.childCredentialPersistence = ""
	cfg.action = "provision-site-bootstrap"
	if got := cfg.effectiveChildCredentialPersistence(); got != childPersistenceSiteSeed {
		t.Fatalf("site bootstrap persistence = %q, want %q", got, childPersistenceSiteSeed)
	}
	cfg.action = "ensure-publisher"
	if got := cfg.effectiveChildCredentialPersistence(); got != childPersistenceSiteSeed {
		t.Fatalf("publisher persistence = %q, want %q", got, childPersistenceSiteSeed)
	}
	cfg.action = "rotate-getter"
	if got := cfg.effectiveChildCredentialPersistence(); got != childPersistenceSiteSeed {
		t.Fatalf("getter persistence = %q, want %q", got, childPersistenceSiteSeed)
	}
	cfg.action = "rotate-object-storage-provider"
	if got := cfg.effectiveChildCredentialPersistence(); got != childPersistenceSiteSeed {
		t.Fatalf("object-storage persistence = %q, want %q", got, childPersistenceSiteSeed)
	}
	cfg.action = "ensure-recovery"
	if got := cfg.effectiveChildCredentialPersistence(); got != childPersistenceControllerOpenBao {
		t.Fatalf("recovery persistence = %q, want %q", got, childPersistenceControllerOpenBao)
	}
}

func TestLoadTokenAdminCredentialsUsesSeparateCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token-admin.env")
	if err := os.WriteFile(path, []byte(`
CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID=token-admin-id
CLOUDFLARE_TOKEN_ADMIN_API_TOKEN=token-admin-value
`), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := testParentCredential()
	admin, err := loadTokenAdminCredentials(context.Background(), config{
		tokenAdminCredentialSource: "env-file",
		tokenAdminCredentialsFile:  path,
		tokenAdminAPITokenEnv:      "CLOUDFLARE_TOKEN_ADMIN_API_TOKEN",
		timeout:                    1,
	}, parent)
	if err != nil {
		t.Fatal(err)
	}
	if admin.AccessKeyID != "token-admin-id" || admin.APIToken != "token-admin-value" {
		t.Fatalf("admin = %+v", admin)
	}
	if admin.SecretAccessKey == "" || strings.Contains(admin.SecretAccessKey, "token-admin-value") {
		t.Fatalf("admin secret was not derived safely: %q", admin.SecretAccessKey)
	}
}

func TestLoadTokenAdminCredentialsUsesEnvSourceWhenOnlyEnvIsConfigured(t *testing.T) {
	t.Setenv("CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID", "token-admin-id")
	t.Setenv("CLOUDFLARE_TOKEN_ADMIN_API_TOKEN", "token-admin-value")

	admin, err := loadTokenAdminCredentials(context.Background(), config{
		tokenAdminAPITokenEnv: "CLOUDFLARE_TOKEN_ADMIN_API_TOKEN",
		timeout:               1,
	}, testParentCredential())
	if err != nil {
		t.Fatal(err)
	}
	if admin.AccessKeyID != "token-admin-id" || admin.APIToken != "token-admin-value" {
		t.Fatalf("admin = %+v", admin)
	}
}

func TestTokenAdminFileConfiguresTokenAdminSource(t *testing.T) {
	cfg := config{tokenAdminAPITokenFile: filepath.Join(t.TempDir(), "token-admin")}
	if !tokenAdminConfigured(cfg) {
		t.Fatal("token-admin file did not configure token-admin credentials")
	}
	if got := cfg.withTokenAdminAutoDefaults(); got.tokenAdminCredentialSource != "" {
		t.Fatalf("token-admin source = %q, want direct file source", got.tokenAdminCredentialSource)
	}
}

func TestLoadTokenAdminCredentialsFallsBackToParent(t *testing.T) {
	parent := testParentCredential()
	admin, err := loadTokenAdminCredentials(context.Background(), config{}, parent)
	if err != nil {
		t.Fatal(err)
	}
	if admin != parent {
		t.Fatalf("admin = %+v, want parent", admin)
	}
}

func TestSiteBootstrapSeedUpdatesContainOnlyBootstrapKeys(t *testing.T) {
	updates := siteBootstrapSeedUpdates(
		r2control.CreatedAPIToken{ID: "publisher-id", Value: "publisher-token"},
		r2control.CreatedAPIToken{S3AccessKeyID: "getter-id", S3SecretKey: "getter-secret"},
		r2control.CreatedAPIToken{S3AccessKeyID: "admin-id", S3SecretKey: "admin-secret"},
		r2control.CreatedAPIToken{S3AccessKeyID: "proxy-id", S3SecretKey: "proxy-secret"},
	)
	want := map[string]string{
		"cloudflare_r2_control_plane_publisher_token_id":    "publisher-id",
		"cloudflare_r2_control_plane_publisher_api_token":   "publisher-token",
		"nomad_artifact_getter_s3_access_key_id":            "getter-id",
		"nomad_artifact_getter_s3_secret_access_key":        "getter-secret",
		"object_storage_service_r2_admin_access_key_id":     "admin-id",
		"object_storage_service_r2_admin_secret_access_key": "admin-secret",
		"object_storage_service_r2_proxy_access_key_id":     "proxy-id",
		"object_storage_service_r2_proxy_secret_access_key": "proxy-secret",
	}
	if len(updates) != len(want) {
		t.Fatalf("updates = %#v, want %#v", updates, want)
	}
	for key, value := range want {
		if updates[key] != value {
			t.Fatalf("updates[%s] = %q, want %q", key, updates[key], value)
		}
	}
}

func TestFingerprintMapDoesNotExposeRawValues(t *testing.T) {
	fingerprints := fingerprintMap(map[string]string{"secret": "raw-value"})
	if fingerprints["secret"] == "" {
		t.Fatal("missing fingerprint")
	}
	if strings.Contains(fingerprints["secret"], "raw-value") {
		t.Fatalf("fingerprint exposed raw value: %q", fingerprints["secret"])
	}
}

func testParentCredential() r2control.ParentCredentials {
	return r2control.ParentCredentials{
		AccessKeyID:     "parent-id",
		SecretAccessKey: "parent-secret",
		APIToken:        "parent-token",
		Source:          "test",
	}
}

func validTestConfig() config {
	return config{
		accountID:                  "c3eaeffaadf7d4847684d4775c16d598",
		bucket:                     "verself-deployment-artifacts",
		keyPrefix:                  "sha256",
		region:                     "auto",
		tempTTL:                    15 * time.Minute,
		uploadSessionTTL:           30 * time.Minute,
		ephemeralPublisherTTL:      time.Hour,
		childTokenTTL:              7 * 24 * time.Hour,
		tokenAdminTTL:              7 * 24 * time.Hour,
		inventoryDepth:             2,
		childCredentialPersistence: childPersistenceControllerOpenBao,
	}
}
