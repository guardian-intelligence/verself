package source

import (
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
)

var slugPattern = regexp.MustCompile(`[^a-z0-9-]+`)

const (
	ProviderForgejo = "forgejo"

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
	RepoID         uuid.UUID
	OrgID          uint64
	CreatedBy      string
	Name           string
	Slug           string
	Description    string
	DefaultBranch  string
	Visibility     string
	Provider       string
	ProviderOwner  string
	ProviderRepo   string
	ProviderRepoID string
	State          string
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateRepositoryRequest struct {
	Name          string
	Description   string
	DefaultBranch string
}

type RepositoryList struct {
	Repositories []Repository
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

type ExternalIntegration struct {
	IntegrationID uuid.UUID
	OrgID         uint64
	CreatedBy     string
	Provider      string
	ExternalRepo  string
	CredentialRef string
	State         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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
	WorkflowRunID      uuid.UUID
	OrgID              uint64
	RepoID             uuid.UUID
	ActorID            string
	IdempotencyKey     string
	Provider           string
	WorkflowPath       string
	Ref                string
	Inputs             map[string]string
	State              string
	ProviderDispatchID string
	FailureReason      string
	TraceID            string
	DispatchedAt       *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ProviderWorkflowDispatch struct {
	ProviderDispatchID string
}

type WebhookDelivery struct {
	WebhookDeliveryID uuid.UUID
	Provider          string
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
