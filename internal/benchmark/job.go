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
	"github.com/forge-metal/forge-metal/internal/zfsharness"
)

// jobResult holds the outcome of a single benchmark job.
type jobResult struct {
	Event *clickhouse.CIEvent
	Err   error
}

// phaseResult records the outcome of a single phase execution.
type phaseResult struct {
	Phase    Phase
	Duration time.Duration
	ExitCode int
}

// runJob executes a single workload on a ZFS clone and returns a populated CIEvent.
//
// Lifecycle:
//  1. Allocate ZFS clone from golden image
//  2. Git clone the repo into the clone workspace
//  3. Run phases sequentially (deps, lint, typecheck, build, test)
//  4. Collect cgroup + ZFS metrics
//  5. Mark done, build CIEvent, release clone
//
// A phase failure (non-zero exit) does NOT stop subsequent phases.
// Only fatal errors (can't allocate clone, git clone fails) abort the job.
func runJob(ctx context.Context, harness *zfsharness.Harness, w Workload,
	hw HardwareInfo, env EnvInfo, cfg *Config, logger *slog.Logger) jobResult {

	jobID := uuid.New()
	e2eStart := time.Now()
	event := &clickhouse.CIEvent{
		JobID:     jobID,
		RunID:     cfg.RunID,
		NodeID:    cfg.NodeID,
		Region:    cfg.Region,
		Plan:      cfg.Plan,
		Repo:      w.Name,
		Branch:    w.Branch,
		StartedAt: e2eStart,
		CreatedAt: e2eStart,
		// Hardware
		CPUModel: hw.CPUModel,
		Cores:    hw.Cores,
		MemoryMB: hw.MemoryMB,
		DiskType: hw.DiskType,
		// Environment
		GoldenSnapshot: harness.GoldenSnapshot(),
		NodeVersion:    env.NodeVersion,
		NPMVersion:     env.NPMVersion,
	}

	logger = logger.With("job_id", jobID, "workload", w.Name)

	// 1. Allocate ZFS clone.
	clone, err := harness.Allocate(ctx, jobID.String())
	if err != nil {
		return jobResult{Event: event, Err: fmt.Errorf("allocate clone: %w", err)}
	}
	defer clone.Release()

	event.ZFSCloneNs = clone.AllocDuration.Nanoseconds()
	logger.Info("clone allocated", "duration_ms", clone.AllocDuration.Milliseconds())

	// 2. Git clone into workspace.
	workDir := filepath.Join(clone.Mountpoint(), "workspace")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return jobResult{Event: event, Err: fmt.Errorf("create workspace: %w", err)}
	}

	gitStart := time.Now()
	if err := gitClone(ctx, w.RepoURL, w.Branch, workDir); err != nil {
		return jobResult{Event: event, Err: fmt.Errorf("git clone %s: %w", w.RepoURL, err)}
	}
	logger.Info("git clone complete", "duration_ms", time.Since(gitStart).Milliseconds())

	// Resolve commit SHA.
	event.CommitSHA = resolveCommitSHA(ctx, workDir)

	// If SubDir is set, run phases from there.
	if w.SubDir != "" {
		workDir = filepath.Join(workDir, w.SubDir)
	}

	// 3. Set up cgroup scope for resource tracking.
	cg, cgErr := newCgroupScope(jobID.String())
	if cgErr != nil {
		logger.Warn("cgroup scope unavailable, resource metrics will be zero", "err", cgErr)
	}
	if cg != nil {
		defer cg.cleanup()
	}

	// 4. Run phases.
	for _, pc := range w.Phases {
		if ctx.Err() != nil {
			break
		}
		pr := runPhase(ctx, pc.Command, workDir, cg, logger)
		applyPhaseResult(event, pc.Phase, pr)
	}

	// 5. Collect metrics.
	if cg != nil {
		if stats, err := cg.collect(); err == nil {
			event.CPUUserMs = stats.CPUUserUs / 1000
			event.CPUSystemMs = stats.CPUSystemUs / 1000
			event.MemoryPeakBytes = stats.MemoryPeak
			event.IOReadBytes = stats.IOReadBytes
			event.IOWriteBytes = stats.IOWriteBytes
		}
	}

	if err := clone.CollectMetrics(ctx); err == nil {
		event.ZFSWrittenBytes = clone.WrittenBytes
	}

	// 6. Mark done + compute totals.
	cleanupStart := time.Now()
	_ = clone.MarkDone(ctx)

	event.TotalCINs = event.DepsInstallNs + event.LintNs + event.TypecheckNs +
		event.BuildNs + event.TestNs
	event.CompletedAt = time.Now()
	event.TotalE2ENs = event.CompletedAt.Sub(e2eStart).Nanoseconds()
	event.CleanupNs = time.Since(cleanupStart).Nanoseconds()

	return jobResult{Event: event}
}

// runPhase executes a single command within the working directory.
// If cg is non-nil, the process is added to the cgroup scope.
func runPhase(ctx context.Context, argv []string, workDir string,
	cg *cgroupScope, logger *slog.Logger) phaseResult {

	start := time.Now()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"CI=true",
		"NODE_ENV=production",
		"NEXT_TELEMETRY_DISABLED=1",
	)
	// Start the process so we can get its PID for cgroup placement.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		logger.Warn("phase start failed", "cmd", argv, "err", err)
		return phaseResult{Duration: time.Since(start), ExitCode: -1}
	}

	// Move into cgroup scope for resource accounting.
	if cg != nil {
		_ = cg.addPID(cmd.Process.Pid)
	}

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	dur := time.Since(start)
	logger.Info("phase complete",
		"cmd", strings.Join(argv, " "),
		"exit", exitCode,
		"duration_ms", dur.Milliseconds(),
	)
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
	// Clone into a temp subdir, then move contents to targetDir.
	// This avoids "directory not empty" issues with the workspace.
	cloneDir := targetDir + ".clone"
	defer os.RemoveAll(cloneDir)

	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repoURL, cloneDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}

	// Move contents from cloneDir into targetDir.
	entries, err := os.ReadDir(cloneDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(cloneDir, e.Name())
		dst := filepath.Join(targetDir, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s: %w", e.Name(), err)
		}
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

