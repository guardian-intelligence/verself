package source

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
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
	ErrRunner           = errors.New("source: runner registration unavailable")
)

var slugPattern = regexp.MustCompile(`[^a-z0-9-]+`)

const (
	BackendForgejo = "forgejo"

	GitCredentialUsername = "verself"
	GitCredentialKind     = "source_git_https"

	WorkflowRunStateDispatching = "dispatching"
	WorkflowRunStateDispatched  = "dispatched"
	WorkflowRunStateFailed      = "failed"
)

type Principal struct {
	Subject string
	OrgID   uint64
	Email   string
}

type Repository struct {
	RepoID        uuid.UUID
	OrgID         uint64
	ProjectID     uuid.UUID
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
	ProjectID     uuid.UUID
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
	ActorID      string
	Username     string
	Scopes       []string
}

type GitCredentialIssuer interface {
	CreateSourceGitCredential(ctx context.Context, principal Principal, input CreateGitCredentialRequest) (GitCredential, error)
	VerifySourceGitCredential(ctx context.Context, orgID uint64, actorID string, token string, requiredScopes []string) (GitCredential, bool, error)
}

type RunnerRepositoryRegistrar interface {
	RegisterRunnerRepository(ctx context.Context, repo Repository) error
}

type OrganizationReference struct {
	OrgID          uint64
	Slug           string
	DisplayName    string
	RedirectedFrom string
}

type OrganizationResolver interface {
	ResolveSourceOrganization(ctx context.Context, slug string) (OrganizationReference, error)
	ResolveSourceOrganizationID(ctx context.Context, orgID uint64) (OrganizationReference, error)
}

type ProjectReference struct {
	ProjectID          uuid.UUID
	OrgID              uint64
	Slug               string
	DisplayName        string
	RedirectedFromSlug string
}

type ProjectResolver interface {
	ResolveSourceProject(ctx context.Context, orgID uint64, projectID uuid.UUID) (ProjectReference, error)
	ResolveSourceProjectSlug(ctx context.Context, orgID uint64, slug string) (ProjectReference, error)
}

type GitRepositoryPath struct {
	OrgSlug     string
	ProjectSlug string
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
	ProjectID      uuid.UUID
	WorkflowPath   string
	Ref            string
	Inputs         map[string]string
	IdempotencyKey string
}

type InternalWorkflowDispatchRequest struct {
	OrgID          uint64
	ActorID        string
	RepoID         uuid.UUID
	ProjectID      uuid.UUID
	WorkflowPath   string
	Ref            string
	Inputs         map[string]string
	IdempotencyKey string
}

type WorkflowRun struct {
	WorkflowRunID     uuid.UUID
	OrgID             uint64
	ProjectID         uuid.UUID
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
	ResolvedProjectID uuid.UUID
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
	if input.ProjectID == uuid.Nil {
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
	if input.RepoID == uuid.Nil || input.ProjectID == uuid.Nil || input.WorkflowPath == "" || input.Ref == "" || input.IdempotencyKey == "" {
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
