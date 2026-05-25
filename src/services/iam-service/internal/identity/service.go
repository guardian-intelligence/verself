package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
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
	OrganizationSlugAvailable(ctx context.Context, slug string) (bool, error)
	CreateOrganizationProfile(ctx context.Context, input CreateOrganizationRequest) (OrganizationProfile, error)
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
	CreateMemberInviteAcceptance(ctx context.Context, invite MemberInviteAcceptance) error
	GetMemberInviteAcceptance(ctx context.Context, tokenHash string, now time.Time) (MemberInviteAcceptance, error)
	AcceptMemberInviteAcceptance(ctx context.Context, tokenHash string, now time.Time) error
}

type SignupStore interface {
	StartSignupIntent(ctx context.Context, intent SignupIntent, now time.Time) (SignupStartDecision, error)
	DeletePendingSignupIntent(ctx context.Context, signupIntentID string) error
	ClaimSignupIntentForVerification(ctx context.Context, signupIntentID string, verificationTokenHash []byte, idempotencyKey string, verifyRequestHash []byte, organizationDisplayName string, requestedOrganizationSlug string, now time.Time, leaseExpiresAt time.Time) (SignupIntent, error)
	RecordSignupIntentStep(ctx context.Context, signupIntentID, step string, leaseExpiresAt time.Time) error
	RecordSignupIntentProviderOrg(ctx context.Context, signupIntentID, providerOrgID string) error
	RecordSignupIntentProviderUser(ctx context.Context, signupIntentID, providerUserID string) error
	RecordSignupIntentOrganization(ctx context.Context, signupIntentID, orgID, organizationSlug string) error
	MarkSignupIntentFailed(ctx context.Context, signupIntentID string, state SignupIntentState, message string) error
	CompleteSignupIntent(ctx context.Context, signupIntentID string, completedAt time.Time) (SignupIntent, error)
	AppendIAMEvent(ctx context.Context, event IAMEvent) error
}

type Directory interface {
	CreateOrganization(ctx context.Context, input DirectoryCreateOrganizationRequest) (DirectoryCreateOrganizationResult, error)
	ListMembers(ctx context.Context, orgID string) ([]Member, error)
	InviteMember(ctx context.Context, orgID string, input InviteMemberRequest) (DirectoryInviteMemberResult, error)
	CompleteMemberInvite(ctx context.Context, input DirectoryCompleteMemberInviteRequest) error
	UpdateHumanProfile(ctx context.Context, subjectID string, input HumanProfileUpdate) (HumanProfile, error)
	CreateServiceAccountCredential(ctx context.Context, orgID string, input ServiceAccountCredentialInput) (subjectID string, material APICredentialIssuedMaterial, err error)
	AddServiceAccountCredential(ctx context.Context, input AddServiceAccountCredentialInput) (APICredentialIssuedMaterial, error)
	RemoveServiceAccountCredential(ctx context.Context, subjectID string, secret APICredentialSecret) error
	DeactivateServiceAccount(ctx context.Context, subjectID string) error
}

type SignupDirectory interface {
	CreateOrganization(ctx context.Context, input DirectoryCreateOrganizationRequest) (DirectoryCreateOrganizationResult, error)
	CreateSignupUser(ctx context.Context, input DirectoryCreateSignupUserRequest) (DirectoryCreateSignupUserResult, error)
}

type AuthorizationGraph interface {
	LookupOrganizations(ctx context.Context, subject AuthorizationSubject, permission, minZedToken string) ([]string, string, error)
	TestOrganizationPermissions(ctx context.Context, orgID string, subject AuthorizationSubject, permissions []string, minZedToken string) ([]string, string, error)
}

type OrganizationOwnerPolicyWriter interface {
	SetOrganizationOwner(ctx context.Context, input OrganizationOwnerPolicyRequest) error
}

type BillingProvisioner interface {
	EnsureBillingOrganization(ctx context.Context, input BillingOrganizationProvisioningRequest) error
}

type Service struct {
	Store              Store
	Directory          Directory
	AuthorizationGraph AuthorizationGraph
	PolicyWriter       OrganizationOwnerPolicyWriter
	Billing            BillingProvisioner
	ProjectID          string
	EmailIdentityKey   []byte
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
	if missing := missingOrganizationMetadataIDs(orgIDs, organizations); len(missing) > 0 {
		slog.Default().WarnContext(ctx, "organization metadata missing for authorization graph result", "missing_org_ids", missing)
	}
	return organizations, nil
}

func missingOrganizationMetadataIDs(orgIDs []string, organizations []OrganizationMetadata) []string {
	seen := map[string]struct{}{}
	for _, organization := range organizations {
		seen[organization.OrgID] = struct{}{}
	}
	missing := []string{}
	for _, orgID := range orgIDs {
		if _, ok := seen[orgID]; !ok {
			missing = append(missing, orgID)
		}
	}
	return missing
}

func (s *Service) CreateOrganization(ctx context.Context, subjectID string, input PublicCreateOrganizationRequest) (Organization, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return Organization{}, fmt.Errorf("%w: subject_id is required", ErrInvalidInput)
	}
	input, err := normalizePublicCreateOrganizationRequest(input)
	if err != nil {
		return Organization{}, err
	}
	store, err := s.store()
	if err != nil {
		return Organization{}, err
	}
	slug, err := s.availableOrganizationSlug(ctx, store, input.Slug, input.DisplayName)
	if err != nil {
		return Organization{}, err
	}
	orgID, err := randomOrganizationID()
	if err != nil {
		return Organization{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return Organization{}, err
	}
	provider, err := directory.CreateOrganization(ctx, DirectoryCreateOrganizationRequest{
		Name:        slug + "-" + strings.TrimPrefix(orgID, organizationPublicIDPrefix),
		AdminUserID: subjectID,
	})
	if err != nil {
		return Organization{}, err
	}
	profile, err := store.CreateOrganizationProfile(ctx, CreateOrganizationRequest{
		OrgID:                 orgID,
		IdentityProviderOrgID: provider.OrganizationID,
		DisplayName:           input.DisplayName,
		Slug:                  slug,
		ActorID:               subjectID,
	})
	if err != nil {
		return Organization{}, err
	}
	policyWriter, err := s.policyWriter()
	if err != nil {
		return Organization{}, err
	}
	if err := policyWriter.SetOrganizationOwner(ctx, OrganizationOwnerPolicyRequest{
		OrgID:       profile.OrgID,
		OwnerUserID: subjectID,
		OperationID: "create-organization",
	}); err != nil {
		return Organization{}, err
	}
	billing, err := s.billing()
	if err != nil {
		return Organization{}, err
	}
	if err := billing.EnsureBillingOrganization(ctx, BillingOrganizationProvisioningRequest{
		OrgID:       profile.OrgID,
		DisplayName: profile.DisplayName,
		TrustTier:   "new",
	}); err != nil {
		return Organization{}, err
	}
	return Organization{
		OrgID:       profile.OrgID,
		DisplayName: profile.DisplayName,
		Slug:        profile.Slug,
		Version:     profile.Version,
	}, nil
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

func (s *Service) OrganizationSlugAvailability(ctx context.Context, slug string) (bool, error) {
	slug = normalizeSlug(slug)
	if err := validateSlug("slug", slug); err != nil {
		return false, err
	}
	store, err := s.store()
	if err != nil {
		return false, err
	}
	return store.OrganizationSlugAvailable(ctx, slug)
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
	store, err := s.store()
	if err != nil {
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
	directoryResult, err := directory.InviteMember(ctx, providerOrgID, normalizeInvite(input))
	if err != nil {
		return InviteMemberResult{}, err
	}
	token, tokenHash, err := randomMemberInviteAcceptanceToken()
	if err != nil {
		return InviteMemberResult{}, err
	}
	now := s.now()
	expiresAt := now.Add(72 * time.Hour)
	if err := store.CreateMemberInviteAcceptance(ctx, MemberInviteAcceptance{
		TokenHash:             tokenHash,
		OrgID:                 principal.OrgID,
		UserID:                directoryResult.UserID,
		Email:                 directoryResult.Email,
		EmailVerificationCode: directoryResult.EmailVerificationCode,
		CreatedAt:             now,
		ExpiresAt:             expiresAt,
	}); err != nil {
		return InviteMemberResult{}, err
	}
	return InviteMemberResult{
		UserID:              directoryResult.UserID,
		Email:               directoryResult.Email,
		Status:              directoryResult.Status,
		Roles:               normalizeInviteRoles(input.Roles),
		AcceptanceToken:     token,
		AcceptanceExpiresAt: expiresAt,
	}, nil
}

func (s *Service) CompleteMemberInvite(ctx context.Context, input CompleteMemberInviteRequest) (MemberInviteAcceptance, error) {
	input, err := normalizeCompleteMemberInvite(input)
	if err != nil {
		return MemberInviteAcceptance{}, err
	}
	store, err := s.store()
	if err != nil {
		return MemberInviteAcceptance{}, err
	}
	tokenHash := memberInviteAcceptanceTokenHash(input.AcceptanceToken)
	now := s.now()
	acceptance, err := store.GetMemberInviteAcceptance(ctx, tokenHash, now)
	if err != nil {
		return MemberInviteAcceptance{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return MemberInviteAcceptance{}, err
	}
	if err := directory.CompleteMemberInvite(ctx, DirectoryCompleteMemberInviteRequest{
		UserID:                acceptance.UserID,
		EmailVerificationCode: acceptance.EmailVerificationCode,
	}); err != nil {
		return MemberInviteAcceptance{}, err
	}
	acceptedAt := s.now()
	if err := store.AcceptMemberInviteAcceptance(ctx, tokenHash, acceptedAt); err != nil {
		return MemberInviteAcceptance{}, err
	}
	acceptance.AcceptedAt = &acceptedAt
	return acceptance, nil
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

func normalizePublicCreateOrganizationRequest(input PublicCreateOrganizationRequest) (PublicCreateOrganizationRequest, error) {
	input.DisplayName = normalizeHumanText(input.DisplayName)
	input.Slug = normalizeSlug(input.Slug)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if err := validateHumanText("display_name", input.DisplayName, 1, 120, 240); err != nil {
		return PublicCreateOrganizationRequest{}, err
	}
	if input.Slug != "" {
		if err := validateSlug("slug", input.Slug); err != nil {
			return PublicCreateOrganizationRequest{}, err
		}
	}
	if input.IdempotencyKey == "" {
		return PublicCreateOrganizationRequest{}, fmt.Errorf("%w: idempotency_key is required", ErrInvalidInput)
	}
	return input, nil
}

func (s *Service) availableOrganizationSlug(ctx context.Context, store Store, requested, displayName string) (string, error) {
	base := normalizeSlug(firstNonEmpty(requested, displayName, "organization"))
	if base == "" {
		base = "organization"
	}
	if requested != "" {
		available, err := store.OrganizationSlugAvailable(ctx, base)
		if err != nil {
			return "", err
		}
		if !available {
			return "", fmt.Errorf("%w: organization slug is unavailable", ErrOrganizationSlugUnavailable)
		}
		return base, nil
	}
	for attempt := 0; attempt < 8; attempt++ {
		candidate := base
		if attempt > 0 {
			suffix, err := randomCrockfordText(6)
			if err != nil {
				return "", err
			}
			candidate = trimSlugBase(base, 73) + "-" + strings.ToLower(suffix)
		}
		available, err := store.OrganizationSlugAvailable(ctx, candidate)
		if err != nil {
			return "", err
		}
		if available {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: organization slug is unavailable", ErrOrganizationSlugUnavailable)
}

func trimSlugBase(value string, max int) string {
	value = strings.Trim(value, "-")
	if len(value) <= max {
		return value
	}
	return strings.Trim(value[:max], "-")
}

func randomOrganizationID() (string, error) {
	payload, err := randomCrockfordText(26)
	if err != nil {
		return "", err
	}
	return organizationPublicIDPrefix + payload, nil
}

func randomCrockfordText(length int) (string, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	b.Grow(length)
	for _, value := range raw {
		b.WriteByte(organizationPublicIDAlphabet[int(value)%len(organizationPublicIDAlphabet)])
	}
	return b.String(), nil
}

func randomMemberInviteAcceptanceToken() (token string, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, memberInviteAcceptanceTokenHash(token), nil
}

func memberInviteAcceptanceTokenHash(token string) string {
	hash, _ := SecretHash(strings.TrimSpace(token))
	return hash
}

func validateInvite(input InviteMemberRequest) error {
	if _, err := mail.ParseAddress(strings.TrimSpace(input.Email)); err != nil {
		return fmt.Errorf("%w: email is invalid", ErrInvalidInput)
	}
	roles := normalizeInviteRoles(input.Roles)
	if len(roles) == 0 {
		return fmt.Errorf("%w: at least one role is required", ErrInvalidInput)
	}
	if len(roles) > 8 {
		return fmt.Errorf("%w: too many roles", ErrInvalidInput)
	}
	for _, role := range roles {
		switch role {
		case "roles/admin", "roles/member", "roles/executionViewer", "roles/billingViewer", "roles/sourceViewer", "roles/secretsUser":
		default:
			return fmt.Errorf("%w: unsupported invite role %q", ErrInvalidInput, role)
		}
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
	input.Roles = normalizeInviteRoles(input.Roles)
	return input
}

func normalizeCompleteMemberInvite(input CompleteMemberInviteRequest) (CompleteMemberInviteRequest, error) {
	input.AcceptanceToken = strings.TrimSpace(input.AcceptanceToken)
	if input.AcceptanceToken == "" {
		return CompleteMemberInviteRequest{}, fmt.Errorf("%w: invite token is required", ErrInvalidInput)
	}
	if len(input.AcceptanceToken) > 512 {
		return CompleteMemberInviteRequest{}, fmt.Errorf("%w: invite token is invalid", ErrInvalidInput)
	}
	return input, nil
}

func normalizeInviteRoles(input []string) []string {
	roles := make([]string, 0, len(input))
	seen := map[string]struct{}{}
	for _, role := range input {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	if len(roles) == 0 {
		roles = append(roles, "roles/member")
	}
	sort.Strings(roles)
	return roles
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
