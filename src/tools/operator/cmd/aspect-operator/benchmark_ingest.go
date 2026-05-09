package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

// benchmark-ingest reads a checkpoint-canary JSON report, enriches each row
// from verself.checkpoint_operations (cache_hit, restore_ms, save_ms,
// used_bytes), asserts warm/dirty cases observed a generation hit, and writes
// one row per case dispatch into verself.benchmark_runs. Run from operator
// context (Pomerium SSH forward); the canary itself stays runnable from CI
// without ClickHouse access.

type benchmarkIngestOptions struct {
	operatorRuntimeOptions
	reportPath string
	verifyWarm bool
	enrich     bool
}

type benchmarkRunRow struct {
	BenchmarkRunID      uuid.UUID `ch:"benchmark_run_id"`
	Workload            string    `ch:"workload"`
	Provider            string    `ch:"provider"`
	CacheState          string    `ch:"cache_state"`
	RunnerClass         string    `ch:"runner_class"`
	RepositoryFullName  string    `ch:"repository_full_name"`
	HeadBranch          string    `ch:"head_branch"`
	CommitSHA           string    `ch:"commit_sha"`
	ProviderRunID       uint64    `ch:"provider_run_id"`
	ProviderJobID       uint64    `ch:"provider_job_id"`
	AttemptID           uuid.UUID `ch:"attempt_id"`
	ExecutionID         uuid.UUID `ch:"execution_id"`
	QueuedMs            uint64    `ch:"queued_ms"`
	BootMs              uint64    `ch:"boot_ms"`
	CheckoutMs          uint64    `ch:"checkout_ms"`
	SetupMs             uint64    `ch:"setup_ms"`
	InstallMs           uint64    `ch:"install_ms"`
	TestMs              uint64    `ch:"test_ms"`
	WallClockMs         uint64    `ch:"wall_clock_ms"`
	CheckpointRestoreMs uint64    `ch:"checkpoint_restore_ms"`
	CheckpointSaveMs    uint64    `ch:"checkpoint_save_ms"`
	CheckpointUsedBytes uint64    `ch:"checkpoint_used_bytes"`
	CacheHit            uint8     `ch:"cache_hit"`
	Result              string    `ch:"result"`
	ErrorClass          string    `ch:"error_class"`
	ObservedAt          time.Time `ch:"observed_at"`
	TraceID             string    `ch:"trace_id"`
}

// checkpointAggregate is the per-execution checkpoint_operations summary the
// ingest path joins onto each benchmark row.
type checkpointAggregate struct {
	CacheHit       bool
	RestoreMs      uint64
	SaveMs         uint64
	UsedBytes      uint64
	SourceSelected bool
	HasFailure     bool
	FailureReason  string
}

func cmdBenchmarkIngest(args []string) error {
	opts := benchmarkIngestOptions{verifyWarm: true, enrich: true}
	fs := flagSet("benchmark-ingest")
	fs.StringVar(&opts.reportPath, "report", "", "Path to a checkpoint-canary JSON report (required)")
	fs.BoolVar(&opts.verifyWarm, "verify-warm", opts.verifyWarm, "Fail if any warm/dirty case did not observe a Checkpoint generation hit")
	fs.BoolVar(&opts.enrich, "enrich", opts.enrich, "Enrich rows from verself.checkpoint_operations before insert")
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if strings.TrimSpace(opts.reportPath) == "" {
		return fmt.Errorf("--report is required")
	}
	raw, err := os.ReadFile(opts.reportPath)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}
	var report checkpointCanaryReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return fmt.Errorf("parse report: %w", err)
	}
	if len(report.Cases) == 0 {
		return fmt.Errorf("no cases in report %s", opts.reportPath)
	}
	rows := make([]benchmarkRunRow, 0, len(report.Cases))
	for _, c := range report.Cases {
		rows = append(rows, benchmarkRunFromCase(c))
	}
	return runOperatorRuntime("benchmark.ingest", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"},
		func(rt *opruntime.Runtime, chClient *opch.Client) error {
			if opts.enrich {
				if err := enrichBenchmarkRows(rt.Ctx, chClient.Conn, rows); err != nil {
					return err
				}
			}
			if err := insertBenchmarkRuns(rt.Ctx, chClient.Conn, rows); err != nil {
				return err
			}
			if opts.verifyWarm {
				if missed := assertWarmHits(rows, report.Cases); len(missed) > 0 {
					return fmt.Errorf("warm-hit assertion failed for %d case(s):\n%s", len(missed), strings.Join(missed, "\n"))
				}
			}
			return nil
		})
}

func benchmarkRunFromCase(c checkpointCanaryCaseResult) benchmarkRunRow {
	row := benchmarkRunRow{
		BenchmarkRunID:     uuid.New(),
		Workload:           c.Workload,
		Provider:           c.Provider,
		CacheState:         c.CacheState,
		RepositoryFullName: c.Repository,
		CommitSHA:          c.CommitSHA,
		WallClockMs:        uint64MaybeNeg(c.WallClockMs),
		Result:             c.Status,
		ErrorClass:         deriveErrorClass(c.Error),
		ObservedAt:         observedAtForCase(c),
	}
	if c.SandboxRun != nil {
		if execID, err := uuid.Parse(strings.TrimSpace(c.SandboxRun.ExecutionID)); err == nil {
			row.ExecutionID = execID
			// SandboxRunRecord.RunID is a uuid that today aliases the execution id;
			// keep both columns set so attempt-keyed and execution-keyed joins work.
			row.AttemptID = execID
		}
		if attemptID, err := uuid.Parse(strings.TrimSpace(c.SandboxRun.RunID)); err == nil && row.AttemptID == uuid.Nil {
			row.AttemptID = attemptID
		}
		if c.SandboxRun.Runner != nil {
			row.RepositoryFullName = firstNonEmpty(c.SandboxRun.Runner.RepositoryFullName, row.RepositoryFullName)
			row.HeadBranch = c.SandboxRun.Runner.HeadBranch
			row.RunnerClass = ""
			row.ProviderRunID = parseUint64(c.SandboxRun.Runner.ProviderRunID)
			row.ProviderJobID = parseUint64(c.SandboxRun.Runner.ProviderJobID)
		}
	}
	return row
}

// enrichBenchmarkRows fills the checkpoint_* columns from
// verself.checkpoint_operations for each row that has a non-zero execution id.
// The query collapses every operation row for the execution into one
// aggregate; the canary's repeatable cold/warm/dirty shape produces one
// Checkpoint mount per attempt today, so the collapse is safe.
func enrichBenchmarkRows(ctx context.Context, conn chdriver.Conn, rows []benchmarkRunRow) error {
	for i := range rows {
		if rows[i].ExecutionID == uuid.Nil {
			continue
		}
		agg, err := fetchCheckpointAggregate(ctx, conn, rows[i].ExecutionID)
		if err != nil {
			return fmt.Errorf("enrich row for execution %s: %w", rows[i].ExecutionID, err)
		}
		if agg.CacheHit {
			rows[i].CacheHit = 1
		}
		rows[i].CheckpointRestoreMs = agg.RestoreMs
		rows[i].CheckpointSaveMs = agg.SaveMs
		rows[i].CheckpointUsedBytes = agg.UsedBytes
	}
	return nil
}

// fetchCheckpointAggregate is the canonical source-of-truth query for
// "did this execution restore from a Checkpoint generation, and how long did
// the host_prepare/commit phases take?". It pulls all rows for an execution
// and reduces them to a typed aggregate. We sum host_prepare durations into
// restore_ms, sum commit + promotion durations into save_ms, and take the max
// used_bytes (used_bytes only flows from commit/promotion rows).
func fetchCheckpointAggregate(ctx context.Context, conn chdriver.Conn, executionID uuid.UUID) (checkpointAggregate, error) {
	const q = `
SELECT
    operation,
    result,
    sum(duration_ms) AS duration_ms,
    max(used_bytes) AS used_bytes
FROM verself.checkpoint_operations
WHERE execution_id = {execution_id:UUID}
GROUP BY operation, result
`
	rows, err := conn.Query(ctx, q, ch.Named("execution_id", executionID.String()))
	if err != nil {
		return checkpointAggregate{}, err
	}
	defer func() { _ = rows.Close() }()
	var agg checkpointAggregate
	for rows.Next() {
		var operation, result string
		var duration uint64
		var used uint64
		if err := rows.Scan(&operation, &result, &duration, &used); err != nil {
			return checkpointAggregate{}, err
		}
		switch operation {
		case "source_select":
			agg.SourceSelected = true
			if result == "hit" {
				agg.CacheHit = true
			}
		case "host_prepare":
			if result == "succeeded" {
				agg.RestoreMs += duration
			}
			if result == "failed" {
				agg.HasFailure = true
				agg.FailureReason = operation + ":" + result
			}
		case "commit", "promotion":
			if result == "succeeded" {
				agg.SaveMs += duration
			}
			if result == "failed" {
				agg.HasFailure = true
				agg.FailureReason = operation + ":" + result
			}
		}
		if used > agg.UsedBytes {
			agg.UsedBytes = used
		}
	}
	return agg, rows.Err()
}

// assertWarmHits returns one human-readable line per warm/dirty case that did
// NOT observe a Checkpoint generation hit. The canary already separates the
// dispatches by RunIndex so cold cases (RunIndex == 0) are exempt.
func assertWarmHits(rows []benchmarkRunRow, cases []checkpointCanaryCaseResult) []string {
	var missed []string
	for i, row := range rows {
		c := cases[i]
		if c.Status != "succeeded" {
			continue
		}
		if c.CacheState == "cold" {
			continue
		}
		if row.CacheHit == 1 {
			continue
		}
		missed = append(missed, fmt.Sprintf("  workload=%s provider=%s cache_state=%s run_index=%d execution_id=%s — no checkpoint_operations(operation='source_select', result='hit') row",
			c.Workload, c.Provider, c.CacheState, c.RunIndex, row.ExecutionID))
	}
	return missed
}

func observedAtForCase(c checkpointCanaryCaseResult) time.Time {
	if !c.CompletedAt.IsZero() {
		return c.CompletedAt
	}
	return time.Now().UTC()
}

func deriveErrorClass(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	if len(msg) > 64 {
		return msg[:64]
	}
	return msg
}

func insertBenchmarkRuns(ctx context.Context, conn chdriver.Conn, rows []benchmarkRunRow) error {
	batch, err := conn.PrepareBatch(ctx, "INSERT INTO verself.benchmark_runs")
	if err != nil {
		return fmt.Errorf("prepare benchmark_runs batch: %w", err)
	}
	for _, row := range rows {
		if err := batch.AppendStruct(&row); err != nil {
			return fmt.Errorf("append benchmark_runs row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send benchmark_runs batch: %w", err)
	}
	return nil
}

func uint64MaybeNeg(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

func parseUint64(raw string) uint64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	var out uint64
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0
		}
		out = out*10 + uint64(ch-'0')
	}
	return out
}
