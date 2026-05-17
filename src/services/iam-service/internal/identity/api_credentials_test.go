package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateAPICredentialCreatesServiceAccountCredential(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	svc := &Service{
		Store:     &apiCredentialTestStore{},
		Directory: &apiCredentialTestDirectory{material: testIssuedMaterial(APICredentialAuthMethodPrivateKeyJWT, "client-1")},
		Now:       func() time.Time { return now },
	}

	result, err := svc.CreateAPICredential(context.Background(), Principal{
		Subject: "owner-1",
		OrgID:   "org_01J8QJ4P1R7S9W2X5M6N8P0Q2",
	}, CreateAPICredentialRequest{
		DisplayName: "sandbox automation",
	})
	if err != nil {
		t.Fatalf("CreateAPICredential: %v", err)
	}
	if result.Credential.OrgID != "org_01J8QJ4P1R7S9W2X5M6N8P0Q2" || result.Credential.SubjectID != "subject-1" || result.Credential.ServiceAccountID == "" {
		t.Fatalf("unexpected credential: %#v", result.Credential)
	}
	if result.IssuedMaterial.KeyContent == "" || result.IssuedMaterial.Fingerprint == "" {
		t.Fatalf("issued material was not returned once: %#v", result.IssuedMaterial)
	}
}

func TestCreateAPICredentialCleansUpServiceAccountWhenStoreFails(t *testing.T) {
	storeErr := errors.New("store failed")
	directory := &apiCredentialTestDirectory{material: testIssuedMaterial(APICredentialAuthMethodPrivateKeyJWT, "client-1")}
	svc := &Service{
		Store:     &apiCredentialTestStore{createErr: storeErr},
		Directory: directory,
		Now:       func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	_, err := svc.CreateAPICredential(context.Background(), Principal{
		Subject: "owner-1",
		OrgID:   "org_01J8QJ4P1R7S9W2X5M6N8P0Q2",
	}, CreateAPICredentialRequest{
		DisplayName: "sandbox automation",
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
	created   APICredential
	createErr error
}

func (s *apiCredentialTestStore) GetOrganizationProfile(context.Context, string, string) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 1}, nil
}

func (s *apiCredentialTestStore) ListOrganizationMetadataByOrgIDs(_ context.Context, orgIDs []string) ([]OrganizationMetadata, error) {
	out := make([]OrganizationMetadata, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		out = append(out, OrganizationMetadata{OrgID: orgID, IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", Version: 1})
	}
	return out, nil
}

func (s *apiCredentialTestStore) ListOrganizationMetadataByProviderOrgIDs(_ context.Context, providerOrgIDs []string) ([]OrganizationMetadata, error) {
	out := make([]OrganizationMetadata, 0, len(providerOrgIDs))
	for _, providerOrgID := range providerOrgIDs {
		out = append(out, OrganizationMetadata{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: providerOrgID, DisplayName: "Acme", Slug: "acme", Version: 1})
	}
	return out, nil
}

func (s *apiCredentialTestStore) OrganizationSlugAvailable(context.Context, string) (bool, error) {
	return true, nil
}

func (s *apiCredentialTestStore) CreateOrganizationProfile(_ context.Context, input CreateOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: input.OrgID, IdentityProviderOrgID: input.IdentityProviderOrgID, DisplayName: input.DisplayName, Slug: input.Slug, State: OrganizationProfileStateActive, Version: 1}, nil
}

func (s *apiCredentialTestStore) UpdateOrganizationProfile(context.Context, Principal, UpdateOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{}, nil
}

func (s *apiCredentialTestStore) ResolveOrganizationProfile(context.Context, ResolveOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 1}, nil
}

func (s *apiCredentialTestStore) CreateServiceAccount(_ context.Context, account ServiceAccount, credential APICredential, secret APICredentialSecret) (ServiceAccount, APICredential, error) {
	if s.createErr != nil {
		return ServiceAccount{}, APICredential{}, s.createErr
	}
	account.SubjectID = credential.SubjectID
	account.ClientID = credential.ClientID
	credential.Fingerprint = secret.Fingerprint
	s.created = credential
	return account, credential, nil
}

func (s *apiCredentialTestStore) ListServiceAccounts(context.Context, string) ([]ServiceAccount, error) {
	return nil, nil
}

func (s *apiCredentialTestStore) GetServiceAccount(context.Context, string, string) (ServiceAccount, error) {
	return ServiceAccount{}, ErrAPICredentialMissing
}

func (s *apiCredentialTestStore) DisableServiceAccount(context.Context, string, string, string, time.Time) (ServiceAccount, []APICredential, error) {
	return ServiceAccount{}, nil, nil
}

func (s *apiCredentialTestStore) CreateAPICredential(_ context.Context, credential APICredential, secret APICredentialSecret) (APICredential, error) {
	if s.createErr != nil {
		return APICredential{}, s.createErr
	}
	credential.Fingerprint = secret.Fingerprint
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

func (d *apiCredentialTestDirectory) CreateOrganization(context.Context, DirectoryCreateOrganizationRequest) (DirectoryCreateOrganizationResult, error) {
	return DirectoryCreateOrganizationResult{OrganizationID: "43"}, nil
}

func (d *apiCredentialTestDirectory) ListMembers(context.Context, string) ([]Member, error) {
	return nil, nil
}

func (d *apiCredentialTestDirectory) InviteMember(context.Context, string, InviteMemberRequest) (InviteMemberResult, error) {
	return InviteMemberResult{}, nil
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
