package governance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type AuditRecord struct {
	OrgID              string         `json:"org_id"`
	ServiceName        string         `json:"service_name"`
	OperationID        string         `json:"operation_id"`
	AuditEvent         string         `json:"audit_event"`
	PrincipalType      string         `json:"principal_type"`
	PrincipalID        string         `json:"principal_id"`
	PrincipalEmail     string         `json:"principal_email,omitempty"`
	Permission         string         `json:"permission"`
	ResourceKind       string         `json:"resource_kind"`
	ResourceID         string         `json:"resource_id,omitempty"`
	Action             string         `json:"action"`
	OrgScope           string         `json:"org_scope"`
	RateLimitClass     string         `json:"rate_limit_class,omitempty"`
	Result             string         `json:"result"`
	ErrorCode          string         `json:"error_code,omitempty"`
	ErrorMessage       string         `json:"error_message,omitempty"`
	ClientIP           string         `json:"client_ip,omitempty"`
	UserAgent          string         `json:"user_agent,omitempty"`
	IdempotencyKeyHash string         `json:"idempotency_key_hash,omitempty"`
	RequestID          string         `json:"request_id,omitempty"`
	TraceID            string         `json:"trace_id,omitempty"`
	Payload            map[string]any `json:"payload,omitempty"`
	RecordedAt         time.Time      `json:"recorded_at,omitempty"`
}

type AuditEvent struct {
	EventID            uuid.UUID `ch:"event_id"`
	RecordedAt         time.Time `ch:"recorded_at"`
	EventDate          time.Time `ch:"event_date"`
	OrgID              string    `ch:"org_id"`
	ServiceName        string    `ch:"service_name"`
	OperationID        string    `ch:"operation_id"`
	AuditEvent         string    `ch:"audit_event"`
	PrincipalType      string    `ch:"principal_type"`
	PrincipalID        string    `ch:"principal_id"`
	PrincipalEmail     string    `ch:"principal_email"`
	Permission         string    `ch:"permission"`
	ResourceKind       string    `ch:"resource_kind"`
	ResourceID         string    `ch:"resource_id"`
	Action             string    `ch:"action"`
	OrgScope           string    `ch:"org_scope"`
	RateLimitClass     string    `ch:"rate_limit_class"`
	Result             string    `ch:"result"`
	ErrorCode          string    `ch:"error_code"`
	ErrorMessage       string    `ch:"error_message"`
	ClientIP           string    `ch:"client_ip"`
	UserAgentHash      string    `ch:"user_agent_hash"`
	IdempotencyKeyHash string    `ch:"idempotency_key_hash"`
	RequestID          string    `ch:"request_id"`
	TraceID            string    `ch:"trace_id"`
	PayloadJSON        string    `ch:"payload_json"`
	ContentSHA256      string    `ch:"content_sha256"`
	Sequence           uint64    `ch:"sequence"`
	PrevHMAC           string    `ch:"prev_hmac"`
	RowHMAC            string    `ch:"row_hmac"`
}

type AuditListFilters struct {
	Limit       int
	Cursor      string
	ServiceName string
	OperationID string
	Result      string
}

type AuditListPage struct {
	Events     []AuditEvent
	NextCursor string
	Limit      int
}

func (s *Service) RecordAuditEvent(ctx context.Context, record AuditRecord) (*AuditEvent, error) {
	ctx, span := tracer.Start(ctx, "governance.audit.record")
	defer span.End()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	record.OrgID = strings.TrimSpace(record.OrgID)
	record.ServiceName = strings.TrimSpace(record.ServiceName)
	record.OperationID = strings.TrimSpace(record.OperationID)
	record.AuditEvent = strings.TrimSpace(record.AuditEvent)
	record.Result = strings.TrimSpace(record.Result)
	record.PrincipalID = strings.TrimSpace(record.PrincipalID)
	if record.OrgID == "" || record.ServiceName == "" || record.OperationID == "" || record.AuditEvent == "" || record.Result == "" {
		return nil, fmt.Errorf("%w: org_id, service_name, operation_id, audit_event, and result are required", ErrInvalidArgument)
	}
	if record.PrincipalType == "" {
		record.PrincipalType = "user"
	}
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now().UTC()
	}
	if record.TraceID == "" {
		if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
			record.TraceID = spanContext.TraceID().String()
		}
	}
	payload, err := canonicalJSON(record)
	if err != nil {
		return nil, fmt.Errorf("%w: canonical audit payload: %v", ErrInvalidArgument, err)
	}
	contentHash := sha256.Sum256(payload)
	eventID := uuid.New()
	event := &AuditEvent{
		EventID:            eventID,
		RecordedAt:         record.RecordedAt.UTC(),
		EventDate:          truncateDate(record.RecordedAt.UTC()),
		OrgID:              record.OrgID,
		ServiceName:        record.ServiceName,
		OperationID:        record.OperationID,
		AuditEvent:         record.AuditEvent,
		PrincipalType:      record.PrincipalType,
		PrincipalID:        record.PrincipalID,
		PrincipalEmail:     record.PrincipalEmail,
		Permission:         record.Permission,
		ResourceKind:       record.ResourceKind,
		ResourceID:         record.ResourceID,
		Action:             record.Action,
		OrgScope:           record.OrgScope,
		RateLimitClass:     record.RateLimitClass,
		Result:             record.Result,
		ErrorCode:          record.ErrorCode,
		ErrorMessage:       record.ErrorMessage,
		ClientIP:           record.ClientIP,
		UserAgentHash:      hashText(record.UserAgent),
		IdempotencyKeyHash: record.IdempotencyKeyHash,
		RequestID:          record.RequestID,
		TraceID:            record.TraceID,
		PayloadJSON:        string(payload),
		ContentSHA256:      hex.EncodeToString(contentHash[:]),
	}
	if err := s.assignAuditSequence(ctx, event); err != nil {
		return nil, err
	}
	if err := s.insertAuditClickHouse(ctx, event); err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", event.OrgID),
		attribute.String("forge_metal.audit_event", event.AuditEvent),
		attribute.Int64("forge_metal.audit_sequence", int64(event.Sequence)),
	)
	return event, nil
}

func (s *Service) assignAuditSequence(ctx context.Context, event *AuditEvent) error {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: begin audit chain tx: %v", ErrStore, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const zeroHMAC = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := tx.Exec(ctx, `
		INSERT INTO governance_audit_chain_state (org_id, sequence, row_hmac)
		VALUES ($1, 0, $2)
		ON CONFLICT (org_id) DO NOTHING
	`, event.OrgID, zeroHMAC); err != nil {
		return fmt.Errorf("%w: initialize audit chain: %v", ErrStore, err)
	}

	var previousSequence uint64
	var previousHMAC string
	err = tx.QueryRow(ctx, `
		SELECT sequence, row_hmac
		FROM governance_audit_chain_state
		WHERE org_id = $1
		FOR UPDATE
	`, event.OrgID).Scan(&previousSequence, &previousHMAC)
	if err != nil {
		return fmt.Errorf("%w: lock audit chain: %v", ErrStore, err)
	}
	event.Sequence = previousSequence + 1
	event.PrevHMAC = previousHMAC
	event.RowHMAC = s.computeRowHMAC(event)
	if _, err := tx.Exec(ctx, `
		UPDATE governance_audit_chain_state
		SET sequence = $2, row_hmac = $3, updated_at = now()
		WHERE org_id = $1
	`, event.OrgID, event.Sequence, event.RowHMAC); err != nil {
		return fmt.Errorf("%w: advance audit chain: %v", ErrStore, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: commit audit chain: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) insertAuditClickHouse(ctx context.Context, event *AuditEvent) error {
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO forge_metal.audit_events")
	if err != nil {
		return fmt.Errorf("%w: prepare audit insert: %v", ErrStore, err)
	}
	if err := batch.AppendStruct(event); err != nil {
		return fmt.Errorf("%w: append audit event: %v", ErrStore, err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("%w: send audit event: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) ListAuditEvents(ctx context.Context, principal Principal, filters AuditListFilters) (AuditListPage, error) {
	ctx, span := tracer.Start(ctx, "governance.audit.list")
	defer span.End()
	if err := s.Validate(); err != nil {
		return AuditListPage{}, err
	}
	orgID := strings.TrimSpace(principal.OrgID)
	if orgID == "" {
		return AuditListPage{}, fmt.Errorf("%w: org id is required", ErrInvalidArgument)
	}
	limit := filters.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	cursor := auditCursor{RecordedAt: time.Unix(0, 0).UTC()}
	cursorEnabled := uint8(0)
	if filters.Cursor != "" {
		parsedCursor, err := parseCursor(filters.Cursor)
		if err != nil {
			return AuditListPage{}, err
		}
		cursor = parsedCursor
		cursorEnabled = 1
	}
	rows, err := s.CH.Query(ctx, `
		SELECT
			event_id, recorded_at, event_date, org_id, service_name, operation_id, audit_event,
			principal_type, principal_id, principal_email,
			permission, resource_kind, resource_id, action, org_scope, rate_limit_class,
			result, error_code, error_message, client_ip, user_agent_hash, idempotency_key_hash,
			request_id, trace_id, payload_json, content_sha256, sequence, prev_hmac, row_hmac
		FROM forge_metal.audit_events
		WHERE org_id = $1
		  AND ($2 = '' OR service_name = $2)
		  AND ($3 = '' OR operation_id = $3)
		  AND ($4 = '' OR result = $4)
		  AND ($5 = 0 OR (recorded_at, sequence) < ($6, $7))
		ORDER BY recorded_at DESC, sequence DESC
		LIMIT $8
	`, orgID, filters.ServiceName, filters.OperationID, filters.Result, cursorEnabled, cursor.RecordedAt, cursor.Sequence, limit+1)
	if err != nil {
		return AuditListPage{}, fmt.Errorf("%w: list audit events: %v", ErrStore, err)
	}
	defer rows.Close()
	events := make([]AuditEvent, 0, limit)
	for rows.Next() {
		var event AuditEvent
		if err := rows.ScanStruct(&event); err != nil {
			return AuditListPage{}, fmt.Errorf("%w: scan audit event: %v", ErrStore, err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return AuditListPage{}, fmt.Errorf("%w: audit rows: %v", ErrStore, err)
	}
	nextCursor := ""
	if len(events) > limit {
		last := events[limit-1]
		nextCursor = makeCursor(last.RecordedAt, last.Sequence)
		events = events[:limit]
	}
	span.SetAttributes(attribute.String("forge_metal.org_id", orgID), attribute.Int("forge_metal.audit_event_count", len(events)))
	return AuditListPage{Events: events, NextCursor: nextCursor, Limit: limit}, nil
}

func (s *Service) computeRowHMAC(event *AuditEvent) string {
	mac := hmac.New(sha256.New, s.HMACKey)
	fmt.Fprintf(mac, "%s\n%s\n%d\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",
		event.OrgID,
		event.EventID.String(),
		event.Sequence,
		event.PrevHMAC,
		event.RecordedAt.Format(time.RFC3339Nano),
		event.ServiceName,
		event.OperationID,
		event.Result,
		event.ContentSHA256,
		event.TraceID,
	)
	return hex.EncodeToString(mac.Sum(nil))
}

func canonicalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func truncateDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func hashText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type auditCursor struct {
	RecordedAt time.Time `json:"recorded_at"`
	Sequence   uint64    `json:"sequence"`
}

func makeCursor(recordedAt time.Time, sequence uint64) string {
	raw, _ := json.Marshal(auditCursor{RecordedAt: recordedAt.UTC(), Sequence: sequence})
	return hex.EncodeToString(raw)
}

func parseCursor(value string) (auditCursor, error) {
	raw, err := hex.DecodeString(value)
	if err != nil {
		return auditCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	var cursor auditCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return auditCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	if cursor.RecordedAt.IsZero() {
		return auditCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	return cursor, nil
}
