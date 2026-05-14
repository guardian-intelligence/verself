package contractapi

import "context"

type OperationDescriptor struct {
	ShapeID             string
	OperationID         string
	Method              string
	Path                string
	DefaultStatus       int
	Readonly            bool
	Paginated           bool
	Identity            IdentityDescriptor
	Authorization       AuthorizationDescriptor
	Audit               AuditDescriptor
	RateLimitBucket     string
	RequestBodyMaxBytes int64
	RequestPayload      PayloadDescriptor
	ResponsePayload     PayloadDescriptor
	ResponseHeaders     []HeaderDescriptor
	Idempotency         IdempotencyDescriptor
	SDK                 SDKDescriptor
	Problems            []ProblemDescriptor
}

type IdentityDescriptor struct {
	Mode       string
	Audience   string
	Principals []string
}

type AuthorizationDescriptor struct {
	Permission         string
	OrganizationSource string
	OrganizationMember string
}

type AuditDescriptor struct {
	Event    string
	Resource string
	Action   string
}

type IdempotencyDescriptor struct {
	Policy string
	Header string
	Member string
}

type PayloadDescriptor struct {
	Member    string
	Target    string
	Kind      string
	MediaType string
	Streaming bool
	Sensitive bool
	Required  bool
}

type HeaderDescriptor struct {
	Member string
	Name   string
}

type SDKDescriptor struct {
	Module    string
	Method    string
	Paginated bool
	Retryable bool
}

type ProblemDescriptor struct {
	ShapeID string
	Type    string
	Code    string
	Status  int
}

type Operation[Input any, Output any] struct {
	Descriptor OperationDescriptor
}

type Handler[Input any, Output any] func(context.Context, *Input) (*Output, error)

type (
	IdempotencyKey string
	ProblemCode    string
	ProblemDetail  string
	ProblemType    string
	RequestID      string
	ResourceName   string
	TraceParent    string
)

type (
	APIActivityCursor        string
	APIActivityEventsLimit   int
	APIActivityOperation     string
	APIActivityService       string
	ActorType                string
	ActorUID                 string
	CredentialUID            string
	DataExportArchive        []byte
	DataExportID             string
	DecimalUint64            string
	ExportArtifactPath       string
	ExportDownloadURL        string
	ExportErrorCode          string
	ExportErrorMessage       string
	ExportFormat             string
	ExportScope              string
	ExportState              string
	GovernanceOrgID          string
	GovernancePermissionName string
	HMACHex                  string
	MediaType                string
	OCSFClassName            string
	OCSFMetadataUID          string
	OCSFStatus               string
	ResourceType             string
	ResourceUID              string
	SHA256Hex                string
	SpanID                   string
	TraceID                  string
)

type APIActivityListOrder string

const (
	APIActivityListOrderAsc  APIActivityListOrder = "asc"
	APIActivityListOrderDesc APIActivityListOrder = "desc"
)

type (
	APIActivityEvents []APIActivityEvent
	DataExportFiles   []DataExportFile
	DataExportJobs    []DataExportJob
	ExportScopes      []ExportScope
)

type ConflictError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type (
	IdempotencyPayloadMismatchError = ConflictError
	PermissionDeniedError           = ConflictError
	RateLimitedError                = ConflictError
	ResourceNotFoundError           = ConflictError
	ServiceUnavailableError         = ConflictError
	UnauthenticatedError            = ConflictError
	ValidationFailedError           = ConflictError
)

type APIActivityEvent struct {
	MetadataUID             OCSFMetadataUID          `json:"metadata_uid" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Time                    string                   `json:"time" required:"true"`
	OrgID                   GovernanceOrgID          `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	Sequence                DecimalUint64            `json:"sequence" required:"true" pattern:"^[0-9]+$"`
	OCSFVersion             string                   `json:"ocsf_version" required:"true" minLength:"1" maxLength:"32"`
	CategoryUID             uint8                    `json:"category_uid" required:"true"`
	CategoryName            string                   `json:"category_name" required:"true"`
	ClassUID                uint16                   `json:"class_uid" required:"true"`
	ClassName               OCSFClassName            `json:"class_name" required:"true"`
	TypeUID                 uint32                   `json:"type_uid" required:"true"`
	ActivityID              uint8                    `json:"activity_id" required:"true"`
	ActivityName            string                   `json:"activity_name" required:"true"`
	ActionID                uint8                    `json:"action_id" required:"true"`
	Action                  string                   `json:"action" required:"true"`
	StatusID                uint8                    `json:"status_id" required:"true"`
	Status                  OCSFStatus               `json:"status" required:"true"`
	StatusCode              string                   `json:"status_code" required:"true"`
	SeverityID              uint8                    `json:"severity_id" required:"true"`
	Severity                string                   `json:"severity" required:"true"`
	APIService              APIActivityService       `json:"api_service" required:"true"`
	APIOperation            APIActivityOperation     `json:"api_operation" required:"true"`
	APIVersion              string                   `json:"api_version,omitempty"`
	ActorType               ActorType                `json:"actor_type" required:"true"`
	ActorUID                ActorUID                 `json:"actor_uid" required:"true"`
	ActorName               string                   `json:"actor_name,omitempty"`
	CredentialUID           *CredentialUID           `json:"credential_uid,omitempty"`
	PrimaryResourceType     ResourceType             `json:"primary_resource_type" required:"true"`
	PrimaryResourceUID      *ResourceUID             `json:"primary_resource_uid,omitempty"`
	PrimaryResourceName     string                   `json:"primary_resource_name,omitempty"`
	PrimaryResourceFullName *ResourceName            `json:"primary_resource_full_name,omitempty"`
	Permission              GovernancePermissionName `json:"permission" required:"true"`
	HTTPRequestUID          *RequestID               `json:"http_request_uid,omitempty"`
	HTTPMethod              string                   `json:"http_method" required:"true"`
	HTTPRoute               string                   `json:"http_route" required:"true"`
	HTTPArgs                string                   `json:"http_args,omitempty"`
	HTTPUserAgent           string                   `json:"http_user_agent,omitempty"`
	SrcEndpointIP           string                   `json:"src_endpoint_ip,omitempty"`
	SrcEndpointName         string                   `json:"src_endpoint_name,omitempty"`
	HTTPResponseCode        uint16                   `json:"http_response_code" required:"true"`
	TraceUID                *TraceID                 `json:"trace_uid,omitempty" pattern:"^[0-9a-f]{32}$"`
	SpanUID                 *SpanID                  `json:"span_uid,omitempty" pattern:"^[0-9a-f]{16}$"`
	OCSFSHA256              SHA256Hex                `json:"ocsf_sha256" required:"true" pattern:"^[0-9a-f]{64}$"`
	PrevHMAC                HMACHex                  `json:"prev_hmac" required:"true" pattern:"^[0-9a-f]{64}$"`
	RowHMAC                 HMACHex                  `json:"row_hmac" required:"true" pattern:"^[0-9a-f]{64}$"`
	HMACKeyID               *string                  `json:"hmac_key_id,omitempty"`
}

type APIActivityFilters struct {
	ActorType     *ActorType            `json:"actor_type,omitempty"`
	ActorUID      *ActorUID             `json:"actor_uid,omitempty"`
	APIService    *APIActivityService   `json:"api_service,omitempty"`
	APIOperation  *APIActivityOperation `json:"api_operation,omitempty"`
	ActivityID    *uint8                `json:"activity_id,omitempty"`
	CredentialUID *CredentialUID        `json:"credential_uid,omitempty"`
	ResourceUID   *ResourceUID          `json:"resource_uid,omitempty"`
	ResourceType  *ResourceType         `json:"resource_type,omitempty"`
	StatusID      *uint8                `json:"status_id,omitempty"`
	StatusCode    *string               `json:"status_code,omitempty"`
	TraceUID      *TraceID              `json:"trace_uid,omitempty"`
}

type CreateDataExportInputBody struct {
	Scopes      *ExportScopes `json:"scopes,omitempty"`
	IncludeLogs *bool         `json:"include_logs,omitempty"`
}

type CreateDataExportInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateDataExportInputBody
}

type DataExportFile struct {
	Path        ExportArtifactPath `json:"path" required:"true" minLength:"1" maxLength:"4096"`
	ContentType MediaType          `json:"content_type" required:"true" minLength:"1" maxLength:"255"`
	Rows        DecimalUint64      `json:"rows" required:"true" pattern:"^[0-9]+$"`
	Bytes       DecimalUint64      `json:"bytes" required:"true" pattern:"^[0-9]+$"`
	SHA256      SHA256Hex          `json:"sha256" required:"true" pattern:"^[0-9a-f]{64}$"`
}

type DataExportJob struct {
	ExportID       DataExportID        `json:"export_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ResourceName   ResourceName        `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	OrgID          GovernanceOrgID     `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	RequestedBy    ActorUID            `json:"requested_by" required:"true" minLength:"1" maxLength:"512"`
	Scopes         ExportScopes        `json:"scopes" required:"true"`
	IncludeLogs    bool                `json:"include_logs" required:"true"`
	Format         ExportFormat        `json:"format" required:"true" minLength:"1" maxLength:"128"`
	State          ExportState         `json:"state" required:"true" minLength:"1" maxLength:"128"`
	ArtifactSHA256 *SHA256Hex          `json:"artifact_sha256,omitempty" pattern:"^[0-9a-f]{64}$"`
	ArtifactBytes  DecimalUint64       `json:"artifact_bytes" required:"true" pattern:"^[0-9]+$"`
	DownloadURL    *ExportDownloadURL  `json:"download_url,omitempty" maxLength:"8192"`
	Files          DataExportFiles     `json:"files" required:"true"`
	CreatedAt      string              `json:"created_at" required:"true"`
	UpdatedAt      string              `json:"updated_at" required:"true"`
	CompletedAt    *string             `json:"completed_at,omitempty"`
	ExpiresAt      string              `json:"expires_at" required:"true"`
	ErrorCode      *ExportErrorCode    `json:"error_code,omitempty" maxLength:"128"`
	ErrorMessage   *ExportErrorMessage `json:"error_message,omitempty" maxLength:"4096"`
}

type DownloadDataExportInput struct {
	ExportID DataExportID `path:"export_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type GetDataExportInput = DownloadDataExportInput

type ListAPIActivitiesInput struct {
	Limit         APIActivityEventsLimit `query:"limit" minimum:"1" maximum:"200"`
	Cursor        APIActivityCursor      `query:"cursor" minLength:"1" maxLength:"4096"`
	Order         APIActivityListOrder   `query:"order"`
	ActorUID      ActorUID               `query:"actor_uid" minLength:"1" maxLength:"512"`
	ActorType     ActorType              `query:"actor_type" minLength:"1" maxLength:"128"`
	APIService    APIActivityService     `query:"api_service" minLength:"1" maxLength:"128"`
	APIOperation  APIActivityOperation   `query:"api_operation" minLength:"1" maxLength:"128"`
	ActivityID    uint8                  `query:"activity_id" minimum:"1" maximum:"99"`
	CredentialUID CredentialUID          `query:"credential_uid" maxLength:"512"`
	ResourceUID   ResourceUID            `query:"resource_uid" maxLength:"512"`
	ResourceType  ResourceType           `query:"resource_type" minLength:"1" maxLength:"128" pattern:"^[a-z][a-z0-9_]*$"`
	StatusID      uint8                  `query:"status_id" minimum:"1" maximum:"99"`
	StatusCode    string                 `query:"status_code" maxLength:"128"`
	TraceUID      TraceID                `query:"trace_uid" pattern:"^[0-9a-f]{32}$"`
}

type ListDataExportsInput struct{}

type ListAPIActivitiesOutputBody struct {
	APIActivities APIActivityEvents      `json:"api_activities" required:"true"`
	NextCursor    *APIActivityCursor     `json:"next_cursor,omitempty" minLength:"1" maxLength:"4096"`
	Limit         APIActivityEventsLimit `json:"limit" required:"true" minimum:"1" maximum:"200"`
	Filters       APIActivityFilters     `json:"filters" required:"true"`
}

type ListAPIActivitiesOutput struct {
	Body ListAPIActivitiesOutputBody
}

type ListDataExportsOutputBody struct {
	Exports DataExportJobs `json:"exports" required:"true"`
}
type (
	ListDataExportsOutput      struct{ Body ListDataExportsOutputBody }
	CreateDataExportOutputBody struct {
		Export DataExportJob `json:"export" required:"true"`
	}
)

type (
	CreateDataExportOutput  struct{ Body CreateDataExportOutputBody }
	GetDataExportOutputBody struct {
		Export DataExportJob `json:"export" required:"true"`
	}
)
type GetDataExportOutput struct{ Body GetDataExportOutputBody }

type DownloadDataExportOutput struct {
	ContentType        MediaType `header:"Content-Type" required:"true" minLength:"1" maxLength:"255"`
	ContentDisposition string    `header:"Content-Disposition" required:"true" minLength:"1" maxLength:"255"`
	Body               []byte
}

var Operations = []OperationDescriptor{
	ListAPIActivities.Descriptor,
	ListDataExports.Descriptor,
	CreateDataExport.Descriptor,
	GetDataExport.Descriptor,
	DownloadDataExport.Descriptor,
}

var ListAPIActivities = Operation[ListAPIActivitiesInput, ListAPIActivitiesOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.governance.v1#ListAPIActivities",
		OperationID:         "list-api-activities",
		Method:              "GET",
		Path:                "/api/v1/governance/ocsf/api-activities",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           true,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "governance-service", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "governance:api_activity:read", OrganizationSource: "token_org_id"},
		Audit:               AuditDescriptor{Event: "governance.ocsf_api_activity.list", Resource: "api_activity", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		SDK:                 SDKDescriptor{Module: "governance.ocsf", Method: "listAPIActivities", Paginated: true, Retryable: true},
		Problems:            commonReadProblems(),
	},
}

var (
	ListDataExports    = Operation[ListDataExportsInput, ListDataExportsOutput]{Descriptor: OperationDescriptor{ShapeID: "verself.governance.v1#ListDataExports", OperationID: "list-data-exports", Method: "GET", Path: "/api/v1/governance/exports", DefaultStatus: 200, Readonly: true, Identity: IdentityDescriptor{Mode: "bearer", Audience: "governance-service", Principals: []string{"browser", "cli", "workload"}}, Authorization: AuthorizationDescriptor{Permission: "governance:api_activity:export", OrganizationSource: "token_org_id"}, Audit: AuditDescriptor{Event: "governance.ocsf_api_activity.export.list", Resource: "data_export", Action: "list"}, RateLimitBucket: "read", SDK: SDKDescriptor{Module: "governance.exports", Method: "list", Retryable: true}, Problems: commonReadProblems()}}
	CreateDataExport   = Operation[CreateDataExportInput, CreateDataExportOutput]{Descriptor: OperationDescriptor{ShapeID: "verself.governance.v1#CreateDataExport", OperationID: "create-data-export", Method: "POST", Path: "/api/v1/governance/exports", DefaultStatus: 201, Identity: IdentityDescriptor{Mode: "bearer", Audience: "governance-service", Principals: []string{"browser", "cli"}}, Authorization: AuthorizationDescriptor{Permission: "governance:api_activity:export", OrganizationSource: "token_org_id"}, Audit: AuditDescriptor{Event: "governance.ocsf_api_activity.export.create", Resource: "data_export", Action: "create"}, RateLimitBucket: "export_create", RequestBodyMaxBytes: 16384, Idempotency: IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"}, SDK: SDKDescriptor{Module: "governance.exports", Method: "create"}, Problems: commonMutationProblems()}}
	GetDataExport      = Operation[GetDataExportInput, GetDataExportOutput]{Descriptor: OperationDescriptor{ShapeID: "verself.governance.v1#GetDataExport", OperationID: "get-data-export", Method: "GET", Path: "/api/v1/governance/exports/{export_id}", DefaultStatus: 200, Readonly: true, Identity: IdentityDescriptor{Mode: "bearer", Audience: "governance-service", Principals: []string{"browser", "cli", "workload"}}, Authorization: AuthorizationDescriptor{Permission: "governance:api_activity:export", OrganizationSource: "token_org_id"}, Audit: AuditDescriptor{Event: "governance.ocsf_api_activity.export.read", Resource: "data_export", Action: "read"}, RateLimitBucket: "read", SDK: SDKDescriptor{Module: "governance.exports", Method: "get", Retryable: true}, Problems: commonReadProblems()}}
	DownloadDataExport = Operation[DownloadDataExportInput, DownloadDataExportOutput]{Descriptor: OperationDescriptor{ShapeID: "verself.governance.v1#DownloadDataExport", OperationID: "download-data-export", Method: "GET", Path: "/api/v1/governance/exports/{export_id}/download", DefaultStatus: 200, Readonly: true, Identity: IdentityDescriptor{Mode: "bearer", Audience: "governance-service", Principals: []string{"browser", "cli"}}, Authorization: AuthorizationDescriptor{Permission: "governance:api_activity:export", OrganizationSource: "token_org_id"}, Audit: AuditDescriptor{Event: "governance.ocsf_api_activity.export.download", Resource: "data_export", Action: "download"}, RateLimitBucket: "export_download", ResponsePayload: PayloadDescriptor{Member: "body", Target: "verself.governance.v1#DataExportArchive", Kind: "blob", MediaType: "application/gzip", Required: true}, ResponseHeaders: []HeaderDescriptor{{Member: "contentDisposition", Name: "Content-Disposition"}, {Member: "contentType", Name: "Content-Type"}}, SDK: SDKDescriptor{Module: "governance.exports", Method: "download", Retryable: true}, Problems: commonReadProblems()}}
)

func commonReadProblems() []ProblemDescriptor {
	return []ProblemDescriptor{
		{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
		{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
		{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
		{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
		{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
	}
}

func commonMutationProblems() []ProblemDescriptor {
	return append(commonReadProblems(),
		ProblemDescriptor{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
		ProblemDescriptor{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
		ProblemDescriptor{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
	)
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	ListAPIActivities(context.Context, *ListAPIActivitiesInput) (*ListAPIActivitiesOutput, error)
	ListDataExports(context.Context, *ListDataExportsInput) (*ListDataExportsOutput, error)
	CreateDataExport(context.Context, *CreateDataExportInput) (*CreateDataExportOutput, error)
	GetDataExport(context.Context, *GetDataExportInput) (*GetDataExportOutput, error)
	DownloadDataExport(context.Context, *DownloadDataExportInput) (*DownloadDataExportOutput, error)
}

type (
	ListAPIActivitiesHandler  = Handler[ListAPIActivitiesInput, ListAPIActivitiesOutput]
	ListDataExportsHandler    = Handler[ListDataExportsInput, ListDataExportsOutput]
	CreateDataExportHandler   = Handler[CreateDataExportInput, CreateDataExportOutput]
	GetDataExportHandler      = Handler[GetDataExportInput, GetDataExportOutput]
	DownloadDataExportHandler = Handler[DownloadDataExportInput, DownloadDataExportOutput]
)
