package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/forge-metal/forge-metal/internal/clickhouse"
)

// jobResult holds the outcome of a single benchmark job.
type jobResult struct {
	Event *clickhouse.CIEvent
	Err   error
}

// phaseResult records the outcome of a single phase execution.
type phaseResult struct {
	Duration time.Duration
	ExitCode int
}

// runJob executes a single workload on a ZFS clone and returns a populated CIEvent.
//
// Lifecycle: allocate clone -> prepare workspace -> run phases -> collect metrics -> release.
// A phase failure (non-zero exit) does NOT stop subsequent phases.
func (r *Runner) runJob(ctx context.Context, w Workload, cfg *Config) jobResult {
	jobID := uuid.New()
	e2eStart := time.Now()
	event := &clickhouse.CIEvent{
		JobID:           jobID,
		RunID:           cfg.RunID,
		NodeID:          cfg.NodeID,
		Region:          cfg.Region,
		Plan:            cfg.Plan,
		Repo:            w.RepoName(),
		Branch:          w.Branch,
		StartedAt:       e2eStart,
		CreatedAt:       e2eStart,
		CPUModel:        r.hw.CPUModel,
		Cores:           r.hw.Cores,
		MemoryMB:        r.hw.MemoryMB,
		DiskType:        r.hw.DiskType,
		GoldenSnapshot:  r.harness.GoldenSnapshot(),
		NodeVersion:     r.env.NodeVersion,
		NPMVersion:      r.env.NPMVersion,
		NPMCacheHit:     boolToUint8(w.NPMCacheHit),
		NextCacheHit:    boolToUint8(w.NextCacheHit),
		TSCCacheHit:     boolToUint8(w.TSCCacheHit),
		LockfileChanged: boolToUint8(w.LockfileChanged),
	}
	event.JobConfigJSON = marshalJobConfig(w)

	if age, err := r.harness.GoldenAge(ctx); err == nil {
		event.GoldenAgeHours = float32(age.Hours())
	}

	logger := r.logger.With("job_id", jobID, "workload", w.Name)

	clone, err := r.harness.Allocate(ctx, jobID.String())
	if err != nil {
		return jobResult{Event: event, Err: fmt.Errorf("allocate clone: %w", err)}
	}
	released := false
	defer func() {
		if !released {
			if err := clone.Release(); err != nil {
				logger.Warn("clone release failed", "err", err)
			}
		}
	}()

	event.ZFSCloneNs = clone.AllocDuration.Nanoseconds()
	logger.Info("clone allocated", "duration_ms", clone.AllocDuration.Milliseconds())

	workDir, err := prepareWorkspace(ctx, clone.Mountpoint(), w)
	if err != nil {
		return jobResult{Event: event, Err: err}
	}

	event.CommitSHA = resolveCommitSHA(ctx, workDir)

	cg, cgErr := newCgroupScope(jobID.String())
	if cgErr != nil {
		logger.Warn("cgroup unavailable, resource metrics will be zero", "err", cgErr)
	}
	if cg != nil {
		defer cg.cleanup()
	}

	phaseEnv := append(os.Environ(),
		"CI=true",
		"NODE_ENV=production",
		"NEXT_TELEMETRY_DISABLED=1",
	)
	phaseEnv = appendEnvMap(phaseEnv, w.Env)

	for _, stage := range buildPhaseStages(w.Phases) {
		if ctx.Err() != nil {
			break
		}
		for _, result := range runPhaseStage(ctx, stage, workDir, phaseEnv, cg, logger) {
			applyPhaseResult(event, result.Phase, result.Result)
		}
	}

	if cg != nil {
		stats := cg.collect()
		event.CPUUserMs = stats.CPUUserUs / 1000
		event.CPUSystemMs = stats.CPUSystemUs / 1000
		event.MemoryPeakBytes = stats.MemoryPeak
		event.IOReadBytes = stats.IOReadBytes
		event.IOWriteBytes = stats.IOWriteBytes
	}

	if err := clone.CollectMetrics(ctx); err == nil {
		event.ZFSWrittenBytes = clone.WrittenBytes
	}

	if err := clone.MarkDone(ctx); err != nil {
		logger.Warn("mark done failed", "err", err)
	}

	event.TotalCINs = event.DepsInstallNs + event.LintNs + event.TypecheckNs +
		event.BuildNs + event.TestNs
	event.CompletedAt = time.Now()
	event.TotalE2ENs = event.CompletedAt.Sub(e2eStart).Nanoseconds()

	// Release clone explicitly to measure actual ZFS destroy duration.
	cleanupStart := time.Now()
	if err := clone.Release(); err != nil {
		logger.Warn("clone release failed", "err", err)
	}
	released = true
	event.CleanupNs = time.Since(cleanupStart).Nanoseconds()

	return jobResult{Event: event}
}

type stageResult struct {
	Phase  Phase
	Result phaseResult
}

// runPhase executes a single command within the working directory.
// Output is captured; on failure, the tail is logged for debugging.
func runPhase(ctx context.Context, argv []string, workDir string, env []string,
	cg *cgroupScope, logger *slog.Logger) phaseResult {

	start := time.Now()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = env

	if cg != nil {
		cmd.SysProcAttr = cg.sysProcAttr()
	}

	out, err := cmd.CombinedOutput()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	dur := time.Since(start)
	if exitCode != 0 {
		logger.Warn("phase failed",
			"cmd", strings.Join(argv, " "),
			"exit", exitCode,
			"duration_ms", dur.Milliseconds(),
			"output_tail", truncate(string(out), 2048),
		)
	} else {
		logger.Info("phase complete",
			"cmd", strings.Join(argv, " "),
			"exit", exitCode,
			"duration_ms", dur.Milliseconds(),
		)
	}

	return phaseResult{Duration: dur, ExitCode: exitCode}
}

func runPhaseStage(ctx context.Context, stage phaseStage, workDir string, env []string,
	cg *cgroupScope, logger *slog.Logger) []stageResult {

	results := make([]stageResult, len(stage.Phases))

	if len(stage.Phases) == 1 {
		pc := stage.Phases[0]
		results[0] = stageResult{
			Phase:  pc.Phase,
			Result: runPhase(ctx, pc.Command, workDir, env, cg, logger.With("phase", pc.Phase)),
		}
		return results
	}

	logger.Info("phase stage starting",
		"stage", stage.Number,
		"parallelism", len(stage.Phases),
	)

	var wg sync.WaitGroup
	wg.Add(len(stage.Phases))
	for i, pc := range stage.Phases {
		go func(i int, pc PhaseCmd) {
			defer wg.Done()
			results[i] = stageResult{
				Phase:  pc.Phase,
				Result: runPhase(ctx, pc.Command, workDir, env, cg, logger.With("phase", pc.Phase)),
			}
		}(i, pc)
	}
	wg.Wait()
	return results
}

// applyPhaseResult maps a phase result into the appropriate CIEvent fields.
func applyPhaseResult(event *clickhouse.CIEvent, phase Phase, pr phaseResult) {
	ns := pr.Duration.Nanoseconds()
	exit := int8(pr.ExitCode)

	switch phase {
	case PhaseDeps:
		event.DepsInstallNs = ns
	case PhaseLint:
		event.LintNs = ns
		event.LintExit = exit
	case PhaseTypecheck:
		event.TypecheckNs = ns
		event.TypecheckExit = exit
	case PhaseBuild:
		event.BuildNs = ns
		event.BuildExit = exit
	case PhaseTest:
		event.TestNs = ns
		event.TestExit = exit
	}
}

func prepareWorkspace(ctx context.Context, cloneMountpoint string, w Workload) (string, error) {
	var workDir string
	switch w.Source {
	case SourceGitClone:
		workDir = filepath.Join(cloneMountpoint, "workspace")
		if err := gitClone(ctx, w.RepoURL, w.Branch, workDir); err != nil {
			return "", fmt.Errorf("git clone %s: %w", w.RepoURL, err)
		}
	case SourceSeededWorkspace:
		resolved, err := resolveSeededWorkspace(cloneMountpoint, w.WorkspacePath)
		if err != nil {
			return "", fmt.Errorf("resolve workspace %q: %w", w.WorkspacePath, err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return "", fmt.Errorf("seeded workspace %s: %w", resolved, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("seeded workspace %s is not a directory", resolved)
		}
		workDir = resolved
	default:
		return "", fmt.Errorf("unsupported source %q", w.Source)
	}

	if w.SubDir != "" {
		workDir = filepath.Join(workDir, w.SubDir)
	}

	info, err := os.Stat(workDir)
	if err != nil {
		return "", fmt.Errorf("workdir %s: %w", workDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir %s is not a directory", workDir)
	}
	return workDir, nil
}

func resolveSeededWorkspace(cloneMountpoint, workspacePath string) (string, error) {
	if workspacePath == "" {
		return "", fmt.Errorf("workspace_path is empty")
	}
	root := filepath.Clean(cloneMountpoint)
	target := filepath.Join(root, workspacePath)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes clone root")
	}
	return target, nil
}

// gitClone performs a shallow git clone into the target directory.
func gitClone(ctx context.Context, repoURL, branch, targetDir string) error {
	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repoURL, targetDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// resolveCommitSHA gets the HEAD commit SHA from a git directory.
func resolveCommitSHA(ctx context.Context, gitDir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = gitDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	sha := strings.TrimSpace(string(out))
	if len(sha) > 40 {
		sha = sha[:40]
	}
	return sha
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

func appendEnvMap(env []string, vars map[string]string) []string {
	if len(vars) == 0 {
		return env
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+vars[key])
	}
	return env
}

func boolToUint8(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

func marshalJobConfig(w Workload) string {
	timeout := ""
	if w.Timeout > 0 {
		timeout = w.Timeout.String()
	}
	payload := struct {
		Name            string            `json:"name"`
		Project         string            `json:"project,omitempty"`
		Source          SourceMode        `json:"source"`
		RepoURL         string            `json:"repo_url,omitempty"`
		Branch          string            `json:"branch,omitempty"`
		WorkspacePath   string            `json:"workspace_path,omitempty"`
		SubDir          string            `json:"sub_dir,omitempty"`
		Timeout         string            `json:"timeout,omitempty"`
		Weight          int               `json:"weight"`
		NPMCacheHit     bool              `json:"npm_cache_hit"`
		NextCacheHit    bool              `json:"next_cache_hit"`
		TSCCacheHit     bool              `json:"tsc_cache_hit"`
		LockfileChanged bool              `json:"lockfile_changed"`
		Env             map[string]string `json:"env,omitempty"`
		Phases          []PhaseCmd        `json:"phases"`
	}{
		Name:            w.Name,
		Project:         w.Project,
		Source:          w.Source,
		RepoURL:         w.RepoURL,
		Branch:          w.Branch,
		WorkspacePath:   w.WorkspacePath,
		SubDir:          w.SubDir,
		Timeout:         timeout,
		Weight:          w.Weight,
		NPMCacheHit:     w.NPMCacheHit,
		NextCacheHit:    w.NextCacheHit,
		TSCCacheHit:     w.TSCCacheHit,
		LockfileChanged: w.LockfileChanged,
		Env:             w.Env,
		Phases:          w.Phases,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"name":%q,"error":"marshal_job_config"}`, w.Name)
	}
	return string(data)
}
