package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/verself/service-runtime/auth"
)

func TestSourceOpenAPIContract(t *testing.T) {
	publicAPI := NewAPI(http.NewServeMux(), "1.0.0", "https://source.api.verself.sh", Config{})
	internalAPI := NewInternalAPI(http.NewServeMux(), "1.0.0", "https://127.0.0.1:4262", Config{})

	publicSpec := publicAPI.OpenAPI()
	internalSpec := internalAPI.OpenAPI()

	assertOnlySecuritySchemes(t, publicSpec.Components.SecuritySchemes, "bearerAuth")
	assertOnlySecuritySchemes(t, internalSpec.Components.SecuritySchemes, "mutualTLS")

	var publicOps int
	for path, pathItem := range publicSpec.Paths {
		if !strings.HasPrefix(path, "/api/") {
			t.Fatalf("public Source OpenAPI exposes non-public path %s", path)
		}
		for _, op := range operationsForPath(pathItem) {
			if op == nil {
				continue
			}
			publicOps++
			assertOnlySecurity(t, op, path, "bearerAuth")
			assertSourcePolicy(t, publicSpec, op, path, false)
		}
	}
	if publicOps != 11 {
		t.Fatalf("unexpected Source public operation count: got %d", publicOps)
	}

	var internalOps int
	for path, pathItem := range internalSpec.Paths {
		if !strings.HasPrefix(path, "/internal/") {
			t.Fatalf("internal Source OpenAPI exposes non-internal path %s", path)
		}
		for _, op := range operationsForPath(pathItem) {
			if op == nil {
				continue
			}
			internalOps++
			assertOnlySecurity(t, op, path, "mutualTLS")
			assertSourcePolicy(t, internalSpec, op, path, true)
		}
	}
	if internalOps != 2 {
		t.Fatalf("unexpected Source internal operation count: got %d", internalOps)
	}
}

func TestSourceCheckoutGrantPublicRequestDoesNotExposeUnimplementedPathPrefix(t *testing.T) {
	api := NewAPI(http.NewServeMux(), "1.0.0", "https://source.api.verself.sh", Config{})
	openAPI := api.OpenAPI()
	op := openAPI.Paths["/api/v1/repos/{repo_id}/checkout-grants"].Post
	if op == nil || op.RequestBody == nil {
		t.Fatal("create checkout grant operation missing request body")
	}
	schema := op.RequestBody.Content["application/json"].Schema
	if schema == nil {
		t.Fatal("create checkout grant operation missing JSON schema")
	}
	if schema.Ref != "" {
		refName := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
		schema = openAPI.Components.Schemas.Map()[refName]
	}
	if schema == nil || schema.Properties == nil {
		t.Fatalf("create checkout grant operation has unexpected schema: %#v", schema)
	}
	if _, ok := schema.Properties["path_prefix"]; ok {
		t.Fatal("public checkout grant request must not expose path_prefix before path-scoped extraction exists")
	}
}

func TestSourceIdentityPermissionChecksRoleBundlesAndDirectScopes(t *testing.T) {
	owner := sourceServiceToken("42", roleOwner)
	if !identityHasPermission(owner, permissionWorkflowWrite) {
		t.Fatal("owner should be allowed to dispatch source workflows")
	}

	admin := sourceServiceToken("42", roleAdmin)
	if !identityHasPermission(admin, permissionGitCredentialWrite) {
		t.Fatal("admin should be allowed to create source git credentials")
	}

	member := sourceServiceToken("42", roleMember)
	if !identityHasPermission(member, permissionRepoRead) {
		t.Fatal("member should be allowed to read source repositories")
	}
	if identityHasPermission(member, permissionRepoWrite) {
		t.Fatal("member should not be allowed to write source repositories")
	}

	unmarkedScope := &auth.Identity{
		OrgID: "42",
		Raw: map[string]any{
			"scope": "openid source:repo:write",
		},
	}
	if identityHasPermission(unmarkedScope, permissionRepoWrite) {
		t.Fatal("plain OAuth scope should not grant operation permissions without an API credential marker")
	}

	scopedClient := &auth.Identity{
		OrgID:   "42",
		Subject: "credential-1",
		Raw: map[string]any{
			"verself:credential_id": "credential-1",
			"permissions":           []string{"source:repo:write"},
		},
	}
	if !identityHasPermission(scopedClient, permissionRepoWrite) {
		t.Fatal("API credential permissions claim should grant matching source permission")
	}
	if identityHasPermission(scopedClient, permissionWorkflowWrite) {
		t.Fatal("API credential permissions claim should not grant unrelated source permission")
	}
}

func TestSourceEnforceOperationPolicyDeniesMissingPermission(t *testing.T) {
	ctx := auth.WithIdentity(context.Background(), sourceServiceToken("42", roleMember))

	principal, err := enforceOperationPolicy(ctx, operationPolicy{Permission: permissionRepoWrite})
	if principal.OrgID != 42 {
		t.Fatalf("expected denied operation to retain org id, got %d", principal.OrgID)
	}
	if err == nil {
		t.Fatal("member should not be allowed to create source repositories")
	}
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusForbidden {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}

func sourceServiceToken(orgID string, roles ...string) *auth.Identity {
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

func assertSourcePolicy(t *testing.T, openAPI *huma.OpenAPI, op *huma.Operation, path string, internal bool) {
	t.Helper()
	rawPolicy, ok := op.Extensions["x-verself-iam"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s missing x-verself-iam policy", op.Method, path)
	}
	for _, key := range []string{"permission", "resource", "action", "org_scope", "rate_limit_class", "audit_event", "source_product_area", "operation_display", "operation_type", "event_category", "risk_level", "data_classification"} {
		if rawPolicy[key] == "" {
			t.Fatalf("%s %s empty policy field %q: %#v", op.Method, path, key, rawPolicy)
		}
	}
	if internal {
		if rawPolicy["internal"] != true {
			t.Fatalf("%s %s internal operation missing internal policy marker: %#v", op.Method, path, rawPolicy)
		}
	} else if rawPolicy["org_scope"] != "token_org_id" {
		t.Fatalf("%s %s public operation has unexpected org_scope: %#v", op.Method, path, rawPolicy)
	}
	if rawPolicy["idempotency"] == idempotencyHeaderKey && !operationHasRequiredParameter(op, "header", "Idempotency-Key") {
		t.Fatalf("%s %s requires Idempotency-Key but does not declare it", op.Method, path)
	}
	if operationRequiresBodyBudget(*op) && rawPolicy["request_body_max_bytes"] == nil {
		t.Fatalf("%s %s missing request_body_max_bytes policy: %#v", op.Method, path, rawPolicy)
	}
	if rawPolicy["idempotency"] == "idempotency_key_body" && !operationHasRequiredRequestBodyProperty(openAPI, op, "idempotency_key") {
		t.Fatalf("%s %s requires idempotency_key but does not declare it in the request body", op.Method, path)
	}
}

func assertOnlySecurity(t *testing.T, op *huma.Operation, path, scheme string) {
	t.Helper()
	if len(op.Security) != 1 {
		t.Fatalf("%s %s must declare exactly one OpenAPI security alternative: %#v", op.Method, path, op.Security)
	}
	if _, ok := op.Security[0][scheme]; !ok || len(op.Security[0]) != 1 {
		t.Fatalf("%s %s must require only %s: %#v", op.Method, path, scheme, op.Security)
	}
}

func assertOnlySecuritySchemes(t *testing.T, schemes map[string]*huma.SecurityScheme, expected string) {
	t.Helper()
	if len(schemes) != 1 {
		t.Fatalf("OpenAPI projection must declare exactly one security scheme: %#v", schemes)
	}
	if _, ok := schemes[expected]; !ok {
		t.Fatalf("OpenAPI projection must declare only %s: %#v", expected, schemes)
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
	for _, param := range op.Parameters {
		if param != nil && param.In == in && param.Name == name && param.Required {
			return true
		}
	}
	return false
}

func operationHasRequiredRequestBodyProperty(openAPI *huma.OpenAPI, op *huma.Operation, name string) bool {
	if op.RequestBody == nil {
		return false
	}
	mediaType := op.RequestBody.Content["application/json"]
	if mediaType == nil || mediaType.Schema == nil {
		return false
	}
	schema := mediaType.Schema
	if schema.Ref != "" {
		refName := strings.TrimPrefix(schema.Ref, "#/components/schemas/")
		schema = openAPI.Components.Schemas.Map()[refName]
	}
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
