package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/iam-service/internal/spicedb"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func TestOpenAPIPublicAPIOperationsDeclareIAMPolicy(t *testing.T) {
	api := NewAPI(http.NewServeMux(), Config{Version: "1.0.0", ListenAddr: "127.0.0.1:0"})
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
			for _, key := range []string{"permission", "resource", "action", "org_scope", "rate_limit_class", "audit_event"} {
				if rawPolicy[key] == "" {
					t.Fatalf("%s %s empty policy field %q: %#v", op.Method, path, key, rawPolicy)
				}
			}
			if rawPolicy["org_scope"] != "token_org_id" && rawPolicy["org_scope"] != "token_role_assignment_org_ids" {
				t.Fatalf("%s %s unexpected org_scope: %#v", op.Method, path, rawPolicy)
			}
			policy := runtimeiam.OperationPolicy{
				Permission:     runtimeiam.Permission(rawPolicy["permission"].(string)),
				Resource:       runtimeiam.ResourceKind(rawPolicy["resource"].(string)),
				Action:         runtimeiam.Action(rawPolicy["action"].(string)),
				OrgScope:       runtimeiam.OrgScope(rawPolicy["org_scope"].(string)),
				RateLimitClass: runtimeiam.RateLimitClass(rawPolicy["rate_limit_class"].(string)),
				AuditEvent:     runtimeiam.AuditEvent(rawPolicy["audit_event"].(string)),
			}
			if value, _ := rawPolicy["idempotency"].(string); value != "" {
				policy.Idempotency = runtimeiam.IdempotencyPolicy(value)
			}
			switch value := rawPolicy["request_body_max_bytes"].(type) {
			case int64:
				policy.BodyLimitBytes = value
			case int:
				policy.BodyLimitBytes = int64(value)
			case float64:
				policy.BodyLimitBytes = int64(value)
			}
			if err := policy.ValidateHTTPOperation(op.Method, op.OperationID); err != nil {
				t.Fatalf("%s %s has invalid typed operation policy: %v", op.Method, path, err)
			}
			if len(op.Security) != 1 || len(op.Security[0]["bearerAuth"]) != 0 {
				t.Fatalf("%s %s must require bearerAuth with no OpenAPI scopes: %#v", op.Method, path, op.Security)
			}
			if operationRequiresBodyBudget(*op) {
				if rawPolicy["request_body_max_bytes"] == nil {
					t.Fatalf("%s %s missing request_body_max_bytes: %#v", op.Method, path, rawPolicy)
				}
				if rawPolicy["action"] != "read" && rawPolicy["action"] != "list" && rawPolicy["action"] != "test" {
					if rawPolicy["idempotency"] != string(idempotencyHeaderKey) {
						t.Fatalf("%s %s mutating IAM operation must require header idempotency: %#v", op.Method, path, rawPolicy)
					}
					if !operationHasRequiredParameter(op, "header", "Idempotency-Key") {
						t.Fatalf("%s %s requires Idempotency-Key but does not declare it", op.Method, path)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no public API operations")
	}
}

func TestIAMRoutePoliciesMatchIdentityCatalog(t *testing.T) {
	api := NewAPI(http.NewServeMux(), Config{Version: "1.0.0", ListenAddr: "127.0.0.1:0"})
	openAPI := api.OpenAPI()
	catalog := iamServiceOperationCatalog(t)

	seen := map[string]struct{}{}
	for path, pathItem := range openAPI.Paths {
		if !strings.HasPrefix(path, "/api/") {
			continue
		}
		for _, op := range operationsForPath(pathItem) {
			if op == nil {
				continue
			}
			want, ok := catalog[op.OperationID]
			if !ok {
				t.Fatalf("%s %s operation %q missing from IAM identity catalog", op.Method, path, op.OperationID)
			}
			seen[op.OperationID] = struct{}{}
			rawPolicy := op.Extensions["x-verself-iam"].(map[string]any)
			for key, wantValue := range map[string]string{
				"permission": string(want.Permission),
				"resource":   string(want.Resource),
				"action":     string(want.Action),
				"org_scope":  string(want.OrgScope),
			} {
				if got := rawPolicy[key]; got != wantValue {
					t.Fatalf("%s %s %s = %#v, want %#v", op.Method, path, key, got, wantValue)
				}
			}
		}
	}

	for operationID := range catalog {
		if _, ok := seen[operationID]; !ok {
			t.Fatalf("IAM identity catalog operation %q is not registered as a public route", operationID)
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
	user := identityServiceToken("42", identity.RoleAdmin)
	if allowed, err := identityHasPermission(ctx, authzSvc, user, permissionMemberCapabilitiesWrite, orgScopeTokenOrgID); err != nil || !allowed {
		t.Fatalf("graph grant should authorize member capability write, allowed=%v err=%v", allowed, err)
	}

	wrongOrg := identityServiceToken("99", identity.RoleAdmin)
	if allowed, err := identityHasPermission(ctx, authzSvc, wrongOrg, permissionMemberCapabilitiesWrite, orgScopeTokenOrgID); err != nil || allowed {
		t.Fatalf("graph grant for org 42 must not authorize org 99, allowed=%v err=%v", allowed, err)
	}

	credentialWithoutServiceAccount := &auth.Identity{
		Subject: "credential-1",
		OrgID:   "42",
		Raw: map[string]any{
			"verself:credential_id": "credential-1",
		},
	}
	if allowed, err := identityHasPermission(ctx, authzSvc, credentialWithoutServiceAccount, permissionMemberInvite, orgScopeTokenOrgID); !errors.Is(err, authz.ErrInvalid) || allowed {
		t.Fatalf("credential without service_account_id must be invalid, allowed=%v err=%v", allowed, err)
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
	return identity.OrganizationProfile{OrgID: "42", DisplayName: "Acme", Slug: "acme", State: identity.OrganizationProfileStateActive, Version: 1}, nil
}

func (s staticIdentityStore) ListOrganizationMetadataByOrgIDs(_ context.Context, orgIDs []string) ([]identity.OrganizationMetadata, error) {
	out := make([]identity.OrganizationMetadata, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		out = append(out, identity.OrganizationMetadata{OrgID: orgID, DisplayName: "Acme", Slug: "acme"})
	}
	return out, nil
}

func (s staticIdentityStore) UpdateOrganizationProfile(context.Context, identity.Principal, identity.UpdateOrganizationRequest) (identity.OrganizationProfile, error) {
	return identity.OrganizationProfile{OrgID: "42", DisplayName: "Acme", Slug: "acme", State: identity.OrganizationProfileStateActive, Version: 2}, nil
}

func (s staticIdentityStore) ResolveOrganizationProfile(context.Context, identity.ResolveOrganizationRequest) (identity.OrganizationProfile, error) {
	return identity.OrganizationProfile{OrgID: "42", DisplayName: "Acme", Slug: "acme", State: identity.OrganizationProfileStateActive, Version: 1}, nil
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
	err := requireOperationIdempotency(context.Background(), runtimeiam.OperationPolicy{Idempotency: idempotencyHeaderKey})
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusBadRequest {
		t.Fatalf("expected bad request for missing idempotency key, got %#v", err)
	}

	ctx := context.WithValue(context.Background(), operationRequestInfoKey{}, operationRequestInfo{IdempotencyKey: "key-1"})
	if err := requireOperationIdempotency(ctx, runtimeiam.OperationPolicy{Idempotency: idempotencyHeaderKey}); err != nil {
		t.Fatalf("valid idempotency key rejected: %v", err)
	}
}

func TestRoleAssignmentOrgIDsNormalizeAndRejectInvalidInput(t *testing.T) {
	ctx := context.Background()
	identity := &auth.Identity{RoleAssignments: []auth.RoleAssignment{
		{OrganizationID: "42", Role: "owner"},
		{OrganizationID: "7", Role: "member"},
		{OrganizationID: "42", Role: "admin"},
		{OrganizationID: "", Role: "member"},
	}}
	orgIDs, err := roleAssignmentOrgIDs(ctx, identity)
	if err != nil {
		t.Fatalf("role assignments rejected: %v", err)
	}
	if got := strings.Join(orgIDs, ","); got != "42,7" {
		t.Fatalf("org IDs = %s, want sorted unique 42,7", got)
	}

	_, err = roleAssignmentOrgIDs(ctx, &auth.Identity{RoleAssignments: []auth.RoleAssignment{{OrganizationID: "not-a-number", Role: "member"}}})
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusBadRequest {
		t.Fatalf("invalid org assignment should be 400, got %#v", err)
	}

	_, err = roleAssignmentOrgIDs(ctx, &auth.Identity{})
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
	policy := runtimeiam.OperationPolicy{
		Permission:     permissionAPICredentialsCreate,
		Resource:       resourceAPICredential,
		Action:         runtimeiam.ActionCreate,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitAPICredentialMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditAPICredentialCreate,
		BodyLimitBytes: bodyLimitSmallJSON,
	}
	auditOperation(ctx, huma.Operation{OperationID: "create-api-credential"}, policy, &auth.Identity{
		Subject: "user-1",
		OrgID:   "42",
	}, createAPICredentialInput{}, nil, "denied", forbidden(ctx, "permission-denied", "missing required permission"))
	auditOperation(ctx, huma.Operation{OperationID: "create-api-credential"}, policy, &auth.Identity{
		Subject: "credential-subject",
		OrgID:   "42",
		Raw: map[string]any{
			"verself:credential_id":      "credential-1",
			"verself:service_account_id": "service-account-1",
		},
	}, createAPICredentialInput{}, nil, "allowed", nil)

	body := logs.String()
	for _, want := range []string{
		"msg=\"identity api operation\"",
		"audit_event=iam.api_credential.create",
		"operation_id=create-api-credential",
		"operation_permission=iam:api_credentials:create",
		"operation_resource=api_credential",
		"operation_action=create",
		"rate_limit_class=api_credential_mutation",
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
		rateLimitMemberMutation: {Limit: 2, Window: time.Minute},
	})
	now := time.Unix(1700000000, 0)
	if decision := limiter.allow(rateLimitMemberMutation, "org:subject:ip", now); !decision.Allowed {
		t.Fatalf("first request should be allowed: %#v", decision)
	}
	if decision := limiter.allow(rateLimitMemberMutation, "org:subject:ip", now.Add(time.Second)); !decision.Allowed {
		t.Fatalf("second request should be allowed: %#v", decision)
	}
	if decision := limiter.allow(rateLimitMemberMutation, "org:subject:ip", now.Add(2*time.Second)); decision.Allowed || decision.RetryAfter <= 0 {
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
