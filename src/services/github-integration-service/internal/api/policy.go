package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/verself/github-integration-service/internal/githubintegration"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
	runtimecatalog "github.com/verself/service-runtime/routecatalog"
)

const maxIdempotencyKeyLength = 128

type requestInfoKey struct{}

type requestInfo struct {
	ClientIP       string
	UserAgent      string
	IdempotencyKey string
}

func registerSecured[I, O any](api huma.API, svc *githubintegration.Service, authorizer runtimeiam.OperationAuthorizer, desc runtimecatalog.Operation, summary string, handler func(context.Context, githubintegration.Principal, *I) (*O, error)) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        desc.ProblemStatuses(),
		Extensions: map[string]any{
			"x-verself-contract": desc,
		},
		Security: []map[string][]string{{"bearerAuth": {}}},
	}
	policy := desc.OperationPolicy()
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	if policy.Idempotency == runtimeiam.IdempotencyHeaderKey {
		op.Parameters = appendIdempotencyHeader(op.Parameters)
	}
	op.Extensions["x-verself-iam"] = policy.OpenAPIExtension()
	op.Middlewares = append(op.Middlewares, captureRequestInfo)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		principal, err := enforce(ctx, authorizer, policy, input)
		if err != nil {
			return nil, err
		}
		out, err := runIdempotent(ctx, svc, principal, desc.OperationID, policy, input, handler)
		if err != nil {
			return nil, problemFromError(err)
		}
		return out, nil
	})
}

func runIdempotent[I, O any](ctx context.Context, svc *githubintegration.Service, principal githubintegration.Principal, operationID string, policy runtimeiam.OperationPolicy, input *I, handler func(context.Context, githubintegration.Principal, *I) (*O, error)) (*O, error) {
	if policy.Idempotency == runtimeiam.IdempotencyNone || policy.Idempotency == "" {
		return handler(ctx, principal, input)
	}
	requestHash, err := idempotencyRequestHash(input)
	if err != nil {
		return nil, err
	}
	lock, payload, replay, err := svc.BeginIdempotency(ctx, principal, operationID, idempotencyKey(ctx, policy, input), requestHash)
	if err != nil {
		return nil, err
	}
	if replay {
		var out O
		if err := json.Unmarshal(payload, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	out, err := handler(ctx, principal, input)
	if err != nil {
		lock.Rollback(ctx)
		return nil, err
	}
	resultPayload, err := json.Marshal(out)
	if err != nil {
		lock.Rollback(ctx)
		return nil, err
	}
	if err := lock.Commit(ctx, resultPayload); err != nil {
		return nil, err
	}
	return out, nil
}

func enforce(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy runtimeiam.OperationPolicy, input any) (githubintegration.Principal, error) {
	identity := auth.FromContext(ctx)
	if identity == nil || strings.TrimSpace(identity.Subject) == "" {
		return githubintegration.Principal{}, huma.Error401Unauthorized("missing authenticated identity")
	}
	if strings.TrimSpace(identity.OrgID) == "" {
		return githubintegration.Principal{}, huma.Error403Forbidden("authenticated identity is missing organization scope")
	}
	if authorizer == nil {
		return githubintegration.Principal{}, huma.Error503ServiceUnavailable("IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, policy)
	if err != nil {
		return githubintegration.Principal{}, huma.Error503ServiceUnavailable("IAM authorization failed", err)
	}
	if !decision.Allowed {
		return githubintegration.Principal{}, huma.Error403Forbidden(fmt.Sprintf("missing required permission %q", policy.Permission))
	}
	if err := requireIdempotency(ctx, policy, input); err != nil {
		return githubintegration.Principal{}, err
	}
	if decision := apiOperationRateLimiter.Allow(policy.RateLimitClass, rateLimitKey(ctx, identity, policy), time.Now()); !decision.Allowed {
		var err error = huma.Error429TooManyRequests("rate limit exceeded")
		if decision.RetryAfter > 0 {
			headers := http.Header{}
			headers.Set("Retry-After", strconv.FormatInt(int64(decision.RetryAfter.Seconds()), 10))
			err = huma.ErrorWithHeaders(err, headers)
		}
		return githubintegration.Principal{}, err
	}
	return githubintegration.Principal{
		OrgID:   strings.TrimSpace(identity.OrgID),
		ActorID: strings.TrimSpace(identity.Subject),
		Email:   strings.TrimSpace(identity.Email),
	}, nil
}

func captureRequestInfo(ctx huma.Context, next func(huma.Context)) {
	info := requestInfo{
		ClientIP:       strings.TrimSpace(ctx.RemoteAddr()),
		UserAgent:      strings.TrimSpace(ctx.Header("User-Agent")),
		IdempotencyKey: strings.TrimSpace(ctx.Header("Idempotency-Key")),
	}
	next(huma.WithContext(ctx, context.WithValue(ctx.Context(), requestInfoKey{}, info)))
}

func requireIdempotency(ctx context.Context, policy runtimeiam.OperationPolicy, input any) error {
	switch policy.Idempotency {
	case "":
		return nil
	case runtimeiam.IdempotencyRequestBodyKey:
		return validateIdempotency("idempotency_key", idempotencyKey(ctx, policy, input))
	case runtimeiam.IdempotencyHeaderKey:
		return validateIdempotency("Idempotency-Key", idempotencyKey(ctx, policy, input))
	default:
		return huma.Error400BadRequest("unsupported idempotency policy")
	}
}

func idempotencyKey(ctx context.Context, policy runtimeiam.OperationPolicy, input any) string {
	switch policy.Idempotency {
	case runtimeiam.IdempotencyRequestBodyKey:
		return nestedStringField(input, "Body", "IdempotencyKey")
	case runtimeiam.IdempotencyHeaderKey:
		return infoFromContext(ctx).IdempotencyKey
	default:
		return ""
	}
}

func validateIdempotency(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return huma.Error400BadRequest(field + " is required")
	}
	if len(value) > maxIdempotencyKeyLength || strings.ContainsAny(value, "\x00\r\n\t") {
		return huma.Error400BadRequest(field + " is invalid")
	}
	return nil
}

func nestedStringField(value any, path ...string) string {
	current := reflectValue(value)
	for _, name := range path {
		if !current.IsValid() || current.Kind() != reflect.Struct {
			return ""
		}
		field := current.FieldByName(name)
		if !field.IsValid() || !field.CanInterface() {
			return ""
		}
		current = reflectValue(field.Interface())
	}
	if !current.IsValid() || current.Kind() != reflect.String {
		return ""
	}
	return current.String()
}

func reflectValue(value any) reflect.Value {
	current := reflect.ValueOf(value)
	for current.IsValid() && (current.Kind() == reflect.Pointer || current.Kind() == reflect.Interface) {
		if current.IsNil() {
			return reflect.Value{}
		}
		current = current.Elem()
	}
	return current
}

func appendIdempotencyHeader(parameters []*huma.Param) []*huma.Param {
	minLength := 8
	maxLength := maxIdempotencyKeyLength
	return append(parameters, &huma.Param{
		Name:     "Idempotency-Key",
		In:       "header",
		Required: true,
		Schema: &huma.Schema{
			Type:      "string",
			MinLength: &minLength,
			MaxLength: &maxLength,
		},
	})
}

func infoFromContext(ctx context.Context) requestInfo {
	info, _ := ctx.Value(requestInfoKey{}).(requestInfo)
	return info
}

func rateLimitKey(ctx context.Context, identity *auth.Identity, policy runtimeiam.OperationPolicy) string {
	info := infoFromContext(ctx)
	return strings.Join([]string{
		string(policy.RateLimitClass),
		string(policy.Permission),
		identity.OrgID,
		identity.Subject,
		info.ClientIP,
	}, "\x00")
}

func idempotencyRequestHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func problemFromError(err error) error {
	switch {
	case errors.Is(err, githubintegration.ErrIdempotencyMismatch):
		return huma.Error409Conflict(err.Error(), err)
	case errors.Is(err, githubintegration.ErrInvalidPageToken):
		return huma.Error400BadRequest(err.Error(), err)
	case errors.Is(err, githubintegration.ErrOAuthNotConfigured), errors.Is(err, githubintegration.ErrSetupNotConfigured):
		return huma.Error503ServiceUnavailable(err.Error(), err)
	case errors.Is(err, githubintegration.ErrInvalidSetupState):
		return huma.Error409Conflict(err.Error(), err)
	case errors.Is(err, githubintegration.ErrGitHubUserAuthorization):
		return huma.Error409Conflict(err.Error(), err)
	case errors.Is(err, githubintegration.ErrInstallationAlreadyBound):
		return huma.Error409Conflict(err.Error(), err)
	case errors.Is(err, githubintegration.ErrGitHubInstallationForbidden):
		return huma.Error403Forbidden(err.Error(), err)
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("resource not found", err)
	default:
		return huma.Error500InternalServerError("github integration operation failed", err)
	}
}

var apiOperationRateLimiter = runtimeiam.NewFixedWindowOperationRateLimiter(map[runtimeiam.RateLimitClass]runtimeiam.RateLimitRule{
	"github_read":     {Limit: 6000, Window: time.Minute},
	"github_mutation": {Limit: 300, Window: time.Minute},
})
