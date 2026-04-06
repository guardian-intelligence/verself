package doctor

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type Status int

const (
	OK              Status = iota // in PATH, version matches
	Missing                       // not in PATH
	VersionMismatch               // in PATH, wrong version
)

type ToolSpec struct {
	Name       string // binary name: "sops"
	VersionCmd string // shell command: "sops --version"
	Expected   string // pinned version: "3.12.2"
}

type CheckResult struct {
	Spec      ToolSpec
	Status    Status
	ActualVer string // "" if missing
	BinPath   string // "" if missing
}

type Summary struct {
	OK              int
	Missing         int
	VersionMismatch int
}

// Resolver abstracts exec lookups so tests can mock them.
type Resolver interface {
	Which(binary string) string
	Version(cmd string) string
}

// Check evaluates a single tool against the resolver.
func Check(spec ToolSpec, r Resolver) CheckResult {
	binPath := r.Which(spec.Name)
	if binPath == "" {
		return CheckResult{Spec: spec, Status: Missing}
	}

	actual := r.Version(spec.VersionCmd)
	if actual == spec.Expected {
		return CheckResult{Spec: spec, Status: OK, ActualVer: actual, BinPath: binPath}
	}

	return CheckResult{Spec: spec, Status: VersionMismatch, ActualVer: actual, BinPath: binPath}
}

// CheckAll runs the full manifest concurrently, returns results in manifest order.
func CheckAll(manifest []ToolSpec, r Resolver) ([]CheckResult, Summary) {
	results := make([]CheckResult, len(manifest))
	var wg sync.WaitGroup
	wg.Add(len(manifest))
	for i, spec := range manifest {
		go func(i int, spec ToolSpec) {
			defer wg.Done()
			results[i] = Check(spec, r)
		}(i, spec)
	}
	wg.Wait()

	var s Summary
	for _, cr := range results {
		switch cr.Status {
		case OK:
			s.OK++
		case Missing:
			s.Missing++
		case VersionMismatch:
			s.VersionMismatch++
		}
	}
	return results, s
}

// SystemResolver implements Resolver using real exec lookups.
type SystemResolver struct{}

// Matches version numbers with 2+ components: "1.26", "3.12.2", "26.3.2.3".
var semverRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)*`)

func (s *SystemResolver) Which(binary string) string {
	path, err := exec.LookPath(binary)
	if err != nil {
		return ""
	}
	// Resolve symlinks to get the real path.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func (s *SystemResolver) Version(cmd string) string {
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
