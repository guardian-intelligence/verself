package githubintegration

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type githubEvent struct {
	ObservedAt             time.Time `ch:"observed_at"`
	EventName              string    `ch:"event_name"`
	Result                 string    `ch:"result"`
	Reason                 string    `ch:"reason"`
	DeliveryID             string    `ch:"delivery_id"`
	Action                 string    `ch:"action"`
	OrgID                  string    `ch:"org_id"`
	ProviderInstallationID uint64    `ch:"provider_installation_id"`
	ProviderRepositoryID   uint64    `ch:"provider_repository_id"`
	ProviderRunID          uint64    `ch:"provider_run_id"`
	ProviderRunAttempt     uint64    `ch:"provider_run_attempt"`
	ProviderJobID          uint64    `ch:"provider_job_id"`
	RepositoryFullName     string    `ch:"repository_full_name"`
	RunnerID               uint64    `ch:"runner_id"`
	RunnerName             string    `ch:"runner_name"`
	RunnerClass            string    `ch:"runner_class"`
	AllocationID           uuid.UUID `ch:"allocation_id"`
	ExecutionID            uuid.UUID `ch:"execution_id"`
	AttemptID              uuid.UUID `ch:"attempt_id"`
	StartedAt              time.Time `ch:"started_at"`
	CompletedAt            time.Time `ch:"completed_at"`
	DurationMs             int64     `ch:"duration_ms"`
	AttributesJSON         string    `ch:"attributes_json"`
	TraceID                string    `ch:"trace_id"`
	SpanID                 string    `ch:"span_id"`
}

func (s *Service) writeEvent(ctx context.Context, row githubEvent) {
	if s == nil || s.cfg.CH == nil {
		return
	}
	if row.ObservedAt.IsZero() {
		row.ObservedAt = time.Now().UTC()
	}
	if row.StartedAt.IsZero() {
		row.StartedAt = row.ObservedAt
	}
	if row.CompletedAt.IsZero() {
		row.CompletedAt = row.ObservedAt
	}
	if row.DurationMs == 0 && !row.StartedAt.IsZero() && !row.CompletedAt.IsZero() {
		row.DurationMs = row.CompletedAt.Sub(row.StartedAt).Milliseconds()
	}
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		row.TraceID = span.TraceID().String()
		row.SpanID = span.SpanID().String()
	}
	if row.AttributesJSON == "" {
		row.AttributesJSON = "{}"
	}
	insertCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	batch, err := s.cfg.CH.PrepareBatch(insertCtx, "INSERT INTO verself.github_integration_events")
	if err != nil {
		if s.cfg.Logger != nil {
			s.cfg.Logger.WarnContext(ctx, "github integration clickhouse batch failed", "event_name", row.EventName, "error", err)
		}
		return
	}
	if err := batch.AppendStruct(&row); err != nil {
		if s.cfg.Logger != nil {
			s.cfg.Logger.WarnContext(ctx, "github integration clickhouse append failed", "event_name", row.EventName, "error", err)
		}
		return
	}
	if err := batch.Send(); err != nil && s.cfg.Logger != nil {
		s.cfg.Logger.WarnContext(ctx, "github integration clickhouse insert failed", "event_name", row.EventName, "error", err)
	}
}

func githubEventFromMetadata(meta webhookMetadata, eventName string, result string, reason string, startedAt, completedAt time.Time) githubEvent {
	attrs := mustJSON(map[string]string{
		"payload_sha256": meta.PayloadSHA256,
	})
	return githubEvent{
		ObservedAt:             completedAt,
		EventName:              eventName,
		Result:                 result,
		Reason:                 truncate(reason, 1024),
		DeliveryID:             meta.DeliveryID,
		Action:                 meta.Action,
		ProviderInstallationID: uint64FromInt64(meta.InstallationID),
		ProviderRepositoryID:   uint64FromInt64(meta.RepositoryID),
		ProviderRunID:          uint64FromInt64(meta.RunID),
		ProviderRunAttempt:     uint64FromInt64(meta.RunAttempt),
		ProviderJobID:          uint64FromInt64(meta.JobID),
		RepositoryFullName:     meta.RepositoryFullName,
		RunnerID:               uint64FromInt64(meta.RunnerID),
		RunnerName:             meta.RunnerName,
		RunnerClass:            meta.RunnerClass,
		AllocationID:           uuidFromLike(meta.AllocationID),
		ExecutionID:            uuidFromLike(meta.ExecutionID),
		AttemptID:              uuidFromLike(meta.AttemptID),
		StartedAt:              startedAt,
		CompletedAt:            completedAt,
		DurationMs:             completedAt.Sub(startedAt).Milliseconds(),
		AttributesJSON:         attrs,
	}
}

func mustJSON(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(payload)
}

func uint64FromInt64(value int64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func uuidFromLike(value uuidLike) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	id, err := uuid.Parse(value.String())
	if err != nil {
		return uuid.Nil
	}
	return id
}
