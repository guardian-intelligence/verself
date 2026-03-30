package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forge-metal/forge-metal/internal/config"
)

func executeRoot(t *testing.T, paths config.Paths, args ...string) (string, error) {
	t.Helper()

	cmd := newRootCmd(paths)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.Execute()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += stderr.String()
	}

	return output, err
}

func writeConfigFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestConfigSetAndGetLocal(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		System: filepath.Join(dir, "etc", "forge-metal", "config.toml"),
		Global: filepath.Join(dir, "home", ".config", "forge-metal", "config.toml"),
		Local:  filepath.Join(dir, "repo", "forge-metal.toml"),
	}

	if _, err := executeRoot(t, paths, "config", "set", "latitude.region", "LAX", "--local"); err != nil {
		t.Fatalf("config set failed: %v", err)
	}

	got, err := executeRoot(t, paths, "config", "get", "latitude.region", "--local")
	if err != nil {
		t.Fatalf("config get failed: %v", err)
	}
	if got != "LAX\n" {
		t.Fatalf("expected LAX, got %q", got)
	}

	data, err := os.ReadFile(paths.Local)
	if err != nil {
		t.Fatalf("read local config: %v", err)
	}
	if !strings.Contains(string(data), "region = 'LAX'") && !strings.Contains(string(data), "region = \"LAX\"") {
		t.Fatalf("expected local config to contain latitude.region, got:\n%s", data)
	}
}

func TestConfigGetShowOriginUsesHighestPrecedence(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		System: filepath.Join(dir, "etc", "forge-metal", "config.toml"),
		Global: filepath.Join(dir, "home", ".config", "forge-metal", "config.toml"),
		Local:  filepath.Join(dir, "repo", "forge-metal.toml"),
	}

	writeConfigFile(t, paths.System, "[latitude]\nregion = \"DAL\"\n")
	writeConfigFile(t, paths.Global, "[latitude]\nregion = \"LAX\"\n")
	writeConfigFile(t, paths.Local, "[latitude]\nregion = \"ASH\"\n")

	got, err := executeRoot(t, paths, "config", "get", "latitude.region", "--show-origin")
	if err != nil {
		t.Fatalf("config get failed: %v", err)
	}

	want := "file:" + paths.Local + "\tASH\n"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestConfigUnsetRevealsLowerPrecedenceValue(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		System: filepath.Join(dir, "etc", "forge-metal", "config.toml"),
		Global: filepath.Join(dir, "home", ".config", "forge-metal", "config.toml"),
		Local:  filepath.Join(dir, "repo", "forge-metal.toml"),
	}

	writeConfigFile(t, paths.Global, "[latitude]\nregion = \"LAX\"\n")
	writeConfigFile(t, paths.Local, "[latitude]\nregion = \"ASH\"\n")

	if _, err := executeRoot(t, paths, "config", "unset", "latitude.region", "--local"); err != nil {
		t.Fatalf("config unset failed: %v", err)
	}

	got, err := executeRoot(t, paths, "config", "get", "latitude.region", "--show-origin")
	if err != nil {
		t.Fatalf("config get failed: %v", err)
	}

	want := "file:" + paths.Global + "\tLAX\n"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestConfigSetRejectsSecretKeys(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		System: filepath.Join(dir, "etc", "forge-metal", "config.toml"),
		Global: filepath.Join(dir, "home", ".config", "forge-metal", "config.toml"),
		Local:  filepath.Join(dir, "repo", "forge-metal.toml"),
	}

	_, err := executeRoot(t, paths, "config", "set", "latitude.auth_token", "secret", "--local")
	if err == nil {
		t.Fatal("expected config set to reject secret keys")
	}
	if !strings.Contains(err.Error(), "LATITUDESH_AUTH_TOKEN") {
		t.Fatalf("expected env guidance, got %v", err)
	}
}

func TestConfigListRedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		System: filepath.Join(dir, "etc", "forge-metal", "config.toml"),
		Global: filepath.Join(dir, "home", ".config", "forge-metal", "config.toml"),
		Local:  filepath.Join(dir, "repo", "forge-metal.toml"),
	}

	writeConfigFile(t, paths.Local, "[latitude]\nauth_token = \"secret-token\"\nregion = \"ASH\"\n")

	got, err := executeRoot(t, paths, "config", "list")
	if err != nil {
		t.Fatalf("config list failed: %v", err)
	}
	if !strings.Contains(got, "latitude.region=ASH") {
		t.Fatalf("expected latitude.region in list, got:\n%s", got)
	}
	if !strings.Contains(got, "latitude.auth_token=***") {
		t.Fatalf("expected redacted token in list, got:\n%s", got)
	}
	if strings.Contains(got, "secret-token") {
		t.Fatalf("did not expect raw token in list, got:\n%s", got)
	}
}

func TestConfigPathDefaultsToLocal(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		System: filepath.Join(dir, "etc", "forge-metal", "config.toml"),
		Global: filepath.Join(dir, "home", ".config", "forge-metal", "config.toml"),
		Local:  filepath.Join(dir, "repo", "forge-metal.toml"),
	}

	got, err := executeRoot(t, paths, "config", "path")
	if err != nil {
		t.Fatalf("config path failed: %v", err)
	}
	if got != paths.Local+"\n" {
		t.Fatalf("expected %q, got %q", paths.Local+"\n", got)
	}
}

func TestConfigPathGlobal(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		System: filepath.Join(dir, "etc", "forge-metal", "config.toml"),
		Global: filepath.Join(dir, "home", ".config", "forge-metal", "config.toml"),
		Local:  filepath.Join(dir, "repo", "forge-metal.toml"),
	}

	got, err := executeRoot(t, paths, "config", "path", "--global")
	if err != nil {
		t.Fatalf("config path failed: %v", err)
	}
	if got != paths.Global+"\n" {
		t.Fatalf("expected %q, got %q", paths.Global+"\n", got)
	}
}

func TestConfigEditUsesEditorAndCreatesFile(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{
		System: filepath.Join(dir, "etc", "forge-metal", "config.toml"),
		Global: filepath.Join(dir, "home", ".config", "forge-metal", "config.toml"),
		Local:  filepath.Join(dir, "repo", "forge-metal.toml"),
	}

	editorLog := filepath.Join(dir, "editor.log")
	editor := filepath.Join(dir, "fake-editor.sh")
	writeConfigFile(t, editor, "#!/usr/bin/env bash\nprintf '%s\\n' \"$1\" >> "+editorLog+"\n")
	if err := os.Chmod(editor, 0755); err != nil {
		t.Fatalf("chmod editor: %v", err)
	}

	oldEditor := os.Getenv("EDITOR")
	if err := os.Setenv("EDITOR", editor); err != nil {
		t.Fatalf("set EDITOR: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Setenv("EDITOR", oldEditor); err != nil {
			t.Fatalf("restore EDITOR: %v", err)
		}
	})

	if _, err := executeRoot(t, paths, "config", "edit"); err != nil {
		t.Fatalf("config edit failed: %v", err)
	}

	data, err := os.ReadFile(editorLog)
	if err != nil {
		t.Fatalf("read editor log: %v", err)
	}
	if string(data) != paths.Local+"\n" {
		t.Fatalf("expected editor to receive %q, got %q", paths.Local+"\n", string(data))
	}
	if _, err := os.Stat(paths.Local); err != nil {
		t.Fatalf("expected local config file to be created: %v", err)
	}
}
