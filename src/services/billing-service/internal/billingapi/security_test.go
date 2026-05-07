package billingapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/verself/service-runtime/auth"
)

func TestOpenAPIPublicBillingOperationsDeclareIAMPolicy(t *testing.T) {
	api := NewAPI(http.NewServeMux(), Config{Version: "2.0.0"})
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
			if rawPolicy["idempotency"] == idempotencyHeaderKey && !operationHasRequiredParameter(op, "header", "Idempotency-Key") {
				t.Fatalf("%s %s requires Idempotency-Key but does not declare it in OpenAPI", op.Method, path)
			}
			if operationRequiresBodyBudget(*op) && rawPolicy["request_body_max_bytes"] == nil {
				t.Fatalf("%s %s missing explicit request_body_max_bytes policy: %#v", op.Method, path, rawPolicy)
			}
		}
	}

	if checked == 0 {
		t.Fatal("checked no public billing API operations")
	}
}

func TestBillingIdentityPermissionChecksRoleBundlesAndDirectScopes(t *testing.T) {
	owner := billingServiceToken("42", roleOwner)
	if !identityHasPermission(owner, permissionBillingCheckout) {
		t.Fatal("owner should be allowed to create billing checkout")
	}

	admin := billingServiceToken("42", roleAdmin)
	if !identityHasPermission(admin, permissionBillingCheckout) {
		t.Fatal("admin should be allowed to create billing checkout")
	}

	member := billingServiceToken("42", roleMember)
	if !identityHasPermission(member, permissionBillingRead) {
		t.Fatal("member should be allowed to read billing")
	}
	if identityHasPermission(member, permissionBillingCheckout) {
		t.Fatal("member should not be allowed to create billing checkout")
	}

	scopedClient := &auth.Identity{
		OrgID:   "42",
		Subject: "credential-1",
		Raw: map[string]any{
			"verself:credential_id": "credential-1",
			"permissions":           []string{"billing:read"},
		},
	}
	if !identityHasPermission(scopedClient, permissionBillingRead) {
		t.Fatal("API credential permissions claim should grant matching billing permission")
	}
	if identityHasPermission(scopedClient, permissionBillingCheckout) {
		t.Fatal("API credential permissions claim should not grant unrelated billing permission")
	}
}

func billingServiceToken(orgID string, roles ...string) *auth.Identity {
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

func TestBillingEnforceOperationPolicyDeniesMissingPermission(t *testing.T) {
	ctx := auth.WithIdentity(context.Background(), &auth.Identity{
		Subject: "user-123",
		OrgID:   "42",
		RoleAssignments: []auth.RoleAssignment{{
			OrganizationID: "42",
			Role:           roleMember,
		}},
	})

	orgID, err := enforceOperationPolicy(ctx, operationPolicy{Permission: permissionBillingCheckout})
	if orgID == 0 {
		t.Fatalf("expected denied operation to retain org id")
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
