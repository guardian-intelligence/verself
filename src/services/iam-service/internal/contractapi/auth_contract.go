package contractapi

type (
	AccountID              string
	DeviceSessionID        string
	AccountConnectionID    string
	IdentityIssuer         string
	AuthChannel            string
	AuthMethod             string
	DeviceSessionState     string
	AccountConnectionState string
)

type (
	AuthMethods                []AuthMethod
	AuthOrganizationContexts   []AuthOrganizationContext
	DeviceSessionSummaries     []DeviceSessionSummary
	AccountConnectionSummaries []AccountConnectionSummary
)

type AccountSummary struct {
	AccountID     AccountID      `json:"accountId" required:"true" pattern:"^acct_[0-9A-HJKMNP-TV-Z]{26}$"`
	Issuer        IdentityIssuer `json:"issuer" required:"true" minLength:"1" maxLength:"2048"`
	Subject       SubjectId      `json:"subject" required:"true" minLength:"1" maxLength:"512"`
	Email         EmailAddress   `json:"email" required:"true" minLength:"3" maxLength:"320"`
	EmailVerified bool           `json:"emailVerified" required:"true"`
	DisplayName   DisplayName    `json:"displayName" required:"true" minLength:"1" maxLength:"120"`
	CreatedAt     string         `json:"createdAt" required:"true"`
}

type AuthOrganizationContext struct {
	OrgID       OrgID       `json:"orgId" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	DisplayName DisplayName `json:"displayName" required:"true" minLength:"1" maxLength:"120"`
	Slug        *OrgSlug    `json:"slug,omitempty" minLength:"1" maxLength:"80" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	Selected    bool        `json:"selected" required:"true"`
}

type DeviceSessionSummary struct {
	SessionID   DeviceSessionID    `json:"sessionId" required:"true" pattern:"^sess_[0-9A-HJKMNP-TV-Z]{26}$"`
	AccountID   AccountID          `json:"accountId" required:"true" pattern:"^acct_[0-9A-HJKMNP-TV-Z]{26}$"`
	Channel     AuthChannel        `json:"channel" required:"true" pattern:"^(browser|cli|sdk|mobile|workload)$"`
	State       DeviceSessionState `json:"state" required:"true" pattern:"^(active|revoked|expired)$"`
	Current     bool               `json:"current" required:"true"`
	DeviceLabel DisplayName        `json:"deviceLabel" required:"true" minLength:"1" maxLength:"160"`
	CreatedAt   string             `json:"createdAt" required:"true"`
	LastSeenAt  string             `json:"lastSeenAt" required:"true"`
	ExpiresAt   string             `json:"expiresAt" required:"true"`
}

type AccountConnectionSummary struct {
	ConnectionID  AccountConnectionID    `json:"connectionId" required:"true" pattern:"^conn_[0-9A-HJKMNP-TV-Z]{26}$"`
	AccountID     AccountID              `json:"accountId" required:"true" pattern:"^acct_[0-9A-HJKMNP-TV-Z]{26}$"`
	Issuer        IdentityIssuer         `json:"issuer" required:"true" minLength:"1" maxLength:"2048"`
	Subject       SubjectId              `json:"subject" required:"true" minLength:"1" maxLength:"512"`
	State         AccountConnectionState `json:"state" required:"true" pattern:"^(linked|removed)$"`
	Email         EmailAddress           `json:"email" required:"true" minLength:"3" maxLength:"320"`
	EmailVerified bool                   `json:"emailVerified" required:"true"`
	CreatedAt     string                 `json:"createdAt" required:"true"`
	LastSeenAt    string                 `json:"lastSeenAt" required:"true"`
}

type AuthContext struct {
	Account       AccountSummary           `json:"account" required:"true"`
	Session       DeviceSessionSummary     `json:"session" required:"true"`
	Organizations AuthOrganizationContexts `json:"organizations" required:"true"`
	SelectedOrgID *OrgID                   `json:"selectedOrgId,omitempty" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	AuthTime      string                   `json:"authTime" required:"true"`
	AuthMethods   AuthMethods              `json:"authMethods" required:"true"`
}

type GetAuthContextInput struct {
	SessionID DeviceSessionID `header:"X-Verself-Device-Session" pattern:"^sess_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type GetAuthContextOutput struct {
	Body AuthContext
}

type CreateDeviceSessionInputBody struct {
	Channel     AuthChannel `json:"channel" required:"true" pattern:"^(browser|cli|sdk|mobile|workload)$"`
	DeviceLabel DisplayName `json:"deviceLabel" required:"true" minLength:"1" maxLength:"160"`
}

type CreateDeviceSessionInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateDeviceSessionInputBody
}

type CreateDeviceSessionOutput struct {
	Body AuthContext
}

type ListDeviceSessionsInput struct {
	SessionID DeviceSessionID `header:"X-Verself-Device-Session" pattern:"^sess_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type ListDeviceSessionsOutputBody struct {
	Sessions DeviceSessionSummaries `json:"sessions" required:"true"`
}

type ListDeviceSessionsOutput struct {
	Body ListDeviceSessionsOutputBody
}

type RevokeDeviceSessionInput struct {
	SessionID        DeviceSessionID `path:"sessionId" required:"true" pattern:"^sess_[0-9A-HJKMNP-TV-Z]{26}$"`
	CurrentSessionID DeviceSessionID `header:"X-Verself-Device-Session" required:"true" pattern:"^sess_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type RevokeDeviceSessionOutput struct{}

type ListAccountConnectionsInput struct {
	SessionID DeviceSessionID `header:"X-Verself-Device-Session" pattern:"^sess_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type ListAccountConnectionsOutputBody struct {
	Connections AccountConnectionSummaries `json:"connections" required:"true"`
}

type ListAccountConnectionsOutput struct {
	Body ListAccountConnectionsOutputBody
}

type RemoveAccountConnectionInput struct {
	ConnectionID     AccountConnectionID `path:"connectionId" required:"true" pattern:"^conn_[0-9A-HJKMNP-TV-Z]{26}$"`
	CurrentSessionID DeviceSessionID     `header:"X-Verself-Device-Session" required:"true" pattern:"^sess_[0-9A-HJKMNP-TV-Z]{26}$"`
}

type RemoveAccountConnectionOutput struct{}

var commonAuthProblems = []ProblemDescriptor{
	{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "https://verself.sh/docs/reference/iam/errors#auth-unauthenticated", Code: "auth.unauthenticated", Status: 401},
	{ShapeID: "verself.iam.v1#DeviceSessionRequiredError", Type: "https://verself.sh/docs/reference/iam/errors#auth-device-session-required", Code: "auth.device_session_required", Status: 401},
	{ShapeID: "verself.iam.v1#SessionRevokedError", Type: "https://verself.sh/docs/reference/iam/errors#auth-session-revoked", Code: "auth.session_revoked", Status: 401},
	{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
	{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
}

var GetAuthContext = Operation[GetAuthContextInput, GetAuthContextOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#GetAuthContext",
		OperationID:         "get-auth-context",
		Method:              "GET",
		Path:                "/api/v1/auth-context",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"cli", "browser", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:account:read", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.account_context.read", Resource: "account", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "auth", Method: "getContext", Paginated: false, Retryable: true},
		Problems:            commonAuthProblems,
	},
}

var CreateDeviceSession = Operation[CreateDeviceSessionInput, CreateDeviceSessionOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#CreateDeviceSession",
		OperationID:         "create-device-session",
		Method:              "POST",
		Path:                "/api/v1/device-sessions",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"cli", "browser", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:device_session:create", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.device_session.create", Resource: "device_session", Action: "create"},
		RateLimitBucket:     "iam_mutation",
		RequestBodyMaxBytes: 4096,
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "auth", Method: "createDeviceSession", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "https://verself.sh/docs/reference/iam/errors#auth-unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.iam.v1#ProviderEmailUnverifiedError", Type: "https://verself.sh/docs/reference/iam/errors#auth-provider-email-unverified", Code: "auth.provider_email_unverified", Status: 403},
			{ShapeID: "verself.iam.v1#AccountLinkRequiredError", Type: "https://verself.sh/docs/reference/iam/errors#auth-account-link-required", Code: "auth.account_link_required", Status: 409},
			{ShapeID: "verself.iam.v1#ProviderSubjectAlreadyLinkedError", Type: "https://verself.sh/docs/reference/iam/errors#auth-provider-subject-already-linked", Code: "auth.provider_subject_already_linked", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
		},
	},
}

var ListDeviceSessions = Operation[ListDeviceSessionsInput, ListDeviceSessionsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#ListDeviceSessions",
		OperationID:         "list-device-sessions",
		Method:              "GET",
		Path:                "/api/v1/device-sessions",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"cli", "browser", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:device_session:read", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.device_session.read", Resource: "device_session", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "auth", Method: "listDeviceSessions", Paginated: false, Retryable: true},
		Problems:            commonAuthProblems,
	},
}

var RevokeDeviceSession = Operation[RevokeDeviceSessionInput, RevokeDeviceSessionOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#RevokeDeviceSession",
		OperationID:         "revoke-device-session",
		Method:              "DELETE",
		Path:                "/api/v1/device-sessions/{sessionId}",
		DefaultStatus:       204,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"cli", "browser", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:device_session:delete", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.device_session.delete", Resource: "device_session", Action: "delete"},
		RateLimitBucket:     "iam_mutation",
		RequestBodyMaxBytes: 1,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "auth", Method: "revokeDeviceSession", Paginated: false, Retryable: false},
		Problems:            commonAuthProblems,
	},
}

var ListAccountConnections = Operation[ListAccountConnectionsInput, ListAccountConnectionsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#ListAccountConnections",
		OperationID:         "list-account-connections",
		Method:              "GET",
		Path:                "/api/v1/account-connections",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"cli", "browser", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:account_connection:read", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.account_connection.read", Resource: "account_connection", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "auth", Method: "listAccountConnections", Paginated: false, Retryable: true},
		Problems:            commonAuthProblems,
	},
}

var RemoveAccountConnection = Operation[RemoveAccountConnectionInput, RemoveAccountConnectionOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.iam.v1#RemoveAccountConnection",
		OperationID:         "remove-account-connection",
		Method:              "DELETE",
		Path:                "/api/v1/account-connections/{connectionId}",
		DefaultStatus:       204,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"cli", "browser", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "iam:account_connection:delete", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "iam.account_connection.delete", Resource: "account_connection", Action: "delete"},
		RateLimitBucket:     "iam_mutation",
		RequestBodyMaxBytes: 1,
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "auth", Method: "removeAccountConnection", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "https://verself.sh/docs/reference/iam/errors#auth-unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.iam.v1#DeviceSessionRequiredError", Type: "https://verself.sh/docs/reference/iam/errors#auth-device-session-required", Code: "auth.device_session_required", Status: 401},
			{ShapeID: "verself.iam.v1#SessionRevokedError", Type: "https://verself.sh/docs/reference/iam/errors#auth-session-revoked", Code: "auth.session_revoked", Status: 401},
			{ShapeID: "verself.iam.v1#ReauthenticationRequiredError", Type: "https://verself.sh/docs/reference/iam/errors#auth-reauthentication-required", Code: "auth.reauthentication_required", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
		},
	},
}
