package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type operationPolicy struct {
	Permission     string
	Resource       string
	Action         string
	OrgScope       string
	RateLimitClass string
	AuditEvent     string
}

func registerMailRoute[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, op huma.Operation, policy operationPolicy, handler func(context.Context, *I) (*O, error)) {
	if op.OperationID == "" {
		panic("missing operation ID for mailbox API route")
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Security = []map[string][]string{{"bearerAuth": {}}}
	op.Extensions["x-verself-iam"] = map[string]any{
		"permission":       policy.Permission,
		"resource":         policy.Resource,
		"action":           policy.Action,
		"org_scope":        policy.OrgScope,
		"rate_limit_class": policy.RateLimitClass,
		"audit_event":      policy.AuditEvent,
	}
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		if err := enforceOperationPolicy(ctx, authorizer, policy); err != nil {
			return nil, err
		}
		return handler(ctx, input)
	})
}

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy operationPolicy) error {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return huma.Error401Unauthorized("missing identity")
	}
	if authorizer == nil {
		return huma.Error503ServiceUnavailable("IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, policy.Permission)
	if err != nil {
		return huma.Error503ServiceUnavailable("IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return huma.Error403Forbidden("missing required mailbox permission")
	}
	return nil
}

func mailReadPolicy(operationID, resource, action string) operationPolicy {
	return operationPolicy{
		Permission:     "mailbox:mail:read",
		Resource:       resource,
		Action:         action,
		OrgScope:       "token_subject",
		RateLimitClass: "read",
		AuditEvent:     "mailbox." + operationID,
	}
}

func mailWritePolicy(operationID, resource, action string) operationPolicy {
	return operationPolicy{
		Permission:     "mailbox:mail:write",
		Resource:       resource,
		Action:         action,
		OrgScope:       "token_subject",
		RateLimitClass: "mailbox_mutation",
		AuditEvent:     "mailbox." + operationID,
	}
}
