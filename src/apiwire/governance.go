package apiwire

type GovernanceAuditEvent struct {
	EventID               string `json:"event_id" doc:"Audit event UUID."`
	RecordedAt            string `json:"recorded_at" format:"date-time" doc:"UTC time when the event was recorded."`
	OrgID                 string `json:"org_id" doc:"Organization ID derived from the caller's token."`
	SourceProductArea     string `json:"source_product_area" doc:"Customer-facing product area, for example Governance or Sandbox."`
	ServiceName           string `json:"service_name" doc:"Internal service that enforced the operation."`
	OperationID           string `json:"operation_id" doc:"Stable OpenAPI operation ID."`
	AuditEvent            string `json:"audit_event" doc:"Stable audit event name."`
	OperationDisplay      string `json:"operation_display" doc:"Short customer-facing operation label."`
	OperationType         string `json:"operation_type" doc:"read, write, delete, authn, authz, billing, export, system, or unknown."`
	EventCategory         string `json:"event_category" doc:"Audit event family for filtering."`
	RiskLevel             string `json:"risk_level" doc:"low, medium, high, or critical."`
	ActorType             string `json:"actor_type" doc:"Actor class, for example user, api_credential, workload, service, or operator."`
	ActorID               string `json:"actor_id" doc:"Exact authenticated actor ID."`
	ActorDisplay          string `json:"actor_display,omitempty" doc:"Display label for the actor when available."`
	ActorOwnerID          string `json:"actor_owner_id,omitempty" doc:"Owner of an API credential or workload actor when known."`
	ActorOwnerDisplay     string `json:"actor_owner_display,omitempty" doc:"Display label for the actor owner when known."`
	CredentialID          string `json:"credential_id,omitempty" doc:"Verself API credential ID when the actor is a credential."`
	CredentialName        string `json:"credential_name,omitempty" doc:"API credential display name when known."`
	CredentialFingerprint string `json:"credential_fingerprint,omitempty" doc:"Non-secret credential fingerprint when known."`
	AuthMethod            string `json:"auth_method,omitempty" doc:"Authentication method used by the actor."`
	MFAPresent            bool   `json:"mfa_present,omitempty" doc:"Whether MFA was present in the authenticated request context."`
	TargetKind            string `json:"target_kind" doc:"Resource kind declared by the operation catalog."`
	TargetID              string `json:"target_id,omitempty" doc:"Target identifier when known and safe to expose."`
	TargetDisplay         string `json:"target_display,omitempty" doc:"Target display label when known."`
	TargetScope           string `json:"target_scope,omitempty" doc:"Target scoping rule or resolved scope."`
	Permission            string `json:"permission" doc:"Verself operation permission."`
	Action                string `json:"action" doc:"Action declared by the operation catalog."`
	OrgScope              string `json:"org_scope" doc:"Organization scoping rule used for the decision."`
	Decision              string `json:"decision" doc:"Authorization decision."`
	Result                string `json:"result" doc:"allowed, denied, or error."`
	DenialReason          string `json:"denial_reason,omitempty" doc:"Stable denial reason when authorization denied the operation."`
	TrustClass            string `json:"trust_class,omitempty" doc:"Trust class used by the policy decision."`
	ErrorCode             string `json:"error_code,omitempty" doc:"Stable problem code when the operation failed."`
	ErrorMessage          string `json:"error_message,omitempty" doc:"Redacted failure detail."`
	ClientIP              string `json:"client_ip,omitempty" doc:"Client IP observed at the trusted service boundary."`
	ClientIPVersion       string `json:"client_ip_version,omitempty" doc:"IPv4 or IPv6."`
	IPChain               string `json:"ip_chain,omitempty" doc:"Comma-separated proxy chain retained as request evidence."`
	UserAgentHash         string `json:"user_agent_hash,omitempty" doc:"SHA-256 hash of the sanitized User-Agent."`
	GeoCountry            string `json:"geo_country,omitempty" doc:"Locally enriched country code when configured."`
	GeoRegion             string `json:"geo_region,omitempty" doc:"Locally enriched region when configured."`
	ASN                   string `json:"asn,omitempty" doc:"Locally enriched autonomous system number when configured."`
	ASNOrg                string `json:"asn_org,omitempty" doc:"Locally enriched autonomous system organization when configured."`
	NetworkType           string `json:"network_type,omitempty" doc:"Locally enriched network type when configured."`
	IdempotencyKeyHash    string `json:"idempotency_key_hash,omitempty" doc:"SHA-256 hash of the caller-provided idempotency key."`
	RequestID             string `json:"request_id,omitempty" doc:"Caller or service request ID when available."`
	TraceID               string `json:"trace_id,omitempty" doc:"OpenTelemetry trace ID."`
	Sequence              string `json:"sequence" doc:"Per-organization tamper-evident sequence number."`
	PrevHMAC              string `json:"prev_hmac" doc:"Previous audit row HMAC in the organization chain."`
	RowHMAC               string `json:"row_hmac" doc:"Current audit row HMAC."`
	ContentSHA256         string `json:"content_sha256" doc:"Canonical payload SHA-256."`
	HMACKeyID             string `json:"hmac_key_id,omitempty" doc:"Audit HMAC key identifier."`
}

type GovernanceAuditEvents struct {
	Schema     string                 `json:"$schema,omitempty"`
	Events     []GovernanceAuditEvent `json:"events"`
	NextCursor string                 `json:"next_cursor,omitempty" doc:"Cursor for the next page."`
	Limit      int32                  `json:"limit" doc:"Applied page size."`
	Filters    GovernanceAuditFilters `json:"filters"`
}

type GovernanceAuditFilters struct {
	ActorID           string `json:"actor_id,omitempty"`
	AuditEvent        string `json:"audit_event,omitempty"`
	CredentialID      string `json:"credential_id,omitempty"`
	HighRisk          bool   `json:"high_risk,omitempty"`
	OperationID       string `json:"operation_id,omitempty"`
	OperationType     string `json:"operation_type,omitempty"`
	Result            string `json:"result,omitempty"`
	RiskLevel         string `json:"risk_level,omitempty"`
	ServiceName       string `json:"service_name,omitempty"`
	SourceProductArea string `json:"source_product_area,omitempty"`
	TargetID          string `json:"target_id,omitempty"`
	TargetKind        string `json:"target_kind,omitempty"`
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
