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

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/identity-service/internal/identity"
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
			rawPolicy, ok := op.Extensions["x-forge-metal-iam"].(map[string]any)
			if !ok {
				t.Fatalf("%s %s missing x-forge-metal-iam policy", op.Method, path)
			}
			for _, key := range []string{"permission", "resource", "action", "org_scope", "rate_limit_class", "audit_event"} {
				if rawPolicy[key] == "" {
					t.Fatalf("%s %s empty policy field %q: %#v", op.Method, path, key, rawPolicy)
				}
			}
			if rawPolicy["org_scope"] != "token_org_id" {
				t.Fatalf("%s %s unexpected org_scope: %#v", op.Method, path, rawPolicy)
			}
			if len(op.Security) != 1 || len(op.Security[0]["bearerAuth"]) != 0 {
				t.Fatalf("%s %s must require bearerAuth with no OpenAPI scopes: %#v", op.Method, path, op.Security)
			}
			if operationRequiresBodyBudget(*op) {
				if rawPolicy["request_body_max_bytes"] == nil {
					t.Fatalf("%s %s missing request_body_max_bytes: %#v", op.Method, path, rawPolicy)
				}
				if rawPolicy["idempotency"] != idempotencyHeaderKey {
					t.Fatalf("%s %s mutating identity operation must require header idempotency: %#v", op.Method, path, rawPolicy)
				}
				if !operationHasRequiredParameter(op, "header", "Idempotency-Key") {
					t.Fatalf("%s %s requires Idempotency-Key but does not declare it", op.Method, path)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no public API operations")
	}
}

func TestIdentityPermissionChecksCurrentOrgRoleBundlesAndDirectScopes(t *testing.T) {
	ctx := context.Background()
	store := staticPolicyStore{capabilities: identity.DefaultMemberCapabilitiesDocument("42", "tester", time.Unix(1700000000, 0).UTC())}
	svc := &identity.Service{
		Store:     store,
		ProjectID: "identity-project",
	}
	admin := identityServiceToken("identity-project", "42", identity.RoleAdmin)
	if allowed, err := identityHasPermission(ctx, svc, admin, permissionMemberCapabilitiesWrite); err != nil || !allowed {
		t.Fatal("org admin should be allowed to write member capabilities")
	}

	wrongOrg := identityServiceToken("identity-project", "99", identity.RoleAdmin)
	wrongOrg.OrgID = "42"
	if allowed, err := identityHasPermission(ctx, svc, wrongOrg, permissionMemberCapabilitiesWrite); err != nil || allowed {
		t.Fatal("role assignment for another org must not grant current org")
	}

	member := identityServiceToken("identity-project", "42", identity.RoleMember)
	if allowed, err := identityHasPermission(ctx, svc, member, permissionMemberRead); err != nil || !allowed {
		t.Fatal("member should be allowed to read members")
	}
	if allowed, err := identityHasPermission(ctx, svc, member, permissionMemberCapabilitiesWrite); err != nil || allowed {
		t.Fatal("member should not be allowed to write member capabilities")
	}

	crossProject := identityServiceToken("sandbox-project", "42", "sandbox_org_admin")
	svcWithDirectory := &identity.Service{
		Store:     store,
		Directory: &staticDirectory{},
		ProjectID: "identity-project",
	}
	if allowed, err := identityHasPermission(ctx, svcWithDirectory, crossProject, permissionMemberCapabilitiesWrite); err != nil || allowed {
		t.Fatalf("cross-project web token must fail closed without a target project role claim, allowed=%v err=%v", allowed, err)
	}

	owner := identityServiceToken("identity-project", "42", identity.RoleOwner)
	if allowed, err := identityHasPermission(ctx, svc, owner, permissionMemberCapabilitiesWrite); err != nil || !allowed {
		t.Fatal("owner should grant all identity-service permissions")
	}

	unmarkedScope := &auth.Identity{Subject: "user-1", OrgID: "42", Raw: map[string]any{"scope": string(permissionMemberInvite)}}
	if allowed, err := identityHasPermission(ctx, svc, unmarkedScope, permissionMemberInvite); err != nil || allowed {
		t.Fatal("plain OAuth scope should not grant operation permissions without an API credential marker")
	}

	scoped := &auth.Identity{
		Subject: "credential-1",
		OrgID:   "42",
		Raw: map[string]any{
			"forge_metal:credential_id": "credential-1",
			"permissions":               []string{string(permissionMemberInvite)},
		},
	}
	if allowed, err := identityHasPermission(ctx, nil, scoped, permissionMemberInvite); err != nil || !allowed {
		t.Fatal("API credential scope should grant matching permission")
	}
}

func identityServiceToken(projectID, orgID string, roles ...string) *auth.Identity {
	assignments := make([]auth.RoleAssignment, 0, len(roles))
	for _, role := range roles {
		assignments = append(assignments, auth.RoleAssignment{
			ProjectID:      projectID,
			OrganizationID: orgID,
			Role:           role,
		})
	}
	return &auth.Identity{
		Subject:         "user-1",
		OrgID:           orgID,
		ProjectID:       projectID,
		RoleAssignments: assignments,
	}
}

type staticPolicyStore struct {
	capabilities identity.MemberCapabilitiesDocument
}

func (s staticPolicyStore) GetMemberCapabilities(context.Context, string, string) (identity.MemberCapabilitiesDocument, error) {
	return s.capabilities, nil
}

func (s staticPolicyStore) PutMemberCapabilities(context.Context, identity.MemberCapabilitiesDocument) (identity.MemberCapabilitiesDocument, error) {
	return s.capabilities, nil
}

func (s staticPolicyStore) CreateAPICredential(context.Context, identity.APICredential, identity.APICredentialSecret) (identity.APICredential, error) {
	return identity.APICredential{}, nil
}

func (s staticPolicyStore) ListAPICredentials(context.Context, string) ([]identity.APICredential, error) {
	return []identity.APICredential{}, nil
}

func (s staticPolicyStore) GetAPICredential(context.Context, string, string) (identity.APICredential, error) {
	return identity.APICredential{}, identity.ErrAPICredentialMissing
}

func (s staticPolicyStore) ActiveAPICredentialSecrets(context.Context, string, string) ([]identity.APICredentialSecret, error) {
	return []identity.APICredentialSecret{}, nil
}

func (s staticPolicyStore) AddAPICredentialSecret(context.Context, string, string, string, identity.APICredentialSecret) (identity.APICredential, error) {
	return identity.APICredential{}, nil
}

func (s staticPolicyStore) RevokeAPICredential(context.Context, string, string, string, time.Time) (identity.APICredential, error) {
	return identity.APICredential{}, nil
}

func (s staticPolicyStore) ResolveAPICredentialClaims(context.Context, string, time.Time) (identity.ResolveAPICredentialClaimsResult, error) {
	return identity.ResolveAPICredentialClaimsResult{}, identity.ErrAPICredentialMissing
}

type staticDirectory struct{}

func (s *staticDirectory) ListMembers(context.Context, string, string) ([]identity.Member, error) {
	return []identity.Member{}, nil
}

func (s *staticDirectory) InviteMember(context.Context, string, string, identity.InviteMemberRequest) (identity.InviteMemberResult, error) {
	return identity.InviteMemberResult{}, nil
}

func (s *staticDirectory) UpdateMemberRoles(context.Context, string, string, string, []string) (identity.Member, error) {
	return identity.Member{}, nil
}

func (s *staticDirectory) CreateServiceAccountCredential(context.Context, string, identity.ServiceAccountCredentialInput) (string, identity.APICredentialIssuedMaterial, error) {
	return "subject-1", identity.APICredentialIssuedMaterial{}, nil
}

func (s *staticDirectory) AddServiceAccountCredential(context.Context, identity.AddServiceAccountCredentialInput) (identity.APICredentialIssuedMaterial, error) {
	return identity.APICredentialIssuedMaterial{}, nil
}

func (s *staticDirectory) RemoveServiceAccountCredential(context.Context, string, identity.APICredentialSecret) error {
	return nil
}

func (s *staticDirectory) DeactivateServiceAccount(context.Context, string) error {
	return nil
}

func TestOperationPolicyRequiresIdempotencyHeader(t *testing.T) {
	err := requireOperationIdempotency(context.Background(), operationPolicy{Idempotency: idempotencyHeaderKey})
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) || statusErr.GetStatus() != http.StatusBadRequest {
		t.Fatalf("expected bad request for missing idempotency key, got %#v", err)
	}

	ctx := context.WithValue(context.Background(), operationRequestInfoKey{}, operationRequestInfo{IdempotencyKey: "key-1"})
	if err := requireOperationIdempotency(ctx, operationPolicy{Idempotency: idempotencyHeaderKey}); err != nil {
		t.Fatalf("valid idempotency key rejected: %v", err)
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
	limiter := newFixedWindowOperationRateLimiter(map[string]rateLimitRule{
		"member_mutation": {Limit: 2, Window: time.Minute},
	})
	now := time.Unix(1700000000, 0)
	if decision := limiter.allow("member_mutation", "org:subject:ip", now); !decision.Allowed {
		t.Fatalf("first request should be allowed: %#v", decision)
	}
	if decision := limiter.allow("member_mutation", "org:subject:ip", now.Add(time.Second)); !decision.Allowed {
		t.Fatalf("second request should be allowed: %#v", decision)
	}
	if decision := limiter.allow("member_mutation", "org:subject:ip", now.Add(2*time.Second)); decision.Allowed || decision.RetryAfter <= 0 {
		t.Fatalf("third request should be throttled: %#v", decision)
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
