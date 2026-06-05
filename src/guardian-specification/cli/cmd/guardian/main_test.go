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
	result := flyResult{
		Name:               "gamma",
		ReadyToFly:         "yes",
		ExecutionMode:      "dry_run",
		StaticConfigDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		SeedDigest:         "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		SeedRoot:           "/var/lib/guardian/seeds/sha256-2222222222222222222222222222222222222222222222222222222222222222",
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

func TestBoardCUEDocument(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"board", path, "--repo-root", dir, "-o", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var result boardResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode board result: %v\n%s", err, stdout.String())
	}
	if result.ReadyToFly != "yes" {
		t.Fatalf("ready_to_fly = %q, want yes\n%s", result.ReadyToFly, stdout.String())
	}
	if result.Seed.Digest == "" || result.Seed.Root == "" {
		t.Fatalf("board result omitted seed digest/root:\n%s", stdout.String())
	}
	if result.Access.Target != "ubuntu@127.0.0.1:22" {
		t.Fatalf("access target = %q, want ubuntu@127.0.0.1:22", result.Access.Target)
	}
}

func TestBoardRenderCUEDocument(t *testing.T) {
	_, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"board", path, "--render", "-o", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var doc guardianDocument
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("decode rendered document: %v\n%s", err, stdout.String())
	}
	if doc.Kind != "FlyProcedure" {
		t.Fatalf("kind = %q, want FlyProcedure", doc.Kind)
	}
	if doc.Name != "" {
		t.Fatalf("name = %q, want optional name omitted", doc.Name)
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
	if len(result.Jobs) != 1 || result.Jobs[0].Status != "ready" {
		t.Fatalf("unexpected jobs: %#v", result.Jobs)
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
		t.Fatalf("write guardian seed source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nomad.hcl"), []byte("job \"openbao\" {}\n"), 0o644); err != nil {
		t.Fatalf("write nomad job: %v", err)
	}
	path := filepath.Join(dir, "gamma.cue")
	if err := os.WriteFile(path, []byte(`package gamma

kind: "FlyProcedure"

staticConfig: {
	baseURL:        "https://gamma.guardianintelligence.org"
	credentialsRef: "gamma-credentials"
}

board: {
	substrate: stateDir: "/var/lib/guardian"
	access: ssh: {
		host:           "127.0.0.1"
		port:           22
		user:           "ubuntu"
		knownHostsFile: "~/.ssh/known_hosts"
	}
	seed: {
		targetRoot: "/var/lib/guardian/seeds"
		paths: [{
			source: "guardian"
			target: "bin/guardian"
			mode:   "0755"
		}]
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
