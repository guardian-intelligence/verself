package jobs

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/verself/sandbox-rental-service/internal/store"
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

	rows, err := s.storeQueries().ListRuns(ctx, store.ListRunsParams{
		OrgID:             dbOrgID(orgID),
		SourceKind:        strings.TrimSpace(filters.SourceKind),
		Status:            strings.TrimSpace(filters.Status),
		Repository:        strings.TrimSpace(filters.Repository),
		Workflow:          strings.TrimSpace(filters.Workflow),
		Branch:            strings.TrimSpace(filters.Branch),
		RunnerClass:       strings.TrimSpace(filters.RunnerClass),
		CursorEnabled:     cursorEnabled,
		CursorUpdatedAt:   pgTime(cursor.UpdatedAt),
		CursorExecutionID: cursor.ExecutionID,
		LimitCount:        int32(limit + 1),
	})
	if err != nil {
		return RunPage{}, fmt.Errorf("list runs: %w", err)
	}

	runs := make([]ExecutionRecord, 0, limit)
	for _, row := range rows {
		record := executionRecordFromListRunRow(row)
		runs = append(runs, record)
	}

	nextCursor := ""
	if len(runs) > limit {
		last := runs[limit-1]
		nextCursor = makeRunCursor(last.UpdatedAt, last.ExecutionID)
		runs = runs[:limit]
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
	row, err := s.storeQueries().GetRun(ctx, store.GetRunParams{
		OrgID:       dbOrgID(orgID),
		ExecutionID: executionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrExecutionMissing
		}
		return nil, fmt.Errorf("get run: %w", err)
	}
	record := executionRecordFromGetRunRow(row)
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

func executionRecordFromListRunRow(row store.ListRunsRow) ExecutionRecord {
	record := ExecutionRecord{
		RunID:            row.ExecutionID,
		ExecutionID:      row.ExecutionID,
		OrgID:            orgIDFromDB(row.OrgID),
		ActorID:          row.ActorID,
		Kind:             row.Kind,
		SourceKind:       row.SourceKind,
		WorkloadKind:     row.WorkloadKind,
		SourceRef:        row.SourceRef,
		RunnerClass:      row.RunnerClass,
		ExternalProvider: row.ExternalProvider,
		ExternalTaskID:   row.ExternalTaskID,
		Provider:         row.Provider,
		ProductID:        row.ProductID,
		Status:           row.State,
		CorrelationID:    row.CorrelationID,
		IdempotencyKey:   row.IdempotencyKey,
		RunCommand:       row.RunCommand,
		CreatedAt:        timeFromPG(row.CreatedAt),
		UpdatedAt:        timeFromPG(row.UpdatedAt),
		LatestAttempt:    attemptRecordFromRunFields(row.AttemptID, row.AttemptSeq, row.AttemptState, row.LeaseID, row.ExecID, row.BillingJobID, row.FailureReason, row.ExitCode, row.DurationMs, row.ZfsWritten, row.StdoutBytes, row.StderrBytes, row.RootfsProvisionedBytes, row.BootTimeUs, row.BlockReadBytes, row.BlockWriteBytes, row.NetRxBytes, row.NetTxBytes, row.VcpuExitCount, row.TraceID, row.StartedAt, row.CompletedAt, row.AttemptCreatedAt, row.AttemptUpdatedAt),
		Runner: RunnerRunMetadata{
			ProviderInstallationID: row.ProviderInstallationID,
			ProviderRunID:          row.ProviderRunID,
			ProviderJobID:          row.ProviderJobID,
			RepositoryFullName:     row.RepositoryFullName,
			WorkflowName:           row.WorkflowName,
			JobName:                row.JobName,
			HeadBranch:             row.HeadBranch,
			HeadSHA:                row.HeadSha,
		},
		Schedule: ScheduleRunMetadata{
			ScheduleID:         row.ScheduleID,
			DisplayName:        row.ScheduleDisplayName,
			TemporalWorkflowID: row.TemporalWorkflowID,
			TemporalRunID:      row.TemporalRunID,
		},
	}
	return record
}

func executionRecordFromGetRunRow(row store.GetRunRow) ExecutionRecord {
	record := ExecutionRecord{
		RunID:            row.ExecutionID,
		ExecutionID:      row.ExecutionID,
		OrgID:            orgIDFromDB(row.OrgID),
		ActorID:          row.ActorID,
		Kind:             row.Kind,
		SourceKind:       row.SourceKind,
		WorkloadKind:     row.WorkloadKind,
		SourceRef:        row.SourceRef,
		RunnerClass:      row.RunnerClass,
		ExternalProvider: row.ExternalProvider,
		ExternalTaskID:   row.ExternalTaskID,
		Provider:         row.Provider,
		ProductID:        row.ProductID,
		Status:           row.State,
		CorrelationID:    row.CorrelationID,
		IdempotencyKey:   row.IdempotencyKey,
		RunCommand:       row.RunCommand,
		CreatedAt:        timeFromPG(row.CreatedAt),
		UpdatedAt:        timeFromPG(row.UpdatedAt),
		LatestAttempt:    attemptRecordFromRunFields(row.AttemptID, row.AttemptSeq, row.AttemptState, row.LeaseID, row.ExecID, row.BillingJobID, row.FailureReason, row.ExitCode, row.DurationMs, row.ZfsWritten, row.StdoutBytes, row.StderrBytes, row.RootfsProvisionedBytes, row.BootTimeUs, row.BlockReadBytes, row.BlockWriteBytes, row.NetRxBytes, row.NetTxBytes, row.VcpuExitCount, row.TraceID, row.StartedAt, row.CompletedAt, row.AttemptCreatedAt, row.AttemptUpdatedAt),
		Runner: RunnerRunMetadata{
			ProviderInstallationID: row.ProviderInstallationID,
			ProviderRunID:          row.ProviderRunID,
			ProviderJobID:          row.ProviderJobID,
			RepositoryFullName:     row.RepositoryFullName,
			WorkflowName:           row.WorkflowName,
			JobName:                row.JobName,
			HeadBranch:             row.HeadBranch,
			HeadSHA:                row.HeadSha,
		},
		Schedule: ScheduleRunMetadata{
			ScheduleID:         row.ScheduleID,
			DisplayName:        row.ScheduleDisplayName,
			TemporalWorkflowID: row.TemporalWorkflowID,
			TemporalRunID:      row.TemporalRunID,
		},
	}
	return record
}

func attemptRecordFromRunFields(
	attemptID uuid.UUID,
	attemptSeq int32,
	state string,
	leaseID string,
	execID string,
	billingJobID int64,
	failureReason string,
	exitCode int32,
	durationMs int64,
	zfsWritten int64,
	stdoutBytes int64,
	stderrBytes int64,
	rootfsProvisionedBytes int64,
	bootTimeUs int64,
	blockReadBytes int64,
	blockWriteBytes int64,
	netRXBytes int64,
	netTXBytes int64,
	vcpuExitCount int64,
	traceID string,
	startedAt pgtype.Timestamptz,
	completedAt pgtype.Timestamptz,
	createdAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
) AttemptRecord {
	return AttemptRecord{
		AttemptID:              attemptID,
		AttemptSeq:             int(attemptSeq),
		State:                  state,
		LeaseID:                leaseID,
		ExecID:                 execID,
		BillingJobID:           billingJobID,
		FailureReason:          failureReason,
		ExitCode:               int(exitCode),
		DurationMs:             durationMs,
		ZFSWritten:             zfsWritten,
		StdoutBytes:            stdoutBytes,
		StderrBytes:            stderrBytes,
		RootfsProvisionedBytes: rootfsProvisionedBytes,
		BootTimeUs:             bootTimeUs,
		BlockReadBytes:         blockReadBytes,
		BlockWriteBytes:        blockWriteBytes,
		NetRXBytes:             netRXBytes,
		NetTXBytes:             netTXBytes,
		VCPUExitCount:          vcpuExitCount,
		TraceID:                traceID,
		StartedAt:              timePtrFromPG(startedAt),
		CompletedAt:            timePtrFromPG(completedAt),
		CreatedAt:              timeFromPG(createdAt),
		UpdatedAt:              timeFromPG(updatedAt),
	}
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
	rows, err := s.storeQueries().ListRunBillingSummaries(ctx, store.ListRunBillingSummariesParams{AttemptIds: attemptIDs})
	if err != nil {
		return nil, fmt.Errorf("list run billing summaries: %w", err)
	}
	summaries := map[uuid.UUID]RunBillingSummary{}
	for _, row := range rows {
		summaries[row.AttemptID] = RunBillingSummary{
			WindowCount:         int(row.WindowCount),
			ReservedChargeUnits: uint64FromInt64(row.ReservedChargeUnits, "reserved charge units"),
			BilledChargeUnits:   uint64FromInt64(row.BilledChargeUnits, "billed charge units"),
			WriteoffChargeUnits: uint64FromInt64(row.WriteoffChargeUnits, "writeoff charge units"),
			CostPerUnit:         uint64FromInt64(row.CostPerUnit, "cost per unit"),
			PricingPhase:        row.PricingPhase,
		}
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
	rows, err := s.storeQueries().ListStickyDiskMountsForAttempts(ctx, store.ListStickyDiskMountsForAttemptsParams{AttemptIds: attemptIDs})
	if err != nil {
		return nil, fmt.Errorf("list sticky disk mounts: %w", err)
	}
	out := map[uuid.UUID][]StickyDiskMountRecord{}
	for _, row := range rows {
		out[row.AttemptID] = append(out[row.AttemptID], StickyDiskMountRecord{
			MountID:             row.MountID,
			MountName:           row.MountName,
			KeyHash:             row.KeyHash,
			MountPath:           row.MountPath,
			BaseGeneration:      row.BaseGeneration,
			CommittedGeneration: row.CommittedGeneration,
			SaveRequested:       row.SaveRequested,
			SaveState:           row.SaveState,
			FailureReason:       row.FailureReason,
			RequestedAt:         timePtrFromPG(row.RequestedAt),
			CompletedAt:         timePtrFromPG(row.CompletedAt),
		})
	}
	return out, nil
}

func traceOrgID(orgID uint64) attribute.KeyValue {
	return attribute.String("verself.org_id", strconv.FormatUint(orgID, 10))
}

func traceInt(key string, value int) attribute.KeyValue {
	return attribute.Int(key, value)
}
