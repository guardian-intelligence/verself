package benchmark

import "time"

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
	Phase   Phase
	Command []string // e.g. ["npm", "run", "lint"]
}

// Workload defines a reproducible CI job against a real project.
type Workload struct {
	Name    string        // human-readable, e.g. "nextjs-blog"
	RepoURL string        // git clone URL (https)
	Branch  string        // branch to check out ("" = default branch)
	SubDir  string        // subdirectory within repo ("" = root)
	Phases  []PhaseCmd    // ordered phases to execute
	Timeout time.Duration // per-workload timeout (0 = use runner default)
}

// DefaultWorkloads returns the built-in catalog of real-world Next.js projects.
// Chosen for diversity in size and build characteristics:
//
//   - small:  next-learn starter — fast deps, fast build, minimal test
//   - medium: taxonomy — real app with TypeScript, lint, typecheck, build
//   - large:  cal.com — large monorepo, heavy deps, long build+test
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
		},
	}
}
