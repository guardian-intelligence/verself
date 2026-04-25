package source

import (
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

type Principal struct {
	Subject string
	OrgID   uint64
	Email   string
}

type Repository struct {
	RepoID        uuid.UUID
	OrgID         uint64
	CreatedBy     string
	Name          string
	Slug          string
	Description   string
	DefaultBranch string
	Visibility    string
	ForgejoOwner  string
	ForgejoRepo   string
	ForgejoRepoID int64
	State         string
	Version       int64
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
