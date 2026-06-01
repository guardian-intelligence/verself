package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadServerAuthTokenCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r2-control-plane", "gamma.token")

	token, err := loadServerAuthToken(config{authTokenFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 32 {
		t.Fatalf("token length = %d", len(token))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %o", info.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != token {
		t.Fatal("token file did not contain generated token")
	}
	again, err := loadServerAuthToken(config{authTokenFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if again != token {
		t.Fatal("existing token was not reused")
	}
}

func TestUploadServerRequiresBearerToken(t *testing.T) {
	server := uploadServer{authToken: "expected"}
	if server.authorized(httptest.NewRequest("POST", "/", nil)) {
		t.Fatal("request without bearer token was authorized")
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
	if _, err := readParentAPIToken(path); err == nil {
		t.Fatal("expected whitespace-separated token to fail")
	}
}
