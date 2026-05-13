package api

import (
	"context"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/verself/projects-service/internal/contractapi"
	"github.com/verself/projects-service/internal/projects"
)

type (
	projectPath               = contractapi.ProjectPathInput
	listProjectsInput         = contractapi.ListProjectsInput
	createProjectInput        = contractapi.CreateProjectInput
	updateProjectInput        = contractapi.PatchProjectInput
	projectLifecycleInput     = contractapi.ProjectLifecycleInput
	listEnvironmentsInput     = contractapi.ListProjectEnvironmentsInput
	createEnvironmentInput    = contractapi.CreateProjectEnvironmentInput
	updateEnvironmentInput    = contractapi.PatchProjectEnvironmentInput
	environmentLifecycleInput = contractapi.ProjectEnvironmentLifecycleInput
)

type (
	projectOutput         = contractapi.ProjectOutput
	projectListOutput     = contractapi.ListProjectsOutput
	environmentOutput     = contractapi.ProjectEnvironmentOutput
	environmentListOutput = contractapi.ListProjectEnvironmentsOutput
)

func createProjectOperation() projectOperation {
	op, policy := projectContract(contractapi.CreateProject.Descriptor, "Create a project")
	return publicProjectOperation[createProjectInput, projectOutput](op, policy, createProject)
}

func listProjectsOperation() projectOperation {
	op, policy := projectContract(contractapi.ListProjects.Descriptor, "List projects")
	return publicProjectOperation[listProjectsInput, projectListOutput](op, policy, listProjects)
}

func getProjectOperation() projectOperation {
	op, policy := projectContract(contractapi.GetProject.Descriptor, "Get a project")
	return publicProjectOperation[projectPath, projectOutput](op, policy, getProject)
}

func updateProjectOperation() projectOperation {
	op, policy := projectContract(contractapi.PatchProject.Descriptor, "Update a project")
	return publicProjectOperation[updateProjectInput, projectOutput](op, policy, updateProject)
}

func archiveProjectOperation() projectOperation {
	op, policy := projectContract(contractapi.ArchiveProject.Descriptor, "Archive a project")
	return publicProjectOperation[projectLifecycleInput, projectOutput](op, policy, archiveProject)
}

func restoreProjectOperation() projectOperation {
	op, policy := projectContract(contractapi.RestoreProject.Descriptor, "Restore a project")
	return publicProjectOperation[projectLifecycleInput, projectOutput](op, policy, restoreProject)
}

func listProjectEnvironmentsOperation() projectOperation {
	op, policy := projectContract(contractapi.ListProjectEnvironments.Descriptor, "List project environments")
	return publicProjectOperation[listEnvironmentsInput, environmentListOutput](op, policy, listEnvironments)
}

func createProjectEnvironmentOperation() projectOperation {
	op, policy := projectContract(contractapi.CreateProjectEnvironment.Descriptor, "Create a project environment")
	return publicProjectOperation[createEnvironmentInput, environmentOutput](op, policy, createEnvironment)
}

func updateProjectEnvironmentOperation() projectOperation {
	op, policy := projectContract(contractapi.PatchProjectEnvironment.Descriptor, "Update a project environment")
	return publicProjectOperation[updateEnvironmentInput, environmentOutput](op, policy, updateEnvironment)
}

func archiveProjectEnvironmentOperation() projectOperation {
	op, policy := projectContract(contractapi.ArchiveProjectEnvironment.Descriptor, "Archive a project environment")
	return publicProjectOperation[environmentLifecycleInput, environmentOutput](op, policy, archiveEnvironment)
}

func projectContract(desc contractapi.OperationDescriptor, summary string) (huma.Operation, projectsOperationPolicy) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        contractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": contractExtension(desc)},
	}
	return op, projectsOperationPolicy{OperationPolicy: operationPolicyFromContract(desc)}
}

func createProject(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *createProjectInput) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *createProjectInput) (*projectOutput, error) {
		project, err := svc.CreateProject(ctx, principal, projects.CreateProjectRequest{
			Slug:           stringFromContractPtr(input.Body.Slug),
			DisplayName:    string(input.Body.DisplayName),
			Description:    stringFromContractPtr(input.Body.Description),
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project, installationID)}, nil
	}
}

func listProjects(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *listProjectsInput) (*projectListOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *listProjectsInput) (*projectListOutput, error) {
		records, nextCursor, err := svc.ListProjects(ctx, principal, projects.ListProjectsRequest{
			State:  string(input.State),
			Limit:  int(input.Limit),
			Cursor: string(input.Cursor),
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectListOutput{Body: contractapi.ListProjectsOutputBody{Projects: projectDTOs(records, installationID), NextCursor: optionalContractString[contractapi.PageToken](nextCursor)}}, nil
	}
}

func getProject(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *projectPath) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *projectPath) (*projectOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		project, err := svc.GetProject(ctx, principal, projectID)
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project, installationID)}, nil
	}
}

func updateProject(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *updateProjectInput) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *updateProjectInput) (*projectOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		version, err := int64FromDecimal(ctx, string(input.Body.Version), "version")
		if err != nil {
			return nil, err
		}
		project, err := svc.UpdateProject(ctx, principal, projects.UpdateProjectRequest{
			ProjectID:      projectID,
			Version:        version,
			Slug:           optionalStringFromContract(input.Body.Slug),
			DisplayName:    optionalStringFromContract(input.Body.DisplayName),
			Description:    optionalStringFromContract(input.Body.Description),
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project, installationID)}, nil
	}
}

func archiveProject(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *projectLifecycleInput) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *projectLifecycleInput) (*projectOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		version, err := int64FromDecimal(ctx, string(input.Body.Version), "version")
		if err != nil {
			return nil, err
		}
		project, err := svc.ArchiveProject(ctx, principal, projects.ProjectLifecycleRequest{
			ProjectID:      projectID,
			Version:        version,
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project, installationID)}, nil
	}
}

func restoreProject(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *projectLifecycleInput) (*projectOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *projectLifecycleInput) (*projectOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		version, err := int64FromDecimal(ctx, string(input.Body.Version), "version")
		if err != nil {
			return nil, err
		}
		project, err := svc.RestoreProject(ctx, principal, projects.ProjectLifecycleRequest{
			ProjectID:      projectID,
			Version:        version,
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectOutput{Body: projectDTO(project, installationID)}, nil
	}
}

func listEnvironments(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *listEnvironmentsInput) (*environmentListOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *listEnvironmentsInput) (*environmentListOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		envs, err := svc.ListEnvironments(ctx, principal, projectID)
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &environmentListOutput{Body: contractapi.ListProjectEnvironmentsOutputBody{Environments: environmentDTOs(envs, installationID)}}, nil
	}
}

func createEnvironment(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *createEnvironmentInput) (*environmentOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *createEnvironmentInput) (*environmentOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		env, err := svc.CreateEnvironment(ctx, principal, projects.CreateEnvironmentRequest{
			ProjectID:        projectID,
			Slug:             string(input.Body.Slug),
			DisplayName:      string(input.Body.DisplayName),
			Kind:             string(input.Body.Kind),
			ProtectionPolicy: stringMapFromContractPtr(input.Body.ProtectionPolicy),
			IdempotencyKey:   operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &environmentOutput{Body: environmentDTO(env, installationID)}, nil
	}
}

func updateEnvironment(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *updateEnvironmentInput) (*environmentOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *updateEnvironmentInput) (*environmentOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		environmentID, err := parseUUID(ctx, input.EnvironmentID, "environment_id")
		if err != nil {
			return nil, err
		}
		version, err := int64FromDecimal(ctx, string(input.Body.Version), "version")
		if err != nil {
			return nil, err
		}
		env, err := svc.UpdateEnvironment(ctx, principal, projects.UpdateEnvironmentRequest{
			ProjectID:        projectID,
			EnvironmentID:    environmentID,
			Version:          version,
			DisplayName:      stringFromContractPtr(input.Body.DisplayName),
			ProtectionPolicy: stringMapFromContractPtr(input.Body.ProtectionPolicy),
			IdempotencyKey:   operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &environmentOutput{Body: environmentDTO(env, installationID)}, nil
	}
}

func archiveEnvironment(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *environmentLifecycleInput) (*environmentOutput, error) {
	return func(ctx context.Context, principal projects.Principal, input *environmentLifecycleInput) (*environmentOutput, error) {
		projectID, err := parseUUID(ctx, input.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		environmentID, err := parseUUID(ctx, input.EnvironmentID, "environment_id")
		if err != nil {
			return nil, err
		}
		version, err := int64FromDecimal(ctx, string(input.Body.Version), "version")
		if err != nil {
			return nil, err
		}
		env, err := svc.ArchiveEnvironment(ctx, principal, projects.EnvironmentLifecycleRequest{
			ProjectID:      projectID,
			EnvironmentID:  environmentID,
			Version:        version,
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &environmentOutput{Body: environmentDTO(env, installationID)}, nil
	}
}

func parseUUID[T ~string](ctx context.Context, value T, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(string(value))
	if err != nil {
		return uuid.Nil, badRequest(ctx, "invalid-"+field, field+" must be a UUID", err)
	}
	return id, nil
}

func int64FromDecimal(ctx context.Context, value, field string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, badRequest(ctx, "invalid-"+field, field+" must be a decimal int64", err)
	}
	return parsed, nil
}

func stringFromContractPtr[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func optionalStringFromContract[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	converted := string(*value)
	return &converted
}

func optionalContractString[T ~string](value string) *T {
	if value == "" {
		return nil
	}
	converted := T(value)
	return &converted
}

func optionalContractTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	out := formatContractTime(*value)
	return &out
}

func formatContractTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func stringMapFromContractPtr[T ~map[string]string](value *T) map[string]string {
	if value == nil || len(*value) == 0 {
		return nil
	}
	out := make(map[string]string, len(*value))
	for key, item := range *value {
		out[key] = item
	}
	return out
}

func optionalContractMap[T ~map[string]string](value map[string]string) *T {
	if len(value) == 0 {
		return nil
	}
	converted := T(value)
	return &converted
}

func projectResourceName(installationID, orgID, projectID string) string {
	return "urn:verself:" + installationID + ":orgs/" + orgID + "/projects/" + projectID
}

func environmentResourceName(installationID, orgID, projectID, environmentID string) string {
	return projectResourceName(installationID, orgID, projectID) + "/environments/" + environmentID
}

func projectDTO(project projects.Project, installationID string) contractapi.ProjectSummary {
	orgID := strconv.FormatUint(project.OrgID, 10)
	projectID := project.ID.String()
	return contractapi.ProjectSummary{
		ProjectID:          contractapi.ProjectID(projectID),
		ResourceName:       contractapi.ResourceName(projectResourceName(installationID, orgID, projectID)),
		OrgID:              contractapi.OrgID(orgID),
		Slug:               contractapi.ProjectSlug(project.Slug),
		RedirectedFromSlug: optionalContractString[contractapi.ProjectSlug](project.RedirectedFromSlug),
		DisplayName:        contractapi.DisplayName(project.DisplayName),
		Description:        optionalContractString[contractapi.ProjectDescription](project.Description),
		State:              contractapi.ProjectState(project.State),
		Version:            contractapi.DecimalInt64(strconv.FormatInt(project.Version, 10)),
		CreatedBy:          contractapi.ActorID(project.CreatedBy),
		UpdatedBy:          contractapi.ActorID(project.UpdatedBy),
		CreatedAt:          formatContractTime(project.CreatedAt),
		UpdatedAt:          formatContractTime(project.UpdatedAt),
		ArchivedAt:         optionalContractTime(project.ArchivedAt),
	}
}

func projectDTOs(records []projects.Project, installationID string) contractapi.ProjectsList {
	out := make(contractapi.ProjectsList, 0, len(records))
	for _, record := range records {
		out = append(out, projectDTO(record, installationID))
	}
	return out
}

func environmentDTO(env projects.Environment, installationID string) contractapi.ProjectEnvironmentSummary {
	orgID := strconv.FormatUint(env.OrgID, 10)
	projectID := env.ProjectID.String()
	return contractapi.ProjectEnvironmentSummary{
		EnvironmentID:       contractapi.EnvironmentID(env.ID.String()),
		ResourceName:        contractapi.ResourceName(environmentResourceName(installationID, orgID, projectID, env.ID.String())),
		ProjectID:           contractapi.ProjectID(projectID),
		ProjectResourceName: contractapi.ResourceName(projectResourceName(installationID, orgID, projectID)),
		OrgID:               contractapi.OrgID(orgID),
		Slug:                contractapi.ProjectSlug(env.Slug),
		DisplayName:         contractapi.DisplayName(env.DisplayName),
		Kind:                contractapi.ProjectEnvironmentKind(env.Kind),
		State:               contractapi.ProjectEnvironmentState(env.State),
		ProtectionPolicy:    optionalContractMap[contractapi.ProjectProtectionPolicy](env.ProtectionPolicy),
		Version:             contractapi.DecimalInt64(strconv.FormatInt(env.Version, 10)),
		CreatedBy:           contractapi.ActorID(env.CreatedBy),
		UpdatedBy:           contractapi.ActorID(env.UpdatedBy),
		CreatedAt:           formatContractTime(env.CreatedAt),
		UpdatedAt:           formatContractTime(env.UpdatedAt),
		ArchivedAt:          optionalContractTime(env.ArchivedAt),
	}
}

func environmentDTOs(records []projects.Environment, installationID string) contractapi.ProjectEnvironments {
	out := make(contractapi.ProjectEnvironments, 0, len(records))
	for _, record := range records {
		out = append(out, environmentDTO(record, installationID))
	}
	return out
}
