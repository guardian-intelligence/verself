package doctor

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type Status int

const (
	OK          Status = iota // in PATH, version matches
	Missing                   // not in PATH, not in nix store
	Installable               // not in PATH, available in nix store
	Upgradable                // in PATH, wrong version, nix-managed
	Conflict                  // in PATH, wrong version, system-managed
)

type ToolSpec struct {
	Name       string // binary name: "sops"
	VersionCmd string // shell command: "sops --version"
	Expected   string // pinned version: "3.12.2"
	NixAttr    string // flake attr: "pkgs.sops"
}

type CheckResult struct {
	Spec      ToolSpec
	Status    Status
	ActualVer string // "" if missing
	BinPath   string // "" if missing
}

type Summary struct {
	OK          int
	Missing     int
	Installable int
	Upgradable  int
	Conflict    int
}

// Resolver abstracts exec/nix lookups so tests can mock them.
type Resolver interface {
	Which(binary string) string
	Version(cmd string) string
	NixStorePath(attr string) string
	IsNixProfile(path string) bool
}

// Check evaluates a single tool against the resolver.
func Check(spec ToolSpec, r Resolver) CheckResult {
	binPath := r.Which(spec.Name)
	if binPath == "" {
		// Not in PATH — check nix store
		storePath := r.NixStorePath(spec.NixAttr)
		if storePath != "" {
			return CheckResult{Spec: spec, Status: Installable}
		}
		return CheckResult{Spec: spec, Status: Missing}
	}

	// In PATH — check version
	actual := r.Version(spec.VersionCmd)
	if actual == spec.Expected {
		return CheckResult{Spec: spec, Status: OK, ActualVer: actual, BinPath: binPath}
	}

	// Wrong version — nix-managed or system?
	if r.IsNixProfile(binPath) {
		return CheckResult{Spec: spec, Status: Upgradable, ActualVer: actual, BinPath: binPath}
	}
	return CheckResult{Spec: spec, Status: Conflict, ActualVer: actual, BinPath: binPath}
}

// CheckAll runs the full manifest, returns the list and summary counts.
func CheckAll(r Resolver) ([]CheckResult, Summary) {
	results := make([]CheckResult, 0, len(Manifest))
	var s Summary
	for _, spec := range Manifest {
		cr := Check(spec, r)
		results = append(results, cr)
		switch cr.Status {
		case OK:
			s.OK++
		case Missing:
			s.Missing++
		case Installable:
			s.Installable++
		case Upgradable:
			s.Upgradable++
		case Conflict:
			s.Conflict++
		}
	}
	return results, s
}

// Fix attempts auto-remediation for Installable and Upgradable results
// by installing the project's dev-tools flake package.
func Fix(results []CheckResult) (fixed []string, errs []error) {
	var fixable []CheckResult
	for _, r := range results {
		if r.Status == Missing || r.Status == Installable || r.Status == Upgradable {
			fixable = append(fixable, r)
		}
	}
	if len(fixable) == 0 {
		return
	}

	cmd := exec.Command("nix", "profile", "install", ".#dev-tools")
	out, err := cmd.CombinedOutput()
	if err != nil {
		errs = append(errs, fmt.Errorf("nix profile install .#dev-tools: %s", strings.TrimSpace(string(out))))
		return
	}

	for _, r := range fixable {
		fixed = append(fixed, r.Spec.Name)
	}
	return
}

// SystemResolver implements Resolver using real exec and nix lookups.
type SystemResolver struct{}

var semverRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

func (SystemResolver) Which(binary string) string {
	path, err := exec.LookPath(binary)
	if err != nil {
		return ""
	}
	return path
}

func (SystemResolver) Version(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}
	out, err := exec.Command(parts[0], parts[1:]...).CombinedOutput()
	if err != nil {
		return ""
	}
	match := semverRe.FindString(string(out))
	return match
}

func (SystemResolver) NixStorePath(attr string) string {
	out, err := exec.Command("nix", "path-info", ".#dev-tools").CombinedOutput()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return ""
	}
	return strings.SplitN(path, "\n", 2)[0]
}

func (SystemResolver) IsNixProfile(path string) bool {
	if strings.HasPrefix(path, "/nix/store/") {
		return true
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(resolved, "/nix/store/")
}
