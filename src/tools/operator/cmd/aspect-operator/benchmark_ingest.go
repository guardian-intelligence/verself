package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
)

// benchmark-ingest reads a checkpoint-canary JSON report and writes one row
// per case dispatch into verself.benchmark_runs. Run from operator context
// (Pomerium SSH forward); the canary itself stays runnable from CI without
// ClickHouse access.

type benchmarkIngestOptions struct {
	operatorRuntimeOptions
	reportPath string
}

type benchmarkRunRow struct {
	BenchmarkRunID       uuid.UUID `ch:"benchmark_run_id"`
	Workload             string    `ch:"workload"`
	Provider             string    `ch:"provider"`
	CacheState           string    `ch:"cache_state"`
	RunnerClass          string    `ch:"runner_class"`
	RepositoryFullName   string    `ch:"repository_full_name"`
	HeadBranch           string    `ch:"head_branch"`
	CommitSHA            string    `ch:"commit_sha"`
	ProviderRunID        uint64    `ch:"provider_run_id"`
	ProviderJobID        uint64    `ch:"provider_job_id"`
	AttemptID            uuid.UUID `ch:"attempt_id"`
	ExecutionID          uuid.UUID `ch:"execution_id"`
	QueuedMs             uint64    `ch:"queued_ms"`
	BootMs               uint64    `ch:"boot_ms"`
	CheckoutMs           uint64    `ch:"checkout_ms"`
	SetupMs              uint64    `ch:"setup_ms"`
	InstallMs            uint64    `ch:"install_ms"`
	TestMs               uint64    `ch:"test_ms"`
	WallClockMs          uint64    `ch:"wall_clock_ms"`
	CheckpointRestoreMs  uint64    `ch:"checkpoint_restore_ms"`
	CheckpointSaveMs     uint64    `ch:"checkpoint_save_ms"`
	CheckpointUsedBytes  uint64    `ch:"checkpoint_used_bytes"`
	CacheHit             uint8     `ch:"cache_hit"`
	Result               string    `ch:"result"`
	ErrorClass           string    `ch:"error_class"`
	ObservedAt           time.Time `ch:"observed_at"`
	TraceID              string    `ch:"trace_id"`
}

func cmdBenchmarkIngest(args []string) error {
	opts := benchmarkIngestOptions{}
	fs := flagSet("benchmark-ingest")
	fs.StringVar(&opts.reportPath, "report", "", "Path to a checkpoint-canary JSON report (required)")
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
	rows := make([]benchmarkRunRow, 0, len(report.Cases))
	for _, c := range report.Cases {
		rows = append(rows, benchmarkRunFromCase(c))
	}
	if len(rows) == 0 {
		return fmt.Errorf("no cases in report %s", opts.reportPath)
	}
	return runOperatorRuntime("benchmark.ingest", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"},
		func(rt *opruntime.Runtime, chClient *opch.Client) error {
			return insertBenchmarkRuns(rt.Ctx, chClient.Conn, rows)
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
		if attemptID, err := uuid.Parse(strings.TrimSpace(c.SandboxRun.RunID)); err == nil {
			// SandboxRunRecord.RunID is the execution_id surfaced over the public API;
			// the typed attempt id is not yet exposed there, so keep both as the same
			// uuid until the sandbox API surfaces attempt_id directly.
			row.ExecutionID = attemptID
			row.AttemptID = attemptID
		}
		if execID, err := uuid.Parse(strings.TrimSpace(c.SandboxRun.ExecutionID)); err == nil {
			row.ExecutionID = execID
		}
		if c.SandboxRun.Runner != nil {
			row.RepositoryFullName = firstNonEmpty(c.SandboxRun.Runner.RepositoryFullName, row.RepositoryFullName)
			row.HeadBranch = c.SandboxRun.Runner.HeadBranch
			row.RunnerClass = "" // sandbox public API does not surface runner_class today
			row.ProviderRunID = parseUint64(c.SandboxRun.Runner.ProviderRunID)
			row.ProviderJobID = parseUint64(c.SandboxRun.Runner.ProviderJobID)
		}
	}
	return row
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
