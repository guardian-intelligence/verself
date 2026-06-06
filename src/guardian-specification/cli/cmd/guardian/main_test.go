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
	dir, _ := writeTestCUEDocument(t)
	var stderr bytes.Buffer
	var opts commandOptions
	var ok bool
	withWorkingDir(t, dir, func() {
		opts, ok = parseCommonFlags("guardian board", []string{"gamma.cue", "-o", "json", "--dry-run"}, &stderr)
	})
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
	withWorkingDir(t, dir, func() {
		code := run([]string{"board", path, "--render", "-o", "json"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("run exited %d, want 2; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
		}
	})
	if !strings.Contains(stderr.String(), "flag provided but not defined: -render") {
		t.Fatalf("stderr omitted unknown render flag:\n%s", stderr.String())
	}
}

func TestBoardCUEDocument(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"board", path, "--dry-run", "-o", "json"}, &stdout, &stderr)
	})
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
	if result.Entrypoint.Name != "gamma" {
		t.Fatalf("entrypoint = %#v, want gamma FlyProcedure", result.Entrypoint)
	}
}

func TestBoardHooksVerifyDigest(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"board", path, "-o", "json"}, &stdout, &stderr)
	})
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
	if result.Upload.Run.Status != "ready" || result.Upload.Extract.Status != "ready" || result.Upload.Verify.Status != "ready" {
		t.Fatalf("hooks not ready: run=%#v extract=%#v verify=%#v", result.Upload.Run, result.Upload.Extract, result.Upload.Verify)
	}
	if result.Access.Status != "ready" {
		t.Fatalf("access hook not ready: %#v", result.Access)
	}
}

func TestFlyDryRunCUEDocument(t *testing.T) {
	dir, path := writeTestCUEDocument(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var code int
	withWorkingDir(t, dir, func() {
		code = run([]string{"fly", path, "--dry-run", "-o", "json"}, &stdout, &stderr)
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
		code = run([]string{"fly", path, "-o", "json"}, &stdout, &stderr)
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
		if cond.Type == "BoardingReady" && cond.Status == "True" && cond.Reason == "ReadyToFly" {
			found = true
		}
	}
	if !found {
		t.Fatalf("BoardingReady true condition not found: %#v", result.Conditions)
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
	writeRequiredArtifacts(t, dir)
	path := filepath.Join(dir, "gamma.cue")
	if err := os.WriteFile(path, []byte(`package gamma

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
		}
	},
	{
		apiVersion: "substrate.guardianintelligence.org/v1alpha1"
		kind:       "Substrate"
		metadata: name: "local"
		spec: {
				access: argv: ["sh", "-c", "test -f MODULE.bazel"]
				upload: {
					bundlePath: ".guardian/board/upload.tar.gz"
					manifestPath: ".guardian/board/upload-manifest.json"
					digestPath: ".guardian/board/upload.sha256"
					run: argv: ["sh", "-c", "cp .guardian/board/upload.tar.gz upload.tar.gz"]
					extract: argv: ["sh", "-c", "rm -rf extracted && mkdir extracted && tar -xzf upload.tar.gz -C extracted"]
					verify: argv: ["sh", "-c", "cd extracted && sha256sum -c guardian-upload-sha256sums.txt >/dev/null && sha256sum ../upload.tar.gz"]
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
`), 0o644); err != nil {
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

func writeRequiredArtifacts(t *testing.T, dir string) {
	t.Helper()
	artifacts := map[string][]byte{
		"bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian":                                                []byte("guardian binary\n"),
		"bazel-bin/src/infrastructure-components/nomad/nomad-runtime.tar":                                                         []byte("nomad runtime\n"),
		"bazel-bin/src/infrastructure-components/nomad/cmd/nomad-recover/nomad-recover_/nomad-recover":                            []byte("nomad recover\n"),
		"bazel-bin/src/infrastructure-components/openbao/openbao-runtime.tar":                                                     []byte("openbao runtime\n"),
		"bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover":                    []byte("openbao recover\n"),
		"bazel-bin/src/infrastructure-components/haproxy/haproxy-runtime.tar":                                                     []byte("haproxy runtime\n"),
		"bazel-bin/src/infrastructure-components/nftables/nftables-runtime.tar":                                                   []byte("nftables runtime\n"),
		"bazel-bin/src/infrastructure-components/nftables/cmd/nftables-apply/nftables-apply_/nftables-apply":                      []byte("nftables apply\n"),
		"bazel-bin/src/infrastructure-components/nats/nats-runtime.tar":                                                           []byte("nats runtime\n"),
		"bazel-bin/src/infrastructure-components/nats/cmd/nats-recover/nats-recover_/nats-recover":                                []byte("nats recover\n"),
		"bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer.tar":                            []byte("nomad observer runtime\n"),
		"bazel-bin/src/infrastructure-components/nomad-observer/cmd/nomad-observer/nomad-observer_/nomad-observer":                []byte("nomad observer binary\n"),
		"bazel-bin/src/infrastructure-components/otelcol/otelcol-runtime.tar":                                                      []byte("otelcol runtime\n"),
		"bazel-bin/src/infrastructure-components/otelcol/otelcol-config.tar":                                                       []byte("otelcol config\n"),
		"bazel-bin/src/infrastructure-components/otelcol/cmd/otelcol-recover/otelcol-recover_/otelcol-recover":                    []byte("otelcol recover\n"),
		"bazel-bin/src/infrastructure-components/postgresql/postgresql_runtime.tar":                                               []byte("postgresql runtime\n"),
		"bazel-bin/src/infrastructure-components/clickhouse/clickhouse-runtime.tar":                                               []byte("clickhouse runtime\n"),
		"bazel-bin/src/infrastructure-components/clickhouse/cmd/clickhouse-recover/clickhouse-recover_/clickhouse-recover":        []byte("clickhouse recover\n"),
		"bazel-bin/src/integrations/cloudflare/control-plane/cloudflare-control-plane-runtime.tar":                                []byte("cloudflare runtime\n"),
		"bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service.tar":                     []byte("object storage runtime\n"),
		"bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service_/object-storage-service": []byte("object storage binary\n"),
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
