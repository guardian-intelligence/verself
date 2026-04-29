// wire_check_test asserts the Bazel-rendered billing OpenAPI 3.1 spec
// satisfies the apiwire numeric-safety contract. The spec path is
// passed as the first program arg by the BUILD rule and resolved
// against TEST_SRCDIR/TEST_WORKSPACE so the test consumes the
// bazel-out artifact directly — no on-disk YAML to drift against.
package openapi_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/verself/apiwire/wirecheck"
)

func TestBillingOpenAPI31SatisfiesWireContract(t *testing.T) {
	if len(os.Args) < 2 {
		t.Fatalf("expected spec path as first arg; got %v", os.Args)
	}
	rel := os.Args[1]
	srcdir := os.Getenv("TEST_SRCDIR")
	if srcdir == "" {
		t.Fatalf("TEST_SRCDIR not set; this test must run under bazelisk test")
	}
	ws := os.Getenv("TEST_WORKSPACE")
	if ws == "" {
		ws = "_main"
	}
	path := filepath.Join(srcdir, ws, rel)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("spec %s not present in test runfiles: %v", path, err)
	}
	violations, err := wirecheck.CheckFile(path)
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	if len(violations) > 0 {
		for _, v := range violations {
			t.Errorf("%s", v)
		}
		t.Fatalf("billing openapi-3.1 has %d wire violation(s)", len(violations))
	}
}
