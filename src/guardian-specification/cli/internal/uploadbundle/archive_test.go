package uploadbundle

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestWriteTarZstdDeterministic(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []File{
		{Source: b, Target: "workspace/b.txt", Mode: "0644"},
		{Source: a, Target: "workspace/a.txt", Mode: "0644"},
	}
	var one bytes.Buffer
	first, err := WriteTarZstd(&one, files)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	var two bytes.Buffer
	second, err := WriteTarZstd(&two, files)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if first.ArchiveSHA256 != second.ArchiveSHA256 {
		t.Fatalf("digest changed: %s != %s", first.ArchiveSHA256, second.ArchiveSHA256)
	}
	if !bytes.Equal(one.Bytes(), two.Bytes()) {
		t.Fatalf("archive bytes changed for identical input")
	}
	if first.CompressedBytes == 0 || first.UncompressedBytes == 0 {
		t.Fatalf("manifest did not record byte counts: %#v", first)
	}
}

func TestWriteTarZstdRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(source, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, err := WriteTarZstd(&out, []File{{Source: source, Target: "../a.txt", Mode: "0644"}})
	if err == nil {
		t.Fatalf("WriteTarZstd accepted path traversal")
	}
}

func TestWorkspaceFilesRequiresBuildArtifacts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := WorkspaceFiles(dir)
	if err == nil {
		t.Fatalf("WorkspaceFiles succeeded without required build artifacts")
	}
}

func TestWorkspaceFilesIncludesUntrackedGitFiles(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRequiredTestArtifacts(t, dir)

	files, err := WorkspaceFiles(dir)
	if err != nil {
		t.Fatalf("WorkspaceFiles: %v", err)
	}
	targets := make([]string, 0, len(files))
	for _, file := range files {
		targets = append(targets, file.Target)
	}
	if !slices.Contains(targets, "workspace/tracked.txt") {
		t.Fatalf("workspace bundle omitted tracked file: %#v", targets)
	}
	if !slices.Contains(targets, "workspace/untracked.txt") {
		t.Fatalf("workspace bundle omitted untracked file: %#v", targets)
	}
}

func writeRequiredTestArtifacts(t *testing.T, dir string) {
	t.Helper()
	for _, artifact := range RequiredBuildArtifacts {
		path := filepath.Join(dir, filepath.FromSlash(artifact.Source))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir artifact parent: %v", err)
		}
		if err := os.WriteFile(path, []byte(artifact.Source+"\n"), 0o755); err != nil {
			t.Fatalf("write artifact %s: %v", artifact.Source, err)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
