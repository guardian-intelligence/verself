package r2control

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
)

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
	const secretPath = "kv-controller/data/integrations/cloudflare/r2/capabilities/deployment-publisher"
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
	tokenFile := writeCredentialTestFile(t, "openbao-token", "openbao-token\n")

	err := WriteParentCredentialsToOpenBao(context.Background(), ParentCredentialConfig{
		OpenBaoAddr:      server.URL,
		OpenBaoPath:      secretPath,
		OpenBaoTokenFile: tokenFile,
		Timeout:          time.Second,
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

func TestLoadParentCredentialsFromOpenBaoDerivesAccessKeyIDFromAPIToken(t *testing.T) {
	const accountID = "c3eaeffaadf7d4847684d4775c16d598"
	const secretPath = "kv-controller/data/integrations/cloudflare/r2/capabilities/deployment-publisher"
	openBao := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/"+secretPath {
			t.Fatalf("openbao path = %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("openbao method = %s", r.Method)
		}
		if r.Header.Get("X-Vault-Token") != "openbao-token" {
			t.Fatalf("openbao token header = %q", r.Header.Get("X-Vault-Token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"data":{"api_token":"token-value"}}}`))
	}))
	defer openBao.Close()
	cloudflare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer cloudflare.Close()
	oldBase := cloudflareAPIBase
	cloudflareAPIBase = cloudflare.URL
	t.Cleanup(func() { cloudflareAPIBase = oldBase })
	tokenFile := writeCredentialTestFile(t, "openbao-token", "openbao-token\n")

	creds, err := LoadParentCredentials(context.Background(), ParentCredentialConfig{
		AccountID:        accountID,
		OpenBaoAddr:      openBao.URL,
		OpenBaoPath:      secretPath,
		OpenBaoTokenFile: tokenFile,
		Timeout:          time.Second,
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

func writeCredentialTestFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
