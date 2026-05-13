package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/mailbox-service/internal/contractapi"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func registerMailRoute[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, op huma.Operation, policy runtimeiam.OperationPolicy, handler func(context.Context, *I) (*O, error)) {
	if op.OperationID == "" {
		panic("missing operation ID for mailbox API route")
	}
	if err := policy.ValidateHTTPOperation(op.Method, op.OperationID); err != nil {
		panic(err)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Security = []map[string][]string{{"bearerAuth": {}}}
	op.Extensions["x-verself-iam"] = policy.OpenAPIExtension()
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		if err := enforceOperationPolicy(ctx, authorizer, policy); err != nil {
			return nil, err
		}
		return handler(ctx, input)
	})
}

func mailboxContract(desc contractapi.OperationDescriptor, summary string) (huma.Operation, runtimeiam.OperationPolicy) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        contractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": contractExtension(desc)},
	}
	return op, operationPolicyFromContract(desc)
}

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy runtimeiam.OperationPolicy) error {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return huma.Error401Unauthorized("missing identity")
	}
	if authorizer == nil {
		return huma.Error503ServiceUnavailable("IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, policy)
	if err != nil {
		return huma.Error503ServiceUnavailable("IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return huma.Error403Forbidden("missing required mailbox permission")
	}
	return nil
}
