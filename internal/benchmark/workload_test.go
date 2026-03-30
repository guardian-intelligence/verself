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

	if len(wc.Workloads) < 5 {
		t.Fatalf("expected at least 5 workloads, got %d", len(wc.Workloads))
	}

	var seededCount int
	var parallelCount int
	for _, w := range wc.Workloads {
		if w.Name == "" {
			t.Error("workload has empty name")
		}
		if w.Project == "" {
			t.Errorf("workload %q: empty project", w.Name)
		}
		switch w.Source {
		case SourceGitClone:
			if w.RepoURL == "" {
				t.Errorf("workload %q: empty repo_url for git clone", w.Name)
			}
		case SourceSeededWorkspace:
			seededCount++
			if w.WorkspacePath == "" {
				t.Errorf("workload %q: empty workspace_path for seeded workspace", w.Name)
			}
		default:
			t.Errorf("workload %q: unexpected source %q", w.Name, w.Source)
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
		if hasParallelStage(w.Phases) {
			parallelCount++
		}
	}

	if seededCount == 0 {
		t.Fatal("expected at least one seeded-workspace workload")
	}
	if parallelCount == 0 {
		t.Fatal("expected at least one workload with parallel phase stages")
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
project = "example"
source = "seeded_workspace"
workspace_path = "workspaces/example"
weight = 2
timeout = "3m"
npm_cache_hit = true
env = { NODE_COMPILE_CACHE = "/var/cache/node-compile-cache" }

[[workload.phases]]
phase = "lint"
command = ["npm", "run", "lint"]
stage = 1
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
	if wc.Workloads[0].Project != "example" {
		t.Errorf("project: got %q, want %q", wc.Workloads[0].Project, "example")
	}
	if wc.Workloads[0].Source != SourceSeededWorkspace {
		t.Errorf("source: got %q, want %q", wc.Workloads[0].Source, SourceSeededWorkspace)
	}
	if wc.Workloads[0].WorkspacePath != "workspaces/example" {
		t.Errorf("workspace_path: got %q, want %q", wc.Workloads[0].WorkspacePath, "workspaces/example")
	}
	if wc.Workloads[0].Weight != 2 {
		t.Errorf("weight: got %d, want 2", wc.Workloads[0].Weight)
	}
	if wc.Workloads[0].Timeout.Minutes() != 3 {
		t.Errorf("timeout: got %s, want 3m", wc.Workloads[0].Timeout)
	}
	if !wc.Workloads[0].NPMCacheHit {
		t.Error("npm_cache_hit: got false, want true")
	}
	if got := wc.Workloads[0].Env["NODE_COMPILE_CACHE"]; got != "/var/cache/node-compile-cache" {
		t.Errorf("env NODE_COMPILE_CACHE: got %q", got)
	}
	if wc.Workloads[0].Phases[0].Stage == nil || *wc.Workloads[0].Phases[0].Stage != 1 {
		t.Fatalf("phase stage: got %v, want 1", wc.Workloads[0].Phases[0].Stage)
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

func TestLoadWorkloads_SeededWorkspaceRequiresPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	os.WriteFile(path, []byte(`
[[workload]]
name = "bad"
source = "seeded_workspace"

[[workload.phases]]
phase = "lint"
command = ["npm", "run", "lint"]
`), 0o644)

	_, err := LoadWorkloads(path)
	if err == nil {
		t.Fatal("expected error for missing workspace_path")
	}
}

func TestLoadWorkloads_RejectsDuplicatePhase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	os.WriteFile(path, []byte(`
[[workload]]
name = "bad"
repo_url = "https://example.com/repo.git"

[[workload.phases]]
phase = "lint"
command = ["npm", "run", "lint"]

[[workload.phases]]
phase = "lint"
command = ["npm", "run", "lint"]
`), 0o644)

	_, err := LoadWorkloads(path)
	if err == nil {
		t.Fatal("expected error for duplicate phase")
	}
}

func TestBuildPhaseStages(t *testing.T) {
	stages := buildPhaseStages([]PhaseCmd{
		{Phase: PhaseDeps, Command: []string{"npm", "ci"}},
		{Phase: PhaseLint, Command: []string{"npm", "run", "lint"}, Stage: intPtr(1)},
		{Phase: PhaseTypecheck, Command: []string{"npx", "tsc", "--noEmit"}, Stage: intPtr(1)},
		{Phase: PhaseBuild, Command: []string{"npm", "run", "build"}, Stage: intPtr(2)},
	})

	if len(stages) != 3 {
		t.Fatalf("stage count: got %d, want 3", len(stages))
	}
	if got := len(stages[1].Phases); got != 2 {
		t.Fatalf("parallel stage phase count: got %d, want 2", got)
	}
	if stages[1].Phases[0].Phase != PhaseLint || stages[1].Phases[1].Phase != PhaseTypecheck {
		t.Fatalf("unexpected parallel stage phases: %+v", stages[1].Phases)
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

func hasParallelStage(phases []PhaseCmd) bool {
	stageCounts := make(map[int]int, len(phases))
	for i, phase := range phases {
		stage := i
		if phase.Stage != nil {
			stage = *phase.Stage
		}
		stageCounts[stage]++
		if stageCounts[stage] > 1 {
			return true
		}
	}
	return false
}
