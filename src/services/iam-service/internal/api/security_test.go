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
	"github.com/verself/iam-service/internal/contractapi"
	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/iam-service/internal/spicedb"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

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
		Type: " api_activity ",
		ID:   " 371564185181576922 ",
	}, "371564185181576922", "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y")
	if resource.Type != "api_activity" || resource.ID != "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y" {
		t.Fatalf("resource = %#v, want canonical api_activity public org ID", resource)
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
	limiter := runtimeiam.NewFixedWindowOperationRateLimiter(map[runtimeiam.RateLimitClass]runtimeiam.RateLimitRule{
		rateLimitIAMMutation: {Limit: 2, Window: time.Minute},
	})
	now := time.Unix(1700000000, 0)
	if decision := limiter.Allow(rateLimitIAMMutation, "org:subject:ip", now); !decision.Allowed {
		t.Fatalf("first request should be allowed: %#v", decision)
	}
	if decision := limiter.Allow(rateLimitIAMMutation, "org:subject:ip", now.Add(time.Second)); !decision.Allowed {
		t.Fatalf("second request should be allowed: %#v", decision)
	}
	if decision := limiter.Allow(rateLimitIAMMutation, "org:subject:ip", now.Add(2*time.Second)); decision.Allowed || decision.RetryAfter <= 0 {
		t.Fatalf("third request should be throttled: %#v", decision)
	}
}
