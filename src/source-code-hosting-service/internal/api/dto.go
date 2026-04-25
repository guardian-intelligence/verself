package api

import (
	"time"

	"github.com/google/uuid"

	"github.com/forge-metal/source-code-hosting-service/internal/source"
)

type Repository struct {
	RepoID        uuid.UUID  `json:"repo_id"`
	OrgID         string     `json:"org_id"`
	OrgPath       string     `json:"org_path"`
	Name          string     `json:"name"`
	Slug          string     `json:"slug"`
	Description   string     `json:"description"`
	DefaultBranch string     `json:"default_branch"`
	Backend       string     `json:"backend"`
	Visibility    string     `json:"visibility"`
	State         string     `json:"state"`
	Version       int32      `json:"version" minimum:"0" maximum:"2147483647"`
	LastPushedAt  *time.Time `json:"last_pushed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type RepositoryList struct {
	Repositories []Repository `json:"repositories"`
}

type CreateRepositoryRequest struct {
	Name          string `json:"name" required:"true" minLength:"1" maxLength:"128"`
	Description   string `json:"description,omitempty" maxLength:"1024"`
	DefaultBranch string `json:"default_branch,omitempty" maxLength:"128"`
}

type CreateGitCredentialRequest struct {
	Label            string `json:"label,omitempty" maxLength:"128"`
	ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty" minimum:"60" maximum:"7776000"`
}

type GitCredential struct {
	CredentialID uuid.UUID `json:"credential_id"`
	OrgID        string    `json:"org_id"`
	OrgPath      string    `json:"org_path"`
	Username     string    `json:"username"`
	Token        string    `json:"token"`
	TokenPrefix  string    `json:"token_prefix"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
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

type CIRun struct {
	CIRunID            uuid.UUID  `json:"ci_run_id"`
	OrgID              string     `json:"org_id"`
	RepoID             uuid.UUID  `json:"repo_id"`
	ActorID            string     `json:"actor_id"`
	RefName            string     `json:"ref_name"`
	CommitSHA          string     `json:"commit_sha"`
	TriggerEvent       string     `json:"trigger_event"`
	State              string     `json:"state"`
	SandboxExecutionID *uuid.UUID `json:"sandbox_execution_id,omitempty"`
	SandboxAttemptID   *uuid.UUID `json:"sandbox_attempt_id,omitempty"`
	FailureReason      string     `json:"failure_reason,omitempty"`
	TraceID            string     `json:"trace_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

type CIRunList struct {
	CIRuns []CIRun `json:"ci_runs"`
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
	WorkflowRunID     uuid.UUID         `json:"workflow_run_id"`
	OrgID             string            `json:"org_id"`
	RepoID            uuid.UUID         `json:"repo_id"`
	ActorID           string            `json:"actor_id"`
	Backend           string            `json:"backend"`
	WorkflowPath      string            `json:"workflow_path"`
	Ref               string            `json:"ref"`
	Inputs            map[string]string `json:"inputs"`
	State             string            `json:"state"`
	BackendDispatchID string            `json:"backend_dispatch_id,omitempty"`
	FailureReason     string            `json:"failure_reason,omitempty"`
	TraceID           string            `json:"trace_id,omitempty"`
	DispatchedAt      *time.Time        `json:"dispatched_at,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type WorkflowRunList struct {
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

func repositoryDTO(repo source.Repository) Repository {
	return Repository{
		RepoID:        repo.RepoID,
		OrgID:         uintString(repo.OrgID),
		OrgPath:       repo.OrgPath,
		Name:          repo.Name,
		Slug:          repo.Slug,
		Description:   repo.Description,
		DefaultBranch: repo.DefaultBranch,
		Backend:       repo.Backend.Backend,
		Visibility:    repo.Visibility,
		State:         repo.State,
		Version:       int32(repo.Version),
		LastPushedAt:  repo.LastPushedAt,
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

func gitCredentialDTO(credential source.GitCredential) GitCredential {
	return GitCredential{
		CredentialID: credential.CredentialID,
		OrgID:        uintString(credential.OrgID),
		OrgPath:      credential.OrgPath,
		Username:     credential.Username,
		Token:        credential.Token,
		TokenPrefix:  credential.TokenPrefix,
		Scopes:       credential.Scopes,
		ExpiresAt:    credential.ExpiresAt,
		CreatedAt:    credential.CreatedAt,
	}
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

func ciRunDTO(run source.CIRun) CIRun {
	out := CIRun{
		CIRunID:       run.CIRunID,
		OrgID:         uintString(run.OrgID),
		RepoID:        run.RepoID,
		ActorID:       run.ActorID,
		RefName:       run.RefName,
		CommitSHA:     run.CommitSHA,
		TriggerEvent:  run.TriggerEvent,
		State:         run.State,
		FailureReason: run.FailureReason,
		TraceID:       run.TraceID,
		CreatedAt:     run.CreatedAt,
		UpdatedAt:     run.UpdatedAt,
		StartedAt:     run.StartedAt,
		CompletedAt:   run.CompletedAt,
	}
	if run.SandboxExecutionID != uuid.Nil {
		out.SandboxExecutionID = &run.SandboxExecutionID
	}
	if run.SandboxAttemptID != uuid.Nil {
		out.SandboxAttemptID = &run.SandboxAttemptID
	}
	return out
}

func ciRunDTOs(runs []source.CIRun) []CIRun {
	out := make([]CIRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, ciRunDTO(run))
	}
	return out
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
		WorkflowRunID:     run.WorkflowRunID,
		OrgID:             uintString(run.OrgID),
		RepoID:            run.RepoID,
		ActorID:           run.ActorID,
		Backend:           run.Backend,
		WorkflowPath:      run.WorkflowPath,
		Ref:               run.Ref,
		Inputs:            inputs,
		State:             run.State,
		BackendDispatchID: run.BackendDispatchID,
		FailureReason:     run.FailureReason,
		TraceID:           run.TraceID,
		DispatchedAt:      run.DispatchedAt,
		CreatedAt:         run.CreatedAt,
		UpdatedAt:         run.UpdatedAt,
	}
}

func workflowRunDTOs(runs []source.WorkflowRun) []WorkflowRun {
	out := make([]WorkflowRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, workflowRunDTO(run))
	}
	return out
}

func uintString(value uint64) string {
	return fmtUint(value)
}
