package toolrun

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/verself/guardian-specification/internal/toolcatalog"
)

func writeTool(t *testing.T, root string, name string, exe string, body string) string {
	t.Helper()
	dir := filepath.Join(root, name, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir tool dir: %v", err)
	}
	path := filepath.Join(dir, exe)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}
	return path
}

func TestLocateResolvesFromImageRoot(t *testing.T) {
	root := t.TempDir()
	want := writeTool(t, root, "fake", "fake", "#!/bin/sh\nexit 0\n")
	t.Setenv(ToolsRootEnv, root)
	resolution, err := Locate(toolcatalog.ResolvedTool{Name: "fake", Executable: "fake"}, Options{})
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if resolution.Executable != want {
		t.Fatalf("Executable = %q, want %q", resolution.Executable, want)
	}
	if resolution.Source != "image" {
		t.Fatalf("Source = %q, want image", resolution.Source)
	}
}

func TestLocateResolvesFromWorkspaceRoot(t *testing.T) {
	ws := t.TempDir()
	t.Setenv(ToolsRootEnv, "")
	want := writeTool(t, filepath.Join(ws, workspaceToolsRootRel), "fake", "fake", "#!/bin/sh\nexit 0\n")
	resolution, err := Locate(toolcatalog.ResolvedTool{Name: "fake", Executable: "fake"}, Options{WorkspaceRoot: ws})
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if resolution.Executable != want {
		t.Fatalf("Executable = %q, want %q", resolution.Executable, want)
	}
	if resolution.Source != "workspace" {
		t.Fatalf("Source = %q, want workspace", resolution.Source)
	}
}

func TestLocateFailsLoudOnMissingTool(t *testing.T) {
	t.Setenv(ToolsRootEnv, t.TempDir())
	_, err := Locate(toolcatalog.ResolvedTool{Name: "absent", Executable: "absent"}, Options{})
	if err == nil {
		t.Fatal("Locate succeeded, want absent-tool failure")
	}
}

func TestExecRunsResolvedTool(t *testing.T) {
	root := t.TempDir()
	writeTool(t, root, "echoer", "echoer", "#!/bin/sh\nprintf '%s' \"$1\"\n")
	t.Setenv(ToolsRootEnv, root)
	var stdout bytes.Buffer
	code := Exec(context.Background(), toolcatalog.ResolvedTool{Name: "echoer", Executable: "echoer"}, []string{"hello"}, Options{Stdout: &stdout, Stderr: &stdout})
	if code != 0 {
		t.Fatalf("Exec exit code = %d, want 0 (output: %q)", code, stdout.String())
	}
	if stdout.String() != "hello" {
		t.Fatalf("Exec stdout = %q, want hello", stdout.String())
	}
}
