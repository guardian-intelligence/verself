package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestReadBazelBuildOutputs(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "bazel-out", "k8-fastbuild", "bin", "pkg", "tool")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir source output: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("tool bytes\n"), 0o755); err != nil {
		t.Fatalf("write source output: %v", err)
	}
	bepPath := filepath.Join(dir, ".guardian", "build", "build-events.json")
	if err := os.MkdirAll(filepath.Dir(bepPath), 0o755); err != nil {
		t.Fatalf("mkdir BEP dir: %v", err)
	}
	bep := fmt.Sprintf(`{"id":{"namedSet":{"id":"0"}},"namedSetOfFiles":{"files":[{"name":"pkg/tool","uri":"file://%s","pathPrefix":["bazel-out","k8-fastbuild","bin"],"digest":"sha256-test","length":"11"}]}}
{"id":{"targetCompleted":{"label":"//pkg:tool"}},"completed":{"success":true,"outputGroup":[{"name":"default","fileSets":[{"id":"0"}]}]}}
`, sourcePath)
	if err := os.WriteFile(bepPath, []byte(bep), 0o644); err != nil {
		t.Fatalf("write BEP: %v", err)
	}
	outputs, err := readBazelBuildOutputs(bepPath)
	if err != nil {
		t.Fatalf("read outputs: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("outputs = %#v, want one output", outputs)
	}
	if outputs[0].TargetPath != "bazel-bin/pkg/tool" {
		t.Fatalf("target path = %q", outputs[0].TargetPath)
	}
	if outputs[0].SourceURI != "file://"+filepath.ToSlash(sourcePath) {
		t.Fatalf("source uri = %q", outputs[0].SourceURI)
	}
}

func TestBazelBuildEventJSONPathUsesExistingFlag(t *testing.T) {
	dir := t.TempDir()
	path, ok := bazelBuildEventJSONPath([]string{"build", "--build_event_json_file=.aspect/bep/build.json", "//pkg:tool"}, dir)
	if !ok {
		t.Fatalf("BEP path flag was not detected")
	}
	if path != filepath.Join(dir, ".aspect", "bep", "build.json") {
		t.Fatalf("BEP path = %q", path)
	}
	abs := filepath.Join(dir, "events.json")
	path, ok = bazelBuildEventJSONPath([]string{"build", "--build_event_json_file", abs, "//pkg:tool"}, dir)
	if !ok {
		t.Fatalf("separate BEP path flag was not detected")
	}
	if path != abs {
		t.Fatalf("absolute BEP path = %q", path)
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
	if result.Preflight.Status != "pending" {
		t.Fatalf("dry-run preflight status = %q, want pending", result.Preflight.Status)
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

func TestPreflightRunsAnsiblePlaybook(t *testing.T) {
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
	if result.Upload.Status != "ready" {
		t.Fatalf("upload status = %q, want ready: %#v", result.Upload.Status, result.Upload)
	}
	if result.Preflight.Status != "ready" {
		t.Fatalf("preflight playbook not ready: %#v", result.Preflight)
	}
	if result.Preflight.Playbook != "preflight.yml" {
		t.Fatalf("playbook = %q, want preflight.yml", result.Preflight.Playbook)
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

func TestRunListUsesCatalog(t *testing.T) {
	dir, _ := writeTestCUEDocument(t)
	writeTestToolCatalog(t, dir)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"run", "--list", "-o", "json"}, &stdout, &stderr)
	})
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var result toolListResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode tool list: %v\n%s", err, stdout.String())
	}
	if len(result.Tools) != 1 || result.Tools[0] != "bazel" {
		t.Fatalf("tools = %#v, want [bazel]", result.Tools)
	}
}

func TestRunInstallShimsUsesRunFlag(t *testing.T) {
	dir, _ := writeTestCUEDocument(t)
	writeTestToolCatalog(t, dir)
	binDir := filepath.Join(dir, "bin")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"run", "--install-shims", "--bin-dir", binDir, "-o", "json"}, &stdout, &stderr)
	})
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var result toolShimResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode shim install: %v\n%s", err, stdout.String())
	}
	wantShim := filepath.Join(binDir, "bazel")
	if len(result.Installed) != 1 || result.Installed[0] != wantShim {
		t.Fatalf("installed = %#v, want [%s]", result.Installed, wantShim)
	}
	if target, err := os.Readlink(wantShim); err != nil {
		t.Fatalf("shim missing: %v", err)
	} else if target == "" {
		t.Fatalf("shim target is empty")
	}
}

func TestRunVerifyUsesToolFlag(t *testing.T) {
	dir, _ := writeTestCUEDocument(t)
	writeExecutableTestToolCatalog(t, dir)
	// Clear any inherited GUARDIAN_TOOLS_ROOT so resolution uses the workspace
	// tool root this helper materialized.
	t.Setenv("GUARDIAN_TOOLS_ROOT", "")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"run", "bazel", "--verify", "-o", "json"}, &stdout, &stderr)
	})
	if code != 0 {
		t.Fatalf("run exited %d, stderr:\n%s", code, stderr.String())
	}
	var result toolWhichResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode tool verify: %v\n%s", err, stdout.String())
	}
	if result.Tool != "bazel" || result.Status != "ready" || result.Source != "workspace" {
		t.Fatalf("verify result = %#v, want bazel ready from workspace", result)
	}
	if _, err := os.Stat(result.Executable); err != nil {
		t.Fatalf("verified executable missing: %v", err)
	}
}

func TestToolCommandRemovedSuggestsRun(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"tool", "verify", "bazel"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exited %d, want 2; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"guardian: unknown command: tool",
		"Did you mean?",
		"guardian run bazel --verify",
		"usage:",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr omitted %q:\n%s", want, stderr.String())
		}
	}
}

func TestUnknownCommandSuggestsSiblingAndPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"prefligth"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exited %d, want 2; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"unknown command: prefligth",
		"guardian preflight",
		"usage:",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr omitted %q:\n%s", want, stderr.String())
		}
	}
}

func TestRunUnknownFlagSuggestsVerifyAndPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"run", "bazel", "--verfy"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exited %d, want 2; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"flag provided but not defined: -verfy",
		"--verify",
		"guardian run",
		"usage:",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr omitted %q:\n%s", want, stderr.String())
		}
	}
}

func TestRunLegacyOperandShapeSuggestsRunFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"run", "verify", "bazel"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exited %d, want 2; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "guardian run bazel --verify") {
		t.Fatalf("stderr omitted run verify suggestion:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("stderr omitted help text:\n%s", stderr.String())
	}
}

func testFlyResult() flyResult {
	return flyResult{
		Name:           "gamma",
		ReadyToFly:     "yes",
		ExecutionMode:  "dry_run",
		ResourceDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		Entrypoint: resourceRefResult{
			APIVersion: "guardian.guardianintelligence.org/v1alpha1",
			Kind:       "FlyProcedure",
			Name:       "gamma",
		},
		Nomad: nomadResult{
			Address:   "http://127.0.0.1:4646",
			Namespace: "default",
			Status:    "ready",
			Reason:    "JobsPlanned",
			Jobs: []nomadJobResult{{
				Name:     "example",
				Path:     "example.nomad.hcl",
				VarsPath: "example.nomad.vars.hcl",
				Type:     "service",
				Status:   "ready",
				Reason:   "AllocationRunning",
			}},
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
	catalog := fmt.Sprintf("tools: bazel: platforms: %q: executable: \"bazel\"\n", runtime.GOOS+"/"+runtime.GOARCH)
	if err := os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(catalog), 0o644); err != nil {
		t.Fatalf("write tool catalog: %v", err)
	}
}

func writeExecutableTestToolCatalog(t *testing.T, dir string) {
	t.Helper()
	writeTestToolCatalog(t, dir)
	toolBin := filepath.Join(dir, ".verself", "tools", "root", "bazel", "bin", "bazel")
	if err := os.MkdirAll(filepath.Dir(toolBin), 0o755); err != nil {
		t.Fatalf("mkdir tool root: %v", err)
	}
	body := []byte("#!/bin/sh\nprintf 'fake bazel: %s\\n' \"$*\"\n")
	if err := os.WriteFile(toolBin, body, 0o755); err != nil {
		t.Fatalf("write tool root bazel: %v", err)
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

func writeTestBuildEvents(t *testing.T, dir string, outputs map[string]string) {
	t.Helper()
	var files []map[string]any
	for targetPath, sourcePath := range outputs {
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("read build output %s: %v", sourcePath, err)
		}
		sum := sha256.Sum256(data)
		name := strings.TrimPrefix(filepath.ToSlash(targetPath), "bazel-bin/")
		files = append(files, map[string]any{
			"name":       name,
			"uri":        "file://" + filepath.ToSlash(sourcePath),
			"pathPrefix": []string{"bazel-out", "k8-fastbuild", "bin"},
			"digest":     hex.EncodeToString(sum[:]),
		})
	}
	namedSet, err := json.Marshal(map[string]any{
		"id": map[string]any{"namedSet": map[string]any{"id": "0"}},
		"namedSetOfFiles": map[string]any{
			"files": files,
		},
	})
	if err != nil {
		t.Fatalf("encode named set: %v", err)
	}
	completed, err := json.Marshal(map[string]any{
		"id": map[string]any{"targetCompleted": map[string]any{"label": "//test:fly_artifacts"}},
		"completed": map[string]any{
			"success": true,
			"outputGroup": []map[string]any{
				{
					"name": "default",
					"fileSets": []map[string]any{
						{"id": "0"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("encode completed event: %v", err)
	}
	body := append(namedSet, '\n')
	body = append(body, completed...)
	body = append(body, '\n')
	path := filepath.Join(dir, ".guardian", "build", "build-events.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir build events dir: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write build events: %v", err)
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
	bazelOut := filepath.Join(dir, "bazel-out", "k8-fastbuild", "bin")
	if err := os.MkdirAll(bazelOut, 0o755); err != nil {
		t.Fatalf("mkdir bazel output dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bazelOut, "guardian"), []byte("built guardian\n"), 0o755); err != nil {
		t.Fatalf("write built artifact: %v", err)
	}
	openBaoRuntime := filepath.Join(bazelOut, "src", "infrastructure-components", "openbao", "openbao-runtime.tar")
	if err := os.MkdirAll(filepath.Dir(openBaoRuntime), 0o755); err != nil {
		t.Fatalf("mkdir openbao runtime dir: %v", err)
	}
	if err := os.WriteFile(openBaoRuntime, []byte("openbao runtime\n"), 0o644); err != nil {
		t.Fatalf("write openbao runtime: %v", err)
	}
	nomadRuntime := filepath.Join(bazelOut, "src", "infrastructure-components", "nomad", "nomad-runtime.tar")
	if err := os.MkdirAll(filepath.Dir(nomadRuntime), 0o755); err != nil {
		t.Fatalf("mkdir nomad runtime dir: %v", err)
	}
	if err := os.WriteFile(nomadRuntime, []byte("nomad runtime\n"), 0o644); err != nil {
		t.Fatalf("write nomad runtime: %v", err)
	}
	// Hermetic ansible-core is a controller-side tar (python/bin interpreter +
	// ansible-playbook + collections); fake it minimally and pack it as a tar the
	// preflight extracts and invokes through the bundled interpreter.
	ansibleStage := filepath.Join(dir, "ansible-stage")
	ansibleBin := filepath.Join(ansibleStage, "python", "bin")
	if err := os.MkdirAll(ansibleBin, 0o755); err != nil {
		t.Fatalf("mkdir ansible bin: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ansibleStage, "collections", "ansible_collections"), 0o755); err != nil {
		t.Fatalf("mkdir ansible collections: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ansibleBin, "python3.13"), []byte("#!/bin/sh\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("write fake python: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ansibleBin, "ansible-playbook"), []byte(`#!/bin/sh
set -eu
found=0
for arg in "$@"; do
  if [ "$arg" = "-i" ]; then
    found=1
  fi
done
test "$found" = 1
printf 'ansible ran\n' > ansible-ran
`), 0o755); err != nil {
		t.Fatalf("write fake ansible-playbook: %v", err)
	}
	ansibleRuntime := filepath.Join(bazelOut, "src", "guardian-specification", "ansible", "runtime", "ansible-runtime.tar")
	if err := os.MkdirAll(filepath.Dir(ansibleRuntime), 0o755); err != nil {
		t.Fatalf("mkdir ansible runtime dir: %v", err)
	}
	if out, err := exec.Command("tar", "-C", ansibleStage, "-cf", ansibleRuntime, ".").CombinedOutput(); err != nil {
		t.Fatalf("tar ansible runtime: %v: %s", err, out)
	}
	podmanRuntime := filepath.Join(bazelOut, "src", "infrastructure-components", "podman", "podman-runtime.tar")
	if err := os.MkdirAll(filepath.Dir(podmanRuntime), 0o755); err != nil {
		t.Fatalf("mkdir podman runtime dir: %v", err)
	}
	if err := os.WriteFile(podmanRuntime, []byte("podman runtime\n"), 0o644); err != nil {
		t.Fatalf("write podman runtime: %v", err)
	}
	rsyncPath := filepath.Join(bazelOut, "src", "guardian-specification", "tools", "rsync")
	if err := os.MkdirAll(filepath.Dir(rsyncPath), 0o755); err != nil {
		t.Fatalf("mkdir rsync dir: %v", err)
	}
	if err := os.WriteFile(rsyncPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake rsync: %v", err)
	}
	writeTestBuildEvents(t, dir, map[string]string{
		openBaoRuntimeTargetPath: openBaoRuntime,
		nomadRuntimeTargetPath:   nomadRuntime,
		podmanRuntimeTargetPath:  podmanRuntime,
		ansibleRuntimeTargetPath: ansibleRuntime,
		rsyncTargetPath:          rsyncPath,
	})
	if err := os.WriteFile(filepath.Join(dir, "preflight.yml"), []byte("---\n- hosts: all\n"), 0o644); err != nil {
		t.Fatalf("write preflight playbook: %v", err)
	}
	sshPath := filepath.Join(dir, "ssh")
	if err := os.WriteFile(sshPath, []byte(`#!/bin/sh
set -eu
remote_command=
for arg in "$@"; do
  remote_command="$arg"
done
test -n "$remote_command"
remote_command="$(printf '%s' "$remote_command" | sed "s#/opt/verself/profile/bin/nomad#$PWD/nomad#g")"
exec sh -c "$remote_command"
`), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	nomadPath := filepath.Join(dir, "nomad")
	if err := os.WriteFile(nomadPath, []byte(`#!/bin/sh
set -eu
if [ "$1" != job ]; then
  echo "unexpected nomad command: $*" >&2
  exit 1
fi
case "$2" in
  plan)
    shift 2
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -no-color) ;;
        -var=repo_root=*) ;;
        -var=artifact_root=*) ;;
        -var=site=gamma) ;;
        -var-file)
          shift
          test -f "$1"
          ;;
        *)
          test -f "$1"
          ;;
      esac
      shift || true
    done
    printf 'Job Modify Index: 7\n'
    exit 0
    ;;
  run)
    test "$3" = "-detach"
    test "$4" = "-check-index"
    test "$5" = "7"
    shift 5
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -var=repo_root=*) ;;
        -var=artifact_root=*) ;;
        -var=site=gamma) ;;
        -var-file)
          shift
          test -f "$1"
          ;;
        *)
          test -f "$1"
          ;;
      esac
      shift || true
    done
    exit 0
    ;;
  allocs)
    printf '[{"CreateIndex":1,"ClientStatus":"running","DesiredStatus":"run","ID":"alloc-test","JobType":"service"}]\n'
    exit 0
    ;;
esac
echo "unexpected nomad job command: $*" >&2
exit 1
`), 0o755); err != nil {
		t.Fatalf("write fake nomad: %v", err)
	}
	jobDir := filepath.Join(dir, "jobs")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatalf("mkdir jobs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "example.nomad.hcl"), []byte(`job "example" {
  datacenters = ["*"]
  type = "service"
}
`), 0o644); err != nil {
		t.Fatalf("write fake job: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "example.nomad.vars.hcl"), []byte(`example_sha256 = "1111111111111111111111111111111111111111111111111111111111111111"
`), 0o644); err != nil {
		t.Fatalf("write fake vars: %v", err)
	}
	path := filepath.Join(dir, "gamma.cue")
	repoRoot := filepath.Join(dir, "remote-repo")
	remoteJobPath := filepath.Join(repoRoot, "workspace", "jobs", "example.nomad.hcl")
	if err := os.MkdirAll(filepath.Dir(remoteJobPath), 0o755); err != nil {
		t.Fatalf("mkdir remote job dir: %v", err)
	}
	if err := os.WriteFile(remoteJobPath, []byte("job \"example\" {}\n"), 0o644); err != nil {
		t.Fatalf("write remote fake job: %v", err)
	}
	remoteVarsPath := filepath.Join(repoRoot, "workspace", "jobs", "example.nomad.vars.hcl")
	if err := os.WriteFile(remoteVarsPath, []byte("example_sha256 = \"1111111111111111111111111111111111111111111111111111111111111111\"\n"), 0o644); err != nil {
		t.Fatalf("write remote fake vars: %v", err)
	}
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
			preflight: ansible: playbook: "preflight.yml"
			nomad: {
				address: "http://127.0.0.1:4646"
				namespace: "default"
				jobs: [
					{
						name: "example"
						path: "jobs/example.nomad.hcl"
						varsPath: "jobs/example.nomad.vars.hcl"
					},
				]
			}
		}
	},
	{
		apiVersion: "substrate.guardianintelligence.org/v1alpha1"
		kind:       "Substrate"
		metadata: name: "local"
		spec: {
				remote: {
					repoRoot: %q
					ssh: [%q, "test-target"]
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
`, repoRoot, sshPath)
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
