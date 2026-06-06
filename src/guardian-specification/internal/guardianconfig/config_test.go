package guardianconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProfileDefaultDocumentPath(t *testing.T) {
	dir := testWorkspace(t)
	writeFile(t, filepath.Join(dir, ".config", "guardian", "guardian.cue"), `defaultProfile: "gamma"
profiles: gamma: document: "../../src/guardian-specification/examples/gamma/gamma.cue"
`)
	writeFile(t, filepath.Join(dir, "src", "guardian-specification", "examples", "gamma", "gamma.cue"), `package gamma
`)

	resolution, err := ResolveProfile(ResolveOptions{CWD: dir})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if resolution.ProfileName != "gamma" {
		t.Fatalf("ProfileName = %q, want gamma", resolution.ProfileName)
	}
	want := filepath.Join(dir, "src", "guardian-specification", "examples", "gamma", "gamma.cue")
	if resolution.DocumentPath != want {
		t.Fatalf("DocumentPath = %q, want %q", resolution.DocumentPath, want)
	}
}

func TestResolveProfileExplicitConfigFileSelectsProfile(t *testing.T) {
	dir := testWorkspace(t)
	configPath := filepath.Join(dir, ".config", "guardian", "guardian.cue")
	writeFile(t, configPath, `defaultProfile: "gamma"
profiles: {
	gamma: document: "../../gamma.cue"
	prod: document: "../../prod.cue"
}
`)
	writeFile(t, filepath.Join(dir, "gamma.cue"), `package gamma
`)
	writeFile(t, filepath.Join(dir, "prod.cue"), `package prod
`)

	resolution, err := ResolveProfile(ResolveOptions{CWD: dir, ConfigPath: configPath, Profile: "prod"})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if resolution.ProfileName != "prod" {
		t.Fatalf("ProfileName = %q, want prod", resolution.ProfileName)
	}
	if resolution.RootConfigPath != configPath {
		t.Fatalf("RootConfigPath = %q, want %q", resolution.RootConfigPath, configPath)
	}
	want := filepath.Join(dir, "prod.cue")
	if resolution.DocumentPath != want {
		t.Fatalf("DocumentPath = %q, want %q", resolution.DocumentPath, want)
	}
}

func TestResolveProfileExplicitConfigFileUsesDefaultProfile(t *testing.T) {
	dir := testWorkspace(t)
	configPath := filepath.Join(dir, ".config", "guardian", "guardian.cue")
	writeFile(t, configPath, `defaultProfile: "gamma"
profiles: gamma: document: "../../gamma.cue"
`)
	writeFile(t, filepath.Join(dir, "gamma.cue"), `package gamma
`)

	resolution, err := ResolveProfile(ResolveOptions{CWD: dir, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if resolution.ProfileName != "gamma" {
		t.Fatalf("ProfileName = %q, want gamma", resolution.ProfileName)
	}
	want := filepath.Join(dir, "gamma.cue")
	if resolution.DocumentPath != want {
		t.Fatalf("DocumentPath = %q, want %q", resolution.DocumentPath, want)
	}
}

func TestResolveProfileExplicitStandaloneDocument(t *testing.T) {
	dir := testWorkspace(t)
	documentPath := filepath.Join(dir, "src", "guardian-specification", "examples", "gamma", "gamma.cue")
	writeFile(t, documentPath, `package gamma
entrypoint: {}
resources: []
`)

	resolution, err := ResolveProfile(ResolveOptions{CWD: dir, ConfigPath: documentPath})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if resolution.ProfileName != "gamma" {
		t.Fatalf("ProfileName = %q, want gamma", resolution.ProfileName)
	}
	if resolution.RootConfigPath != "" {
		t.Fatalf("RootConfigPath = %q, want empty for standalone document", resolution.RootConfigPath)
	}
	if resolution.DocumentPath != documentPath {
		t.Fatalf("DocumentPath = %q, want %q", resolution.DocumentPath, documentPath)
	}
}

func TestResolveProfileExplicitStandaloneDocumentRejectsProfileOperand(t *testing.T) {
	dir := testWorkspace(t)
	documentPath := filepath.Join(dir, "gamma.cue")
	writeFile(t, documentPath, `package gamma
entrypoint: {}
resources: []
`)

	_, err := ResolveProfile(ResolveOptions{CWD: dir, ConfigPath: documentPath, Profile: "prod"})
	if err == nil {
		t.Fatal("ResolveProfile succeeded, want standalone file plus profile error")
	}
	if !strings.Contains(err.Error(), `profile "prod" cannot be used with standalone config file gamma.cue`) {
		t.Fatalf("error = %q, want standalone profile rejection", err)
	}
}

func TestResolveProfileRejectsNestedRootConfig(t *testing.T) {
	dir := testWorkspace(t)
	configRoot := filepath.Join(dir, ".config", "guardian")
	writeFile(t, filepath.Join(configRoot, "guardian.cue"), `defaultProfile: "gamma"
profiles: gamma: document: "profiles/gamma.cue"
`)
	writeFile(t, filepath.Join(configRoot, "profiles", "gamma.cue"), `defaultProfile: "prod"
profiles: prod: document: "../../prod.cue"
`)

	_, err := ResolveProfile(ResolveOptions{CWD: dir, Profile: "gamma"})
	if err == nil {
		t.Fatal("ResolveProfile succeeded, want nested root config error")
	}
	if !strings.Contains(err.Error(), `profile "gamma" resolved to .config/guardian/profiles/gamma.cue, which is a Guardian root config`) {
		t.Fatalf("error = %q, want nested root config rejection", err)
	}
}

func TestResolveProfileSearchErrorListsPaths(t *testing.T) {
	dir := testWorkspace(t)
	writeFile(t, filepath.Join(dir, ".config", "guardian", "guardian.cue"), `defaultProfile: "gamma"
`)

	resolution, err := ResolveProfile(ResolveOptions{CWD: dir, Profile: "prod"})
	if err == nil {
		t.Fatal("ResolveProfile succeeded, want missing profile error")
	}
	for _, want := range []string{
		`guardian: profile "prod" not found`,
		`.config/guardian/profiles/prod.cue`,
		`.config/guardian/profiles/prod.yaml`,
		`.config/guardian/profiles/prod.toml`,
		`.config/guardian/profiles/prod/guardian.cue`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error omitted %q:\n%s", want, err.Error())
		}
	}
	if len(resolution.Searched) != 4 {
		t.Fatalf("searched count = %d, want 4", len(resolution.Searched))
	}
}

func TestListProfilesIncludesRootAndFiles(t *testing.T) {
	dir := testWorkspace(t)
	configRoot := filepath.Join(dir, ".config", "guardian")
	writeFile(t, filepath.Join(configRoot, "guardian.cue"), `defaultProfile: "gamma"
profiles: prod: document: "profiles/prod.cue"
`)
	writeFile(t, filepath.Join(configRoot, "profiles", "gamma.cue"), `entrypoint: {}
`)
	resolution, err := ResolveProfile(ResolveOptions{CWD: dir, Profile: "gamma"})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	got := strings.Join(ListProfiles(resolution), ",")
	if got != "gamma,prod" {
		t.Fatalf("profiles = %q, want gamma,prod", got)
	}
}

func testWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "MODULE.bazel"), `module(name = "guardian_config_test")
`)
	return dir
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
