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
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	AuditSchemaVersion = "2026-04-18.v2"
	zeroHMAC           = "0000000000000000000000000000000000000000000000000000000000000000"
)

type AuditRecord struct {
	SchemaVersion         string         `json:"schema_version,omitempty"`
	OrgID                 string         `json:"org_id"`
	Environment           string         `json:"environment,omitempty"`
	SourceProductArea     string         `json:"source_product_area"`
	ServiceName           string         `json:"service_name"`
	ServiceVersion        string         `json:"service_version,omitempty"`
	WriterInstanceID      string         `json:"writer_instance_id,omitempty"`
	RequestID             string         `json:"request_id,omitempty"`
	TraceID               string         `json:"trace_id,omitempty"`
	SpanID                string         `json:"span_id,omitempty"`
	ParentSpanID          string         `json:"parent_span_id,omitempty"`
	RouteTemplate         string         `json:"route_template,omitempty"`
	HTTPMethod            string         `json:"http_method,omitempty"`
	HTTPStatus            uint16         `json:"http_status,omitempty"`
	DurationMS            float64        `json:"duration_ms,omitempty"`
	IdempotencyKeyHash    string         `json:"idempotency_key_hash,omitempty"`
	ActorType             string         `json:"actor_type"`
	ActorID               string         `json:"actor_id"`
	ActorDisplay          string         `json:"actor_display,omitempty"`
	ActorOrgID            string         `json:"actor_org_id,omitempty"`
	ActorOwnerID          string         `json:"actor_owner_id,omitempty"`
	ActorOwnerDisplay     string         `json:"actor_owner_display,omitempty"`
	CredentialID          string         `json:"credential_id,omitempty"`
	CredentialName        string         `json:"credential_name,omitempty"`
	CredentialFingerprint string         `json:"credential_fingerprint,omitempty"`
	AuthMethod            string         `json:"auth_method,omitempty"`
	AuthAssuranceLevel    string         `json:"auth_assurance_level,omitempty"`
	MFAPresent            bool           `json:"mfa_present,omitempty"`
	SessionIDHash         string         `json:"session_id_hash,omitempty"`
	DelegationChain       string         `json:"delegation_chain,omitempty"`
	ActorSPIFFEID         string         `json:"actor_spiffe_id,omitempty"`
	OperationID           string         `json:"operation_id"`
	AuditEvent            string         `json:"audit_event"`
	OperationDisplay      string         `json:"operation_display,omitempty"`
	OperationType         string         `json:"operation_type"`
	EventCategory         string         `json:"event_category"`
	RiskLevel             string         `json:"risk_level"`
	DataClassification    string         `json:"data_classification,omitempty"`
	RateLimitClass        string         `json:"rate_limit_class,omitempty"`
	TargetKind            string         `json:"target_kind"`
	TargetID              string         `json:"target_id,omitempty"`
	TargetDisplay         string         `json:"target_display,omitempty"`
	TargetScope           string         `json:"target_scope,omitempty"`
	TargetPathHash        string         `json:"target_path_hash,omitempty"`
	ResourceOwnerOrgID    string         `json:"resource_owner_org_id,omitempty"`
	ResourceRegion        string         `json:"resource_region,omitempty"`
	Permission            string         `json:"permission"`
	Action                string         `json:"action"`
	OrgScope              string         `json:"org_scope"`
	PolicyID              string         `json:"policy_id,omitempty"`
	PolicyVersion         string         `json:"policy_version,omitempty"`
	PolicyHash            string         `json:"policy_hash,omitempty"`
	MatchedRule           string         `json:"matched_rule,omitempty"`
	Decision              string         `json:"decision,omitempty"`
	Result                string         `json:"result"`
	DenialReason          string         `json:"denial_reason,omitempty"`
	TrustClass            string         `json:"trust_class,omitempty"`
	ClientIP              string         `json:"client_ip,omitempty"`
	ClientIPVersion       string         `json:"client_ip_version,omitempty"`
	ClientIPHash          string         `json:"client_ip_hash,omitempty"`
	IPChain               string         `json:"ip_chain,omitempty"`
	IPChainTrustedHops    uint8          `json:"ip_chain_trusted_hops,omitempty"`
	UserAgentRaw          string         `json:"user_agent_raw,omitempty"`
	UserAgentHash         string         `json:"user_agent_hash,omitempty"`
	RefererOrigin         string         `json:"referer_origin,omitempty"`
	Origin                string         `json:"origin,omitempty"`
	Host                  string         `json:"host,omitempty"`
	TLSSubjectHash        string         `json:"tls_subject_hash,omitempty"`
	MTLSSubjectHash       string         `json:"mtls_subject_hash,omitempty"`
	GeoCountry            string         `json:"geo_country,omitempty"`
	GeoRegion             string         `json:"geo_region,omitempty"`
	GeoCity               string         `json:"geo_city,omitempty"`
	ASN                   uint32         `json:"asn,omitempty"`
	ASNOrg                string         `json:"asn_org,omitempty"`
	NetworkType           string         `json:"network_type,omitempty"`
	GeoSource             string         `json:"geo_source,omitempty"`
	GeoSourceVersion      string         `json:"geo_source_version,omitempty"`
	ChangedFields         string         `json:"changed_fields,omitempty"`
	BeforeHash            string         `json:"before_hash,omitempty"`
	AfterHash             string         `json:"after_hash,omitempty"`
	ContentSHA256         string         `json:"content_sha256,omitempty"`
	ArtifactSHA256        string         `json:"artifact_sha256,omitempty"`
	ArtifactBytes         uint64         `json:"artifact_bytes,omitempty"`
	ErrorCode             string         `json:"error_code,omitempty"`
	ErrorClass            string         `json:"error_class,omitempty"`
	ErrorMessage          string         `json:"error_message,omitempty"`
	SecretMount           string         `json:"secret_mount,omitempty"`
	SecretPathHash        string         `json:"secret_path_hash,omitempty"`
	SecretVersion         uint64         `json:"secret_version,omitempty"`
	SecretOperation       string         `json:"secret_operation,omitempty"`
	LeaseIDHash           string         `json:"lease_id_hash,omitempty"`
	LeaseTTLSeconds       uint64         `json:"lease_ttl_seconds,omitempty"`
	KeyID                 string         `json:"key_id,omitempty"`
	OpenBaoRequestID      string         `json:"openbao_request_id,omitempty"`
	OpenBaoAccessorHash   string         `json:"openbao_accessor_hash,omitempty"`
	RetentionClass        string         `json:"retention_class,omitempty"`
	LegalHold             bool           `json:"legal_hold,omitempty"`
	HMACKeyID             string         `json:"hmac_key_id,omitempty"`
	Payload               map[string]any `json:"payload,omitempty"`
	RecordedAt            time.Time      `json:"recorded_at,omitempty"`
}

type AuditEvent struct {
	RecordedAt            time.Time `ch:"recorded_at"`
	EventDate             time.Time `ch:"event_date"`
	IngestedAt            time.Time `ch:"ingested_at"`
	SchemaVersion         string    `ch:"schema_version"`
	EventID               uuid.UUID `ch:"event_id"`
	OrgID                 string    `ch:"org_id"`
	Environment           string    `ch:"environment"`
	SourceProductArea     string    `ch:"source_product_area"`
	ServiceName           string    `ch:"service_name"`
	ServiceVersion        string    `ch:"service_version"`
	WriterInstanceID      string    `ch:"writer_instance_id"`
	RequestID             string    `ch:"request_id"`
	TraceID               string    `ch:"trace_id"`
	SpanID                string    `ch:"span_id"`
	ParentSpanID          string    `ch:"parent_span_id"`
	RouteTemplate         string    `ch:"route_template"`
	HTTPMethod            string    `ch:"http_method"`
	HTTPStatus            uint16    `ch:"http_status"`
	DurationMS            float64   `ch:"duration_ms"`
	IdempotencyKeyHash    string    `ch:"idempotency_key_hash"`
	ActorType             string    `ch:"actor_type"`
	ActorID               string    `ch:"actor_id"`
	ActorDisplay          string    `ch:"actor_display"`
	ActorOrgID            string    `ch:"actor_org_id"`
	ActorOwnerID          string    `ch:"actor_owner_id"`
	ActorOwnerDisplay     string    `ch:"actor_owner_display"`
	CredentialID          string    `ch:"credential_id"`
	CredentialName        string    `ch:"credential_name"`
	CredentialFingerprint string    `ch:"credential_fingerprint"`
	AuthMethod            string    `ch:"auth_method"`
	AuthAssuranceLevel    string    `ch:"auth_assurance_level"`
	MFAPresent            uint8     `ch:"mfa_present"`
	SessionIDHash         string    `ch:"session_id_hash"`
	DelegationChain       string    `ch:"delegation_chain"`
	ActorSPIFFEID         string    `ch:"actor_spiffe_id"`
	OperationID           string    `ch:"operation_id"`
	AuditEvent            string    `ch:"audit_event"`
	OperationDisplay      string    `ch:"operation_display"`
	OperationType         string    `ch:"operation_type"`
	EventCategory         string    `ch:"event_category"`
	RiskLevel             string    `ch:"risk_level"`
	DataClassification    string    `ch:"data_classification"`
	RateLimitClass        string    `ch:"rate_limit_class"`
	TargetKind            string    `ch:"target_kind"`
	TargetID              string    `ch:"target_id"`
	TargetDisplay         string    `ch:"target_display"`
	TargetScope           string    `ch:"target_scope"`
	TargetPathHash        string    `ch:"target_path_hash"`
	ResourceOwnerOrgID    string    `ch:"resource_owner_org_id"`
	ResourceRegion        string    `ch:"resource_region"`
	Permission            string    `ch:"permission"`
	Action                string    `ch:"action"`
	OrgScope              string    `ch:"org_scope"`
	PolicyID              string    `ch:"policy_id"`
	PolicyVersion         string    `ch:"policy_version"`
	PolicyHash            string    `ch:"policy_hash"`
	MatchedRule           string    `ch:"matched_rule"`
	Decision              string    `ch:"decision"`
	Result                string    `ch:"result"`
	DenialReason          string    `ch:"denial_reason"`
	TrustClass            string    `ch:"trust_class"`
	ClientIP              string    `ch:"client_ip"`
	ClientIPVersion       string    `ch:"client_ip_version"`
	ClientIPHash          string    `ch:"client_ip_hash"`
	IPChain               string    `ch:"ip_chain"`
	IPChainTrustedHops    uint8     `ch:"ip_chain_trusted_hops"`
	UserAgentRaw          string    `ch:"user_agent_raw"`
	UserAgentHash         string    `ch:"user_agent_hash"`
	RefererOrigin         string    `ch:"referer_origin"`
	Origin                string    `ch:"origin"`
	Host                  string    `ch:"host"`
	TLSSubjectHash        string    `ch:"tls_subject_hash"`
	MTLSSubjectHash       string    `ch:"mtls_subject_hash"`
	GeoCountry            string    `ch:"geo_country"`
	GeoRegion             string    `ch:"geo_region"`
	GeoCity               string    `ch:"geo_city"`
	ASN                   uint32    `ch:"asn"`
	ASNOrg                string    `ch:"asn_org"`
	NetworkType           string    `ch:"network_type"`
	GeoSource             string    `ch:"geo_source"`
	GeoSourceVersion      string    `ch:"geo_source_version"`
	ChangedFields         string    `ch:"changed_fields"`
	BeforeHash            string    `ch:"before_hash"`
	AfterHash             string    `ch:"after_hash"`
	ContentSHA256         string    `ch:"content_sha256"`
	ArtifactSHA256        string    `ch:"artifact_sha256"`
	ArtifactBytes         uint64    `ch:"artifact_bytes"`
	ErrorCode             string    `ch:"error_code"`
	ErrorClass            string    `ch:"error_class"`
	ErrorMessage          string    `ch:"error_message"`
	SecretMount           string    `ch:"secret_mount"`
	SecretPathHash        string    `ch:"secret_path_hash"`
	SecretVersion         uint64    `ch:"secret_version"`
	SecretOperation       string    `ch:"secret_operation"`
	LeaseIDHash           string    `ch:"lease_id_hash"`
	LeaseTTLSeconds       uint64    `ch:"lease_ttl_seconds"`
	KeyID                 string    `ch:"key_id"`
	OpenBaoRequestID      string    `ch:"openbao_request_id"`
	OpenBaoAccessorHash   string    `ch:"openbao_accessor_hash"`
	PayloadJSON           string    `ch:"payload_json"`
	Sequence              uint64    `ch:"sequence"`
	PrevHMAC              string    `ch:"prev_hmac"`
	RowHMAC               string    `ch:"row_hmac"`
	HMACKeyID             string    `ch:"hmac_key_id"`
	RetentionClass        string    `ch:"retention_class"`
	LegalHold             uint8     `ch:"legal_hold"`
}

type AuditListFilters struct {
	Limit             int
	Cursor            string
	Order             string // "desc" (default) or "asc"; controls (recorded_at, sequence) ordering.
	ActorID           string
	AuditEvent        string
	CredentialID      string
	HighRisk          bool
	OperationID       string
	OperationType     string
	Result            string
	RiskLevel         string
	ServiceName       string
	SourceProductArea string
	TargetID          string
	TargetKind        string
}

type AuditListPage struct {
	Events     []AuditEvent
	NextCursor string
	Limit      int
}

type auditExecQuerier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type auditPendingRow struct {
	RowJSON string
}

func (s *Service) RecordAuditEvent(ctx context.Context, record AuditRecord) (*AuditEvent, error) {
	ctx, span := tracer.Start(ctx, "governance.audit.record")
	defer span.End()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	record = s.normalizeAuditRecord(ctx, record)
	if record.OrgID == "" || record.SourceProductArea == "" || record.ServiceName == "" || record.OperationID == "" || record.AuditEvent == "" || record.Result == "" {
		return nil, fmt.Errorf("%w: org_id, source_product_area, service_name, operation_id, audit_event, and result are required", ErrInvalidArgument)
	}
	if record.ActorType == "" || record.ActorID == "" || record.TargetKind == "" || record.OperationType == "" || record.EventCategory == "" || record.RiskLevel == "" {
		return nil, fmt.Errorf("%w: actor, target, operation_type, event_category, and risk_level are required", ErrInvalidArgument)
	}
	payload, err := canonicalJSON(record)
	if err != nil {
		return nil, fmt.Errorf("%w: canonical audit payload: %v", ErrInvalidArgument, err)
	}
	contentHash := sha256.Sum256(payload)
	if record.ContentSHA256 == "" {
		record.ContentSHA256 = hex.EncodeToString(contentHash[:])
	}
	event := eventFromAuditRecord(record, string(payload))
	if err := s.assignAuditSequence(ctx, event); err != nil {
		return nil, err
	}
	if err := s.projectAuditEvent(ctx, s.PG, event); err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("verself.org_id", event.OrgID),
		attribute.String("verself.audit_event", event.AuditEvent),
		attribute.String("verself.actor_type", event.ActorType),
		attribute.String("verself.risk_level", event.RiskLevel),
		attribute.Int64("verself.audit_sequence", int64(event.Sequence)),
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

	rows, err := tx.Query(ctx, `
		SELECT row_json
		FROM governance_audit_events
		WHERE projected_at IS NULL
		ORDER BY recorded_at ASC, sequence ASC, event_id ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("%w: list pending audit events: %v", ErrStore, err)
	}
	defer rows.Close()

	pending := make([]AuditEvent, 0, limit)
	for rows.Next() {
		var row auditPendingRow
		if err := rows.Scan(&row.RowJSON); err != nil {
			return 0, fmt.Errorf("%w: scan pending audit event: %v", ErrStore, err)
		}
		var event AuditEvent
		if err := json.Unmarshal([]byte(row.RowJSON), &event); err != nil {
			return 0, fmt.Errorf("%w: unmarshal pending audit event: %v", ErrStore, err)
		}
		pending = append(pending, event)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("%w: pending audit rows: %v", ErrStore, err)
	}
	rows.Close()
	projected := 0
	for i := range pending {
		if err := s.projectAuditEvent(ctx, tx, &pending[i]); err != nil {
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
	record.Environment = firstNonEmpty(record.Environment, s.Environment, "default")
	record.SourceProductArea = strings.TrimSpace(record.SourceProductArea)
	record.ServiceName = strings.TrimSpace(record.ServiceName)
	record.ServiceVersion = firstNonEmpty(record.ServiceVersion, s.ServiceVersion)
	record.WriterInstanceID = firstNonEmpty(record.WriterInstanceID, s.WriterInstanceID)
	record.ActorType = firstNonEmpty(record.ActorType, "user")
	record.ActorID = strings.TrimSpace(record.ActorID)
	record.ActorDisplay = strings.TrimSpace(record.ActorDisplay)
	record.ActorOrgID = firstNonEmpty(record.ActorOrgID, record.OrgID)
	record.ActorOwnerID = strings.TrimSpace(record.ActorOwnerID)
	record.ActorOwnerDisplay = strings.TrimSpace(record.ActorOwnerDisplay)
	record.CredentialID = strings.TrimSpace(record.CredentialID)
	record.CredentialName = strings.TrimSpace(record.CredentialName)
	record.CredentialFingerprint = strings.TrimSpace(record.CredentialFingerprint)
	record.OperationID = strings.TrimSpace(record.OperationID)
	record.AuditEvent = strings.TrimSpace(record.AuditEvent)
	record.OperationDisplay = strings.TrimSpace(record.OperationDisplay)
	record.OperationType = strings.TrimSpace(record.OperationType)
	record.EventCategory = strings.TrimSpace(record.EventCategory)
	record.RiskLevel = strings.TrimSpace(record.RiskLevel)
	record.DataClassification = firstNonEmpty(record.DataClassification, "restricted")
	record.TargetKind = strings.TrimSpace(record.TargetKind)
	record.TargetID = strings.TrimSpace(record.TargetID)
	record.TargetDisplay = strings.TrimSpace(record.TargetDisplay)
	record.TargetScope = strings.TrimSpace(record.TargetScope)
	record.TargetPathHash = strings.TrimSpace(record.TargetPathHash)
	record.ResourceOwnerOrgID = firstNonEmpty(record.ResourceOwnerOrgID, record.OrgID)
	record.Permission = strings.TrimSpace(record.Permission)
	record.Action = strings.TrimSpace(record.Action)
	record.OrgScope = strings.TrimSpace(record.OrgScope)
	record.PolicyID = firstNonEmpty(record.PolicyID, record.ServiceName+"."+record.OperationID)
	record.PolicyVersion = firstNonEmpty(record.PolicyVersion, AuditSchemaVersion)
	record.MatchedRule = strings.TrimSpace(record.MatchedRule)
	record.Decision = firstNonEmpty(record.Decision, decisionForResult(record.Result))
	record.Result = strings.TrimSpace(record.Result)
	record.DenialReason = strings.TrimSpace(record.DenialReason)
	record.TrustClass = firstNonEmpty(record.TrustClass, "standard")
	record.ClientIP = strings.TrimSpace(record.ClientIP)
	record.ClientIPVersion = firstNonEmpty(record.ClientIPVersion, ipVersion(record.ClientIP))
	record.ClientIPHash = firstNonEmpty(record.ClientIPHash, hashText(record.ClientIP))
	record.IPChain = strings.TrimSpace(record.IPChain)
	record.UserAgentRaw = sanitizeUserAgent(record.UserAgentRaw)
	record.UserAgentHash = firstNonEmpty(record.UserAgentHash, hashText(record.UserAgentRaw))
	record.RefererOrigin = strings.TrimSpace(record.RefererOrigin)
	record.Origin = strings.TrimSpace(record.Origin)
	record.Host = strings.TrimSpace(record.Host)
	record.AuthMethod = strings.TrimSpace(record.AuthMethod)
	record.AuthAssuranceLevel = strings.TrimSpace(record.AuthAssuranceLevel)
	record.SessionIDHash = strings.TrimSpace(record.SessionIDHash)
	record.DelegationChain = strings.TrimSpace(record.DelegationChain)
	record.ActorSPIFFEID = strings.TrimSpace(record.ActorSPIFFEID)
	record.GeoCountry = strings.TrimSpace(record.GeoCountry)
	record.GeoRegion = strings.TrimSpace(record.GeoRegion)
	record.GeoCity = strings.TrimSpace(record.GeoCity)
	record.ASNOrg = strings.TrimSpace(record.ASNOrg)
	record.NetworkType = strings.TrimSpace(record.NetworkType)
	record.GeoSource = strings.TrimSpace(record.GeoSource)
	record.GeoSourceVersion = strings.TrimSpace(record.GeoSourceVersion)
	record.ErrorCode = strings.TrimSpace(record.ErrorCode)
	record.ErrorClass = strings.TrimSpace(record.ErrorClass)
	record.ErrorMessage = strings.TrimSpace(record.ErrorMessage)
	record.RetentionClass = firstNonEmpty(record.RetentionClass, "audit_events")
	record.HMACKeyID = firstNonEmpty(record.HMACKeyID, s.HMACKeyID, "default")
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now().UTC()
	}
	if record.TraceID == "" {
		if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
			record.TraceID = spanContext.TraceID().String()
		}
	}
	if record.SpanID == "" {
		if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasSpanID() {
			record.SpanID = spanContext.SpanID().String()
		}
	}
	return record
}

func eventFromAuditRecord(record AuditRecord, payloadJSON string) *AuditEvent {
	return &AuditEvent{
		RecordedAt:            record.RecordedAt.UTC(),
		EventDate:             truncateDate(record.RecordedAt.UTC()),
		IngestedAt:            time.Now().UTC(),
		SchemaVersion:         record.SchemaVersion,
		EventID:               uuid.New(),
		OrgID:                 record.OrgID,
		Environment:           record.Environment,
		SourceProductArea:     record.SourceProductArea,
		ServiceName:           record.ServiceName,
		ServiceVersion:        record.ServiceVersion,
		WriterInstanceID:      record.WriterInstanceID,
		RequestID:             record.RequestID,
		TraceID:               record.TraceID,
		SpanID:                record.SpanID,
		ParentSpanID:          record.ParentSpanID,
		RouteTemplate:         record.RouteTemplate,
		HTTPMethod:            strings.ToUpper(record.HTTPMethod),
		HTTPStatus:            record.HTTPStatus,
		DurationMS:            record.DurationMS,
		IdempotencyKeyHash:    record.IdempotencyKeyHash,
		ActorType:             record.ActorType,
		ActorID:               record.ActorID,
		ActorDisplay:          record.ActorDisplay,
		ActorOrgID:            record.ActorOrgID,
		ActorOwnerID:          record.ActorOwnerID,
		ActorOwnerDisplay:     record.ActorOwnerDisplay,
		CredentialID:          record.CredentialID,
		CredentialName:        record.CredentialName,
		CredentialFingerprint: record.CredentialFingerprint,
		AuthMethod:            record.AuthMethod,
		AuthAssuranceLevel:    record.AuthAssuranceLevel,
		MFAPresent:            boolToUInt8(record.MFAPresent),
		SessionIDHash:         record.SessionIDHash,
		DelegationChain:       record.DelegationChain,
		ActorSPIFFEID:         record.ActorSPIFFEID,
		OperationID:           record.OperationID,
		AuditEvent:            record.AuditEvent,
		OperationDisplay:      record.OperationDisplay,
		OperationType:         record.OperationType,
		EventCategory:         record.EventCategory,
		RiskLevel:             record.RiskLevel,
		DataClassification:    record.DataClassification,
		RateLimitClass:        record.RateLimitClass,
		TargetKind:            record.TargetKind,
		TargetID:              record.TargetID,
		TargetDisplay:         record.TargetDisplay,
		TargetScope:           record.TargetScope,
		TargetPathHash:        record.TargetPathHash,
		ResourceOwnerOrgID:    record.ResourceOwnerOrgID,
		ResourceRegion:        record.ResourceRegion,
		Permission:            record.Permission,
		Action:                record.Action,
		OrgScope:              record.OrgScope,
		PolicyID:              record.PolicyID,
		PolicyVersion:         record.PolicyVersion,
		PolicyHash:            firstNonEmpty(record.PolicyHash, hashText(record.PolicyID+"\x00"+record.PolicyVersion)),
		MatchedRule:           record.MatchedRule,
		Decision:              record.Decision,
		Result:                record.Result,
		DenialReason:          record.DenialReason,
		TrustClass:            record.TrustClass,
		ClientIP:              record.ClientIP,
		ClientIPVersion:       record.ClientIPVersion,
		ClientIPHash:          record.ClientIPHash,
		IPChain:               record.IPChain,
		IPChainTrustedHops:    record.IPChainTrustedHops,
		UserAgentRaw:          record.UserAgentRaw,
		UserAgentHash:         record.UserAgentHash,
		RefererOrigin:         record.RefererOrigin,
		Origin:                record.Origin,
		Host:                  record.Host,
		TLSSubjectHash:        record.TLSSubjectHash,
		MTLSSubjectHash:       record.MTLSSubjectHash,
		GeoCountry:            record.GeoCountry,
		GeoRegion:             record.GeoRegion,
		GeoCity:               record.GeoCity,
		ASN:                   record.ASN,
		ASNOrg:                record.ASNOrg,
		NetworkType:           record.NetworkType,
		GeoSource:             record.GeoSource,
		GeoSourceVersion:      record.GeoSourceVersion,
		ChangedFields:         record.ChangedFields,
		BeforeHash:            record.BeforeHash,
		AfterHash:             record.AfterHash,
		ContentSHA256:         record.ContentSHA256,
		ArtifactSHA256:        record.ArtifactSHA256,
		ArtifactBytes:         record.ArtifactBytes,
		ErrorCode:             record.ErrorCode,
		ErrorClass:            record.ErrorClass,
		ErrorMessage:          record.ErrorMessage,
		SecretMount:           record.SecretMount,
		SecretPathHash:        record.SecretPathHash,
		SecretVersion:         record.SecretVersion,
		SecretOperation:       record.SecretOperation,
		LeaseIDHash:           record.LeaseIDHash,
		LeaseTTLSeconds:       record.LeaseTTLSeconds,
		KeyID:                 record.KeyID,
		OpenBaoRequestID:      record.OpenBaoRequestID,
		OpenBaoAccessorHash:   record.OpenBaoAccessorHash,
		PayloadJSON:           payloadJSON,
		HMACKeyID:             record.HMACKeyID,
		RetentionClass:        record.RetentionClass,
		LegalHold:             boolToUInt8(record.LegalHold),
	}
}

func (s *Service) assignAuditSequence(ctx context.Context, event *AuditEvent) error {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: begin audit chain tx: %v", ErrStore, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

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
	rowJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("%w: marshal audit row: %v", ErrStore, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO governance_audit_events (
			org_id, sequence, event_id, recorded_at, event_date, ingested_at,
			schema_version, payload_json, row_json, prev_hmac, row_hmac, hmac_key_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, event.OrgID, int64(event.Sequence), event.EventID, event.RecordedAt, event.EventDate, event.IngestedAt,
		event.SchemaVersion, event.PayloadJSON, string(rowJSON), event.PrevHMAC, event.RowHMAC, event.HMACKeyID); err != nil {
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

// projectAuditEvent keeps the Postgres outbox row pending until ClickHouse has the event.
func (s *Service) projectAuditEvent(ctx context.Context, exec auditExecQuerier, event *AuditEvent) error {
	projected, err := s.auditEventProjected(ctx, event.EventID)
	if err != nil {
		return err
	}
	if !projected {
		if err := s.insertAuditClickHouse(ctx, event); err != nil {
			return err
		}
	}
	return s.markAuditEventProjected(ctx, exec, event.OrgID, int64(event.Sequence))
}

func (s *Service) auditEventProjected(ctx context.Context, eventID uuid.UUID) (bool, error) {
	var found int
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

func (s *Service) markAuditEventProjected(ctx context.Context, exec auditExecQuerier, orgID string, sequence int64) error {
	tag, err := exec.Exec(ctx, `
		UPDATE governance_audit_events
		SET projected_at = COALESCE(projected_at, now())
		WHERE org_id = $1
		  AND sequence = $2
	`, orgID, sequence)
	if err != nil {
		return fmt.Errorf("%w: mark audit projection: %v", ErrStore, err)
	}
	if tag.RowsAffected() == 0 {
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
	highRisk := uint8(0)
	if filters.HighRisk {
		highRisk = 1
	}
	ascending := strings.EqualFold(strings.TrimSpace(filters.Order), "asc")
	// Cursor direction + ORDER BY flip together so forward pagination walks
	// the same direction the user is reading. Composing DESC with '<' walks
	// newest→oldest; ASC with '>' walks oldest→newest.
	var query string
	if ascending {
		query = auditEventSelectSQL() + `
		FROM verself.audit_events
		WHERE org_id = $1
		  AND ($2 = '' OR service_name = $2)
		  AND ($3 = '' OR operation_id = $3)
		  AND ($4 = '' OR result = $4)
		  AND ($5 = '' OR actor_id = $5)
		  AND ($6 = '' OR target_id = $6)
		  AND ($7 = '' OR target_kind = $7)
		  AND ($8 = '' OR operation_type = $8)
		  AND ($9 = '' OR risk_level = $9)
		  AND ($10 = '' OR source_product_area = $10)
		  AND ($11 = '' OR audit_event = $11)
		  AND ($12 = '' OR credential_id = $12)
		  AND ($13 = 0 OR risk_level IN ('high', 'critical') OR operation_type IN ('write', 'delete', 'export') OR result IN ('denied', 'error'))
		  AND ($14 = 0 OR (recorded_at, sequence) > ($15, $16))
		ORDER BY recorded_at ASC, sequence ASC
		LIMIT $17
	`
	} else {
		query = auditEventSelectSQL() + `
		FROM verself.audit_events
		WHERE org_id = $1
		  AND ($2 = '' OR service_name = $2)
		  AND ($3 = '' OR operation_id = $3)
		  AND ($4 = '' OR result = $4)
		  AND ($5 = '' OR actor_id = $5)
		  AND ($6 = '' OR target_id = $6)
		  AND ($7 = '' OR target_kind = $7)
		  AND ($8 = '' OR operation_type = $8)
		  AND ($9 = '' OR risk_level = $9)
		  AND ($10 = '' OR source_product_area = $10)
		  AND ($11 = '' OR audit_event = $11)
		  AND ($12 = '' OR credential_id = $12)
		  AND ($13 = 0 OR risk_level IN ('high', 'critical') OR operation_type IN ('write', 'delete', 'export') OR result IN ('denied', 'error'))
		  AND ($14 = 0 OR (recorded_at, sequence) < ($15, $16))
		ORDER BY recorded_at DESC, sequence DESC
		LIMIT $17
	`
	}
	rows, err := s.CH.Query(ctx, query, orgID, filters.ServiceName, filters.OperationID, filters.Result, filters.ActorID, filters.TargetID, filters.TargetKind, filters.OperationType, filters.RiskLevel, filters.SourceProductArea, filters.AuditEvent, filters.CredentialID, highRisk, cursorEnabled, cursor.RecordedAt, cursor.Sequence, limit+1)
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
	span.SetAttributes(attribute.String("verself.org_id", orgID), attribute.Int("verself.audit_event_count", len(events)))
	return AuditListPage{Events: events, NextCursor: nextCursor, Limit: limit}, nil
}

func auditEventSelectSQL() string {
	return `
		SELECT
			recorded_at, event_date, ingested_at, schema_version, event_id, org_id,
			environment, source_product_area, service_name, service_version, writer_instance_id,
			request_id, trace_id, span_id, parent_span_id, route_template, http_method, http_status,
			duration_ms, idempotency_key_hash,
			actor_type, actor_id, actor_display, actor_org_id, actor_owner_id, actor_owner_display,
			credential_id, credential_name, credential_fingerprint, auth_method, auth_assurance_level,
			mfa_present, session_id_hash, delegation_chain, actor_spiffe_id,
			operation_id, audit_event, operation_display, operation_type, event_category, risk_level,
			data_classification, rate_limit_class,
			target_kind, target_id, target_display, target_scope, target_path_hash, resource_owner_org_id,
			resource_region,
			permission, action, org_scope, policy_id, policy_version, policy_hash, matched_rule, decision,
			result, denial_reason, trust_class,
			client_ip, client_ip_version, client_ip_hash, ip_chain, ip_chain_trusted_hops, user_agent_raw,
			user_agent_hash, referer_origin, origin, host, tls_subject_hash, mtls_subject_hash,
			geo_country, geo_region, geo_city, asn, asn_org, network_type, geo_source, geo_source_version,
			changed_fields, before_hash, after_hash, content_sha256, artifact_sha256, artifact_bytes,
			error_code, error_class, error_message,
			secret_mount, secret_path_hash, secret_version, secret_operation, lease_id_hash, lease_ttl_seconds,
			key_id, openbao_request_id, openbao_accessor_hash,
			payload_json, sequence, prev_hmac, row_hmac, hmac_key_id, retention_class, legal_hold
	`
}

func (s *Service) computeRowHMAC(event *AuditEvent) string {
	mac := hmac.New(sha256.New, s.HMACKey)
	fmt.Fprintf(mac, "%s\n%s\n%s\n%d\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",
		event.SchemaVersion,
		event.OrgID,
		event.EventID.String(),
		event.Sequence,
		event.PrevHMAC,
		event.RecordedAt.Format(time.RFC3339Nano),
		event.ServiceName,
		event.OperationID,
		event.ActorType,
		event.ActorID,
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

func sanitizeUserAgent(value string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		switch r {
		case '\x00', '\r', '\n', '\t':
			return -1
		default:
			return r
		}
	}, value))
	if len(value) > 512 {
		return value[:512]
	}
	return value
}

func ipVersion(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	if ip.To4() != nil {
		return "ipv4"
	}
	return "ipv6"
}

func decisionForResult(result string) string {
	switch strings.TrimSpace(result) {
	case "allowed":
		return "allow"
	case "denied":
		return "deny"
	case "error":
		return "error"
	default:
		return ""
	}
}

func boolToUInt8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
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

// Cursor wire format is "<rfc3339nano>.<sequence>" — URL-safe ASCII with no
// extra encoding. Pre-release hard cutover from the previous hex(json) shape;
// stored cursors from older pages are rejected as invalid on next click.
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
