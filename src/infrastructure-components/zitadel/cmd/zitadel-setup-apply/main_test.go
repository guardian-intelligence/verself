package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeExternalDomain(t *testing.T) {
	input := []byte(`# Zitadel configuration.
# Managed by Ansible - do not edit manually.

Port: 8085
ExternalDomain: auth.example.test
ExternalPort: 443
`)
	got, err := normalizeExternalDomain(input, "example.test")
	if err != nil {
		t.Fatalf("normalize external domain: %v", err)
	}
	if !bytes.Contains(got, []byte("ExternalDomain: example.test\n")) {
		t.Fatalf("external domain was not rewritten:\n%s", string(got))
	}
	if bytes.Contains(got, []byte("auth.example.test")) {
		t.Fatalf("legacy auth domain remained:\n%s", string(got))
	}
	if !bytes.Contains(got, []byte("Managed by Nomad")) {
		t.Fatalf("management marker was not updated:\n%s", string(got))
	}
	again, err := normalizeExternalDomain(got, "example.test")
	if err != nil {
		t.Fatalf("normalize twice: %v", err)
	}
	if !bytes.Equal(again, got) {
		t.Fatalf("normalization is not idempotent:\nonce:\n%s\n\ntwice:\n%s", string(got), string(again))
	}
}

func TestNormalizeExternalDomainRequiresKey(t *testing.T) {
	if _, err := normalizeExternalDomain([]byte("Port: 8085\n"), "example.test"); err == nil {
		t.Fatal("expected missing ExternalDomain to fail")
	}
}

func TestRenderDiscoveryHosts(t *testing.T) {
	got := string(renderDiscoveryHosts("example.test"))
	want := "127.0.0.1 localhost\n::1 localhost ip6-localhost ip6-loopback\n127.0.0.1 example.test\n"
	if got != want {
		t.Fatalf("hosts file:\n%s", got)
	}
}

func TestNormalizeSecretFileTrimsTemplateNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "masterkey")
	if err := os.WriteFile(path, []byte("01234567890123456789012345678901\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := normalizeSecretFile(path, os.Getuid(), os.Getgid(), 0o600); err != nil {
		t.Fatalf("normalize secret: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "01234567890123456789012345678901" {
		t.Fatalf("secret file was not trimmed: %q", string(got))
	}
}

func TestPublishAdminPATAcceptsPreviouslyPublishedOpenBaoPAT(t *testing.T) {
	values := map[string]string{
		"/v1/kv-runtime/data/secret/org/auth-control-plane.zitadel.admin_token": "existing-pat",
		"/v1/kv-runtime/data/secret/org/iam-service.zitadel.admin_token":        "existing-pat",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		value, ok := values[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]string{"value": value}},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()
	cfg := config{
		adminPATPath: filepath.Join(t.TempDir(), "missing-admin.pat"),
		adminPATSecrets: []string{
			"auth-control-plane.zitadel.admin_token",
			"iam-service.zitadel.admin_token",
		},
		openBaoAddr:  server.URL,
		openBaoToken: "token",
	}
	var stdout bytes.Buffer
	if err := publishAdminPAT(context.Background(), cfg, &stdout); err != nil {
		t.Fatalf("publish admin PAT: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "already present in OpenBao") {
		t.Fatalf("expected OpenBao presence output, got %q", got)
	}
}
