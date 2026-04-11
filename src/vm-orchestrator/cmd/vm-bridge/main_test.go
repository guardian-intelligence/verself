package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

func TestBuildRuntimeEnvUsesExplicitRegistry(t *testing.T) {
	t.Parallel()

	env, err := buildRuntimeEnv(map[string]string{
		"FORGE_METAL_NPM_REGISTRY": "http://10.0.0.1:4873",
	}, vmproto.NetworkConfig{})
	if err != nil {
		t.Fatalf("buildRuntimeEnv: %v", err)
	}

	values := map[string]string{}
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			t.Fatalf("malformed env entry: %q", entry)
		}
		values[key] = value
	}

	if values["FORGE_METAL_NPM_REGISTRY"] != "http://10.0.0.1:4873" {
		t.Fatalf("FORGE_METAL_NPM_REGISTRY: got %q", values["FORGE_METAL_NPM_REGISTRY"])
	}
	if values["NPM_CONFIG_REGISTRY"] != "http://10.0.0.1:4873" {
		t.Fatalf("NPM_CONFIG_REGISTRY: got %q", values["NPM_CONFIG_REGISTRY"])
	}
	if values["npm_config_registry"] != "http://10.0.0.1:4873" {
		t.Fatalf("npm_config_registry: got %q", values["npm_config_registry"])
	}
	if values["BUN_CONFIG_REGISTRY"] != "http://10.0.0.1:4873" {
		t.Fatalf("BUN_CONFIG_REGISTRY: got %q", values["BUN_CONFIG_REGISTRY"])
	}
}

func TestBuildRuntimeEnvUsesHostServicePlane(t *testing.T) {
	t.Parallel()

	env, err := buildRuntimeEnv(nil, vmproto.NetworkConfig{
		HostServiceIP:   "10.255.0.1",
		HostServicePort: 18080,
	})
	if err != nil {
		t.Fatalf("buildRuntimeEnv: %v", err)
	}

	values := map[string]string{}
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			t.Fatalf("malformed env entry: %q", entry)
		}
		values[key] = value
	}

	if values["FORGE_METAL_HOST_SERVICE_IP"] != "10.255.0.1" {
		t.Fatalf("FORGE_METAL_HOST_SERVICE_IP: got %q", values["FORGE_METAL_HOST_SERVICE_IP"])
	}
	if values["FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN"] != "http://10.255.0.1:18080" {
		t.Fatalf("FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN: got %q", values["FORGE_METAL_HOST_SERVICE_HTTP_ORIGIN"])
	}
	if values["NPM_CONFIG_REGISTRY"] != "http://10.255.0.1:4873" {
		t.Fatalf("NPM_CONFIG_REGISTRY: got %q", values["NPM_CONFIG_REGISTRY"])
	}
	if values["FORGE_METAL_VM_BRIDGE_SOCKET"] != bridgeSocketPath {
		t.Fatalf("FORGE_METAL_VM_BRIDGE_SOCKET: got %q", values["FORGE_METAL_VM_BRIDGE_SOCKET"])
	}
}

func TestBuildRuntimeEnvDoesNotForceCIOrRegistry(t *testing.T) {
	t.Parallel()

	env, err := buildRuntimeEnv(nil, vmproto.NetworkConfig{})
	if err != nil {
		t.Fatalf("buildRuntimeEnv: %v", err)
	}

	values := map[string]string{}
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			t.Fatalf("malformed env entry: %q", entry)
		}
		values[key] = value
	}

	if _, ok := values["CI"]; ok {
		t.Fatalf("CI should be explicitly supplied by the caller, got %q", values["CI"])
	}
	if _, ok := values["NPM_CONFIG_REGISTRY"]; ok {
		t.Fatalf("NPM_CONFIG_REGISTRY should not be injected without an explicit registry or host-service plane, got %q", values["NPM_CONFIG_REGISTRY"])
	}
}

func TestNormalizeWorkDirFallsBackToWorkspace(t *testing.T) {
	t.Parallel()

	if got := normalizeWorkDir("   "); got != defaultWorkDir {
		t.Fatalf("normalizeWorkDir blank: got %q want %q", got, defaultWorkDir)
	}
	if got := normalizeWorkDir("/workspace/apps/web"); got != "/workspace/apps/web" {
		t.Fatalf("normalizeWorkDir explicit: got %q", got)
	}
}

func TestRunCLIHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if err := runCLI([]string{"--help"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runCLI help: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "vm-bridge snapshot save <ref>") {
		t.Fatalf("help output: %q", got)
	}
}

func TestRunCLIRejectsInvalidSnapshotRefBeforeDial(t *testing.T) {
	t.Parallel()

	err := runCLI([]string{"snapshot", "save", "../host"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected invalid ref error")
	}
	if strings.Contains(err.Error(), "connect vm-bridge") {
		t.Fatalf("expected validation before local socket dial, got %v", err)
	}
}
