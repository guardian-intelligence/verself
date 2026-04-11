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

	"github.com/forge-metal/apiwire"
	auth "github.com/forge-metal/auth-middleware"
)

func TestOpenAPIPublicAPIOperationsDeclareIAMPolicy(t *testing.T) {
	api := NewAPI(http.NewServeMux(), "1.0.0", "127.0.0.1:0", nil, nil)

	var checked int
	for path, pathItem := range api.OpenAPI().Paths {
		if !strings.HasPrefix(path, "/api/") {
			continue
		}
		for _, op := range operationsForPath(pathItem) {
			if op == nil {
				continue
			}
			checked++

			rawPolicy, ok := op.Extensions["x-forge-metal-iam"].(map[string]any)
			if !ok {
				t.Fatalf("%s %s missing x-forge-metal-iam policy", op.Method, path)
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
		}
	}

	if checked != len(publicAPIOperationIDs()) {
		t.Fatalf("checked %d public API operations, want %d", checked, len(publicAPIOperationIDs()))
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

func TestIdentityPermissionChecksRoleBundlesAndDirectScopes(t *testing.T) {
	admin := &auth.Identity{
		OrgID: "42",
		RoleAssignments: []auth.RoleAssignment{{
			OrganizationID: "42",
			Role:           roleSandboxOrgAdmin,
		}},
	}
	if !identityHasPermission(admin, permissionBillingCheckout) {
		t.Fatal("sandbox org admin should be allowed to create billing checkout")
	}

	member := &auth.Identity{
		OrgID: "42",
		RoleAssignments: []auth.RoleAssignment{{
			OrganizationID: "42",
			Role:           roleSandboxOrgMember,
		}},
	}
	if !identityHasPermission(member, permissionExecutionSubmit) {
		t.Fatal("sandbox org member should be allowed to submit executions")
	}
	if identityHasPermission(member, permissionBillingCheckout) {
		t.Fatal("sandbox org member should not be allowed to create billing checkout")
	}

	scopedClient := &auth.Identity{
		OrgID: "42",
		Raw: map[string]any{
			"scope": "openid sandbox:logs:read",
		},
	}
	if !identityHasPermission(scopedClient, permissionLogsRead) {
		t.Fatal("direct OAuth scope should grant matching operation permission")
	}
	if identityHasPermission(scopedClient, permissionExecutionSubmit) {
		t.Fatal("direct OAuth scope should not grant unrelated permissions")
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
			name:   "execution body key",
			policy: operationPolicy{Idempotency: idempotencyRequestBodyKey},
			input:  &SubmitExecutionInput{Body: apiwire.SandboxSubmitRequest{}},
			ctx:    context.Background(),
		},
		{
			name:   "repo import provider key",
			policy: operationPolicy{Idempotency: idempotencyProviderRepoID},
			input:  &ImportRepoInput{Body: apiwire.SandboxImportRepoRequest{}},
			ctx:    context.Background(),
		},
		{
			name:   "header idempotency key",
			policy: operationPolicy{Idempotency: idempotencyHeaderKey},
			input:  &RepoIDPath{RepoID: "repo-id"},
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
		"repo_mutation": {Limit: 2, Window: time.Minute},
	})
	now := time.Unix(1700000000, 0)
	if decision := limiter.allow("repo_mutation", "org:subject:ip", now); !decision.Allowed {
		t.Fatalf("first request should be allowed: %#v", decision)
	}
	if decision := limiter.allow("repo_mutation", "org:subject:ip", now.Add(time.Second)); !decision.Allowed {
		t.Fatalf("second request should be allowed: %#v", decision)
	}
	if decision := limiter.allow("repo_mutation", "org:subject:ip", now.Add(2*time.Second)); decision.Allowed || decision.RetryAfter <= 0 {
		t.Fatalf("third request should be throttled with retry_after: %#v", decision)
	}
	if decision := limiter.allow("repo_mutation", "org:subject:ip", now.Add(time.Minute)); !decision.Allowed {
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
