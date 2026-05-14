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

type ResourceName string

type TraceParent string

type ActorID string

type BackendDispatchID string

type BlobDownloadURL string

type BranchName string

type CheckoutGrantID string

type CheckoutGrantToken string

type CredentialExpirySeconds int64

type FailureReason string

type GitCredentialID string

type GitCredentialLabel string

type GitObjectEncoding string

type GitObjectName string

type GitObjectSha string

type GitObjectType string

type GitRef string

type GitScope string

type GitToken string

type GitTokenPrefix string

type GitURL string

type GitUsername string

type OrgID string

type OrgSlug string

type ProjectID string

type ProjectSlug string

type RepositoryDescription string

type RepositoryID string

type RepositoryName string

type RepositoryPath string

type RepositoryVersion int

type RepositoryVisibility string

type SafeByteCount int64

type SourceBackend string

type SourceState string

type TraceID string

type WorkflowInputName string

type WorkflowInputValue string

type WorkflowPath string

type WorkflowRunID string

type GitScopes []GitScope

type Refs []SourceRefView

type Repositories []RepositorySummary

type TreeEntries []TreeEntry

type WorkflowRuns []WorkflowRunSummary

type WorkflowInputs map[string]WorkflowInputValue

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

type CheckoutGrantSummary struct {
	GrantID      CheckoutGrantID    `json:"grant_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ResourceName ResourceName       `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	RepoID       RepositoryID       `json:"repo_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Ref          GitRef             `json:"ref" required:"true" minLength:"1" maxLength:"1024"`
	Token        CheckoutGrantToken `json:"token" required:"true" minLength:"1" maxLength:"4096"`
	ExpiresAt    string             `json:"expires_at" required:"true"`
}

type CreateSourceCheckoutGrantInputBody struct {
	Ref *GitRef `json:"ref,omitempty" minLength:"1" maxLength:"1024"`
}

type CreateSourceCheckoutGrantInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	RepoID         RepositoryID   `path:"repo_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Body           CreateSourceCheckoutGrantInputBody
}

type CreateSourceGitCredentialInputBody struct {
	ExpiresInSeconds *CredentialExpirySeconds `json:"expires_in_seconds,omitempty" minimum:"60" maximum:"7776000"`
	Label            *GitCredentialLabel      `json:"label,omitempty" minLength:"1" maxLength:"128"`
	Scopes           GitScopes                `json:"scopes" required:"true"`
}

type CreateSourceGitCredentialInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateSourceGitCredentialInputBody
}

type CreateSourceRepositoryInputBody struct {
	DefaultBranch *BranchName            `json:"default_branch,omitempty" minLength:"1" maxLength:"128"`
	Description   *RepositoryDescription `json:"description,omitempty" maxLength:"1024"`
	ProjectID     ProjectID              `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type CreateSourceRepositoryInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateSourceRepositoryInputBody
}

type CreateSourceWorkflowRunInputBody struct {
	Inputs       *WorkflowInputs `json:"inputs,omitempty" maxLength:"64"`
	ProjectID    ProjectID       `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Ref          *GitRef         `json:"ref,omitempty" minLength:"1" maxLength:"1024"`
	WorkflowPath WorkflowPath    `json:"workflow_path" required:"true" minLength:"1" maxLength:"4096"`
}

type CreateSourceWorkflowRunInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	RepoID         RepositoryID   `path:"repo_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Body           CreateSourceWorkflowRunInputBody
}

type GetSourceBlobInput struct {
	Path   RepositoryPath `query:"path" required:"true" minLength:"1" maxLength:"8192"`
	Ref    GitRef         `query:"ref" minLength:"1" maxLength:"1024"`
	RepoID RepositoryID   `path:"repo_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type GetSourceTreeInput struct {
	Path   RepositoryPath `query:"path" minLength:"1" maxLength:"8192"`
	Ref    GitRef         `query:"ref" minLength:"1" maxLength:"1024"`
	RepoID RepositoryID   `path:"repo_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type GitCredentialSummary struct {
	CredentialID GitCredentialID `json:"credential_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ResourceName ResourceName    `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	OrgID        OrgID           `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	Username     GitUsername     `json:"username" required:"true" minLength:"1" maxLength:"128"`
	Token        GitToken        `json:"token" required:"true" minLength:"1" maxLength:"4096"`
	Scopes       GitScopes       `json:"scopes" required:"true"`
	TokenPrefix  GitTokenPrefix  `json:"token_prefix" required:"true" minLength:"1" maxLength:"32"`
	ExpiresAt    string          `json:"expires_at" required:"true"`
	CreatedAt    string          `json:"created_at" required:"true"`
}

type ListSourceRepositoriesInput struct {
	ProjectID ProjectID `query:"project_id" pattern:"^[0-9a-fA-F-]{36}$"`
}

type RepositoryPathInput struct {
	RepoID RepositoryID `path:"repo_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type RepositorySummary struct {
	RepoID              RepositoryID          `json:"repo_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ResourceName        ResourceName          `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	OrgID               OrgID                 `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	ProjectID           ProjectID             `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ProjectResourceName ResourceName          `json:"projectResourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	OrgSlug             OrgSlug               `json:"org_slug" required:"true" minLength:"1" maxLength:"128"`
	ProjectSlug         ProjectSlug           `json:"project_slug" required:"true" minLength:"1" maxLength:"128"`
	GitHTTPURL          GitURL                `json:"git_http_url" required:"true" minLength:"1" maxLength:"8192"`
	Name                RepositoryName        `json:"name" required:"true" minLength:"1" maxLength:"255"`
	Description         RepositoryDescription `json:"description" required:"true" maxLength:"1024"`
	DefaultBranch       BranchName            `json:"default_branch" required:"true" minLength:"1" maxLength:"128"`
	Backend             SourceBackend         `json:"backend" required:"true" minLength:"1" maxLength:"128"`
	Visibility          RepositoryVisibility  `json:"visibility" required:"true" minLength:"1" maxLength:"128"`
	State               SourceState           `json:"state" required:"true" minLength:"1" maxLength:"128"`
	Version             RepositoryVersion     `json:"version" required:"true" minimum:"0" maximum:"2147483647"`
	LastPushedAt        *string               `json:"last_pushed_at,omitempty"`
	CreatedAt           string                `json:"created_at" required:"true"`
	UpdatedAt           string                `json:"updated_at" required:"true"`
}

type SourceBlobView struct {
	Path        RepositoryPath    `json:"path" required:"true" minLength:"1" maxLength:"8192"`
	Name        GitObjectName     `json:"name" required:"true" minLength:"1" maxLength:"255"`
	Encoding    GitObjectEncoding `json:"encoding" required:"true" minLength:"1" maxLength:"128"`
	Content     string            `json:"content" required:"true"`
	Size        SafeByteCount     `json:"size" required:"true" minimum:"0" maximum:"9007199254740991"`
	Sha         GitObjectSha      `json:"sha" required:"true" pattern:"^[0-9a-fA-F]{40,64}$"`
	DownloadURL *BlobDownloadURL  `json:"download_url,omitempty" minLength:"1" maxLength:"8192"`
}

type SourceRefView struct {
	Name   GitRef       `json:"name" required:"true" minLength:"1" maxLength:"1024"`
	Commit GitObjectSha `json:"commit" required:"true" pattern:"^[0-9a-fA-F]{40,64}$"`
}

type TreeEntry struct {
	Path RepositoryPath `json:"path" required:"true" minLength:"1" maxLength:"8192"`
	Type GitObjectType  `json:"type" required:"true" minLength:"1" maxLength:"128"`
	Size SafeByteCount  `json:"size" required:"true" minimum:"0" maximum:"9007199254740991"`
	Sha  GitObjectSha   `json:"sha" required:"true" pattern:"^[0-9a-fA-F]{40,64}$"`
}

type WorkflowRunPathInput struct {
	WorkflowRunID WorkflowRunID `path:"workflow_run_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type WorkflowRunSummary struct {
	WorkflowRunID          WorkflowRunID      `json:"workflow_run_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ResourceName           ResourceName       `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	OrgID                  OrgID              `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	ProjectID              ProjectID          `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ProjectResourceName    ResourceName       `json:"projectResourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	RepoID                 RepositoryID       `json:"repo_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	RepositoryResourceName ResourceName       `json:"repositoryResourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	ActorID                ActorID            `json:"actor_id" required:"true" minLength:"1" maxLength:"512"`
	Backend                SourceBackend      `json:"backend" required:"true" minLength:"1" maxLength:"128"`
	WorkflowPath           WorkflowPath       `json:"workflow_path" required:"true" minLength:"1" maxLength:"4096"`
	Ref                    GitRef             `json:"ref" required:"true" minLength:"1" maxLength:"1024"`
	Inputs                 WorkflowInputs     `json:"inputs" required:"true" maxLength:"64"`
	State                  SourceState        `json:"state" required:"true" minLength:"1" maxLength:"128"`
	BackendDispatchID      *BackendDispatchID `json:"backend_dispatch_id,omitempty" minLength:"1" maxLength:"1024"`
	FailureReason          *FailureReason     `json:"failure_reason,omitempty" maxLength:"8192"`
	TraceID                *TraceID           `json:"trace_id,omitempty" minLength:"1" maxLength:"128"`
	DispatchedAt           *string            `json:"dispatched_at,omitempty"`
	CreatedAt              string             `json:"created_at" required:"true"`
	UpdatedAt              string             `json:"updated_at" required:"true"`
}

type GitCredentialOutput struct {
	Body GitCredentialSummary
}

type ListSourceRepositoriesOutputBody struct {
	Repositories Repositories `json:"repositories" required:"true"`
}

type ListSourceRepositoriesOutput struct {
	Body ListSourceRepositoriesOutputBody
}

type RepositoryOutput struct {
	Body RepositorySummary
}

type GetSourceBlobOutput struct {
	Body SourceBlobView
}

type CheckoutGrantOutput struct {
	Body CheckoutGrantSummary
}

type ListSourceRefsOutputBody struct {
	Refs Refs `json:"refs" required:"true"`
}

type ListSourceRefsOutput struct {
	Body ListSourceRefsOutputBody
}

type GetSourceTreeOutputBody struct {
	Entries TreeEntries `json:"entries" required:"true"`
}

type GetSourceTreeOutput struct {
	Body GetSourceTreeOutputBody
}

type ListSourceWorkflowRunsOutputBody struct {
	WorkflowRuns WorkflowRuns `json:"workflow_runs" required:"true"`
}

type ListSourceWorkflowRunsOutput struct {
	Body ListSourceWorkflowRunsOutputBody
}

type WorkflowRunOutput struct {
	Body WorkflowRunSummary
}

var Operations = []OperationDescriptor{
	CreateSourceGitCredential.Descriptor,
	ListSourceRepositories.Descriptor,
	CreateSourceRepository.Descriptor,
	GetSourceRepository.Descriptor,
	GetSourceBlob.Descriptor,
	CreateSourceCheckoutGrant.Descriptor,
	ListSourceRefs.Descriptor,
	GetSourceTree.Descriptor,
	ListSourceWorkflowRuns.Descriptor,
	CreateSourceWorkflowRun.Descriptor,
	GetSourceWorkflowRun.Descriptor,
}

var CreateSourceGitCredential = Operation[CreateSourceGitCredentialInput, GitCredentialOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#CreateSourceGitCredential",
		OperationID:         "create-source-git-credential",
		Method:              "POST",
		Path:                "/api/v1/git-credentials",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:git_credential:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.git_credential.create", Resource: "source_git_credential", Action: "create"},
		RateLimitBucket:     "source_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "source.gitCredentials", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ListSourceRepositories = Operation[ListSourceRepositoriesInput, ListSourceRepositoriesOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#ListSourceRepositories",
		OperationID:         "list-source-repositories",
		Method:              "GET",
		Path:                "/api/v1/repos",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:repo:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.repo.list", Resource: "source_repository", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "source.repositories", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateSourceRepository = Operation[CreateSourceRepositoryInput, RepositoryOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#CreateSourceRepository",
		OperationID:         "create-source-repository",
		Method:              "POST",
		Path:                "/api/v1/repos",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:repo:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.repo.create", Resource: "source_repository", Action: "create"},
		RateLimitBucket:     "source_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "source.repositories", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var GetSourceRepository = Operation[RepositoryPathInput, RepositoryOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#GetSourceRepository",
		OperationID:         "get-source-repository",
		Method:              "GET",
		Path:                "/api/v1/repos/{repo_id}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:repo:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.repo.read", Resource: "source_repository", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "source.repositories", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetSourceBlob = Operation[GetSourceBlobInput, GetSourceBlobOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#GetSourceBlob",
		OperationID:         "get-source-blob",
		Method:              "GET",
		Path:                "/api/v1/repos/{repo_id}/blob",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:repo:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.blob.get", Resource: "source_blob", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "source.blobs", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateSourceCheckoutGrant = Operation[CreateSourceCheckoutGrantInput, CheckoutGrantOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#CreateSourceCheckoutGrant",
		OperationID:         "create-source-checkout-grant",
		Method:              "POST",
		Path:                "/api/v1/repos/{repo_id}/checkout-grants",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:checkout:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.checkout_grant.create", Resource: "source_checkout_grant", Action: "create"},
		RateLimitBucket:     "source_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "source.checkoutGrants", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
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

var ListSourceRefs = Operation[RepositoryPathInput, ListSourceRefsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#ListSourceRefs",
		OperationID:         "list-source-refs",
		Method:              "GET",
		Path:                "/api/v1/repos/{repo_id}/refs",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:repo:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.refs.list", Resource: "source_ref", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "source.refs", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var GetSourceTree = Operation[GetSourceTreeInput, GetSourceTreeOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#GetSourceTree",
		OperationID:         "get-source-tree",
		Method:              "GET",
		Path:                "/api/v1/repos/{repo_id}/tree",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:repo:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.tree.get", Resource: "source_tree", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "source.trees", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var ListSourceWorkflowRuns = Operation[RepositoryPathInput, ListSourceWorkflowRunsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#ListSourceWorkflowRuns",
		OperationID:         "list-source-workflow-runs",
		Method:              "GET",
		Path:                "/api/v1/repos/{repo_id}/workflow-runs",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:workflow:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.workflow_run.list", Resource: "source_workflow_run", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "source.workflowRuns", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateSourceWorkflowRun = Operation[CreateSourceWorkflowRunInput, WorkflowRunOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#CreateSourceWorkflowRun",
		OperationID:         "create-source-workflow-run",
		Method:              "POST",
		Path:                "/api/v1/repos/{repo_id}/workflow-runs",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:workflow:write", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.workflow.dispatch", Resource: "source_workflow_run", Action: "create"},
		RateLimitBucket:     "source_mutation",
		RequestBodyMaxBytes: 262144,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "source.workflowRuns", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
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

var GetSourceWorkflowRun = Operation[WorkflowRunPathInput, WorkflowRunOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.source.v1#GetSourceWorkflowRun",
		OperationID:         "get-source-workflow-run",
		Method:              "GET",
		Path:                "/api/v1/workflow-runs/{workflow_run_id}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "source:workflow:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "source.workflow_run.read", Resource: "source_workflow_run", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "source.workflowRuns", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	CreateSourceGitCredential(context.Context, *CreateSourceGitCredentialInput) (*GitCredentialOutput, error)
	ListSourceRepositories(context.Context, *ListSourceRepositoriesInput) (*ListSourceRepositoriesOutput, error)
	CreateSourceRepository(context.Context, *CreateSourceRepositoryInput) (*RepositoryOutput, error)
	GetSourceRepository(context.Context, *RepositoryPathInput) (*RepositoryOutput, error)
	GetSourceBlob(context.Context, *GetSourceBlobInput) (*GetSourceBlobOutput, error)
	CreateSourceCheckoutGrant(context.Context, *CreateSourceCheckoutGrantInput) (*CheckoutGrantOutput, error)
	ListSourceRefs(context.Context, *RepositoryPathInput) (*ListSourceRefsOutput, error)
	GetSourceTree(context.Context, *GetSourceTreeInput) (*GetSourceTreeOutput, error)
	ListSourceWorkflowRuns(context.Context, *RepositoryPathInput) (*ListSourceWorkflowRunsOutput, error)
	CreateSourceWorkflowRun(context.Context, *CreateSourceWorkflowRunInput) (*WorkflowRunOutput, error)
	GetSourceWorkflowRun(context.Context, *WorkflowRunPathInput) (*WorkflowRunOutput, error)
}

type CreateSourceGitCredentialHandler = Handler[CreateSourceGitCredentialInput, GitCredentialOutput]

type ListSourceRepositoriesHandler = Handler[ListSourceRepositoriesInput, ListSourceRepositoriesOutput]

type CreateSourceRepositoryHandler = Handler[CreateSourceRepositoryInput, RepositoryOutput]

type GetSourceRepositoryHandler = Handler[RepositoryPathInput, RepositoryOutput]

type GetSourceBlobHandler = Handler[GetSourceBlobInput, GetSourceBlobOutput]

type CreateSourceCheckoutGrantHandler = Handler[CreateSourceCheckoutGrantInput, CheckoutGrantOutput]

type ListSourceRefsHandler = Handler[RepositoryPathInput, ListSourceRefsOutput]

type GetSourceTreeHandler = Handler[GetSourceTreeInput, GetSourceTreeOutput]

type ListSourceWorkflowRunsHandler = Handler[RepositoryPathInput, ListSourceWorkflowRunsOutput]

type CreateSourceWorkflowRunHandler = Handler[CreateSourceWorkflowRunInput, WorkflowRunOutput]

type GetSourceWorkflowRunHandler = Handler[WorkflowRunPathInput, WorkflowRunOutput]
