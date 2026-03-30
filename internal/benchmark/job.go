package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
// Lifecycle: allocate clone -> git clone -> run phases -> collect metrics -> release.
// A phase failure (non-zero exit) does NOT stop subsequent phases.
func (r *Runner) runJob(ctx context.Context, w Workload, cfg *Config) jobResult {
	jobID := uuid.New()
	e2eStart := time.Now()
	event := &clickhouse.CIEvent{
		JobID:          jobID,
		RunID:          cfg.RunID,
		NodeID:         cfg.NodeID,
		Region:         cfg.Region,
		Plan:           cfg.Plan,
		Repo:           w.Name,
		Branch:         w.Branch,
		StartedAt:      e2eStart,
		CreatedAt:      e2eStart,
		CPUModel:       r.hw.CPUModel,
		Cores:          r.hw.Cores,
		MemoryMB:       r.hw.MemoryMB,
		DiskType:       r.hw.DiskType,
		GoldenSnapshot: r.harness.GoldenSnapshot(),
		NodeVersion:    r.env.NodeVersion,
		NPMVersion:     r.env.NPMVersion,
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

	// git clone creates the target directory.
	workDir := filepath.Join(clone.Mountpoint(), "workspace")
	if err := gitClone(ctx, w.RepoURL, w.Branch, workDir); err != nil {
		return jobResult{Event: event, Err: fmt.Errorf("git clone %s: %w", w.RepoURL, err)}
	}

	event.CommitSHA = resolveCommitSHA(ctx, workDir)

	if w.SubDir != "" {
		workDir = filepath.Join(workDir, w.SubDir)
	}

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

	for _, pc := range w.Phases {
		if ctx.Err() != nil {
			break
		}
		pr := runPhase(ctx, pc.Command, workDir, phaseEnv, cg, logger)
		applyPhaseResult(event, pc.Phase, pr)
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
