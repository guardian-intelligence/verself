// Package toolspin asserts that the single bazel pin digest is identical in
// every place it must be hand-mirrored. The bootstrap scripts run before Bazel
// exists, so they cannot read the module graph; the make-skill release lockfile
// drives an independent rules_multitool hub. This test is the seam that keeps
// those hand-authored copies from drifting away from the canonical pin in
// //src/guardian-specification:guardian_tools.MODULE.bazel.
package toolspin

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// canonical: controller_http_file(guardian_tool_bazel) linux_amd64_sha256.
var canonicalRe = regexp.MustCompile(`linux_amd64_sha256\s*=\s*"([0-9a-f]{64})"`)

// bootstrap: bazel_sha256="..." in the stage-zero shell pivot.
var bootstrapRe = regexp.MustCompile(`bazel_sha256="([0-9a-f]{64})"`)

func TestBazelPinIsSingleSourceOfTruth(t *testing.T) {
	canonical := findOne(t, "src/guardian-specification/guardian_tools.MODULE.bazel", canonicalRe)
	bootstrapLinux := findOne(t, "src/tools/dev/bootstrap/bootstrap-linux-amd64", bootstrapRe)

	if bootstrapLinux != canonical {
		t.Errorf("bootstrap-linux-amd64 bazel digest %s != canonical pin %s; reconcile bootstrap with guardian_tools.MODULE.bazel", bootstrapLinux, canonical)
	}

	// The darwin bootstrap pins only aspect (its bazel is the same controller
	// pin resolved per-host), so there is no darwin bazel digest to mirror.

	release := findBazelLockDigest(t, "src/make-skill/release/tools.lock.json")
	if release != canonical {
		t.Errorf("make-skill release lockfile bazel digest %s != canonical pin %s", release, canonical)
	}
}

func findOne(t *testing.T, path string, re *regexp.Regexp) string {
	t.Helper()
	data := readSource(t, path)
	m := re.FindStringSubmatch(data)
	if m == nil {
		t.Fatalf("%s: no bazel digest matched %s", path, re)
	}
	return m[1]
}

// findBazelLockDigest pulls the bazel entry's sha256 out of a rules_multitool
// lockfile without a JSON dependency: it scans for the first sha256 after the
// "bazel" key.
func findBazelLockDigest(t *testing.T, path string) string {
	t.Helper()
	data := readSource(t, path)
	idx := strings.Index(data, `"bazel"`)
	if idx < 0 {
		t.Fatalf("%s: no bazel entry", path)
	}
	m := regexp.MustCompile(`"sha256":\s*"([0-9a-f]{64})"`).FindStringSubmatch(data[idx:])
	if m == nil {
		t.Fatalf("%s: no sha256 after bazel entry", path)
	}
	return m[1]
}

func readSource(t *testing.T, rel string) string {
	t.Helper()
	srcdir := strings.TrimSpace(os.Getenv("TEST_SRCDIR"))
	candidates := []string{rel}
	if srcdir != "" {
		candidates = append([]string{
			srcdir + "/_main/" + rel,
			srcdir + "/" + rel,
		}, candidates...)
	}
	for _, path := range candidates {
		if b, err := os.ReadFile(path); err == nil {
			return string(b)
		}
	}
	t.Fatalf("read %s from runfiles or source tree", rel)
	return ""
}
