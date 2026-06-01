package r2control

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateLocalTemporaryCredentials(t *testing.T) {
	now := time.Unix(1_786_000_000, 0).UTC()
	creds, err := createLocalTemporaryCredentialsAt(now, "https://c3eaeffaadf7d4847684d4775c16d598.r2.cloudflarestorage.com", "c3eaeffaadf7d4847684d4775c16d598", "parent-secret", TemporaryCredentialRequest{
		ParentAccessKeyID: "parent-access",
		Bucket:            "nomad-artifacts-gamma",
		Permission:        TemporaryPermissionObjectReadWrite,
		TTL:               15 * time.Minute,
		Objects:           []string{"/sha256/abc/service.tar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "parent-access" {
		t.Fatalf("access key = %q", creds.AccessKeyID)
	}
	rawSessionToken, err := base64.StdEncoding.DecodeString(creds.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	jwtValue := strings.TrimPrefix(string(rawSessionToken), "jwt/")
	if jwtValue == string(rawSessionToken) {
		t.Fatalf("session token prefix missing")
	}
	if creds.SecretAccessKey != SHA256Hex([]byte(jwtValue)) {
		t.Fatalf("temporary secret was not derived from signed jwt")
	}
	parts := strings.Split(jwtValue, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt parts = %d", len(parts))
	}
	mac := hmac.New(sha256.New, []byte("parent-secret"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), mustDecodeRawURL(t, parts[2])) {
		t.Fatalf("jwt signature did not verify")
	}
	var claims struct {
		Bucket string `json:"bucket"`
		Scope  string `json:"scope"`
		Paths  struct {
			ObjectPaths []string `json:"objectPaths"`
		} `json:"paths"`
		Sub string `json:"sub"`
		Iss string `json:"iss"`
		Aud string `json:"aud"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(mustDecodeRawURL(t, parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Bucket != "nomad-artifacts-gamma" || claims.Scope != TemporaryPermissionObjectReadWrite {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.Sub != "c3eaeffaadf7d4847684d4775c16d598" || claims.Iss != "parent-access" || claims.Aud != "c3eaeffaadf7d4847684d4775c16d598.r2.cloudflarestorage.com" {
		t.Fatalf("identity claims = %+v", claims)
	}
	if claims.Iat != now.Unix() || claims.Exp != now.Add(15*time.Minute).Unix() {
		t.Fatalf("time claims = %+v", claims)
	}
	if len(claims.Paths.ObjectPaths) != 1 || claims.Paths.ObjectPaths[0] != "sha256/abc/service.tar" {
		t.Fatalf("object paths = %#v", claims.Paths.ObjectPaths)
	}
}

func mustDecodeRawURL(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
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
