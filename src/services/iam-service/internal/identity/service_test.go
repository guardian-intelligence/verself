package identity

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestServiceMembersHidesMachineUsers(t *testing.T) {
	directory := &fakeMembersDirectory{members: []Member{
		{UserID: "u1", Type: MemberTypeHuman, Email: "ceo@example.test", DisplayName: "CEO"},
		{UserID: "u2", Type: MemberTypeHuman, Email: "agent@example.test", DisplayName: "Agent"},
		{UserID: "u3", Type: MemberTypeMachine, LoginName: "ci-bot", DisplayName: "ci-bot"},
	}}
	svc := &Service{
		Store:     fakeMembersStore{},
		Directory: directory,
	}

	got, err := svc.Members(context.Background(), Principal{Subject: "u2", OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2"})
	if err != nil {
		t.Fatalf("Members: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 visible members, got %d: %#v", len(got), got)
	}
	for _, member := range got {
		if member.Type == MemberTypeMachine {
			t.Fatalf("machine user leaked into members table: %#v", member)
		}
	}
}

func TestAccessibleOrganizationsUsesAuthorizationGraph(t *testing.T) {
	svc := &Service{
		Store: fakeMembersStore{},
		AuthorizationGraph: fakeMembersAuthz{
			orgIDs: []string{"org_01J8QJ4P1R7S9W2X5M6N8P0Q2"},
		},
	}

	got, err := svc.AccessibleOrganizations(context.Background(), AuthorizationSubject{Kind: AuthorizationSubjectKindUser, ID: "user-1"})
	if err != nil {
		t.Fatalf("AccessibleOrganizations: %v", err)
	}
	if len(got) != 1 || got[0].OrgID != "org_01J8QJ4P1R7S9W2X5M6N8P0Q2" {
		t.Fatalf("unexpected organizations: %#v", got)
	}
}

func TestServiceCreateOrganizationCreatesDirectoryOrgAndProfile(t *testing.T) {
	store := &fakeSignupStore{}
	directory := &fakeMembersDirectory{}
	svc := &Service{
		Store:     store,
		Directory: directory,
	}

	got, err := svc.CreateOrganization(context.Background(), "user-1", PublicCreateOrganizationRequest{
		DisplayName:    "  Acme   Labs  ",
		Slug:           "Acme_Labs",
		IdempotencyKey: "signup-key",
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if !strings.HasPrefix(got.OrgID, "org_") || got.Slug != "acme-labs" || got.DisplayName != "Acme Labs" || got.Version != 1 {
		t.Fatalf("unexpected created organization: %#v", got)
	}
	if directory.createInput.AdminUserID != "user-1" || !strings.HasPrefix(directory.createInput.Name, "acme-labs-") {
		t.Fatalf("unexpected directory create input: %#v", directory.createInput)
	}
	if store.created.OrgID != got.OrgID || store.created.IdentityProviderOrgID != "43" || store.created.ActorID != "user-1" {
		t.Fatalf("unexpected stored profile input: %#v", store.created)
	}
}

func TestServiceInviteMemberNormalizesRolesForDirectoryAndPolicy(t *testing.T) {
	directory := &fakeMembersDirectory{}
	svc := &Service{
		Store:     fakeMembersStore{},
		Directory: directory,
	}

	got, err := svc.InviteMember(context.Background(), Principal{Subject: "owner-1", OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2"}, InviteMemberRequest{
		Email: " invited@example.test ",
		Roles: []string{"roles/member", "roles/admin", "roles/member"},
	})
	if err != nil {
		t.Fatalf("InviteMember: %v", err)
	}
	if directory.inviteProviderOrgID != "42" {
		t.Fatalf("provider org = %q", directory.inviteProviderOrgID)
	}
	if directory.inviteInput.Email != "invited@example.test" {
		t.Fatalf("directory invite input = %#v", directory.inviteInput)
	}
	if strings.Join(got.Roles, ",") != "roles/admin,roles/member" {
		t.Fatalf("roles = %#v", got.Roles)
	}
}

type fakeMembersDirectory struct {
	members             []Member
	createInput         DirectoryCreateOrganizationRequest
	inviteProviderOrgID string
	inviteInput         InviteMemberRequest
}

func (d *fakeMembersDirectory) ListMembers(context.Context, string) ([]Member, error) {
	out := make([]Member, len(d.members))
	copy(out, d.members)
	return out, nil
}

func (d *fakeMembersDirectory) CreateOrganization(_ context.Context, input DirectoryCreateOrganizationRequest) (DirectoryCreateOrganizationResult, error) {
	d.createInput = input
	return DirectoryCreateOrganizationResult{OrganizationID: "43"}, nil
}

func (d *fakeMembersDirectory) InviteMember(_ context.Context, providerOrgID string, input InviteMemberRequest) (InviteMemberResult, error) {
	d.inviteProviderOrgID = providerOrgID
	d.inviteInput = input
	return InviteMemberResult{UserID: "user-invite", Email: input.Email, Status: "invited"}, nil
}

func (d *fakeMembersDirectory) UpdateHumanProfile(context.Context, string, HumanProfileUpdate) (HumanProfile, error) {
	return HumanProfile{}, nil
}

func (d *fakeMembersDirectory) CreateServiceAccountCredential(context.Context, string, ServiceAccountCredentialInput) (string, APICredentialIssuedMaterial, error) {
	return "", APICredentialIssuedMaterial{}, nil
}

func (d *fakeMembersDirectory) AddServiceAccountCredential(context.Context, AddServiceAccountCredentialInput) (APICredentialIssuedMaterial, error) {
	return APICredentialIssuedMaterial{}, nil
}

func (d *fakeMembersDirectory) RemoveServiceAccountCredential(context.Context, string, APICredentialSecret) error {
	return nil
}

func (d *fakeMembersDirectory) DeactivateServiceAccount(context.Context, string) error {
	return nil
}

type fakeMembersAuthz struct {
	orgIDs []string
}

func (a fakeMembersAuthz) LookupOrganizations(context.Context, AuthorizationSubject, string, string) ([]string, string, error) {
	return append([]string(nil), a.orgIDs...), "zed-token", nil
}

func (a fakeMembersAuthz) TestOrganizationPermissions(context.Context, string, AuthorizationSubject, []string, string) ([]string, string, error) {
	return nil, "zed-token", nil
}

type fakeMembersStore struct{}

type fakeSignupStore struct {
	fakeMembersStore
	created CreateOrganizationRequest
}

func (s *fakeSignupStore) CreateOrganizationProfile(_ context.Context, input CreateOrganizationRequest) (OrganizationProfile, error) {
	s.created = input
	return OrganizationProfile{OrgID: input.OrgID, IdentityProviderOrgID: input.IdentityProviderOrgID, DisplayName: input.DisplayName, Slug: input.Slug, State: OrganizationProfileStateActive, Version: 1}, nil
}

func (fakeMembersStore) GetOrganizationProfile(context.Context, string, string) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 1}, nil
}

func (fakeMembersStore) ListOrganizationMetadataByOrgIDs(_ context.Context, orgIDs []string) ([]OrganizationMetadata, error) {
	out := make([]OrganizationMetadata, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		out = append(out, OrganizationMetadata{OrgID: orgID, IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", Version: 1})
	}
	return out, nil
}

func (fakeMembersStore) ListOrganizationMetadataByProviderOrgIDs(_ context.Context, providerOrgIDs []string) ([]OrganizationMetadata, error) {
	out := make([]OrganizationMetadata, 0, len(providerOrgIDs))
	for _, providerOrgID := range providerOrgIDs {
		out = append(out, OrganizationMetadata{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: providerOrgID, DisplayName: "Acme", Slug: "acme", Version: 1})
	}
	return out, nil
}

func (fakeMembersStore) OrganizationSlugAvailable(context.Context, string) (bool, error) {
	return true, nil
}

func (fakeMembersStore) CreateOrganizationProfile(_ context.Context, input CreateOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: input.OrgID, IdentityProviderOrgID: input.IdentityProviderOrgID, DisplayName: input.DisplayName, Slug: input.Slug, State: OrganizationProfileStateActive, Version: 1}, nil
}

func (fakeMembersStore) UpdateOrganizationProfile(context.Context, Principal, UpdateOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 2}, nil
}

func (fakeMembersStore) ResolveOrganizationProfile(context.Context, ResolveOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 1}, nil
}

func (fakeMembersStore) CreateServiceAccount(context.Context, ServiceAccount, APICredential, APICredentialSecret) (ServiceAccount, APICredential, error) {
	return ServiceAccount{}, APICredential{}, nil
}

func (fakeMembersStore) ListServiceAccounts(context.Context, string) ([]ServiceAccount, error) {
	return nil, nil
}

func (fakeMembersStore) GetServiceAccount(context.Context, string, string) (ServiceAccount, error) {
	return ServiceAccount{}, ErrAPICredentialMissing
}

func (fakeMembersStore) DisableServiceAccount(context.Context, string, string, string, time.Time) (ServiceAccount, []APICredential, error) {
	return ServiceAccount{}, nil, nil
}

func (fakeMembersStore) CreateAPICredential(context.Context, APICredential, APICredentialSecret) (APICredential, error) {
	return APICredential{}, nil
}

func (fakeMembersStore) ListAPICredentials(context.Context, string) ([]APICredential, error) {
	return nil, nil
}

func (fakeMembersStore) GetAPICredential(context.Context, string, string) (APICredential, error) {
	return APICredential{}, ErrAPICredentialMissing
}

func (fakeMembersStore) ActiveAPICredentialSecrets(context.Context, string, string) ([]APICredentialSecret, error) {
	return nil, nil
}

func (fakeMembersStore) AddAPICredentialSecret(context.Context, string, string, string, APICredentialSecret) (APICredential, error) {
	return APICredential{}, nil
}

func (fakeMembersStore) RevokeAPICredential(context.Context, string, string, string, time.Time) (APICredential, error) {
	return APICredential{}, nil
}

func (fakeMembersStore) ResolveAPICredentialClaims(context.Context, string, time.Time) (ResolveAPICredentialClaimsResult, error) {
	return ResolveAPICredentialClaimsResult{}, ErrAPICredentialMissing
}
