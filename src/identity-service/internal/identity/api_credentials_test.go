package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateAPICredentialValidatesRequestedPermissions(t *testing.T) {
	defaults := DefaultMemberCapabilitiesDocument("42", "tester", time.Unix(1700000000, 0).UTC())
	svc := &Service{
		Store:     &apiCredentialTestStore{capabilities: defaults},
		Directory: &apiCredentialTestDirectory{material: testIssuedMaterial(APICredentialAuthMethodPrivateKeyJWT, "client-1")},
		ProjectID: "identity-project",
		Now:       func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	_, err := svc.CreateAPICredential(context.Background(), Principal{
		Subject: "member-1",
		OrgID:   "42",
		Roles:   []string{RoleMember},
	}, CreateAPICredentialRequest{
		DisplayName: "credential mint",
		Permissions: []string{PermissionAPICredentialsCreate},
	})
	if !errors.Is(err, ErrInvalidCapabilities) {
		t.Fatalf("member should not mint api_credentials:create, got %v", err)
	}

	result, err := svc.CreateAPICredential(context.Background(), Principal{
		Subject: "owner-1",
		OrgID:   "42",
		Roles:   []string{RoleOwner},
	}, CreateAPICredentialRequest{
		DisplayName: "sandbox automation",
		Permissions: []string{PermissionSandboxExecutionScheduleWrite, PermissionSandboxLogsRead},
	})
	if err != nil {
		t.Fatalf("owner should mint sandbox permissions: %v", err)
	}
	if result.Credential.OrgID != "42" || result.Credential.SubjectID != "subject-1" {
		t.Fatalf("unexpected credential: %#v", result.Credential)
	}
	if result.Credential.PolicyVersionAtIssue != 0 {
		t.Fatalf("unexpected policy version at issue: %d", result.Credential.PolicyVersionAtIssue)
	}
	if result.IssuedMaterial.KeyContent == "" || result.IssuedMaterial.Fingerprint == "" {
		t.Fatalf("issued material was not returned once: %#v", result.IssuedMaterial)
	}
}

func TestCreateAPICredentialCleansUpServiceAccountWhenStoreFails(t *testing.T) {
	storeErr := errors.New("store failed")
	directory := &apiCredentialTestDirectory{material: testIssuedMaterial(APICredentialAuthMethodPrivateKeyJWT, "client-1")}
	svc := &Service{
		Store: &apiCredentialTestStore{
			capabilities: DefaultMemberCapabilitiesDocument("42", "tester", time.Unix(1700000000, 0).UTC()),
			createErr:    storeErr,
		},
		Directory: directory,
		ProjectID: "identity-project",
		Now:       func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	_, err := svc.CreateAPICredential(context.Background(), Principal{
		Subject: "owner-1",
		OrgID:   "42",
		Roles:   []string{RoleOwner},
	}, CreateAPICredentialRequest{
		DisplayName: "sandbox automation",
		Permissions: []string{PermissionSandboxExecutionScheduleWrite},
	})
	if !errors.Is(err, storeErr) {
		t.Fatalf("expected store error, got %v", err)
	}
	if len(directory.deactivatedSubjects) != 1 || directory.deactivatedSubjects[0] != "subject-1" {
		t.Fatalf("service account was not cleaned up: %#v", directory.deactivatedSubjects)
	}
}

func testIssuedMaterial(method APICredentialAuthMethod, clientID string) APICredentialIssuedMaterial {
	material := APICredentialIssuedMaterial{
		AuthMethod:  method,
		ClientID:    clientID,
		TokenURL:    "https://auth.example.com/oauth/v2/token",
		Fingerprint: "sha256:test",
	}
	switch method {
	case APICredentialAuthMethodPrivateKeyJWT:
		material.KeyID = "key-1"
		material.KeyContent = "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----"
	case APICredentialAuthMethodClientSecret:
		material.ClientSecret = "secret-1"
	}
	return material
}

type apiCredentialTestStore struct {
	capabilities MemberCapabilitiesDocument
	created      APICredential
	createErr    error
}

func (s *apiCredentialTestStore) GetOrganizationProfile(context.Context, string, string, string) (OrganizationProfile, error) {
	return OrganizationProfile{}, nil
}

func (s *apiCredentialTestStore) UpdateOrganizationProfile(context.Context, Principal, UpdateOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{}, nil
}

func (s *apiCredentialTestStore) ResolveOrganizationProfile(context.Context, ResolveOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{}, nil
}

func (s *apiCredentialTestStore) GetMemberCapabilities(context.Context, string, string) (MemberCapabilitiesDocument, error) {
	return s.capabilities, nil
}

func (s *apiCredentialTestStore) PutMemberCapabilities(context.Context, MemberCapabilitiesDocument) (MemberCapabilitiesDocument, error) {
	return MemberCapabilitiesDocument{}, nil
}

func (s *apiCredentialTestStore) GetOrgACLState(context.Context, string, string) (OrgACLState, error) {
	return OrgACLState{Version: 1}, nil
}

func (s *apiCredentialTestStore) UpdateMemberRolesCommand(context.Context, UpdateMemberRolesCommand, Directory, string) (UpdateMemberRolesResult, error) {
	return UpdateMemberRolesResult{}, nil
}

func (s *apiCredentialTestStore) CreateAPICredential(_ context.Context, credential APICredential, secret APICredentialSecret) (APICredential, error) {
	if s.createErr != nil {
		return APICredential{}, s.createErr
	}
	credential.Fingerprint = secret.Fingerprint
	credential.Permissions = append([]string(nil), credential.Permissions...)
	s.created = credential
	return credential, nil
}

func (s *apiCredentialTestStore) ListAPICredentials(context.Context, string) ([]APICredential, error) {
	return nil, nil
}

func (s *apiCredentialTestStore) GetAPICredential(context.Context, string, string) (APICredential, error) {
	return s.created, nil
}

func (s *apiCredentialTestStore) ActiveAPICredentialSecrets(context.Context, string, string) ([]APICredentialSecret, error) {
	return nil, nil
}

func (s *apiCredentialTestStore) AddAPICredentialSecret(context.Context, string, string, string, APICredentialSecret) (APICredential, error) {
	return APICredential{}, nil
}

func (s *apiCredentialTestStore) RevokeAPICredential(context.Context, string, string, string, time.Time) (APICredential, error) {
	return APICredential{}, nil
}

func (s *apiCredentialTestStore) ResolveAPICredentialClaims(context.Context, string, time.Time) (ResolveAPICredentialClaimsResult, error) {
	return ResolveAPICredentialClaimsResult{}, ErrAPICredentialMissing
}

type apiCredentialTestDirectory struct {
	material            APICredentialIssuedMaterial
	deactivatedSubjects []string
}

func (d *apiCredentialTestDirectory) ListMembers(context.Context, string, string) ([]Member, error) {
	return nil, nil
}

func (d *apiCredentialTestDirectory) InviteMember(context.Context, string, string, InviteMemberRequest) (InviteMemberResult, error) {
	return InviteMemberResult{}, nil
}

func (d *apiCredentialTestDirectory) UpdateMemberRoles(context.Context, string, string, string, []string) (Member, error) {
	return Member{}, nil
}

func (d *apiCredentialTestDirectory) UpdateHumanProfile(context.Context, string, HumanProfileUpdate) (HumanProfile, error) {
	return HumanProfile{}, nil
}

func (d *apiCredentialTestDirectory) CreateServiceAccountCredential(_ context.Context, _ string, input ServiceAccountCredentialInput) (string, APICredentialIssuedMaterial, error) {
	material := d.material
	material.ClientID = input.ClientID
	return "subject-1", material, nil
}

func (d *apiCredentialTestDirectory) AddServiceAccountCredential(context.Context, AddServiceAccountCredentialInput) (APICredentialIssuedMaterial, error) {
	return d.material, nil
}

func (d *apiCredentialTestDirectory) RemoveServiceAccountCredential(context.Context, string, APICredentialSecret) error {
	return nil
}

func (d *apiCredentialTestDirectory) DeactivateServiceAccount(_ context.Context, subjectID string) error {
	d.deactivatedSubjects = append(d.deactivatedSubjects, subjectID)
	return nil
}
