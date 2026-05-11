package dto

type GovernanceAuditEvent struct {
	EventID            string       `json:"event_id" doc:"Audit event UUID."`
	RecordedAt         string       `json:"recorded_at" format:"date-time" doc:"UTC time when the event was recorded."`
	OrgID              string       `json:"org_id" doc:"Organization ID derived from the caller's token."`
	Sequence           string       `json:"sequence" doc:"Per-organization tamper-evident sequence number."`
	EventSource        string       `json:"event_source" doc:"Service or system that recorded the event."`
	EventName          string       `json:"event_name" doc:"Stable operation name."`
	AuditEvent         string       `json:"audit_event" doc:"Stable audit event name."`
	ActorType          string       `json:"actor_type" doc:"Actor class, for example user, service_account, workload, or service."`
	ActorID            string       `json:"actor_id" doc:"Exact authenticated actor ID."`
	CredentialID       string       `json:"credential_id,omitempty" doc:"Credential ID used to authenticate the actor when present."`
	TargetType         string       `json:"target_type" doc:"Resource type being authorized."`
	TargetID           string       `json:"target_id,omitempty" doc:"Target identifier when known and safe to expose."`
	TargetResourceName ResourceName `json:"targetResourceName,omitempty" doc:"Globally unique Verself resource name for the target when known."`
	Permission         string       `json:"permission" doc:"Permission checked for the operation."`
	Outcome            string       `json:"outcome" doc:"allowed, denied, or error."`
	ErrorCode          string       `json:"error_code,omitempty" doc:"Stable problem code when the operation failed."`
	TraceID            string       `json:"trace_id,omitempty" doc:"OpenTelemetry trace ID."`
	DetailSHA256       string       `json:"detail_sha256" doc:"SHA-256 hash of the optional audit detail payload."`
	PrevHMAC           string       `json:"prev_hmac" doc:"Previous audit row HMAC in the organization chain."`
	RowHMAC            string       `json:"row_hmac" doc:"Current audit row HMAC."`
	HMACKeyID          string       `json:"hmac_key_id,omitempty" doc:"Audit HMAC key identifier."`
}

type GovernanceAuditEvents struct {
	Schema     string                 `json:"$schema,omitempty"`
	Events     []GovernanceAuditEvent `json:"events"`
	NextCursor string                 `json:"next_cursor,omitempty" doc:"Cursor for the next page."`
	Limit      int32                  `json:"limit" doc:"Applied page size."`
	Filters    GovernanceAuditFilters `json:"filters"`
}

type GovernanceAuditFilters struct {
	ActorID            string       `json:"actor_id,omitempty"`
	AuditEvent         string       `json:"audit_event,omitempty"`
	CredentialID       string       `json:"credential_id,omitempty"`
	EventName          string       `json:"event_name,omitempty"`
	EventSource        string       `json:"event_source,omitempty"`
	Outcome            string       `json:"outcome,omitempty"`
	TargetID           string       `json:"target_id,omitempty"`
	TargetType         string       `json:"target_type,omitempty"`
	TargetResourceName ResourceName `json:"targetResourceName,omitempty"`
}

type GovernanceCreateExportRequest struct {
	Scopes      []string `json:"scopes,omitempty" doc:"Export scopes. Empty means identity, billing, sandbox, and audit."`
	IncludeLogs bool     `json:"include_logs,omitempty" doc:"Include high-volume sandbox log content."`
}

type GovernanceExportFile struct {
	Path        string `json:"path" doc:"Path inside the tar.gz artifact."`
	ContentType string `json:"content_type" doc:"File media type."`
	Rows        string `json:"rows" doc:"JavaScript-safe row count."`
	Bytes       string `json:"bytes" doc:"JavaScript-safe file byte count."`
	SHA256      string `json:"sha256" doc:"File SHA-256."`
}

type GovernanceExportJob struct {
	ExportID       string                 `json:"export_id" doc:"Export job UUID."`
	ResourceName   ResourceName           `json:"resourceName" doc:"Globally unique Verself resource name for this export."`
	OrgID          string                 `json:"org_id" doc:"Organization ID."`
	RequestedBy    string                 `json:"requested_by" doc:"Zitadel subject that requested the export."`
	Scopes         []string               `json:"scopes"`
	IncludeLogs    bool                   `json:"include_logs"`
	Format         string                 `json:"format"`
	State          string                 `json:"state"`
	ArtifactSHA256 string                 `json:"artifact_sha256,omitempty"`
	ArtifactBytes  string                 `json:"artifact_bytes"`
	DownloadURL    string                 `json:"download_url,omitempty"`
	Files          []GovernanceExportFile `json:"files"`
	CreatedAt      string                 `json:"created_at" format:"date-time"`
	UpdatedAt      string                 `json:"updated_at" format:"date-time"`
	CompletedAt    string                 `json:"completed_at,omitempty" format:"date-time"`
	ExpiresAt      string                 `json:"expires_at" format:"date-time"`
	ErrorCode      string                 `json:"error_code,omitempty"`
	ErrorMessage   string                 `json:"error_message,omitempty"`
}

type GovernanceExportJobs struct {
	Schema  string                `json:"$schema,omitempty"`
	Exports []GovernanceExportJob `json:"exports"`
}
