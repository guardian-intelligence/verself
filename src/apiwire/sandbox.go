package apiwire

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type SandboxSubmitRequest struct {
	Kind           string `json:"kind"`
	ProductID      string `json:"product_id,omitempty"`
	Provider       string `json:"provider,omitempty"`
	IdempotencyKey string `json:"idempotency_key" required:"true" maxLength:"128"`
	RepoID         string `json:"repo_id,omitempty"`
	Repo           string `json:"repo,omitempty"`
	RepoURL        string `json:"repo_url,omitempty"`
	Ref            string `json:"ref,omitempty"`
	DefaultBranch  string `json:"default_branch,omitempty"`
	RunCommand     string `json:"run_command,omitempty"`
}

type SandboxImportRepoRequest struct {
	Provider       string `json:"provider,omitempty"`
	ProviderHost   string `json:"provider_host,omitempty"`
	ProviderRepoID string `json:"provider_repo_id,omitempty"`
	Owner          string `json:"owner,omitempty"`
	Name           string `json:"name,omitempty"`
	FullName       string `json:"full_name,omitempty"`
	CloneURL       string `json:"clone_url"`
	DefaultBranch  string `json:"default_branch,omitempty"`
}

type SandboxRepoRecord struct {
	RepoID               uuid.UUID       `json:"repo_id"`
	OrgID                OrgID           `json:"org_id"`
	Provider             string          `json:"provider"`
	ProviderHost         string          `json:"provider_host"`
	ProviderRepoID       string          `json:"provider_repo_id"`
	Owner                string          `json:"owner"`
	Name                 string          `json:"name"`
	FullName             string          `json:"full_name"`
	CloneURL             string          `json:"clone_url"`
	DefaultBranch        string          `json:"default_branch"`
	State                string          `json:"state"`
	CompatibilityStatus  string          `json:"compatibility_status"`
	CompatibilitySummary json.RawMessage `json:"compatibility_summary,omitempty"`
	LastScannedSHA       string          `json:"last_scanned_sha,omitempty"`
	LastError            string          `json:"last_error,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	ArchivedAt           *time.Time      `json:"archived_at,omitempty"`
}

type SandboxCreateWebhookEndpointRequest struct {
	Provider     string `json:"provider,omitempty" enum:"forgejo" doc:"Git provider to accept webhook events from"`
	ProviderHost string `json:"provider_host" required:"true" maxLength:"255" doc:"Public git host, for example git.example.com or codeberg.org"`
	Label        string `json:"label,omitempty" maxLength:"255" doc:"Operator-visible endpoint label"`
}

type SandboxWebhookEndpointRecord struct {
	EndpointID        uuid.UUID  `json:"endpoint_id"`
	IntegrationID     uuid.UUID  `json:"integration_id"`
	OrgID             OrgID      `json:"org_id"`
	Provider          string     `json:"provider"`
	ProviderHost      string     `json:"provider_host"`
	Label             string     `json:"label"`
	Active            bool       `json:"active"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	LastDeliveryAt    *time.Time `json:"last_delivery_at,omitempty"`
	DeliveryCount     int64      `json:"delivery_count" minimum:"0" maximum:"9007199254740991"`
	SecretFingerprint string     `json:"secret_fingerprint,omitempty"`
}

type SandboxCreateWebhookEndpointResponse struct {
	EndpointID        uuid.UUID `json:"endpoint_id"`
	IntegrationID     uuid.UUID `json:"integration_id"`
	WebhookURL        string    `json:"webhook_url"`
	Secret            string    `json:"secret"`
	Provider          string    `json:"provider"`
	ProviderHost      string    `json:"provider_host"`
	Label             string    `json:"label"`
	Active            bool      `json:"active"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	DeliveryCount     int64     `json:"delivery_count" minimum:"0" maximum:"9007199254740991"`
	SecretFingerprint string    `json:"secret_fingerprint,omitempty"`
}

type SandboxRotateWebhookEndpointSecretResponse struct {
	EndpointID         uuid.UUID `json:"endpoint_id"`
	Secret             string    `json:"secret"`
	SecretFingerprint  string    `json:"secret_fingerprint"`
	RotatedAt          time.Time `json:"rotated_at"`
	PreviousRetiringAt time.Time `json:"previous_retiring_at"`
}

type SandboxSubmitExecutionResult struct {
	ExecutionID string `json:"execution_id"`
	AttemptID   string `json:"attempt_id"`
	Status      string `json:"status"`
}

type SandboxExecutionRecord struct {
	ExecutionID    uuid.UUID              `json:"execution_id"`
	OrgID          OrgID                  `json:"org_id"`
	ActorID        string                 `json:"actor_id"`
	Kind           string                 `json:"kind"`
	Provider       string                 `json:"provider,omitempty"`
	ProductID      string                 `json:"product_id"`
	Status         string                 `json:"status"`
	CorrelationID  string                 `json:"correlation_id,omitempty"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
	RepoID         string                 `json:"repo_id,omitempty"`
	Repo           string                 `json:"repo,omitempty"`
	RepoURL        string                 `json:"repo_url,omitempty"`
	Ref            string                 `json:"ref,omitempty"`
	DefaultBranch  string                 `json:"default_branch,omitempty"`
	RunCommand     string                 `json:"run_command,omitempty"`
	LatestAttempt  SandboxAttemptRecord   `json:"latest_attempt"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	BillingWindows []SandboxBillingWindow `json:"billing_windows,omitempty"`
}

type SandboxExecutionLogs struct {
	ExecutionID string `json:"execution_id"`
	AttemptID   string `json:"attempt_id"`
	Logs        string `json:"logs"`
}

type SandboxBillingCheckoutRequest struct {
	ProductID   string `json:"product_id" required:"true" maxLength:"255" doc:"Product to purchase credits for"`
	AmountCents int64  `json:"amount_cents" required:"true" minimum:"1" maximum:"9007199254740991" doc:"Amount in cents"`
	SuccessURL  string `json:"success_url" required:"true" maxLength:"2048"`
	CancelURL   string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type SandboxBillingSubscriptionRequest struct {
	PlanID     string `json:"plan_id" required:"true" maxLength:"255" doc:"Plan to subscribe to"`
	Cadence    string `json:"cadence,omitempty" enum:"monthly,annual" doc:"Billing cadence (default monthly)"`
	SuccessURL string `json:"success_url" required:"true" maxLength:"2048"`
	CancelURL  string `json:"cancel_url" required:"true" maxLength:"2048"`
}

type SandboxBillingPortalRequest struct {
	ReturnURL string `json:"return_url" required:"true" maxLength:"2048"`
}

type SandboxAttemptRecord struct {
	AttemptID         uuid.UUID  `json:"attempt_id"`
	AttemptSeq        int        `json:"attempt_seq" minimum:"0" maximum:"9007199254740991"`
	State             string     `json:"state"`
	OrchestratorJobID string     `json:"orchestrator_job_id,omitempty"`
	BillingJobID      int64      `json:"billing_job_id,omitempty" minimum:"0" maximum:"9007199254740991"`
	FailureReason     string     `json:"failure_reason,omitempty"`
	ExitCode          int        `json:"exit_code,omitempty" minimum:"0" maximum:"255"`
	DurationMs        int64      `json:"duration_ms,omitempty" minimum:"0" maximum:"9007199254740991"`
	ZFSWritten        int64      `json:"zfs_written,omitempty" minimum:"0" maximum:"9007199254740991"`
	StdoutBytes       int64      `json:"stdout_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	StderrBytes       int64      `json:"stderr_bytes,omitempty" minimum:"0" maximum:"9007199254740991"`
	TraceID           string     `json:"trace_id,omitempty"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
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
