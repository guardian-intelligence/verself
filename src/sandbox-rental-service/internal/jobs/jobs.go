// Package jobs implements the sandbox job lifecycle: submit, run, bill, record.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	billingclient "github.com/forge-metal/billing-client"
	fastsandbox "github.com/forge-metal/fast-sandbox"
	"github.com/google/uuid"
)

var ErrQuotaExceeded = errors.New("sandbox-rental: quota exceeded")

// SandboxJobLog is a ClickHouse row for sandbox job log chunks.
type SandboxJobLog struct {
	JobID     string    `ch:"job_id"`
	Seq       uint32    `ch:"seq"`
	Stream    string    `ch:"stream"`
	Chunk     string    `ch:"chunk"`
	CreatedAt time.Time `ch:"created_at"`
}

// SandboxJobEvent is a ClickHouse row for sandbox job telemetry.
type SandboxJobEvent struct {
	JobID       string    `ch:"job_id"`
	OrgID       uint64    `ch:"org_id"`
	UserID      string    `ch:"user_id"`
	RepoURL     string    `ch:"repo_url"`
	RunCommand  string    `ch:"run_command"`
	Status      string    `ch:"status"`
	ExitCode    int32     `ch:"exit_code"`
	DurationMs  int64     `ch:"duration_ms"`
	ZFSWritten  uint64    `ch:"zfs_written"`
	StdoutBytes uint64    `ch:"stdout_bytes"`
	StderrBytes uint64    `ch:"stderr_bytes"`
	StartedAt   time.Time `ch:"started_at"`
	CompletedAt time.Time `ch:"completed_at"`
	CreatedAt   time.Time `ch:"created_at"`
}

// Service manages the sandbox job lifecycle.
type Service struct {
	PG            *sql.DB
	CH            driver.Conn
	CHDatabase    string
	Orchestrator  *fastsandbox.Orchestrator
	Billing       *billingclient.ServiceClient
	BillingVCPUs  int
	BillingMemMiB int
	Logger        *slog.Logger
}

// Submit creates a job record and starts asynchronous execution.
// It returns the job ID immediately; the caller polls for completion.
func (s *Service) Submit(ctx context.Context, orgID uint64, userID, repoURL, runCommand string) (uuid.UUID, error) {
	jobID := uuid.New()
	now := time.Now().UTC()
	billingJobID, err := s.nextBillingJobID(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("allocate billing job id: %w", err)
	}

	currentConcurrent, err := s.countRunningJobs(ctx, orgID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("count running jobs: %w", err)
	}

	quotaResult, err := s.Billing.CheckQuotas(ctx, orgID, "sandbox", map[string]float64{
		"vcpu":           float64(s.BillingVCPUs),
		"gib":            float64(s.BillingMemMiB) / 1024.0,
		"concurrent_vms": float64(currentConcurrent + 1),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("billing check quotas: %w", err)
	}
	if !quotaResult.Allowed {
		return uuid.Nil, ErrQuotaExceeded
	}

	reservation, err := s.Billing.Reserve(ctx, billingJobID, orgID, "sandbox", userID, "job", jobID.String(), map[string]float64{
		"vcpu": float64(s.BillingVCPUs),
		"gib":  float64(s.BillingMemMiB) / 1024.0,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("billing reserve: %w", err)
	}
	reservationJSON, err := json.Marshal(reservation)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal billing reservation: %w", err)
	}

	// Insert job record.
	_, err = s.PG.ExecContext(ctx,
		`INSERT INTO jobs (id, org_id, user_id, repo_url, run_command, status, billing_job_id, billing_reservation, started_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, 'running', $6, $7::jsonb, $8, $8)`,
		jobID, orgID, userID, repoURL, nullString(runCommand), billingJobID, string(reservationJSON), now,
	)
	if err != nil {
		if voidErr := s.Billing.Void(ctx, reservation); voidErr != nil {
			s.Logger.Error("billing void after insert failure", "job_id", jobID, "error", voidErr)
		}
		return uuid.Nil, fmt.Errorf("insert job: %w", err)
	}

	// Run in background goroutine.
	go s.execute(jobID, orgID, userID, repoURL, runCommand, reservation)

	return jobID, nil
}

func (s *Service) execute(jobID uuid.UUID, orgID uint64, userID, repoURL, runCommand string, reservation billingclient.Reservation) {
	ctx := context.Background()
	startedAt := time.Now().UTC()

	cmd := []string{"echo", "hello"}
	if runCommand != "" {
		cmd = []string{"sh", "-c", runCommand}
	}

	jobCfg := fastsandbox.JobConfig{
		JobID:      jobID.String(),
		RunCommand: cmd,
		Env: map[string]string{
			"REPO_URL": repoURL,
		},
	}

	result, err := s.Orchestrator.Run(ctx, jobCfg)
	completedAt := time.Now().UTC()
	durationMs := completedAt.Sub(startedAt).Milliseconds()

	status := "completed"
	exitCode := 0
	if err != nil {
		status = "failed"
		s.Logger.Error("job execution failed", "job_id", jobID, "error", err)
		if voidErr := s.Billing.Void(ctx, reservation); voidErr != nil {
			s.Logger.Error("billing void", "job_id", jobID, "error", voidErr)
		}
	} else {
		exitCode = result.ExitCode
		if exitCode != 0 {
			status = "failed"
		}
		actualSeconds := uint32(durationMs / 1000)
		if actualSeconds == 0 {
			actualSeconds = 1
		}
		if settleErr := s.Billing.Settle(ctx, reservation, actualSeconds); settleErr != nil {
			s.Logger.Error("billing settle", "job_id", jobID, "error", settleErr)
		}
	}

	var zfsWritten uint64
	var stdoutBytes, stderrBytes uint64
	if err == nil {
		zfsWritten = result.ZFSWritten
		stdoutBytes = result.StdoutBytes
		stderrBytes = result.StderrBytes
	}

	// Update PG job record.
	_, pgErr := s.PG.ExecContext(ctx,
		`UPDATE jobs SET status = $1, exit_code = $2, duration_ms = $3, zfs_written = $4, completed_at = $5 WHERE id = $6`,
		status, exitCode, durationMs, int64(zfsWritten), completedAt, jobID,
	)
	if pgErr != nil {
		s.Logger.Error("update job record", "job_id", jobID, "error", pgErr)
	}

	// Dual-write logs to PG + ClickHouse.
	if err == nil && result.Logs != "" {
		s.writeLogChunks(ctx, jobID, result.Logs, startedAt)
	}

	// Write job telemetry to ClickHouse.
	s.writeJobEvent(ctx, SandboxJobEvent{
		JobID:       jobID.String(),
		OrgID:       orgID,
		UserID:      userID,
		RepoURL:     repoURL,
		RunCommand:  runCommand,
		Status:      status,
		ExitCode:    int32(exitCode),
		DurationMs:  durationMs,
		ZFSWritten:  zfsWritten,
		StdoutBytes: stdoutBytes,
		StderrBytes: stderrBytes,
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		CreatedAt:   startedAt,
	})
}

// writeLogChunks splits the complete log output into chunks and dual-writes to PG and ClickHouse.
func (s *Service) writeLogChunks(ctx context.Context, jobID uuid.UUID, logs string, createdAt time.Time) {
	const chunkSize = 8192
	seq := 0

	for start := 0; start < len(logs); start += chunkSize {
		end := start + chunkSize
		if end > len(logs) {
			end = len(logs)
		}
		chunk := logs[start:end]

		// PG write (feeds ElectricSQL → TanStack DB in browser).
		_, pgErr := s.PG.ExecContext(ctx,
			`INSERT INTO job_logs (job_id, seq, stream, chunk, created_at) VALUES ($1, $2, $3, $4, $5)`,
			jobID, seq, "stdout", []byte(chunk), createdAt,
		)
		if pgErr != nil {
			s.Logger.Error("write log chunk to PG", "job_id", jobID, "seq", seq, "error", pgErr)
		}

		// ClickHouse write (feeds observability dashboards).
		chErr := s.writeLogChunkCH(ctx, SandboxJobLog{
			JobID:     jobID.String(),
			Seq:       uint32(seq),
			Stream:    "stdout",
			Chunk:     chunk,
			CreatedAt: createdAt,
		})
		if chErr != nil {
			s.Logger.Error("write log chunk to ClickHouse", "job_id", jobID, "seq", seq, "error", chErr)
		}

		seq++
	}
}

func (s *Service) writeLogChunkCH(ctx context.Context, row SandboxJobLog) error {
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".sandbox_job_logs")
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("append row: %w", err)
	}
	return batch.Send()
}

func (s *Service) writeJobEvent(ctx context.Context, event SandboxJobEvent) {
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".sandbox_job_events")
	if err != nil {
		s.Logger.Error("prepare job event batch", "error", err)
		return
	}
	if err := batch.AppendStruct(&event); err != nil {
		s.Logger.Error("append job event", "error", err)
		return
	}
	if err := batch.Send(); err != nil {
		s.Logger.Error("send job event batch", "error", err)
	}
}

// GetJob retrieves a job record by ID.
func (s *Service) GetJob(ctx context.Context, jobID uuid.UUID) (*JobRecord, error) {
	row := s.PG.QueryRowContext(ctx,
		`SELECT id, org_id, user_id, repo_url, run_command, status, exit_code, duration_ms, zfs_written, started_at, completed_at, created_at
		 FROM jobs WHERE id = $1`, jobID,
	)
	var j JobRecord
	var runCommand, startedAt, completedAt sql.NullString
	var exitCode, durationMs, zfsWritten sql.NullInt64
	err := row.Scan(&j.ID, &j.OrgID, &j.UserID, &j.RepoURL, &runCommand, &j.Status,
		&exitCode, &durationMs, &zfsWritten, &startedAt, &completedAt, &j.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan job: %w", err)
	}
	j.RunCommand = runCommand.String
	j.ExitCode = int(exitCode.Int64)
	j.DurationMs = durationMs.Int64
	j.ZFSWritten = zfsWritten.Int64
	if startedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, startedAt.String)
		j.StartedAt = &t
	}
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, completedAt.String)
		j.CompletedAt = &t
	}
	return &j, nil
}

// GetJobLogs retrieves log chunks for a job.
func (s *Service) GetJobLogs(ctx context.Context, jobID uuid.UUID) (string, error) {
	rows, err := s.PG.QueryContext(ctx,
		`SELECT chunk FROM job_logs WHERE job_id = $1 ORDER BY seq`, jobID,
	)
	if err != nil {
		return "", fmt.Errorf("query job logs: %w", err)
	}
	defer rows.Close()

	var buf strings.Builder
	for rows.Next() {
		var chunk []byte
		if err := rows.Scan(&chunk); err != nil {
			return "", fmt.Errorf("scan log chunk: %w", err)
		}
		buf.Write(chunk)
	}
	return buf.String(), rows.Err()
}

func (s *Service) nextBillingJobID(ctx context.Context) (int64, error) {
	var id int64
	if err := s.PG.QueryRowContext(ctx, `SELECT nextval('job_billing_id_seq')`).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Service) countRunningJobs(ctx context.Context, orgID uint64) (int64, error) {
	var count int64
	if err := s.PG.QueryRowContext(ctx, `
		SELECT count(*)
		FROM jobs
		WHERE org_id = $1
		  AND status = 'running'
	`, orgID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// JobRecord is the PG representation of a job.
type JobRecord struct {
	ID          uuid.UUID  `json:"id"`
	OrgID       uint64     `json:"org_id"`
	UserID      string     `json:"user_id"`
	RepoURL     string     `json:"repo_url"`
	RunCommand  string     `json:"run_command,omitempty"`
	Status      string     `json:"status"`
	ExitCode    int        `json:"exit_code,omitempty"`
	DurationMs  int64      `json:"duration_ms,omitempty"`
	ZFSWritten  int64      `json:"zfs_written,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
