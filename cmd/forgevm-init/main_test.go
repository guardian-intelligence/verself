package main

import (
	"strings"
	"testing"
)

func TestBuildRuntimeEnvUsesExplicitRegistry(t *testing.T) {
	t.Parallel()

	env, err := buildRuntimeEnv(map[string]string{
		"FORGE_METAL_NPM_REGISTRY": "http://10.0.0.1:4873",
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

func TestNormalizeWorkDirFallsBackToWorkspace(t *testing.T) {
	t.Parallel()

	if got := normalizeWorkDir("   "); got != defaultWorkDir {
		t.Fatalf("normalizeWorkDir blank: got %q want %q", got, defaultWorkDir)
	}
	if got := normalizeWorkDir("/workspace/apps/web"); got != "/workspace/apps/web" {
		t.Fatalf("normalizeWorkDir explicit: got %q", got)
	}
}
