package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/verself/auth-middleware"
)

func TestOpenAPIPublicAPIOperationsDeclareIAMPolicy(t *testing.T) {
	api := NewAPI(http.NewServeMux(), "1.0.0", "127.0.0.1:0", nil, nil, nil, PublicAPIConfig{})
	openAPI := api.OpenAPI()

	var checked int
	for path, pathItem := range openAPI.Paths {
		if !strings.HasPrefix(path, "/api/") {
			continue
		}
		for _, op := range operationsForPath(pathItem) {
			if op == nil {
				continue
			}
			checked++

			rawPolicy, ok := op.Extensions["x-verself-iam"].(map[string]any)
			if !ok {
				t.Fatalf("%s %s missing x-verself-iam policy", op.Method, path)
			}
			if rawPolicy["permission"] == "" {
				t.Fatalf("%s %s has empty IAM permission: %#v", op.Method, path, rawPolicy)
			}
			if rawPolicy["org_scope"] != "token_org_id" {
				t.Fatalf("%s %s has unexpected org_scope: %#v", op.Method, path, rawPolicy)
			}
			if len(op.Security) != 1 || len(op.Security[0]["bearerAuth"]) != 0 {
				t.Fatalf("%s %s must require bearerAuth with no OpenAPI scopes: %#v", op.Method, path, op.Security)
			}
			if rawPolicy["idempotency"] == idempotencyHeaderKey &&
				!operationHasRequiredParameter(op, "header", "Idempotency-Key") {
				t.Fatalf("%s %s requires Idempotency-Key but does not declare it in OpenAPI", op.Method, path)
			}
			if rawPolicy["idempotency"] == idempotencyRequestBodyKey &&
				!operationHasRequiredRequestBodyProperty(openAPI, op, "idempotency_key") {
				t.Fatalf("%s %s requires idempotency_key but does not declare it as a required request body field", op.Method, path)
			}
			if operationRequiresBodyBudget(*op) {
				if rawPolicy["request_body_max_bytes"] == nil {
					t.Fatalf("%s %s missing explicit request_body_max_bytes policy: %#v", op.Method, path, rawPolicy)
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("checked no public API operations")
	}
}

func TestBillingProxyErrorRedactsUpstreamDetails(t *testing.T) {
	err := billingProxyError(context.Background(), errors.New("postgres://billing:secret@127.0.0.1:5432/billing: connection refused"))

	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected huma status error, got %T", err)
	}
	if statusErr.GetStatus() != http.StatusBadGateway {
		t.Fatalf("status: got %d want %d", statusErr.GetStatus(), http.StatusBadGateway)
	}

	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal error: %v", marshalErr)
	}
	body := string(payload)
	for _, leaked := range []string{"postgres://", "secret", "127.0.0.1", "connection refused"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("billing proxy error leaked %q in %s", leaked, body)
		}
	}
	if !strings.Contains(body, "billing service unavailable") {
		t.Fatalf("billing proxy error body does not include stable public detail: %s", body)
	}
}

func TestBillingProxyErrorMapsNoStripeCustomer(t *testing.T) {
	err := billingPortalProxyResponseError(context.Background(), http.StatusUnprocessableEntity, []byte(`{"type":"urn:verself:problem:billing:no-stripe-customer","detail":"no stripe customer linked to this org","status":422}`))

	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected huma status error, got %T", err)
	}
	if statusErr.GetStatus() != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d want %d", statusErr.GetStatus(), http.StatusUnprocessableEntity)
	}

	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal error: %v", marshalErr)
	}
	body := string(payload)
	if !strings.Contains(body, "billing portal requires an existing Stripe customer") {
		t.Fatalf("billing proxy error body does not include stable public detail: %s", body)
	}
	if strings.Contains(body, "billing-client:") {
		t.Fatalf("billing proxy error leaked upstream sentinel in %s", body)
	}
}

func TestIdentityPermissionChecksRoleBundlesAndDirectScopes(t *testing.T) {
	owner := sandboxServiceToken("42", roleOwner)
	if !identityHasPermission(owner, permissionBillingCheckout) {
		t.Fatal("owner should be allowed to create billing checkout")
	}

	admin := sandboxServiceToken("42", roleAdmin)
	if !identityHasPermission(admin, permissionBillingCheckout) {
		t.Fatal("admin should be allowed to create billing checkout")
	}

	member := sandboxServiceToken("42", roleMember)
	if !identityHasPermission(member, permissionScheduleWrite) {
		t.Fatal("member should be allowed to manage execution schedules")
	}
	if identityHasPermission(member, permissionBillingCheckout) {
		t.Fatal("member should not be allowed to create billing checkout")
	}

	unmarkedScope := &auth.Identity{
		OrgID: "42",
		Raw: map[string]any{
			"scope": "openid sandbox:logs:read",
		},
	}
	if identityHasPermission(unmarkedScope, permissionLogsRead) {
		t.Fatal("plain OAuth scope should not grant operation permissions without an API credential marker")
	}

	scopedClient := &auth.Identity{
		OrgID:   "42",
		Subject: "credential-1",
		Raw: map[string]any{
			"verself:credential_id": "credential-1",
			"permissions":               []string{"sandbox:logs:read"},
		},
	}
	if !identityHasPermission(scopedClient, permissionLogsRead) {
		t.Fatal("API credential permissions claim should grant matching operation permission")
	}
	if identityHasPermission(scopedClient, permissionScheduleWrite) {
		t.Fatal("API credential permissions claim should not grant unrelated permissions")
	}
}

func sandboxServiceToken(orgID string, roles ...string) *auth.Identity {
	assignments := make([]auth.RoleAssignment, 0, len(roles))
	for _, role := range roles {
		assignments = append(assignments, auth.RoleAssignment{
			OrganizationID: orgID,
			Role:           role,
		})
	}
	return &auth.Identity{
		Subject:         "user-1",
		OrgID:           orgID,
		RoleAssignments: assignments,
	}
}

func TestEnforceOperationPolicyDeniesMissingPermission(t *testing.T) {
	ctx := auth.WithIdentity(context.Background(), &auth.Identity{
		Subject: "user-123",
		OrgID:   "42",
		RoleAssignments: []auth.RoleAssignment{{
			OrganizationID: "42",
			Role:           roleMember,
		}},
	})

	identity, err := enforceOperationPolicy(ctx, operationPolicy{
		Permission: permissionBillingCheckout,
	}, &EmptyInput{})
	if identity == nil || identity.Subject != "user-123" {
		t.Fatalf("expected denied operation to retain identity, got %#v", identity)
	}
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusForbidden {
		t.Fatalf("expected forbidden missing-permission error, got %#v", err)
	}
}

func TestBillingReturnURLValidationRequiresAllowedOrigin(t *testing.T) {
	origins, err := ParseBillingReturnOrigins("https://console.example.com, http://127.0.0.1:4244")
	if err != nil {
		t.Fatalf("parse origins: %v", err)
	}

	if err := validateBillingReturnURLs(context.Background(), origins,
		billingReturnURLField{Name: "success_url", URL: "https://console.example.com/billing?purchased=true"},
		billingReturnURLField{Name: "cancel_url", URL: "http://127.0.0.1:4244/billing/credits"},
	); err != nil {
		t.Fatalf("valid return URLs rejected: %v", err)
	}

	err = validateBillingReturnURLs(context.Background(), origins,
		billingReturnURLField{Name: "success_url", URL: "https://evil.example.com/billing"},
	)
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusBadRequest {
		t.Fatalf("expected bad request for unregistered origin, got %#v", err)
	}
}

func TestParseBillingReturnOriginsRejectsRedirectURL(t *testing.T) {
	if _, err := ParseBillingReturnOrigins("https://console.example.com/callback"); err == nil {
		t.Fatal("expected origin parser to reject URL with path")
	}
}

func TestOperationPolicyRequiresDeclaredIdempotency(t *testing.T) {
	tests := []struct {
		name   string
		policy operationPolicy
		input  any
		ctx    context.Context
	}{
		{
			name:   "schedule body key",
			policy: operationPolicy{Idempotency: idempotencyRequestBodyKey},
			input:  &CreateExecutionScheduleInput{},
			ctx:    context.Background(),
		},
		{
			name:   "github install header key",
			policy: operationPolicy{Idempotency: idempotencyHeaderKey},
			input:  &EmptyInput{},
			ctx:    context.Background(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := requireOperationIdempotency(tc.ctx, tc.policy, tc.input)
			var statusErr huma.StatusError
			if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusBadRequest {
				t.Fatalf("expected bad request idempotency error, got %#v", err)
			}
		})
	}
}

func TestFixedWindowOperationRateLimiter(t *testing.T) {
	limiter := newFixedWindowOperationRateLimiter(map[string]rateLimitRule{
		"execution_schedule_mutation": {Limit: 2, Window: time.Minute},
	})
	now := time.Unix(1700000000, 0)
	if decision := limiter.allow("execution_schedule_mutation", "org:subject:ip", now); !decision.Allowed {
		t.Fatalf("first request should be allowed: %#v", decision)
	}
	if decision := limiter.allow("execution_schedule_mutation", "org:subject:ip", now.Add(time.Second)); !decision.Allowed {
		t.Fatalf("second request should be allowed: %#v", decision)
	}
	if decision := limiter.allow("execution_schedule_mutation", "org:subject:ip", now.Add(2*time.Second)); decision.Allowed || decision.RetryAfter <= 0 {
		t.Fatalf("third request should be throttled with retry_after: %#v", decision)
	}
	if decision := limiter.allow("execution_schedule_mutation", "org:subject:ip", now.Add(time.Minute)); !decision.Allowed {
		t.Fatalf("next window should be allowed: %#v", decision)
	}
}

func operationsForPath(pathItem *huma.PathItem) []*huma.Operation {
	return []*huma.Operation{
		pathItem.Get,
		pathItem.Post,
		pathItem.Put,
		pathItem.Patch,
		pathItem.Delete,
		pathItem.Head,
		pathItem.Options,
		pathItem.Trace,
	}
}

func operationHasRequiredParameter(op *huma.Operation, in, name string) bool {
	if op == nil {
		return false
	}
	for _, param := range op.Parameters {
		if param == nil {
			continue
		}
		if param.In == in && param.Name == name && param.Required {
			return true
		}
	}
	return false
}

func operationHasRequiredRequestBodyProperty(openAPI *huma.OpenAPI, op *huma.Operation, name string) bool {
	if openAPI == nil || op == nil || op.RequestBody == nil {
		return false
	}
	mediaType := op.RequestBody.Content["application/json"]
	if mediaType == nil || mediaType.Schema == nil {
		return false
	}
	schema := resolveOpenAPISchema(openAPI, mediaType.Schema)
	if schema == nil {
		return false
	}
	for _, required := range schema.Required {
		if required == name {
			return true
		}
	}
	return false
}

func resolveOpenAPISchema(openAPI *huma.OpenAPI, schema *huma.Schema) *huma.Schema {
	if schema == nil || schema.Ref == "" {
		return schema
	}
	if openAPI.Components == nil || openAPI.Components.Schemas == nil {
		return nil
	}
	return openAPI.Components.Schemas.SchemaFromRef(schema.Ref)
}
