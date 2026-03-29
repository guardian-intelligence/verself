package provision

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

	"github.com/forge-metal/forge-metal/internal/config"
)

// mockPrompter returns canned answers in sequence.
type mockPrompter struct {
	answers []string
	idx     int
}

func (m *mockPrompter) next() string {
	if m.idx >= len(m.answers) {
		return ""
	}
	a := m.answers[m.idx]
	m.idx++
	return a
}

func (m *mockPrompter) Ask(prompt string) string       { return m.next() }
func (m *mockPrompter) AskSecret(prompt string) string  { return m.next() }
func (m *mockPrompter) Confirm(prompt string) bool      { return m.next() == "y" }
func (m *mockPrompter) Select(prompt string, options []string) (int, string) {
	a := m.next()
	// Support "0", "1", etc. for index selection.
	for i, o := range options {
		if strings.HasPrefix(o, a) {
			return i, o
		}
	}
	return 0, options[0]
}

// latitudeAPI returns a test HTTP server that handles the Latitude.sh API endpoints
// used by the wizard.
func latitudeAPI() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/auth/current_user":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"id": "user-1",
					"attributes": map[string]interface{}{
						"email": "test@example.com",
					},
				},
			})

		case r.URL.Path == "/projects":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id": "proj-1",
						"attributes": map[string]interface{}{
							"name": "My Project",
							"slug": "my-project",
						},
					},
				},
			})

		case r.URL.Path == "/regions":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id": "reg-1",
						"attributes": map[string]interface{}{
							"slug":    "ASH",
							"city":    "Ashburn",
							"country": "US",
						},
					},
					{
						"id": "reg-2",
						"attributes": map[string]interface{}{
							"slug":    "DAL",
							"city":    "Dallas",
							"country": "US",
						},
					},
				},
			})

		case r.URL.Path == "/plans":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id": "plan-1",
						"attributes": map[string]interface{}{
							"name": "Large Server",
							"slug": "s3-large-x86",
						},
					},
				},
			})

		default:
			http.NotFound(w, r)
		}
	}))
}

func TestWizard_HappyPath(t *testing.T) {
	srv := latitudeAPI()
	defer srv.Close()

	// Create a temp SSH key.
	dir := t.TempDir()
	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)

	tfDir := filepath.Join(dir, "terraform")
	os.MkdirAll(tfDir, 0755)

	cfg := &config.Config{}
	cfg.Latitude.AuthToken = "test-token"
	cfg.Latitude.Region = "ASH"
	cfg.Latitude.Plan = "s3-large-x86"
	cfg.Latitude.OperatingSystem = "ubuntu_24_04_x64_lts"
	cfg.Latitude.Billing = "hourly"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			"Latitude.sh",   // S0: select provider
			// S1: token from config (no prompt)
			// S2: auto-selected (single project)
			// S3: SSH key exists (no prompt)
			"ASH",           // S4: select region
			"s3-large-x86",  // S5: select plan
			"y",             // S6: confirm
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg:          cfg,
		Prompter:     prompter,
		Out:          &buf,
		TerraformDir: tfDir,
		LatBaseURL:   srv.URL,
	}

	// Run stops at S8 (provision) since tofu isn't installed in tests.
	// We verify S0–S7 work correctly.
	err := w.Run(context.Background())

	output := buf.String()

	// Verify we got through the wizard steps.
	if !strings.Contains(output, "Authenticated as test@example.com") {
		t.Errorf("expected auth confirmation, got:\n%s", output)
	}
	if !strings.Contains(output, "My Project") {
		t.Errorf("expected project name, got:\n%s", output)
	}
	if !strings.Contains(output, "Provision Summary") {
		t.Errorf("expected summary, got:\n%s", output)
	}

	// Verify tfvars was written.
	tfvarsPath := filepath.Join(tfDir, "terraform.tfvars.json")
	data, readErr := os.ReadFile(tfvarsPath)
	if readErr != nil {
		t.Fatalf("tfvars not written: %v", readErr)
	}

	var tfvars map[string]interface{}
	if err := json.Unmarshal(data, &tfvars); err != nil {
		t.Fatalf("invalid tfvars JSON: %v", err)
	}
	if tfvars["project_id"] != "proj-1" {
		t.Errorf("expected project_id=proj-1, got %v", tfvars["project_id"])
	}
	if tfvars["region"] != "ASH" {
		t.Errorf("expected region=ASH, got %v", tfvars["region"])
	}

	// The error should be from tofu not being available, not from the wizard logic.
	if err != nil && !strings.Contains(err.Error(), "tofu") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWizard_DisabledProvider(t *testing.T) {
	cfg := &config.Config{}
	prompter := &mockPrompter{
		answers: []string{"Hetzner"}, // disabled provider
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg:      cfg,
		Prompter: prompter,
		Out:      &buf,
	}

	err := w.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for disabled provider")
	}
	if !strings.Contains(err.Error(), "not yet supported") {
		t.Errorf("expected 'not yet supported' error, got: %v", err)
	}
}

func TestWizard_MissingSSHKey_GenerateDeclined(t *testing.T) {
	srv := latitudeAPI()
	defer srv.Close()

	dir := t.TempDir()
	sshPub := filepath.Join(dir, "nonexistent.pub")

	cfg := &config.Config{}
	cfg.Latitude.AuthToken = "test-token"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			"Latitude.sh",  // S0
			// S1: token from config
			// S2: auto-selected
			"n",            // S3: decline generate
			"",             // S3: empty custom path
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg:          cfg,
		Prompter:     prompter,
		Out:          &buf,
		TerraformDir: filepath.Join(dir, "tf"),
		LatBaseURL:   srv.URL,
	}

	err := w.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for missing SSH key")
	}
	if !strings.Contains(err.Error(), "SSH key is required") {
		t.Errorf("expected SSH key error, got: %v", err)
	}
}

func TestWizard_ConfirmAbort(t *testing.T) {
	srv := latitudeAPI()
	defer srv.Close()

	dir := t.TempDir()
	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)

	cfg := &config.Config{}
	cfg.Latitude.AuthToken = "test-token"
	cfg.Latitude.Region = "ASH"
	cfg.Latitude.Plan = "s3-large-x86"
	cfg.Latitude.OperatingSystem = "ubuntu_24_04_x64_lts"
	cfg.Latitude.Billing = "hourly"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			"Latitude.sh",   // S0
			"ASH",           // S4
			"s3-large-x86",  // S5
			"n",             // S6: decline
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg:          cfg,
		Prompter:     prompter,
		Out:          &buf,
		TerraformDir: filepath.Join(dir, "tf"),
		LatBaseURL:   srv.URL,
	}

	err := w.Run(context.Background())
	if err == nil || err.Error() != "aborted" {
		t.Errorf("expected 'aborted' error, got: %v", err)
	}
}

func TestWizard_InvalidToken_ThenValid(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/auth/current_user" {
			callCount++
			if callCount == 1 {
				// First call: invalid token (from config).
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error": "unauthorized"}`))
				return
			}
			// Second call: valid token (from prompt).
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"id": "user-1",
					"attributes": map[string]interface{}{
						"email": "test@example.com",
					},
				},
			})
			return
		}

		if r.URL.Path == "/projects" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id": "proj-1",
						"attributes": map[string]interface{}{
							"name": "Test",
							"slug": "test",
						},
					},
				},
			})
			return
		}

		if r.URL.Path == "/regions" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id": "reg-1",
						"attributes": map[string]interface{}{
							"slug": "ASH", "city": "Ashburn", "country": "US",
						},
					},
				},
			})
			return
		}

		if r.URL.Path == "/plans" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"id": "plan-1",
						"attributes": map[string]interface{}{
							"name": "Large", "slug": "s3-large-x86",
						},
					},
				},
			})
			return
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)

	cfg := &config.Config{}
	cfg.Latitude.AuthToken = "bad-token"
	cfg.Latitude.Region = "ASH"
	cfg.Latitude.Plan = "s3-large-x86"
	cfg.Latitude.OperatingSystem = "ubuntu_24_04_x64_lts"
	cfg.Latitude.Billing = "hourly"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			"Latitude.sh",   // S0
			"good-token",    // S1: prompted after bad token
			"ASH",           // S4
			"s3-large-x86",  // S5
			"y",             // S6
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg:          cfg,
		Prompter:     prompter,
		Out:          &buf,
		TerraformDir: filepath.Join(dir, "terraform"),
		LatBaseURL:   srv.URL,
	}

	os.MkdirAll(filepath.Join(dir, "terraform"), 0755)
	err := w.Run(context.Background())

	output := buf.String()
	if !strings.Contains(output, "Existing token is invalid") {
		t.Errorf("expected invalid token message, got:\n%s", output)
	}
	if !strings.Contains(output, "Authenticated as test@example.com") {
		t.Errorf("expected successful auth after re-prompt, got:\n%s", output)
	}

	// Should fail at tofu, not at wizard logic.
	if err != nil && !strings.Contains(err.Error(), "tofu") {
		t.Errorf("unexpected error: %v", err)
	}
}
