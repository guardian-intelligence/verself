package billingapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
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
			if rawPolicy["idempotency"] == string(idempotencyHeaderKey) && !operationHasRequiredParameter(op, "header", "Idempotency-Key") {
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

func TestBillingEnforceOperationPolicyAllowsIAMDecision(t *testing.T) {
	ctx := auth.WithIdentity(context.Background(), &auth.Identity{Subject: "user-123", OrgID: "org_42"})

	orgID, err := enforceOperationPolicy(ctx, fakeAuthorizer{string(permissionBillingCheckout): true}, runtimeiam.OperationPolicy{Permission: permissionBillingCheckout})
	if err != nil {
		t.Fatalf("expected IAM allow decision, got %v", err)
	}
	if orgID != "org_42" {
		t.Fatalf("org id = %q, want org_42", orgID)
	}
}

func TestBillingEnforceOperationPolicyDeniesMissingPermission(t *testing.T) {
	ctx := auth.WithIdentity(context.Background(), &auth.Identity{
		Subject: "user-123",
		OrgID:   "org_42",
	})

	orgID, err := enforceOperationPolicy(ctx, fakeAuthorizer{}, runtimeiam.OperationPolicy{Permission: permissionBillingCheckout})
	if orgID == "" {
		t.Fatalf("expected denied operation to retain org id")
	}
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusForbidden {
		t.Fatalf("expected forbidden missing-permission error, got %#v", err)
	}
}

type fakeAuthorizer map[string]bool

func (f fakeAuthorizer) AuthorizeOperation(_ context.Context, _ *auth.Identity, policy runtimeiam.OperationPolicy) (runtimeiam.AuthorizationDecision, error) {
	permission := string(policy.Permission)
	return runtimeiam.AuthorizationDecision{
		Allowed:     f[permission],
		Permission:  policy.Permission,
		Resource:    policy.Resource,
		Action:      policy.Action,
		OrgScope:    policy.OrgScope,
		Permissions: []runtimeiam.Permission{policy.Permission},
	}, nil
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
