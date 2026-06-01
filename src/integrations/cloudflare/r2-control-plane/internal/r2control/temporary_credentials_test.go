package r2control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateTemporaryCredentials(t *testing.T) {
	var gotPath string
	var gotBody struct {
		Bucket            string   `json:"bucket"`
		ParentAccessKeyID string   `json:"parentAccessKeyId"`
		Permission        string   `json:"permission"`
		TTLSeconds        int64    `json:"ttlSeconds"`
		Prefixes          []string `json:"prefixes"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer parent-api-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{
			"success": true,
			"result": {
				"accessKeyId": "temporary-access",
				"secretAccessKey": "temporary-secret",
				"sessionToken": "temporary-session"
			}
		}`))
	}))
	defer server.Close()

	client := &CloudflareAPIClient{
		apiBase: server.URL,
		token:   "parent-api-token",
		http:    server.Client(),
	}
	creds, err := client.CreateTemporaryCredentials(context.Background(), "c3eaeffaadf7d4847684d4775c16d598", TemporaryCredentialRequest{
		ParentAccessKeyID: "parent-access",
		Bucket:            "nomad-artifacts-gamma",
		Permission:        TemporaryPermissionObjectReadWrite,
		TTL:               30 * time.Minute,
		Prefixes:          []string{"/sha256/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/accounts/c3eaeffaadf7d4847684d4775c16d598/r2/temp-access-credentials" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody.Bucket != "nomad-artifacts-gamma" || gotBody.ParentAccessKeyID != "parent-access" || gotBody.Permission != TemporaryPermissionObjectReadWrite || gotBody.TTLSeconds != 1800 {
		t.Fatalf("body = %+v", gotBody)
	}
	if len(gotBody.Prefixes) != 1 || gotBody.Prefixes[0] != "sha256/" {
		t.Fatalf("prefixes = %#v", gotBody.Prefixes)
	}
	if creds.AccessKeyID != "temporary-access" || creds.SecretAccessKey != "temporary-secret" || creds.SessionToken != "temporary-session" {
		t.Fatalf("credentials = %+v", creds)
	}
}

func TestCreateR2AllBucketsTokenUsesBucketPermissionOnAccountResource(t *testing.T) {
	const accountID = "c3eaeffaadf7d4847684d4775c16d598"
	var tokenBody struct {
		Name     string `json:"name"`
		Policies []struct {
			Resources        map[string]map[string]string `json:"resources"`
			PermissionGroups []map[string]string          `json:"permission_groups"`
		} `json:"policies"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/accounts/" + accountID + "/tokens/permission_groups":
			if r.URL.Query().Get("scope") != "com.cloudflare.edge.r2.bucket" {
				t.Fatalf("permission group scope = %q", r.URL.Query().Get("scope"))
			}
			_, _ = w.Write([]byte(`{
				"success": true,
				"result": [{
					"id": "permission-group-id",
					"name": "Workers R2 Storage Bucket Item Write",
					"scopes": ["com.cloudflare.edge.r2.bucket"]
				}]
			}`))
		case "/accounts/" + accountID + "/tokens":
			if err := json.NewDecoder(r.Body).Decode(&tokenBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{
				"success": true,
				"result": {
					"id": "created-token-id",
					"name": "test-token",
					"value": "created-token-value"
				}
			}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &CloudflareAPIClient{
		apiBase: server.URL,
		token:   "parent-api-token",
		http:    server.Client(),
	}
	token, err := client.CreateR2AllBucketsToken(context.Background(), accountID, "test-token", PermissionR2BucketItemWrite)
	if err != nil {
		t.Fatal(err)
	}
	if token.S3AccessKeyID != "created-token-id" || token.S3SecretKey == "" {
		t.Fatalf("token = %+v", token)
	}
	if tokenBody.Name != "test-token" || len(tokenBody.Policies) != 1 {
		t.Fatalf("body = %+v", tokenBody)
	}
	accountResource := "com.cloudflare.api.account." + accountID
	if tokenBody.Policies[0].Resources[accountResource]["com.cloudflare.edge.r2.bucket.*"] != "*" {
		t.Fatalf("resources = %#v", tokenBody.Policies[0].Resources)
	}
	if tokenBody.Policies[0].PermissionGroups[0]["id"] != "permission-group-id" {
		t.Fatalf("permission groups = %#v", tokenBody.Policies[0].PermissionGroups)
	}
}
