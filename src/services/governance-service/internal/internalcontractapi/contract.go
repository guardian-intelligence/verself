package internalcontractapi

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
	ProblemCode   string
	ProblemDetail string
	ProblemType   string
	RequestID     string
	ResourceName  string
	TraceParent   string
)

type (
	APIActivityAction        string
	APIActivityOperation     string
	APIActivityService       string
	APIActivityStatus        string
	APIEventCode             string
	ActorType                string
	ActorUID                 string
	CredentialUID            string
	DecimalUint64            string
	GovernanceOrgID          string
	GovernancePermissionName string
	HMACHex                  string
	HTTPHost                 string
	HTTPMethod               string
	HTTPReferrer             string
	HTTPResponseMessage      string
	HTTPResponseStatus       string
	HTTPSafeArgs             string
	HTTPScheme               string
	HTTPXForwardedFor        string
	OCSFMetadataUID          string
	ProblemStatusCode        string
	ResourceType             string
	ResourceUID              string
	TraceID                  string
	SpanID                   string
)

const (
	APIActivityStatusSuccess APIActivityStatus = "Success"
	APIActivityStatusFailure APIActivityStatus = "Failure"
	APIActivityStatusOther   APIActivityStatus = "Other"
)

type AuthorizationDecision string

const (
	AuthorizationDecisionAllowed AuthorizationDecision = "Allowed"
	AuthorizationDecisionDenied  AuthorizationDecision = "Denied"
)

type APIActivityResources []APIActivityResource

type APIActivityAccepted struct {
	MetadataUID OCSFMetadataUID `json:"metadata_uid" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Sequence    DecimalUint64   `json:"sequence" required:"true" pattern:"^[0-9]+$"`
	RowHMAC     HMACHex         `json:"row_hmac" required:"true" pattern:"^[0-9a-f]{64}$"`
}

type APIActivityHTTPRequest struct {
	Method          HTTPMethod         `json:"method" required:"true" minLength:"1" maxLength:"32"`
	Route           string             `json:"route" required:"true" minLength:"1" maxLength:"4096"`
	RequestUID      *RequestID         `json:"request_uid,omitempty" maxLength:"128"`
	Args            *HTTPSafeArgs      `json:"args,omitempty" maxLength:"8192"`
	UserAgent       *string            `json:"user_agent,omitempty" maxLength:"512"`
	SrcEndpointIP   *string            `json:"src_endpoint_ip,omitempty" maxLength:"128"`
	SrcEndpointName *string            `json:"src_endpoint_name,omitempty" maxLength:"255"`
	XForwardedFor   *HTTPXForwardedFor `json:"x_forwarded_for,omitempty" maxLength:"1024"`
	Referrer        *HTTPReferrer      `json:"referrer,omitempty" maxLength:"1024"`
	Host            *HTTPHost          `json:"host,omitempty" maxLength:"255"`
	Scheme          *HTTPScheme        `json:"scheme,omitempty" maxLength:"16"`
}

type APIActivityHTTPResponse struct {
	Code    uint16               `json:"code" required:"true" minimum:"100" maximum:"599"`
	Message *HTTPResponseMessage `json:"message,omitempty" maxLength:"255"`
	Status  *HTTPResponseStatus  `json:"status,omitempty" maxLength:"255"`
}

type APIActivityResource struct {
	Type     ResourceType  `json:"type" required:"true" minLength:"1" maxLength:"128" pattern:"^[a-z][a-z0-9_-]*$"`
	UID      *ResourceUID  `json:"uid,omitempty" maxLength:"512"`
	Name     *string       `json:"name,omitempty" maxLength:"1024"`
	FullName *ResourceName `json:"full_name,omitempty" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	Role     *string       `json:"role,omitempty" maxLength:"64"`
	RoleID   *uint8        `json:"role_id,omitempty" minimum:"1" maximum:"4"`
}

type APIActivityRecord struct {
	OrgID                 GovernanceOrgID          `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	APIService            APIActivityService       `json:"api_service" required:"true" minLength:"1" maxLength:"128"`
	APIOperation          APIActivityOperation     `json:"api_operation" required:"true" minLength:"1" maxLength:"128"`
	APIEventCode          APIEventCode             `json:"api_event_code" required:"true" minLength:"1" maxLength:"255"`
	APIAction             APIActivityAction        `json:"api_action" required:"true" minLength:"1" maxLength:"128"`
	APIVersion            *string                  `json:"api_version,omitempty" maxLength:"64"`
	ActorType             ActorType                `json:"actor_type" required:"true" minLength:"1" maxLength:"128" pattern:"^[a-z][a-z0-9_.-]*$"`
	ActorUID              ActorUID                 `json:"actor_uid" required:"true" minLength:"1" maxLength:"512"`
	ActorName             *string                  `json:"actor_name,omitempty" maxLength:"512"`
	ActorEmail            *string                  `json:"actor_email,omitempty" maxLength:"512"`
	CredentialUID         *CredentialUID           `json:"credential_uid,omitempty" maxLength:"512"`
	Permission            GovernancePermissionName `json:"permission" required:"true" minLength:"1" maxLength:"255"`
	Resources             APIActivityResources     `json:"resources" required:"true" minItems:"1" maxItems:"16"`
	HTTPRequest           APIActivityHTTPRequest   `json:"http_request" required:"true"`
	HTTPResponse          APIActivityHTTPResponse  `json:"http_response" required:"true"`
	AuthorizationDecision AuthorizationDecision    `json:"authorization_decision" required:"true"`
	Status                APIActivityStatus        `json:"status" required:"true"`
	StatusCode            ProblemStatusCode        `json:"status_code" required:"true" minLength:"1" maxLength:"128"`
	StatusDetail          *string                  `json:"status_detail,omitempty" maxLength:"4096"`
	TraceUID              *TraceID                 `json:"trace_uid,omitempty" pattern:"^[0-9a-f]{32}$"`
	SpanUID               *SpanID                  `json:"span_uid,omitempty" pattern:"^[0-9a-f]{16}$"`
	ObservedAt            *string                  `json:"observed_at,omitempty"`
	Unmapped              *map[string]any          `json:"unmapped,omitempty"`
}

type AppendAPIActivityInput struct {
	Body APIActivityRecord
}

type AppendAPIActivityOutputBody struct {
	Accepted APIActivityAccepted `json:"accepted" required:"true"`
}

type AppendAPIActivityOutput struct {
	Body AppendAPIActivityOutputBody
}

var Operations = []OperationDescriptor{
	AppendAPIActivity.Descriptor,
}

var AppendAPIActivity = Operation[AppendAPIActivityInput, AppendAPIActivityOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.governance.v1#AppendAPIActivity",
		OperationID:         "append-api-activity",
		Method:              "POST",
		Path:                "/internal/v1/ocsf/api-activities",
		DefaultStatus:       202,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "governance-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "governance:api_activity:append", OrganizationSource: "body_org_id", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "governance.ocsf_api_activity.append", Resource: "api_activity", Action: "write"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 32768,
		RequestPayload:      PayloadDescriptor{Member: "record", Target: "verself.governance.v1#APIActivityRecord", Kind: "structure", MediaType: "", Streaming: false, Sensitive: false, Required: true},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "governanceInternal.apiActivity", Method: "append", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

type Handlers = InternalHandlers

type InternalHandlers interface {
	AppendAPIActivity(context.Context, *AppendAPIActivityInput) (*AppendAPIActivityOutput, error)
}

type AppendAPIActivityHandler = Handler[AppendAPIActivityInput, AppendAPIActivityOutput]
