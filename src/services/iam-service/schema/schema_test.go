package schema

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerselfSchemaValidatesWithPinnedZed(t *testing.T) {
	if os.Getenv("RUNFILES_DIR") == "" && os.Getenv("TEST_SRCDIR") == "" {
		t.Skip("pinned zed schema validation runs under //src/services/iam-service/schema:schema_test")
	}

	zedPath := runfile(t, "src/services/iam-service/schema/zed.bin")
	schemaPath := runfile(t, "src/services/iam-service/schema/verself.zed")

	cmd := exec.Command(zedPath, "validate", "--fail-on-warn", schemaPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zed validate: %v\n%s", err, output)
	}
}

func runfile(t *testing.T, rel string) string {
	t.Helper()

	workspace := os.Getenv("TEST_WORKSPACE")
	if workspace == "" {
		workspace = "_main"
	}

	candidates := []string{rel}
	for _, base := range []string{os.Getenv("RUNFILES_DIR"), os.Getenv("TEST_SRCDIR")} {
		if base == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(base, rel),
			filepath.Join(base, workspace, rel),
			filepath.Join(base, "_main", rel),
		)
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %s: %v", path, err)
		}
	}
	t.Fatalf("runfile %q not found; checked %s", rel, strings.Join(candidates, ", "))
	return ""
}
