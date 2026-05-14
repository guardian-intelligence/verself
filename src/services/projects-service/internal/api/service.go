package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/verself/projects-service/internal/internalcontractapi"
	"github.com/verself/projects-service/internal/projects"
	workloadauth "github.com/verself/service-runtime/workload"
)

type (
	resolveProjectInput     = internalcontractapi.ResolveProjectInput
	resolveEnvironmentInput = internalcontractapi.ResolveProjectEnvironmentInput
	projectEventsInput      = internalcontractapi.ListProjectEventsServiceInput
)

type (
	resolveProjectOutput     = internalcontractapi.ResolveProjectOutput
	resolveEnvironmentOutput = internalcontractapi.ResolveProjectEnvironmentOutput
	projectEventsOutput      = internalcontractapi.ListProjectEventsServiceOutput
)

func resolveProjectOperation() projectOperation {
	return serviceOnlyProjectOperation[resolveProjectInput, resolveProjectOutput](internalProjectContract(internalcontractapi.ResolveProject.Descriptor, "Resolve a project for a repo-owned service"), projectsOperationPolicy{
		OperationPolicy: operationPolicyFromInternalContract(internalcontractapi.ResolveProject.Descriptor),
		Service:         true,
		ServicePeers: []string{
			workloadauth.ServiceSourceCodeHosting,
			workloadauth.ServiceSandboxRental,
		},
	}, resolveProject)
}

func resolveProjectEnvironmentOperation() projectOperation {
	return serviceOnlyProjectOperation[resolveEnvironmentInput, resolveEnvironmentOutput](internalProjectContract(internalcontractapi.ResolveProjectEnvironment.Descriptor, "Resolve a project environment for a repo-owned service"), projectsOperationPolicy{
		OperationPolicy: operationPolicyFromInternalContract(internalcontractapi.ResolveProjectEnvironment.Descriptor),
		Service:         true,
		ServicePeers: []string{
			workloadauth.ServiceSourceCodeHosting,
			workloadauth.ServiceSandboxRental,
		},
	}, resolveEnvironment)
}

func listProjectEventsOperation() projectOperation {
	return serviceOnlyProjectOperation[projectEventsInput, projectEventsOutput](internalProjectContract(internalcontractapi.ListProjectEventsService.Descriptor, "List project domain events"), projectsOperationPolicy{
		OperationPolicy: operationPolicyFromInternalContract(internalcontractapi.ListProjectEventsService.Descriptor),
		Service:         true,
		ServicePeers:    []string{workloadauth.ServiceGovernance},
	}, listEvents)
}

func internalProjectContract(desc internalcontractapi.OperationDescriptor, summary string) huma.Operation {
	return huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        internalContractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": internalContractExtension(desc)},
	}
}

func resolveProject(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *resolveProjectInput) (*resolveProjectOutput, error) {
	return func(ctx context.Context, _ projects.Principal, input *resolveProjectInput) (*resolveProjectOutput, error) {
		projectID, err := optionalUUID(ctx, input.Body.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		orgID, err := orgIDFromContract(ctx, string(input.Body.OrgID), "org_id")
		if err != nil {
			return nil, err
		}
		project, err := svc.ResolveProject(ctx, projects.ResolveProjectRequest{
			OrgID:         orgID,
			ProjectID:     projectID,
			Slug:          stringFromContractPtr(input.Body.Slug),
			RequireActive: input.Body.RequireActive,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &resolveProjectOutput{Body: internalcontractapi.ResolveProjectOutputBody{Project: internalProjectDTO(project, installationID)}}, nil
	}
}

func resolveEnvironment(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *resolveEnvironmentInput) (*resolveEnvironmentOutput, error) {
	return func(ctx context.Context, _ projects.Principal, input *resolveEnvironmentInput) (*resolveEnvironmentOutput, error) {
		projectID, err := parseUUID(ctx, input.Body.ProjectID, "project_id")
		if err != nil {
			return nil, err
		}
		environmentID, err := optionalUUID(ctx, input.Body.EnvironmentID, "environment_id")
		if err != nil {
			return nil, err
		}
		orgID, err := orgIDFromContract(ctx, string(input.Body.OrgID), "org_id")
		if err != nil {
			return nil, err
		}
		env, err := svc.ResolveEnvironment(ctx, projects.ResolveEnvironmentRequest{
			OrgID:         orgID,
			ProjectID:     projectID,
			EnvironmentID: environmentID,
			Slug:          stringFromContractPtr(input.Body.Slug),
			RequireActive: input.Body.RequireActive,
		})
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &resolveEnvironmentOutput{Body: internalcontractapi.ResolveProjectEnvironmentOutputBody{Environment: internalEnvironmentDTO(env, installationID)}}, nil
	}
}

func listEvents(svc *projects.Service, installationID string) func(context.Context, projects.Principal, *projectEventsInput) (*projectEventsOutput, error) {
	return func(ctx context.Context, _ projects.Principal, input *projectEventsInput) (*projectEventsOutput, error) {
		orgID, err := orgIDFromContract(ctx, string(input.OrgID), "org_id")
		if err != nil {
			return nil, err
		}
		events, nextCursor, err := svc.ListEvents(ctx, orgID, string(input.Cursor), int(input.Limit))
		if err != nil {
			return nil, projectsError(ctx, err)
		}
		return &projectEventsOutput{Body: internalcontractapi.ListProjectEventsServiceOutputBody{Events: eventDTOs(events, installationID), NextCursor: optionalContractString[internalcontractapi.PageToken](nextCursor)}}, nil
	}
}

func optionalUUID[T ~string](ctx context.Context, value *T, field string) (uuid.UUID, error) {
	if value == nil || string(*value) == "" {
		return uuid.Nil, nil
	}
	return parseUUID(ctx, *value, field)
}

func orgIDFromContract(ctx context.Context, value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", badRequest(ctx, "invalid-"+field, field+" is required", nil)
	}
	return value, nil
}

func internalProjectDTO(project projects.Project, installationID string) internalcontractapi.ProjectSummary {
	orgID := project.OrgID
	projectID := project.ID.String()
	return internalcontractapi.ProjectSummary{
		ProjectID:          internalcontractapi.ProjectID(projectID),
		ResourceName:       internalcontractapi.ResourceName(projectResourceName(installationID, orgID, projectID)),
		OrgID:              internalcontractapi.OrgID(orgID),
		Slug:               internalcontractapi.ProjectSlug(project.Slug),
		RedirectedFromSlug: optionalContractString[internalcontractapi.ProjectSlug](project.RedirectedFromSlug),
		DisplayName:        internalcontractapi.DisplayName(project.DisplayName),
		Description:        optionalContractString[internalcontractapi.ProjectDescription](project.Description),
		State:              internalcontractapi.ProjectState(project.State),
		Version:            internalcontractapi.DecimalInt64(strconv.FormatInt(project.Version, 10)),
		CreatedBy:          internalcontractapi.ActorID(project.CreatedBy),
		UpdatedBy:          internalcontractapi.ActorID(project.UpdatedBy),
		CreatedAt:          formatContractTime(project.CreatedAt),
		UpdatedAt:          formatContractTime(project.UpdatedAt),
		ArchivedAt:         optionalContractTime(project.ArchivedAt),
	}
}

func internalEnvironmentDTO(env projects.Environment, installationID string) internalcontractapi.ProjectEnvironmentSummary {
	orgID := env.OrgID
	projectID := env.ProjectID.String()
	return internalcontractapi.ProjectEnvironmentSummary{
		EnvironmentID:       internalcontractapi.EnvironmentID(env.ID.String()),
		ResourceName:        internalcontractapi.ResourceName(environmentResourceName(installationID, orgID, projectID, env.ID.String())),
		ProjectID:           internalcontractapi.ProjectID(projectID),
		ProjectResourceName: internalcontractapi.ResourceName(projectResourceName(installationID, orgID, projectID)),
		OrgID:               internalcontractapi.OrgID(orgID),
		Slug:                internalcontractapi.ProjectSlug(env.Slug),
		DisplayName:         internalcontractapi.DisplayName(env.DisplayName),
		Kind:                internalcontractapi.ProjectEnvironmentKind(env.Kind),
		State:               internalcontractapi.ProjectEnvironmentState(env.State),
		ProtectionPolicy:    optionalContractMap[internalcontractapi.ProjectProtectionPolicy](env.ProtectionPolicy),
		Version:             internalcontractapi.DecimalInt64(strconv.FormatInt(env.Version, 10)),
		CreatedBy:           internalcontractapi.ActorID(env.CreatedBy),
		UpdatedBy:           internalcontractapi.ActorID(env.UpdatedBy),
		CreatedAt:           formatContractTime(env.CreatedAt),
		UpdatedAt:           formatContractTime(env.UpdatedAt),
		ArchivedAt:          optionalContractTime(env.ArchivedAt),
	}
}

func eventDTO(event projects.Event, installationID string) internalcontractapi.ProjectEventSummary {
	var environmentID *internalcontractapi.EnvironmentID
	var envResourceName *internalcontractapi.ResourceName
	if event.EnvironmentID != uuid.Nil {
		environmentID = optionalContractString[internalcontractapi.EnvironmentID](event.EnvironmentID.String())
		orgID := event.OrgID
		envResourceName = optionalContractString[internalcontractapi.ResourceName](environmentResourceName(installationID, orgID, event.ProjectID.String(), event.EnvironmentID.String()))
	}
	orgID := event.OrgID
	projectID := event.ProjectID.String()
	return internalcontractapi.ProjectEventSummary{
		EventID:                 internalcontractapi.ProjectEventID(event.ID.String()),
		OrgID:                   internalcontractapi.OrgID(orgID),
		ProjectID:               internalcontractapi.ProjectID(projectID),
		ProjectResourceName:     internalcontractapi.ResourceName(projectResourceName(installationID, orgID, projectID)),
		EnvironmentID:           environmentID,
		EnvironmentResourceName: envResourceName,
		EventType:               internalcontractapi.EventType(event.EventType),
		ActorID:                 internalcontractapi.ActorID(event.ActorID),
		Payload:                 optionalContractMap[internalcontractapi.ProjectEventPayload](event.Payload),
		TraceID:                 optionalContractString[internalcontractapi.TraceID](event.TraceID),
		CreatedAt:               formatContractTime(event.CreatedAt),
	}
}

func eventDTOs(records []projects.Event, installationID string) internalcontractapi.ProjectEvents {
	out := make(internalcontractapi.ProjectEvents, 0, len(records))
	for _, record := range records {
		out = append(out, eventDTO(record, installationID))
	}
	return out
}
