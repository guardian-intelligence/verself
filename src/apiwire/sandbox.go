package apiwire

import (
	"time"

	"github.com/google/uuid"
)

type SandboxSubmitRequest struct {
	Kind           string `json:"kind"`
	RunnerClass    string `json:"runner_class,omitempty" doc:"Runner class label, for example metal-4vcpu-ubuntu-2404."`
	ProductID      string `json:"product_id,omitempty"`
	Provider       string `json:"provider,omitempty"`
	IdempotencyKey string `json:"idempotency_key" required:"true" maxLength:"128"`
	RunCommand     string `json:"run_command,omitempty"`
	MaxWallSeconds uint64 `json:"max_wall_seconds,omitempty" minimum:"1" maximum:"9007199254740991"`
}

type SandboxGitHubInstallationConnectResponse struct {
	State     string    `json:"state" doc:"Opaque installation state token embedded in the GitHub App setup URL."`
	SetupURL  string    `json:"setup_url" doc:"GitHub App installation URL for the current Forge Metal organization."`
	ExpiresAt time.Time `json:"expires_at" doc:"Time after which the setup URL state is no longer accepted."`
}

type SandboxGitHubInstallationRecord struct {
	InstallationID string    `json:"installation_id" doc:"GitHub App installation ID encoded as a string for JavaScript-safe transport."`
	OrgID          OrgID     `json:"org_id"`
	AccountLogin   string    `json:"account_login"`
	AccountType    string    `json:"account_type"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SandboxSubmitExecutionResult struct {
	ExecutionID string `json:"execution_id"`
	AttemptID   string `json:"attempt_id"`
	Status      string `json:"status"`
}

type SandboxExecutionRecord struct {
	ExecutionID      uuid.UUID              `json:"execution_id"`
	OrgID            OrgID                  `json:"org_id"`
	ActorID          string                 `json:"actor_id"`
	Kind             string                 `json:"kind"`
	SourceKind       string                 `json:"source_kind,omitempty"`
	WorkloadKind     string                 `json:"workload_kind,omitempty"`
	SourceRef        string                 `json:"source_ref,omitempty"`
	RunnerClass      string                 `json:"runner_class,omitempty"`
	ExternalProvider string                 `json:"external_provider,omitempty"`
	ExternalTaskID   string                 `json:"external_task_id,omitempty"`
	Provider         string                 `json:"provider,omitempty"`
	ProductID        string                 `json:"product_id"`
	Status           string                 `json:"status"`
	CorrelationID    string                 `json:"correlation_id,omitempty"`
	IdempotencyKey   string                 `json:"idempotency_key,omitempty"`
	RunCommand       string                 `json:"run_command,omitempty"`
	LatestAttempt    SandboxAttemptRecord   `json:"latest_attempt"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	BillingWindows   []SandboxBillingWindow `json:"billing_windows,omitempty"`
}

type SandboxExecutionLogs struct {
	ExecutionID string `json:"execution_id"`
	AttemptID   string `json:"attempt_id"`
	Logs        string `json:"logs"`
}

type SandboxBillingCheckoutRequest struct {
	ProductID   string `json:"product_id" required:"true" maxLength:"255" doc:"Product context for checkout display and metadata; purchased top-ups are account-scoped"`
	AmountCents int64  `json:"amount_cents" required:"true" minimum:"1" maximum:"9007199254740991" doc:"Amount in cents"`
	SuccessURL  string `json:"success_url" required:"true" maxLength:"2048"`
	CancelURL   string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type SandboxBillingContractRequest struct {
	PlanID     string `json:"plan_id" required:"true" maxLength:"255" doc:"Plan to activate"`
	Cadence    string `json:"cadence,omitempty" enum:"monthly" doc:"Billing cadence (default monthly)"`
	SuccessURL string `json:"success_url" required:"true" maxLength:"2048"`
	CancelURL  string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type SandboxBillingContractChangeRequest struct {
	TargetPlanID string `json:"target_plan_id" required:"true" maxLength:"255" doc:"Plan to upgrade into immediately"`
	SuccessURL   string `json:"success_url" required:"true" maxLength:"2048"`
	CancelURL    string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type SandboxBillingPortalRequest struct {
	ReturnURL string `json:"return_url" required:"true" maxLength:"2048"`
}

type SandboxAttemptRecord struct {
	AttemptID     uuid.UUID  `json:"attempt_id"`
	AttemptSeq    int        `json:"attempt_seq" minimum:"0" maximum:"9007199254740991"`
	State         string     `json:"state"`
	LeaseID       string     `json:"lease_id,omitempty"`
	ExecID        string     `json:"exec_id,omitempty"`
	BillingJobID  int64      `json:"billing_job_id,omitempty" minimum:"0" maximum:"9007199254740991"`
	FailureReason string     `json:"failure_reason,omitempty"`
	ExitCode      *int       `json:"exit_code,omitempty" minimum:"0" maximum:"255"`
	DurationMs    int64      `json:"duration_ms,omitempty" minimum:"0" maximum:"9007199254740991"`
	ZFSWritten    int64      `json:"zfs_written,omitempty" minimum:"0" maximum:"9007199254740991"`
	StdoutBytes   int64      `json:"stdout_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	StderrBytes   int64      `json:"stderr_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	TraceID       string     `json:"trace_id,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type SandboxBillingWindow struct {
	AttemptID        uuid.UUID  `json:"attempt_id"`
	BillingWindowID  string     `json:"billing_window_id"`
	WindowSeq        int        `json:"window_seq" minimum:"0" maximum:"9007199254740991"`
	ReservationShape string     `json:"reservation_shape"`
	ReservedQuantity int        `json:"reserved_quantity" minimum:"0" maximum:"9007199254740991"`
	ActualQuantity   int        `json:"actual_quantity,omitempty" minimum:"0" maximum:"9007199254740991"`
	PricingPhase     string     `json:"pricing_phase,omitempty"`
	State            string     `json:"state"`
	WindowStart      time.Time  `json:"window_start"`
	CreatedAt        time.Time  `json:"created_at"`
	SettledAt        *time.Time `json:"settled_at,omitempty"`
}
