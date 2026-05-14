package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Store interface {
	GetOrganizationProfile(ctx context.Context, orgID, actorID string) (OrganizationProfile, error)
	ListOrganizationMetadataByOrgIDs(ctx context.Context, orgIDs []string) ([]OrganizationMetadata, error)
	ListOrganizationMetadataByProviderOrgIDs(ctx context.Context, providerOrgIDs []string) ([]OrganizationMetadata, error)
	UpdateOrganizationProfile(ctx context.Context, principal Principal, input UpdateOrganizationRequest) (OrganizationProfile, error)
	ResolveOrganizationProfile(ctx context.Context, input ResolveOrganizationRequest) (OrganizationProfile, error)
	CreateServiceAccount(ctx context.Context, account ServiceAccount, credential APICredential, secret APICredentialSecret) (ServiceAccount, APICredential, error)
	ListServiceAccounts(ctx context.Context, orgID string) ([]ServiceAccount, error)
	GetServiceAccount(ctx context.Context, orgID, serviceAccountID string) (ServiceAccount, error)
	DisableServiceAccount(ctx context.Context, orgID, serviceAccountID, actor string, now time.Time) (ServiceAccount, []APICredential, error)
	CreateAPICredential(ctx context.Context, credential APICredential, secret APICredentialSecret) (APICredential, error)
	ListAPICredentials(ctx context.Context, orgID string) ([]APICredential, error)
	GetAPICredential(ctx context.Context, orgID, credentialID string) (APICredential, error)
	ActiveAPICredentialSecrets(ctx context.Context, orgID, credentialID string) ([]APICredentialSecret, error)
	AddAPICredentialSecret(ctx context.Context, orgID, credentialID, actor string, secret APICredentialSecret) (APICredential, error)
	RevokeAPICredential(ctx context.Context, orgID, credentialID, actor string, now time.Time) (APICredential, error)
	ResolveAPICredentialClaims(ctx context.Context, subjectID string, usedAt time.Time) (ResolveAPICredentialClaimsResult, error)
}

type Directory interface {
	ListMembers(ctx context.Context, orgID string) ([]Member, error)
	InviteMember(ctx context.Context, orgID string, input InviteMemberRequest) (InviteMemberResult, error)
	UpdateHumanProfile(ctx context.Context, subjectID string, input HumanProfileUpdate) (HumanProfile, error)
	CreateServiceAccountCredential(ctx context.Context, orgID string, input ServiceAccountCredentialInput) (subjectID string, material APICredentialIssuedMaterial, err error)
	AddServiceAccountCredential(ctx context.Context, input AddServiceAccountCredentialInput) (APICredentialIssuedMaterial, error)
	RemoveServiceAccountCredential(ctx context.Context, subjectID string, secret APICredentialSecret) error
	DeactivateServiceAccount(ctx context.Context, subjectID string) error
}

type AuthorizationGraph interface {
	LookupOrganizations(ctx context.Context, subject AuthorizationSubject, permission, minZedToken string) ([]string, string, error)
	TestOrganizationPermissions(ctx context.Context, orgID string, subject AuthorizationSubject, permissions []string, minZedToken string) ([]string, string, error)
}

type Service struct {
	Store              Store
	Directory          Directory
	AuthorizationGraph AuthorizationGraph
	ProjectID          string
	Now                func() time.Time
}

func (s *Service) Organization(ctx context.Context, principal Principal) (Organization, error) {
	if err := principal.validate(); err != nil {
		return Organization{}, err
	}
	store, err := s.store()
	if err != nil {
		return Organization{}, err
	}
	profile, err := store.GetOrganizationProfile(ctx, principal.OrgID, principal.Subject)
	if err != nil {
		return Organization{}, err
	}
	return Organization{
		OrgID:       principal.OrgID,
		DisplayName: profile.DisplayName,
		Slug:        profile.Slug,
		Version:     profile.Version,
	}, nil
}

func (s *Service) AccessibleOrganizations(ctx context.Context, subject AuthorizationSubject) ([]OrganizationMetadata, error) {
	graph, err := s.authorizationGraph()
	if err != nil {
		return nil, err
	}
	orgIDs, _, err := graph.LookupOrganizations(ctx, subject, "read", "")
	if err != nil {
		return nil, err
	}
	orgIDs = normalizeOrganizationIDs(orgIDs)
	if len(orgIDs) == 0 {
		return []OrganizationMetadata{}, nil
	}
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	organizations, err := store.ListOrganizationMetadataByOrgIDs(ctx, orgIDs)
	if err != nil {
		return nil, err
	}
	if len(organizations) != len(orgIDs) {
		return nil, fmt.Errorf("%w: organization metadata is missing for one or more policy grants", ErrOrganizationMissing)
	}
	return organizations, nil
}

func (s *Service) UpdateOrganization(ctx context.Context, principal Principal, input UpdateOrganizationRequest) (Organization, error) {
	if err := principal.validate(); err != nil {
		return Organization{}, err
	}
	store, err := s.store()
	if err != nil {
		return Organization{}, err
	}
	if _, err := store.UpdateOrganizationProfile(ctx, principal, input); err != nil {
		return Organization{}, err
	}
	return s.Organization(ctx, principal)
}

func (s *Service) ResolveOrganization(ctx context.Context, input ResolveOrganizationRequest) (OrganizationProfile, error) {
	if strings.TrimSpace(input.OrgID) == "" && strings.TrimSpace(input.IdentityProviderOrgID) == "" && strings.TrimSpace(input.Slug) == "" {
		return OrganizationProfile{}, fmt.Errorf("%w: org_id, identity_provider_org_id, or slug is required", ErrInvalidInput)
	}
	store, err := s.store()
	if err != nil {
		return OrganizationProfile{}, err
	}
	return store.ResolveOrganizationProfile(ctx, input)
}

func (s *Service) Members(ctx context.Context, principal Principal) ([]Member, error) {
	if err := principal.validate(); err != nil {
		return nil, err
	}
	members, err := s.members(ctx, principal.OrgID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(members))
	for _, member := range members {
		if member.Type == MemberTypeMachine {
			continue
		}
		out = append(out, member)
	}
	return out, nil
}

func (s *Service) InviteMember(ctx context.Context, principal Principal, input InviteMemberRequest) (InviteMemberResult, error) {
	if err := principal.validate(); err != nil {
		return InviteMemberResult{}, err
	}
	if err := validateInvite(input); err != nil {
		return InviteMemberResult{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return InviteMemberResult{}, err
	}
	providerOrgID, err := s.providerOrgID(ctx, principal.OrgID, principal.Subject)
	if err != nil {
		return InviteMemberResult{}, err
	}
	result, err := directory.InviteMember(ctx, providerOrgID, normalizeInvite(input))
	if err != nil {
		return InviteMemberResult{}, err
	}
	return result, nil
}

func (s *Service) UpdateHumanProfile(ctx context.Context, subjectID string, input HumanProfileUpdate) (HumanProfile, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return HumanProfile{}, fmt.Errorf("%w: subject_id is required", ErrInvalidInput)
	}
	input, err := normalizeHumanProfileUpdate(input)
	if err != nil {
		return HumanProfile{}, err
	}
	directory, err := s.directoryClient()
	if err != nil {
		return HumanProfile{}, err
	}
	return directory.UpdateHumanProfile(ctx, subjectID, input)
}

func (s *Service) ListServiceAccounts(ctx context.Context, principal Principal) ([]ServiceAccount, error) {
	if err := principal.validate(); err != nil {
		return nil, err
	}
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	accounts, err := store.ListServiceAccounts(ctx, principal.OrgID)
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

func (s *Service) GetServiceAccount(ctx context.Context, principal Principal, serviceAccountID string) (ServiceAccount, error) {
	if err := principal.validate(); err != nil {
		return ServiceAccount{}, err
	}
	serviceAccountID = strings.TrimSpace(serviceAccountID)
	if serviceAccountID == "" {
		return ServiceAccount{}, fmt.Errorf("%w: service_account_id is required", ErrInvalidInput)
	}
	store, err := s.store()
	if err != nil {
		return ServiceAccount{}, err
	}
	account, err := store.GetServiceAccount(ctx, principal.OrgID, serviceAccountID)
	if err != nil {
		return ServiceAccount{}, err
	}
	return account, nil
}

func (s *Service) DisableServiceAccount(ctx context.Context, principal Principal, serviceAccountID string) (DisableServiceAccountResult, error) {
	if err := principal.validate(); err != nil {
		return DisableServiceAccountResult{}, err
	}
	serviceAccountID = strings.TrimSpace(serviceAccountID)
	if serviceAccountID == "" {
		return DisableServiceAccountResult{}, fmt.Errorf("%w: service_account_id is required", ErrInvalidInput)
	}
	store, err := s.store()
	if err != nil {
		return DisableServiceAccountResult{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return DisableServiceAccountResult{}, err
	}
	account, err := store.GetServiceAccount(ctx, principal.OrgID, serviceAccountID)
	if err != nil {
		return DisableServiceAccountResult{}, err
	}
	if err := directory.DeactivateServiceAccount(ctx, account.SubjectID); err != nil {
		return DisableServiceAccountResult{}, err
	}
	account, credentials, err := store.DisableServiceAccount(ctx, principal.OrgID, serviceAccountID, principal.Subject, s.now())
	if err != nil {
		return DisableServiceAccountResult{}, err
	}
	return DisableServiceAccountResult{ServiceAccount: account, Credentials: credentials}, nil
}

func (s *Service) ListAPICredentials(ctx context.Context, principal Principal) ([]APICredential, error) {
	if err := principal.validate(); err != nil {
		return nil, err
	}
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	credentials, err := store.ListAPICredentials(ctx, principal.OrgID)
	if err != nil {
		return nil, err
	}
	return credentials, nil
}

func (s *Service) GetAPICredential(ctx context.Context, principal Principal, credentialID string) (APICredential, error) {
	if err := principal.validate(); err != nil {
		return APICredential{}, err
	}
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" {
		return APICredential{}, fmt.Errorf("%w: credential_id is required", ErrInvalidInput)
	}
	store, err := s.store()
	if err != nil {
		return APICredential{}, err
	}
	credential, err := store.GetAPICredential(ctx, principal.OrgID, credentialID)
	if err != nil {
		return APICredential{}, err
	}
	return credential, nil
}

func (s *Service) CreateAPICredential(ctx context.Context, principal Principal, input CreateAPICredentialRequest) (CreateAPICredentialResult, error) {
	if err := principal.validate(); err != nil {
		return CreateAPICredentialResult{}, err
	}
	input, err := s.normalizeCreateAPICredentialRequest(input)
	if err != nil {
		return CreateAPICredentialResult{}, err
	}
	store, err := s.store()
	if err != nil {
		return CreateAPICredentialResult{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return CreateAPICredentialResult{}, err
	}
	now := s.now()
	credentialID := uuid.NewString()
	serviceAccountID := strings.TrimSpace(input.ServiceAccountID)
	createServiceAccount := serviceAccountID == ""
	var account ServiceAccount
	var material APICredentialIssuedMaterial
	clientID := ""
	subjectID := ""
	if createServiceAccount {
		serviceAccountID = uuid.NewString()
		clientID = "verself-sa-" + strings.ReplaceAll(serviceAccountID, "-", "")
		account = ServiceAccount{
			ServiceAccountID: serviceAccountID,
			OrgID:            principal.OrgID,
			ClientID:         clientID,
			DisplayName:      input.DisplayName,
			Description:      input.Description,
			Status:           ServiceAccountStatusActive,
			CreatedAt:        now,
			CreatedBy:        principal.Subject,
			UpdatedAt:        now,
		}
		providerOrgID, err := s.providerOrgID(ctx, principal.OrgID, principal.Subject)
		if err != nil {
			return CreateAPICredentialResult{}, err
		}
		subjectID, material, err = directory.CreateServiceAccountCredential(ctx, providerOrgID, ServiceAccountCredentialInput{
			CredentialID: credentialID,
			ClientID:     clientID,
			DisplayName:  input.DisplayName,
			AuthMethod:   input.AuthMethod,
			ExpiresAt:    input.ExpiresAt,
		})
		if err != nil {
			return CreateAPICredentialResult{}, err
		}
		account.SubjectID = subjectID
	} else {
		account, err = store.GetServiceAccount(ctx, principal.OrgID, serviceAccountID)
		if err != nil {
			return CreateAPICredentialResult{}, err
		}
		if account.Status != ServiceAccountStatusActive {
			return CreateAPICredentialResult{}, fmt.Errorf("%w: service account is not active", ErrInvalidInput)
		}
		subjectID = account.SubjectID
		clientID = account.ClientID
		material, err = directory.AddServiceAccountCredential(ctx, AddServiceAccountCredentialInput{
			SubjectID:  account.SubjectID,
			ClientID:   account.ClientID,
			AuthMethod: input.AuthMethod,
			ExpiresAt:  input.ExpiresAt,
		})
		if err != nil {
			return CreateAPICredentialResult{}, err
		}
	}
	credential := APICredential{
		CredentialID:     credentialID,
		ServiceAccountID: serviceAccountID,
		OrgID:            principal.OrgID,
		ClientID:         clientID,
		DisplayName:      input.DisplayName,
		Status:           APICredentialStatusActive,
		AuthMethod:       input.AuthMethod,
		CreatedAt:        now,
		CreatedBy:        principal.Subject,
		UpdatedAt:        now,
		ExpiresAt:        input.ExpiresAt,
	}
	if err := validateIssuedMaterial(input.AuthMethod, material); err != nil {
		cleanupErr := cleanupIssuedCredential(ctx, directory, createServiceAccount, subjectID, APICredentialSecret{})
		return CreateAPICredentialResult{}, errors.Join(err, cleanupErr)
	}
	credential.SubjectID = subjectID
	credential.ClientID = firstNonEmpty(material.ClientID, clientID)
	account.ClientID = credential.ClientID
	secret := credentialSecretFromMaterial(credential, material, principal.Subject, now, input.ExpiresAt)
	credential.Fingerprint = secret.Fingerprint
	if createServiceAccount {
		account, credential, err = store.CreateServiceAccount(ctx, account, credential, secret)
	} else {
		credential, err = store.CreateAPICredential(ctx, credential, secret)
	}
	if err != nil {
		cleanupErr := cleanupIssuedCredential(ctx, directory, createServiceAccount, subjectID, secret)
		return CreateAPICredentialResult{}, errors.Join(err, cleanupErr)
	}
	material.Fingerprint = credential.Fingerprint
	material.ClientID = credential.ClientID
	return CreateAPICredentialResult{Credential: credential, IssuedMaterial: material}, nil
}

func (s *Service) RollAPICredential(ctx context.Context, principal Principal, credentialID string, input RollAPICredentialRequest) (RollAPICredentialResult, error) {
	if err := principal.validate(); err != nil {
		return RollAPICredentialResult{}, err
	}
	credential, err := s.GetAPICredential(ctx, principal, credentialID)
	if err != nil {
		return RollAPICredentialResult{}, err
	}
	if credential.Status != APICredentialStatusActive {
		return RollAPICredentialResult{}, fmt.Errorf("%w: credential is not active", ErrInvalidInput)
	}
	input.AuthMethod = normalizeAuthMethod(firstNonEmpty(string(input.AuthMethod), string(credential.AuthMethod)))
	if err := validateAuthMethod(input.AuthMethod); err != nil {
		return RollAPICredentialResult{}, err
	}
	store, err := s.store()
	if err != nil {
		return RollAPICredentialResult{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return RollAPICredentialResult{}, err
	}
	oldSecrets, err := store.ActiveAPICredentialSecrets(ctx, principal.OrgID, credential.CredentialID)
	if err != nil {
		return RollAPICredentialResult{}, err
	}
	material, err := directory.AddServiceAccountCredential(ctx, AddServiceAccountCredentialInput{
		SubjectID:  credential.SubjectID,
		ClientID:   credential.ClientID,
		AuthMethod: input.AuthMethod,
		ExpiresAt:  credential.ExpiresAt,
	})
	if err != nil {
		return RollAPICredentialResult{}, err
	}
	now := s.now()
	secret := credentialSecretFromMaterial(credential, material, principal.Subject, now, credential.ExpiresAt)
	cleanupNewSecret := func(cause error) error {
		cleanupErr := directory.RemoveServiceAccountCredential(ctx, credential.SubjectID, secret)
		return errors.Join(cause, cleanupErr)
	}
	if err := validateIssuedMaterial(input.AuthMethod, material); err != nil {
		return RollAPICredentialResult{}, cleanupNewSecret(err)
	}
	for _, old := range oldSecrets {
		// Zitadel exposes one machine-user client secret; deleting after AddSecret would delete the new secret.
		if old.AuthMethod == APICredentialAuthMethodClientSecret || old.ProviderKeyID == material.KeyID {
			continue
		}
		if err := directory.RemoveServiceAccountCredential(ctx, credential.SubjectID, old); err != nil {
			return RollAPICredentialResult{}, cleanupNewSecret(err)
		}
	}
	credential, err = store.AddAPICredentialSecret(ctx, principal.OrgID, credential.CredentialID, principal.Subject, secret)
	if err != nil {
		return RollAPICredentialResult{}, cleanupNewSecret(err)
	}
	material.Fingerprint = credential.Fingerprint
	material.ClientID = credential.ClientID
	return RollAPICredentialResult{Credential: credential, IssuedMaterial: material}, nil
}

func (s *Service) RevokeAPICredential(ctx context.Context, principal Principal, credentialID string) (APICredential, error) {
	if err := principal.validate(); err != nil {
		return APICredential{}, err
	}
	credential, err := s.GetAPICredential(ctx, principal, credentialID)
	if err != nil {
		return APICredential{}, err
	}
	store, err := s.store()
	if err != nil {
		return APICredential{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return APICredential{}, err
	}
	secrets, err := store.ActiveAPICredentialSecrets(ctx, principal.OrgID, credential.CredentialID)
	if err != nil {
		return APICredential{}, err
	}
	for _, secret := range secrets {
		if err := directory.RemoveServiceAccountCredential(ctx, credential.SubjectID, secret); err != nil {
			return APICredential{}, err
		}
	}
	return store.RevokeAPICredential(ctx, principal.OrgID, credential.CredentialID, principal.Subject, s.now())
}

func (s *Service) ResolveAPICredentialClaims(ctx context.Context, subjectID string) (ResolveAPICredentialClaimsResult, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return ResolveAPICredentialClaimsResult{}, fmt.Errorf("%w: subject_id is required", ErrInvalidInput)
	}
	store, err := s.store()
	if err != nil {
		return ResolveAPICredentialClaimsResult{}, err
	}
	result, err := store.ResolveAPICredentialClaims(ctx, subjectID, s.now())
	if err != nil {
		return ResolveAPICredentialClaimsResult{}, err
	}
	return result, nil
}

func (s *Service) members(ctx context.Context, orgID string) ([]Member, error) {
	directory, err := s.directory()
	if err != nil {
		return nil, err
	}
	providerOrgID, err := s.providerOrgID(ctx, orgID, "system:directory")
	if err != nil {
		return nil, err
	}
	return directory.ListMembers(ctx, providerOrgID)
}

func (s *Service) providerOrgID(ctx context.Context, orgID, actor string) (string, error) {
	store, err := s.store()
	if err != nil {
		return "", err
	}
	profile, err := store.GetOrganizationProfile(ctx, orgID, actor)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(profile.IdentityProviderOrgID) == "" {
		return "", fmt.Errorf("%w: identity_provider_org_id is required", ErrInvalidInput)
	}
	return strings.TrimSpace(profile.IdentityProviderOrgID), nil
}

func (s *Service) store() (Store, error) {
	if s == nil || s.Store == nil {
		return nil, ErrStoreUnavailable
	}
	return s.Store, nil
}

func (s *Service) authorizationGraph() (AuthorizationGraph, error) {
	if s == nil || s.AuthorizationGraph == nil {
		return nil, ErrStoreUnavailable
	}
	return s.AuthorizationGraph, nil
}

func (s *Service) directory() (Directory, error) {
	return s.directoryClient()
}

func (s *Service) directoryClient() (Directory, error) {
	if s == nil || s.Directory == nil {
		return nil, ErrZitadelUnavailable
	}
	return s.Directory, nil
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (p Principal) validate() error {
	if strings.TrimSpace(p.Subject) == "" {
		return fmt.Errorf("%w: subject is required", ErrInvalidInput)
	}
	if strings.TrimSpace(p.OrgID) == "" {
		return fmt.Errorf("%w: org_id is required", ErrInvalidInput)
	}
	return nil
}

func validateInvite(input InviteMemberRequest) error {
	if _, err := mail.ParseAddress(strings.TrimSpace(input.Email)); err != nil {
		return fmt.Errorf("%w: email is invalid", ErrInvalidInput)
	}
	return nil
}

func normalizeHumanProfileUpdate(input HumanProfileUpdate) (HumanProfileUpdate, error) {
	input.GivenName = strings.TrimSpace(input.GivenName)
	input.FamilyName = strings.TrimSpace(input.FamilyName)
	if input.GivenName == "" {
		return HumanProfileUpdate{}, fmt.Errorf("%w: given_name is required", ErrInvalidInput)
	}
	if input.FamilyName == "" {
		return HumanProfileUpdate{}, fmt.Errorf("%w: family_name is required", ErrInvalidInput)
	}
	if len(input.GivenName) > 100 {
		return HumanProfileUpdate{}, fmt.Errorf("%w: given_name is too long", ErrInvalidInput)
	}
	if len(input.FamilyName) > 100 {
		return HumanProfileUpdate{}, fmt.Errorf("%w: family_name is too long", ErrInvalidInput)
	}
	if hasProfileControlCharacter(input.GivenName) || hasProfileControlCharacter(input.FamilyName) {
		return HumanProfileUpdate{}, fmt.Errorf("%w: profile name contains unsupported control characters", ErrInvalidInput)
	}
	if input.DisplayName != nil {
		displayName := strings.TrimSpace(*input.DisplayName)
		if len(displayName) > 200 {
			return HumanProfileUpdate{}, fmt.Errorf("%w: display_name is too long", ErrInvalidInput)
		}
		if hasProfileControlCharacter(displayName) {
			return HumanProfileUpdate{}, fmt.Errorf("%w: display_name contains unsupported control characters", ErrInvalidInput)
		}
		input.DisplayName = &displayName
	}
	return input, nil
}

func hasProfileControlCharacter(value string) bool {
	return strings.ContainsAny(value, "\x00\r\n")
}

func normalizeInvite(input InviteMemberRequest) InviteMemberRequest {
	input.Email = strings.TrimSpace(input.Email)
	input.GivenName = strings.TrimSpace(input.GivenName)
	input.FamilyName = strings.TrimSpace(input.FamilyName)
	return input
}

func normalizeOrganizationIDs(orgIDs []string) []string {
	if len(orgIDs) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		orgID = strings.TrimSpace(orgID)
		if orgID == "" {
			continue
		}
		if _, ok := seen[orgID]; ok {
			continue
		}
		seen[orgID] = struct{}{}
		out = append(out, orgID)
	}
	sort.Strings(out)
	return out
}

func (s *Service) normalizeCreateAPICredentialRequest(input CreateAPICredentialRequest) (CreateAPICredentialRequest, error) {
	input.ServiceAccountID = strings.TrimSpace(input.ServiceAccountID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" {
		return CreateAPICredentialRequest{}, fmt.Errorf("%w: display_name is required", ErrInvalidInput)
	}
	if len(input.DisplayName) > 200 {
		return CreateAPICredentialRequest{}, fmt.Errorf("%w: display_name is too long", ErrInvalidInput)
	}
	input.Description = strings.TrimSpace(input.Description)
	if len(input.Description) > 1000 {
		return CreateAPICredentialRequest{}, fmt.Errorf("%w: description is too long", ErrInvalidInput)
	}
	input.AuthMethod = normalizeAuthMethod(string(input.AuthMethod))
	if err := validateAuthMethod(input.AuthMethod); err != nil {
		return CreateAPICredentialRequest{}, err
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(s.now()) {
		return CreateAPICredentialRequest{}, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidInput)
	}
	return input, nil
}

func normalizeAuthMethod(value string) APICredentialAuthMethod {
	value = strings.TrimSpace(value)
	if value == "" {
		return APICredentialAuthMethodPrivateKeyJWT
	}
	return APICredentialAuthMethod(value)
}

func validateAuthMethod(method APICredentialAuthMethod) error {
	switch method {
	case APICredentialAuthMethodPrivateKeyJWT, APICredentialAuthMethodClientSecret:
		return nil
	default:
		return fmt.Errorf("%w: unsupported auth_method %q", ErrInvalidInput, method)
	}
}

func validateIssuedMaterial(expected APICredentialAuthMethod, material APICredentialIssuedMaterial) error {
	if material.AuthMethod != expected {
		return fmt.Errorf("%w: issued material auth_method %q does not match requested %q", ErrZitadelUnavailable, material.AuthMethod, expected)
	}
	if strings.TrimSpace(material.TokenURL) == "" {
		return fmt.Errorf("%w: issued material missing token_url", ErrZitadelUnavailable)
	}
	switch expected {
	case APICredentialAuthMethodPrivateKeyJWT:
		if strings.TrimSpace(material.KeyID) == "" || strings.TrimSpace(material.KeyContent) == "" {
			return fmt.Errorf("%w: issued private-key JWT material is incomplete", ErrZitadelUnavailable)
		}
	case APICredentialAuthMethodClientSecret:
		if strings.TrimSpace(material.ClientSecret) == "" {
			return fmt.Errorf("%w: issued client-secret material is incomplete", ErrZitadelUnavailable)
		}
	default:
		return validateAuthMethod(expected)
	}
	return nil
}

func cleanupIssuedCredential(ctx context.Context, directory Directory, deactivateSubject bool, subjectID string, secret APICredentialSecret) error {
	if directory == nil || strings.TrimSpace(subjectID) == "" {
		return nil
	}
	if deactivateSubject {
		return directory.DeactivateServiceAccount(ctx, subjectID)
	}
	if strings.TrimSpace(secret.SecretID) == "" {
		return nil
	}
	return directory.RemoveServiceAccountCredential(ctx, subjectID, secret)
}

func credentialSecretFromMaterial(credential APICredential, material APICredentialIssuedMaterial, actor string, now time.Time, expiresAt *time.Time) APICredentialSecret {
	secretText := firstNonEmpty(material.KeyContent, material.ClientSecret, material.KeyID)
	fingerprint, rawHash := SecretHash(secretText)
	providerKeyID := firstNonEmpty(material.KeyID, material.ClientID)
	return APICredentialSecret{
		SecretID:      uuid.NewString(),
		CredentialID:  credential.CredentialID,
		AuthMethod:    material.AuthMethod,
		ProviderKeyID: providerKeyID,
		Fingerprint:   fingerprint,
		SecretHash:    rawHash,
		HashAlgorithm: "sha256",
		CreatedAt:     now,
		CreatedBy:     actor,
		ExpiresAt:     expiresAt,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func IsInvalid(err error) bool {
	return errors.Is(err, ErrInvalidInput)
}
