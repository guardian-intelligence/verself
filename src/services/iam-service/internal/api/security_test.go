package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/contractapi"
	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/iam-service/internal/internalcontractapi"
	"github.com/verself/iam-service/internal/spicedb"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func TestPublicOpenAPIConformsToContract(t *testing.T) {
	api := NewAPI(http.NewServeMux(), Config{Version: "1.0.0", ListenAddr: "127.0.0.1:0"})
	openAPI := api.OpenAPI()
	contracts := contractOperationsByID(t)

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
			want, ok := contracts[op.OperationID]
			if !ok {
				t.Fatalf("%s %s operation %q is not generated from Smithy", op.Method, path, op.OperationID)
			}
			assertPublicOperationConforms(t, path, op, want)
		}
	}
	if checked != len(contracts) {
		t.Fatalf("checked %d public operations, want %d", checked, len(contracts))
	}
}

func TestInternalOpenAPIConformsToContract(t *testing.T) {
	api := NewInternalAPI(http.NewServeMux(), "1.0.0", "http://127.0.0.1:0", nil)
	openAPI := api.OpenAPI()
	contracts := internalContractOperationsByID(t)

	var checked int
	for path, pathItem := range openAPI.Paths {
		if !strings.HasPrefix(path, "/internal/") {
			continue
		}
		for _, op := range operationsForPath(pathItem) {
			if op == nil {
				continue
			}
			checked++
			want, ok := contracts[op.OperationID]
			if !ok {
				t.Fatalf("%s %s operation %q is not generated from Smithy", op.Method, path, op.OperationID)
			}
			assertInternalOperationConforms(t, path, op, want)
		}
	}
	if checked != len(contracts) {
		t.Fatalf("checked %d internal operations, want %d", checked, len(contracts))
	}
}

func assertPublicOperationConforms(t *testing.T, path string, op *huma.Operation, want contractapi.OperationDescriptor) {
	t.Helper()
	assertOperationTransportConforms(t, path, op, want.Method, want.Path, want.DefaultStatus, want.RequestBodyMaxBytes)
	if len(op.Security) != 1 || len(op.Security[0]) != 1 || len(op.Security[0]["bearerAuth"]) != 0 {
		t.Fatalf("%s %s must require bearerAuth with no OpenAPI scopes: %#v", op.Method, path, op.Security)
	}
	rawContract := operationContractExtension(t, path, op)
	for key, wantValue := range map[string]string{
		"shape_id":            want.ShapeID,
		"operation_id":        want.OperationID,
		"identity":            want.Identity.Mode,
		"audience":            want.Identity.Audience,
		"permission":          want.Authorization.Permission,
		"organization_source": want.Authorization.OrganizationSource,
		"organization_member": want.Authorization.OrganizationMember,
		"audit_event":         want.Audit.Event,
		"resource":            want.Audit.Resource,
		"action":              want.Audit.Action,
		"rate_limit_bucket":   want.RateLimitBucket,
		"idempotency":         want.Idempotency.Policy,
	} {
		if got := rawContract[key]; got != wantValue {
			t.Fatalf("%s %s %s = %#v, want %#v", op.Method, path, key, got, wantValue)
		}
	}
	if got := contractExtensionInt64(rawContract["request_body_max_bytes"]); got != want.RequestBodyMaxBytes {
		t.Fatalf("%s %s request_body_max_bytes = %#v, want %d", op.Method, path, rawContract["request_body_max_bytes"], want.RequestBodyMaxBytes)
	}
	for _, problem := range want.Problems {
		assertProblemResponseConforms(t, path, op, problem.Status)
	}
	if runtimeiam.OperationRequiresBodyBudget(op.Method) {
		if want.Idempotency.Policy != string(idempotencyHeaderKey) {
			t.Fatalf("%s %s mutating generated operation must require header idempotency: %#v", op.Method, path, rawContract)
		}
		if !operationHasRequiredParameter(op, "header", "Idempotency-Key") {
			t.Fatalf("%s %s requires Idempotency-Key but does not declare it", op.Method, path)
		}
	}
}

func assertInternalOperationConforms(t *testing.T, path string, op *huma.Operation, want internalcontractapi.OperationDescriptor) {
	t.Helper()
	assertOperationTransportConforms(t, path, op, want.Method, want.Path, want.DefaultStatus, want.RequestBodyMaxBytes)
	switch want.Identity.Mode {
	case "spiffe_mtls":
		if len(op.Security) != 1 || len(op.Security[0]) != 1 || len(op.Security[0]["mutualTLS"]) != 0 {
			t.Fatalf("%s %s must require mutualTLS with no OpenAPI scopes: %#v", op.Method, path, op.Security)
		}
	case "spiffe_mtls_bearer":
		if len(op.Security) != 1 || len(op.Security[0]) != 2 || len(op.Security[0]["mutualTLS"]) != 0 || len(op.Security[0]["bearerAuth"]) != 0 {
			t.Fatalf("%s %s must require mutualTLS and bearerAuth with no OpenAPI scopes: %#v", op.Method, path, op.Security)
		}
	default:
		t.Fatalf("%s %s has unsupported internal identity mode %q", op.Method, path, want.Identity.Mode)
	}
	rawContract := operationContractExtension(t, path, op)
	for key, wantValue := range map[string]string{
		"shape_id":            want.ShapeID,
		"operation_id":        want.OperationID,
		"identity":            want.Identity.Mode,
		"audience":            want.Identity.Audience,
		"permission":          want.Authorization.Permission,
		"organization_source": want.Authorization.OrganizationSource,
		"organization_member": want.Authorization.OrganizationMember,
		"audit_event":         want.Audit.Event,
		"resource":            want.Audit.Resource,
		"action":              want.Audit.Action,
		"rate_limit_bucket":   want.RateLimitBucket,
		"idempotency":         want.Idempotency.Policy,
	} {
		if got := rawContract[key]; got != wantValue {
			t.Fatalf("%s %s %s = %#v, want %#v", op.Method, path, key, got, wantValue)
		}
	}
	if got := contractExtensionInt64(rawContract["request_body_max_bytes"]); got != want.RequestBodyMaxBytes {
		t.Fatalf("%s %s request_body_max_bytes = %#v, want %d", op.Method, path, rawContract["request_body_max_bytes"], want.RequestBodyMaxBytes)
	}
	for _, problem := range want.Problems {
		assertProblemResponseConforms(t, path, op, problem.Status)
	}
}

func assertOperationTransportConforms(t *testing.T, path string, op *huma.Operation, method string, contractPath string, defaultStatus int, bodyBudget int64) {
	t.Helper()
	if op.Method != method || path != contractPath {
		t.Fatalf("%s %s drifted from generated contract %s %s", op.Method, path, method, contractPath)
	}
	if op.DefaultStatus != defaultStatus {
		t.Fatalf("%s %s default status = %d, want %d", op.Method, path, op.DefaultStatus, defaultStatus)
	}
	response := op.Responses[strconv.Itoa(defaultStatus)]
	if response == nil {
		t.Fatalf("%s %s missing default response %d", op.Method, path, defaultStatus)
	}
	if defaultStatus != http.StatusNoContent {
		assertJSONSchema(t, path, op, "response", response.Content)
	}
	if bodyBudget <= 0 {
		if op.RequestBody != nil {
			t.Fatalf("%s %s declares an unexpected request body", op.Method, path)
		}
		return
	}
	if op.MaxBodyBytes != bodyBudget {
		t.Fatalf("%s %s body limit = %d, want %d", op.Method, path, op.MaxBodyBytes, bodyBudget)
	}
	if op.RequestBody == nil {
		t.Fatalf("%s %s missing request body schema", op.Method, path)
	}
	assertJSONSchema(t, path, op, "request", op.RequestBody.Content)
}

func assertJSONSchema(t *testing.T, path string, op *huma.Operation, direction string, content map[string]*huma.MediaType) {
	t.Helper()
	media := content["application/json"]
	if media == nil {
		t.Fatalf("%s %s missing application/json %s content", op.Method, path, direction)
	}
	if media.Schema == nil {
		t.Fatalf("%s %s missing application/json %s schema", op.Method, path, direction)
	}
	schema := media.Schema
	if schema.Ref == "" && schema.Type == "" && len(schema.Properties) == 0 && len(schema.AllOf) == 0 && len(schema.AnyOf) == 0 && len(schema.OneOf) == 0 {
		t.Fatalf("%s %s has empty application/json %s schema", op.Method, path, direction)
	}
}

func assertProblemResponseConforms(t *testing.T, path string, op *huma.Operation, status int) {
	t.Helper()
	response := op.Responses[strconv.Itoa(status)]
	if response == nil {
		t.Fatalf("%s %s missing problem response %d", op.Method, path, status)
	}
	for _, media := range response.Content {
		if media == nil || media.Schema == nil {
			continue
		}
		schema := media.Schema
		if schema.Ref != "" || schema.Type != "" || len(schema.Properties) > 0 || len(schema.AllOf) > 0 || len(schema.AnyOf) > 0 || len(schema.OneOf) > 0 {
			return
		}
	}
	t.Fatalf("%s %s problem response %d has no schema", op.Method, path, status)
}

func operationContractExtension(t *testing.T, path string, op *huma.Operation) map[string]any {
	t.Helper()
	rawContract, ok := op.Extensions["x-verself-contract"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s missing x-verself-contract metadata", op.Method, path)
	}
	return rawContract
}

func contractExtensionInt64(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func TestGeneratedIAMContractMatchesIdentityCatalog(t *testing.T) {
	catalog := iamServiceOperationCatalog(t)

	seen := map[string]struct{}{}
	for _, operation := range contractapi.Operations {
		want, ok := catalog[operation.OperationID]
		if !ok {
			t.Fatalf("generated operation %q missing from IAM identity catalog", operation.OperationID)
		}
		seen[operation.OperationID] = struct{}{}
		for field, gotWant := range map[string][2]string{
			"permission": {string(want.Permission), operation.Authorization.Permission},
			"resource":   {string(want.Resource), operation.Audit.Resource},
			"action":     {string(want.Action), operation.Audit.Action},
			"org_scope":  {string(want.OrgScope), runtimeOrgScopeFromContract(operation.Authorization.OrganizationSource)},
		} {
			if gotWant[0] != gotWant[1] {
				t.Fatalf("%s %s = %q, want %q", operation.OperationID, field, gotWant[0], gotWant[1])
			}
		}
	}
	for operationID := range catalog {
		if _, ok := seen[operationID]; !ok {
			t.Fatalf("IAM identity catalog operation %q is not generated from Smithy", operationID)
		}
	}
}

func TestIdentityPermissionChecksAuthorizationGraph(t *testing.T) {
	ctx := context.Background()
	authzSvc := authz.New(staticAuthzBackend{
		allowedOrgID:     "42",
		allowedSubjectID: "user-1",
		allowedOrgPerms:  map[string]struct{}{"read": {}, "manage_iam": {}},
	})
	runtime := publicRuntime{authz: authzSvc}
	user := identityServiceToken("42", identity.RoleAdmin)
	if allowed, err := runtime.identityHasContractPermission(ctx, user, contractapi.UpdateOrganization.Descriptor, []string{"42"}); err != nil || !allowed {
		t.Fatalf("graph grant should authorize organization update, allowed=%v err=%v", allowed, err)
	}

	wrongOrg := identityServiceToken("99", identity.RoleAdmin)
	if allowed, err := runtime.identityHasContractPermission(ctx, wrongOrg, contractapi.UpdateOrganization.Descriptor, []string{"99"}); err != nil || allowed {
		t.Fatalf("graph grant for org 42 must not authorize org 99, allowed=%v err=%v", allowed, err)
	}

	credentialWithoutServiceAccount := &auth.Identity{
		Subject: "credential-1",
		OrgID:   "42",
		Raw: map[string]any{
			"verself:credential_id": "credential-1",
		},
	}
	if allowed, err := runtime.identityHasContractPermission(ctx, credentialWithoutServiceAccount, contractapi.GetOrganization.Descriptor, []string{"42"}); !errors.Is(err, authz.ErrInvalid) || allowed {
		t.Fatalf("credential without service_account_id must be invalid, allowed=%v err=%v", allowed, err)
	}
}

func TestInternalAuthorizationResourceRefNormalizesAuditLogOrgID(t *testing.T) {
	resource := internalAuthorizationResourceRef(authz.ResourceRef{
		Type: " audit_log ",
		ID:   " 371564185181576922 ",
	}, "371564185181576922", "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y")
	if resource.Type != "audit_log" || resource.ID != "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y" {
		t.Fatalf("resource = %#v, want canonical audit_log public org ID", resource)
	}

	project := internalAuthorizationResourceRef(authz.ResourceRef{
		Type: "project",
		ID:   "371564185181576922",
	}, "371564185181576922", "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y")
	if project.ID != "371564185181576922" {
		t.Fatalf("project resource ID = %q, want original ID", project.ID)
	}
}

func identityServiceToken(orgID string, roles ...string) *auth.Identity {
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

type staticIdentityStore struct {
	capabilities identity.MemberCapabilitiesDocument
}

type staticAuthzBackend struct {
	allowedOrgID     string
	allowedSubjectID string
	allowedOrgPerms  map[string]struct{}
}

func (b staticAuthzBackend) Check(_ context.Context, resource spicedb.ResourceRef, permission string, subject spicedb.SubjectRef, _ string) (bool, string, error) {
	if resource.Type == "org" && resource.ID == b.allowedOrgID {
		if _, ok := b.allowedOrgPerms[permission]; ok && subject.ID != "" {
			return true, "zed-test", nil
		}
	}
	return false, "zed-test", nil
}

func (b staticAuthzBackend) ReadResourceRelationships(context.Context, spicedb.ResourceRef, map[string]struct{}) ([]spicedb.Relationship, string, error) {
	return nil, "", nil
}

func (b staticAuthzBackend) ReplaceResourceRelationships(context.Context, []spicedb.Relationship, []spicedb.Relationship, map[string]any) (string, error) {
	return "", nil
}

func (s staticIdentityStore) GetOrganizationProfile(context.Context, string, string) (identity.OrganizationProfile, error) {
	return identity.OrganizationProfile{OrgID: "42", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: identity.OrganizationProfileStateActive, Version: 1}, nil
}

func (s staticIdentityStore) ListOrganizationMetadataByOrgIDs(_ context.Context, orgIDs []string) ([]identity.OrganizationMetadata, error) {
	out := make([]identity.OrganizationMetadata, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		out = append(out, identity.OrganizationMetadata{OrgID: orgID, IdentityProviderOrgID: orgID, DisplayName: "Acme", Slug: "acme", Version: 1, OrgACLVersion: 1})
	}
	return out, nil
}

func (s staticIdentityStore) ListOrganizationMetadataByProviderOrgIDs(_ context.Context, providerOrgIDs []string) ([]identity.OrganizationMetadata, error) {
	out := make([]identity.OrganizationMetadata, 0, len(providerOrgIDs))
	for _, providerOrgID := range providerOrgIDs {
		out = append(out, identity.OrganizationMetadata{OrgID: providerOrgID, IdentityProviderOrgID: providerOrgID, DisplayName: "Acme", Slug: "acme", Version: 1, OrgACLVersion: 1})
	}
	return out, nil
}

func (s staticIdentityStore) UpdateOrganizationProfile(context.Context, identity.Principal, identity.UpdateOrganizationRequest) (identity.OrganizationProfile, error) {
	return identity.OrganizationProfile{OrgID: "42", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: identity.OrganizationProfileStateActive, Version: 2}, nil
}

func (s staticIdentityStore) ResolveOrganizationProfile(context.Context, identity.ResolveOrganizationRequest) (identity.OrganizationProfile, error) {
	return identity.OrganizationProfile{OrgID: "42", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: identity.OrganizationProfileStateActive, Version: 1}, nil
}

func (s staticIdentityStore) GetMemberCapabilities(context.Context, string, string) (identity.MemberCapabilitiesDocument, error) {
	return s.capabilities, nil
}

func (s staticIdentityStore) PutMemberCapabilities(context.Context, identity.MemberCapabilitiesDocument) (identity.MemberCapabilitiesDocument, error) {
	return s.capabilities, nil
}

func (s staticIdentityStore) GetOrgACLState(context.Context, string, string) (identity.OrgACLState, error) {
	return identity.OrgACLState{Version: 1}, nil
}

func (s staticIdentityStore) UpdateMemberRolesCommand(context.Context, identity.UpdateMemberRolesCommand, identity.Directory, string) (identity.UpdateMemberRolesResult, error) {
	return identity.UpdateMemberRolesResult{}, nil
}

func (s staticIdentityStore) CreateServiceAccount(context.Context, identity.ServiceAccount, identity.APICredential, identity.APICredentialSecret) (identity.ServiceAccount, identity.APICredential, error) {
	return identity.ServiceAccount{}, identity.APICredential{}, nil
}

func (s staticIdentityStore) ListServiceAccounts(context.Context, string) ([]identity.ServiceAccount, error) {
	return []identity.ServiceAccount{}, nil
}

func (s staticIdentityStore) GetServiceAccount(context.Context, string, string) (identity.ServiceAccount, error) {
	return identity.ServiceAccount{}, identity.ErrAPICredentialMissing
}

func (s staticIdentityStore) DisableServiceAccount(context.Context, string, string, string, time.Time) (identity.ServiceAccount, []identity.APICredential, error) {
	return identity.ServiceAccount{}, nil, nil
}

func (s staticIdentityStore) CreateAPICredential(context.Context, identity.APICredential, identity.APICredentialSecret) (identity.APICredential, error) {
	return identity.APICredential{}, nil
}

func (s staticIdentityStore) ListAPICredentials(context.Context, string) ([]identity.APICredential, error) {
	return []identity.APICredential{}, nil
}

func (s staticIdentityStore) GetAPICredential(context.Context, string, string) (identity.APICredential, error) {
	return identity.APICredential{}, identity.ErrAPICredentialMissing
}

func (s staticIdentityStore) ActiveAPICredentialSecrets(context.Context, string, string) ([]identity.APICredentialSecret, error) {
	return []identity.APICredentialSecret{}, nil
}

func (s staticIdentityStore) AddAPICredentialSecret(context.Context, string, string, string, identity.APICredentialSecret) (identity.APICredential, error) {
	return identity.APICredential{}, nil
}

func (s staticIdentityStore) RevokeAPICredential(context.Context, string, string, string, time.Time) (identity.APICredential, error) {
	return identity.APICredential{}, nil
}

func (s staticIdentityStore) ResolveAPICredentialClaims(context.Context, string, time.Time) (identity.ResolveAPICredentialClaimsResult, error) {
	return identity.ResolveAPICredentialClaimsResult{}, identity.ErrAPICredentialMissing
}

func TestOperationPolicyRequiresIdempotencyHeader(t *testing.T) {
	err := requireContractIdempotency(context.Background(), contractapi.UpdateOrganization.Descriptor)
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusBadRequest {
		t.Fatalf("expected bad request for missing idempotency key, got %#v", err)
	}

	ctx := context.WithValue(context.Background(), operationRequestInfoKey{}, operationRequestInfo{IdempotencyKey: "key-1"})
	if err := requireContractIdempotency(ctx, contractapi.UpdateOrganization.Descriptor); err != nil {
		t.Fatalf("valid idempotency key rejected: %v", err)
	}
}

func TestRoleAssignmentOrgIDsNormalizeProviderIDs(t *testing.T) {
	ctx := context.Background()
	identity := &auth.Identity{RoleAssignments: []auth.RoleAssignment{
		{OrganizationID: "42", Role: "owner"},
		{OrganizationID: "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y", Role: "member"},
		{OrganizationID: "42", Role: "admin"},
		{OrganizationID: "", Role: "member"},
	}}
	orgIDs, err := roleAssignmentOrgIDs(ctx, identity)
	if err != nil {
		t.Fatalf("role assignments rejected: %v", err)
	}
	if got := strings.Join(orgIDs, ","); got != "42,org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y" {
		t.Fatalf("org IDs = %s, want sorted unique provider IDs", got)
	}

	_, err = roleAssignmentOrgIDs(ctx, &auth.Identity{})
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusForbidden {
		t.Fatalf("missing assignments should be 403, got %#v", err)
	}
}

func TestAuditOperationWritesStructuredLogForUserAndServiceAccount(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
		if attr.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return attr
	}})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ctx := context.WithValue(context.Background(), operationRequestInfoKey{}, operationRequestInfo{IdempotencyKey: "secret-retry-key"})
	operation := contractapi.UpdateMemberRole.Descriptor
	policy := operationPolicyFromContract(operation)
	input := contractapi.UpdateMemberRoleInput{MemberID: "member_00000000000000000000000000"}
	auditOperation(ctx, huma.Operation{OperationID: operation.OperationID}, policy, &auth.Identity{
		Subject: "user-1",
		OrgID:   "42",
	}, input, nil, "denied", forbidden(ctx, "permission-denied", "missing required permission"))
	auditOperation(ctx, huma.Operation{OperationID: operation.OperationID}, policy, &auth.Identity{
		Subject: "credential-subject",
		OrgID:   "42",
		Raw: map[string]any{
			"verself:credential_id":      "credential-1",
			"verself:service_account_id": "service-account-1",
		},
	}, input, nil, "allowed", nil)

	body := logs.String()
	for _, want := range []string{
		"msg=\"identity api operation\"",
		"audit_event=iam.member.update_role",
		"operation_id=update-member-role",
		"operation_permission=iam:member:update_role",
		"operation_resource=member",
		"operation_action=update",
		"rate_limit_class=iam_mutation",
		"outcome=denied",
		"outcome=allowed",
		"subject=user-1",
		"subject=credential-subject",
		"org_id=42",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("audit log missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "secret-retry-key") {
		t.Fatalf("audit log leaked raw idempotency key:\n%s", body)
	}
}

func TestProblemRedactsInternalCause(t *testing.T) {
	err := upstreamFailure(context.Background(), "zitadel-unavailable", "identity provider unavailable", errors.New("Bearer secret http://127.0.0.1:8085 exploded"))
	payload, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal problem: %v", marshalErr)
	}
	body := string(payload)
	for _, leaked := range []string{"Bearer secret", "127.0.0.1", "exploded"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("problem leaked %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, "identity provider unavailable") {
		t.Fatalf("problem missing stable detail: %s", body)
	}
}

func TestFixedWindowOperationRateLimiter(t *testing.T) {
	limiter := newFixedWindowOperationRateLimiter(map[runtimeiam.RateLimitClass]rateLimitRule{
		rateLimitIAMMutation: {Limit: 2, Window: time.Minute},
	})
	now := time.Unix(1700000000, 0)
	if decision := limiter.allow(rateLimitIAMMutation, "org:subject:ip", now); !decision.Allowed {
		t.Fatalf("first request should be allowed: %#v", decision)
	}
	if decision := limiter.allow(rateLimitIAMMutation, "org:subject:ip", now.Add(time.Second)); !decision.Allowed {
		t.Fatalf("second request should be allowed: %#v", decision)
	}
	if decision := limiter.allow(rateLimitIAMMutation, "org:subject:ip", now.Add(2*time.Second)); decision.Allowed || decision.RetryAfter <= 0 {
		t.Fatalf("third request should be throttled: %#v", decision)
	}
}

func iamServiceOperationCatalog(t *testing.T) map[string]identity.Operation {
	t.Helper()
	out := map[string]identity.Operation{}
	for _, service := range identity.DefaultOperations().Services {
		if service.Service != "iam-service" {
			continue
		}
		for _, operation := range service.Operations {
			if _, duplicate := out[operation.OperationID]; duplicate {
				t.Fatalf("duplicate IAM catalog operation %q", operation.OperationID)
			}
			out[operation.OperationID] = operation
		}
	}
	if len(out) == 0 {
		t.Fatal("IAM service operation catalog is empty")
	}
	return out
}

func contractOperationsByID(t *testing.T) map[string]contractapi.OperationDescriptor {
	t.Helper()
	out := map[string]contractapi.OperationDescriptor{}
	for _, operation := range contractapi.Operations {
		if _, duplicate := out[operation.OperationID]; duplicate {
			t.Fatalf("duplicate generated operation %q", operation.OperationID)
		}
		out[operation.OperationID] = operation
	}
	if len(out) == 0 {
		t.Fatal("generated IAM contract is empty")
	}
	return out
}

func internalContractOperationsByID(t *testing.T) map[string]internalcontractapi.OperationDescriptor {
	t.Helper()
	out := map[string]internalcontractapi.OperationDescriptor{}
	for _, operation := range internalcontractapi.Operations {
		if _, duplicate := out[operation.OperationID]; duplicate {
			t.Fatalf("duplicate generated internal operation %q", operation.OperationID)
		}
		out[operation.OperationID] = operation
	}
	if len(out) == 0 {
		t.Fatal("generated IAM internal contract is empty")
	}
	return out
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
