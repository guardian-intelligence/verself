package domain

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockPrompter struct {
	answers []string
	idx     int
}

func (m *mockPrompter) Ask(prompt string) string {
	if m.idx >= len(m.answers) {
		return ""
	}
	a := m.answers[m.idx]
	m.idx++
	return a
}

func (m *mockPrompter) AskSecret(prompt string) string { return m.Ask(prompt) }
func (m *mockPrompter) Confirm(prompt string) bool      { return m.Ask(prompt) == "y" }

func TestWriteDomain_Replace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.yml")

	content := "forge_metal_version: \"0.1.0\"\nforge_metal_domain: \"old.com\"\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeDomain(path, "new.com"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `forge_metal_domain: "new.com"`) {
		t.Fatalf("expected new domain, got: %s", data)
	}
	if strings.Contains(string(data), "old.com") {
		t.Fatalf("old domain still present: %s", data)
	}
}

func TestWriteDomain_Append(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.yml")

	content := "forge_metal_version: \"0.1.0\"\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeDomain(path, "example.com"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `forge_metal_domain: "example.com"`) {
		t.Fatalf("expected domain appended, got: %s", data)
	}
}

func TestRun_ValidToken(t *testing.T) {
	// Mock Cloudflare API
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"success": true,
			"errors":  []interface{}{},
			"result": []map[string]interface{}{
				{
					"id":          "abc123",
					"name":        "test.com",
					"status":      "active",
					"permissions": []string{"#dns_records:edit", "#dns_records:read", "#zone:read"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := t.TempDir()
	varsFile := filepath.Join(dir, "main.yml")
	secretsFile := filepath.Join(dir, "secrets.sops.yml")

	os.WriteFile(varsFile, []byte("forge_metal_domain: \"\"\n"), 0644)
	os.WriteFile(secretsFile, []byte("{}"), 0644)

	p := &mockPrompter{answers: []string{"test-token-123"}}
	var buf bytes.Buffer

	cfg := Config{
		Domain:      "test.com",
		AnsibleVars: varsFile,
		SecretsFile: secretsFile,
		CFBaseURL:   srv.URL,
	}

	// Run will fail at saveToken (no real sops), but we can verify the flow
	// up to the token validation step
	err := Run(cfg, p, &buf)

	output := buf.String()
	if !strings.Contains(output, "Domain: test.com") {
		t.Errorf("expected domain header, got: %s", output)
	}
	if !strings.Contains(output, "admin.test.com") {
		t.Errorf("expected subdomain listing, got: %s", output)
	}

	// The error should be from sops (not installed in test env), not from validation
	if err != nil && !strings.Contains(err.Error(), "sops") && !strings.Contains(err.Error(), "save token") {
		t.Errorf("unexpected error: %v", err)
	}
}
