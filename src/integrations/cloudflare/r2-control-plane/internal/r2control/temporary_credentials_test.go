package r2control

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
