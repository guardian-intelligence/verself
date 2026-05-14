package api

import (
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/verself/source-code-hosting-service/internal/contractapi"
	"github.com/verself/source-code-hosting-service/internal/internalcontractapi"
	"github.com/verself/source-code-hosting-service/internal/source"
)

type resourcePathSegment struct {
	Collection string
	ID         string
}

func repositoryView(repo source.Repository, org source.OrganizationReference, project source.ProjectReference, publicBaseURL string, installationID string) contractapi.RepositorySummary {
	orgID := repo.OrgID
	projectID := project.ProjectID.String()
	return contractapi.RepositorySummary{
		RepoID:              contractapi.RepositoryID(repo.RepoID.String()),
		ResourceName:        contractapi.ResourceName(resourceNameRepository(installationID, orgID, projectID, repo.RepoID.String())),
		OrgID:               contractapi.OrgID(orgID),
		ProjectID:           contractapi.ProjectID(repo.ProjectID.String()),
		ProjectResourceName: contractapi.ResourceName(resourceNameProject(installationID, orgID, projectID)),
		OrgSlug:             contractapi.OrgSlug(org.Slug),
		ProjectSlug:         contractapi.ProjectSlug(project.Slug),
		GitHTTPURL:          contractapi.GitURL(gitHTTPURL(publicBaseURL, org.Slug, project.Slug)),
		Name:                contractapi.RepositoryName(repo.Name),
		Description:         contractapi.RepositoryDescription(repo.Description),
		DefaultBranch:       contractapi.BranchName(repo.DefaultBranch),
		Backend:             contractapi.SourceBackend(repo.Backend.Backend),
		Visibility:          contractapi.RepositoryVisibility(repo.Visibility),
		State:               contractapi.SourceState(repo.State),
		Version:             contractapi.RepositoryVersion(int(int32FromInt64(repo.Version, "repository version"))),
		LastPushedAt:        optionalTimestamp(repo.LastPushedAt),
		CreatedAt:           timestampString(repo.CreatedAt),
		UpdatedAt:           timestampString(repo.UpdatedAt),
	}
}

func repositoryViews(repos []source.Repository, org source.OrganizationReference, projects map[uuid.UUID]source.ProjectReference, publicBaseURL string, installationID string) contractapi.Repositories {
	out := make(contractapi.Repositories, 0, len(repos))
	for _, repo := range repos {
		out = append(out, repositoryView(repo, org, projects[repo.ProjectID], publicBaseURL, installationID))
	}
	return out
}

func gitCredentialView(credential source.GitCredential, installationID string) contractapi.GitCredentialSummary {
	orgID := credential.OrgID
	return contractapi.GitCredentialSummary{
		CredentialID: contractapi.GitCredentialID(credential.CredentialID.String()),
		ResourceName: contractapi.ResourceName(resourceNameCredential(installationID, orgID, credential.CredentialID.String())),
		OrgID:        contractapi.OrgID(orgID),
		Username:     contractapi.GitUsername(credential.Username),
		Token:        contractapi.GitToken(credential.Token),
		TokenPrefix:  contractapi.GitTokenPrefix(credential.TokenPrefix),
		Scopes:       gitScopeViews(credential.Scopes),
		ExpiresAt:    timestampString(credential.ExpiresAt),
		CreatedAt:    timestampString(credential.CreatedAt),
	}
}

func refViews(refs []source.Ref) contractapi.Refs {
	out := make(contractapi.Refs, 0, len(refs))
	for _, ref := range refs {
		out = append(out, contractapi.SourceRefView{
			Name:   contractapi.GitRef(ref.Name),
			Commit: contractapi.GitObjectSha(ref.Commit),
		})
	}
	return out
}

func treeEntryViews(entries []source.TreeEntry) contractapi.TreeEntries {
	out := make(contractapi.TreeEntries, 0, len(entries))
	for _, entry := range entries {
		out = append(out, contractapi.TreeEntry{
			Path: contractapi.RepositoryPath(entry.Path),
			Type: contractapi.GitObjectType(entry.Type),
			Size: contractapi.SafeByteCount(entry.Size),
			Sha:  contractapi.GitObjectSha(entry.SHA),
		})
	}
	return out
}

func blobView(blob source.Blob) contractapi.SourceBlobView {
	return contractapi.SourceBlobView{
		Path:        contractapi.RepositoryPath(blob.Path),
		Name:        contractapi.GitObjectName(blob.Name),
		Encoding:    contractapi.GitObjectEncoding(blob.Encoding),
		Content:     blob.Content,
		Size:        contractapi.SafeByteCount(blob.Size),
		Sha:         contractapi.GitObjectSha(blob.SHA),
		DownloadURL: optionalStringAlias[contractapi.BlobDownloadURL](blob.DownloadURL),
	}
}

func checkoutGrantView(grant source.CheckoutGrant, installationID string) contractapi.CheckoutGrantSummary {
	return contractapi.CheckoutGrantSummary{
		GrantID:      contractapi.CheckoutGrantID(grant.GrantID.String()),
		ResourceName: contractapi.ResourceName(resourceNameCheckoutGrant(installationID, grant.OrgID, grant.GrantID.String())),
		RepoID:       contractapi.RepositoryID(grant.RepoID.String()),
		Ref:          contractapi.GitRef(grant.Ref),
		Token:        contractapi.CheckoutGrantToken(grant.Token),
		ExpiresAt:    timestampString(grant.ExpiresAt),
	}
}

func workflowRunView(run source.WorkflowRun, installationID string) contractapi.WorkflowRunSummary {
	orgID := run.OrgID
	projectID := run.ProjectID.String()
	repoID := run.RepoID.String()
	return contractapi.WorkflowRunSummary{
		WorkflowRunID:          contractapi.WorkflowRunID(run.WorkflowRunID.String()),
		ResourceName:           contractapi.ResourceName(resourceNameSourceWorkflowRun(installationID, orgID, projectID, repoID, run.WorkflowRunID.String())),
		OrgID:                  contractapi.OrgID(orgID),
		ProjectID:              contractapi.ProjectID(projectID),
		ProjectResourceName:    contractapi.ResourceName(resourceNameProject(installationID, orgID, projectID)),
		RepoID:                 contractapi.RepositoryID(repoID),
		RepositoryResourceName: contractapi.ResourceName(resourceNameRepository(installationID, orgID, projectID, repoID)),
		ActorID:                contractapi.ActorID(run.ActorID),
		Backend:                contractapi.SourceBackend(run.Backend),
		WorkflowPath:           contractapi.WorkflowPath(run.WorkflowPath),
		Ref:                    contractapi.GitRef(run.Ref),
		Inputs:                 workflowInputViews(run.Inputs),
		State:                  contractapi.SourceState(run.State),
		BackendDispatchID:      optionalStringAlias[contractapi.BackendDispatchID](run.BackendDispatchID),
		FailureReason:          optionalStringAlias[contractapi.FailureReason](run.FailureReason),
		TraceID:                optionalStringAlias[contractapi.TraceID](run.TraceID),
		DispatchedAt:           optionalTimestamp(run.DispatchedAt),
		CreatedAt:              timestampString(run.CreatedAt),
		UpdatedAt:              timestampString(run.UpdatedAt),
	}
}

func workflowRunViews(runs []source.WorkflowRun, installationID string) contractapi.WorkflowRuns {
	out := make(contractapi.WorkflowRuns, 0, len(runs))
	for _, run := range runs {
		out = append(out, workflowRunView(run, installationID))
	}
	return out
}

func internalWorkflowRunView(run source.WorkflowRun, installationID string) internalcontractapi.WorkflowRunSummary {
	orgID := run.OrgID
	projectID := run.ProjectID.String()
	repoID := run.RepoID.String()
	return internalcontractapi.WorkflowRunSummary{
		WorkflowRunID:          internalcontractapi.WorkflowRunID(run.WorkflowRunID.String()),
		ResourceName:           internalcontractapi.ResourceName(resourceNameSourceWorkflowRun(installationID, orgID, projectID, repoID, run.WorkflowRunID.String())),
		OrgID:                  internalcontractapi.OrgID(orgID),
		ProjectID:              internalcontractapi.ProjectID(projectID),
		ProjectResourceName:    internalcontractapi.ResourceName(resourceNameProject(installationID, orgID, projectID)),
		RepoID:                 internalcontractapi.RepositoryID(repoID),
		RepositoryResourceName: internalcontractapi.ResourceName(resourceNameRepository(installationID, orgID, projectID, repoID)),
		ActorID:                internalcontractapi.ActorID(run.ActorID),
		Backend:                internalcontractapi.SourceBackend(run.Backend),
		WorkflowPath:           internalcontractapi.WorkflowPath(run.WorkflowPath),
		Ref:                    internalcontractapi.GitRef(run.Ref),
		Inputs:                 internalWorkflowInputViews(run.Inputs),
		State:                  internalcontractapi.SourceState(run.State),
		BackendDispatchID:      optionalStringAlias[internalcontractapi.BackendDispatchID](run.BackendDispatchID),
		FailureReason:          optionalStringAlias[internalcontractapi.FailureReason](run.FailureReason),
		TraceID:                optionalStringAlias[internalcontractapi.TraceID](run.TraceID),
		DispatchedAt:           optionalTimestamp(run.DispatchedAt),
		CreatedAt:              timestampString(run.CreatedAt),
		UpdatedAt:              timestampString(run.UpdatedAt),
	}
}

func gitScopeViews(scopes []string) contractapi.GitScopes {
	out := make(contractapi.GitScopes, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, contractapi.GitScope(scope))
	}
	return out
}

func gitScopesFromContract(scopes contractapi.GitScopes) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, string(scope))
	}
	return out
}

func workflowInputViews(inputs map[string]string) contractapi.WorkflowInputs {
	out := make(contractapi.WorkflowInputs, len(inputs))
	for key, value := range inputs {
		out[key] = contractapi.WorkflowInputValue(value)
	}
	return out
}

func internalWorkflowInputViews(inputs map[string]string) internalcontractapi.WorkflowInputs {
	out := make(internalcontractapi.WorkflowInputs, len(inputs))
	for key, value := range inputs {
		out[key] = internalcontractapi.WorkflowInputValue(value)
	}
	return out
}

func workflowInputsFromContract(inputs *contractapi.WorkflowInputs) map[string]string {
	if inputs == nil {
		return nil
	}
	out := make(map[string]string, len(*inputs))
	for key, value := range *inputs {
		out[key] = string(value)
	}
	return out
}

func workflowInputsFromInternalContract(inputs *internalcontractapi.WorkflowInputs) map[string]string {
	if inputs == nil {
		return nil
	}
	out := make(map[string]string, len(*inputs))
	for key, value := range *inputs {
		out[key] = string(value)
	}
	return out
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

func timestampString(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func optionalTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}
	out := timestampString(*value)
	return &out
}

func optionalStringAlias[T ~string](value string) *T {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := T(value)
	return &out
}

func resourceNameProject(installationID, orgID, projectID string) string {
	return formatResourceName(installationID,
		resourcePathSegment{Collection: "orgs", ID: orgID},
		resourcePathSegment{Collection: "projects", ID: projectID},
	)
}

func resourceNameRepository(installationID, orgID, projectID, repositoryID string) string {
	return formatResourceName(installationID,
		resourcePathSegment{Collection: "orgs", ID: orgID},
		resourcePathSegment{Collection: "projects", ID: projectID},
		resourcePathSegment{Collection: "repositories", ID: repositoryID},
	)
}

func resourceNameSourceWorkflowRun(installationID, orgID, projectID, repositoryID, workflowRunID string) string {
	return formatResourceName(installationID,
		resourcePathSegment{Collection: "orgs", ID: orgID},
		resourcePathSegment{Collection: "projects", ID: projectID},
		resourcePathSegment{Collection: "repositories", ID: repositoryID},
		resourcePathSegment{Collection: "workflowRuns", ID: workflowRunID},
	)
}

func resourceNameCredential(installationID, orgID, credentialID string) string {
	return formatResourceName(installationID,
		resourcePathSegment{Collection: "orgs", ID: orgID},
		resourcePathSegment{Collection: "credentials", ID: credentialID},
	)
}

func resourceNameCheckoutGrant(installationID, orgID, grantID string) string {
	return formatResourceName(installationID,
		resourcePathSegment{Collection: "orgs", ID: orgID},
		resourcePathSegment{Collection: "checkoutGrants", ID: grantID},
	)
}

func formatResourceName(installationID string, segments ...resourcePathSegment) string {
	installationID = strings.TrimSpace(installationID)
	parts := make([]string, 0, len(segments)*2)
	for _, segment := range segments {
		parts = append(parts, strings.TrimSpace(segment.Collection), strings.TrimSpace(segment.ID))
	}
	return "urn:verself:" + installationID + ":" + strings.Join(parts, "/")
}
