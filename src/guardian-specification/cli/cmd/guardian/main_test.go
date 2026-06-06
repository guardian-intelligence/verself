package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestPositionalProfileAllowsFlagsAfterOperand(t *testing.T) {
	dir, _ := writeTestCUEDocument(t)
	var stderr bytes.Buffer
	var opts commandOptions
	var ok bool
	withWorkingDir(t, dir, func() {
		opts, ok = parseProfileFlags("guardian preflight", []string{"gamma", "-o", "json", "--dry-run"}, &stderr)
	})
	if !ok {
		t.Fatalf("parseProfileFlags failed: %s", stderr.String())
	}
	if opts.Profile != "gamma" {
		t.Fatalf("Profile = %q, want gamma", opts.Profile)
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
	withWorkingDir(t, dir, func() {
		code := run([]string{"preflight", "-f", path, "--render", "-o", "json"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("run exited %d, want 2; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
		}
	})
	if !strings.Contains(stderr.String(), "flag provided but not defined: -render") {
		t.Fatalf("stderr omitted unknown render flag:\n%s", stderr.String())
	}
}

func TestPreflightCUEDocument(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"preflight", "-f", path, "--dry-run", "-o", "json"}, &stdout, &stderr)
	})
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var result preflightResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode preflight result: %v\n%s", err, stdout.String())
	}
	if result.ReadyToFly != "no" {
		t.Fatalf("ready_to_fly = %q, want no for dry run\n%s", result.ReadyToFly, stdout.String())
	}
	if result.Profile != "gamma" {
		t.Fatalf("profile = %q, want gamma", result.Profile)
	}
	if result.Status != "ready" {
		t.Fatalf("status = %q, want ready for dry run", result.Status)
	}
	if result.Access.Status != "pending" {
		t.Fatalf("dry-run access status = %q, want pending", result.Access.Status)
	}
	if result.Kernel.Status != "pending" {
		t.Fatalf("dry-run kernel status = %q, want pending", result.Kernel.Status)
	}
	if result.Entrypoint.Name != "gamma" {
		t.Fatalf("entrypoint = %#v, want gamma FlyProcedure", result.Entrypoint)
	}
}

func TestPreflightExplicitRootConfigFile(t *testing.T) {
	dir, _ := writeTestCUEDocument(t)
	writeTestGuardianConfig(t, dir)
	configPath := filepath.Join(dir, ".config", "guardian", "guardian.cue")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"preflight", "-f", configPath, "gamma", "--dry-run", "-o", "json"}, &stdout, &stderr)
	})
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var result preflightResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode preflight result: %v\n%s", err, stdout.String())
	}
	if result.Profile != "gamma" {
		t.Fatalf("profile = %q, want gamma", result.Profile)
	}
	if result.Status != "ready" {
		t.Fatalf("status = %q, want ready", result.Status)
	}
}

func TestPreflightProfileDiscovery(t *testing.T) {
	dir, _ := writeTestCUEDocument(t)
	writeTestGuardianConfig(t, dir)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"preflight", "gamma", "--dry-run", "-o", "json"}, &stdout, &stderr)
	})
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var result preflightResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode preflight result: %v\n%s", err, stdout.String())
	}
	if result.Profile != "gamma" {
		t.Fatalf("profile = %q, want gamma", result.Profile)
	}
}

func TestPreflightHooksVerifyDigest(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"preflight", "-f", path, "-o", "json"}, &stdout, &stderr)
	})
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s\nstdout:\n%s", code, stderr.String(), stdout.String())
	}
	var result preflightResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode preflight result: %v\n%s", err, stdout.String())
	}
	if result.ReadyToFly != "yes" {
		t.Fatalf("ready_to_fly = %q, want yes\n%s", result.ReadyToFly, stdout.String())
	}
	if result.Upload.Digest == "" {
		t.Fatalf("upload digest was not reported: %#v", result.Upload)
	}
	if result.Upload.Run.Status != "ready" || result.Upload.Extract.Status != "ready" || result.Upload.Verify.Status != "ready" {
		t.Fatalf("hooks not ready: run=%#v extract=%#v verify=%#v", result.Upload.Run, result.Upload.Extract, result.Upload.Verify)
	}
	if result.Access.Status != "ready" {
		t.Fatalf("access hook not ready: %#v", result.Access)
	}
	if result.Kernel.Status != "ready" {
		t.Fatalf("kernel hooks not ready: %#v", result.Kernel)
	}
}

func TestFlyDryRunCUEDocument(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"fly", "-f", path, "--dry-run", "-o", "json"}, &stdout, &stderr)
	})
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
		t.Fatalf("ready_to_fly = %q, want yes for dry run\n%s", result.ReadyToFly, stdout.String())
	}
}

func TestFlyLiveCUEDocument(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"fly", "-f", path, "-o", "json"}, &stdout, &stderr)
	})
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s\nstdout:\n%s", code, stderr.String(), stdout.String())
	}
	var result flyResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode fly result: %v\n%s", err, stdout.String())
	}
	if result.ReadyToFly != "yes" {
		t.Fatalf("ready_to_fly = %q, want yes\n%s", result.ReadyToFly, stdout.String())
	}
	found := false
	for _, cond := range result.Conditions {
		if cond.Type == "PreflightReady" && cond.Status == "True" && cond.Reason == "ReadyToFly" {
			found = true
		}
	}
	if !found {
		t.Fatalf("PreflightReady true condition not found: %#v", result.Conditions)
	}
}

func TestFlyRunExecutesVerifiedRemoteCatalogTool(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	writeTestToolCatalog(t, dir)
	repoRoot := filepath.Join(dir, "remote-repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir remote workspace: %v", err)
	}
	remoteGuardian := filepath.Join(dir, "remote-guardian")
	if err := os.WriteFile(remoteGuardian, []byte(`#!/bin/sh
set -eu
if [ "$1" = "tool" ] && [ "$2" = "verify" ] && [ "$3" = "bazel" ]; then
  printf '{"status":"ready"}\n'
  exit 0
fi
if [ "$1" = "run" ] && [ "$2" = "bazel" ] && [ "$3" = "--" ]; then
  shift 3
  printf 'remote bazel: %s\n' "$*"
  exit 0
fi
exit 64
`), 0o755); err != nil {
		t.Fatalf("write remote guardian: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"fly", "run", "-f", path, "--", "bazel", "test", "//src/guardian-specification/..."}, &stdout, &stderr)
	})
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s\nstdout:\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "remote bazel: test //src/guardian-specification/...") {
		t.Fatalf("stdout omitted remote bazel invocation:\n%s", stdout.String())
	}
}

func testFlyResult() flyResult {
	return flyResult{
		Name:           "gamma",
		ReadyToFly:     "yes",
		ExecutionMode:  "dry_run",
		ResourceDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		UploadDigest:   "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		Entrypoint: resourceRefResult{
			APIVersion: "guardian.guardianintelligence.org/v1alpha1",
			Kind:       "FlyProcedure",
			Name:       "gamma",
		},
		Nomad: hookResult{
			Argv:   []string{"sh", "-c", "test -d extracted"},
			Status: "ready",
			Reason: "HookSucceeded",
		},
		Conditions: []condition{{
			Type:     "PreflightReady",
			Status:   "True",
			Reason:   "ReadyToFly",
			Message:  "preflight inputs are ready for fly",
			Resource: "preflight",
		}},
	}
}

func writeTestToolCatalog(t *testing.T, dir string) {
	t.Helper()
	configDir := filepath.Join(dir, ".config", "guardian")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir guardian config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(`tools: bazel: platforms: "linux/amd64": {
	ref: "oci.verself.sh/tools/bazel@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	executable: "bazel"
	admission: "admitted"
}
`), 0o644); err != nil {
		t.Fatalf("write tool catalog: %v", err)
	}
}

func writeTestGuardianConfig(t *testing.T, dir string) {
	t.Helper()
	configDir := filepath.Join(dir, ".config", "guardian")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir guardian config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "guardian.cue"), []byte(`defaultProfile: "gamma"
profiles: gamma: document: "../../gamma.cue"
`), 0o644); err != nil {
		t.Fatalf("write guardian config: %v", err)
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
	if err := os.WriteFile(filepath.Join(dir, "MODULE.bazel"), []byte("module(name = \"guardian_test\")\n"), 0o644); err != nil {
		t.Fatalf("write MODULE.bazel: %v", err)
	}
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
	if err := os.Mkdir(filepath.Join(dir, "bazel-bin"), 0o755); err != nil {
		t.Fatalf("mkdir bazel-bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bazel-bin", "guardian"), []byte("built guardian\n"), 0o755); err != nil {
		t.Fatalf("write built artifact: %v", err)
	}
	path := filepath.Join(dir, "gamma.cue")
	repoRoot := filepath.Join(dir, "remote-repo")
	remoteGuardian := filepath.Join(dir, "remote-guardian")
	document := fmt.Sprintf(`package gamma

entrypoint: {
	apiVersion: "guardian.guardianintelligence.org/v1alpha1"
	kind:       "FlyProcedure"
	name:       "gamma"
}

resources: [
	{
		apiVersion: "guardian.guardianintelligence.org/v1alpha1"
		kind:       "FlyProcedure"
		metadata: name: "gamma"
		spec: {
			substrateRef: {
				apiVersion: "substrate.guardianintelligence.org/v1alpha1"
				kind:       "Substrate"
				name:       "local"
			}
			nomad: run: argv: ["sh", "-c", "test -d extracted"]
		}
	},
	{
		apiVersion: "substrate.guardianintelligence.org/v1alpha1"
		kind:       "Substrate"
		metadata: name: "local"
		spec: {
				access: argv: ["sh", "-c", "test -f MODULE.bazel"]
				upload: {
					run: argv: ["sh", "-c", "rm -rf uploaded && mkdir -p uploaded && cp -a MODULE.bazel guardian bazel-bin .guardian uploaded/"]
					extract: argv: ["sh", "-c", "rm -rf extracted && cp -a uploaded extracted"]
					verify: argv: ["sh", "-c", "test -f extracted/.guardian/fly/document.json && find extracted -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum"]
				}
					kernel: {
						openbaoPrepare: argv: ["sh", "-c", "test -d extracted"]
						nomad: argv: ["sh", "-c", "test -d extracted"]
						verify: argv: ["sh", "-c", "test -d extracted"]
					}
					remote: {
						repoRoot: %q
						guardian: %q
						ssh: ["sh", "-c"]
					}
			}
		},
	{
		apiVersion: "cloudflare.guardianintelligence.org/v1alpha1"
		kind:       "AccountAuthority"
		metadata: name: "cloudflare-account-admin"
		spec: accountID: "acct-test"
	},
	{
		apiVersion: "networking.guardianintelligence.org/v1alpha1"
		kind:       "PublicOrigin"
		metadata: name: "product"
		spec: url: "https://gamma.verself.sh"
	},
]
`, repoRoot, remoteGuardian)
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatalf("write gamma.cue: %v", err)
	}
	return dir, path
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	fn()
}
