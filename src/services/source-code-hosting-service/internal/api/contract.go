package api

import (
	"context"
	"sort"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/service-runtime/iam"
	"github.com/verself/source-code-hosting-service/internal/contractapi"
	"github.com/verself/source-code-hosting-service/internal/internalcontractapi"
	"github.com/verself/source-code-hosting-service/internal/source"
)

func registerPublicSourceRoute[I, O any](api huma.API, authorizer iam.OperationAuthorizer, desc contractapi.OperationDescriptor, summary string, handler func(context.Context, source.Principal, *I) (*O, error)) {
	op, policy := sourceContract(desc, summary)
	registerSourceRoute(api, authorizer, op, policy, handler)
}

func registerInternalSourceRoute[I, O any](api huma.API, authorizer iam.OperationAuthorizer, desc internalcontractapi.OperationDescriptor, summary string, handler func(context.Context, source.Principal, *I) (*O, error)) {
	op, policy := sourceInternalContract(desc, summary)
	registerSourceRoute(api, authorizer, op, policy, handler)
}

func sourceContract(desc contractapi.OperationDescriptor, summary string) (huma.Operation, sourceOperationPolicy) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        contractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": contractExtension(desc)},
	}
	return op, sourceOperationPolicy{OperationPolicy: operationPolicyFromContract(desc)}
}

func sourceInternalContract(desc internalcontractapi.OperationDescriptor, summary string) (huma.Operation, sourceOperationPolicy) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        internalContractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": internalContractExtension(desc)},
	}
	return op, sourceOperationPolicy{
		OperationPolicy: operationPolicyFromInternalContract(desc),
		Internal:        true,
	}
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

func operationPolicyFromContract(desc contractapi.OperationDescriptor) iam.OperationPolicy {
	return iam.OperationPolicy{
		Permission:     iam.Permission(desc.Authorization.Permission),
		Resource:       iam.ResourceKind(desc.Audit.Resource),
		Action:         iam.Action(desc.Audit.Action),
		OrgScope:       iam.OrgScope(runtimeOrgScopeFromContract(desc.Authorization.OrganizationSource)),
		RateLimitClass: iam.RateLimitClass(desc.RateLimitBucket),
		Idempotency:    iam.IdempotencyPolicy(desc.Idempotency.Policy),
		AuditEvent:     iam.AuditEvent(desc.Audit.Event),
		BodyLimitBytes: desc.RequestBodyMaxBytes,
	}
}

func operationPolicyFromInternalContract(desc internalcontractapi.OperationDescriptor) iam.OperationPolicy {
	return iam.OperationPolicy{
		Permission:     iam.Permission(desc.Authorization.Permission),
		Resource:       iam.ResourceKind(desc.Audit.Resource),
		Action:         iam.Action(desc.Audit.Action),
		OrgScope:       iam.OrgScope(runtimeOrgScopeFromContract(desc.Authorization.OrganizationSource)),
		RateLimitClass: iam.RateLimitClass(desc.RateLimitBucket),
		Idempotency:    iam.IdempotencyPolicy(desc.Idempotency.Policy),
		AuditEvent:     iam.AuditEvent(desc.Audit.Event),
		BodyLimitBytes: desc.RequestBodyMaxBytes,
	}
}

func runtimeOrgScopeFromContract(source string) string {
	switch source {
	case "request_subject":
		return string(iam.OrgScopeTokenSubject)
	case "input_member":
		return string(iam.OrgScopePathOrgID)
	default:
		return source
	}
}

func contractProblemStatuses(problems []contractapi.ProblemDescriptor) []int {
	statuses := make([]int, 0, len(problems))
	for _, problem := range problems {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	return compactStatuses(statuses)
}

func internalContractProblemStatuses(problems []internalcontractapi.ProblemDescriptor) []int {
	statuses := make([]int, 0, len(problems))
	for _, problem := range problems {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	return compactStatuses(statuses)
}

func compactStatuses(statuses []int) []int {
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
