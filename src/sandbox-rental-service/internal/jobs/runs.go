package jobs

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
)

var ErrRunCursorInvalid = errors.New("sandbox-rental: run cursor invalid")

type RunListFilters struct {
	Limit       int
	Cursor      string
	SourceKind  string
	Status      string
	Repository  string
	Workflow    string
	Branch      string
	RunnerClass string
}

type RunPage struct {
	Runs       []ExecutionRecord
	NextCursor string
	Limit      int
}

type RunBillingSummary struct {
	WindowCount         int
	ReservedChargeUnits uint64
	BilledChargeUnits   uint64
	WriteoffChargeUnits uint64
	CostPerUnit         uint64
	PricingPhase        string
}

type RunnerRunMetadata struct {
	ProviderInstallationID int64
	ProviderRunID          int64
	ProviderJobID          int64
	RepositoryFullName     string
	WorkflowName           string
	JobName                string
	HeadBranch             string
	HeadSHA                string
}

type ScheduleRunMetadata struct {
	ScheduleID         uuid.UUID
	DisplayName        string
	TemporalWorkflowID string
	TemporalRunID      string
}

type StickyDiskMountRecord struct {
	MountID             uuid.UUID
	MountName           string
	KeyHash             string
	MountPath           string
	BaseGeneration      int64
	CommittedGeneration int64
	SaveRequested       bool
	SaveState           string
	FailureReason       string
	RequestedAt         *time.Time
	CompletedAt         *time.Time
}

type runCursor struct {
	UpdatedAt   time.Time
	ExecutionID uuid.UUID
}

func makeRunCursor(updatedAt time.Time, executionID uuid.UUID) string {
	return updatedAt.UTC().Format(time.RFC3339Nano) + "." + executionID.String()
}

func parseRunCursor(value string) (runCursor, error) {
	sep := strings.LastIndex(value, ".")
	if sep <= 0 || sep == len(value)-1 {
		return runCursor{}, ErrRunCursorInvalid
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, value[:sep])
	if err != nil {
		return runCursor{}, ErrRunCursorInvalid
	}
	executionID, err := uuid.Parse(value[sep+1:])
	if err != nil {
		return runCursor{}, ErrRunCursorInvalid
	}
	if updatedAt.IsZero() {
		return runCursor{}, ErrRunCursorInvalid
	}
	return runCursor{UpdatedAt: updatedAt.UTC(), ExecutionID: executionID}, nil
}

func (s *Service) ListRuns(ctx context.Context, orgID uint64, filters RunListFilters) (RunPage, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.runs.list")
	defer span.End()

	limit := filters.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	cursor := runCursor{UpdatedAt: time.Unix(0, 0).UTC(), ExecutionID: uuid.Nil}
	cursorEnabled := false
	if strings.TrimSpace(filters.Cursor) != "" {
		parsed, err := parseRunCursor(filters.Cursor)
		if err != nil {
			return RunPage{}, err
		}
		cursor = parsed
		cursorEnabled = true
	}

	rows, err := s.PGX.Query(ctx, `
		SELECT
			e.execution_id, e.org_id, e.actor_id, e.kind, e.source_kind, e.workload_kind, e.source_ref,
			e.runner_class, e.external_provider, e.external_task_id, e.provider, e.product_id, e.state,
			e.correlation_id, e.idempotency_key, e.run_command, e.created_at, e.updated_at,
			a.attempt_id, a.attempt_seq, a.state, COALESCE(a.lease_id,''), COALESCE(a.exec_id,''), COALESCE(a.billing_job_id, 0),
			a.failure_reason, a.exit_code, a.duration_ms, a.zfs_written, a.stdout_bytes, a.stderr_bytes,
			a.rootfs_provisioned_bytes, a.boot_time_us, a.block_read_bytes, a.block_write_bytes, a.net_rx_bytes, a.net_tx_bytes,
			a.vcpu_exit_count, a.trace_id, a.started_at, a.completed_at, a.created_at, a.updated_at,
			COALESCE(rr.provider_installation_id, 0), COALESCE(rr.provider_run_id, 0), COALESCE(rr.provider_job_id, 0),
			COALESCE(rr.repository_full_name, ''), COALESCE(rr.workflow_name, ''), COALESCE(rr.job_name, ''),
			COALESCE(rr.head_branch, ''), COALESCE(rr.head_sha, ''),
			COALESCE(sc.schedule_id, '00000000-0000-0000-0000-000000000000'::uuid), COALESCE(sc.display_name, ''),
			COALESCE(sc.temporal_workflow_id, ''), COALESCE(sc.temporal_run_id, '')
		FROM executions e
		JOIN LATERAL (
			SELECT attempt_id, attempt_seq, state, lease_id, exec_id, billing_job_id, failure_reason, exit_code,
			       duration_ms, zfs_written, stdout_bytes, stderr_bytes, rootfs_provisioned_bytes, boot_time_us,
			       block_read_bytes, block_write_bytes, net_rx_bytes, net_tx_bytes, vcpu_exit_count,
			       trace_id, started_at, completed_at, created_at, updated_at
			FROM execution_attempts
			WHERE execution_id = e.execution_id
			ORDER BY attempt_seq DESC
			LIMIT 1
		) a ON true
		LEFT JOIN LATERAL (
			SELECT
				j.provider_installation_id AS provider_installation_id,
				j.provider_run_id AS provider_run_id,
				j.provider_job_id AS provider_job_id,
				j.repository_full_name,
				j.workflow_name,
				j.job_name,
				j.head_branch,
				j.head_sha
			FROM runner_allocations ga
			LEFT JOIN runner_job_bindings gb ON gb.allocation_id = ga.allocation_id
			LEFT JOIN runner_jobs j ON j.provider = ga.provider AND j.provider_job_id = COALESCE(gb.provider_job_id, ga.requested_for_provider_job_id)
			WHERE ga.execution_id = e.execution_id
			ORDER BY j.updated_at DESC, j.provider_job_id DESC
			LIMIT 1
		) rr ON true
		LEFT JOIN LATERAL (
			SELECT
				d.schedule_id,
				s.display_name,
				d.temporal_workflow_id,
				d.temporal_run_id
			FROM execution_schedule_dispatches d
			JOIN execution_schedules s ON s.schedule_id = d.schedule_id
			WHERE d.source_workflow_run_id = e.execution_id
			ORDER BY d.created_at DESC, d.dispatch_id DESC
			LIMIT 1
		) sc ON true
		WHERE e.org_id = $1
		  AND ($2 = '' OR e.source_kind = $2)
		  AND ($3 = '' OR e.state = $3)
		  AND ($4 = '' OR COALESCE(rr.repository_full_name, '') = $4)
		  AND ($5 = '' OR COALESCE(rr.workflow_name, '') = $5)
		  AND ($6 = '' OR COALESCE(rr.head_branch, '') = $6)
		  AND ($7 = '' OR e.runner_class = $7)
		  AND ($8 = false OR (e.updated_at, e.execution_id) < ($9, $10))
		ORDER BY e.updated_at DESC, e.execution_id DESC
		LIMIT $11
	`, orgID, strings.TrimSpace(filters.SourceKind), strings.TrimSpace(filters.Status), strings.TrimSpace(filters.Repository),
		strings.TrimSpace(filters.Workflow), strings.TrimSpace(filters.Branch), strings.TrimSpace(filters.RunnerClass),
		cursorEnabled, cursor.UpdatedAt, cursor.ExecutionID, limit+1)
	if err != nil {
		return RunPage{}, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	runs := make([]ExecutionRecord, 0, limit)
	attemptIDs := make([]uuid.UUID, 0, limit)
	for rows.Next() {
		record, err := scanExecutionRecord(rows)
		if err != nil {
			return RunPage{}, err
		}
		runs = append(runs, record)
		attemptIDs = append(attemptIDs, record.LatestAttempt.AttemptID)
	}
	if err := rows.Err(); err != nil {
		return RunPage{}, fmt.Errorf("iterate runs: %w", err)
	}

	nextCursor := ""
	if len(runs) > limit {
		last := runs[limit-1]
		nextCursor = makeRunCursor(last.UpdatedAt, last.ExecutionID)
		runs = runs[:limit]
		attemptIDs = attemptIDs[:limit]
	}
	runs, err = s.attachRunBillingSummaries(ctx, runs)
	if err != nil {
		return RunPage{}, err
	}
	span.SetAttributes(traceOrgID(orgID), traceInt("sandbox.run_count", len(runs)))
	return RunPage{Runs: runs, NextCursor: nextCursor, Limit: limit}, nil
}

func (s *Service) GetRun(ctx context.Context, orgID uint64, executionID uuid.UUID) (*ExecutionRecord, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.runs.get")
	defer span.End()
	record, err := s.loadRun(ctx, orgID, executionID, true, true)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(traceOrgID(orgID), attribute.String("execution.id", executionID.String()))
	return record, nil
}

func (s *Service) loadRun(ctx context.Context, orgID uint64, executionID uuid.UUID, includeWindows, includeSticky bool) (*ExecutionRecord, error) {
	rows, err := s.PGX.Query(ctx, `
		SELECT
			e.execution_id, e.org_id, e.actor_id, e.kind, e.source_kind, e.workload_kind, e.source_ref,
			e.runner_class, e.external_provider, e.external_task_id, e.provider, e.product_id, e.state,
			e.correlation_id, e.idempotency_key, e.run_command, e.created_at, e.updated_at,
			a.attempt_id, a.attempt_seq, a.state, COALESCE(a.lease_id,''), COALESCE(a.exec_id,''), COALESCE(a.billing_job_id, 0),
			a.failure_reason, a.exit_code, a.duration_ms, a.zfs_written, a.stdout_bytes, a.stderr_bytes,
			a.rootfs_provisioned_bytes, a.boot_time_us, a.block_read_bytes, a.block_write_bytes, a.net_rx_bytes, a.net_tx_bytes,
			a.vcpu_exit_count, a.trace_id, a.started_at, a.completed_at, a.created_at, a.updated_at,
			COALESCE(rr.provider_installation_id, 0), COALESCE(rr.provider_run_id, 0), COALESCE(rr.provider_job_id, 0),
			COALESCE(rr.repository_full_name, ''), COALESCE(rr.workflow_name, ''), COALESCE(rr.job_name, ''),
			COALESCE(rr.head_branch, ''), COALESCE(rr.head_sha, ''),
			COALESCE(sc.schedule_id, '00000000-0000-0000-0000-000000000000'::uuid), COALESCE(sc.display_name, ''),
			COALESCE(sc.temporal_workflow_id, ''), COALESCE(sc.temporal_run_id, '')
		FROM executions e
		JOIN LATERAL (
			SELECT attempt_id, attempt_seq, state, lease_id, exec_id, billing_job_id, failure_reason, exit_code,
			       duration_ms, zfs_written, stdout_bytes, stderr_bytes, rootfs_provisioned_bytes, boot_time_us,
			       block_read_bytes, block_write_bytes, net_rx_bytes, net_tx_bytes, vcpu_exit_count,
			       trace_id, started_at, completed_at, created_at, updated_at
			FROM execution_attempts
			WHERE execution_id = e.execution_id
			ORDER BY attempt_seq DESC
			LIMIT 1
		) a ON true
		LEFT JOIN LATERAL (
			SELECT
				j.provider_installation_id AS provider_installation_id,
				j.provider_run_id AS provider_run_id,
				j.provider_job_id AS provider_job_id,
				j.repository_full_name,
				j.workflow_name,
				j.job_name,
				j.head_branch,
				j.head_sha
			FROM runner_allocations ga
			LEFT JOIN runner_job_bindings gb ON gb.allocation_id = ga.allocation_id
			LEFT JOIN runner_jobs j ON j.provider = ga.provider AND j.provider_job_id = COALESCE(gb.provider_job_id, ga.requested_for_provider_job_id)
			WHERE ga.execution_id = e.execution_id
			ORDER BY j.updated_at DESC, j.provider_job_id DESC
			LIMIT 1
		) rr ON true
		LEFT JOIN LATERAL (
			SELECT
				d.schedule_id,
				s.display_name,
				d.temporal_workflow_id,
				d.temporal_run_id
			FROM execution_schedule_dispatches d
			JOIN execution_schedules s ON s.schedule_id = d.schedule_id
			WHERE d.source_workflow_run_id = e.execution_id
			ORDER BY d.created_at DESC, d.dispatch_id DESC
			LIMIT 1
		) sc ON true
		WHERE e.org_id = $1
		  AND e.execution_id = $2
	`, orgID, executionID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrExecutionMissing
	}
	record, err := scanExecutionRecord(rows)
	if err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate get run: %w", err)
	}
	runs, err := s.attachRunBillingSummaries(ctx, []ExecutionRecord{record})
	if err != nil {
		return nil, err
	}
	record = runs[0]
	if includeWindows {
		windows, err := s.listBillingWindows(ctx, record.LatestAttempt.AttemptID)
		if err != nil {
			return nil, err
		}
		record.BillingWindows = windows
	}
	if includeSticky {
		sticky, err := s.listStickyDiskMountsForAttempts(ctx, []uuid.UUID{record.LatestAttempt.AttemptID})
		if err != nil {
			return nil, err
		}
		record.StickyDiskMounts = sticky[record.LatestAttempt.AttemptID]
	}
	return &record, nil
}

func scanExecutionRecord(rows interface {
	Scan(dest ...any) error
},
) (ExecutionRecord, error) {
	var record ExecutionRecord
	record.RunID = record.ExecutionID
	var attempt AttemptRecord
	if err := rows.Scan(
		&record.ExecutionID, &record.OrgID, &record.ActorID, &record.Kind, &record.SourceKind, &record.WorkloadKind, &record.SourceRef,
		&record.RunnerClass, &record.ExternalProvider, &record.ExternalTaskID, &record.Provider, &record.ProductID, &record.Status,
		&record.CorrelationID, &record.IdempotencyKey, &record.RunCommand, &record.CreatedAt, &record.UpdatedAt,
		&attempt.AttemptID, &attempt.AttemptSeq, &attempt.State, &attempt.LeaseID, &attempt.ExecID, &attempt.BillingJobID,
		&attempt.FailureReason, &attempt.ExitCode, &attempt.DurationMs, &attempt.ZFSWritten, &attempt.StdoutBytes, &attempt.StderrBytes,
		&attempt.RootfsProvisionedBytes, &attempt.BootTimeUs, &attempt.BlockReadBytes, &attempt.BlockWriteBytes, &attempt.NetRXBytes, &attempt.NetTXBytes,
		&attempt.VCPUExitCount, &attempt.TraceID, &attempt.StartedAt, &attempt.CompletedAt, &attempt.CreatedAt, &attempt.UpdatedAt,
		&record.Runner.ProviderInstallationID, &record.Runner.ProviderRunID, &record.Runner.ProviderJobID, &record.Runner.RepositoryFullName, &record.Runner.WorkflowName,
		&record.Runner.JobName, &record.Runner.HeadBranch, &record.Runner.HeadSHA,
		&record.Schedule.ScheduleID, &record.Schedule.DisplayName, &record.Schedule.TemporalWorkflowID, &record.Schedule.TemporalRunID,
	); err != nil {
		return ExecutionRecord{}, fmt.Errorf("scan run: %w", err)
	}
	record.RunID = record.ExecutionID
	record.LatestAttempt = attempt
	return record, nil
}

func (s *Service) attachRunBillingSummaries(ctx context.Context, runs []ExecutionRecord) ([]ExecutionRecord, error) {
	if len(runs) == 0 {
		return runs, nil
	}
	attemptIDs := make([]uuid.UUID, 0, len(runs))
	for _, run := range runs {
		if run.LatestAttempt.AttemptID != uuid.Nil {
			attemptIDs = append(attemptIDs, run.LatestAttempt.AttemptID)
		}
	}
	if len(attemptIDs) == 0 {
		return runs, nil
	}
	rows, err := s.PGX.Query(ctx, `
		SELECT
			attempt_id,
			count(*)::int,
			COALESCE(sum(reserved_charge_units), 0),
			COALESCE(sum(billed_charge_units), 0),
			COALESCE(sum(writeoff_charge_units), 0),
			COALESCE(max(cost_per_unit), 0),
			COALESCE(max(pricing_phase), '')
		FROM execution_billing_windows
		WHERE attempt_id = ANY($1)
		GROUP BY attempt_id
	`, attemptIDs)
	if err != nil {
		return nil, fmt.Errorf("list run billing summaries: %w", err)
	}
	defer rows.Close()
	summaries := map[uuid.UUID]RunBillingSummary{}
	for rows.Next() {
		var attemptID uuid.UUID
		var summary RunBillingSummary
		if err := rows.Scan(&attemptID, &summary.WindowCount, &summary.ReservedChargeUnits, &summary.BilledChargeUnits, &summary.WriteoffChargeUnits, &summary.CostPerUnit, &summary.PricingPhase); err != nil {
			return nil, fmt.Errorf("scan run billing summary: %w", err)
		}
		summaries[attemptID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run billing summaries: %w", err)
	}
	for idx := range runs {
		if summary, ok := summaries[runs[idx].LatestAttempt.AttemptID]; ok {
			runs[idx].BillingSummary = summary
		}
	}
	return runs, nil
}

func (s *Service) listStickyDiskMountsForAttempts(ctx context.Context, attemptIDs []uuid.UUID) (map[uuid.UUID][]StickyDiskMountRecord, error) {
	if len(attemptIDs) == 0 {
		return map[uuid.UUID][]StickyDiskMountRecord{}, nil
	}
	rows, err := s.PGX.Query(ctx, `
		SELECT
			attempt_id, mount_id, mount_name, key_hash, mount_path, base_generation, committed_generation,
			save_requested, save_state, failure_reason, requested_at, completed_at
		FROM execution_sticky_disk_mounts
		WHERE attempt_id = ANY($1)
		ORDER BY sort_order, mount_name
	`, attemptIDs)
	if err != nil {
		return nil, fmt.Errorf("list sticky disk mounts: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID][]StickyDiskMountRecord{}
	for rows.Next() {
		var attemptID uuid.UUID
		var record StickyDiskMountRecord
		if err := rows.Scan(&attemptID, &record.MountID, &record.MountName, &record.KeyHash, &record.MountPath, &record.BaseGeneration, &record.CommittedGeneration, &record.SaveRequested, &record.SaveState, &record.FailureReason, &record.RequestedAt, &record.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan sticky disk mount: %w", err)
		}
		out[attemptID] = append(out[attemptID], record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sticky disk mounts: %w", err)
	}
	return out, nil
}

func traceOrgID(orgID uint64) attribute.KeyValue {
	return attribute.String("verself.org_id", strconv.FormatUint(orgID, 10))
}

func traceInt(key string, value int) attribute.KeyValue {
	return attribute.Int(key, value)
}
