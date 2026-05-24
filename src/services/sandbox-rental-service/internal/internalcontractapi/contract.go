package internalcontractapi

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

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type TraceParent string

type OrgID string

type ProjectID string

type Provider string

type ProviderOwner string

type ProviderRepo string

type ProviderRepositoryID string

type RepositoryFullName string

type SourceRepositoryID string

type AttemptID string

type ExecutionID string

type CorrelationID string

type DecimalUint64 string

type HeadBranch string

type HeadSHA string

type JobName string

type JobShapeId string

type GenerationSetHash string

type RunnerBootstrapKind string

type RunnerBootstrapPayload string

type RunnerName string

type PromotionPolicy string

type WorkflowName string

type WorkflowPath string

type TrustClass string

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

type InternalRegisterRunnerRepositoryInputBody struct {
	OrgID                OrgID                `json:"org_id" required:"true" minLength:"1" maxLength:"128"`
	ProjectID            ProjectID            `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Provider             Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderOwner        ProviderOwner        `json:"provider_owner" required:"true" minLength:"1" maxLength:"255"`
	ProviderRepo         ProviderRepo         `json:"provider_repo" required:"true" minLength:"1" maxLength:"255"`
	ProviderRepositoryID ProviderRepositoryID `json:"provider_repository_id" required:"true" minLength:"1" maxLength:"512"`
	RepositoryFullName   *RepositoryFullName  `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	SourceRepositoryID   *SourceRepositoryID  `json:"source_repository_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
}

type InternalRegisterRunnerRepositoryInput struct {
	Body InternalRegisterRunnerRepositoryInputBody
}

type RunnerRepositoryRegistration struct {
	ProjectID            ProjectID            `json:"project_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Provider             Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderRepositoryID ProviderRepositoryID `json:"provider_repository_id" required:"true" minLength:"1" maxLength:"512"`
	SourceRepositoryID   *SourceRepositoryID  `json:"source_repository_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	State                string               `json:"state" required:"true"`
}

type InternalRegisterRunnerRepositoryOutputBody struct {
	Registration RunnerRepositoryRegistration `json:"registration" required:"true"`
}

type InternalRegisterRunnerRepositoryOutput struct {
	Body InternalRegisterRunnerRepositoryOutputBody
}

type RunnerLabels []string

type RunnerJobObservation struct {
	Provider               Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderJobID          DecimalUint64        `json:"provider_job_id" required:"true" pattern:"^[0-9]+$"`
	ProviderRepositoryID   ProviderRepositoryID `json:"provider_repository_id" required:"true" minLength:"1" maxLength:"512"`
	ProviderInstallationID *DecimalUint64       `json:"provider_installation_id,omitempty" pattern:"^[0-9]+$"`
	RepositoryFullName     *RepositoryFullName  `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	ProviderRunID          *DecimalUint64       `json:"provider_run_id,omitempty" pattern:"^[0-9]+$"`
	ProviderRunAttempt     *DecimalUint64       `json:"provider_run_attempt,omitempty" pattern:"^[0-9]+$"`
	JobName                *JobName             `json:"job_name,omitempty" minLength:"1" maxLength:"1024"`
	HeadSHA                *HeadSHA             `json:"head_sha,omitempty" minLength:"1" maxLength:"128"`
	HeadBranch             *HeadBranch          `json:"head_branch,omitempty" minLength:"1" maxLength:"1024"`
	WorkflowName           *WorkflowName        `json:"workflow_name,omitempty" minLength:"1" maxLength:"1024"`
	Status                 string               `json:"status" required:"true"`
	Conclusion             *string              `json:"conclusion,omitempty"`
	Labels                 RunnerLabels         `json:"labels,omitempty"`
	RunnerID               *DecimalUint64       `json:"runner_id,omitempty" pattern:"^[0-9]+$"`
	RunnerName             *RunnerName          `json:"runner_name,omitempty" minLength:"1" maxLength:"512"`
	StartedAt              *string              `json:"started_at,omitempty"`
	CompletedAt            *string              `json:"completed_at,omitempty"`
	DeliveryID             *CorrelationID       `json:"delivery_id,omitempty" minLength:"1" maxLength:"512"`
}

type RunnerWorkflowRunObservation struct {
	Provider               Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderRepositoryID   ProviderRepositoryID `json:"provider_repository_id" required:"true" minLength:"1" maxLength:"512"`
	ProviderInstallationID *DecimalUint64       `json:"provider_installation_id,omitempty" pattern:"^[0-9]+$"`
	ProviderRunID          *DecimalUint64       `json:"provider_run_id,omitempty" pattern:"^[0-9]+$"`
	ProviderRunAttempt     *DecimalUint64       `json:"provider_run_attempt,omitempty" pattern:"^[0-9]+$"`
	RepositoryFullName     *RepositoryFullName  `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	EventName              *string              `json:"event_name,omitempty"`
	HeadSHA                *HeadSHA             `json:"head_sha,omitempty" minLength:"1" maxLength:"128"`
	HeadBranch             *HeadBranch          `json:"head_branch,omitempty" minLength:"1" maxLength:"1024"`
	HeadRepositoryFullName *RepositoryFullName  `json:"head_repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	BaseSHA                *HeadSHA             `json:"base_sha,omitempty" minLength:"1" maxLength:"128"`
	BaseBranch             *HeadBranch          `json:"base_branch,omitempty" minLength:"1" maxLength:"1024"`
	WorkflowPath           *WorkflowPath        `json:"workflow_path,omitempty" minLength:"1" maxLength:"4096"`
	PullRequestNumber      *DecimalUint64       `json:"pull_request_number,omitempty" pattern:"^[0-9]+$"`
	CommitCount            *DecimalUint64       `json:"commit_count,omitempty" pattern:"^[0-9]+$"`
}

type RunnerCacheManifestContentBase64 string

type RunnerCacheManifest struct {
	SourceKind    string                           `json:"source_kind" required:"true" minLength:"1" maxLength:"64"`
	SourcePath    string                           `json:"source_path" required:"true" minLength:"1" maxLength:"4096"`
	SourceSHA     string                           `json:"source_sha" required:"true" minLength:"1" maxLength:"128"`
	ContentBase64 RunnerCacheManifestContentBase64 `json:"content_base64" required:"true" minLength:"1" maxLength:"87384"`
}

type InternalSubmitRunnerJobInputBody struct {
	Observation      RunnerJobObservation          `json:"observation" required:"true"`
	RunnerName       RunnerName                    `json:"runner_name" required:"true" minLength:"1" maxLength:"512"`
	RunnerID         *DecimalUint64                `json:"runner_id,omitempty" pattern:"^[0-9]+$"`
	BootstrapKind    RunnerBootstrapKind           `json:"bootstrap_kind" required:"true" minLength:"1" maxLength:"64"`
	BootstrapPayload RunnerBootstrapPayload        `json:"bootstrap_payload" required:"true" minLength:"1" maxLength:"32768"`
	WorkflowRun      *RunnerWorkflowRunObservation `json:"workflow_run,omitempty"`
	CacheManifest    *RunnerCacheManifest          `json:"cache_manifest,omitempty"`
}

type InternalSubmitRunnerJobInput struct {
	Body InternalSubmitRunnerJobInputBody
}

type InternalSubmitRunnerJobOutputBody struct {
	AllocationID AttemptID      `json:"allocation_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	ExecutionID  ExecutionID    `json:"execution_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	AttemptID    AttemptID      `json:"attempt_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	RunnerName   RunnerName     `json:"runner_name" required:"true" minLength:"1" maxLength:"512"`
	RunnerID     *DecimalUint64 `json:"runner_id,omitempty" pattern:"^[0-9]+$"`
	Created      bool           `json:"created" required:"true"`
}

type InternalSubmitRunnerJobOutput struct {
	Body InternalSubmitRunnerJobOutputBody
}

type InternalGetRunnerAllocationInput struct {
	AllocationID AttemptID `path:"allocation_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
}

type RunnerAllocationStatus struct {
	AllocationID           AttemptID            `json:"allocation_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	Provider               Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderRepositoryID   ProviderRepositoryID `json:"provider_repository_id" required:"true" pattern:"^[0-9]+$"`
	ProviderInstallationID *DecimalUint64       `json:"provider_installation_id,omitempty" pattern:"^[0-9]+$"`
	RunnerClass            *string              `json:"runner_class,omitempty" minLength:"1" maxLength:"255"`
	RunnerName             *RunnerName          `json:"runner_name,omitempty" minLength:"1" maxLength:"512"`
	RunnerID               *DecimalUint64       `json:"runner_id,omitempty" pattern:"^[0-9]+$"`
	OriginProviderJobID    *DecimalUint64       `json:"origin_provider_job_id,omitempty" pattern:"^[0-9]+$"`
	AssignedProviderJobID  *DecimalUint64       `json:"assigned_provider_job_id,omitempty" pattern:"^[0-9]+$"`
	ExecutionID            *ExecutionID         `json:"execution_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	AttemptID              *AttemptID           `json:"attempt_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	State                  string               `json:"state" required:"true"`
	FailureReason          *string              `json:"failure_reason,omitempty"`
	ExecutionState         *string              `json:"execution_state,omitempty"`
	AttemptState           *string              `json:"attempt_state,omitempty"`
}

type InternalGetRunnerAllocationOutputBody struct {
	Allocation RunnerAllocationStatus `json:"allocation" required:"true"`
}

type InternalGetRunnerAllocationOutput struct {
	Body InternalGetRunnerAllocationOutputBody
}

type InternalObserveRunnerJobInputBody struct {
	Observation RunnerJobObservation          `json:"observation" required:"true"`
	WorkflowRun *RunnerWorkflowRunObservation `json:"workflow_run,omitempty"`
}

type InternalObserveRunnerJobInput struct {
	Body InternalObserveRunnerJobInputBody
}

type InternalObserveRunnerJobOutputBody struct {
	State        string       `json:"state" required:"true"`
	AllocationID *AttemptID   `json:"allocation_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	ExecutionID  *ExecutionID `json:"execution_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
	AttemptID    *AttemptID   `json:"attempt_id,omitempty" pattern:"^[0-9a-fA-F-]{36}$"`
}

type InternalObserveRunnerJobOutput struct {
	Body InternalObserveRunnerJobOutputBody
}

type InternalObserveRunnerWorkflowRunInputBody struct {
	WorkflowRun RunnerWorkflowRunObservation `json:"workflow_run" required:"true"`
}

type InternalObserveRunnerWorkflowRunInput struct {
	Body InternalObserveRunnerWorkflowRunInputBody
}

type InternalObserveRunnerWorkflowRunOutputBody struct {
	State string `json:"state" required:"true"`
}

type InternalObserveRunnerWorkflowRunOutput struct {
	Body InternalObserveRunnerWorkflowRunOutputBody
}

type GoldenSnapshotBarrierEvidence struct {
	Provider                 Provider             `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ProviderInstallationID   *DecimalUint64       `json:"provider_installation_id,omitempty" pattern:"^[0-9]+$"`
	ProviderRepositoryID     ProviderRepositoryID `json:"provider_repository_id" required:"true" pattern:"^[0-9]+$"`
	ProviderRunID            DecimalUint64        `json:"provider_run_id" required:"true" pattern:"^[0-9]+$"`
	ProviderRunAttempt       DecimalUint64        `json:"provider_run_attempt" required:"true" pattern:"^[0-9]+$"`
	ProviderJobID            DecimalUint64        `json:"provider_job_id" required:"true" pattern:"^[0-9]+$"`
	RepositoryFullName       *RepositoryFullName  `json:"repository_full_name,omitempty" minLength:"1" maxLength:"1024"`
	HeadSHA                  *HeadSHA             `json:"head_sha,omitempty" minLength:"1" maxLength:"128"`
	Conclusion               *string              `json:"conclusion,omitempty"`
	RunnerID                 *DecimalUint64       `json:"runner_id,omitempty" pattern:"^[0-9]+$"`
	RunnerName               *RunnerName          `json:"runner_name,omitempty" minLength:"1" maxLength:"512"`
	ExecutionID              ExecutionID          `json:"execution_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	AttemptID                AttemptID            `json:"attempt_id" required:"true" pattern:"^[0-9a-fA-F-]{36}$"`
	LeaseID                  *string              `json:"lease_id,omitempty"`
	JobShapeID               *JobShapeId          `json:"job_shape_id,omitempty" minLength:"1" maxLength:"128"`
	DurableGenerationSetHash *GenerationSetHash   `json:"durable_generation_set_hash,omitempty" minLength:"1" maxLength:"128"`
	TrustClass               *TrustClass          `json:"trust_class,omitempty" minLength:"1" maxLength:"128"`
	PromotionPolicy          *PromotionPolicy     `json:"promotion_policy,omitempty" minLength:"1" maxLength:"128"`
}

type InternalRequestGoldenSnapshotBarrierInputBody struct {
	Evidence GoldenSnapshotBarrierEvidence `json:"evidence" required:"true"`
}

type InternalRequestGoldenSnapshotBarrierInput struct {
	Body InternalRequestGoldenSnapshotBarrierInputBody
}

type InternalRequestGoldenSnapshotBarrierOutputBody struct {
	State  string  `json:"state" required:"true"`
	Reason *string `json:"reason,omitempty"`
}

type InternalRequestGoldenSnapshotBarrierOutput struct {
	Body InternalRequestGoldenSnapshotBarrierOutputBody
}

var Operations = []OperationDescriptor{
	InternalRegisterRunnerRepository.Descriptor,
	InternalSubmitRunnerJob.Descriptor,
	InternalGetRunnerAllocation.Descriptor,
	InternalObserveRunnerJob.Descriptor,
	InternalObserveRunnerWorkflowRun.Descriptor,
	InternalRequestGoldenSnapshotBarrier.Descriptor,
}

var InternalRegisterRunnerRepository = Operation[InternalRegisterRunnerRepositoryInput, InternalRegisterRunnerRepositoryOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#InternalRegisterRunnerRepository",
		OperationID:         "internal-register-runner-repository",
		Method:              "POST",
		Path:                "/internal/v1/runner/repositories",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "sandbox-rental-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:runner_repository:register", OrganizationSource: "body_org_id", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "sandbox.runner_repository.register", Resource: "runner_repository", Action: "register"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "sandboxInternal.runnerRepositories", Method: "register", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var InternalSubmitRunnerJob = Operation[InternalSubmitRunnerJobInput, InternalSubmitRunnerJobOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#InternalSubmitRunnerJob",
		OperationID:         "internal-submit-runner-job",
		Method:              "POST",
		Path:                "/internal/v1/runner/jobs",
		DefaultStatus:       201,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "sandbox-rental-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:runner_job:submit", OrganizationSource: "request_id"},
		Audit:               AuditDescriptor{Event: "sandbox.runner_job.submit", Resource: "runner_job", Action: "submit"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 131072,
		SDK:                 SDKDescriptor{Module: "sandboxInternal.runnerJobs", Method: "submit", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var InternalGetRunnerAllocation = Operation[InternalGetRunnerAllocationInput, InternalGetRunnerAllocationOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#InternalGetRunnerAllocation",
		OperationID:         "internal-get-runner-allocation",
		Method:              "GET",
		Path:                "/internal/v1/runner/allocations/{allocation_id}",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "sandbox-rental-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:runner_job:observe", OrganizationSource: "request_id"},
		Audit:               AuditDescriptor{Event: "sandbox.runner_job.observe", Resource: "runner_repository", Action: "read"},
		RateLimitBucket:     "internal_read",
		RequestBodyMaxBytes: 1024,
		SDK:                 SDKDescriptor{Module: "sandboxInternal.runnerAllocations", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var InternalObserveRunnerJob = Operation[InternalObserveRunnerJobInput, InternalObserveRunnerJobOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#InternalObserveRunnerJob",
		OperationID:         "internal-observe-runner-job",
		Method:              "POST",
		Path:                "/internal/v1/runner/jobs/observations",
		DefaultStatus:       202,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "sandbox-rental-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:runner_job:observe", OrganizationSource: "request_id"},
		Audit:               AuditDescriptor{Event: "sandbox.runner_job.observe", Resource: "runner_job", Action: "observe"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 131072,
		SDK:                 SDKDescriptor{Module: "sandboxInternal.runnerJobs", Method: "observe", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var InternalObserveRunnerWorkflowRun = Operation[InternalObserveRunnerWorkflowRunInput, InternalObserveRunnerWorkflowRunOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#InternalObserveRunnerWorkflowRun",
		OperationID:         "internal-observe-runner-workflow-run",
		Method:              "POST",
		Path:                "/internal/v1/runner/workflow-runs/observations",
		DefaultStatus:       202,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "sandbox-rental-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:runner_job:observe", OrganizationSource: "request_id"},
		Audit:               AuditDescriptor{Event: "sandbox.runner_job.observe", Resource: "runner_job", Action: "observe_workflow_run"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 131072,
		SDK:                 SDKDescriptor{Module: "sandboxInternal.runnerWorkflowRuns", Method: "observe", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var InternalRequestGoldenSnapshotBarrier = Operation[InternalRequestGoldenSnapshotBarrierInput, InternalRequestGoldenSnapshotBarrierOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.sandbox.v1#InternalRequestGoldenSnapshotBarrier",
		OperationID:         "internal-request-golden-snapshot-barrier",
		Method:              "POST",
		Path:                "/internal/v1/golden-snapshot-barriers",
		DefaultStatus:       202,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "sandbox-rental-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "sandbox:runner_job:observe", OrganizationSource: "request_id"},
		Audit:               AuditDescriptor{Event: "sandbox.golden_snapshot_barrier.request", Resource: "runner_repository", Action: "verify"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 131072,
		SDK:                 SDKDescriptor{Module: "sandboxInternal.goldenSnapshotBarriers", Method: "request", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	InternalRegisterRunnerRepository(context.Context, *InternalRegisterRunnerRepositoryInput) (*InternalRegisterRunnerRepositoryOutput, error)
	InternalSubmitRunnerJob(context.Context, *InternalSubmitRunnerJobInput) (*InternalSubmitRunnerJobOutput, error)
	InternalGetRunnerAllocation(context.Context, *InternalGetRunnerAllocationInput) (*InternalGetRunnerAllocationOutput, error)
	InternalObserveRunnerJob(context.Context, *InternalObserveRunnerJobInput) (*InternalObserveRunnerJobOutput, error)
	InternalObserveRunnerWorkflowRun(context.Context, *InternalObserveRunnerWorkflowRunInput) (*InternalObserveRunnerWorkflowRunOutput, error)
	InternalRequestGoldenSnapshotBarrier(context.Context, *InternalRequestGoldenSnapshotBarrierInput) (*InternalRequestGoldenSnapshotBarrierOutput, error)
}

type (
	InternalRegisterRunnerRepositoryHandler     = Handler[InternalRegisterRunnerRepositoryInput, InternalRegisterRunnerRepositoryOutput]
	InternalSubmitRunnerJobHandler              = Handler[InternalSubmitRunnerJobInput, InternalSubmitRunnerJobOutput]
	InternalGetRunnerAllocationHandler          = Handler[InternalGetRunnerAllocationInput, InternalGetRunnerAllocationOutput]
	InternalObserveRunnerJobHandler             = Handler[InternalObserveRunnerJobInput, InternalObserveRunnerJobOutput]
	InternalObserveRunnerWorkflowRunHandler     = Handler[InternalObserveRunnerWorkflowRunInput, InternalObserveRunnerWorkflowRunOutput]
	InternalRequestGoldenSnapshotBarrierHandler = Handler[InternalRequestGoldenSnapshotBarrierInput, InternalRequestGoldenSnapshotBarrierOutput]
)
