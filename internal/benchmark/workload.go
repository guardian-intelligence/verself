package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// Phase represents a discrete CI step.
type Phase string

const (
	PhaseDeps      Phase = "deps"
	PhaseLint      Phase = "lint"
	PhaseTypecheck Phase = "typecheck"
	PhaseBuild     Phase = "build"
	PhaseTest      Phase = "test"
)

// SourceMode defines how a benchmark job gets its repository content.
type SourceMode string

const (
	SourceGitClone        SourceMode = "git_clone"
	SourceSeededWorkspace SourceMode = "seeded_workspace"
)

// PhaseCmd defines what to run for a single CI phase.
type PhaseCmd struct {
	Phase   Phase    `toml:"phase" json:"phase"`
	Command []string `toml:"command" json:"command"`
	Stage   *int     `toml:"stage" json:"stage,omitempty"`
}

// Workload defines a reproducible CI job against a real project.
type Workload struct {
	Name            string            `toml:"name"`
	Project         string            `toml:"project"`
	Source          SourceMode        `toml:"source"`
	RepoURL         string            `toml:"repo_url"`
	Branch          string            `toml:"branch"`
	WorkspacePath   string            `toml:"workspace_path"`
	SubDir          string            `toml:"sub_dir"`
	Phases          []PhaseCmd        `toml:"phases"`
	Env             map[string]string `toml:"env"`
	NPMCacheHit     bool              `toml:"npm_cache_hit"`
	NextCacheHit    bool              `toml:"next_cache_hit"`
	TSCCacheHit     bool              `toml:"tsc_cache_hit"`
	LockfileChanged bool              `toml:"lockfile_changed"`
	Timeout         time.Duration     `toml:"-"`
	RawTimeout      string            `toml:"timeout"`
	Weight          int               `toml:"weight"` // relative frequency in mix (default 1)
}

// WorkloadConfig holds workloads and optional runtime settings
// parsed from a TOML file.
type WorkloadConfig struct {
	Workloads   []Workload
	Concurrency int           // 0 = not specified in file
	JobTimeout  time.Duration // 0 = not specified in file
}

// workloadFile is the TOML wire format.
type workloadFile struct {
	Concurrency   int        `toml:"concurrency"`
	RawJobTimeout string     `toml:"job_timeout"`
	Workloads     []Workload `toml:"workload"`
}

// LoadWorkloads reads workload definitions and optional settings from a TOML file.
func LoadWorkloads(path string) (*WorkloadConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workloads file: %w", err)
	}

	var f workloadFile
	if err := toml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse workloads TOML: %w", err)
	}

	for i := range f.Workloads {
		w := &f.Workloads[i]
		if err := normalizeWorkload(w, i); err != nil {
			return nil, err
		}
	}

	if len(f.Workloads) == 0 {
		return nil, fmt.Errorf("workloads file contains no workloads")
	}

	wc := &WorkloadConfig{
		Workloads:   f.Workloads,
		Concurrency: f.Concurrency,
	}
	if f.RawJobTimeout != "" {
		d, err := time.ParseDuration(f.RawJobTimeout)
		if err != nil {
			return nil, fmt.Errorf("parse job_timeout %q: %w", f.RawJobTimeout, err)
		}
		wc.JobTimeout = d
	}

	return wc, nil
}

func normalizeWorkload(w *Workload, index int) error {
	if w.Weight <= 0 {
		w.Weight = 1
	}
	if w.RawTimeout != "" {
		d, err := time.ParseDuration(w.RawTimeout)
		if err != nil {
			return fmt.Errorf("workload %q: parse timeout %q: %w", w.Name, w.RawTimeout, err)
		}
		w.Timeout = d
	}
	if w.Name == "" {
		return fmt.Errorf("workload at index %d: name is required", index)
	}
	if w.Project == "" {
		w.Project = w.Name
	}
	if w.Source == "" {
		w.Source = SourceGitClone
	}

	switch w.Source {
	case SourceGitClone:
		if w.RepoURL == "" {
			return fmt.Errorf("workload %q: repo_url is required for %q", w.Name, w.Source)
		}
	case SourceSeededWorkspace:
		if w.WorkspacePath == "" {
			return fmt.Errorf("workload %q: workspace_path is required for %q", w.Name, w.Source)
		}
		w.WorkspacePath = filepath.Clean(w.WorkspacePath)
	default:
		return fmt.Errorf("workload %q: unknown source %q", w.Name, w.Source)
	}

	if len(w.Phases) == 0 {
		return fmt.Errorf("workload %q: at least one phase is required", w.Name)
	}

	seen := make(map[Phase]struct{}, len(w.Phases))
	for j := range w.Phases {
		pc := &w.Phases[j]
		if err := validatePhase(pc, w.Name); err != nil {
			return err
		}
		if _, ok := seen[pc.Phase]; ok {
			return fmt.Errorf("workload %q: phase %q configured more than once", w.Name, pc.Phase)
		}
		seen[pc.Phase] = struct{}{}
	}

	return nil
}

func validatePhase(pc *PhaseCmd, workloadName string) error {
	switch pc.Phase {
	case PhaseDeps, PhaseLint, PhaseTypecheck, PhaseBuild, PhaseTest:
	default:
		return fmt.Errorf("workload %q: unknown phase %q", workloadName, pc.Phase)
	}
	if len(pc.Command) == 0 {
		return fmt.Errorf("workload %q: phase %q has empty command", workloadName, pc.Phase)
	}
	if pc.Stage != nil && *pc.Stage < 0 {
		return fmt.Errorf("workload %q: phase %q has negative stage %d", workloadName, pc.Phase, *pc.Stage)
	}
	return nil
}

// RepoName returns the stable repository/project label for telemetry.
func (w Workload) RepoName() string {
	if w.Project != "" {
		return w.Project
	}
	return w.Name
}

type phaseStage struct {
	Number int
	Phases []PhaseCmd
}

func buildPhaseStages(phases []PhaseCmd) []phaseStage {
	stageBuckets := make(map[int][]PhaseCmd, len(phases))
	stageOrder := make([]int, 0, len(phases))

	for i, pc := range phases {
		stageNumber := i
		if pc.Stage != nil {
			stageNumber = *pc.Stage
		}
		if _, ok := stageBuckets[stageNumber]; !ok {
			stageOrder = append(stageOrder, stageNumber)
		}
		stageBuckets[stageNumber] = append(stageBuckets[stageNumber], pc)
	}

	sort.Ints(stageOrder)

	stages := make([]phaseStage, 0, len(stageOrder))
	for _, n := range stageOrder {
		stages = append(stages, phaseStage{
			Number: n,
			Phases: stageBuckets[n],
		})
	}
	return stages
}

// BuildSelectionTable expands workloads by weight into an index table
// for weighted round-robin dispatch.
// Example: weights [3, 1, 1] -> table [0, 0, 0, 1, 2].
func BuildSelectionTable(workloads []Workload) []int {
	var table []int
	for i, w := range workloads {
		weight := w.Weight
		if weight <= 0 {
			weight = 1
		}
		for range weight {
			table = append(table, i)
		}
	}
	return table
}

// DefaultWorkloads returns the built-in catalog of real-world Next.js projects.
// Used as fallback when no workloads TOML file is provided.
func DefaultWorkloads() []Workload {
	return []Workload{
		{
			Name:    "next-learn",
			Project: "next-learn",
			Source:  SourceGitClone,
			RepoURL: "https://github.com/vercel/next-learn.git",
			Branch:  "main",
			SubDir:  "dashboard/final-example",
			Phases: []PhaseCmd{
				{Phase: PhaseDeps, Command: []string{"npm", "ci"}},
				{Phase: PhaseLint, Command: []string{"npm", "run", "lint"}},
				{Phase: PhaseBuild, Command: []string{"npm", "run", "build"}},
			},
			Timeout: 5 * time.Minute,
			Weight:  1,
		},
		{
			Name:    "taxonomy",
			Project: "taxonomy",
			Source:  SourceGitClone,
			RepoURL: "https://github.com/shadcn-ui/taxonomy.git",
			Branch:  "main",
			Phases: []PhaseCmd{
				{Phase: PhaseDeps, Command: []string{"npm", "ci"}},
				{Phase: PhaseLint, Command: []string{"npm", "run", "lint"}, Stage: intPtr(1)},
				{Phase: PhaseTypecheck, Command: []string{"npx", "tsc", "--noEmit"}, Stage: intPtr(1)},
				{Phase: PhaseBuild, Command: []string{"npm", "run", "build"}},
			},
			Timeout: 8 * time.Minute,
			Weight:  1,
		},
		{
			Name:    "cal.com",
			Project: "cal.com",
			Source:  SourceGitClone,
			RepoURL: "https://github.com/calcom/cal.com.git",
			Branch:  "main",
			SubDir:  "apps/web",
			Phases: []PhaseCmd{
				{Phase: PhaseDeps, Command: []string{"npm", "ci"}},
				{Phase: PhaseLint, Command: []string{"npm", "run", "lint"}, Stage: intPtr(1)},
				{Phase: PhaseTypecheck, Command: []string{"npx", "tsc", "--noEmit"}, Stage: intPtr(1)},
				{Phase: PhaseBuild, Command: []string{"npm", "run", "build"}},
				{Phase: PhaseTest, Command: []string{"npm", "test", "--", "--passWithNoTests"}},
			},
			Timeout: 15 * time.Minute,
			Weight:  1,
		},
	}
}

func intPtr(v int) *int {
	return &v
}
