package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Test resolver: filesystem-backed, runs real binaries
// ---------------------------------------------------------------------------

type testResolver struct {
	binDir string
}

func (r *testResolver) Which(binary string) string {
	path := filepath.Join(r.binDir, binary)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func (r *testResolver) Version(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}
	bin := parts[0]
	localBin := filepath.Join(r.binDir, filepath.Base(bin))
	if _, err := os.Stat(localBin); err == nil {
		bin = localBin
	}
	out, err := exec.Command(bin, parts[1:]...).CombinedOutput()
	if err != nil {
		return ""
	}
	return semverRe.FindString(string(out))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeBinary(t *testing.T, dir, name, versionOutput string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\n", versionOutput)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write binary %s: %v", name, err)
	}
	return path
}

func versionOutput(name, version string) string {
	switch name {
	case "go":
		return fmt.Sprintf("go version go%s linux/amd64", version)
	case "opentofu":
		return fmt.Sprintf(`{"terraform_version":"%s"}`, version)
	case "ansible":
		return fmt.Sprintf("ansible [core %s]", version)
	case "jq":
		return fmt.Sprintf("jq-%s", version)
	default:
		return version
	}
}

func newBinDir(t *testing.T) string {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return binDir
}

// testManifest is a small manifest used across tests.
var testManifest = []ToolSpec{
	{"go", "go version", "1.25.8"},
	{"opentofu", "tofu version -json", "1.11.5"},
	{"ansible", "ansible --version", "2.20.3"},
	{"sops", "sops --version", "3.12.2"},
	{"age", "age --version", "1.3.1"},
	{"buf", "buf --version", "1.66.1"},
	{"shellcheck", "shellcheck --version", "0.11.0"},
	{"jq", "jq --version", "1.8.1"},
}

func toolNames(manifest []ToolSpec) []string {
	names := make([]string, len(manifest))
	for i := range manifest {
		names[i] = manifest[i].Name
	}
	return names
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCleanState(t *testing.T) {
	binDir := newBinDir(t)
	r := &testResolver{binDir: binDir}
	results, summary := CheckAll(testManifest, r)

	if summary.Missing != len(testManifest) {
		t.Fatalf("expected all %d missing, got missing=%d", len(testManifest), summary.Missing)
	}

	for _, res := range results {
		if res.Status != Missing {
			t.Errorf("%s: expected Missing, got %d", res.Spec.Name, res.Status)
		}
	}
}

func TestAllOK(t *testing.T) {
	binDir := newBinDir(t)
	for _, spec := range testManifest {
		// The binary name for opentofu is "tofu"
		binName := spec.Name
		if binName == "opentofu" {
			binName = "tofu"
		}
		writeBinary(t, binDir, binName, versionOutput(spec.Name, spec.Expected))
	}

	r := &testResolver{binDir: binDir}
	// For opentofu, the Which looks up "opentofu" but the binary is "tofu".
	// In real usage, the version_cmd handles this. For tests, let's use a manifest
	// where Name matches the binary name.
	manifest := make([]ToolSpec, len(testManifest))
	copy(manifest, testManifest)
	// Fix opentofu -> tofu for the test
	for i := range manifest {
		if manifest[i].Name == "opentofu" {
			manifest[i].Name = "tofu"
		}
	}

	results, summary := CheckAll(manifest, r)

	if summary.OK != len(manifest) {
		t.Fatalf("expected all %d OK, got ok=%d missing=%d mismatch=%d",
			len(manifest), summary.OK, summary.Missing, summary.VersionMismatch)
	}

	for _, res := range results {
		if res.Status != OK {
			t.Errorf("%s: expected OK, got %d (actual=%q)", res.Spec.Name, res.Status, res.ActualVer)
		}
	}
}

func TestVersionMismatch(t *testing.T) {
	binDir := newBinDir(t)

	writeBinary(t, binDir, "go", "go version go1.21.0 linux/amd64")
	writeBinary(t, binDir, "shellcheck", "version: 0.9.0")

	manifest := []ToolSpec{
		{"go", "go version", "1.25.8"},
		{"shellcheck", "shellcheck --version", "0.11.0"},
	}

	r := &testResolver{binDir: binDir}
	results, summary := CheckAll(manifest, r)

	if summary.VersionMismatch != 2 {
		t.Fatalf("expected 2 mismatches, got %d", summary.VersionMismatch)
	}

	for _, res := range results {
		if res.Status != VersionMismatch {
			t.Errorf("%s: expected VersionMismatch, got %d", res.Spec.Name, res.Status)
		}
	}
}

func TestMixedState(t *testing.T) {
	binDir := newBinDir(t)

	// go: correct version
	writeBinary(t, binDir, "go", "go version go1.25.8 linux/amd64")
	// shellcheck: wrong version
	writeBinary(t, binDir, "shellcheck", "version: 0.9.0")
	// sops: missing (not written)

	manifest := []ToolSpec{
		{"go", "go version", "1.25.8"},
		{"shellcheck", "shellcheck --version", "0.11.0"},
		{"sops", "sops --version", "3.12.2"},
	}

	r := &testResolver{binDir: binDir}
	results, summary := CheckAll(manifest, r)

	if summary.OK != 1 || summary.VersionMismatch != 1 || summary.Missing != 1 {
		t.Fatalf("expected 1 ok, 1 mismatch, 1 missing; got ok=%d mismatch=%d missing=%d",
			summary.OK, summary.VersionMismatch, summary.Missing)
	}

	expectations := map[string]Status{
		"go":         OK,
		"shellcheck": VersionMismatch,
		"sops":       Missing,
	}
	for _, res := range results {
		want, ok := expectations[res.Spec.Name]
		if !ok {
			t.Errorf("unexpected tool %s", res.Spec.Name)
			continue
		}
		if res.Status != want {
			t.Errorf("%s: expected %d, got %d", res.Spec.Name, want, res.Status)
		}
	}
}

func TestRealVersionExtraction(t *testing.T) {
	binDir := newBinDir(t)

	goBin := writeBinary(t, binDir, "go", "go version go1.21.0 linux/amd64")
	scBin := writeBinary(t, binDir, "shellcheck", "ShellCheck - shell script analysis tool\nversion: 0.9.0")

	sr := &SystemResolver{}

	if v := sr.Version(goBin); v != "1.21.0" {
		t.Errorf("go: expected 1.21.0, got %q", v)
	}
	if v := sr.Version(scBin); v != "0.9.0" {
		t.Errorf("shellcheck: expected 0.9.0, got %q", v)
	}
}

func TestLoadManifestFrom(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "dev-tools.json")

	content := `{
		"go": {"version": "1.25.8", "version_cmd": "go version"},
		"sops": {"version": "3.12.2", "version_cmd": "sops --version"}
	}`
	if err := os.WriteFile(jsonPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	specs, err := loadManifestFrom(jsonPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	// go should come before sops in the fixed order
	if specs[0].Name != "go" {
		t.Errorf("expected go first, got %s", specs[0].Name)
	}
	if specs[0].Expected != "1.25.8" {
		t.Errorf("expected 1.25.8, got %s", specs[0].Expected)
	}
	if specs[1].Name != "sops" {
		t.Errorf("expected sops second, got %s", specs[1].Name)
	}
}
