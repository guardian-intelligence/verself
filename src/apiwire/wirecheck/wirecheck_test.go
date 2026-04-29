package wirecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFileRejectsUnsafeInteger64Schemas(t *testing.T) {
	path := writeFixture(t, `
openapi: 3.1.0
components:
  schemas:
    UnsafeID:
      type: integer
      format: uint64
    NullableUnsafeID:
      type:
        - integer
        - "null"
      format: int64
`)

	violations, err := CheckFile(path)
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %#v", violations)
	}
	if !strings.Contains(violations[0].Path, "UnsafeID") {
		t.Fatalf("expected first violation path to include UnsafeID, got %q", violations[0].Path)
	}
	if !strings.Contains(violations[1].Path, "NullableUnsafeID") {
		t.Fatalf("expected second violation path to include NullableUnsafeID, got %q", violations[1].Path)
	}
}

func TestCheckFileAllowsSafeInteger64Schemas(t *testing.T) {
	path := writeFixture(t, `
openapi: 3.1.0
components:
  schemas:
    SafeCount:
      type: integer
      format: int64
      maximum: 9007199254740991
    ExplicitBigint:
      type: integer
      format: uint64
      x-js-wire: bigint
    DecimalString:
      type: string
      pattern: "^[0-9]+$"
`)

	violations, err := CheckFile(path)
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %#v", violations)
	}
}

func TestCheckFileRejectsUnsafeMaximum(t *testing.T) {
	path := writeFixture(t, `
openapi: 3.1.0
components:
  schemas:
    TooLarge:
      type: integer
      format: int64
      maximum: 9007199254740992
`)

	violations, err := CheckFile(path)
	if err != nil {
		t.Fatalf("CheckFile: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %#v", violations)
	}
}

func writeFixture(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
