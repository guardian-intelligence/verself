package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	answers       []string
	idx           int
	selectPrompts []string
}

func (m *mockPrompter) next() string {
	if m.idx >= len(m.answers) {
		return ""
	}
	a := m.answers[m.idx]
	m.idx++
	return a
}

func (m *mockPrompter) Ask(prompt string) string { return m.next() }
func (m *mockPrompter) AskWithDefault(prompt, current string) string {
	a := m.next()
	if a == "" {
		return current
	}
	return a
}
func (m *mockPrompter) AskSecret(prompt string) string { return m.next() }
func (m *mockPrompter) Confirm(prompt string) bool     { return m.next() == "y" }
func (m *mockPrompter) Select(prompt string, options []string) (int, string) {
	m.selectPrompts = append(m.selectPrompts, prompt)
	a := m.next()
	for i, o := range options {
		if strings.HasPrefix(o, a) {
			return i, o
		}
	}
	return 0, options[0]
}

type testRegion struct {
	ID      string
	Slug    string
	Name    string
	Country string
}

type testPlan struct {
	ID      string
	Slug    string
	Name    string
	Regions []string
}

// latitudeAPI returns a test HTTP server that handles all Latitude.sh endpoints.
func latitudeAPI() *httptest.Server {
	return latitudeAPIWithChoices(
		[]testRegion{
			{ID: "reg-1", Slug: "ASH", Name: "Ashburn", Country: "United States"},
			{ID: "reg-2", Slug: "DAL", Name: "Dallas", Country: "United States"},
		},
		[]testPlan{
			{ID: "plan-1", Slug: "s3-large-x86", Name: "Large Server", Regions: []string{"ASH", "DAL"}},
		},
	)
}

func latitudeAPIWithChoices(regions []testRegion, plans []testPlan) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/user/profile":
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
			data := make([]map[string]interface{}, 0, len(regions))
			for _, region := range regions {
				data = append(data, map[string]interface{}{
					"id": region.ID,
					"attributes": map[string]interface{}{
						"slug": region.Slug,
						"name": region.Name,
						"country": map[string]interface{}{
							"name": region.Country,
							"slug": strings.ToLower(strings.ReplaceAll(region.Country, " ", "-")),
						},
					},
				})
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"data": data})

		case r.URL.Path == "/plans":
			data := make([]map[string]interface{}, 0, len(plans))
			for _, plan := range plans {
				data = append(data, map[string]interface{}{
					"id": plan.ID,
					"attributes": map[string]interface{}{
						"name": plan.Name,
						"slug": plan.Slug,
						"regions": []map[string]interface{}{
							{
								"locations": map[string]interface{}{
									"available": plan.Regions,
								},
							},
						},
					},
				})
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"data": data})

		default:
			http.NotFound(w, r)
		}
	}))
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

func prependPath(t *testing.T, dir string) {
	t.Helper()

	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+old); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Setenv("PATH", old); err != nil {
			t.Fatalf("restore PATH: %v", err)
		}
	})
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeFakeTofu(t *testing.T, binDir, logPath string) {
	t.Helper()

	script := fmt.Sprintf(`#!/usr/bin/env bash
set -eu
printf '%%s:%%s\n' "$1" "${LATITUDESH_AUTH_TOKEN:-}" >> %q
exit 0
`, logPath)
	writeExecutable(t, filepath.Join(binDir, "tofu"), script)
}

func writeFakeInventory(t *testing.T, root string) {
	t.Helper()
	writeExecutable(t, filepath.Join(root, "scripts", "generate-inventory.sh"), "#!/usr/bin/env bash\nexit 0\n")
}

// TestWizard_AllConfigured tests the fast path: everything pre-configured,
// no interactive prompts needed, straight to confirm.
func TestWizard_AllConfigured(t *testing.T) {
	srv := latitudeAPI()
	defer srv.Close()

	dir := t.TempDir()
	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)
	tfDir := filepath.Join(dir, "terraform")
	os.MkdirAll(tfDir, 0755)

	cfg := &config.Config{}
	cfg.Latitude.AuthToken = "test-token"
	cfg.Latitude.Project = "proj-1"
	cfg.Latitude.Region = "ASH"
	cfg.Latitude.Plan = "s3-large-x86"
	cfg.Latitude.OperatingSystem = "ubuntu_24_04_x64_lts"
	cfg.Latitude.Billing = "hourly"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			// No prompts for fields — all resolved from config.
			"y", // confirm
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg: cfg, Prompter: prompter, Out: &buf,
		TerraformDir: tfDir, LatBaseURL: srv.URL,
	}

	err := w.Run(context.Background())
	output := buf.String()

	// All fields should show ✓
	if !strings.Contains(output, "✓ API Token") {
		t.Errorf("expected ✓ API Token, got:\n%s", output)
	}
	if !strings.Contains(output, "✓ Project") {
		t.Errorf("expected ✓ Project, got:\n%s", output)
	}
	if !strings.Contains(output, "✓ Region") {
		t.Errorf("expected ✓ Region, got:\n%s", output)
	}
	if !strings.Contains(output, "✓ Plan") {
		t.Errorf("expected ✓ Plan, got:\n%s", output)
	}
	if !strings.Contains(output, "Provision Summary") {
		t.Errorf("expected summary, got:\n%s", output)
	}

	// Verify tfvars written.
	data, readErr := os.ReadFile(filepath.Join(tfDir, "terraform.tfvars.json"))
	if readErr != nil {
		t.Fatalf("tfvars not written: %v", readErr)
	}
	var tfvars map[string]interface{}
	json.Unmarshal(data, &tfvars)
	if tfvars["region"] != "ASH" {
		t.Errorf("expected region=ASH, got %v", tfvars["region"])
	}

	// Should fail at tofu, not wizard logic.
	if err != nil && !strings.Contains(err.Error(), "tofu") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestWizard_TokenMissing tests that only the token is prompted when
// everything else is configured.
func TestWizard_TokenMissing(t *testing.T) {
	srv := latitudeAPI()
	defer srv.Close()

	dir := t.TempDir()
	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)
	tfDir := filepath.Join(dir, "terraform")
	os.MkdirAll(tfDir, 0755)

	cfg := &config.Config{}
	// Token deliberately empty.
	cfg.Latitude.Project = "proj-1"
	cfg.Latitude.Region = "ASH"
	cfg.Latitude.Plan = "s3-large-x86"
	cfg.Latitude.OperatingSystem = "ubuntu_24_04_x64_lts"
	cfg.Latitude.Billing = "hourly"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			"good-token", // prompted for token
			"y",          // confirm
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg: cfg, Prompter: prompter, Out: &buf,
		TerraformDir: tfDir, LatBaseURL: srv.URL,
	}

	err := w.Run(context.Background())
	output := buf.String()

	if !strings.Contains(output, "API token required") {
		t.Errorf("expected token prompt, got:\n%s", output)
	}
	if !strings.Contains(output, "✓ API Token") {
		t.Errorf("expected ✓ after entering token, got:\n%s", output)
	}
	// Other fields should not be prompted.
	if strings.Contains(output, "Select region") {
		t.Errorf("region should not be prompted, got:\n%s", output)
	}

	if err != nil && !strings.Contains(err.Error(), "tofu") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestWizard_InvalidTokenRetry tests re-prompting when saved token is invalid.
func TestWizard_InvalidTokenRetry(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/user/profile" {
			callCount++
			if callCount == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error": "unauthorized"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"id": "user-1", "attributes": map[string]interface{}{"email": "test@example.com"},
				},
			})
			return
		}
		if r.URL.Path == "/projects" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "proj-1", "attributes": map[string]interface{}{"name": "Test", "slug": "test"}},
				},
			})
			return
		}
		if r.URL.Path == "/regions" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "reg-1", "attributes": map[string]interface{}{
						"slug": "ASH", "name": "Ashburn",
						"country": map[string]interface{}{"name": "US", "slug": "us"},
					}},
				},
			})
			return
		}
		if r.URL.Path == "/plans" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "plan-1", "attributes": map[string]interface{}{
						"name": "Large", "slug": "s3-large-x86",
						"regions": []map[string]interface{}{{"locations": map[string]interface{}{"available": []string{"ASH"}}}},
					}},
				},
			})
			return
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)
	tfDir := filepath.Join(dir, "terraform")
	os.MkdirAll(tfDir, 0755)

	cfg := &config.Config{}
	cfg.Latitude.AuthToken = "bad-token"
	cfg.Latitude.Project = "proj-1"
	cfg.Latitude.Region = "ASH"
	cfg.Latitude.Plan = "s3-large-x86"
	cfg.Latitude.OperatingSystem = "ubuntu_24_04_x64_lts"
	cfg.Latitude.Billing = "hourly"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			"good-token", // re-prompted after bad token
			"y",          // confirm
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg: cfg, Prompter: prompter, Out: &buf,
		TerraformDir: tfDir, LatBaseURL: srv.URL,
	}

	err := w.Run(context.Background())
	output := buf.String()

	if !strings.Contains(output, "saved token is invalid") {
		t.Errorf("expected invalid token message, got:\n%s", output)
	}
	if !strings.Contains(output, "✓ API Token") {
		t.Errorf("expected ✓ after re-entering token, got:\n%s", output)
	}

	if err != nil && !strings.Contains(err.Error(), "tofu") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestWizard_ConfirmAbort tests that declining confirmation returns an error.
func TestWizard_ConfirmAbort(t *testing.T) {
	srv := latitudeAPI()
	defer srv.Close()

	dir := t.TempDir()
	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)

	cfg := &config.Config{}
	cfg.Latitude.AuthToken = "test-token"
	cfg.Latitude.Project = "proj-1"
	cfg.Latitude.Region = "ASH"
	cfg.Latitude.Plan = "s3-large-x86"
	cfg.Latitude.OperatingSystem = "ubuntu_24_04_x64_lts"
	cfg.Latitude.Billing = "hourly"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			"n", // decline
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg: cfg, Prompter: prompter, Out: &buf,
		TerraformDir: filepath.Join(dir, "tf"), LatBaseURL: srv.URL,
	}

	err := w.Run(context.Background())
	if err == nil || err.Error() != "aborted" {
		t.Errorf("expected 'aborted' error, got: %v", err)
	}
}

// TestWizard_SSHKeyMissing tests SSH key generation prompt.
func TestWizard_SSHKeyMissing(t *testing.T) {
	srv := latitudeAPI()
	defer srv.Close()

	dir := t.TempDir()
	sshPub := filepath.Join(dir, "nonexistent.pub")

	cfg := &config.Config{}
	cfg.Latitude.AuthToken = "test-token"
	cfg.Latitude.Project = "proj-1"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			"n", // decline generate
			"",  // empty custom path
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg: cfg, Prompter: prompter, Out: &buf,
		TerraformDir: filepath.Join(dir, "tf"), LatBaseURL: srv.URL,
	}

	err := w.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for missing SSH key")
	}
	if !strings.Contains(err.Error(), "SSH key is required") {
		t.Errorf("expected SSH key error, got: %v", err)
	}
}

func TestWizard_FirstRunPromptsForRegionAndPlanDefaults(t *testing.T) {
	srv := latitudeAPIWithChoices(
		[]testRegion{
			{ID: "reg-1", Slug: "DAL", Name: "Dallas", Country: "United States"},
			{ID: "reg-2", Slug: "ASH", Name: "Ashburn", Country: "United States"},
		},
		[]testPlan{
			{ID: "plan-0", Slug: "s1-small-x86", Name: "Small Server", Regions: []string{"ASH"}},
			{ID: "plan-1", Slug: "s3-large-x86", Name: "Large Server", Regions: []string{"ASH"}},
		},
	)
	defer srv.Close()

	dir := t.TempDir()
	withWorkingDir(t, dir)

	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)
	tfDir := filepath.Join(dir, "terraform")
	os.MkdirAll(tfDir, 0755)

	binDir := filepath.Join(dir, "bin")
	writeFakeTofu(t, binDir, filepath.Join(dir, "tofu-env.log"))
	writeFakeInventory(t, dir)
	prependPath(t, binDir)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{
		answers: []string{
			"good-token", // token prompt
			"",           // region: accept default from embedded config
			"",           // plan: accept default from embedded config
			"y",          // confirm
		},
	}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg: cfg, Prompter: prompter, Out: &buf,
		TerraformDir: tfDir, LatBaseURL: srv.URL,
	}

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v\n%s", err, buf.String())
	}

	recordedPrompts := strings.Join(prompter.selectPrompts, "\n")
	if !strings.Contains(recordedPrompts, "Select region:") {
		t.Fatalf("expected region prompt on first run, got prompts: %v", prompter.selectPrompts)
	}
	if !strings.Contains(recordedPrompts, "Select server plan:") {
		t.Fatalf("expected plan prompt on first run, got prompts: %v", prompter.selectPrompts)
	}

	data, err := os.ReadFile(filepath.Join(tfDir, "terraform.tfvars.json"))
	if err != nil {
		t.Fatalf("read tfvars: %v", err)
	}
	var tfvars map[string]interface{}
	if err := json.Unmarshal(data, &tfvars); err != nil {
		t.Fatalf("unmarshal tfvars: %v", err)
	}
	if tfvars["region"] != "ASH" {
		t.Fatalf("expected default region ASH, got %v", tfvars["region"])
	}
	if tfvars["plan"] != "s3-large-x86" {
		t.Fatalf("expected default plan s3-large-x86, got %v", tfvars["plan"])
	}
}

func TestWizard_SavedSelectionsSkipRegionAndPlanPrompts(t *testing.T) {
	srv := latitudeAPIWithChoices(
		[]testRegion{
			{ID: "reg-1", Slug: "DAL", Name: "Dallas", Country: "United States"},
			{ID: "reg-2", Slug: "ASH", Name: "Ashburn", Country: "United States"},
		},
		[]testPlan{
			{ID: "plan-0", Slug: "s1-small-x86", Name: "Small Server", Regions: []string{"ASH"}},
			{ID: "plan-1", Slug: "s3-large-x86", Name: "Large Server", Regions: []string{"ASH"}},
		},
	)
	defer srv.Close()

	dir := t.TempDir()
	withWorkingDir(t, dir)

	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)
	tfDir := filepath.Join(dir, "terraform")
	os.MkdirAll(tfDir, 0755)

	writeExecutable(t, filepath.Join(dir, "forge-metal.toml"), `[latitude]
auth_token = "saved-token"
project = "proj-1"
region = "ASH"
plan = "s3-large-x86"
`)

	binDir := filepath.Join(dir, "bin")
	writeFakeTofu(t, binDir, filepath.Join(dir, "tofu-env.log"))
	writeFakeInventory(t, dir)
	prependPath(t, binDir)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.SSH.PublicKeyPath = sshPub
	cfg.Latitude.OperatingSystem = "ubuntu_24_04_x64_lts"
	cfg.Latitude.Billing = "hourly"

	prompter := &mockPrompter{answers: []string{"y"}}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg: cfg, Prompter: prompter, Out: &buf,
		TerraformDir: tfDir, LatBaseURL: srv.URL,
	}

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v\n%s", err, buf.String())
	}

	recordedPrompts := strings.Join(prompter.selectPrompts, "\n")
	if strings.Contains(recordedPrompts, "Select region:") {
		t.Fatalf("did not expect region prompt on saved config, got prompts: %v", prompter.selectPrompts)
	}
	if strings.Contains(recordedPrompts, "Select server plan:") {
		t.Fatalf("did not expect plan prompt on saved config, got prompts: %v", prompter.selectPrompts)
	}

	output := buf.String()
	if strings.Contains(output, "Latitude.sh API token required") {
		t.Fatalf("did not expect token prompt on saved config, got:\n%s", output)
	}
}

func TestWizard_ProvisionPassesTokenToTofu(t *testing.T) {
	srv := latitudeAPI()
	defer srv.Close()

	dir := t.TempDir()
	withWorkingDir(t, dir)

	sshPub := filepath.Join(dir, "id_ed25519.pub")
	os.WriteFile(sshPub, []byte("ssh-ed25519 AAAA test@test"), 0644)
	tfDir := filepath.Join(dir, "terraform")
	os.MkdirAll(tfDir, 0755)

	logPath := filepath.Join(dir, "tofu-env.log")
	binDir := filepath.Join(dir, "bin")
	writeFakeTofu(t, binDir, logPath)
	writeFakeInventory(t, dir)
	prependPath(t, binDir)

	cfg := &config.Config{}
	cfg.Latitude.AuthToken = "test-token"
	cfg.Latitude.Project = "proj-1"
	cfg.Latitude.Region = "ASH"
	cfg.Latitude.Plan = "s3-large-x86"
	cfg.Latitude.OperatingSystem = "ubuntu_24_04_x64_lts"
	cfg.Latitude.Billing = "hourly"
	cfg.SSH.PublicKeyPath = sshPub

	prompter := &mockPrompter{answers: []string{"y"}}

	var buf bytes.Buffer
	w := &Wizard{
		Cfg: cfg, Prompter: prompter, Out: &buf,
		TerraformDir: tfDir, LatBaseURL: srv.URL,
	}

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run failed: %v\n%s", err, buf.String())
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read tofu env log: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "init:test-token") {
		t.Fatalf("expected tofu init to receive LATITUDESH_AUTH_TOKEN, got:\n%s", output)
	}
	if !strings.Contains(output, "apply:test-token") {
		t.Fatalf("expected tofu apply to receive LATITUDESH_AUTH_TOKEN, got:\n%s", output)
	}
}
