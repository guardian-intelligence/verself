package board

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

type boardResult struct {
	ReadyToFly string `json:"ready_to_fly"`
	Upload     struct {
		Digest            string `json:"digest"`
		ObservedDigest    string `json:"observed_digest"`
		CompressedBytes   int64  `json:"compressed_bytes"`
		UncompressedBytes int64  `json:"uncompressed_bytes"`
	} `json:"upload"`
}

func BenchmarkGuardianBoard(b *testing.B) {
	crd := os.Getenv("GUARDIAN_BOARD_BENCH_CRD")
	if crd == "" {
		b.Skip("GUARDIAN_BOARD_BENCH_CRD is required")
	}
	guardian := os.Getenv("GUARDIAN_BOARD_BENCH_GUARDIAN")
	if guardian == "" {
		guardian = "guardian"
	}
	repoRoot := os.Getenv("GUARDIAN_BOARD_BENCH_REPO_ROOT")
	if repoRoot == "" {
		repoRoot = "."
	}
	warm := runBoard(b, guardian, crd, repoRoot)
	if warm.ReadyToFly != "yes" {
		b.Fatalf("warmup ready_to_fly = %q, want yes", warm.ReadyToFly)
	}
	if warm.Upload.Digest == "" || warm.Upload.Digest != warm.Upload.ObservedDigest {
		b.Fatalf("warmup upload digest mismatch: expected=%s observed=%s", warm.Upload.Digest, warm.Upload.ObservedDigest)
	}
	b.ReportMetric(float64(warm.Upload.CompressedBytes), "compressed_bytes")
	b.ReportMetric(float64(warm.Upload.UncompressedBytes), "uncompressed_bytes")
	b.SetBytes(warm.Upload.CompressedBytes)
	b.ReportAllocs()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := runBoard(b, guardian, crd, repoRoot)
		if result.ReadyToFly != "yes" {
			b.Fatalf("ready_to_fly = %q, want yes", result.ReadyToFly)
		}
		if result.Upload.Digest != result.Upload.ObservedDigest {
			b.Fatalf("upload digest mismatch: expected=%s observed=%s", result.Upload.Digest, result.Upload.ObservedDigest)
		}
	}
}

func runBoard(tb testing.TB, guardian string, crd string, repoRoot string) boardResult {
	tb.Helper()
	cmd := exec.Command(guardian, "board", crd, "--repo-root", repoRoot, "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		tb.Fatalf("%s: %v\n%s", commandString(guardian, crd, repoRoot), err, output)
	}
	var result boardResult
	if err := json.Unmarshal(output, &result); err != nil {
		tb.Fatalf("decode guardian board output: %v\n%s", err, output)
	}
	return result
}

func commandString(guardian string, crd string, repoRoot string) string {
	return fmt.Sprintf("%s board %s --repo-root %s -o json", guardian, crd, repoRoot)
}
