package governance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	dto "github.com/verself/domain-transfer-objects"
	"github.com/verself/governance-service/internal/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	AuditSchemaVersion = "2026-05-11.v1"
	zeroHMAC           = "0000000000000000000000000000000000000000000000000000000000000000"
	emptyDetailJSON    = "{}"
)

type AuditRecord struct {
	SchemaVersion      string         `json:"schema_version,omitempty"`
	OrgID              string         `json:"org_id"`
	EventSource        string         `json:"event_source"`
	EventName          string         `json:"event_name"`
	AuditEvent         string         `json:"audit_event"`
	ActorType          string         `json:"actor_type"`
	ActorID            string         `json:"actor_id"`
	CredentialID       string         `json:"credential_id,omitempty"`
	TargetType         string         `json:"target_type"`
	TargetID           string         `json:"target_id,omitempty"`
	TargetResourceName string         `json:"targetResourceName,omitempty"`
	Permission         string         `json:"permission"`
	Outcome            string         `json:"outcome"`
	ErrorCode          string         `json:"error_code,omitempty"`
	TraceID            string         `json:"trace_id,omitempty"`
	HMACKeyID          string         `json:"hmac_key_id,omitempty"`
	Detail             map[string]any `json:"detail,omitempty"`
	RecordedAt         time.Time      `json:"recorded_at,omitempty"`
}

type AuditEvent struct {
	RecordedAt         time.Time `ch:"recorded_at"`
	EventDate          time.Time `ch:"event_date"`
	SchemaVersion      string    `ch:"schema_version"`
	EventID            uuid.UUID `ch:"event_id"`
	OrgID              string    `ch:"org_id"`
	Sequence           uint64    `ch:"sequence"`
	EventSource        string    `ch:"event_source"`
	EventName          string    `ch:"event_name"`
	AuditEvent         string    `ch:"audit_event"`
	ActorType          string    `ch:"actor_type"`
	ActorID            string    `ch:"actor_id"`
	CredentialID       string    `ch:"credential_id"`
	TargetType         string    `ch:"target_type"`
	TargetID           string    `ch:"target_id"`
	TargetResourceName string    `ch:"target_resource_name"`
	Permission         string    `ch:"permission"`
	Outcome            string    `ch:"outcome"`
	ErrorCode          string    `ch:"error_code"`
	TraceID            string    `ch:"trace_id"`
	DetailSHA256       string    `ch:"detail_sha256"`
	PrevHMAC           string    `ch:"prev_hmac"`
	RowHMAC            string    `ch:"row_hmac"`
	HMACKeyID          string    `ch:"hmac_key_id"`
}

type AuditEventDetail struct {
	EventID      uuid.UUID `ch:"event_id"`
	OrgID        string    `ch:"org_id"`
	EventDate    time.Time `ch:"event_date"`
	DetailJSON   string    `ch:"detail_json"`
	DetailSHA256 string    `ch:"detail_sha256"`
}

type AuditListFilters struct {
	Limit              int
	Cursor             string
	Order              string // "desc" (default) or "asc"; controls (recorded_at, sequence) ordering.
	ActorID            string
	AuditEvent         string
	CredentialID       string
	EventName          string
	EventSource        string
	Outcome            string
	TargetID           string
	TargetType         string
	TargetResourceName string
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
	record = s.normalizeAuditRecord(ctx, record)
	if record.OrgID == "" || record.EventSource == "" || record.EventName == "" || record.AuditEvent == "" || record.Outcome == "" {
		return nil, fmt.Errorf("%w: org_id, event_source, event_name, audit_event, and outcome are required", ErrInvalidArgument)
	}
	if record.ActorType == "" || record.ActorID == "" || record.TargetType == "" || record.Permission == "" {
		return nil, fmt.Errorf("%w: actor, target_type, and permission are required", ErrInvalidArgument)
	}
	detailJSON, detailHash, err := auditDetail(record.Detail)
	if err != nil {
		return nil, err
	}
	event := eventFromAuditRecord(record, detailHash)
	if err := s.assignAuditSequence(ctx, event, detailJSON); err != nil {
		return nil, err
	}
	if err := s.projectAuditEvent(ctx, store.New(s.PG), event, detailJSON); err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("verself.org_id", event.OrgID),
		attribute.String("verself.audit_event", event.AuditEvent),
		attribute.String("verself.actor_type", event.ActorType),
		attribute.String("verself.outcome", event.Outcome),
		attribute.Int64("verself.audit_sequence", int64FromUint64(event.Sequence, "audit sequence")),
	)
	return event, nil
}

func (s *Service) ProjectPendingAuditEvents(ctx context.Context, limit int) (int, error) {
	ctx, span := tracer.Start(ctx, "governance.audit.project_pending")
	defer span.End()
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("%w: begin audit projection tx: %v", ErrStore, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := store.New(tx)
	rows, err := qtx.ClaimPendingAuditEventRows(ctx, store.ClaimPendingAuditEventRowsParams{LimitCount: int32(limit)})
	if err != nil {
		return 0, fmt.Errorf("%w: list pending audit events: %v", ErrStore, err)
	}

	projected := 0
	for _, row := range rows {
		var event AuditEvent
		if err := json.Unmarshal([]byte(row.RowJson), &event); err != nil {
			return projected, fmt.Errorf("%w: unmarshal pending audit event: %v", ErrStore, err)
		}
		if err := s.projectAuditEvent(ctx, qtx, &event, row.PayloadJson); err != nil {
			return projected, err
		}
		projected++
	}
	if err := tx.Commit(ctx); err != nil {
		return projected, fmt.Errorf("%w: commit audit projection tx: %v", ErrStore, err)
	}
	span.SetAttributes(attribute.Int("verself.audit_projected_count", projected))
	return projected, nil
}

func (s *Service) normalizeAuditRecord(ctx context.Context, record AuditRecord) AuditRecord {
	record.SchemaVersion = firstNonEmpty(record.SchemaVersion, AuditSchemaVersion)
	record.OrgID = strings.TrimSpace(record.OrgID)
	record.EventSource = strings.TrimSpace(record.EventSource)
	record.EventName = strings.TrimSpace(record.EventName)
	record.AuditEvent = firstNonEmpty(record.AuditEvent, record.EventSource+"."+record.EventName)
	record.ActorType = firstNonEmpty(record.ActorType, "user")
	record.ActorID = strings.TrimSpace(record.ActorID)
	record.CredentialID = strings.TrimSpace(record.CredentialID)
	record.TargetType = strings.TrimSpace(record.TargetType)
	record.TargetID = strings.TrimSpace(record.TargetID)
	record.TargetResourceName = strings.TrimSpace(record.TargetResourceName)
	if record.TargetResourceName == "" {
		record.TargetResourceName = s.targetResourceName(record).String()
	}
	record.Permission = strings.TrimSpace(record.Permission)
	record.Outcome = strings.TrimSpace(record.Outcome)
	record.ErrorCode = strings.TrimSpace(record.ErrorCode)
	record.HMACKeyID = firstNonEmpty(record.HMACKeyID, s.HMACKeyID, "default")
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now().UTC()
	}
	record.RecordedAt = record.RecordedAt.UTC()
	if record.TraceID == "" {
		if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
			record.TraceID = spanContext.TraceID().String()
		}
	}
	return record
}

func (s *Service) targetResourceName(record AuditRecord) dto.ResourceName {
	if s.InstallationID == "" || record.OrgID == "" {
		return ""
	}
	switch record.TargetType {
	case "organization", "audit_log":
		if record.TargetID != "" && record.TargetType == "audit_log" {
			return dto.ResourceNameAuditExport(s.InstallationID, record.OrgID, record.TargetID)
		}
		return dto.ResourceNameOrg(s.InstallationID, record.OrgID)
	case "organization_member":
		return optionalResourceName(record.TargetID, dto.ResourceNameMember(s.InstallationID, record.OrgID, record.TargetID))
	case "service_account":
		return optionalResourceName(record.TargetID, dto.ResourceNameMachinePrincipal(s.InstallationID, record.OrgID, record.TargetID))
	case "api_credential", "opaque_credential":
		return optionalResourceName(record.TargetID, dto.ResourceNameCredential(s.InstallationID, record.OrgID, record.TargetID))
	case "secret":
		return optionalResourceName(record.TargetID, dto.ResourceNameSecret(s.InstallationID, record.OrgID, record.TargetID))
	case "variable":
		return optionalResourceName(record.TargetID, dto.ResourceNameVariable(s.InstallationID, record.OrgID, record.TargetID))
	case "transit_key":
		return optionalResourceName(record.TargetID, dto.ResourceNameTransitKey(s.InstallationID, record.OrgID, record.TargetID))
	case "run", "execution":
		return optionalResourceName(record.TargetID, dto.ResourceNameRun(s.InstallationID, record.OrgID, record.TargetID))
	case "execution_schedule":
		return optionalResourceName(record.TargetID, dto.ResourceNameSchedule(s.InstallationID, record.OrgID, record.TargetID))
	case "github_installation":
		return optionalResourceName(record.TargetID, dto.ResourceNameGitHubInstallation(s.InstallationID, record.OrgID, record.TargetID))
	default:
		return ""
	}
}

func optionalResourceName(id string, name dto.ResourceName) dto.ResourceName {
	if id == "" {
		return ""
	}
	return name
}

func eventFromAuditRecord(record AuditRecord, detailHash string) *AuditEvent {
	return &AuditEvent{
		RecordedAt:         record.RecordedAt,
		EventDate:          truncateDate(record.RecordedAt),
		SchemaVersion:      record.SchemaVersion,
		EventID:            uuid.New(),
		OrgID:              record.OrgID,
		EventSource:        record.EventSource,
		EventName:          record.EventName,
		AuditEvent:         record.AuditEvent,
		ActorType:          record.ActorType,
		ActorID:            record.ActorID,
		CredentialID:       record.CredentialID,
		TargetType:         record.TargetType,
		TargetID:           record.TargetID,
		TargetResourceName: record.TargetResourceName,
		Permission:         record.Permission,
		Outcome:            record.Outcome,
		ErrorCode:          record.ErrorCode,
		TraceID:            record.TraceID,
		DetailSHA256:       detailHash,
		HMACKeyID:          record.HMACKeyID,
	}
}

func (s *Service) assignAuditSequence(ctx context.Context, event *AuditEvent, detailJSON string) error {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: begin audit chain tx: %v", ErrStore, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := store.New(tx)
	if err := qtx.InitializeAuditChainState(ctx, store.InitializeAuditChainStateParams{
		OrgID:   event.OrgID,
		RowHmac: zeroHMAC,
	}); err != nil {
		return fmt.Errorf("%w: initialize audit chain: %v", ErrStore, err)
	}

	chain, err := qtx.LockAuditChainState(ctx, store.LockAuditChainStateParams{OrgID: event.OrgID})
	if err != nil {
		return fmt.Errorf("%w: lock audit chain: %v", ErrStore, err)
	}
	if chain.Sequence < 0 || chain.Sequence == math.MaxInt64 {
		return fmt.Errorf("%w: audit chain sequence out of range", ErrStore)
	}
	event.Sequence = uint64(chain.Sequence) + 1
	event.PrevHMAC = chain.RowHmac
	event.RowHMAC = s.computeRowHMAC(event)
	sequence, err := auditSequenceInt64(event.Sequence)
	if err != nil {
		return err
	}
	if err := qtx.AdvanceAuditChainState(ctx, store.AdvanceAuditChainStateParams{
		OrgID:    event.OrgID,
		Sequence: sequence,
		RowHmac:  event.RowHMAC,
	}); err != nil {
		return fmt.Errorf("%w: advance audit chain: %v", ErrStore, err)
	}
	rowJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("%w: marshal audit row: %v", ErrStore, err)
	}
	if err := qtx.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		OrgID:         event.OrgID,
		Sequence:      sequence,
		EventID:       event.EventID,
		RecordedAt:    pgtype.Timestamptz{Time: event.RecordedAt, Valid: true},
		EventDate:     event.EventDate,
		IngestedAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		SchemaVersion: event.SchemaVersion,
		PayloadJson:   detailJSON,
		RowJson:       string(rowJSON),
		PrevHmac:      event.PrevHMAC,
		RowHmac:       event.RowHMAC,
		HmacKeyID:     event.HMACKeyID,
	}); err != nil {
		return fmt.Errorf("%w: stage audit event: %v", ErrStore, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: commit audit chain: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) insertAuditClickHouse(ctx context.Context, event *AuditEvent) error {
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.audit_events")
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

func (s *Service) insertAuditDetailClickHouse(ctx context.Context, event *AuditEvent, detailJSON string) error {
	detail := AuditEventDetail{
		EventID:      event.EventID,
		OrgID:        event.OrgID,
		EventDate:    event.EventDate,
		DetailJSON:   detailJSON,
		DetailSHA256: event.DetailSHA256,
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.audit_event_details")
	if err != nil {
		return fmt.Errorf("%w: prepare audit detail insert: %v", ErrStore, err)
	}
	if err := batch.AppendStruct(&detail); err != nil {
		return fmt.Errorf("%w: append audit detail: %v", ErrStore, err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("%w: send audit detail: %v", ErrStore, err)
	}
	return nil
}

// projectAuditEvent keeps the Postgres outbox row pending until ClickHouse has the event and its detail payload.
func (s *Service) projectAuditEvent(ctx context.Context, q *store.Queries, event *AuditEvent, detailJSON string) error {
	projected, err := s.auditEventProjected(ctx, event.EventID)
	if err != nil {
		return err
	}
	if !projected {
		if err := s.insertAuditClickHouse(ctx, event); err != nil {
			return err
		}
	}
	if hasAuditDetail(detailJSON) {
		detailProjected, err := s.auditDetailProjected(ctx, event.EventID)
		if err != nil {
			return err
		}
		if !detailProjected {
			if err := s.insertAuditDetailClickHouse(ctx, event, detailJSON); err != nil {
				return err
			}
		}
	}
	sequence, err := auditSequenceInt64(event.Sequence)
	if err != nil {
		return err
	}
	return s.markAuditEventProjected(ctx, q, event.OrgID, sequence)
}

func (s *Service) auditEventProjected(ctx context.Context, eventID uuid.UUID) (bool, error) {
	var found uint8
	err := s.CH.QueryRow(ctx, `
		SELECT 1
		FROM verself.audit_events
		WHERE event_id = $1
		LIMIT 1
	`, eventID).Scan(&found)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("%w: check audit projection: %v", ErrStore, err)
	}
	return true, nil
}

func (s *Service) auditDetailProjected(ctx context.Context, eventID uuid.UUID) (bool, error) {
	var found uint8
	err := s.CH.QueryRow(ctx, `
		SELECT 1
		FROM verself.audit_event_details
		WHERE event_id = $1
		LIMIT 1
	`, eventID).Scan(&found)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("%w: check audit detail projection: %v", ErrStore, err)
	}
	return true, nil
}

func (s *Service) markAuditEventProjected(ctx context.Context, q *store.Queries, orgID string, sequence int64) error {
	affected, err := q.MarkAuditEventProjected(ctx, store.MarkAuditEventProjectedParams{
		OrgID:    orgID,
		Sequence: sequence,
	})
	if err != nil {
		return fmt.Errorf("%w: mark audit projection: %v", ErrStore, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: mark audit projection: missing staging row", ErrStore)
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
	ascending := strings.EqualFold(strings.TrimSpace(filters.Order), "asc")
	// Cursor direction + ORDER BY flip together so forward pagination walks
	// the same direction the user is reading.
	var query string
	if ascending {
		query = auditEventSelectSQL() + `
		FROM verself.audit_events
		WHERE org_id = $1
		  AND ($2 = '' OR event_source = $2)
		  AND ($3 = '' OR event_name = $3)
		  AND ($4 = '' OR outcome = $4)
		  AND ($5 = '' OR actor_id = $5)
		  AND ($6 = '' OR target_id = $6)
		  AND ($7 = '' OR target_type = $7)
		  AND ($8 = '' OR audit_event = $8)
		  AND ($9 = '' OR credential_id = $9)
		  AND ($10 = '' OR target_resource_name = $10)
		  AND ($11 = 0 OR (recorded_at, sequence) > ($12, $13))
		ORDER BY recorded_at ASC, sequence ASC
		LIMIT $14
	`
	} else {
		query = auditEventSelectSQL() + `
		FROM verself.audit_events
		WHERE org_id = $1
		  AND ($2 = '' OR event_source = $2)
		  AND ($3 = '' OR event_name = $3)
		  AND ($4 = '' OR outcome = $4)
		  AND ($5 = '' OR actor_id = $5)
		  AND ($6 = '' OR target_id = $6)
		  AND ($7 = '' OR target_type = $7)
		  AND ($8 = '' OR audit_event = $8)
		  AND ($9 = '' OR credential_id = $9)
		  AND ($10 = '' OR target_resource_name = $10)
		  AND ($11 = 0 OR (recorded_at, sequence) < ($12, $13))
		ORDER BY recorded_at DESC, sequence DESC
		LIMIT $14
	`
	}
	rows, err := s.CH.Query(ctx, query, orgID, filters.EventSource, filters.EventName, filters.Outcome, filters.ActorID, filters.TargetID, filters.TargetType, filters.AuditEvent, filters.CredentialID, filters.TargetResourceName, cursorEnabled, cursor.RecordedAt, cursor.Sequence, limit+1)
	if err != nil {
		return AuditListPage{}, fmt.Errorf("%w: list audit events: %v", ErrStore, err)
	}
	defer func() { _ = rows.Close() }()
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
	span.SetAttributes(attribute.String("verself.org_id", orgID), attribute.Int("verself.audit_event_count", len(events)))
	return AuditListPage{Events: events, NextCursor: nextCursor, Limit: limit}, nil
}

func auditEventSelectSQL() string {
	return `
		SELECT
			recorded_at, event_date, schema_version, event_id, org_id, sequence,
			event_source, event_name, audit_event,
			actor_type, actor_id, credential_id,
			target_type, target_id, target_resource_name, permission,
			outcome, error_code, trace_id, detail_sha256,
			prev_hmac, row_hmac, hmac_key_id
	`
}

func (s *Service) computeRowHMAC(event *AuditEvent) string {
	mac := hmac.New(sha256.New, s.HMACKey)
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%s\n%d\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",
		event.SchemaVersion,
		event.OrgID,
		event.EventID.String(),
		event.Sequence,
		event.PrevHMAC,
		event.RecordedAt.Format(time.RFC3339Nano),
		event.EventSource,
		event.EventName,
		event.ActorType,
		event.ActorID,
		event.TargetResourceName,
		event.Outcome,
		event.DetailSHA256,
		event.TraceID,
	)
	return hex.EncodeToString(mac.Sum(nil))
}

// PostgreSQL BIGINT is signed, so generated query calls must reject wrapped audit sequences.
func auditSequenceInt64(sequence uint64) (int64, error) {
	if sequence > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("%w: audit sequence out of range", ErrStore)
	}
	return int64(sequence), nil
}

func auditDetail(detail map[string]any) (string, string, error) {
	if len(detail) == 0 {
		return emptyDetailJSON, hashTextRaw(emptyDetailJSON), nil
	}
	payload, err := canonicalJSON(detail)
	if err != nil {
		return "", "", fmt.Errorf("%w: canonical audit detail: %v", ErrInvalidArgument, err)
	}
	return string(payload), hashTextRaw(string(payload)), nil
}

func hasAuditDetail(detailJSON string) bool {
	detailJSON = strings.TrimSpace(detailJSON)
	return detailJSON != "" && detailJSON != emptyDetailJSON
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
	return hashTextRaw(value)
}

func hashTextRaw(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type auditCursor struct {
	RecordedAt time.Time
	Sequence   uint64
}

// Cursor wire format is "<rfc3339nano>.<sequence>".
func makeCursor(recordedAt time.Time, sequence uint64) string {
	return recordedAt.UTC().Format(time.RFC3339Nano) + "." + strconv.FormatUint(sequence, 10)
}

func parseCursor(value string) (auditCursor, error) {
	sep := strings.LastIndex(value, ".")
	if sep <= 0 || sep == len(value)-1 {
		return auditCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	recordedAt, err := time.Parse(time.RFC3339Nano, value[:sep])
	if err != nil {
		return auditCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	sequence, err := strconv.ParseUint(value[sep+1:], 10, 64)
	if err != nil {
		return auditCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	if recordedAt.IsZero() {
		return auditCursor{}, fmt.Errorf("%w: invalid cursor", ErrInvalidArgument)
	}
	return auditCursor{RecordedAt: recordedAt.UTC(), Sequence: sequence}, nil
}
