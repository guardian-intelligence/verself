package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostSecretsYAML(t *testing.T) {
	body, err := hostSecretsYAML()
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, key := range []string{
		"cloudflare_api_token:",
		"postgresql_admin_password:",
		"zitadel_masterkey:",
		"github_integration_service_github_app_webhook_secret:",
		"postgresql_iam_service_password:",
	} {
		if !strings.Contains(text, key) {
			t.Fatalf("expected %q in generated secrets:\n%s", key, text)
		}
	}
	if !strings.Contains(text, "zitadel_admin_password: ") || !strings.Contains(text, "#@F1") {
		t.Fatalf("expected Zitadel password complexity suffix in generated secrets:\n%s", text)
	}
}

func TestAgeRecipient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.txt")
	if err := os.WriteFile(path, []byte("# public key: age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqql9p5x\nAGE-SECRET-KEY-1...\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recipient, err := ageRecipient(path)
	if err != nil {
		t.Fatal(err)
	}
	if recipient != "age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqql9p5x" {
		t.Fatalf("recipient = %q", recipient)
	}
}

func TestWriteSOPSConfig(t *testing.T) {
	dir := t.TempDir()
	if err := writeSOPSConfig(dir, "age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqql9p5x"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".sops.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "path_regex: \\.sops\\.yml$") {
		t.Fatalf("unexpected .sops.yaml:\n%s", text)
	}
}
