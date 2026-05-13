package api

import (
	"context"
	"sort"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/sandbox-rental-service/internal/contractapi"
	"github.com/verself/sandbox-rental-service/internal/internalcontractapi"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func registerSandboxContractRoute[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, operation contractapi.Operation[I, O], summary string, handler contractapi.Handler[I, O]) {
	desc := operation.Descriptor
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        contractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": contractExtension(desc)},
	}
	registerSecured(api, authorizer, secured(op, operationPolicyFromContract(desc)), func(ctx context.Context, input *I) (*O, error) {
		return handler(ctx, input)
	})
}

func registerInternalSandboxContractRoute[I, O any](api huma.API, operation internalcontractapi.Operation[I, O], summary string, handler internalcontractapi.Handler[I, O]) {
	desc := operation.Descriptor
	policy := operationPolicyFromInternalContract(desc)
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        internalContractProblemStatuses(desc.Problems),
		Extensions: map[string]any{
			"x-verself-contract": internalContractExtension(desc),
			"x-verself-iam":      policy.OpenAPIExtension(),
		},
		Security: []map[string][]string{{"mutualTLS": {}}},
	}
	if desc.RequestBodyMaxBytes > 0 {
		op.MaxBodyBytes = desc.RequestBodyMaxBytes
	}
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		return handler(ctx, input)
	})
}

func contractExtension(desc contractapi.OperationDescriptor) map[string]any {
	return map[string]any{
		"shape_id":               desc.ShapeID,
		"operation_id":           desc.OperationID,
		"identity":               desc.Identity.Mode,
		"audience":               desc.Identity.Audience,
		"permission":             desc.Authorization.Permission,
		"organization_source":    desc.Authorization.OrganizationSource,
		"organization_member":    desc.Authorization.OrganizationMember,
		"audit_event":            desc.Audit.Event,
		"resource":               desc.Audit.Resource,
		"action":                 desc.Audit.Action,
		"rate_limit_bucket":      desc.RateLimitBucket,
		"request_body_max_bytes": desc.RequestBodyMaxBytes,
		"idempotency":            desc.Idempotency.Policy,
	}
}

func internalContractExtension(desc internalcontractapi.OperationDescriptor) map[string]any {
	return map[string]any{
		"shape_id":               desc.ShapeID,
		"operation_id":           desc.OperationID,
		"identity":               desc.Identity.Mode,
		"audience":               desc.Identity.Audience,
		"permission":             desc.Authorization.Permission,
		"organization_source":    desc.Authorization.OrganizationSource,
		"organization_member":    desc.Authorization.OrganizationMember,
		"audit_event":            desc.Audit.Event,
		"resource":               desc.Audit.Resource,
		"action":                 desc.Audit.Action,
		"rate_limit_bucket":      desc.RateLimitBucket,
		"request_body_max_bytes": desc.RequestBodyMaxBytes,
		"idempotency":            desc.Idempotency.Policy,
	}
}

func operationPolicyFromContract(desc contractapi.OperationDescriptor) runtimeiam.OperationPolicy {
	return runtimeiam.OperationPolicy{
		Permission:     runtimeiam.Permission(desc.Authorization.Permission),
		Resource:       runtimeiam.ResourceKind(desc.Audit.Resource),
		Action:         runtimeiam.Action(desc.Audit.Action),
		OrgScope:       runtimeiam.OrgScope(desc.Authorization.OrganizationSource),
		RateLimitClass: runtimeiam.RateLimitClass(desc.RateLimitBucket),
		Idempotency:    runtimeiam.IdempotencyPolicy(desc.Idempotency.Policy),
		AuditEvent:     runtimeiam.AuditEvent(desc.Audit.Event),
		BodyLimitBytes: desc.RequestBodyMaxBytes,
	}
}

func operationPolicyFromInternalContract(desc internalcontractapi.OperationDescriptor) runtimeiam.OperationPolicy {
	return runtimeiam.OperationPolicy{
		Permission:     runtimeiam.Permission(desc.Authorization.Permission),
		Resource:       runtimeiam.ResourceKind(desc.Audit.Resource),
		Action:         runtimeiam.Action(desc.Audit.Action),
		OrgScope:       runtimeiam.OrgScope(desc.Authorization.OrganizationSource),
		RateLimitClass: runtimeiam.RateLimitClass(desc.RateLimitBucket),
		Idempotency:    runtimeiam.IdempotencyPolicy(desc.Idempotency.Policy),
		AuditEvent:     runtimeiam.AuditEvent(desc.Audit.Event),
		BodyLimitBytes: desc.RequestBodyMaxBytes,
	}
}

func contractProblemStatuses(problems []contractapi.ProblemDescriptor) []int {
	statuses := make([]int, 0, len(problems))
	for _, problem := range problems {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	return uniqueSortedStatuses(statuses)
}

func internalContractProblemStatuses(problems []internalcontractapi.ProblemDescriptor) []int {
	statuses := make([]int, 0, len(problems))
	for _, problem := range problems {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	return uniqueSortedStatuses(statuses)
}

func uniqueSortedStatuses(statuses []int) []int {
	sort.Ints(statuses)
	out := statuses[:0]
	previous := 0
	for _, status := range statuses {
		if status == previous {
			continue
		}
		out = append(out, status)
		previous = status
	}
	return out
}
