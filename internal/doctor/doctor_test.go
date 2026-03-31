// Doctor tests — real binary execution against temp directories.
//
// Each test gets a fresh t.TempDir(), writes executable scripts to it,
// and runs the doctor logic against a filesystem-backed resolver that
// calls exec.Command on real binaries. This catches version extraction
// and PATH resolution bugs without mocks.
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
// Test resolver: filesystem-backed, runs real binaries on ZFS clones
// ---------------------------------------------------------------------------

// cloneResolver looks up binaries at real paths within a ZFS clone.
// Version() calls exec.Command on actual scripts — no mocking.
type cloneResolver struct {
	binDir   string          // directory containing executable scripts
	nixPaths map[string]bool // paths considered nix-managed
	nixAvail bool            // whether dev-tools are "available" in nix store
}

func (r *cloneResolver) Which(binary string) string {
	path := filepath.Join(r.binDir, binary)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func (r *cloneResolver) Version(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}
	// Resolve the binary from our binDir, not the system PATH.
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

func (r *cloneResolver) NixStorePath(attr string) string {
	if r.nixAvail {
		return "/nix/store/fake-dev-tools"
	}
	return ""
}

func (r *cloneResolver) IsNixProfile(path string) bool {
	if r.nixPaths != nil {
		return r.nixPaths[path]
	}
	return strings.HasPrefix(path, "/nix/store/")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeBinary creates an executable shell script that prints versionOutput.
func writeBinary(t *testing.T, dir, name, versionOutput string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\n", versionOutput)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write binary %s: %v", name, err)
	}
	return path
}

// versionOutput returns a realistic version string for a tool name.
func versionOutput(name, version string) string {
	switch name {
	case "go":
		return fmt.Sprintf("go version go%s linux/amd64", version)
	case "tofu":
		return fmt.Sprintf(`{"terraform_version":"%s"}`, version)
	case "ansible":
		return fmt.Sprintf("ansible [core %s]", version)
	case "jq":
		return fmt.Sprintf("jq-%s", version)
	default:
		return version
	}
}

// toolNames returns the names of all tools in the manifest.
func toolNames() []string {
	names := make([]string, len(Manifest))
	for i := range Manifest {
		names[i] = Manifest[i].Name
	}
	return names
}

// findLine returns lines in output that contain substr.
func findLine(output, substr string) string {
	var matches []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, substr) {
			matches = append(matches, line)
		}
	}
	return strings.Join(matches, "\n")
}

// formatOutput replicates the CLI table formatting from cmd/forge-metal/doctor.go.
func formatOutput(results []CheckResult, summary Summary) string {
	var b strings.Builder
	fmt.Fprintln(&b, "    Tool          Have       Want")
	for _, r := range results {
		var icon, have, note string
		switch r.Status {
		case OK:
			icon, have = "✓", r.ActualVer
		case Missing, Installable:
			icon, have = "✗", "—"
		case Upgradable:
			icon, have = "⚠", r.ActualVer
		case Conflict:
			icon, have, note = "⚠", r.ActualVer, r.BinPath
		}
		if note != "" {
			fmt.Fprintf(&b, "  %s %-12s  %-8s   %-8s   %s\n", icon, r.Spec.Name, have, r.Spec.Expected, note)
		} else {
			fmt.Fprintf(&b, "  %s %-12s  %-8s   %s\n", icon, r.Spec.Name, have, r.Spec.Expected)
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintf(&b, "%d ok", summary.OK)
	if summary.Installable > 0 {
		fmt.Fprintf(&b, ", %d installable", summary.Installable)
	}
	if summary.Upgradable > 0 {
		fmt.Fprintf(&b, ", %d upgradable", summary.Upgradable)
	}
	if summary.Missing > 0 {
		fmt.Fprintf(&b, ", %d missing", summary.Missing)
	}
	if summary.Conflict > 0 {
		fmt.Fprintf(&b, ", %d conflict", summary.Conflict)
	}
	fmt.Fprintln(&b)

	if summary.Conflict > 0 {
		fmt.Fprintln(&b, "  hint: remove system versions or ensure ~/.nix-profile/bin is first in PATH")
	}

	return b.String()
}

// newBinDir creates a temp directory with a bin/ subdirectory for test scripts.
func newBinDir(t *testing.T) string {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	return binDir
}

// ---------------------------------------------------------------------------
// Tests — each gets a fresh ZFS clone
// ---------------------------------------------------------------------------

// TestCleanState: no tools installed, all 8 report missing.
func TestCleanState(t *testing.T) {
	binDir := newBinDir(t)

	r := &cloneResolver{binDir: binDir}
	results, summary := CheckAll(r)
	output := formatOutput(results, summary)

	if summary.Missing+summary.Installable+summary.Upgradable+summary.Conflict == 0 {
		t.Fatal("expected issues in clean state")
	}

	for _, name := range toolNames() {
		if !strings.Contains(findLine(output, name), "✗") {
			t.Errorf("%s not marked ✗", name)
		}
	}

	if strings.Contains(output, "✓") {
		t.Fatalf("unexpected ✓ in clean state:\n%s", output)
	}

	_ = results
}

// TestFixFlow: all missing → write binaries → all OK.
func TestFixFlow(t *testing.T) {
	binDir := newBinDir(t)

	// Phase 1: all missing.
	r1 := &cloneResolver{binDir: binDir}
	results1, _ := CheckAll(r1)

	var fixedNames []string
	for _, r := range results1 {
		if r.Status == Missing || r.Status == Installable || r.Status == Upgradable {
			fixedNames = append(fixedNames, r.Spec.Name)
		}
	}
	if len(fixedNames) != len(Manifest) {
		t.Fatalf("expected all %d tools fixable, got %d", len(Manifest), len(fixedNames))
	}

	// Phase 2: write correct binaries (simulates nix profile install).
	for _, spec := range Manifest {
		writeBinary(t, binDir, spec.Name, versionOutput(spec.Name, spec.Expected))
	}

	r2 := &cloneResolver{binDir: binDir, nixAvail: true}
	results2, summary2 := CheckAll(r2)
	output := formatOutput(results2, summary2)

	for _, name := range toolNames() {
		if !strings.Contains(findLine(output, name), "✓") {
			t.Errorf("%s not ✓ after fix:\n%s", name, output)
		}
	}

	if !strings.Contains(output, "8 ok") {
		t.Errorf("expected '8 ok':\n%s", output)
	}
}

// TestSystemConflict: wrong-version system binaries → Conflict with path shown.
func TestSystemConflict(t *testing.T) {
	binDir := newBinDir(t)

	writeBinary(t, binDir, "go", "go version go1.21.0 linux/amd64")
	writeBinary(t, binDir, "shellcheck", "version: 0.9.0")

	r := &cloneResolver{
		binDir:   binDir,
		nixPaths: map[string]bool{},
	}

	results, summary := CheckAll(r)
	output := formatOutput(results, summary)

	if summary.Conflict == 0 {
		t.Fatal("expected conflicts")
	}

	goLine := findLine(output, "go")
	if !strings.Contains(goLine, "⚠") || !strings.Contains(goLine, "1.21.0") {
		t.Errorf("go conflict not shown correctly:\n%s", goLine)
	}

	scLine := findLine(output, "shellcheck")
	if !strings.Contains(scLine, "⚠") || !strings.Contains(scLine, "0.9.0") {
		t.Errorf("shellcheck conflict not shown correctly:\n%s", scLine)
	}

	if !strings.Contains(output, "hint:") {
		t.Errorf("missing hint line:\n%s", output)
	}

	_ = results
}

// TestMixedConflict: all tools OK except system go shadows nix go.
func TestMixedConflict(t *testing.T) {
	binDir := newBinDir(t)

	nixPaths := make(map[string]bool)
	for _, spec := range Manifest {
		p := writeBinary(t, binDir, spec.Name, versionOutput(spec.Name, spec.Expected))
		nixPaths[p] = true
	}

	// Override go: wrong version, not nix-managed.
	goPath := writeBinary(t, binDir, "go", "go version go1.21.0 linux/amd64")
	delete(nixPaths, goPath)

	r := &cloneResolver{binDir: binDir, nixAvail: true, nixPaths: nixPaths}
	results, summary := CheckAll(r)
	output := formatOutput(results, summary)

	if !strings.Contains(findLine(output, "go"), "⚠") {
		t.Errorf("go should be ⚠")
	}

	for _, name := range []string{"tofu", "ansible", "sops", "age", "buf", "shellcheck", "jq"} {
		if !strings.Contains(findLine(output, name), "✓") {
			t.Errorf("%s should be ✓", name)
		}
	}

	if !strings.Contains(output, "1 conflict") {
		t.Errorf("expected '1 conflict':\n%s", output)
	}
}

// TestAllOK: every tool present at correct version.
func TestAllOK(t *testing.T) {
	binDir := newBinDir(t)

	for _, spec := range Manifest {
		writeBinary(t, binDir, spec.Name, versionOutput(spec.Name, spec.Expected))
	}

	r := &cloneResolver{binDir: binDir, nixAvail: true}
	results, summary := CheckAll(r)
	output := formatOutput(results, summary)

	for _, name := range toolNames() {
		if !strings.Contains(findLine(output, name), "✓") {
			t.Errorf("%s not ✓:\n%s", name, output)
		}
	}

	if !strings.Contains(output, "8 ok") {
		t.Errorf("expected '8 ok':\n%s", output)
	}

	if strings.Contains(output, "conflict") || strings.Contains(output, "missing") {
		t.Errorf("unexpected issues:\n%s", output)
	}
}

// TestIdempotent: after fix, second check finds nothing to fix.
func TestIdempotent(t *testing.T) {
	binDir := newBinDir(t)

	for _, spec := range Manifest {
		writeBinary(t, binDir, spec.Name, versionOutput(spec.Name, spec.Expected))
	}

	r := &cloneResolver{binDir: binDir, nixAvail: true}
	results, _ := CheckAll(r)

	var fixable int
	for _, res := range results {
		if res.Status == Missing || res.Status == Installable || res.Status == Upgradable {
			fixable++
		}
	}

	if fixable != 0 {
		t.Errorf("expected 0 fixable, got %d", fixable)
	}
}

// TestRealVersionExtraction: SystemResolver.Version against real scripts on ZFS.
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
	if sr.IsNixProfile(goBin) {
		t.Error("ZFS path should not be detected as nix profile")
	}
}
