package r2control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseEnvFile(t *testing.T) {
	values, err := ParseEnvFile([]byte(`
CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID='abc'
CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY="def"
`))
	if err != nil {
		t.Fatal(err)
	}
	if values["CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID"] != "abc" {
		t.Fatalf("access key = %q", values["CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID"])
	}
	if values["CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY"] != "def" {
		t.Fatalf("secret key = %q", values["CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY"])
	}
}

func TestParentCredentialsDeriveS3SecretFromAPIToken(t *testing.T) {
	creds, err := parentCredentialsFromValues(map[string]string{
		"token_id":  "token-id",
		"api_token": "token-value",
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "token-id" {
		t.Fatalf("access key = %q", creds.AccessKeyID)
	}
	if creds.APIToken != "token-value" {
		t.Fatalf("api token = %q", creds.APIToken)
	}
	if creds.SecretAccessKey == "" || strings.Contains(creds.SecretAccessKey, "token-value") {
		t.Fatalf("secret access key was not derived safely: %q", creds.SecretAccessKey)
	}
}

func TestWriteParentCredentialsToOpenBao(t *testing.T) {
	const secretPath = "kv-controller/data/integrations/cloudflare/r2-admin"
	var gotBody struct {
		Data map[string]string `json:"data"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/"+secretPath {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("X-Vault-Token") != "openbao-token" {
			t.Fatalf("token header = %q", r.Header.Get("X-Vault-Token"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()
	t.Setenv("BAO_TOKEN", "openbao-token")

	err := WriteParentCredentialsToOpenBao(context.Background(), ParentCredentialConfig{
		OpenBaoAddr: server.URL,
		OpenBaoPath: secretPath,
		Timeout:     time.Second,
	}, map[string]string{
		"api_token": "parent-token",
		"token_id":  "parent-token-id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody.Data["api_token"] != "parent-token" || gotBody.Data["token_id"] != "parent-token-id" {
		t.Fatalf("body = %+v", gotBody)
	}
}

func TestLoadParentCredentialsDerivesAccessKeyIDFromAPIToken(t *testing.T) {
	const accountID = "c3eaeffaadf7d4847684d4775c16d598"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/"+accountID+"/tokens/verify" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token-value" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{
			"success": true,
			"result": {
				"id": "verified-token-id",
				"status": "active"
			}
		}`))
	}))
	defer server.Close()
	oldBase := cloudflareAPIBase
	cloudflareAPIBase = server.URL
	t.Cleanup(func() { cloudflareAPIBase = oldBase })
	t.Setenv("CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID", "")
	t.Setenv("CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY", "")
	t.Setenv("CLOUDFLARE_R2_ADMIN_API_TOKEN", "token-value")

	creds, err := LoadParentCredentials(context.Background(), ParentCredentialConfig{
		Source:    ParentCredentialSourceEnv,
		AccountID: accountID,
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "verified-token-id" {
		t.Fatalf("access key = %q", creds.AccessKeyID)
	}
	if creds.SecretAccessKey == "" || strings.Contains(creds.SecretAccessKey, "token-value") {
		t.Fatalf("secret access key was not derived safely: %q", creds.SecretAccessKey)
	}
}
