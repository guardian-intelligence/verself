package board

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type boardResult struct {
	ReadyToFly string `json:"ready_to_fly"`
	Upload     struct {
		Digest string `json:"digest"`
	} `json:"upload"`
}

func BenchmarkGuardianBoard(b *testing.B) {
	crd := "src/guardian-specification/examples/gamma/gamma.cue"
	guardian := "guardian"
	warm := runBoard(b, guardian, crd)
	if warm.ReadyToFly != "yes" {
		b.Fatalf("warmup ready_to_fly = %q, want yes", warm.ReadyToFly)
	}
	if warm.Upload.Digest == "" {
		b.Fatal("warmup upload digest was empty")
	}
	b.ReportAllocs()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := runBoard(b, guardian, crd)
		if result.ReadyToFly != "yes" {
			b.Fatalf("ready_to_fly = %q, want yes", result.ReadyToFly)
		}
		if result.Upload.Digest == "" {
			b.Fatal("upload digest was empty")
		}
	}
}

func runBoard(tb testing.TB, guardian string, crd string) boardResult {
	tb.Helper()
	workspaceRoot, err := discoverWorkspaceRoot()
	if err != nil {
		tb.Fatal(err)
	}
	cmd := exec.Command(guardian, "board", crd, "-o", "json")
	cmd.Dir = workspaceRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		tb.Fatalf("%s: %v\n%s", commandString(guardian, crd), err, output)
	}
	var result boardResult
	if err := json.Unmarshal(output, &result); err != nil {
		tb.Fatalf("decode guardian board output: %v\n%s", err, output)
	}
	return result
}

func commandString(guardian string, crd string) string {
	return fmt.Sprintf("%s board %s -o json", guardian, crd)
}

func discoverWorkspaceRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if stat, err := os.Stat(filepath.Join(dir, "MODULE.bazel")); err == nil && stat.Mode().IsRegular() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a Bazel workspace")
		}
		dir = parent
	}
}
