package deployment

import "time"

const (
	ServiceName = "deployment-service"

	StateQueued    = "queued"
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"

	SourceGitHubActionsOIDC = "github_actions_oidc"
)

type SubmitRequest struct {
	Site string `json:"site"`
	SHA  string `json:"sha"`
}

type BootstrapCheckResponse struct {
	Ready       bool              `json:"ready"`
	FailedCount uint16            `json:"failed_count"`
	Checks      []DependencyCheck `json:"checks"`
	Traceparent string            `json:"traceparent"`
}

type Source struct {
	Kind           string
	Subject        string
	Repository     string
	Ref            string
	SHA            string
	RunID          string
	RunAttempt     uint32
	Workflow       string
	JobWorkflowRef string
}

type Record struct {
	DeploymentID             string    `json:"deployment_id"`
	Site                     string    `json:"site"`
	SHA                      string    `json:"sha"`
	DeployRunKey             string    `json:"deploy_run_key"`
	SourceKind               string    `json:"source_kind"`
	SourceSubject            string    `json:"source_subject"`
	Repository               string    `json:"repository"`
	Ref                      string    `json:"ref"`
	Workflow                 string    `json:"workflow"`
	JobWorkflowRef           string    `json:"job_workflow_ref"`
	ProviderRunID            string    `json:"provider_run_id"`
	ProviderRunAttempt       uint32    `json:"provider_run_attempt"`
	State                    string    `json:"state"`
	ErrorCode                string    `json:"error_code"`
	ErrorDetail              string    `json:"error_detail"`
	NomadSubmittedJobs       uint32    `json:"nomad_submitted_jobs"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
	StartedAt                time.Time `json:"started_at"`
	FinishedAt               time.Time `json:"finished_at"`
}

type Event struct {
	EventID      int64     `json:"event_id"`
	DeploymentID string    `json:"deployment_id"`
	At           time.Time `json:"at"`
	State        string    `json:"state"`
	Code         string    `json:"code"`
	Detail       string    `json:"detail"`
}

type DependencyCheck struct {
	Stage  string `json:"stage"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}
