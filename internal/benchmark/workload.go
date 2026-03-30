package benchmark

import (
	"fmt"
	"os"
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

// PhaseCmd defines what to run for a single CI phase.
type PhaseCmd struct {
	Phase   Phase    `toml:"phase"`
	Command []string `toml:"command"`
}

// Workload defines a reproducible CI job against a real project.
type Workload struct {
	Name       string        `toml:"name"`
	RepoURL    string        `toml:"repo_url"`
	Branch     string        `toml:"branch"`
	SubDir     string        `toml:"sub_dir"`
	Phases     []PhaseCmd    `toml:"phases"`
	Timeout    time.Duration `toml:"-"`
	RawTimeout string        `toml:"timeout"`
	Weight     int           `toml:"weight"` // relative frequency in mix (default 1)
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
		if w.Weight <= 0 {
			w.Weight = 1
		}
		if w.RawTimeout != "" {
			d, err := time.ParseDuration(w.RawTimeout)
			if err != nil {
				return nil, fmt.Errorf("workload %q: parse timeout %q: %w", w.Name, w.RawTimeout, err)
			}
			w.Timeout = d
		}
		if w.Name == "" {
			return nil, fmt.Errorf("workload at index %d: name is required", i)
		}
		if w.RepoURL == "" {
			return nil, fmt.Errorf("workload %q: repo_url is required", w.Name)
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
			RepoURL: "https://github.com/vercel/next-learn.git",
			Branch:  "main",
			SubDir:  "dashboard/final-example",
			Phases: []PhaseCmd{
				{PhaseDeps, []string{"npm", "ci"}},
				{PhaseLint, []string{"npm", "run", "lint"}},
				{PhaseBuild, []string{"npm", "run", "build"}},
			},
			Timeout: 5 * time.Minute,
			Weight:  1,
		},
		{
			Name:    "taxonomy",
			RepoURL: "https://github.com/shadcn-ui/taxonomy.git",
			Branch:  "main",
			Phases: []PhaseCmd{
				{PhaseDeps, []string{"npm", "ci"}},
				{PhaseLint, []string{"npm", "run", "lint"}},
				{PhaseTypecheck, []string{"npx", "tsc", "--noEmit"}},
				{PhaseBuild, []string{"npm", "run", "build"}},
			},
			Timeout: 8 * time.Minute,
			Weight:  1,
		},
		{
			Name:    "cal.com",
			RepoURL: "https://github.com/calcom/cal.com.git",
			Branch:  "main",
			SubDir:  "apps/web",
			Phases: []PhaseCmd{
				{PhaseDeps, []string{"npm", "ci"}},
				{PhaseLint, []string{"npm", "run", "lint"}},
				{PhaseTypecheck, []string{"npx", "tsc", "--noEmit"}},
				{PhaseBuild, []string{"npm", "run", "build"}},
				{PhaseTest, []string{"npm", "test", "--", "--passWithNoTests"}},
			},
			Timeout: 15 * time.Minute,
			Weight:  1,
		},
	}
}
