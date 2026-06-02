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

func TestCreateR2AllBucketsTokenUsesBucketPermissionOnAccountResource(t *testing.T) {
	const accountID = "c3eaeffaadf7d4847684d4775c16d598"
	var tokenBody struct {
		Name      string `json:"name"`
		ExpiresOn string `json:"expires_on"`
		Policies  []struct {
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
	expiresOn := time.Unix(1_786_086_400, 0).UTC()
	token, err := client.CreateR2AllBucketsToken(context.Background(), accountID, "test-token", PermissionR2BucketItemWrite, expiresOn)
	if err != nil {
		t.Fatal(err)
	}
	if token.S3AccessKeyID != "created-token-id" || token.S3SecretKey == "" {
		t.Fatalf("token = %+v", token)
	}
	if tokenBody.Name != "test-token" || len(tokenBody.Policies) != 1 {
		t.Fatalf("body = %+v", tokenBody)
	}
	if tokenBody.ExpiresOn != expiresOn.Format(time.RFC3339) {
		t.Fatalf("expires_on = %q", tokenBody.ExpiresOn)
	}
	accountResource := "com.cloudflare.api.account." + accountID
	if tokenBody.Policies[0].Resources[accountResource]["com.cloudflare.edge.r2.bucket.*"] != "*" {
		t.Fatalf("resources = %#v", tokenBody.Policies[0].Resources)
	}
	if tokenBody.Policies[0].PermissionGroups[0]["id"] != "permission-group-id" {
		t.Fatalf("permission groups = %#v", tokenBody.Policies[0].PermissionGroups)
	}
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

func TestAccountTokenUpdateRollAndDelete(t *testing.T) {
	const accountID = "c3eaeffaadf7d4847684d4775c16d598"
	const tokenID = "created-token-id"
	expiresOn := time.Unix(1_786_086_400, 0).UTC()
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/"+accountID+"/tokens/"+tokenID:
			seen["details"] = true
			_, _ = w.Write([]byte(`{
				"success": true,
				"result": {
					"id": "created-token-id",
					"name": "managed-child-token",
					"status": "active",
					"expires_on": "2026-06-08T00:00:00Z",
					"policies": [{
						"effect": "allow",
						"permission_groups": [{"id": "permission-group-id"}],
						"resources": {"com.cloudflare.api.account.c3eaeffaadf7d4847684d4775c16d598": "*"}
					}]
				}
			}`))
		case r.Method == http.MethodPut && r.URL.Path == "/accounts/"+accountID+"/tokens/"+tokenID:
			seen["update"] = true
			var body AccountToken
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Name != "managed-child-token" || body.ExpiresOn != expiresOn.Format(time.RFC3339) || len(body.Policies) != 1 {
				t.Fatalf("update body = %+v", body)
			}
			_ = json.NewEncoder(w).Encode(tokenUpdateResponse{
				Success: true,
				Result: AccountToken{
					ID:        tokenID,
					Name:      body.Name,
					Status:    "active",
					Policies:  body.Policies,
					ExpiresOn: body.ExpiresOn,
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/accounts/"+accountID+"/tokens/"+tokenID+"/value":
			seen["roll"] = true
			_ = json.NewEncoder(w).Encode(tokenRollValueResponse{Success: true, Result: "new-token-value"})
		case r.Method == http.MethodDelete && r.URL.Path == "/accounts/"+accountID+"/tokens/"+tokenID:
			seen["delete"] = true
			_, _ = w.Write([]byte(`{"success": true, "result": {"id": "created-token-id"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := &CloudflareAPIClient{
		apiBase: server.URL,
		token:   "parent-api-token",
		http:    server.Client(),
	}
	token, err := client.GetAccountToken(context.Background(), accountID, tokenID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := client.UpdateAccountTokenExpiresOn(context.Background(), accountID, token, expiresOn)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ExpiresOn != expiresOn.Format(time.RFC3339) {
		t.Fatalf("updated expires_on = %q", updated.ExpiresOn)
	}
	value, err := client.RollAccountTokenValue(context.Background(), accountID, tokenID)
	if err != nil {
		t.Fatal(err)
	}
	if value != "new-token-value" {
		t.Fatalf("rolled value = %q", value)
	}
	if err := client.DeleteAccountToken(context.Background(), accountID, tokenID); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"details", "update", "roll", "delete"} {
		if !seen[key] {
			t.Fatalf("did not see %s request", key)
		}
	}
}

func TestCreateZoneTokenWithPermissionsScopesSpecificZones(t *testing.T) {
	const accountID = "c3eaeffaadf7d4847684d4775c16d598"
	expiresOn := time.Unix(1_786_086_400, 0).UTC()
	seenCreate := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/"+accountID+"/tokens/permission_groups":
			name := r.URL.Query().Get("name")
			if r.URL.Query().Get("scope") != "com.cloudflare.api.account.zone" {
				t.Fatalf("permission scope = %q", r.URL.Query().Get("scope"))
			}
			_ = json.NewEncoder(w).Encode(permissionGroupsResponse{
				Success: true,
				Result: []permissionGroup{{
					ID:     "permission-" + strings.ToLower(strings.ReplaceAll(name, " ", "-")),
					Name:   name,
					Scopes: []string{"com.cloudflare.api.account.zone"},
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/accounts/"+accountID+"/tokens":
			seenCreate = true
			var body struct {
				Name      string        `json:"name"`
				Policies  []tokenPolicy `json:"policies"`
				ExpiresOn string        `json:"expires_on"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Name != "verself-gamma-dns" || body.ExpiresOn != expiresOn.Format(time.RFC3339) || len(body.Policies) != 1 {
				t.Fatalf("create body = %+v", body)
			}
			resources := body.Policies[0].Resources
			if resources["com.cloudflare.api.account.zone.zone-product"] != "*" || resources["com.cloudflare.api.account.zone.zone-company"] != "*" {
				t.Fatalf("resources = %#v", resources)
			}
			if len(body.Policies[0].PermissionGroups) != 2 {
				t.Fatalf("permission_groups = %#v", body.Policies[0].PermissionGroups)
			}
			_ = json.NewEncoder(w).Encode(tokenCreateResponse{
				Success: true,
				Result: struct {
					ID        string        `json:"id"`
					Name      string        `json:"name"`
					Value     string        `json:"value"`
					ExpiresOn string        `json:"expires_on"`
					Policies  []tokenPolicy `json:"policies"`
				}{
					ID:        "created-token-id",
					Name:      body.Name,
					Value:     "created-token-value",
					ExpiresOn: body.ExpiresOn,
					Policies:  body.Policies,
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client := &CloudflareAPIClient{
		apiBase: server.URL,
		token:   "parent-api-token",
		http:    server.Client(),
	}
	token, err := client.CreateZoneTokenWithPermissions(context.Background(), accountID, []string{"zone-product", "zone-company"}, "verself-gamma-dns", []string{
		PermissionZoneRead,
		PermissionDNSWrite,
	}, expiresOn)
	if err != nil {
		t.Fatal(err)
	}
	if !seenCreate {
		t.Fatal("did not see create request")
	}
	if token.ID != "created-token-id" || token.Value != "created-token-value" || token.PermissionGroup != "Zone Read,DNS Write" {
		t.Fatalf("token = %+v", token)
	}
}
