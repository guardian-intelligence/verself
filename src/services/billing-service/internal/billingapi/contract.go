package billingapi

import (
	"context"
	"sort"

	"github.com/danielgtaylor/huma/v2"
	"github.com/spiffe/go-spiffe/v2/spiffeid"

	"github.com/verself/billing-service/internal/billing"
	"github.com/verself/billing-service/internal/contractapi"
	"github.com/verself/billing-service/internal/internalcontractapi"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func registerPublicBillingContractRoute[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, desc contractapi.OperationDescriptor, summary string, handler func(context.Context, billing.OrgID, *I) (*O, error)) {
	op, policy := publicBillingContract(desc, summary)
	registerPublicBillingRoute(api, authorizer, op, policy, handler)
}

func registerInternalBillingContractRoute[I, O any](api huma.API, peers []spiffeid.ID, desc internalcontractapi.OperationDescriptor, summary string, handler func(context.Context, *I) (*O, error)) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        internalContractProblemStatuses(desc.Problems),
		Extensions: map[string]any{
			"x-verself-contract": internalContractExtension(desc),
			"x-verself-iam":      operationPolicyFromInternalContract(desc).OpenAPIExtension(),
		},
		Security: []map[string][]string{{"mutualTLS": {}}},
	}
	if desc.RequestBodyMaxBytes > 0 {
		op.MaxBodyBytes = desc.RequestBodyMaxBytes
	}
	op.Middlewares = append(op.Middlewares, requireInternalPeerMiddleware(api, peers))
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		return handler(ctx, input)
	})
}

func publicBillingContract(desc contractapi.OperationDescriptor, summary string) (huma.Operation, runtimeiam.OperationPolicy) {
	policy := operationPolicyFromContract(desc)
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        contractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": contractExtension(desc)},
	}
	return op, policy
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
		OrgScope:       runtimeiam.OrgScope(runtimeOrgScopeFromContract(desc.Authorization.OrganizationSource)),
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
		OrgScope:       runtimeiam.OrgScope(runtimeOrgScopeFromContract(desc.Authorization.OrganizationSource)),
		RateLimitClass: runtimeiam.RateLimitClass(desc.RateLimitBucket),
		Idempotency:    runtimeiam.IdempotencyPolicy(desc.Idempotency.Policy),
		AuditEvent:     runtimeiam.AuditEvent(desc.Audit.Event),
		BodyLimitBytes: desc.RequestBodyMaxBytes,
	}
}

func runtimeOrgScopeFromContract(source string) string {
	switch source {
	case "request_subject":
		return string(runtimeiam.OrgScopeTokenSubject)
	case "input_member":
		return string(runtimeiam.OrgScopePathOrgID)
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
