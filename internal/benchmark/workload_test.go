package benchmark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWorkloads(t *testing.T) {
	// Find config/workloads.toml relative to repo root.
	path := filepath.Join("..", "..", "config", "workloads.toml")
	if _, err := os.Stat(path); err != nil {
		t.Skip("config/workloads.toml not found (run from repo root)")
	}

	wc, err := LoadWorkloads(path)
	if err != nil {
		t.Fatalf("LoadWorkloads: %v", err)
	}

	if len(wc.Workloads) != 3 {
		t.Fatalf("expected 3 workloads, got %d", len(wc.Workloads))
	}

	for _, w := range wc.Workloads {
		if w.Name == "" {
			t.Error("workload has empty name")
		}
		if w.RepoURL == "" {
			t.Errorf("workload %q: empty repo_url", w.Name)
		}
		if w.Weight < 1 {
			t.Errorf("workload %q: weight %d < 1", w.Name, w.Weight)
		}
		if len(w.Phases) == 0 {
			t.Errorf("workload %q: no phases", w.Name)
		}
		if w.Timeout == 0 {
			t.Errorf("workload %q: timeout not parsed", w.Name)
		}
	}
}

func TestLoadWorkloads_Invalid(t *testing.T) {
	_, err := LoadWorkloads("/nonexistent/path.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadWorkloads_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.toml")
	os.WriteFile(path, []byte(""), 0o644)

	_, err := LoadWorkloads(path)
	if err == nil {
		t.Fatal("expected error for empty workloads")
	}
}

func TestLoadWorkloads_WithSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	os.WriteFile(path, []byte(`
concurrency = 8
job_timeout = "15m"

[[workload]]
name = "test"
repo_url = "https://example.com/repo.git"
weight = 2
timeout = "3m"

[[workload.phases]]
phase = "deps"
command = ["npm", "ci"]
`), 0o644)

	wc, err := LoadWorkloads(path)
	if err != nil {
		t.Fatalf("LoadWorkloads: %v", err)
	}

	if wc.Concurrency != 8 {
		t.Errorf("concurrency: got %d, want 8", wc.Concurrency)
	}
	if wc.JobTimeout.Minutes() != 15 {
		t.Errorf("job_timeout: got %s, want 15m", wc.JobTimeout)
	}
	if wc.Workloads[0].Weight != 2 {
		t.Errorf("weight: got %d, want 2", wc.Workloads[0].Weight)
	}
	if wc.Workloads[0].Timeout.Minutes() != 3 {
		t.Errorf("timeout: got %s, want 3m", wc.Workloads[0].Timeout)
	}
}

func TestBuildSelectionTable(t *testing.T) {
	workloads := []Workload{
		{Name: "a", Weight: 3},
		{Name: "b", Weight: 1},
		{Name: "c", Weight: 2},
	}

	table := BuildSelectionTable(workloads)

	// Expected: [0,0,0, 1, 2,2]
	expected := []int{0, 0, 0, 1, 2, 2}
	if len(table) != len(expected) {
		t.Fatalf("table length: got %d, want %d", len(table), len(expected))
	}
	for i, v := range table {
		if v != expected[i] {
			t.Errorf("table[%d]: got %d, want %d", i, v, expected[i])
		}
	}
}

func TestBuildSelectionTable_DefaultWeight(t *testing.T) {
	workloads := []Workload{
		{Name: "a", Weight: 0},
		{Name: "b"},
	}

	table := BuildSelectionTable(workloads)

	// Weight 0 and unset both default to 1.
	if len(table) != 2 {
		t.Fatalf("table length: got %d, want 2", len(table))
	}
}

func TestDefaultWorkloads(t *testing.T) {
	workloads := DefaultWorkloads()
	if len(workloads) != 3 {
		t.Fatalf("expected 3 default workloads, got %d", len(workloads))
	}
	for _, w := range workloads {
		if w.Weight < 1 {
			t.Errorf("workload %q: weight %d < 1", w.Name, w.Weight)
		}
	}
}
