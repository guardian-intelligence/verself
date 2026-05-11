package api

import (
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	dto "github.com/verself/domain-transfer-objects"
	"github.com/verself/source-code-hosting-service/internal/source"
)

type Repository struct {
	RepoID              uuid.UUID        `json:"repo_id"`
	ResourceName        dto.ResourceName `json:"resourceName" doc:"Globally unique Verself resource name for this repository."`
	OrgID               string           `json:"org_id"`
	ProjectID           uuid.UUID        `json:"project_id"`
	ProjectResourceName dto.ResourceName `json:"projectResourceName" doc:"Globally unique Verself resource name for the parent project."`
	OrgSlug             string           `json:"org_slug"`
	ProjectSlug         string           `json:"project_slug"`
	GitHTTPURL          string           `json:"git_http_url"`
	Name                string           `json:"name"`
	Description         string           `json:"description"`
	DefaultBranch       string           `json:"default_branch"`
	Backend             string           `json:"backend"`
	Visibility          string           `json:"visibility"`
	State               string           `json:"state"`
	Version             int32            `json:"version" minimum:"0" maximum:"2147483647"`
	LastPushedAt        *time.Time       `json:"last_pushed_at,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

type RepositoryList struct {
	Repositories []Repository `json:"repositories"`
}

type CreateRepositoryRequest struct {
	ProjectID     uuid.UUID `json:"project_id" required:"true"`
	Description   string    `json:"description,omitempty" maxLength:"1024"`
	DefaultBranch string    `json:"default_branch,omitempty" maxLength:"128"`
}

type CreateGitCredentialRequest struct {
	Label            string   `json:"label,omitempty" maxLength:"128"`
	ExpiresInSeconds int64    `json:"expires_in_seconds,omitempty" minimum:"60" maximum:"7776000"`
	Scopes           []string `json:"scopes" required:"true" minItems:"1" maxItems:"2"`
}

type GitCredential struct {
	CredentialID uuid.UUID        `json:"credential_id"`
	ResourceName dto.ResourceName `json:"resourceName" doc:"Globally unique Verself resource name for this Git credential."`
	OrgID        string           `json:"org_id"`
	Username     string           `json:"username"`
	Token        string           `json:"token"`
	TokenPrefix  string           `json:"token_prefix"`
	Scopes       []string         `json:"scopes"`
	ExpiresAt    time.Time        `json:"expires_at"`
	CreatedAt    time.Time        `json:"created_at"`
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
	Ref string `json:"ref,omitempty" maxLength:"255"`
}

type CheckoutGrant struct {
	GrantID      uuid.UUID        `json:"grant_id"`
	ResourceName dto.ResourceName `json:"resourceName" doc:"Globally unique Verself resource name for this checkout grant."`
	RepoID       uuid.UUID        `json:"repo_id"`
	Ref          string           `json:"ref"`
	Token        string           `json:"token"`
	ExpiresAt    time.Time        `json:"expires_at"`
}

type CreateWorkflowRunRequest struct {
	ProjectID    uuid.UUID         `json:"project_id" required:"true"`
	WorkflowPath string            `json:"workflow_path" required:"true" minLength:"1" maxLength:"512"`
	Ref          string            `json:"ref,omitempty" maxLength:"255"`
	Inputs       map[string]string `json:"inputs,omitempty"`
}

type InternalCreateWorkflowRunRequest struct {
	OrgID          string            `json:"org_id" required:"true"`
	ActorID        string            `json:"actor_id" required:"true" minLength:"1" maxLength:"255"`
	RepoID         uuid.UUID         `json:"repo_id" required:"true"`
	ProjectID      uuid.UUID         `json:"project_id" required:"true"`
	WorkflowPath   string            `json:"workflow_path" required:"true" minLength:"1" maxLength:"512"`
	Ref            string            `json:"ref,omitempty" maxLength:"255"`
	Inputs         map[string]string `json:"inputs,omitempty"`
	IdempotencyKey string            `json:"idempotency_key" required:"true" minLength:"1" maxLength:"128"`
}

type WorkflowRun struct {
	WorkflowRunID          uuid.UUID         `json:"workflow_run_id"`
	ResourceName           dto.ResourceName  `json:"resourceName" doc:"Globally unique Verself resource name for this workflow run."`
	OrgID                  string            `json:"org_id"`
	ProjectID              uuid.UUID         `json:"project_id"`
	ProjectResourceName    dto.ResourceName  `json:"projectResourceName" doc:"Globally unique Verself resource name for the parent project."`
	RepoID                 uuid.UUID         `json:"repo_id"`
	RepositoryResourceName dto.ResourceName  `json:"repositoryResourceName" doc:"Globally unique Verself resource name for the repository."`
	ActorID                string            `json:"actor_id"`
	Backend                string            `json:"backend"`
	WorkflowPath           string            `json:"workflow_path"`
	Ref                    string            `json:"ref"`
	Inputs                 map[string]string `json:"inputs"`
	State                  string            `json:"state"`
	BackendDispatchID      string            `json:"backend_dispatch_id,omitempty"`
	FailureReason          string            `json:"failure_reason,omitempty"`
	TraceID                string            `json:"trace_id,omitempty"`
	DispatchedAt           *time.Time        `json:"dispatched_at,omitempty"`
	CreatedAt              time.Time         `json:"created_at"`
	UpdatedAt              time.Time         `json:"updated_at"`
}

type WorkflowRunList struct {
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

func repositoryDTO(repo source.Repository, org source.OrganizationReference, project source.ProjectReference, publicBaseURL string, installationID string) Repository {
	orgID := uintString(repo.OrgID)
	projectID := project.ProjectID.String()
	return Repository{
		RepoID:              repo.RepoID,
		ResourceName:        dto.ResourceNameRepository(installationID, orgID, projectID, repo.RepoID.String()),
		OrgID:               orgID,
		ProjectID:           repo.ProjectID,
		ProjectResourceName: dto.ResourceNameProject(installationID, orgID, projectID),
		OrgSlug:             org.Slug,
		ProjectSlug:         project.Slug,
		GitHTTPURL:          gitHTTPURL(publicBaseURL, org.Slug, project.Slug),
		Name:                repo.Name,
		Description:         repo.Description,
		DefaultBranch:       repo.DefaultBranch,
		Backend:             repo.Backend.Backend,
		Visibility:          repo.Visibility,
		State:               repo.State,
		Version:             int32FromInt64(repo.Version, "repository version"),
		LastPushedAt:        repo.LastPushedAt,
		CreatedAt:           repo.CreatedAt,
		UpdatedAt:           repo.UpdatedAt,
	}
}

func repositoryDTOs(repos []source.Repository, org source.OrganizationReference, projects map[uuid.UUID]source.ProjectReference, publicBaseURL string, installationID string) []Repository {
	out := make([]Repository, 0, len(repos))
	for _, repo := range repos {
		out = append(out, repositoryDTO(repo, org, projects[repo.ProjectID], publicBaseURL, installationID))
	}
	return out
}

func gitCredentialDTO(credential source.GitCredential, installationID string) GitCredential {
	orgID := uintString(credential.OrgID)
	return GitCredential{
		CredentialID: credential.CredentialID,
		ResourceName: dto.ResourceNameCredential(installationID, orgID, credential.CredentialID.String()),
		OrgID:        orgID,
		Username:     credential.Username,
		Token:        credential.Token,
		TokenPrefix:  credential.TokenPrefix,
		Scopes:       credential.Scopes,
		ExpiresAt:    credential.ExpiresAt,
		CreatedAt:    credential.CreatedAt,
	}
}

func gitHTTPURL(publicBaseURL, orgSlug, projectSlug string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if parsed, err := url.Parse(base); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.Path = path.Join(parsed.Path, orgSlug, projectSlug+".git")
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	return base + "/" + url.PathEscape(orgSlug) + "/" + url.PathEscape(projectSlug) + ".git"
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

func checkoutGrantDTO(grant source.CheckoutGrant, installationID string) CheckoutGrant {
	return CheckoutGrant{
		GrantID:      grant.GrantID,
		ResourceName: dto.ResourceNameCheckoutGrant(installationID, uintString(grant.OrgID), grant.GrantID.String()),
		RepoID:       grant.RepoID,
		Ref:          grant.Ref,
		Token:        grant.Token,
		ExpiresAt:    grant.ExpiresAt,
	}
}

func workflowRunDTO(run source.WorkflowRun, installationID string) WorkflowRun {
	inputs := run.Inputs
	if inputs == nil {
		inputs = map[string]string{}
	}
	orgID := uintString(run.OrgID)
	projectID := run.ProjectID.String()
	repoID := run.RepoID.String()
	return WorkflowRun{
		WorkflowRunID:          run.WorkflowRunID,
		ResourceName:           dto.ResourceNameSourceWorkflowRun(installationID, orgID, projectID, repoID, run.WorkflowRunID.String()),
		OrgID:                  orgID,
		ProjectID:              run.ProjectID,
		ProjectResourceName:    dto.ResourceNameProject(installationID, orgID, projectID),
		RepoID:                 run.RepoID,
		RepositoryResourceName: dto.ResourceNameRepository(installationID, orgID, projectID, repoID),
		ActorID:                run.ActorID,
		Backend:                run.Backend,
		WorkflowPath:           run.WorkflowPath,
		Ref:                    run.Ref,
		Inputs:                 inputs,
		State:                  run.State,
		BackendDispatchID:      run.BackendDispatchID,
		FailureReason:          run.FailureReason,
		TraceID:                run.TraceID,
		DispatchedAt:           run.DispatchedAt,
		CreatedAt:              run.CreatedAt,
		UpdatedAt:              run.UpdatedAt,
	}
}

func workflowRunDTOs(runs []source.WorkflowRun, installationID string) []WorkflowRun {
	out := make([]WorkflowRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, workflowRunDTO(run, installationID))
	}
	return out
}

func uintString(value uint64) string {
	return fmtUint(value)
}
