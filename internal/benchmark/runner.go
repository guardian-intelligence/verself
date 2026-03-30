package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/forge-metal/forge-metal/internal/clickhouse"
	"github.com/forge-metal/forge-metal/internal/zfsharness"
)

// Config holds tunable benchmark parameters.
// Immutable once created — reconfiguration swaps the entire struct atomically.
type Config struct {
	Workloads   []Workload
	Concurrency int           // max parallel jobs
	Iterations  int           // total jobs to run (0 = run until stopped)
	JobTimeout  time.Duration // default per-job timeout
	RunID       string        // shared across all jobs in a session
	NodeID      string        // bare-metal node identifier
	Region      string        // datacenter region
	Plan        string        // server plan
}

// RunStats holds live counters for the benchmark run.
type RunStats struct {
	Completed int64
	Failed    int64
	InFlight  int64
	Elapsed   time.Duration
}

// Runner orchestrates concurrent benchmark job execution.
type Runner struct {
	harness  *zfsharness.Harness
	chClient *clickhouse.Client // nil = log-only mode
	config   atomic.Pointer[Config]
	hw       HardwareInfo
	env      EnvInfo

	completed atomic.Int64
	failed    atomic.Int64
	inFlight  atomic.Int64
	startTime time.Time

	// slotOpen signals the dispatch loop that a concurrency slot freed up
	// or that concurrency limits changed via Reconfigure.
	slotOpen chan struct{}

	logger *slog.Logger
}

// New creates a Runner. Detects hardware and environment once at startup.
func New(harness *zfsharness.Harness, chClient *clickhouse.Client,
	cfg Config, logger *slog.Logger) *Runner {

	if cfg.RunID == "" {
		cfg.RunID = uuid.New().String()
	}

	r := &Runner{
		harness:   harness,
		chClient:  chClient,
		hw:        detectHardware(),
		env:       detectEnv(),
		startTime: time.Now(),
		slotOpen:  make(chan struct{}, 1),
		logger:    logger,
	}
	r.config.Store(&cfg)
	return r
}

// Run starts the benchmark loop. Blocks until ctx is cancelled or
// Iterations is reached. Uses inFlight counter + slotOpen signal
// instead of a fixed-size semaphore, so Reconfigure'd concurrency
// limits take effect immediately.
//
// Individual job failures are logged but do not stop the runner.
func (r *Runner) Run(ctx context.Context) error {
	r.startTime = time.Now()
	cfg := r.config.Load()

	r.logger.Info("benchmark starting",
		"concurrency", cfg.Concurrency,
		"iterations", cfg.Iterations,
		"workloads", len(cfg.Workloads),
		"run_id", cfg.RunID,
		"hardware", fmt.Sprintf("%s (%d cores, %dMB)", r.hw.CPUModel, r.hw.Cores, r.hw.MemoryMB),
		"node", r.env.NodeVersion,
	)

	if len(cfg.Workloads) == 0 {
		return fmt.Errorf("no workloads configured")
	}

	initCgroupSlice(r.logger)
	cleanStaleCgroupScopes(r.logger)

	var wg sync.WaitGroup
	var next, dispatched int

	for {
		if ctx.Err() != nil {
			break
		}

		cfg = r.config.Load()

		if cfg.Iterations > 0 && dispatched >= cfg.Iterations {
			break
		}
		if len(cfg.Workloads) == 0 {
			break
		}

		// Wait for a concurrency slot. Re-reads config each wake so
		// Reconfigure'd concurrency limits take effect immediately.
		for r.inFlight.Load() >= int64(cfg.Concurrency) {
			select {
			case <-r.slotOpen:
				cfg = r.config.Load()
			case <-ctx.Done():
				goto wait
			}
		}

		w := cfg.Workloads[next%len(cfg.Workloads)]
		next++
		dispatched++

		timeout := cfg.JobTimeout
		if w.Timeout > 0 {
			timeout = w.Timeout
		}

		wg.Add(1)
		r.inFlight.Add(1)
		go func(w Workload, timeout time.Duration, cfgSnap *Config) {
			defer func() {
				r.inFlight.Add(-1)
				select {
				case r.slotOpen <- struct{}{}:
				default:
				}
				wg.Done()
			}()

			jobCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			result := r.runJob(jobCtx, w, cfgSnap)
			if result.Err != nil {
				r.failed.Add(1)
				r.logger.Error("job failed",
					"workload", w.Name,
					"err", result.Err,
				)
			} else {
				r.completed.Add(1)
			}

			// Emit event even on failure — we want partial data.
			if result.Event != nil {
				r.emit(ctx, result.Event)
			}
		}(w, timeout, cfg)
	}

wait:
	wg.Wait()

	stats := r.Stats()
	r.logger.Info("benchmark complete",
		"completed", stats.Completed,
		"failed", stats.Failed,
		"elapsed", stats.Elapsed.Round(time.Millisecond),
	)

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// Reconfigure atomically swaps the active configuration.
// In-flight jobs continue with their captured config snapshot.
// The next dispatch iteration picks up the new config.
func (r *Runner) Reconfigure(cfg Config) {
	if cfg.RunID == "" {
		cfg.RunID = r.config.Load().RunID
	}
	r.config.Store(&cfg)
	r.logger.Info("reconfigured",
		"concurrency", cfg.Concurrency,
		"workloads", len(cfg.Workloads),
		"iterations", cfg.Iterations,
	)
	// Wake dispatch loop so it re-reads config.
	select {
	case r.slotOpen <- struct{}{}:
	default:
	}
}

// Stats returns current run statistics.
func (r *Runner) Stats() RunStats {
	return RunStats{
		Completed: r.completed.Load(),
		Failed:    r.failed.Load(),
		InFlight:  r.inFlight.Load(),
		Elapsed:   time.Since(r.startTime),
	}
}

// emit logs the event and optionally writes it to ClickHouse.
func (r *Runner) emit(ctx context.Context, event *clickhouse.CIEvent) {
	r.logger.Info("benchmark_event",
		"job_id", event.JobID,
		"workload", event.Repo,
		"zfs_clone_ms", event.ZFSCloneNs/1e6,
		"deps_ms", event.DepsInstallNs/1e6,
		"lint_ms", event.LintNs/1e6,
		"typecheck_ms", event.TypecheckNs/1e6,
		"build_ms", event.BuildNs/1e6,
		"test_ms", event.TestNs/1e6,
		"total_ci_ms", event.TotalCINs/1e6,
		"total_e2e_ms", event.TotalE2ENs/1e6,
		"zfs_written_mb", event.ZFSWrittenBytes/(1024*1024),
		"mem_peak_mb", event.MemoryPeakBytes/(1024*1024),
		"cpu_user_ms", event.CPUUserMs,
		"lint_exit", event.LintExit,
		"build_exit", event.BuildExit,
		"test_exit", event.TestExit,
	)

	if r.chClient != nil {
		if err := r.chClient.InsertEvent(ctx, event); err != nil {
			r.logger.Warn("clickhouse insert failed", "err", err)
		}
	}
}
