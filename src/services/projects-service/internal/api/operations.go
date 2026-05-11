package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/projects-service/internal/projects"
	runtimeiam "github.com/verself/service-runtime/iam"
	workloadauth "github.com/verself/service-runtime/workload"
)

type apiProjection uint8

const (
	apiProjectionPublic apiProjection = 1 << iota
	apiProjectionInternal
)

var publicServicePeers = []string{
	workloadauth.ServiceSourceCodeHosting,
	workloadauth.ServiceSandboxRental,
}

type projectOperation interface {
	id() string
	projections() apiProjection
	register(api huma.API, svc *projects.Service, projection apiProjection, authorizer runtimeiam.OperationAuthorizer)
}

type projectOperationDef[I, O any] struct {
	op                 huma.Operation
	policy             projectsOperationPolicy
	enabledProjections apiProjection
	serviceMirrorPeers []string
	handler            func(*projects.Service) func(context.Context, projects.Principal, *I) (*O, error)
}

func (o projectOperationDef[I, O]) id() string {
	return o.op.OperationID
}

func (o projectOperationDef[I, O]) projections() apiProjection {
	return o.enabledProjections
}

func (o projectOperationDef[I, O]) register(api huma.API, svc *projects.Service, projection apiProjection, authorizer runtimeiam.OperationAuthorizer) {
	policy := o.policy
	if projection == apiProjectionInternal && !policy.Service {
		policy.Service = true
		policy.ServicePeers = append([]string(nil), o.serviceMirrorPeers...)
	}
	registerProjectsRoute(api, authorizer, o.op, policy, o.handler(svc))
}

func registerProjectOperations(api huma.API, svc *projects.Service, projection apiProjection, authorizer runtimeiam.OperationAuthorizer) {
	for _, op := range projectOperations() {
		if op.projections()&projection == 0 {
			continue
		}
		op.register(api, svc, projection, authorizer)
	}
}

func publicProjectOperation[I, O any](
	op huma.Operation,
	policy projectsOperationPolicy,
	handler func(*projects.Service) func(context.Context, projects.Principal, *I) (*O, error),
) projectOperationDef[I, O] {
	return projectOperationDef[I, O]{
		op:                 op,
		policy:             policy,
		enabledProjections: apiProjectionPublic | apiProjectionInternal,
		serviceMirrorPeers: publicServicePeers,
		handler:            handler,
	}
}

func serviceOnlyProjectOperation[I, O any](
	op huma.Operation,
	policy projectsOperationPolicy,
	handler func(*projects.Service) func(context.Context, projects.Principal, *I) (*O, error),
) projectOperationDef[I, O] {
	policy.Service = true
	return projectOperationDef[I, O]{
		op:                 op,
		policy:             policy,
		enabledProjections: apiProjectionInternal,
		handler:            handler,
	}
}

func projectOperations() []projectOperation {
	return []projectOperation{
		createProjectOperation(),
		listProjectsOperation(),
		getProjectOperation(),
		updateProjectOperation(),
		archiveProjectOperation(),
		restoreProjectOperation(),
		listProjectEnvironmentsOperation(),
		createProjectEnvironmentOperation(),
		updateProjectEnvironmentOperation(),
		archiveProjectEnvironmentOperation(),
		resolveProjectOperation(),
		resolveProjectEnvironmentOperation(),
		listProjectEventsOperation(),
	}
}
