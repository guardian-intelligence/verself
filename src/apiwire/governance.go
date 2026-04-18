package apiwire

type GovernanceAuditEvent struct {
	EventID            string `json:"event_id" doc:"Audit event UUID."`
	RecordedAt         string `json:"recorded_at" format:"date-time" doc:"UTC time when the event was recorded."`
	OrgID              string `json:"org_id" doc:"Organization ID derived from the caller's token."`
	ServiceName        string `json:"service_name" doc:"Service that enforced the operation."`
	OperationID        string `json:"operation_id" doc:"Stable OpenAPI operation ID."`
	AuditEvent         string `json:"audit_event" doc:"Stable audit event name."`
	PrincipalType      string `json:"principal_type" doc:"Principal class, for example user or api_credential."`
	PrincipalID        string `json:"principal_id" doc:"Zitadel subject or service-account subject."`
	PrincipalEmail     string `json:"principal_email,omitempty" doc:"Email when available for human users."`
	Permission         string `json:"permission" doc:"Forge Metal operation permission."`
	ResourceKind       string `json:"resource_kind" doc:"Resource kind declared by the operation catalog."`
	ResourceID         string `json:"resource_id,omitempty" doc:"Resource identifier when known and safe to expose."`
	Action             string `json:"action" doc:"Action declared by the operation catalog."`
	OrgScope           string `json:"org_scope" doc:"Organization scoping rule used for the decision."`
	RateLimitClass     string `json:"rate_limit_class,omitempty" doc:"Rate-limit bucket applied to the operation."`
	Result             string `json:"result" doc:"allowed, denied, or error."`
	ErrorCode          string `json:"error_code,omitempty" doc:"Stable problem code when the operation failed."`
	ErrorMessage       string `json:"error_message,omitempty" doc:"Redacted failure detail."`
	ClientIP           string `json:"client_ip,omitempty" doc:"Client IP observed at the service boundary."`
	IdempotencyKeyHash string `json:"idempotency_key_hash,omitempty" doc:"SHA-256 hash of the caller-provided idempotency key."`
	RequestID          string `json:"request_id,omitempty" doc:"Caller or service request ID when available."`
	TraceID            string `json:"trace_id,omitempty" doc:"OpenTelemetry trace ID."`
	Sequence           string `json:"sequence" doc:"Per-organization tamper-evident sequence number."`
	PrevHMAC           string `json:"prev_hmac" doc:"Previous audit row HMAC in the organization chain."`
	RowHMAC            string `json:"row_hmac" doc:"Current audit row HMAC."`
	ContentSHA256      string `json:"content_sha256" doc:"Canonical payload SHA-256."`
}

type GovernanceAuditEvents struct {
	Schema     string                 `json:"$schema,omitempty"`
	Events     []GovernanceAuditEvent `json:"events"`
	NextCursor string                 `json:"next_cursor,omitempty" doc:"Cursor for the next page."`
	Limit      int32                  `json:"limit" doc:"Applied page size."`
	Filters    GovernanceAuditFilters `json:"filters"`
}

type GovernanceAuditFilters struct {
	ServiceName string `json:"service_name,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	Result      string `json:"result,omitempty"`
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
