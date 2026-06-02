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
		Bucket:            "verself-deployment-artifacts",
		Permission:        TemporaryPermissionObjectReadWrite,
		TTL:               15 * time.Minute,
		Objects:           []string{"/gamma/sha256/abc/service.tar"},
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
	if claims.Bucket != "verself-deployment-artifacts" || claims.Scope != TemporaryPermissionObjectReadWrite {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.Sub != "c3eaeffaadf7d4847684d4775c16d598" || claims.Iss != "parent-access" || claims.Aud != "c3eaeffaadf7d4847684d4775c16d598.r2.cloudflarestorage.com" {
		t.Fatalf("identity claims = %+v", claims)
	}
	if claims.Iat != now.Unix() || claims.Exp != now.Add(15*time.Minute).Unix() {
		t.Fatalf("time claims = %+v", claims)
	}
	if len(claims.Paths.ObjectPaths) != 1 || claims.Paths.ObjectPaths[0] != "gamma/sha256/abc/service.tar" {
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

func TestCreateTemporaryCredentialsUsesCloudflareAPI(t *testing.T) {
	const accountID = "c3eaeffaadf7d4847684d4775c16d598"
	var body struct {
		Bucket            string   `json:"bucket"`
		ParentAccessKeyID string   `json:"parentAccessKeyId"`
		Permission        string   `json:"permission"`
		TTLSeconds        int64    `json:"ttlSeconds"`
		Objects           []string `json:"objects"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/accounts/"+accountID+"/r2/temp-access-credentials" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer publisher-api-token" {
			t.Fatalf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{
			"success": true,
			"result": {
				"accessKeyId": "tmp-access",
				"secretAccessKey": "tmp-secret",
				"sessionToken": "tmp-session"
			}
		}`))
	}))
	defer server.Close()

	client := &CloudflareAPIClient{
		apiBase: server.URL,
		token:   "publisher-api-token",
		http:    server.Client(),
	}
	creds, err := client.CreateTemporaryCredentials(context.Background(), accountID, TemporaryCredentialRequest{
		ParentAccessKeyID: "publisher-token-id",
		Bucket:            "verself-deployment-artifacts",
		Permission:        TemporaryPermissionObjectReadWrite,
		TTL:               15 * time.Minute,
		Objects:           []string{"/gamma/sha256/abc/service.tar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "tmp-access" || creds.SecretAccessKey != "tmp-secret" || creds.SessionToken != "tmp-session" {
		t.Fatalf("creds = %+v", creds)
	}
	if body.Bucket != "verself-deployment-artifacts" || body.ParentAccessKeyID != "publisher-token-id" || body.Permission != TemporaryPermissionObjectReadWrite {
		t.Fatalf("body = %+v", body)
	}
	if body.TTLSeconds != int64((15*time.Minute)/time.Second) {
		t.Fatalf("ttlSeconds = %d", body.TTLSeconds)
	}
	if len(body.Objects) != 1 || body.Objects[0] != "gamma/sha256/abc/service.tar" {
		t.Fatalf("objects = %#v", body.Objects)
	}
}
