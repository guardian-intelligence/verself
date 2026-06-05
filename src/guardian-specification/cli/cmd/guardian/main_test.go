package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	toon "github.com/toon-format/toon-go"
	"gopkg.in/yaml.v3"
)

func TestWriteOutputFormats(t *testing.T) {
	result := testFlyResult()

	for _, format := range []string{"json", "yaml", "yml", "toml", "toon"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeOutput(&out, format, result); err != nil {
				t.Fatalf("writeOutput(%s): %v", format, err)
			}
			if !strings.Contains(out.String(), "ready_to_fly") {
				t.Fatalf("%s output omitted stable ready_to_fly key:\n%s", format, out.String())
			}
			assertDecodable(t, format, out.Bytes())
		})
	}
}

func TestProjectionOutputFormatsRejected(t *testing.T) {
	result := testFlyResult()

	for _, format := range []string{"text", "table", "dot", "mermaid"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			err := writeOutput(&out, format, result)
			if err == nil {
				t.Fatalf("writeOutput(%s) succeeded, want unsupported encoding failure", format)
			}
			if !strings.Contains(err.Error(), "unsupported encoding") {
				t.Fatalf("writeOutput(%s) error = %q, want unsupported encoding", format, err)
			}
		})
	}
}

func TestPositionalFileAllowsFlagsAfterOperand(t *testing.T) {
	var stderr bytes.Buffer
	opts, ok := parseCommonFlags("guardian board", []string{"gamma.cue", "-o", "json", "--dry-run"}, &stderr)
	if !ok {
		t.Fatalf("parseCommonFlags failed: %s", stderr.String())
	}
	if opts.File != "gamma.cue" {
		t.Fatalf("File = %q, want gamma.cue", opts.File)
	}
	if opts.Output != "json" {
		t.Fatalf("Output = %q, want json", opts.Output)
	}
	if !opts.DryRun {
		t.Fatalf("DryRun = false, want true")
	}
}

func TestRenderFlagRejected(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"board", path, "--repo-root", dir, "--render", "-o", "json"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exited %d, want 2; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -render") {
		t.Fatalf("stderr omitted unknown render flag:\n%s", stderr.String())
	}
}

func TestBoardCUEDocument(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"board", path, "--repo-root", dir, "--dry-run", "-o", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var result boardResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode board result: %v\n%s", err, stdout.String())
	}
	if result.ReadyToFly != "no" {
		t.Fatalf("ready_to_fly = %q, want no for dry run\n%s", result.ReadyToFly, stdout.String())
	}
	if result.Upload.Digest == "" {
		t.Fatalf("board result omitted upload digest:\n%s", stdout.String())
	}
	if result.Access.Status != "pending" {
		t.Fatalf("dry-run access status = %q, want pending", result.Access.Status)
	}
	if result.StaticConfig.BaseURL != "https://gamma.guardianintelligence.org" {
		t.Fatalf("static config base URL = %q, want gamma URL", result.StaticConfig.BaseURL)
	}
	if result.Nomad.Address != "http://127.0.0.1:4646" || len(result.Jobs) != 1 {
		t.Fatalf("board result omitted declared Nomad plan: %#v jobs=%#v", result.Nomad, result.Jobs)
	}
}

func TestBoardHooksVerifyDigest(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"board", path, "--repo-root", dir, "-o", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s\nstdout:\n%s", code, stderr.String(), stdout.String())
	}
	var result boardResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode board result: %v\n%s", err, stdout.String())
	}
	if result.ReadyToFly != "yes" {
		t.Fatalf("ready_to_fly = %q, want yes\n%s", result.ReadyToFly, stdout.String())
	}
	if result.Upload.Digest == "" || result.Upload.ObservedDigest != result.Upload.Digest {
		t.Fatalf("upload digests did not match: %#v", result.Upload)
	}
	if result.Upload.Run.Status != "ready" || result.Upload.Verify.Status != "ready" {
		t.Fatalf("hooks not ready: run=%#v verify=%#v", result.Upload.Run, result.Upload.Verify)
	}
	if result.Access.Status != "ready" {
		t.Fatalf("access hook not ready: %#v", result.Access)
	}
}

func TestFlyDryRunCUEDocument(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"fly", path, "--repo-root", dir, "--dry-run", "-o", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var result flyResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode fly result: %v\n%s", err, stdout.String())
	}
	if result.ExecutionMode != "dry_run" {
		t.Fatalf("execution_mode = %q, want dry_run", result.ExecutionMode)
	}
	if result.ReadyToFly != "yes" {
		t.Fatalf("ready_to_fly = %q, want yes for dry-run plan\n%s", result.ReadyToFly, stdout.String())
	}
	if len(result.Jobs) != 1 || result.Jobs[0].Status != "ready" {
		t.Fatalf("unexpected jobs: %#v", result.Jobs)
	}
}

func testFlyResult() flyResult {
	return flyResult{
		Name:               "gamma",
		ReadyToFly:         "yes",
		ExecutionMode:      "dry_run",
		StaticConfigDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		UploadDigest:       "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		Nomad:              nomadPlanResult{Address: "http://127.0.0.1:4646", Namespace: "default"},
		Jobs: []nomadJobResult{{
			Path:        "nomad.hcl",
			RequiredFor: []string{"recovery"},
			Status:      "ready",
			Reason:      "PathResolved",
		}},
		Conditions: []condition{{
			Type:     "BoardingReady",
			Status:   "True",
			Reason:   "ReadyToFly",
			Message:  "boarding inputs are ready for fly",
			Resource: "board",
		}},
	}
}

func assertDecodable(t *testing.T, format string, data []byte) {
	t.Helper()
	switch format {
	case "json":
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("decode json: %v", err)
		}
	case "yaml", "yml":
		var decoded map[string]any
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("decode yaml: %v", err)
		}
	case "toml":
		var decoded map[string]any
		if err := toml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("decode toml: %v", err)
		}
	case "toon":
		if _, err := toon.Decode(data); err != nil {
			t.Fatalf("decode toon: %v", err)
		}
	default:
		t.Fatalf("test does not handle format %q", format)
	}
}

func writeTestCUEDocument(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "cue.mod"), 0o755); err != nil {
		t.Fatalf("mkdir cue.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cue.mod", "module.cue"), []byte(`module: "guardian.test"
language: version: "v0.11.0"
`), 0o644); err != nil {
		t.Fatalf("write module.cue: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "guardian"), []byte("guardian binary\n"), 0o755); err != nil {
		t.Fatalf("write guardian source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nomad.hcl"), []byte("job \"openbao\" {}\n"), 0o644); err != nil {
		t.Fatalf("write nomad job: %v", err)
	}
	writeRequiredArtifacts(t, dir)
	path := filepath.Join(dir, "gamma.cue")
	if err := os.WriteFile(path, []byte(`package gamma

kind: "FlyProcedure"

staticConfig: {
	baseURL:        "https://gamma.guardianintelligence.org"
	credentialsRef: "gamma-credentials"
}

board: {
	access: argv: ["sh", "-c", "test -r \"$GUARDIAN_UPLOAD_BUNDLE\""]
	upload: {
		run: argv: ["sh", "-c", "cp \"$GUARDIAN_UPLOAD_BUNDLE\" upload.tar.zst"]
		verify: argv: ["sh", "-c", "sha256sum upload.tar.zst"]
	}
}

nomad: {
	address:   "http://127.0.0.1:4646"
	namespace: "default"
	jobs: [{
		path:        "nomad.hcl"
		requiredFor: ["recovery"]
	}]
}
`), 0o644); err != nil {
		t.Fatalf("write gamma.cue: %v", err)
	}
	return dir, path
}

func writeRequiredArtifacts(t *testing.T, dir string) {
	t.Helper()
	artifacts := map[string][]byte{
		"bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian": []byte("guardian binary\n"),
		"bazel-bin/src/infrastructure-components/nomad/nomad-runtime.tar":          []byte("nomad runtime\n"),
	}
	for rel, data := range artifacts {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir artifact parent: %v", err)
		}
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatalf("write artifact %s: %v", rel, err)
		}
	}
}
