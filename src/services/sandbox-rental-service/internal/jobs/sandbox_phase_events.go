package jobs

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type sandboxPhaseRow struct {
	ObservedAt             time.Time `ch:"observed_at"`
	EventSource            string    `ch:"event_source"`
	PhaseGroup             string    `ch:"phase_group"`
	PhaseName              string    `ch:"phase_name"`
	PhaseOrder             uint16    `ch:"phase_order"`
	Result                 string    `ch:"result"`
	Reason                 string    `ch:"reason"`
	OrgID                  string    `ch:"org_id"`
	ExecutionID            uuid.UUID `ch:"execution_id"`
	AttemptID              uuid.UUID `ch:"attempt_id"`
	AllocationID           uuid.UUID `ch:"allocation_id"`
	Provider               string    `ch:"provider"`
	ExternalProvider       string    `ch:"external_provider"`
	ProviderInstallationID uint64    `ch:"provider_installation_id"`
	ProviderRepositoryID   uint64    `ch:"provider_repository_id"`
	ProviderRunID          uint64    `ch:"provider_run_id"`
	ProviderRunAttempt     uint64    `ch:"provider_run_attempt"`
	ProviderJobID          uint64    `ch:"provider_job_id"`
	RepositoryFullName     string    `ch:"repository_full_name"`
	WorkflowName           string    `ch:"workflow_name"`
	JobName                string    `ch:"job_name"`
	HeadBranch             string    `ch:"head_branch"`
	HeadSHA                string    `ch:"head_sha"`
	RunnerClass            string    `ch:"runner_class"`
	RunnerName             string    `ch:"runner_name"`
	LeaseID                string    `ch:"lease_id"`
	ExecID                 string    `ch:"exec_id"`
	CorrelationID          string    `ch:"correlation_id"`
	StartedAt              time.Time `ch:"started_at"`
	CompletedAt            time.Time `ch:"completed_at"`
	DurationMs             int64     `ch:"duration_ms"`
	AttributesJSON         string    `ch:"attributes_json"`
	TraceID                string    `ch:"trace_id"`
	SpanID                 string    `ch:"span_id"`
}

type sandboxPhaseAttrs map[string]string

const sandboxPhaseInsertTimeout = 2 * time.Second

func (s *Service) recordExecutionPhase(ctx context.Context, item executionWorkItem, eventSource, phaseName, result, reason string, startedAt, completedAt time.Time, attrs sandboxPhaseAttrs) {
	row := sandboxPhaseRowFromWorkItem(ctx, item, eventSource, phaseName, result, reason, startedAt, completedAt, attrs)
	go s.writeSandboxPhaseEvent(detachedContext(ctx), row)
}

func (s *Service) recordRunnerIdentityPhase(ctx context.Context, identity RunnerExecutionIdentity, item executionWorkItem, eventSource, phaseName, result, reason string, startedAt, completedAt time.Time, attrs sandboxPhaseAttrs) {
	row := sandboxPhaseRowFromWorkItem(ctx, item, eventSource, phaseName, result, reason, startedAt, completedAt, attrs)
	applyRunnerIdentityToPhase(&row, identity)
	go s.writeSandboxPhaseEvent(detachedContext(ctx), row)
}

func (s *Service) writeSandboxPhaseEvent(ctx context.Context, row sandboxPhaseRow) {
	if s == nil {
		return
	}
	if err := s.writeSandboxPhaseEvents(ctx, []sandboxPhaseRow{row}); err != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "sandbox phase event insert failed", "phase_name", row.PhaseName, "result", row.Result, "error", err)
	}
}

func (s *Service) writeSandboxPhaseEvents(ctx context.Context, rows []sandboxPhaseRow) error {
	if s == nil || s.CH == nil || len(rows) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, sandboxPhaseInsertTimeout)
		defer cancel()
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".sandbox_phase_events")
	if err != nil {
		return err
	}
	for i := range rows {
		normalizeSandboxPhaseRow(&rows[i])
		if err := batch.AppendStruct(&rows[i]); err != nil {
			return err
		}
	}
	return batch.Send()
}

func sandboxPhaseRowFromWorkItem(ctx context.Context, item executionWorkItem, eventSource, phaseName, result, reason string, startedAt, completedAt time.Time, attrs sandboxPhaseAttrs) sandboxPhaseRow {
	row := sandboxPhaseBase(ctx, eventSource, phaseName, result, reason, startedAt, completedAt, attrs)
	row.OrgID = item.OrgID
	row.ExecutionID = item.ExecutionID
	row.AttemptID = item.AttemptID
	row.Provider = item.Provider
	row.ExternalProvider = item.ExternalProvider
	row.ProviderJobID = parseUint64Decimal(item.ExternalTaskID)
	row.RunnerClass = item.RunnerClass
	row.LeaseID = item.LeaseID
	row.ExecID = item.ExecID
	row.CorrelationID = item.CorrelationID
	return row
}

func sandboxPhaseRowFromRun(record ExecutionRecord, eventSource, phaseName, result, reason string, startedAt, completedAt time.Time, attrs sandboxPhaseAttrs) sandboxPhaseRow {
	row := sandboxPhaseBase(context.Background(), eventSource, phaseName, result, reason, startedAt, completedAt, attrs)
	row.OrgID = record.OrgID
	row.ExecutionID = record.ExecutionID
	row.AttemptID = record.LatestAttempt.AttemptID
	row.Provider = record.Provider
	row.ExternalProvider = record.ExternalProvider
	row.ProviderInstallationID = int64ToUint64(record.Runner.ProviderInstallationID)
	row.ProviderRunID = int64ToUint64(record.Runner.ProviderRunID)
	row.ProviderJobID = int64ToUint64(record.Runner.ProviderJobID)
	row.RepositoryFullName = record.Runner.RepositoryFullName
	row.WorkflowName = record.Runner.WorkflowName
	row.JobName = record.Runner.JobName
	row.HeadBranch = record.Runner.HeadBranch
	row.HeadSHA = record.Runner.HeadSHA
	row.RunnerClass = record.RunnerClass
	row.LeaseID = record.LatestAttempt.LeaseID
	row.ExecID = record.LatestAttempt.ExecID
	row.CorrelationID = record.CorrelationID
	row.TraceID = record.LatestAttempt.TraceID
	return row
}

func sandboxPhaseBase(ctx context.Context, eventSource, phaseName, result, reason string, startedAt, completedAt time.Time, attrs sandboxPhaseAttrs) sandboxPhaseRow {
	if ctx == nil {
		ctx = context.Background()
	}
	spanContext := trace.SpanContextFromContext(ctx)
	row := sandboxPhaseRow{
		ObservedAt:     time.Now().UTC(),
		EventSource:    eventSource,
		PhaseGroup:     sandboxPhaseGroup(phaseName),
		PhaseName:      phaseName,
		PhaseOrder:     sandboxPhaseOrder(phaseName),
		Result:         firstNonEmpty(result, "succeeded"),
		Reason:         reason,
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
		AttributesJSON: encodeSandboxPhaseAttrs(attrs),
		TraceID:        spanContext.TraceID().String(),
		SpanID:         spanContext.SpanID().String(),
	}
	normalizeSandboxPhaseRow(&row)
	return row
}

func applyRunnerIdentityToPhase(row *sandboxPhaseRow, identity RunnerExecutionIdentity) {
	if row == nil {
		return
	}
	row.ExecutionID = firstUUID(row.ExecutionID, identity.ExecutionID)
	row.AttemptID = firstUUID(row.AttemptID, identity.AttemptID)
	row.AllocationID = firstUUID(row.AllocationID, identity.AllocationID)
	row.OrgID = firstNonEmpty(row.OrgID, identity.OrgID)
	row.Provider = firstNonEmpty(row.Provider, identity.Provider)
	row.ExternalProvider = firstNonEmpty(row.ExternalProvider, identity.Provider)
	row.ProviderInstallationID = firstNonZeroUint64(row.ProviderInstallationID, int64ToUint64(identity.ProviderInstallationID))
	row.ProviderRepositoryID = firstNonZeroUint64(row.ProviderRepositoryID, int64ToUint64(identity.ProviderRepositoryID))
	row.ProviderRunID = firstNonZeroUint64(row.ProviderRunID, int64ToUint64(identity.ProviderRunID))
	row.ProviderRunAttempt = firstNonZeroUint64(row.ProviderRunAttempt, int64ToUint64(identity.ProviderRunAttempt))
	row.ProviderJobID = firstNonZeroUint64(row.ProviderJobID, int64ToUint64(identity.ProviderJobID))
	row.RepositoryFullName = firstNonEmpty(row.RepositoryFullName, identity.RepositoryFullName)
	row.WorkflowName = firstNonEmpty(row.WorkflowName, identity.WorkflowName)
	row.JobName = firstNonEmpty(row.JobName, identity.JobName)
	row.HeadBranch = firstNonEmpty(row.HeadBranch, identity.HeadBranch, identity.RunHeadBranch)
	row.HeadSHA = firstNonEmpty(row.HeadSHA, identity.HeadSHA, identity.RunHeadSHA)
	row.RunnerClass = firstNonEmpty(row.RunnerClass, identity.RunnerClass)
	row.RunnerName = firstNonEmpty(row.RunnerName, identity.RunnerName)
}

func normalizeSandboxPhaseRow(row *sandboxPhaseRow) {
	now := time.Now().UTC()
	if row.ObservedAt.IsZero() {
		row.ObservedAt = now
	}
	if row.EventSource == "" {
		row.EventSource = "sandbox-rental"
	}
	if row.Result == "" {
		row.Result = "succeeded"
	}
	if row.PhaseGroup == "" {
		row.PhaseGroup = sandboxPhaseGroup(row.PhaseName)
	}
	if row.PhaseOrder == 0 {
		row.PhaseOrder = sandboxPhaseOrder(row.PhaseName)
	}
	if row.CompletedAt.IsZero() {
		row.CompletedAt = row.ObservedAt
	}
	if row.StartedAt.IsZero() {
		row.StartedAt = row.CompletedAt
	}
	row.StartedAt = row.StartedAt.UTC()
	row.CompletedAt = row.CompletedAt.UTC()
	duration := row.CompletedAt.Sub(row.StartedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	row.DurationMs = duration
}

func encodeSandboxPhaseAttrs(attrs sandboxPhaseAttrs) string {
	if len(attrs) == 0 {
		return ""
	}
	payload, err := json.Marshal(attrs)
	if err != nil {
		return ""
	}
	return string(payload)
}

func sandboxPhaseGroup(phaseName string) string {
	switch {
	case strings.HasPrefix(phaseName, "github.webhook."):
		return "github.webhook"
	case strings.HasPrefix(phaseName, "github.runner."):
		return "github.runner"
	case strings.HasPrefix(phaseName, "runner.bootstrap."):
		return "runner.bootstrap"
	case strings.HasPrefix(phaseName, "sandbox.execution."):
		return "sandbox.execution"
	case strings.HasPrefix(phaseName, "sandbox.durable."):
		return "sandbox.durable"
	case strings.HasPrefix(phaseName, "sandbox.billing."):
		return "sandbox.billing"
	case strings.HasPrefix(phaseName, "vm."):
		return "vm"
	default:
		if idx := strings.LastIndex(phaseName, "."); idx > 0 {
			return phaseName[:idx]
		}
		return phaseName
	}
}

func sandboxPhaseOrder(phaseName string) uint16 {
	switch phaseName {
	case "github.webhook.workflow_job":
		return 10
	case "github.runner.allocate":
		return 20
	case "sandbox.execution.submit":
		return 40
	case "sandbox.execution.load_work":
		return 50
	case "sandbox.billing.reserve":
		return 60
	case "sandbox.durable.prepare":
		return 70
	case "sandbox.durable.prepare.identity":
		return 71
	case "sandbox.durable.prepare.github_run_hydrate":
		return 72
	case "sandbox.durable.prepare.declaration":
		return 73
	case "sandbox.durable.prepare.storage_capacity":
		return 74
	case "sandbox.durable.prepare.plan":
		return 75
	case "sandbox.durable.storage_quota":
		return 80
	case "vm.lease.acquire":
		return 90
	case "vm.lease.ready_wait":
		return 100
	case "sandbox.durable.mount_results":
		return 110
	case "vm.exec.start":
		return 120
	case "sandbox.billing.activate":
		return 130
	case "runner.bootstrap.fetch_jit":
		return 140
	case "runner.bootstrap.prepare_runtime":
		return 141
	case "runner.bootstrap.launch_runner":
		return 142
	case "runner.bootstrap.total":
		return 143
	case "github.runner.start_to_listen":
		return 150
	case "github.runner.assignment_wait":
		return 160
	case "github.runner.job":
		return 170
	case "github.runner.post_job_exit":
		return 180
	case "vm.exec.wait":
		return 190
	case "github.job.result_wait":
		return 200
	case "sandbox.durable.seal":
		return 210
	case "sandbox.billing.settle":
		return 220
	case "sandbox.execution.complete":
		return 230
	case "vm.lease.release":
		return 240
	case "sandbox.runner.mark_exited":
		return 250
	default:
		return 1000
	}
}

func parseUint64Decimal(value string) uint64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func firstUUID(values ...uuid.UUID) uuid.UUID {
	for _, value := range values {
		if value != uuid.Nil {
			return value
		}
	}
	return uuid.Nil
}

func firstNonZeroUint64(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func sandboxPhaseResult(err error) (string, string) {
	if err == nil {
		return "succeeded", ""
	}
	return "failed", err.Error()
}
