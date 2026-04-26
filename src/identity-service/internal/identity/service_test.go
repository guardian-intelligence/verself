package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestValidateMemberCapabilitiesRejectsUnknownKeys(t *testing.T) {
	doc := DefaultMemberCapabilitiesDocument("42", "user-1", time.Unix(1700000000, 0).UTC())
	doc.EnabledKeys = append(doc.EnabledKeys, "unknown_capability")

	if err := ValidateMemberCapabilities(doc); !errors.Is(err, ErrInvalidCapabilities) {
		t.Fatalf("expected ErrInvalidCapabilities, got %v", err)
	}
}

func TestValidateMemberCapabilitiesRejectsDuplicates(t *testing.T) {
	doc := DefaultMemberCapabilitiesDocument("42", "user-1", time.Unix(1700000000, 0).UTC())
	if len(doc.EnabledKeys) == 0 {
		t.Fatal("default capability set should not be empty")
	}
	doc.EnabledKeys = append(doc.EnabledKeys, doc.EnabledKeys[0])

	if err := ValidateMemberCapabilities(doc); !errors.Is(err, ErrInvalidCapabilities) {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
}

func TestOwnerAndAdminGrantAllKnownPermissions(t *testing.T) {
	doc := MemberCapabilitiesDocument{OrgID: "42"}
	knownCount := len(KnownPermissions())

	for _, role := range []string{RoleOwner, RoleAdmin} {
		permissions := PermissionsForRoles(doc, []string{role})
		if len(permissions) != knownCount {
			t.Fatalf("role %q should grant %d permissions, got %d", role, knownCount, len(permissions))
		}
	}
}

func TestMemberPermissionsAreCapabilityDerived(t *testing.T) {
	doc := MemberCapabilitiesDocument{OrgID: "42", EnabledKeys: []string{}}
	baseline := PermissionsForRoles(doc, []string{RoleMember})

	for _, expected := range baselineMemberPermissions {
		if !contains(baseline, expected) {
			t.Fatalf("baseline member permissions missing %q (got %v)", expected, baseline)
		}
	}
	if contains(baseline, PermissionSandboxExecutionScheduleWrite) {
		t.Fatal("member without deploy_executions capability must not hold sandbox:execution_schedule:write")
	}

	doc.EnabledKeys = []string{"deploy_executions"}
	enabled := PermissionsForRoles(doc, []string{RoleMember})
	if !contains(enabled, PermissionSandboxExecutionScheduleWrite) {
		t.Fatalf("deploy_executions should grant sandbox:execution_schedule:write, got %v", enabled)
	}
}

func TestMemberCannotMintNonMemberEligiblePermission(t *testing.T) {
	doc := MemberCapabilitiesDocument{OrgID: "42", EnabledKeys: DefaultCapabilityKeys()}
	principal := Principal{Subject: "u-1", OrgID: "42", Roles: []string{RoleMember}}

	// api_credentials:create is admin-only (not member-eligible).
	err := validateCredentialPermissions(doc, principal, []string{PermissionAPICredentialsCreate})
	if !errors.Is(err, ErrInvalidCapabilities) {
		t.Fatalf("expected ErrInvalidCapabilities for member minting api_credentials:create, got %v", err)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestServiceMembersHidesMachineUsersAndOwners(t *testing.T) {
	directory := &fakeMembersDirectory{members: []Member{
		{UserID: "u1", Type: MemberTypeHuman, Email: "ceo@example.test", DisplayName: "CEO", RoleKeys: []string{RoleOwner}},
		{UserID: "u2", Type: MemberTypeHuman, Email: "agent@example.test", DisplayName: "Agent", RoleKeys: []string{RoleAdmin}},
		{UserID: "u3", Type: MemberTypeHuman, Email: "rocky@example.test", DisplayName: "Rocky", RoleKeys: []string{RoleMember}},
		{UserID: "u4", Type: MemberTypeMachine, LoginName: "assume-platform-admin", DisplayName: "assume-platform-admin", RoleKeys: []string{RoleOwner}},
		{UserID: "u5", Type: MemberTypeMachine, LoginName: "ci-bot", DisplayName: "ci-bot", RoleKeys: []string{RoleAdmin}},
	}}
	svc := &Service{
		Store:     fakeMembersStore{},
		Directory: directory,
		ProjectID: "identity",
	}

	got, err := svc.Members(context.Background(), Principal{Subject: "u2", OrgID: "42"})
	if err != nil {
		t.Fatalf("Members: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 visible members (admin + member), got %d: %#v", len(got), got)
	}
	for _, member := range got {
		if member.Type == MemberTypeMachine {
			t.Fatalf("machine user leaked into members table: %#v", member)
		}
		if contains(member.RoleKeys, RoleOwner) {
			t.Fatalf("owner-role member leaked into members table: %#v", member)
		}
	}
}

type fakeMembersDirectory struct {
	members []Member
}

func (d *fakeMembersDirectory) ListMembers(context.Context, string, string) ([]Member, error) {
	out := make([]Member, len(d.members))
	copy(out, d.members)
	return out, nil
}

func (d *fakeMembersDirectory) InviteMember(context.Context, string, string, InviteMemberRequest) (InviteMemberResult, error) {
	return InviteMemberResult{}, nil
}

func (d *fakeMembersDirectory) UpdateMemberRoles(context.Context, string, string, string, []string) (Member, error) {
	return Member{}, nil
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

type fakeMembersStore struct{}

func (fakeMembersStore) GetOrganizationProfile(context.Context, string, string) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 1}, nil
}

func (fakeMembersStore) ListOrganizationMetadataByOrgIDs(_ context.Context, orgIDs []string) ([]OrganizationMetadata, error) {
	out := make([]OrganizationMetadata, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		out = append(out, OrganizationMetadata{OrgID: orgID, DisplayName: "Acme", Slug: "acme"})
	}
	return out, nil
}

func (fakeMembersStore) UpdateOrganizationProfile(context.Context, Principal, UpdateOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 2}, nil
}

func (fakeMembersStore) ResolveOrganizationProfile(context.Context, ResolveOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 1}, nil
}

func (fakeMembersStore) GetMemberCapabilities(context.Context, string, string) (MemberCapabilitiesDocument, error) {
	return MemberCapabilitiesDocument{}, nil
}

func (fakeMembersStore) PutMemberCapabilities(context.Context, MemberCapabilitiesDocument) (MemberCapabilitiesDocument, error) {
	return MemberCapabilitiesDocument{}, nil
}

func (fakeMembersStore) GetOrgACLState(context.Context, string, string) (OrgACLState, error) {
	return OrgACLState{Version: 1}, nil
}

func (fakeMembersStore) UpdateMemberRolesCommand(context.Context, UpdateMemberRolesCommand, Directory, string) (UpdateMemberRolesResult, error) {
	return UpdateMemberRolesResult{}, nil
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
