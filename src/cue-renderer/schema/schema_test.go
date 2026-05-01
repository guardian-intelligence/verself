package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
)

// loadSchema returns a cue.Value evaluating the schema package, so tests
// can validate fixture instances against #SSHPrincipal / #SSHCAConfig
// without spinning up the full topology graph.
func loadSchema(t *testing.T) (cue.Value, *cue.Context) {
	t.Helper()

	root := schemaRoot(t)

	cfg := &load.Config{Dir: root}
	insts := load.Instances([]string{"./schema"}, cfg)
	if len(insts) == 0 {
		t.Fatal("load.Instances: empty")
	}
	if err := insts[0].Err; err != nil {
		t.Fatalf("load schema: %v", err)
	}

	ctx := cuecontext.New()
	v := ctx.BuildInstance(insts[0])
	if err := v.Err(); err != nil {
		t.Fatalf("build schema: %v", err)
	}
	return v, ctx
}

func schemaRoot(t *testing.T) string {
	t.Helper()
	if srcdir := os.Getenv("TEST_SRCDIR"); srcdir != "" {
		ws := os.Getenv("TEST_WORKSPACE")
		if ws == "" {
			ws = "_main"
		}
		dir := filepath.Join(srcdir, ws, "src/cue-renderer")
		if _, err := os.Stat(filepath.Join(dir, "cue.mod", "module.cue")); err == nil {
			return dir
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for cur := wd; cur != "/" && cur != "."; cur = filepath.Dir(cur) {
		candidate := filepath.Join(cur, "src/cue-renderer")
		if _, err := os.Stat(filepath.Join(candidate, "cue.mod", "module.cue")); err == nil {
			return candidate
		}
		if _, err := os.Stat(filepath.Join(cur, "cue.mod", "module.cue")); err == nil {
			return cur
		}
	}
	t.Fatalf("could not locate src/cue-renderer/cue.mod/module.cue from %s", wd)
	return ""
}

// TestSSHPrincipal_RejectsMalformed asserts the SSH principal schema
// refuses every footgun shape we have observed:
//
//   - missing source_address_cidrs (a cert valid from anywhere)
//   - empty source_address_cidrs (same outcome via different syntax)
//   - automation principal with no force_command (workload-issued shell)
//   - max_ttl_seconds beyond the 24h ceiling
//   - max_ttl_seconds non-positive
//
// Each subtest unifies a malformed value with #SSHPrincipal and checks
// that .Validate(cue.Concrete(true)) returns an error mentioning the
// expected field. A passing test means the schema can no longer be
// landed with one of these regressions.
func TestSSHPrincipal_RejectsMalformed(t *testing.T) {
	schemaVal, ctx := loadSchema(t)
	def := schemaVal.LookupPath(cue.ParsePath("#SSHPrincipal"))
	// Don't call Err on the bare definition: #SSHPrincipal references a
	// disjunction that resolves only once a concrete role is unified in.
	// The Unify+Validate path below is what actually exercises the schema.
	if !def.Exists() {
		t.Fatal("#SSHPrincipal not found in schema package")
	}

	cases := []struct {
		name     string
		instance string
		wantErr  string
	}{
		{
			name: "missing_source_cidrs",
			instance: `{
				name: "naked"
				role: "operator"
				max_ttl_seconds: 900
			}`,
			wantErr: "source_address_cidrs",
		},
		{
			name: "empty_source_cidrs",
			instance: `{
				name: "naked"
				role: "operator"
				max_ttl_seconds: 900
				source_address_cidrs: []
			}`,
			wantErr: "source_address_cidrs",
		},
		{
			name: "automation_without_force_command",
			instance: `{
				name: "ci_runner"
				role: "automation"
				max_ttl_seconds: 60
				source_address_cidrs: ["10.0.0.0/8"]
			}`,
			wantErr: "force_command",
		},
		{
			name: "ttl_above_ceiling",
			instance: `{
				name: "operator_too_long"
				role: "operator"
				max_ttl_seconds: 86401
				source_address_cidrs: ["10.0.0.0/8"]
			}`,
			wantErr: "max_ttl_seconds",
		},
		{
			name: "ttl_non_positive",
			instance: `{
				name: "operator_zero_ttl"
				role: "operator"
				max_ttl_seconds: 0
				source_address_cidrs: ["10.0.0.0/8"]
			}`,
			wantErr: "max_ttl_seconds",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			val := ctx.CompileString(tc.instance)
			if err := val.Err(); err != nil {
				t.Fatalf("compile instance: %v", err)
			}
			merged := def.Unify(val)
			err := merged.Validate(cue.Concrete(true))
			if err == nil {
				t.Fatalf("expected error mentioning %q, got nil; merged value:\n%v",
					tc.wantErr, merged)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error to mention %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

// TestSSHPrincipal_AcceptsWellFormed proves the malformed-rejection
// tests above aren't false positives that fire on every shape — the
// schema must still admit the canonical operator and automation
// principals declared in instances/prod/config.cue.
func TestSSHPrincipal_AcceptsWellFormed(t *testing.T) {
	schemaVal, ctx := loadSchema(t)
	def := schemaVal.LookupPath(cue.ParsePath("#SSHPrincipal"))
	// Don't call Err on the bare definition: #SSHPrincipal references a
	// disjunction that resolves only once a concrete role is unified in.
	// The Unify+Validate path below is what actually exercises the schema.
	if !def.Exists() {
		t.Fatal("#SSHPrincipal not found in schema package")
	}

	cases := []struct {
		name     string
		instance string
	}{
		{
			name: "operator_with_pty",
			instance: `{
				name: "operator"
				role: "operator"
				max_ttl_seconds: 900
				source_address_cidrs: ["10.66.66.0/24"]
				permit_pty: true
			}`,
		},
		{
			name: "automation_with_force_command",
			instance: `{
				name: "canary"
				role: "automation"
				max_ttl_seconds: 60
				source_address_cidrs: ["127.0.0.1/32"]
				force_command: "/bin/true"
				permit_pty: false
			}`,
		},
		{
			name: "breakglass_24h",
			instance: `{
				name: "breakglass"
				role: "breakglass"
				max_ttl_seconds: 86400
				source_address_cidrs: ["0.0.0.0/0"]
			}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			val := ctx.CompileString(tc.instance)
			if err := val.Err(); err != nil {
				t.Fatalf("compile instance: %v", err)
			}
			merged := def.Unify(val)
			if err := merged.Validate(cue.Concrete(true)); err != nil {
				t.Fatalf("expected well-formed principal to validate, got: %v", err)
			}
		})
	}
}
