package source

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalid          = errors.New("source: invalid request")
	ErrNotFound         = errors.New("source: not found")
	ErrConflict         = errors.New("source: conflict")
	ErrUnauthorized     = errors.New("source: unauthorized")
	ErrStoreUnavailable = errors.New("source: store unavailable")
	ErrForgejo          = errors.New("source: forgejo unavailable")
	ErrSandbox          = errors.New("source: sandbox unavailable")
)

var slugPattern = regexp.MustCompile(`[^a-z0-9-]+`)

const (
	BackendForgejo = "forgejo"

	GitCredentialUsername = "fm"

	DefaultSourceCIRunCommand = "printf 'forge-metal source ci\\n'"

	WorkflowRunStateDispatching = "dispatching"
	WorkflowRunStateDispatched  = "dispatched"
	WorkflowRunStateFailed      = "failed"

	CIRunStateQueued    = "queued"
	CIRunStateRunning   = "running"
	CIRunStateSucceeded = "succeeded"
	CIRunStateFailed    = "failed"
	CIRunStateCanceled  = "canceled"
	CIRunStateSkipped   = "skipped"
)

type Principal struct {
	Subject string
	OrgID   uint64
	Email   string
}

type Repository struct {
	RepoID        uuid.UUID
	OrgID         uint64
	OrgPath       string
	CreatedBy     string
	Name          string
	Slug          string
	Description   string
	DefaultBranch string
	Visibility    string
	State         string
	Version       int64
	LastPushedAt  *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Backend       RepositoryBackend
}

type RepositoryBackend struct {
	BackendID     uuid.UUID
	RepoID        uuid.UUID
	Backend       string
	BackendOwner  string
	BackendRepo   string
	BackendRepoID string
	State         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateRepositoryRequest struct {
	Name          string
	Description   string
	DefaultBranch string
}

type RepositoryList struct {
	Repositories []Repository
}

type GitCredential struct {
	CredentialID uuid.UUID
	OrgID        uint64
	OrgPath      string
	ActorID      string
	Label        string
	Username     string
	Token        string
	TokenPrefix  string
	Scopes       []string
	State        string
	ExpiresAt    time.Time
	LastUsedAt   *time.Time
	CreatedAt    time.Time
}

type CreateGitCredentialRequest struct {
	Label            string
	ExpiresInSeconds int64
}

type GitPrincipal struct {
	CredentialID uuid.UUID
	OrgID        uint64
	OrgPath      string
	ActorID      string
	Username     string
	Scopes       []string
}

type GitRepositoryPath struct {
	OrgPath     string
	Slug        string
	Endpoint    string
	Service     string
	ReceivePack bool
	UploadPack  bool
}

type Ref struct {
	Name   string
	Commit string
}

type TreeEntry struct {
	Path string
	Type string
	Size int64
	SHA  string
}

type Blob struct {
	Path        string
	Name        string
	Encoding    string
	Content     string
	Size        int64
	SHA         string
	DownloadURL string
}

type CIRun struct {
	CIRunID            uuid.UUID
	OrgID              uint64
	RepoID             uuid.UUID
	ActorID            string
	RefName            string
	CommitSHA          string
	TriggerEvent       string
	State              string
	SandboxExecutionID uuid.UUID
	SandboxAttemptID   uuid.UUID
	FailureReason      string
	TraceID            string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
}

type SandboxCISubmitter interface {
	SubmitSourceCIRun(ctx context.Context, repo Repository, run CIRun) (SandboxCISubmission, error)
}

type SandboxCISubmission struct {
	ExecutionID uuid.UUID
	AttemptID   uuid.UUID
}

type CheckoutGrant struct {
	GrantID    uuid.UUID
	RepoID     uuid.UUID
	OrgID      uint64
	ActorID    string
	Ref        string
	PathPrefix string
	Token      string
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

type WorkflowDispatchRequest struct {
	RepoID         uuid.UUID
	WorkflowPath   string
	Ref            string
	Inputs         map[string]string
	IdempotencyKey string
}

type InternalWorkflowDispatchRequest struct {
	OrgID          uint64
	ActorID        string
	RepoID         uuid.UUID
	WorkflowPath   string
	Ref            string
	Inputs         map[string]string
	IdempotencyKey string
}

type WorkflowRun struct {
	WorkflowRunID     uuid.UUID
	OrgID             uint64
	RepoID            uuid.UUID
	ActorID           string
	IdempotencyKey    string
	Backend           string
	WorkflowPath      string
	Ref               string
	Inputs            map[string]string
	State             string
	BackendDispatchID string
	FailureReason     string
	TraceID           string
	DispatchedAt      *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type BackendWorkflowDispatch struct {
	BackendDispatchID string
}

type WebhookDelivery struct {
	WebhookDeliveryID uuid.UUID
	Backend           string
	DeliveryID        string
	EventType         string
	SignatureValid    bool
	Result            string
	ResolvedOrgID     uint64
	ResolvedRepoID    uuid.UUID
	TraceID           string
	Details           map[string]any
	CreatedAt         time.Time
}

func NormalizeSlug(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = slugPattern.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	return slug
}

func OrgPath(orgID uint64) string {
	if orgID == 0 {
		return ""
	}
	return "org-" + strconv.FormatUint(orgID, 10)
}

func ValidatePrincipal(principal Principal) error {
	if strings.TrimSpace(principal.Subject) == "" || principal.OrgID == 0 {
		return ErrInvalid
	}
	return nil
}

func NormalizeCreate(input CreateRepositoryRequest) (CreateRepositoryRequest, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.DefaultBranch = strings.TrimSpace(input.DefaultBranch)
	if input.DefaultBranch == "" {
		input.DefaultBranch = "main"
	}
	if NormalizeSlug(input.Name) == "" {
		return CreateRepositoryRequest{}, ErrInvalid
	}
	if len(input.Name) > 128 || len(input.Description) > 1024 || len(input.DefaultBranch) > 128 {
		return CreateRepositoryRequest{}, ErrInvalid
	}
	return input, nil
}

func NormalizeCreateGitCredential(input CreateGitCredentialRequest) (CreateGitCredentialRequest, error) {
	input.Label = strings.TrimSpace(input.Label)
	if input.Label == "" {
		input.Label = "git push"
	}
	if input.ExpiresInSeconds == 0 {
		input.ExpiresInSeconds = int64((30 * 24 * time.Hour).Seconds())
	}
	if input.ExpiresInSeconds < 60 || input.ExpiresInSeconds > int64((90*24*time.Hour).Seconds()) {
		return CreateGitCredentialRequest{}, ErrInvalid
	}
	if len(input.Label) > 128 {
		return CreateGitCredentialRequest{}, ErrInvalid
	}
	return input, nil
}

func NormalizeWorkflowDispatch(input WorkflowDispatchRequest, defaultRef string) (WorkflowDispatchRequest, error) {
	input.WorkflowPath = normalizeWorkflowPath(input.WorkflowPath)
	input.Ref = strings.TrimSpace(input.Ref)
	if input.Ref == "" {
		input.Ref = strings.TrimSpace(defaultRef)
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.Inputs == nil {
		input.Inputs = map[string]string{}
	}
	cleanInputs := make(map[string]string, len(input.Inputs))
	for rawKey, value := range input.Inputs {
		key := strings.TrimSpace(rawKey)
		if key == "" || len(key) > 128 || len(value) > 4096 {
			return WorkflowDispatchRequest{}, ErrInvalid
		}
		if rawKey != key {
			return WorkflowDispatchRequest{}, ErrInvalid
		}
		cleanInputs[key] = value
	}
	input.Inputs = cleanInputs
	if input.RepoID == uuid.Nil || input.WorkflowPath == "" || input.Ref == "" || input.IdempotencyKey == "" {
		return WorkflowDispatchRequest{}, ErrInvalid
	}
	if len(input.WorkflowPath) > 512 || len(input.Ref) > 255 || len(input.IdempotencyKey) > 128 {
		return WorkflowDispatchRequest{}, ErrInvalid
	}
	return input, nil
}

func normalizeWorkflowPath(value string) string {
	path := strings.Trim(strings.TrimSpace(value), "/")
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if path == "" || strings.Contains(path, "..") {
		return ""
	}
	if !strings.HasPrefix(path, ".forgejo/workflows/") && !strings.HasPrefix(path, ".github/workflows/") {
		return ""
	}
	if !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml") {
		return ""
	}
	return path
}

func workflowInputsJSON(inputs map[string]string) ([]byte, error) {
	if inputs == nil {
		inputs = map[string]string{}
	}
	return json.Marshal(inputs)
}
