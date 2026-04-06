// Package jobs implements the sandbox job lifecycle: submit, run, bill, record.
package jobs

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	fastsandbox "github.com/forge-metal/fast-sandbox"
	"github.com/google/uuid"
)

// BillingClient calls the billing-service HTTP API for reservation lifecycle.
type BillingClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Logger     *slog.Logger
}

// ReserveRequest is the payload for POST /internal/billing/v1/reserve.
type ReserveRequest struct {
	OrgID     uint64 `json:"org_id"`
	ProductID string `json:"product_id"`
	ActorID   string `json:"actor_id"`
	SourceRef string `json:"source_ref"`
}

// ReserveResponse is the response from the reserve endpoint.
type ReserveResponse struct {
	ReservationID string `json:"reservation_id"`
}

// Reserve calls billing-service to reserve credits before work begins.
// Returns empty reservation ID and nil error if billing is unavailable (logged as warning).
func (b *BillingClient) Reserve(ctx context.Context, req ReserveRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal reserve request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL+"/internal/billing/v1/reserve", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create reserve request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := b.HTTPClient.Do(httpReq)
	if err != nil {
		b.Logger.Warn("billing reserve unavailable, proceeding without billing", "error", err)
		return "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPaymentRequired {
		return "", fmt.Errorf("insufficient balance")
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		b.Logger.Warn("billing reserve failed", "status", resp.StatusCode, "body", string(respBody))
		return "", nil
	}

	var reserveResp ReserveResponse
	if err := json.NewDecoder(resp.Body).Decode(&reserveResp); err != nil {
		b.Logger.Warn("billing reserve: decode response", "error", err)
		return "", nil
	}
	return reserveResp.ReservationID, nil
}

// Settle calls billing-service to settle actual usage after job completion.
func (b *BillingClient) Settle(ctx context.Context, reservationID string, durationMs int64) {
	if reservationID == "" {
		return
	}
	payload := map[string]any{"reservation_id": reservationID, "actual_seconds": uint32(durationMs / 1000)}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL+"/internal/billing/v1/settle", bytes.NewReader(body))
	if err != nil {
		b.Logger.Warn("billing settle: create request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.HTTPClient.Do(req)
	if err != nil {
		b.Logger.Warn("billing settle: request failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b.Logger.Warn("billing settle: unexpected status", "status", resp.StatusCode)
	}
}

// Void calls billing-service to release a reservation after failure.
func (b *BillingClient) Void(ctx context.Context, reservationID string) {
	if reservationID == "" {
		return
	}
	payload := map[string]any{"reservation_id": reservationID}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.BaseURL+"/internal/billing/v1/void", bytes.NewReader(body))
	if err != nil {
		b.Logger.Warn("billing void: create request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.HTTPClient.Do(req)
	if err != nil {
		b.Logger.Warn("billing void: request failed", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b.Logger.Warn("billing void: unexpected status", "status", resp.StatusCode)
	}
}

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
	PG           *sql.DB
	CH           driver.Conn
	CHDatabase   string
	Orchestrator *fastsandbox.Orchestrator
	Billing      *BillingClient
	Logger       *slog.Logger
}

// Submit creates a job record and starts asynchronous execution.
// It returns the job ID immediately; the caller polls for completion.
func (s *Service) Submit(ctx context.Context, orgID uint64, userID, repoURL, runCommand string) (uuid.UUID, error) {
	jobID := uuid.New()
	now := time.Now().UTC()

	// Reserve billing credits.
	reservationID, err := s.Billing.Reserve(ctx, ReserveRequest{
		OrgID:     orgID,
		ProductID: "sandbox",
		ActorID:   userID,
		SourceRef: jobID.String(),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("billing reserve: %w", err)
	}

	// Insert job record.
	_, err = s.PG.ExecContext(ctx,
		`INSERT INTO jobs (id, org_id, user_id, repo_url, run_command, status, billing_reservation_id, started_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, 'running', $6, $7, $7)`,
		jobID, orgID, userID, repoURL, nullString(runCommand), nullString(reservationID), now,
	)
	if err != nil {
		s.Billing.Void(ctx, reservationID)
		return uuid.Nil, fmt.Errorf("insert job: %w", err)
	}

	// Run in background goroutine.
	go s.execute(jobID, orgID, userID, repoURL, runCommand, reservationID)

	return jobID, nil
}

func (s *Service) execute(jobID uuid.UUID, orgID uint64, userID, repoURL, runCommand, reservationID string) {
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
		s.Billing.Void(ctx, reservationID)
	} else {
		exitCode = result.ExitCode
		if exitCode != 0 {
			status = "failed"
		}
		s.Billing.Settle(ctx, reservationID, durationMs)
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
