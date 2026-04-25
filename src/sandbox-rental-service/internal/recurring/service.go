package recurring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	tclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const (
	DefaultNamespace = "sandbox-rental-service"
	DefaultTaskQueue = "sandbox-rental-service.recurring-vm"

	WorkflowName = "ExecutionScheduleDispatchWorkflow"
	ActivityName = "ExecutionScheduleDispatchActivity"

	SourceKindExecutionSchedule = "execution_schedule"

	StateActive = "active"
	StatePaused = "paused"

	DispatchStatePending   = "pending"
	DispatchStateSubmitted = "submitted"
	DispatchStateFailed    = "failed"

	minIntervalSeconds = 15
	maxDispatches      = 10
)

var (
	ErrScheduleMissing = errors.New("sandbox-rental: execution schedule missing")
	ErrTemporalMissing = errors.New("sandbox-rental: recurring temporal client unavailable")
	ErrDispatcherNil   = errors.New("sandbox-rental: recurring workflow dispatcher unavailable")
	ErrInvalid         = errors.New("sandbox-rental: invalid execution schedule")
	ErrConflict        = errors.New("sandbox-rental: execution schedule conflict")
)

var tracer = otel.Tracer("sandbox-rental-service/recurring")

type WorkflowDispatcher interface {
	DispatchWorkflow(ctx context.Context, req WorkflowDispatchRequest) (WorkflowDispatchResult, error)
}

type WorkflowDispatchRequest struct {
	OrgID              uint64
	ActorID            string
	SourceRepositoryID uuid.UUID
	WorkflowPath       string
	Ref                string
	Inputs             map[string]string
	IdempotencyKey     string
}

type WorkflowDispatchResult struct {
	WorkflowRunID uuid.UUID
	State         string
}

type Config struct {
	PGX            *pgxpool.Pool
	TemporalClient tclient.Client
	Namespace      string
	TaskQueue      string
	Logger         *slog.Logger
	Dispatcher     WorkflowDispatcher
}

type Service struct {
	pgx            *pgxpool.Pool
	temporalClient tclient.Client
	namespace      string
	taskQueue      string
	logger         *slog.Logger
	dispatcher     WorkflowDispatcher
}

type CreateRequest struct {
	DisplayName        string
	IdempotencyKey     string
	SourceRepositoryID uuid.UUID
	WorkflowPath       string
	Ref                string
	Inputs             map[string]string
	IntervalSeconds    uint32
	Paused             bool
}

type ScheduleRecord struct {
	ScheduleID         uuid.UUID
	OrgID              uint64
	ActorID            string
	DisplayName        string
	IdempotencyKey     string
	TemporalScheduleID string
	TemporalNamespace  string
	TaskQueue          string
	State              string
	IntervalSeconds    uint32
	SourceRepositoryID uuid.UUID
	WorkflowPath       string
	Ref                string
	Inputs             map[string]string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Dispatches         []DispatchRecord
}

type DispatchRecord struct {
	DispatchID          uuid.UUID
	ScheduleID          uuid.UUID
	TemporalWorkflowID  string
	TemporalRunID       string
	SourceWorkflowRunID *uuid.UUID
	WorkflowState       string
	State               string
	FailureReason       string
	ScheduledAt         time.Time
	SubmittedAt         *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type WorkflowInput struct {
	ScheduleID         string
	OrgID              uint64
	ActorID            string
	SourceRepositoryID string
	WorkflowPath       string
	Ref                string
	Inputs             map[string]string
	TemporalScheduleID string
}

type DispatchInput struct {
	ScheduleID         string
	OrgID              uint64
	ActorID            string
	SourceRepositoryID string
	WorkflowPath       string
	Ref                string
	Inputs             map[string]string
	TemporalScheduleID string
	TemporalWorkflowID string
	TemporalRunID      string
	ScheduledAt        time.Time
}

type DispatchResult struct {
	DispatchID          string
	SourceWorkflowRunID string
	WorkflowState       string
	State               string
}

func NewService(cfg Config) (*Service, error) {
	if cfg.PGX == nil {
		return nil, errors.New("recurring service requires pgx pool")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	namespace := strings.TrimSpace(cfg.Namespace)
	if namespace == "" {
		namespace = DefaultNamespace
	}
	taskQueue := strings.TrimSpace(cfg.TaskQueue)
	if taskQueue == "" {
		taskQueue = DefaultTaskQueue
	}
	return &Service{
		pgx:            cfg.PGX,
		temporalClient: cfg.TemporalClient,
		namespace:      namespace,
		taskQueue:      taskQueue,
		logger:         logger,
		dispatcher:     cfg.Dispatcher,
	}, nil
}

func (s *Service) RegisterWorker(workerInstance worker.Worker) {
	workerInstance.RegisterWorkflowWithOptions(ExecutionScheduleDispatchWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	workerInstance.RegisterActivityWithOptions(s.DispatchExecutionActivity, activity.RegisterOptions{Name: ActivityName})
}

func (s *Service) CreateSchedule(ctx context.Context, orgID uint64, actorID string, req CreateRequest) (_ ScheduleRecord, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.execution_schedule.create")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if s.temporalClient == nil {
		return ScheduleRecord{}, ErrTemporalMissing
	}
	normalized, err := normalizeCreateRequest(req)
	if err != nil {
		return ScheduleRecord{}, err
	}
	if existing, err := s.loadScheduleByIdempotencyKey(ctx, orgID, normalized.IdempotencyKey); err == nil {
		if !scheduleMatchesCreateRequest(*existing, normalized) {
			return ScheduleRecord{}, fmt.Errorf("%w: idempotency key reused with different execution schedule", ErrConflict)
		}
		return *existing, nil
	} else if !errors.Is(err, ErrScheduleMissing) {
		return ScheduleRecord{}, err
	}

	scheduleID := uuid.New()
	temporalScheduleID := temporalScheduleIDFor(scheduleID)
	handle, err := s.temporalClient.ScheduleClient().Create(ctx, tclient.ScheduleOptions{
		ID: temporalScheduleID,
		Spec: tclient.ScheduleSpec{
			Intervals: []tclient.ScheduleIntervalSpec{{
				Every: time.Duration(normalized.IntervalSeconds) * time.Second,
			}},
		},
		Action: &tclient.ScheduleWorkflowAction{
			ID:       temporalScheduleID + "/dispatch",
			Workflow: WorkflowName,
			Args: []interface{}{WorkflowInput{
				ScheduleID:         scheduleID.String(),
				OrgID:              orgID,
				ActorID:            actorID,
				SourceRepositoryID: normalized.SourceRepositoryID.String(),
				WorkflowPath:       normalized.WorkflowPath,
				Ref:                normalized.Ref,
				Inputs:             normalized.Inputs,
				TemporalScheduleID: temporalScheduleID,
			}},
			TaskQueue: s.taskQueue,
		},
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		Paused:  normalized.Paused,
		Memo: map[string]interface{}{
			"schedule_id":          scheduleID.String(),
			"org_id":               fmt.Sprintf("%d", orgID),
			"temporal_schedule_id": temporalScheduleID,
			"display_name":         normalized.DisplayName,
		},
	})
	if err != nil {
		return ScheduleRecord{}, fmt.Errorf("create temporal schedule: %w", err)
	}

	now := time.Now().UTC()
	row := ScheduleRecord{
		ScheduleID:         scheduleID,
		OrgID:              orgID,
		ActorID:            actorID,
		DisplayName:        normalized.DisplayName,
		IdempotencyKey:     normalized.IdempotencyKey,
		TemporalScheduleID: temporalScheduleID,
		TemporalNamespace:  s.namespace,
		TaskQueue:          s.taskQueue,
		State:              stateForPaused(normalized.Paused),
		IntervalSeconds:    normalized.IntervalSeconds,
		SourceRepositoryID: normalized.SourceRepositoryID,
		WorkflowPath:       normalized.WorkflowPath,
		Ref:                normalized.Ref,
		Inputs:             normalized.Inputs,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.insertScheduleRow(ctx, row); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = handle.Delete(cleanupCtx)
		return ScheduleRecord{}, err
	}
	span.SetAttributes(
		attribute.String("sandbox.schedule_id", row.ScheduleID.String()),
		attribute.String("temporal.schedule_id", row.TemporalScheduleID),
		attribute.String("temporal.namespace", row.TemporalNamespace),
		attribute.String("temporal.task_queue", row.TaskQueue),
	)
	return row, nil
}

func (s *Service) ListSchedules(ctx context.Context, orgID uint64) (_ []ScheduleRecord, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.execution_schedule.list")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	rows, err := s.pgx.Query(ctx, `SELECT
		schedule_id, org_id, actor_id, display_name, idempotency_key,
		temporal_schedule_id, temporal_namespace, task_queue, state,
		interval_seconds, source_repository_id, workflow_path, ref, inputs_json, created_at, updated_at
		FROM execution_schedules
		WHERE org_id = $1
		ORDER BY created_at DESC, schedule_id DESC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list execution schedules: %w", err)
	}
	defer rows.Close()
	out := make([]ScheduleRecord, 0, 16)
	for rows.Next() {
		record, scanErr := scanScheduleRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		dispatches, dispatchErr := s.loadDispatches(ctx, record.ScheduleID)
		if dispatchErr != nil {
			return nil, dispatchErr
		}
		record.Dispatches = dispatches
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution schedules: %w", err)
	}
	return out, nil
}

func (s *Service) GetSchedule(ctx context.Context, orgID uint64, scheduleID uuid.UUID) (_ *ScheduleRecord, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.execution_schedule.read")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	record, err := s.loadSchedule(ctx, orgID, scheduleID)
	if err != nil {
		return nil, err
	}
	dispatches, err := s.loadDispatches(ctx, record.ScheduleID)
	if err != nil {
		return nil, err
	}
	record.Dispatches = dispatches
	return record, nil
}

func (s *Service) PauseSchedule(ctx context.Context, orgID uint64, scheduleID uuid.UUID) (_ *ScheduleRecord, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.execution_schedule.pause")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if s.temporalClient == nil {
		return nil, ErrTemporalMissing
	}
	record, err := s.loadSchedule(ctx, orgID, scheduleID)
	if err != nil {
		return nil, err
	}
	handle := s.temporalClient.ScheduleClient().GetHandle(ctx, record.TemporalScheduleID)
	if err := handle.Pause(ctx, tclient.SchedulePauseOptions{Note: "paused via sandbox-rental-service"}); err != nil {
		return nil, fmt.Errorf("pause temporal schedule: %w", err)
	}
	if err := s.updateScheduleState(ctx, record.ScheduleID, StatePaused); err != nil {
		return nil, err
	}
	record.State = StatePaused
	record.UpdatedAt = time.Now().UTC()
	return record, nil
}

func (s *Service) ResumeSchedule(ctx context.Context, orgID uint64, scheduleID uuid.UUID) (_ *ScheduleRecord, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.execution_schedule.resume")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if s.temporalClient == nil {
		return nil, ErrTemporalMissing
	}
	record, err := s.loadSchedule(ctx, orgID, scheduleID)
	if err != nil {
		return nil, err
	}
	handle := s.temporalClient.ScheduleClient().GetHandle(ctx, record.TemporalScheduleID)
	if err := handle.Unpause(ctx, tclient.ScheduleUnpauseOptions{Note: "resumed via sandbox-rental-service"}); err != nil {
		return nil, fmt.Errorf("resume temporal schedule: %w", err)
	}
	if err := s.updateScheduleState(ctx, record.ScheduleID, StateActive); err != nil {
		return nil, err
	}
	record.State = StateActive
	record.UpdatedAt = time.Now().UTC()
	return record, nil
}

func ExecutionScheduleDispatchWorkflow(ctx workflow.Context, input WorkflowInput) error {
	info := workflow.GetInfo(ctx)
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Second,
			MaximumAttempts:    3,
		},
	})
	return workflow.ExecuteActivity(activityCtx, ActivityName, DispatchInput{
		ScheduleID:         input.ScheduleID,
		OrgID:              input.OrgID,
		ActorID:            input.ActorID,
		SourceRepositoryID: input.SourceRepositoryID,
		WorkflowPath:       input.WorkflowPath,
		Ref:                input.Ref,
		Inputs:             input.Inputs,
		TemporalScheduleID: input.TemporalScheduleID,
		TemporalWorkflowID: info.WorkflowExecution.ID,
		TemporalRunID:      info.WorkflowExecution.RunID,
		ScheduledAt:        workflow.Now(ctx).UTC(),
	}).Get(activityCtx, nil)
}

func (s *Service) DispatchExecutionActivity(ctx context.Context, input DispatchInput) (_ DispatchResult, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.execution_schedule.dispatch.submit")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if s.dispatcher == nil {
		return DispatchResult{}, ErrDispatcherNil
	}
	scheduleID, err := uuid.Parse(strings.TrimSpace(input.ScheduleID))
	if err != nil {
		return DispatchResult{}, fmt.Errorf("parse schedule id: %w", err)
	}
	record, err := s.recordDispatchStart(ctx, scheduleID, input)
	if err != nil {
		return DispatchResult{}, err
	}
	if record.SourceWorkflowRunID != nil && record.State == DispatchStateSubmitted {
		return DispatchResult{
			DispatchID:          record.DispatchID.String(),
			SourceWorkflowRunID: record.SourceWorkflowRunID.String(),
			WorkflowState:       record.WorkflowState,
			State:               record.State,
		}, nil
	}

	idempotencyKey := dispatchIdempotencyKey(scheduleID, input.TemporalWorkflowID, input.TemporalRunID)
	sourceRepoID, err := uuid.Parse(strings.TrimSpace(input.SourceRepositoryID))
	if err != nil {
		_ = s.markDispatchFailed(context.Background(), record.DispatchID, err.Error())
		return DispatchResult{}, fmt.Errorf("parse source repository id: %w", err)
	}
	result, err := s.dispatcher.DispatchWorkflow(ctx, WorkflowDispatchRequest{
		OrgID:              input.OrgID,
		ActorID:            input.ActorID,
		SourceRepositoryID: sourceRepoID,
		WorkflowPath:       input.WorkflowPath,
		Ref:                input.Ref,
		Inputs:             input.Inputs,
		IdempotencyKey:     idempotencyKey,
	})
	if err != nil {
		_ = s.markDispatchFailed(context.Background(), record.DispatchID, err.Error())
		return DispatchResult{}, err
	}
	if err := s.markDispatchSubmitted(ctx, record.DispatchID, result.WorkflowRunID, result.State); err != nil {
		return DispatchResult{}, err
	}
	span.SetAttributes(
		attribute.String("sandbox.schedule_id", scheduleID.String()),
		attribute.String("sandbox.dispatch_id", record.DispatchID.String()),
		attribute.String("source.workflow_run_id", result.WorkflowRunID.String()),
		attribute.String("source.workflow_state", result.State),
		attribute.String("temporal.schedule_id", input.TemporalScheduleID),
		attribute.String("temporal.workflow_id", input.TemporalWorkflowID),
		attribute.String("temporal.run_id", input.TemporalRunID),
	)
	return DispatchResult{
		DispatchID:          record.DispatchID.String(),
		SourceWorkflowRunID: result.WorkflowRunID.String(),
		WorkflowState:       result.State,
		State:               DispatchStateSubmitted,
	}, nil
}

func (s *Service) insertScheduleRow(ctx context.Context, record ScheduleRecord) error {
	inputs, err := json.Marshal(record.Inputs)
	if err != nil {
		return fmt.Errorf("encode workflow schedule inputs: %w", err)
	}
	_, err = s.pgx.Exec(ctx, `INSERT INTO execution_schedules (
		schedule_id, org_id, actor_id, display_name, idempotency_key,
		temporal_schedule_id, temporal_namespace, task_queue, state,
		interval_seconds, source_repository_id, workflow_path, ref, inputs_json, created_at, updated_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		record.ScheduleID, record.OrgID, record.ActorID, record.DisplayName, record.IdempotencyKey,
		record.TemporalScheduleID, record.TemporalNamespace, record.TaskQueue, record.State,
		int(record.IntervalSeconds), record.SourceRepositoryID, record.WorkflowPath, record.Ref, inputs, record.CreatedAt, record.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert execution schedule: %w", err)
	}
	return nil
}

func (s *Service) loadSchedule(ctx context.Context, orgID uint64, scheduleID uuid.UUID) (*ScheduleRecord, error) {
	row := s.pgx.QueryRow(ctx, `SELECT
		schedule_id, org_id, actor_id, display_name, idempotency_key,
		temporal_schedule_id, temporal_namespace, task_queue, state,
		interval_seconds, source_repository_id, workflow_path, ref, inputs_json, created_at, updated_at
		FROM execution_schedules
		WHERE org_id = $1 AND schedule_id = $2`, orgID, scheduleID)
	record, err := scanScheduleRecord(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScheduleMissing
		}
		return nil, err
	}
	return &record, nil
}

func (s *Service) loadScheduleByIdempotencyKey(ctx context.Context, orgID uint64, idempotencyKey string) (*ScheduleRecord, error) {
	row := s.pgx.QueryRow(ctx, `SELECT
		schedule_id, org_id, actor_id, display_name, idempotency_key,
		temporal_schedule_id, temporal_namespace, task_queue, state,
		interval_seconds, source_repository_id, workflow_path, ref, inputs_json, created_at, updated_at
		FROM execution_schedules
		WHERE org_id = $1 AND idempotency_key = $2`, orgID, strings.TrimSpace(idempotencyKey))
	record, err := scanScheduleRecord(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScheduleMissing
		}
		return nil, err
	}
	return &record, nil
}

func (s *Service) updateScheduleState(ctx context.Context, scheduleID uuid.UUID, state string) error {
	commandTag, err := s.pgx.Exec(ctx, `UPDATE execution_schedules SET state = $1, updated_at = $2 WHERE schedule_id = $3`, state, time.Now().UTC(), scheduleID)
	if err != nil {
		return fmt.Errorf("update execution schedule state: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return ErrScheduleMissing
	}
	return nil
}

func (s *Service) loadDispatches(ctx context.Context, scheduleID uuid.UUID) ([]DispatchRecord, error) {
	rows, err := s.pgx.Query(ctx, `SELECT
		dispatch_id, schedule_id, temporal_workflow_id, temporal_run_id, source_workflow_run_id,
		workflow_state, state, failure_reason, scheduled_at, submitted_at, created_at, updated_at
		FROM execution_schedule_dispatches
		WHERE schedule_id = $1
		ORDER BY created_at DESC, dispatch_id DESC
		LIMIT $2`, scheduleID, maxDispatches)
	if err != nil {
		return nil, fmt.Errorf("list execution schedule dispatches: %w", err)
	}
	defer rows.Close()
	out := make([]DispatchRecord, 0, maxDispatches)
	for rows.Next() {
		record, scanErr := scanDispatchRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution schedule dispatches: %w", err)
	}
	return out, nil
}

func (s *Service) recordDispatchStart(ctx context.Context, scheduleID uuid.UUID, input DispatchInput) (_ DispatchRecord, err error) {
	now := time.Now().UTC()
	row := s.pgx.QueryRow(ctx, `INSERT INTO execution_schedule_dispatches (
		dispatch_id, schedule_id, temporal_workflow_id, temporal_run_id,
		state, failure_reason, scheduled_at, submitted_at, created_at, updated_at
	) VALUES ($1,$2,$3,$4,$5,'',$6,NULL,$7,$7)
	ON CONFLICT (schedule_id, temporal_workflow_id, temporal_run_id)
	DO UPDATE SET updated_at = EXCLUDED.updated_at
	RETURNING
		dispatch_id, schedule_id, temporal_workflow_id, temporal_run_id, source_workflow_run_id,
		workflow_state, state, failure_reason, scheduled_at, submitted_at, created_at, updated_at`,
		uuid.New(), scheduleID, strings.TrimSpace(input.TemporalWorkflowID), strings.TrimSpace(input.TemporalRunID),
		DispatchStatePending, input.ScheduledAt.UTC(), now)
	record, err := scanDispatchRecord(row)
	if err != nil {
		return DispatchRecord{}, err
	}
	return record, nil
}

func (s *Service) markDispatchSubmitted(ctx context.Context, dispatchID, sourceWorkflowRunID uuid.UUID, workflowState string) error {
	commandTag, err := s.pgx.Exec(ctx, `UPDATE execution_schedule_dispatches
		SET state = $1,
			failure_reason = '',
			source_workflow_run_id = $2,
			workflow_state = $3,
			submitted_at = $4,
			updated_at = $4
		WHERE dispatch_id = $5`,
		DispatchStateSubmitted, sourceWorkflowRunID, strings.TrimSpace(workflowState), time.Now().UTC(), dispatchID)
	if err != nil {
		return fmt.Errorf("mark dispatch submitted: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return ErrScheduleMissing
	}
	return nil
}

func (s *Service) markDispatchFailed(ctx context.Context, dispatchID uuid.UUID, reason string) error {
	_, err := s.pgx.Exec(ctx, `UPDATE execution_schedule_dispatches
		SET state = $1,
			failure_reason = $2,
			updated_at = $3
		WHERE dispatch_id = $4`,
		DispatchStateFailed, truncateReason(reason), time.Now().UTC(), dispatchID)
	if err != nil {
		return fmt.Errorf("mark dispatch failed: %w", err)
	}
	return nil
}

func normalizeCreateRequest(req CreateRequest) (CreateRequest, error) {
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.WorkflowPath = normalizeWorkflowPath(req.WorkflowPath)
	req.Ref = strings.TrimSpace(req.Ref)
	if req.Ref == "" {
		req.Ref = "main"
	}
	cleanInputs := make(map[string]string, len(req.Inputs))
	for rawKey, value := range req.Inputs {
		key := strings.TrimSpace(rawKey)
		if key == "" || rawKey != key || len(key) > 128 || len(value) > 4096 {
			return CreateRequest{}, fmt.Errorf("%w: workflow inputs must use non-empty trimmed keys no longer than 128 characters and values no longer than 4096 characters", ErrInvalid)
		}
		cleanInputs[key] = value
	}
	req.Inputs = cleanInputs
	if req.IdempotencyKey == "" {
		return CreateRequest{}, fmt.Errorf("%w: idempotency_key is required", ErrInvalid)
	}
	if req.SourceRepositoryID == uuid.Nil {
		return CreateRequest{}, fmt.Errorf("%w: source_repository_id is required", ErrInvalid)
	}
	if req.WorkflowPath == "" {
		return CreateRequest{}, fmt.Errorf("%w: workflow_path must be a .forgejo/workflows or .github/workflows YAML path", ErrInvalid)
	}
	if req.Ref == "" {
		return CreateRequest{}, fmt.Errorf("%w: ref is required", ErrInvalid)
	}
	if req.IntervalSeconds < minIntervalSeconds {
		return CreateRequest{}, fmt.Errorf("%w: interval_seconds must be at least %d", ErrInvalid, minIntervalSeconds)
	}
	if req.DisplayName == "" {
		req.DisplayName = req.WorkflowPath
	}
	return req, nil
}

func scheduleMatchesCreateRequest(record ScheduleRecord, req CreateRequest) bool {
	return record.DisplayName == req.DisplayName &&
		record.SourceRepositoryID == req.SourceRepositoryID &&
		record.WorkflowPath == req.WorkflowPath &&
		record.Ref == req.Ref &&
		record.IntervalSeconds == req.IntervalSeconds &&
		maps.Equal(record.Inputs, req.Inputs)
}

func normalizeWorkflowPath(value string) string {
	path := strings.Trim(strings.TrimSpace(value), "/")
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if path == "" || strings.Contains(path, "..") {
		return ""
	}
	if !strings.HasPrefix(path, ".forgejo/workflows/") && !strings.HasPrefix(path, ".github/workflows/") {
		return ""
	}
	if !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml") {
		return ""
	}
	return path
}

func scanScheduleRecord(scanner interface {
	Scan(dest ...any) error
},
) (ScheduleRecord, error) {
	var record ScheduleRecord
	var intervalSeconds int
	var inputsJSON []byte
	if err := scanner.Scan(
		&record.ScheduleID,
		&record.OrgID,
		&record.ActorID,
		&record.DisplayName,
		&record.IdempotencyKey,
		&record.TemporalScheduleID,
		&record.TemporalNamespace,
		&record.TaskQueue,
		&record.State,
		&intervalSeconds,
		&record.SourceRepositoryID,
		&record.WorkflowPath,
		&record.Ref,
		&inputsJSON,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return ScheduleRecord{}, err
	}
	record.IntervalSeconds = uint32(intervalSeconds)
	if len(inputsJSON) > 0 {
		if err := json.Unmarshal(inputsJSON, &record.Inputs); err != nil {
			return ScheduleRecord{}, fmt.Errorf("decode workflow schedule inputs: %w", err)
		}
	}
	if record.Inputs == nil {
		record.Inputs = map[string]string{}
	}
	return record, nil
}

func scanDispatchRecord(scanner interface {
	Scan(dest ...any) error
},
) (DispatchRecord, error) {
	var record DispatchRecord
	if err := scanner.Scan(
		&record.DispatchID,
		&record.ScheduleID,
		&record.TemporalWorkflowID,
		&record.TemporalRunID,
		&record.SourceWorkflowRunID,
		&record.WorkflowState,
		&record.State,
		&record.FailureReason,
		&record.ScheduledAt,
		&record.SubmittedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return DispatchRecord{}, fmt.Errorf("scan execution schedule dispatch: %w", err)
	}
	return record, nil
}

func temporalScheduleIDFor(scheduleID uuid.UUID) string {
	return "execution-schedule/" + scheduleID.String()
}

func dispatchIdempotencyKey(scheduleID uuid.UUID, _ string, runID string) string {
	return fmt.Sprintf("execution-schedule/%s/%s", scheduleID.String(), strings.TrimSpace(runID))
}

func stateForPaused(paused bool) string {
	if paused {
		return StatePaused
	}
	return StateActive
}

func truncateReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return "dispatch_failed"
	}
	if len(trimmed) > 512 {
		return trimmed[:512]
	}
	return trimmed
}
