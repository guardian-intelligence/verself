package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/verself/service-runtime/auth"
)

func TestOpenAPIPublicAPIOperationsDeclareIAMPolicy(t *testing.T) {
	api := NewAPI(http.NewServeMux(), "1.0.0", "127.0.0.1:0", nil, nil, PublicAPIConfig{})
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

func TestIdentityPermissionChecksRoleBundlesAndDirectScopes(t *testing.T) {
	owner := sandboxServiceToken("42", roleOwner)
	if !identityHasPermission(owner, permissionStickyDiskWrite) {
		t.Fatal("owner should be allowed to reset sticky disks")
	}

	admin := sandboxServiceToken("42", roleAdmin)
	if !identityHasPermission(admin, permissionGitHubWrite) {
		t.Fatal("admin should be allowed to manage GitHub installation")
	}

	member := sandboxServiceToken("42", roleMember)
	if !identityHasPermission(member, permissionScheduleWrite) {
		t.Fatal("member should be allowed to manage execution schedules")
	}
	if identityHasPermission(member, permissionGitHubWrite) {
		t.Fatal("member should not be allowed to manage GitHub installation")
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
			"permissions":           []string{"sandbox:logs:read"},
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
		Permission: permissionGitHubWrite,
	}, &EmptyInput{})
	if identity == nil || identity.Subject != "user-123" {
		t.Fatalf("expected denied operation to retain identity, got %#v", identity)
	}
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusForbidden {
		t.Fatalf("expected forbidden missing-permission error, got %#v", err)
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
