package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/projects-service/internal/projects"
	workloadauth "github.com/verself/service-runtime/workload"
)

type apiSurface uint8

const (
	apiSurfacePublic apiSurface = 1 << iota
	apiSurfaceInternal
)

var publicInternalPeers = []string{
	workloadauth.ServiceSourceCodeHosting,
	workloadauth.ServiceSandboxRental,
}

type projectOperation interface {
	id() string
	surfaces() apiSurface
	register(api huma.API, svc *projects.Service, surface apiSurface)
}

type projectOperationDef[I, O any] struct {
	op                  huma.Operation
	policy              operationPolicy
	enabledSurfaces     apiSurface
	internalMirrorPeers []string
	handler             func(*projects.Service) func(context.Context, projects.Principal, *I) (*O, error)
}

func (o projectOperationDef[I, O]) id() string {
	return o.op.OperationID
}

func (o projectOperationDef[I, O]) surfaces() apiSurface {
	return o.enabledSurfaces
}

func (o projectOperationDef[I, O]) register(api huma.API, svc *projects.Service, surface apiSurface) {
	policy := o.policy
	if surface == apiSurfaceInternal && !policy.Internal {
		policy.Internal = true
		policy.InternalPeers = append([]string(nil), o.internalMirrorPeers...)
	}
	registerProjectsRoute(api, o.op, policy, o.handler(svc))
}

func registerProjectOperations(api huma.API, svc *projects.Service, surface apiSurface) {
	for _, op := range projectOperations() {
		if op.surfaces()&surface == 0 {
			continue
		}
		op.register(api, svc, surface)
	}
}

func publicProjectOperation[I, O any](
	op huma.Operation,
	policy operationPolicy,
	handler func(*projects.Service) func(context.Context, projects.Principal, *I) (*O, error),
) projectOperationDef[I, O] {
	return projectOperationDef[I, O]{
		op:                  op,
		policy:              policy,
		enabledSurfaces:     apiSurfacePublic | apiSurfaceInternal,
		internalMirrorPeers: publicInternalPeers,
		handler:             handler,
	}
}

func internalProjectOperation[I, O any](
	op huma.Operation,
	policy operationPolicy,
	handler func(*projects.Service) func(context.Context, projects.Principal, *I) (*O, error),
) projectOperationDef[I, O] {
	policy.Internal = true
	return projectOperationDef[I, O]{
		op:              op,
		policy:          policy,
		enabledSurfaces: apiSurfaceInternal,
		handler:         handler,
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
