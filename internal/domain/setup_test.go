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

func (m *mockPrompter) AskWithDefault(prompt, current string) string {
	a := m.Ask(prompt)
	if a == "" {
		return current
	}
	return a
}
func (m *mockPrompter) AskSecret(prompt string) string                      { return m.Ask(prompt) }
func (m *mockPrompter) Confirm(prompt string) bool                          { return m.Ask(prompt) == "y" }
func (m *mockPrompter) Select(prompt string, options []string) (int, string) { return 0, options[0] }

// --- Helper to create a mock Cloudflare API server ---

func validCFServer(domain string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"success": true,
			"errors":  []interface{}{},
			"result": []map[string]interface{}{
				{
					"id":          "abc123",
					"name":        domain,
					"status":      "active",
					"permissions": []string{"#dns_records:edit", "#dns_records:read", "#zone:read"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func invalidCFServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"success": false,
			"errors":  []map[string]interface{}{{"code": 6003, "message": "Invalid request headers"}},
			"result":  []interface{}{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

// --- Pure function tests ---

func TestMaskToken(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "****"},
		{"short", "****"},
		{"1234567", "****"},
		{"12345678", "123****5678"},
		{"cf-abcdefghijklmnop", "cf-****mnop"},
	}
	for _, tt := range tests {
		got := maskToken(tt.in)
		if got != tt.want {
			t.Errorf("maskToken(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestReadExistingDomain(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"present", `forge_metal_domain: "anveio.com"`, "anveio.com"},
		{"empty quotes", `forge_metal_domain: ""`, ""},
		{"missing", `forge_metal_version: "0.1.0"`, ""},
		{"with surrounding", "foo: bar\nforge_metal_domain: \"test.dev\"\nbaz: 1\n", "test.dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "main.yml")
			os.WriteFile(path, []byte(tt.content), 0644)

			got := readExistingDomain(path)
			if got != tt.want {
				t.Errorf("readExistingDomain() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadExistingDomain_NoFile(t *testing.T) {
	got := readExistingDomain("/nonexistent/path/main.yml")
	if got != "" {
		t.Errorf("expected empty for nonexistent file, got %q", got)
	}
}

func TestWriteDomain_Replace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.yml")

	content := "forge_metal_version: \"0.1.0\"\nforge_metal_domain: \"old.com\"\n"
	os.WriteFile(path, []byte(content), 0644)

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
	os.WriteFile(path, []byte(content), 0644)

	if err := writeDomain(path, "example.com"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `forge_metal_domain: "example.com"`) {
		t.Fatalf("expected domain appended, got: %s", data)
	}
}

// --- Integration tests for Run() ---

func setupTestFiles(t *testing.T, domain, token string) (varsFile, secretsFile string, dir string) {
	t.Helper()
	dir = t.TempDir()
	varsFile = filepath.Join(dir, "main.yml")
	secretsFile = filepath.Join(dir, "secrets.sops.yml")

	varsContent := `forge_metal_version: "0.1.0"` + "\n"
	if domain != "" {
		varsContent += `forge_metal_domain: "` + domain + `"` + "\n"
	}
	os.WriteFile(varsFile, []byte(varsContent), 0644)
	os.WriteFile(secretsFile, []byte("{}"), 0644)

	return varsFile, secretsFile, dir
}

func makeConfig(varsFile, secretsFile, cfURL, token string) Config {
	return Config{
		AnsibleVars: varsFile,
		SecretsFile: secretsFile,
		CFBaseURL:   cfURL,
		ReadToken: func(string) string {
			return token
		},
		WriteToken: func(string, string) error {
			return nil
		},
	}
}

func TestRun_AllConfigured(t *testing.T) {
	srv := validCFServer("anveio.com")
	defer srv.Close()

	varsFile, secretsFile, _ := setupTestFiles(t, "anveio.com", "")
	cfg := makeConfig(varsFile, secretsFile, srv.URL, "test-token-abc123")

	p := &mockPrompter{} // no answers needed — should not prompt
	var buf bytes.Buffer

	err := Run(cfg, p, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "2/2 configured") {
		t.Errorf("expected 2/2 configured, got:\n%s", output)
	}
	if !strings.Contains(output, "All fields configured") {
		t.Errorf("expected 'All fields configured', got:\n%s", output)
	}
	if !strings.Contains(output, "anveio.com") {
		t.Errorf("expected domain in output, got:\n%s", output)
	}
	// Should not have prompted
	if p.idx != 0 {
		t.Errorf("expected no prompts, but %d answers were consumed", p.idx)
	}
}

func TestRun_MissingDomain(t *testing.T) {
	srv := validCFServer("new.dev")
	defer srv.Close()

	varsFile, secretsFile, _ := setupTestFiles(t, "", "")
	cfg := makeConfig(varsFile, secretsFile, srv.URL, "test-token-abc123")

	// Prompt will ask for domain, then token will be validated with that domain.
	// But token was read from disk and domain was unknown, so token shows as "needs domain".
	// After domain is entered, token will be re-validated.
	// However, the CF server validates for "new.dev", so the existing token (read from disk)
	// needs to be re-validated against the new domain.
	p := &mockPrompter{answers: []string{"new.dev"}}
	var buf bytes.Buffer

	err := Run(cfg, p, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "new.dev") {
		t.Errorf("expected domain in output, got:\n%s", output)
	}
	// Domain was written
	data, _ := os.ReadFile(varsFile)
	if !strings.Contains(string(data), `forge_metal_domain: "new.dev"`) {
		t.Errorf("expected domain written to file, got: %s", data)
	}
}

func TestRun_MissingToken(t *testing.T) {
	srv := validCFServer("anveio.com")
	defer srv.Close()

	varsFile, secretsFile, _ := setupTestFiles(t, "anveio.com", "")
	cfg := makeConfig(varsFile, secretsFile, srv.URL, "") // no token on disk

	p := &mockPrompter{answers: []string{"new-valid-token123"}}
	var buf bytes.Buffer

	err := Run(cfg, p, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "✗ missing") {
		t.Errorf("expected missing marker for token, got:\n%s", output)
	}
	if !strings.Contains(output, "✓ valid") {
		t.Errorf("expected valid after prompt, got:\n%s", output)
	}
}

func TestRun_InvalidTokenOnDisk(t *testing.T) {
	varsFile, secretsFile, _ := setupTestFiles(t, "anveio.com", "")

	// Server discriminates by token value — "bad-token" is rejected, "good-token-12345678" is accepted.
	combinedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "Bearer good-token-12345678" {
			resp := map[string]interface{}{
				"success": true,
				"errors":  []interface{}{},
				"result": []map[string]interface{}{
					{
						"id":          "abc123",
						"name":        "anveio.com",
						"status":      "active",
						"permissions": []string{"#dns_records:edit", "#dns_records:read", "#zone:read"},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			resp := map[string]interface{}{
				"success": false,
				"errors":  []map[string]interface{}{{"code": 6003, "message": "Invalid request headers"}},
				"result":  []interface{}{},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer combinedSrv.Close()

	cfg := makeConfig(varsFile, secretsFile, combinedSrv.URL, "bad-token")
	p := &mockPrompter{answers: []string{"good-token-12345678"}}
	var buf bytes.Buffer

	err := Run(cfg, p, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "✗ invalid") {
		t.Errorf("expected invalid marker for bad token, got:\n%s", output)
	}
}

func TestRun_Headless(t *testing.T) {
	srv := validCFServer("headless.dev")
	defer srv.Close()

	varsFile, secretsFile, _ := setupTestFiles(t, "", "")
	cfg := makeConfig(varsFile, secretsFile, srv.URL, "")
	cfg.Domain = "headless.dev"
	cfg.Token = "headless-token-12345678"

	p := &mockPrompter{} // should not be used
	var buf bytes.Buffer

	err := Run(cfg, p, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "✓ valid") {
		t.Errorf("expected valid token confirmation, got:\n%s", output)
	}
	if !strings.Contains(output, "headless.dev") {
		t.Errorf("expected domain in summary, got:\n%s", output)
	}
	if !strings.Contains(output, "Updated") {
		t.Errorf("expected Updated lines, got:\n%s", output)
	}
	if p.idx != 0 {
		t.Errorf("expected no prompts in headless mode, but %d answers consumed", p.idx)
	}

	// Verify domain was written to file
	data, _ := os.ReadFile(varsFile)
	if !strings.Contains(string(data), `forge_metal_domain: "headless.dev"`) {
		t.Errorf("expected domain written, got: %s", data)
	}
}

func TestRun_HeadlessInvalidToken(t *testing.T) {
	srv := invalidCFServer()
	defer srv.Close()

	varsFile, secretsFile, _ := setupTestFiles(t, "", "")
	cfg := makeConfig(varsFile, secretsFile, srv.URL, "")
	cfg.Domain = "test.com"
	cfg.Token = "bad-token"

	p := &mockPrompter{}
	var buf bytes.Buffer

	err := Run(cfg, p, &buf)
	if err == nil {
		t.Fatal("expected error for invalid token in headless mode")
	}
	if !strings.Contains(err.Error(), "token validation failed") {
		t.Errorf("expected token validation error, got: %v", err)
	}
}

func TestRun_PartialFlags_DomainOnly(t *testing.T) {
	srv := validCFServer("partial.dev")
	defer srv.Close()

	varsFile, secretsFile, _ := setupTestFiles(t, "", "")
	cfg := makeConfig(varsFile, secretsFile, srv.URL, "")
	cfg.Domain = "partial.dev" // domain via flag, no token

	p := &mockPrompter{answers: []string{"user-entered-token123"}}
	var buf bytes.Buffer

	err := Run(cfg, p, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should have prompted for token (at least 1 answer consumed)
	if p.idx == 0 {
		t.Error("expected prompt for token, but no answers consumed")
	}
	if !strings.Contains(output, "partial.dev") {
		t.Errorf("expected domain in output, got:\n%s", output)
	}
}

func TestRun_NoSOPS(t *testing.T) {
	cfg := Config{
		AnsibleVars: "/tmp/main.yml",
		SecretsFile: "/nonexistent/secrets.sops.yml",
	}

	p := &mockPrompter{}
	var buf bytes.Buffer

	err := Run(cfg, p, &buf)
	if err == nil {
		t.Fatal("expected error for missing secrets file")
	}
	if !strings.Contains(err.Error(), "setup-sops") {
		t.Errorf("expected setup-sops hint, got: %v", err)
	}
}
