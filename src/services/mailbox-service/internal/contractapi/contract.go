package contractapi

import (
	"context"
)

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

type IdempotencyKey string

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type TraceParent string

type AccountID string

type EmailAddress string

type EmailID string

type ForwardedEmailID string

type ForwarderMailbox string

type MailBodyHTML string

type MailBodyText string

type MailboxDisplayName string

type MailboxID string

type MailboxServiceURL string

type MailboxStatusError string

type MailboxSyncAccounts map[string]MailboxSyncAccountStatusView

type IdempotencyPayloadMismatchError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type PermissionDeniedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type RateLimitedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ResourceNotFoundError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ServiceUnavailableError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type UnauthenticatedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ValidationFailedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type EmptyInput struct{}

type MailBodyResult struct {
	AccountID AccountID    `json:"account_id" required:"true" minLength:"1" maxLength:"255"`
	EmailID   EmailID      `json:"email_id" required:"true" minLength:"1" maxLength:"255"`
	TextBody  MailBodyText `json:"text_body" required:"true" maxLength:"1048576"`
	HtmlBody  MailBodyHTML `json:"html_body" required:"true" maxLength:"1048576"`
	FetchedAt string       `json:"fetched_at" required:"true"`
}

type MailEmailIdempotentInput struct {
	EmailID        EmailID        `path:"email_id" required:"true" minLength:"1" maxLength:"255"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type MailEmailPathInput struct {
	EmailID EmailID `path:"email_id" required:"true" minLength:"1" maxLength:"255"`
}

type MailMoveInputBody struct {
	MailboxID MailboxID `json:"mailbox_id" required:"true" minLength:"1" maxLength:"255"`
}

type MailMoveInput struct {
	EmailID        EmailID        `path:"email_id" required:"true" minLength:"1" maxLength:"255"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           MailMoveInputBody
}

type MailMutationResult struct {
	EmailID EmailID `json:"email_id" required:"true" minLength:"1" maxLength:"255"`
	Status  string  `json:"status" required:"true"`
}

type MailboxAccountView struct {
	AccountID        AccountID          `json:"account_id" required:"true" minLength:"1" maxLength:"255"`
	EmailAddress     EmailAddress       `json:"email_address" required:"true" minLength:"1" maxLength:"320"`
	DisplayName      MailboxDisplayName `json:"display_name" required:"true" maxLength:"255"`
	DefaultMailboxID *MailboxID         `json:"default_mailbox_id,omitempty" minLength:"1" maxLength:"255"`
}

type MailboxForwarderStatusView struct {
	Enabled                 bool                `json:"enabled" required:"true"`
	Running                 bool                `json:"running" required:"true"`
	Mailbox                 ForwarderMailbox    `json:"mailbox" required:"true" maxLength:"320"`
	ForwardTargetConfigured bool                `json:"forward_target_configured" required:"true"`
	LastError               *MailboxStatusError `json:"last_error,omitempty" maxLength:"4096"`
	LastSyncAt              *string             `json:"last_sync_at,omitempty"`
	LastForwardedAt         *string             `json:"last_forwarded_at,omitempty"`
	LastForwardedEmailID    *ForwardedEmailID   `json:"last_forwarded_email_id,omitempty" maxLength:"255"`
}

type MailboxSyncAccountStatusView struct {
	AccountID       AccountID           `json:"account_id" required:"true" minLength:"1" maxLength:"255"`
	Running         bool                `json:"running" required:"true"`
	Connected       bool                `json:"connected" required:"true"`
	LastSyncAt      *string             `json:"last_sync_at,omitempty"`
	LastEventAt     *string             `json:"last_event_at,omitempty"`
	LastConnectedAt *string             `json:"last_connected_at,omitempty"`
	LastError       *MailboxStatusError `json:"last_error,omitempty" maxLength:"4096"`
}

type MailboxSyncStatusView struct {
	StartedAt       string                      `json:"started_at" required:"true"`
	StalwartBaseURL MailboxServiceURL           `json:"stalwart_base_url" required:"true" maxLength:"1024"`
	PublicBaseURL   MailboxServiceURL           `json:"public_base_url" required:"true" maxLength:"1024"`
	Forwarder       MailboxForwarderStatusView  `json:"forwarder" required:"true"`
	MailboxSync     MailboxSyncWorkerStatusView `json:"mailbox_sync" required:"true"`
}

type MailboxSyncWorkerStatusView struct {
	Running         bool                `json:"running" required:"true"`
	LastDiscoveryAt *string             `json:"last_discovery_at,omitempty"`
	LastError       *MailboxStatusError `json:"last_error,omitempty" maxLength:"4096"`
	Accounts        MailboxSyncAccounts `json:"accounts" required:"true"`
}

type MailAccountOutput struct {
	Body MailboxAccountView
}

type MailBodyOutput struct {
	Body MailBodyResult
}

type MailMutationOutput struct {
	Body MailMutationResult
}

type MailSyncStatusOutputBody struct {
	Status MailboxSyncStatusView `json:"status" required:"true"`
}

type MailSyncStatusOutput struct {
	Body MailSyncStatusOutputBody
}

var Operations = []OperationDescriptor{
	MailAccount.Descriptor,
	MailBody.Descriptor,
	MailFlag.Descriptor,
	MailMove.Descriptor,
	MailMarkRead.Descriptor,
	MailTrash.Descriptor,
	MailUnflag.Descriptor,
	MailMarkUnread.Descriptor,
	MailSyncStatus.Descriptor,
}

var MailAccount = Operation[EmptyInput, MailAccountOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.mailbox.v1#MailAccount",
		OperationID:         "mail-account",
		Method:              "GET",
		Path:                "/api/v1/mail/account",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "mailbox-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "mailbox:account:read", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "mailbox.mail_account", Resource: "mailbox_account", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "mailbox.account", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var MailBody = Operation[MailEmailPathInput, MailBodyOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.mailbox.v1#MailBody",
		OperationID:         "mail-body",
		Method:              "GET",
		Path:                "/api/v1/mail/emails/{email_id}/body",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "mailbox-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "mailbox:mail:read", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "mailbox.mail_body", Resource: "mailbox_email", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "mailbox.mail", Method: "body", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var MailFlag = Operation[MailEmailIdempotentInput, MailMutationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.mailbox.v1#MailFlag",
		OperationID:         "mail-flag",
		Method:              "POST",
		Path:                "/api/v1/mail/emails/{email_id}/flag",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "mailbox-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "mailbox:mail:write", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "mailbox.mail_flag", Resource: "mailbox_email", Action: "write"},
		RateLimitBucket:     "mail_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "mailbox.mail", Method: "flag", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var MailMove = Operation[MailMoveInput, MailMutationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.mailbox.v1#MailMove",
		OperationID:         "mail-move",
		Method:              "POST",
		Path:                "/api/v1/mail/emails/{email_id}/move",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "mailbox-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "mailbox:mail:write", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "mailbox.mail_move", Resource: "mailbox_email", Action: "write"},
		RateLimitBucket:     "mail_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "mailbox.mail", Method: "move", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var MailMarkRead = Operation[MailEmailIdempotentInput, MailMutationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.mailbox.v1#MailMarkRead",
		OperationID:         "mail-mark-read",
		Method:              "POST",
		Path:                "/api/v1/mail/emails/{email_id}/read",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "mailbox-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "mailbox:mail:write", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "mailbox.mail_mark_read", Resource: "mailbox_email", Action: "write"},
		RateLimitBucket:     "mail_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "mailbox.mail", Method: "markRead", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var MailTrash = Operation[MailEmailIdempotentInput, MailMutationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.mailbox.v1#MailTrash",
		OperationID:         "mail-trash",
		Method:              "POST",
		Path:                "/api/v1/mail/emails/{email_id}/trash",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "mailbox-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "mailbox:mail:write", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "mailbox.mail_trash", Resource: "mailbox_email", Action: "write"},
		RateLimitBucket:     "mail_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "mailbox.mail", Method: "trash", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var MailUnflag = Operation[MailEmailIdempotentInput, MailMutationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.mailbox.v1#MailUnflag",
		OperationID:         "mail-unflag",
		Method:              "POST",
		Path:                "/api/v1/mail/emails/{email_id}/unflag",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "mailbox-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "mailbox:mail:write", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "mailbox.mail_unflag", Resource: "mailbox_email", Action: "write"},
		RateLimitBucket:     "mail_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "mailbox.mail", Method: "unflag", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var MailMarkUnread = Operation[MailEmailIdempotentInput, MailMutationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.mailbox.v1#MailMarkUnread",
		OperationID:         "mail-mark-unread",
		Method:              "POST",
		Path:                "/api/v1/mail/emails/{email_id}/unread",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "mailbox-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "mailbox:mail:write", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "mailbox.mail_mark_unread", Resource: "mailbox_email", Action: "write"},
		RateLimitBucket:     "mail_mutation",
		RequestBodyMaxBytes: 1024,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "mailbox.mail", Method: "markUnread", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var MailSyncStatus = Operation[EmptyInput, MailSyncStatusOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.mailbox.v1#MailSyncStatus",
		OperationID:         "mail-sync-status",
		Method:              "GET",
		Path:                "/api/v1/mail/sync/status",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "mailbox-service", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "mailbox:sync_status:read", OrganizationSource: "request_subject", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "mailbox.mail_sync_status", Resource: "mailbox_sync_status", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "mailbox.sync", Method: "status", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	MailAccount(context.Context, *EmptyInput) (*MailAccountOutput, error)
	MailBody(context.Context, *MailEmailPathInput) (*MailBodyOutput, error)
	MailFlag(context.Context, *MailEmailIdempotentInput) (*MailMutationOutput, error)
	MailMove(context.Context, *MailMoveInput) (*MailMutationOutput, error)
	MailMarkRead(context.Context, *MailEmailIdempotentInput) (*MailMutationOutput, error)
	MailTrash(context.Context, *MailEmailIdempotentInput) (*MailMutationOutput, error)
	MailUnflag(context.Context, *MailEmailIdempotentInput) (*MailMutationOutput, error)
	MailMarkUnread(context.Context, *MailEmailIdempotentInput) (*MailMutationOutput, error)
	MailSyncStatus(context.Context, *EmptyInput) (*MailSyncStatusOutput, error)
}

type MailAccountHandler = Handler[EmptyInput, MailAccountOutput]

type MailBodyHandler = Handler[MailEmailPathInput, MailBodyOutput]

type MailFlagHandler = Handler[MailEmailIdempotentInput, MailMutationOutput]

type MailMoveHandler = Handler[MailMoveInput, MailMutationOutput]

type MailMarkReadHandler = Handler[MailEmailIdempotentInput, MailMutationOutput]

type MailTrashHandler = Handler[MailEmailIdempotentInput, MailMutationOutput]

type MailUnflagHandler = Handler[MailEmailIdempotentInput, MailMutationOutput]

type MailMarkUnreadHandler = Handler[MailEmailIdempotentInput, MailMutationOutput]

type MailSyncStatusHandler = Handler[EmptyInput, MailSyncStatusOutput]
