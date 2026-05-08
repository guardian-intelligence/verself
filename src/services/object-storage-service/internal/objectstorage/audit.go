package objectstorage

import "context"

type AuditRecord struct {
	OrgID        string         `json:"org_id"`
	EventSource  string         `json:"event_source"`
	EventName    string         `json:"event_name"`
	AuditEvent   string         `json:"audit_event"`
	ActorType    string         `json:"actor_type"`
	ActorID      string         `json:"actor_id"`
	CredentialID string         `json:"credential_id,omitempty"`
	Permission   string         `json:"permission"`
	TargetType   string         `json:"target_type"`
	TargetID     string         `json:"target_id,omitempty"`
	Outcome      string         `json:"outcome"`
	ErrorCode    string         `json:"error_code,omitempty"`
	TraceID      string         `json:"trace_id,omitempty"`
	Detail       map[string]any `json:"detail,omitempty"`
}

type AuditSink func(ctx context.Context, record AuditRecord) error
