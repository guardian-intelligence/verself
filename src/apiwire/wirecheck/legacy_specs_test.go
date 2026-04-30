// legacy_specs_test asserts the apiwire numeric-safety contract on the
// committed OpenAPI 3.1 specs of every service whose spec is not yet a
// Bazel-graph artifact (i.e. every service except billing, which has
// its own //src/billing-service/openapi:wire_check_test against the
// bazel-out spec).
//
// As services migrate to the verself_openapi_yaml + go_test pattern
// (see //src/billing-service/openapi/BUILD.bazel for the template),
// drop the corresponding entry from legacySpecs and add a per-service
// wire_check_test alongside the new spec rule. When legacySpecs goes
// empty, retire this whole file.
//
// The committed YAML files are declared as Bazel data so editing one
// invalidates this test; the test then re-runs `wirecheck.CheckFile`
// on every spec, even unchanged ones, to keep cross-service drift
// (e.g. a shared apiwire DTO regression) catchable from any single
// edit.
package wirecheck_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/verself/apiwire/wirecheck"
)

// legacySpecs is workspace-relative; resolved at runtime against
// TEST_SRCDIR/TEST_WORKSPACE so the test reads the runfiles copy
// staged by `data` in BUILD.bazel.
var legacySpecs = []string{
	"src/governance-service/openapi/openapi-3.1.yaml",
	"src/identity-service/openapi/openapi-3.1.yaml",
	"src/identity-service/openapi/internal-openapi-3.1.yaml",
	"src/secrets-service/openapi/openapi-3.1.yaml",
	"src/source-code-hosting-service/openapi/openapi-3.1.yaml",
	"src/source-code-hosting-service/openapi/internal-openapi-3.1.yaml",
	"src/mailbox-service/openapi/openapi-3.1.yaml",
	"src/object-storage-service/openapi/openapi-3.1.yaml",
	"src/profile-service/openapi/openapi-3.1.yaml",
	"src/profile-service/openapi/internal-openapi-3.1.yaml",
	"src/notifications-service/openapi/openapi-3.1.yaml",
	"src/projects-service/openapi/openapi-3.1.yaml",
	"src/projects-service/openapi/internal-openapi-3.1.yaml",
	"src/sandbox-rental-service/openapi/openapi-3.1.yaml",
	"src/sandbox-rental-service/openapi/internal-openapi-3.1.yaml",
}

func TestLegacySpecsSatisfyWireContract(t *testing.T) {
	srcdir := os.Getenv("TEST_SRCDIR")
	if srcdir == "" {
		t.Fatalf("TEST_SRCDIR not set; this test must run under bazelisk test")
	}
	ws := os.Getenv("TEST_WORKSPACE")
	if ws == "" {
		ws = "_main"
	}
	for _, rel := range legacySpecs {
		t.Run(rel, func(t *testing.T) {
			path := filepath.Join(srcdir, ws, rel)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("spec %s not present in test runfiles: %v", path, err)
			}
			violations, err := wirecheck.CheckFile(path)
			if err != nil {
				t.Fatalf("CheckFile: %v", err)
			}
			for _, v := range violations {
				t.Errorf("%s", v)
			}
		})
	}
}
