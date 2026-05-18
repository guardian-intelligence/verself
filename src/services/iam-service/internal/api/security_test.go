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
		allowedOrgID:     "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y",
		allowedSubjectID: "user-1",
		allowedOrgPerms:  map[string]struct{}{"read": {}, "manage_iam": {}},
	})
	runtime := publicRuntime{authz: authzSvc}
	user := identityServiceToken("org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y")
	if allowed, err := runtime.identityHasContractPermission(ctx, user, contractapi.UpdateOrganization.Descriptor, []string{"org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y"}); err != nil || !allowed {
		t.Fatalf("graph grant should authorize organization update, allowed=%v err=%v", allowed, err)
	}

	wrongOrg := identityServiceToken("org_9ZQ7AZ5QW69HZ9PSWZQ05A7Z9V")
	if allowed, err := runtime.identityHasContractPermission(ctx, wrongOrg, contractapi.UpdateOrganization.Descriptor, []string{"org_9ZQ7AZ5QW69HZ9PSWZQ05A7Z9V"}); err != nil || allowed {
		t.Fatalf("graph grant for the allowed org must not authorize another org, allowed=%v err=%v", allowed, err)
	}

	credentialWithoutServiceAccount := &auth.Identity{
		Subject: "credential-1",
		OrgID:   "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y",
		Raw: map[string]any{
			"verself:credential_id": "credential-1",
		},
	}
	if allowed, err := runtime.identityHasContractPermission(ctx, credentialWithoutServiceAccount, contractapi.GetOrganization.Descriptor, []string{"org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y"}); !errors.Is(err, authz.ErrInvalid) || allowed {
		t.Fatalf("credential without service_account_id must be invalid, allowed=%v err=%v", allowed, err)
	}
}

func identityServiceToken(orgID string) *auth.Identity {
	return &auth.Identity{
		Subject: "user-1",
		OrgID:   orgID,
	}
}

type staticIdentityStore struct{}

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

func (b staticAuthzBackend) LookupResources(_ context.Context, resourceType, permission string, subject spicedb.SubjectRef, _ uint32, _ string) ([]string, string, error) {
	if resourceType == "org" && permission == "read" && subject.ID != "" {
		return []string{b.allowedOrgID}, "zed-test", nil
	}
	return nil, "zed-test", nil
}

func (b staticAuthzBackend) ReadResourceRelationships(context.Context, spicedb.ResourceRef, map[string]struct{}) ([]spicedb.Relationship, string, error) {
	return nil, "", nil
}

func (b staticAuthzBackend) ReplaceResourceRelationships(context.Context, []spicedb.Relationship, []spicedb.Relationship, map[string]any) (string, error) {
	return "", nil
}

func (s staticIdentityStore) GetOrganizationProfile(context.Context, string, string) (identity.OrganizationProfile, error) {
	return identity.OrganizationProfile{OrgID: "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: identity.OrganizationProfileStateActive, Version: 1}, nil
}

func (s staticIdentityStore) ListOrganizationMetadataByOrgIDs(_ context.Context, orgIDs []string) ([]identity.OrganizationMetadata, error) {
	out := make([]identity.OrganizationMetadata, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		out = append(out, identity.OrganizationMetadata{OrgID: orgID, IdentityProviderOrgID: orgID, DisplayName: "Acme", Slug: "acme", Version: 1})
	}
	return out, nil
}

func (s staticIdentityStore) ListOrganizationMetadataByProviderOrgIDs(_ context.Context, providerOrgIDs []string) ([]identity.OrganizationMetadata, error) {
	out := make([]identity.OrganizationMetadata, 0, len(providerOrgIDs))
	for _, providerOrgID := range providerOrgIDs {
		out = append(out, identity.OrganizationMetadata{OrgID: "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y", IdentityProviderOrgID: providerOrgID, DisplayName: "Acme", Slug: "acme", Version: 1})
	}
	return out, nil
}

func (s staticIdentityStore) OrganizationSlugAvailable(context.Context, string) (bool, error) {
	return true, nil
}

func (s staticIdentityStore) CreateOrganizationProfile(_ context.Context, input identity.CreateOrganizationRequest) (identity.OrganizationProfile, error) {
	return identity.OrganizationProfile{OrgID: input.OrgID, IdentityProviderOrgID: input.IdentityProviderOrgID, DisplayName: input.DisplayName, Slug: input.Slug, State: identity.OrganizationProfileStateActive, Version: 1}, nil
}

func (s staticIdentityStore) UpdateOrganizationProfile(context.Context, identity.Principal, identity.UpdateOrganizationRequest) (identity.OrganizationProfile, error) {
	return identity.OrganizationProfile{OrgID: "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: identity.OrganizationProfileStateActive, Version: 2}, nil
}

func (s staticIdentityStore) ResolveOrganizationProfile(context.Context, identity.ResolveOrganizationRequest) (identity.OrganizationProfile, error) {
	return identity.OrganizationProfile{OrgID: "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: identity.OrganizationProfileStateActive, Version: 1}, nil
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

func (s staticIdentityStore) CreateMemberInviteAcceptance(context.Context, identity.MemberInviteAcceptance) error {
	return nil
}

func (s staticIdentityStore) GetMemberInviteAcceptance(context.Context, string, time.Time) (identity.MemberInviteAcceptance, error) {
	return identity.MemberInviteAcceptance{}, identity.ErrMemberMissing
}

func (s staticIdentityStore) AcceptMemberInviteAcceptance(context.Context, string, time.Time) error {
	return identity.ErrMemberMissing
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

func TestPublicContractOperationDoesNotRequireBearerIdentity(t *testing.T) {
	runtime := publicRuntime{installationID: "inst_01J8QJ4P1R7S9W2X5M6N8P0Q2"}
	ctx := context.WithValue(context.Background(), operationRequestInfoKey{}, operationRequestInfo{
		ClientIP:       "203.0.113.10",
		IdempotencyKey: "invite-retry-key",
	})

	identityValue, err := runtime.BeforeOperation(ctx, contractapi.AcceptMemberInvite.Descriptor, &contractapi.AcceptMemberInviteInput{})
	if err != nil {
		t.Fatalf("public operation rejected without bearer identity: %v", err)
	}
	publicIdentity, ok := identityValue.(publicOperationIdentity)
	if !ok {
		t.Fatalf("unexpected public identity value: %#v", identityValue)
	}
	if publicIdentity.Auth != nil || len(publicIdentity.PublicOrgIDs) != 1 || publicIdentity.PublicOrgIDs[0] != "inst_01J8QJ4P1R7S9W2X5M6N8P0Q2" {
		t.Fatalf("unexpected public identity scope: %#v", publicIdentity)
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
	operation := contractapi.SetIamPolicy.Descriptor
	policy := operationPolicyFromContract(operation)
	input := contractapi.SetIamPolicyInput{
		OrgID: "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y",
		Body: contractapi.SetIamPolicyInputBody{
			Policy: contractapi.IAMPolicy{Version: 1},
		},
	}
	auditOperation(ctx, huma.Operation{OperationID: operation.OperationID}, policy, &auth.Identity{
		Subject: "user-1",
		OrgID:   "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y",
	}, input, nil, "denied", forbidden(ctx, "permission-denied", "missing required permission"))
	auditOperation(ctx, huma.Operation{OperationID: operation.OperationID}, policy, &auth.Identity{
		Subject: "credential-subject",
		OrgID:   "org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y",
		Raw: map[string]any{
			"verself:credential_id":      "credential-1",
			"verself:service_account_id": "service-account-1",
		},
	}, input, nil, "allowed", nil)

	body := logs.String()
	for _, want := range []string{
		"msg=\"identity api operation\"",
		"audit_event=iam.policy.set",
		"operation_id=set-iam-policy",
		"operation_permission=iam:policy:set",
		"operation_resource=organization",
		"operation_action=set",
		"rate_limit_class=iam_mutation",
		"outcome=denied",
		"outcome=allowed",
		"subject=user-1",
		"subject=credential-subject",
		"org_id=org_G1ZRBDTWBCGK0BQCKMAPKBWZ4Y",
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
