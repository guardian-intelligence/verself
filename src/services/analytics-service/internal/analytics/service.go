package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel/trace"
	logspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
)

const (
	defaultQueryLimit = 100
	maxQueryLimit     = 500
)

type Service struct {
	CH              clickhouse.Conn
	Dataset         DatasetContext
	AllowedPrefixes []string
}

func (s *Service) Ready(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("analytics service is nil")
	}
	if s.CH == nil {
		return fmt.Errorf("analytics clickhouse connection is nil")
	}
	if err := validateDatasetContext(s.Dataset.Normalized()); err != nil {
		return err
	}
	if len(s.AllowedPrefixes) == 0 {
		return fmt.Errorf("analytics allowed event prefixes are empty")
	}
	if err := s.CH.Ping(ctx); err != nil {
		return fmt.Errorf("analytics clickhouse ping: %w", err)
	}
	return nil
}

func (s *Service) IngestLogs(ctx context.Context, source Source, req *logspb.ExportLogsServiceRequest) (int, error) {
	if s == nil || s.CH == nil {
		return 0, fmt.Errorf("analytics clickhouse connection unavailable")
	}
	rows, err := RowsFromLogs(s.Dataset, source, req, s.AllowedPrefixes, time.Now())
	if err != nil {
		return 0, err
	}
	if len(rows) > 0 {
		batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.analytics_events")
		if err != nil {
			return 0, fmt.Errorf("prepare analytics events batch: %w", err)
		}
		for i := range rows {
			if err := appendAnalyticsEvent(batch, &rows[i]); err != nil {
				return 0, err
			}
		}
		if err := batch.Send(); err != nil {
			return 0, fmt.Errorf("send analytics events batch: %w", err)
		}
	}
	if err := s.RecordIngestEvent(ctx, source, OutcomeAllowed, uint32FromInt(len(rows)), 0, ""); err != nil {
		return len(rows), err
	}
	return len(rows), nil
}

func (s *Service) RecordIngestEvent(ctx context.Context, source Source, outcome string, accepted, rejected uint32, errorCode string) error {
	if s == nil || s.CH == nil {
		return fmt.Errorf("analytics clickhouse connection unavailable")
	}
	now := time.Now().UTC()
	row := IngestEventRow{
		EventDate:       now.Truncate(24 * time.Hour),
		RecordedAt:      now,
		OrgID:           strings.TrimSpace(s.Dataset.OrgID),
		ProjectID:       strings.TrimSpace(s.Dataset.ProjectID),
		DatasetID:       strings.TrimSpace(s.Dataset.DatasetID),
		Environment:     strings.TrimSpace(s.Dataset.Environment),
		SourceKind:      strings.TrimSpace(source.Kind),
		SourceSubject:   strings.TrimSpace(source.Subject),
		RequestKind:     RequestKindOTLPLogs,
		Outcome:         strings.TrimSpace(outcome),
		AcceptedRecords: accepted,
		RejectedRecords: rejected,
		ErrorCode:       strings.TrimSpace(errorCode),
		TraceID:         traceIDFromContext(ctx),
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.analytics_ingest_events")
	if err != nil {
		return fmt.Errorf("prepare analytics ingest event batch: %w", err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("append analytics ingest event: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send analytics ingest event: %w", err)
	}
	return nil
}

func (s *Service) QueryEvents(ctx context.Context, request QueryRequest) (QueryEventsPage, error) {
	if s == nil || s.CH == nil {
		return QueryEventsPage{}, fmt.Errorf("analytics clickhouse connection unavailable")
	}
	request = normalizeQuery(request, s.Dataset.Environment)
	if err := validateQuery(request); err != nil {
		return QueryEventsPage{}, err
	}
	query := `
SELECT
    observed_at,
    timestamp,
    event_id,
    org_id,
    project_id,
    dataset_id,
    environment,
    signal_kind,
    event_name,
    source_kind,
    source_subject,
    service_name,
    service_version,
    severity_text,
    status,
    trace_id,
    span_id,
    parent_span_id,
    duration_ms,
    body,
    body_redaction_status,
    string_attributes,
    int_attributes,
    float_attributes,
    bool_attributes
FROM verself.analytics_events
WHERE org_id = ? AND project_id = ? AND dataset_id = ? AND environment = ? AND observed_at >= ? AND observed_at <= ?`
	args := []any{request.OrgID, request.ProjectID, request.DatasetID, request.Environment, request.From, request.To}
	if request.EventName != "" {
		query += " AND event_name = ?"
		args = append(args, request.EventName)
	}
	if request.TraceID != "" {
		query += " AND trace_id = ?"
		args = append(args, request.TraceID)
	}
	query += " ORDER BY observed_at DESC, event_id DESC LIMIT ?"
	args = append(args, request.Limit)
	var rows []EventRow
	if err := s.CH.Select(ctx, &rows, query, args...); err != nil {
		return QueryEventsPage{}, fmt.Errorf("query analytics events: %w", err)
	}
	page := QueryEventsPage{Events: make([]QueryEvent, 0, len(rows)), Limit: request.Limit}
	for _, row := range rows {
		page.Events = append(page.Events, queryEvent(row, request.Raw))
	}
	return page, nil
}

func (s *Service) RecordAccessEvent(ctx context.Context, row AccessEventRow) error {
	if s == nil || s.CH == nil {
		return fmt.Errorf("analytics clickhouse connection unavailable")
	}
	now := time.Now().UTC()
	row.EventDate = now.Truncate(24 * time.Hour)
	row.RecordedAt = now
	row.TraceID = firstNonEmpty(row.TraceID, traceIDFromContext(ctx))
	row.OrgID = strings.TrimSpace(row.OrgID)
	row.ProjectID = strings.TrimSpace(row.ProjectID)
	row.DatasetID = strings.TrimSpace(row.DatasetID)
	row.Environment = strings.TrimSpace(row.Environment)
	row.SubjectType = strings.TrimSpace(row.SubjectType)
	row.SubjectID = strings.TrimSpace(row.SubjectID)
	row.OperationPermission = strings.TrimSpace(row.OperationPermission)
	row.ResourcePermission = strings.TrimSpace(row.ResourcePermission)
	row.Outcome = strings.TrimSpace(row.Outcome)
	row.ErrorCode = strings.TrimSpace(row.ErrorCode)
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.analytics_access_events")
	if err != nil {
		return fmt.Errorf("prepare analytics access event batch: %w", err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("append analytics access event: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send analytics access event: %w", err)
	}
	return nil
}

func appendAnalyticsEvent(batch chdriver.Batch, row *EventRow) error {
	if err := batch.AppendStruct(row); err != nil {
		return fmt.Errorf("append analytics event: %w", err)
	}
	return nil
}

func ParseAllowedPrefixes(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeQuery(request QueryRequest, defaultEnvironment string) QueryRequest {
	request.OrgID = strings.TrimSpace(request.OrgID)
	request.ProjectID = strings.TrimSpace(request.ProjectID)
	request.DatasetID = strings.TrimSpace(request.DatasetID)
	request.Environment = firstNonEmpty(request.Environment, defaultEnvironment)
	request.EventName = strings.TrimSpace(request.EventName)
	request.TraceID = strings.TrimSpace(request.TraceID)
	if request.Limit <= 0 {
		request.Limit = defaultQueryLimit
	}
	if request.Limit > maxQueryLimit {
		request.Limit = maxQueryLimit
	}
	now := time.Now().UTC()
	if request.To.IsZero() {
		request.To = now
	}
	if request.From.IsZero() {
		request.From = request.To.Add(-24 * time.Hour)
	}
	return request
}

func validateQuery(request QueryRequest) error {
	if request.OrgID == "" {
		return fmt.Errorf("org_id is required")
	}
	if request.ProjectID == "" {
		return fmt.Errorf("project_id is required")
	}
	if request.DatasetID == "" {
		return fmt.Errorf("dataset_id is required")
	}
	if request.Environment == "" {
		return fmt.Errorf("environment is required")
	}
	if request.From.After(request.To) {
		return fmt.Errorf("from must be before to")
	}
	if request.EventName != "" && !eventNamePattern.MatchString(request.EventName) {
		return fmt.Errorf("event_name is invalid")
	}
	return nil
}

func queryEvent(row EventRow, raw bool) QueryEvent {
	out := QueryEvent{
		EventID:      row.EventID,
		ObservedAt:   row.ObservedAt.UTC().Format(time.RFC3339Nano),
		Timestamp:    row.Timestamp.UTC().Format(time.RFC3339Nano),
		OrgID:        row.OrgID,
		ProjectID:    row.ProjectID,
		DatasetID:    row.DatasetID,
		Environment:  row.Environment,
		SignalKind:   row.SignalKind,
		EventName:    row.EventName,
		SourceKind:   row.SourceKind,
		ServiceName:  row.ServiceName,
		SeverityText: row.SeverityText,
		Status:       row.Status,
		TraceID:      row.TraceID,
		SpanID:       row.SpanID,
		DurationMs:   row.DurationMs,
	}
	if raw {
		out.Body = row.Body
		out.BodyRedactionStatus = row.BodyRedactionStatus
		out.StringAttributes = row.StringAttributes
		out.IntAttributes = row.IntAttributes
		out.FloatAttributes = row.FloatAttributes
		out.BoolAttributes = row.BoolAttributes
	}
	return out
}

func traceIDFromContext(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.HasTraceID() {
		return ""
	}
	return spanContext.TraceID().String()
}

func uint32FromInt(value int) uint32 {
	if value < 0 {
		return 0
	}
	if value > int(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(value)
}
