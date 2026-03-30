package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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
	Workloads      []Workload
	SelectionTable []int // weighted index into Workloads, built by prepareConfig
	Concurrency    int
	Iterations     int           // total jobs to run (0 = run until stopped)
	JobTimeout     time.Duration // default per-job timeout
	RunID          string
	NodeID         string
	Region         string
	Plan           string
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

	// Per-workload tracking for end-of-run summary.
	mu        sync.Mutex
	durations map[string][]time.Duration
	written   map[string][]uint64
}

// New creates a Runner. Detects hardware and environment once at startup.
func New(harness *zfsharness.Harness, chClient *clickhouse.Client,
	cfg Config, logger *slog.Logger) *Runner {

	if cfg.RunID == "" {
		cfg.RunID = uuid.New().String()
	}
	prepareConfig(&cfg)

	r := &Runner{
		harness:   harness,
		chClient:  chClient,
		hw:        detectHardware(),
		env:       detectEnv(),
		startTime: time.Now(),
		slotOpen:  make(chan struct{}, 1),
		logger:    logger,
		durations: make(map[string][]time.Duration),
		written:   make(map[string][]uint64),
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

	// Progress ticker — logs throughput every 10s, stops when Run returns.
	progressCtx, progressCancel := context.WithCancel(ctx)
	go r.progressLoop(progressCtx)

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
		if len(cfg.Workloads) == 0 || len(cfg.SelectionTable) == 0 {
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

		w := cfg.Workloads[cfg.SelectionTable[next%len(cfg.SelectionTable)]]
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
				r.recordResult(w.Name,
					time.Duration(result.Event.TotalE2ENs),
					result.Event.ZFSWrittenBytes)
			}
		}(w, timeout, cfg)
	}

wait:
	wg.Wait()
	progressCancel()
	r.logSummary()

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
	prepareConfig(&cfg)
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

// prepareConfig builds derived fields (SelectionTable) from the config.
func prepareConfig(cfg *Config) {
	cfg.SelectionTable = BuildSelectionTable(cfg.Workloads)
}

// recordResult tracks per-workload metrics for the end-of-run summary.
func (r *Runner) recordResult(workload string, e2e time.Duration, writtenBytes uint64) {
	r.mu.Lock()
	r.durations[workload] = append(r.durations[workload], e2e)
	r.written[workload] = append(r.written[workload], writtenBytes)
	r.mu.Unlock()
}

// progressLoop logs throughput every 10 seconds until ctx is cancelled.
func (r *Runner) progressLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastCompleted int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			completed := r.completed.Load()
			delta := completed - lastCompleted
			lastCompleted = completed
			// 10s interval: multiply by 6 for per-minute rate.
			rate := float64(delta) * 6

			r.logger.Info("progress",
				"completed", completed,
				"failed", r.failed.Load(),
				"in_flight", r.inFlight.Load(),
				"jobs_per_min", fmt.Sprintf("%.1f", rate),
				"elapsed", time.Since(r.startTime).Round(time.Second),
			)
		}
	}
}

// logSummary prints per-workload percentile breakdown at end of run.
func (r *Runner) logSummary() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.durations) == 0 {
		return
	}

	r.logger.Info("--- benchmark summary ---")
	for name, durs := range r.durations {
		sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })

		var totalWritten uint64
		for _, w := range r.written[name] {
			totalWritten += w
		}

		r.logger.Info("workload",
			"name", name,
			"count", len(durs),
			"p50", percentile(durs, 0.50).Round(time.Millisecond),
			"p99", percentile(durs, 0.99).Round(time.Millisecond),
			"min", durs[0].Round(time.Millisecond),
			"max", durs[len(durs)-1].Round(time.Millisecond),
			"zfs_written_total_mb", totalWritten/(1024*1024),
		)
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}
