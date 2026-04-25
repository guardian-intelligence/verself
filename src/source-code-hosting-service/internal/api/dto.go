package api

import (
	"time"

	"github.com/google/uuid"

	"github.com/forge-metal/source-code-hosting-service/internal/source"
)

type Repository struct {
	RepoID        uuid.UUID `json:"repo_id"`
	OrgID         string    `json:"org_id"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	Provider      string    `json:"provider"`
	Visibility    string    `json:"visibility"`
	State         string    `json:"state"`
	Version       int32     `json:"version" minimum:"0" maximum:"2147483647"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type RepositoryList struct {
	Repositories []Repository `json:"repositories"`
}

type CreateRepositoryRequest struct {
	Name          string `json:"name" required:"true" minLength:"1" maxLength:"128"`
	Description   string `json:"description,omitempty" maxLength:"1024"`
	DefaultBranch string `json:"default_branch,omitempty" maxLength:"128"`
}

type Ref struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

type RefList struct {
	Refs []Ref `json:"refs"`
}

type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size" minimum:"0" maximum:"9007199254740991"`
	SHA  string `json:"sha"`
}

type Tree struct {
	Entries []TreeEntry `json:"entries"`
}

type Blob struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Encoding    string `json:"encoding"`
	Content     string `json:"content"`
	Size        int64  `json:"size" minimum:"0" maximum:"9007199254740991"`
	SHA         string `json:"sha"`
	DownloadURL string `json:"download_url,omitempty"`
}

type CreateCheckoutGrantRequest struct {
	Ref        string `json:"ref,omitempty" maxLength:"255"`
	PathPrefix string `json:"path_prefix,omitempty" maxLength:"1024"`
}

type CheckoutGrant struct {
	GrantID   uuid.UUID `json:"grant_id"`
	RepoID    uuid.UUID `json:"repo_id"`
	Ref       string    `json:"ref"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type CreateWorkflowRunRequest struct {
	WorkflowPath string            `json:"workflow_path" required:"true" minLength:"1" maxLength:"512"`
	Ref          string            `json:"ref,omitempty" maxLength:"255"`
	Inputs       map[string]string `json:"inputs,omitempty"`
}

type InternalCreateWorkflowRunRequest struct {
	OrgID          string            `json:"org_id" required:"true"`
	ActorID        string            `json:"actor_id" required:"true" minLength:"1" maxLength:"255"`
	RepoID         uuid.UUID         `json:"repo_id" required:"true"`
	WorkflowPath   string            `json:"workflow_path" required:"true" minLength:"1" maxLength:"512"`
	Ref            string            `json:"ref,omitempty" maxLength:"255"`
	Inputs         map[string]string `json:"inputs,omitempty"`
	IdempotencyKey string            `json:"idempotency_key" required:"true" minLength:"1" maxLength:"128"`
}

type WorkflowRun struct {
	WorkflowRunID      uuid.UUID         `json:"workflow_run_id"`
	OrgID              string            `json:"org_id"`
	RepoID             uuid.UUID         `json:"repo_id"`
	ActorID            string            `json:"actor_id"`
	Provider           string            `json:"provider"`
	WorkflowPath       string            `json:"workflow_path"`
	Ref                string            `json:"ref"`
	Inputs             map[string]string `json:"inputs"`
	State              string            `json:"state"`
	ProviderDispatchID string            `json:"provider_dispatch_id,omitempty"`
	FailureReason      string            `json:"failure_reason,omitempty"`
	TraceID            string            `json:"trace_id,omitempty"`
	DispatchedAt       *time.Time        `json:"dispatched_at,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

type WorkflowRunList struct {
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

type CreateIntegrationRequest struct {
	Provider      string `json:"provider" required:"true" minLength:"1" maxLength:"64"`
	ExternalRepo  string `json:"external_repo" required:"true" minLength:"1" maxLength:"512"`
	CredentialRef string `json:"credential_ref,omitempty" maxLength:"512"`
}

type ExternalIntegration struct {
	IntegrationID uuid.UUID `json:"integration_id"`
	OrgID         string    `json:"org_id"`
	Provider      string    `json:"provider"`
	ExternalRepo  string    `json:"external_repo"`
	CredentialRef string    `json:"credential_ref,omitempty"`
	State         string    `json:"state"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func repositoryDTO(repo source.Repository) Repository {
	return Repository{
		RepoID:        repo.RepoID,
		OrgID:         uintString(repo.OrgID),
		Name:          repo.Name,
		Slug:          repo.Slug,
		Description:   repo.Description,
		DefaultBranch: repo.DefaultBranch,
		Provider:      repo.Provider,
		Visibility:    repo.Visibility,
		State:         repo.State,
		Version:       int32(repo.Version),
		CreatedAt:     repo.CreatedAt,
		UpdatedAt:     repo.UpdatedAt,
	}
}

func repositoryDTOs(repos []source.Repository) []Repository {
	out := make([]Repository, 0, len(repos))
	for _, repo := range repos {
		out = append(out, repositoryDTO(repo))
	}
	return out
}

func refDTOs(refs []source.Ref) []Ref {
	out := make([]Ref, 0, len(refs))
	for _, ref := range refs {
		out = append(out, Ref{Name: ref.Name, Commit: ref.Commit})
	}
	return out
}

func treeEntryDTOs(entries []source.TreeEntry) []TreeEntry {
	out := make([]TreeEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, TreeEntry{Path: entry.Path, Type: entry.Type, Size: entry.Size, SHA: entry.SHA})
	}
	return out
}

func blobDTO(blob source.Blob) Blob {
	return Blob{
		Path:        blob.Path,
		Name:        blob.Name,
		Encoding:    blob.Encoding,
		Content:     blob.Content,
		Size:        blob.Size,
		SHA:         blob.SHA,
		DownloadURL: blob.DownloadURL,
	}
}

func checkoutGrantDTO(grant source.CheckoutGrant) CheckoutGrant {
	return CheckoutGrant{
		GrantID:   grant.GrantID,
		RepoID:    grant.RepoID,
		Ref:       grant.Ref,
		Token:     grant.Token,
		ExpiresAt: grant.ExpiresAt,
	}
}

func workflowRunDTO(run source.WorkflowRun) WorkflowRun {
	inputs := run.Inputs
	if inputs == nil {
		inputs = map[string]string{}
	}
	return WorkflowRun{
		WorkflowRunID:      run.WorkflowRunID,
		OrgID:              uintString(run.OrgID),
		RepoID:             run.RepoID,
		ActorID:            run.ActorID,
		Provider:           run.Provider,
		WorkflowPath:       run.WorkflowPath,
		Ref:                run.Ref,
		Inputs:             inputs,
		State:              run.State,
		ProviderDispatchID: run.ProviderDispatchID,
		FailureReason:      run.FailureReason,
		TraceID:            run.TraceID,
		DispatchedAt:       run.DispatchedAt,
		CreatedAt:          run.CreatedAt,
		UpdatedAt:          run.UpdatedAt,
	}
}

func workflowRunDTOs(runs []source.WorkflowRun) []WorkflowRun {
	out := make([]WorkflowRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, workflowRunDTO(run))
	}
	return out
}

func integrationDTO(integration source.ExternalIntegration) ExternalIntegration {
	return ExternalIntegration{
		IntegrationID: integration.IntegrationID,
		OrgID:         uintString(integration.OrgID),
		Provider:      integration.Provider,
		ExternalRepo:  integration.ExternalRepo,
		CredentialRef: integration.CredentialRef,
		State:         integration.State,
		CreatedAt:     integration.CreatedAt,
		UpdatedAt:     integration.UpdatedAt,
	}
}

func uintString(value uint64) string {
	return fmtUint(value)
}
