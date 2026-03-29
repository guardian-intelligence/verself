package doctor

import "testing"

// mockResolver lets tests control all lookups.
type mockResolver struct {
	which        func(string) string
	version      func(string) string
	nixStorePath func(string) string
	isNixProfile func(string) bool
}

func newMock() *mockResolver {
	return &mockResolver{
		which:        func(string) string { return "" },
		version:      func(string) string { return "" },
		nixStorePath: func(string) string { return "" },
		isNixProfile: func(string) bool { return false },
	}
}

func (m *mockResolver) Which(binary string) string        { return m.which(binary) }
func (m *mockResolver) Version(cmd string) string          { return m.version(cmd) }
func (m *mockResolver) NixStorePath(attr string) string    { return m.nixStorePath(attr) }
func (m *mockResolver) IsNixProfile(path string) bool      { return m.isNixProfile(path) }

var sopsSpec = ToolSpec{"sops", "sops --version", "3.12.2", "pkgs.sops"}

// Test 1: Tool in PATH, correct version → OK
func TestCheck_OK(t *testing.T) {
	m := newMock()
	m.which = func(string) string { return "/nix/store/abc-sops-3.12.2/bin/sops" }
	m.version = func(string) string { return "3.12.2" }

	r := Check(sopsSpec, m)
	if r.Status != OK {
		t.Fatalf("expected OK, got %d", r.Status)
	}
	if r.ActualVer != "3.12.2" {
		t.Fatalf("expected version 3.12.2, got %s", r.ActualVer)
	}
}

// Test 2: Tool not in PATH, not in nix store → Missing
func TestCheck_Missing(t *testing.T) {
	m := newMock()

	r := Check(sopsSpec, m)
	if r.Status != Missing {
		t.Fatalf("expected Missing, got %d", r.Status)
	}
}

// Test 3: Tool not in PATH, available in nix store → Installable
func TestCheck_Installable(t *testing.T) {
	m := newMock()
	m.nixStorePath = func(string) string { return "/nix/store/abc-forge-metal-dev-tools" }

	r := Check(sopsSpec, m)
	if r.Status != Installable {
		t.Fatalf("expected Installable, got %d", r.Status)
	}
}

// Test 4: Tool in PATH, wrong version, nix-managed → Upgradable
func TestCheck_Upgradable(t *testing.T) {
	m := newMock()
	m.which = func(string) string { return "/nix/store/old-sops-3.10.0/bin/sops" }
	m.version = func(string) string { return "3.10.0" }
	m.isNixProfile = func(path string) bool { return path == "/nix/store/old-sops-3.10.0/bin/sops" }

	r := Check(sopsSpec, m)
	if r.Status != Upgradable {
		t.Fatalf("expected Upgradable, got %d", r.Status)
	}
	if r.ActualVer != "3.10.0" {
		t.Fatalf("expected version 3.10.0, got %s", r.ActualVer)
	}
}

// Test 5: Tool in PATH, wrong version, system-managed → Conflict
func TestCheck_Conflict(t *testing.T) {
	m := newMock()
	m.which = func(string) string { return "/usr/bin/sops" }
	m.version = func(string) string { return "3.10.0" }

	r := Check(sopsSpec, m)
	if r.Status != Conflict {
		t.Fatalf("expected Conflict, got %d", r.Status)
	}
	if r.ActualVer != "3.10.0" {
		t.Fatalf("expected version 3.10.0, got %s", r.ActualVer)
	}
	if r.BinPath != "/usr/bin/sops" {
		t.Fatalf("expected path /usr/bin/sops, got %s", r.BinPath)
	}
}

// Test 6: Tool in PATH, version command fails → Conflict
func TestCheck_VersionFails(t *testing.T) {
	m := newMock()
	m.which = func(string) string { return "/usr/local/bin/sops" }
	m.version = func(string) string { return "" }

	r := Check(sopsSpec, m)
	if r.Status != Conflict {
		t.Fatalf("expected Conflict, got %d", r.Status)
	}
}

// Test 7: Batch — mixed states across tools
func TestCheckAll_MixedStates(t *testing.T) {
	goSpec := ToolSpec{"go", "go version", "1.25.0", "pkgs.go_1_25"}
	ansibleSpec := ToolSpec{"ansible", "ansible --version", "2.16.3", "pkgs.ansible"}

	// Save and restore Manifest
	orig := Manifest
	Manifest = []ToolSpec{goSpec, sopsSpec, ansibleSpec}
	defer func() { Manifest = orig }()

	m := newMock()
	m.which = func(binary string) string {
		switch binary {
		case "go":
			return "/nix/store/abc-go/bin/go"
		case "ansible":
			return "/usr/bin/ansible"
		default:
			return ""
		}
	}
	m.version = func(cmd string) string {
		switch {
		case cmd == "go version":
			return "1.25.0"
		case cmd == "ansible --version":
			return "2.14.0"
		default:
			return ""
		}
	}
	m.isNixProfile = func(path string) bool {
		return false // ansible at /usr/bin is not nix
	}

	results, summary := CheckAll(m)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	expected := []Status{OK, Missing, Conflict}
	for i, want := range expected {
		if results[i].Status != want {
			t.Errorf("results[%d] (%s): expected status %d, got %d",
				i, results[i].Spec.Name, want, results[i].Status)
		}
	}

	if summary.OK != 1 || summary.Missing != 1 || summary.Conflict != 1 {
		t.Errorf("unexpected summary: %+v", summary)
	}
}
