package objectstorage

import "context"

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
	RouteTemplate         string         `json:"route_template,omitempty"`
	HTTPMethod            string         `json:"http_method,omitempty"`
	HTTPStatus            uint16         `json:"http_status,omitempty"`
	DurationMS            float64        `json:"duration_ms,omitempty"`
	ActorType             string         `json:"actor_type"`
	ActorID               string         `json:"actor_id"`
	ActorDisplay          string         `json:"actor_display,omitempty"`
	CredentialID          string         `json:"credential_id,omitempty"`
	CredentialName        string         `json:"credential_name,omitempty"`
	CredentialFingerprint string         `json:"credential_fingerprint,omitempty"`
	AuthMethod            string         `json:"auth_method,omitempty"`
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
	Permission            string         `json:"permission"`
	Action                string         `json:"action"`
	OrgScope              string         `json:"org_scope"`
	Decision              string         `json:"decision,omitempty"`
	Result                string         `json:"result"`
	ContentSHA256         string         `json:"content_sha256,omitempty"`
	ErrorClass            string         `json:"error_class,omitempty"`
	ErrorMessage          string         `json:"error_message,omitempty"`
	Payload               map[string]any `json:"payload,omitempty"`
}

type AuditSink func(ctx context.Context, record AuditRecord) error
