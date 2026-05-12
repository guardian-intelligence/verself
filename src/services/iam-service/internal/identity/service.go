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
	GetMemberCapabilities(ctx context.Context, orgID, actor string) (MemberCapabilitiesDocument, error)
	PutMemberCapabilities(ctx context.Context, doc MemberCapabilitiesDocument) (MemberCapabilitiesDocument, error)
	GetOrgACLState(ctx context.Context, orgID, actor string) (OrgACLState, error)
	UpdateMemberRolesCommand(ctx context.Context, command UpdateMemberRolesCommand, directory Directory, projectID string) (UpdateMemberRolesResult, error)
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
	ListMembers(ctx context.Context, orgID, projectID string) ([]Member, error)
	InviteMember(ctx context.Context, orgID, projectID string, input InviteMemberRequest) (InviteMemberResult, error)
	UpdateMemberRoles(ctx context.Context, orgID, projectID, userID string, roleKeys []string) (Member, error)
	UpdateHumanProfile(ctx context.Context, subjectID string, input HumanProfileUpdate) (HumanProfile, error)
	CreateServiceAccountCredential(ctx context.Context, orgID string, input ServiceAccountCredentialInput) (subjectID string, material APICredentialIssuedMaterial, err error)
	AddServiceAccountCredential(ctx context.Context, input AddServiceAccountCredentialInput) (APICredentialIssuedMaterial, error)
	RemoveServiceAccountCredential(ctx context.Context, subjectID string, secret APICredentialSecret) error
	DeactivateServiceAccount(ctx context.Context, subjectID string) error
}

type AuthorizationGraph interface {
	ReconcileOrganizationRoles(ctx context.Context, orgID string, members []Member, capabilities MemberCapabilitiesDocument, operation string) (string, error)
	ReconcileMemberRoles(ctx context.Context, orgID string, member Member, operation string) (string, error)
	ReconcileCapabilityGrants(ctx context.Context, orgID string, capabilities MemberCapabilitiesDocument, operation string) (string, error)
	ReconcileServiceAccountPermissions(ctx context.Context, orgID, serviceAccountID string, permissions []string, operation string) (string, error)
	TestOrganizationPermissions(ctx context.Context, orgID string, subject AuthorizationSubject, permissions []string, minZedToken string) ([]string, string, error)
}

type Service struct {
	Store              Store
	Directory          Directory
	AuthorizationGraph AuthorizationGraph
	ProjectID          string
	Now                func() time.Time
}

type providerOrgDirectory struct {
	next          Directory
	providerOrgID string
}

func directoryForProviderOrgID(next Directory, providerOrgID string) Directory {
	return providerOrgDirectory{next: next, providerOrgID: strings.TrimSpace(providerOrgID)}
}

func (d providerOrgDirectory) ListMembers(ctx context.Context, _ string, projectID string) ([]Member, error) {
	return d.next.ListMembers(ctx, d.providerOrgID, projectID)
}

func (d providerOrgDirectory) InviteMember(ctx context.Context, _ string, projectID string, input InviteMemberRequest) (InviteMemberResult, error) {
	return d.next.InviteMember(ctx, d.providerOrgID, projectID, input)
}

func (d providerOrgDirectory) UpdateMemberRoles(ctx context.Context, _ string, projectID, userID string, roleKeys []string) (Member, error) {
	return d.next.UpdateMemberRoles(ctx, d.providerOrgID, projectID, userID, roleKeys)
}

func (d providerOrgDirectory) UpdateHumanProfile(ctx context.Context, subjectID string, input HumanProfileUpdate) (HumanProfile, error) {
	return d.next.UpdateHumanProfile(ctx, subjectID, input)
}

func (d providerOrgDirectory) CreateServiceAccountCredential(ctx context.Context, _ string, input ServiceAccountCredentialInput) (string, APICredentialIssuedMaterial, error) {
	return d.next.CreateServiceAccountCredential(ctx, d.providerOrgID, input)
}

func (d providerOrgDirectory) AddServiceAccountCredential(ctx context.Context, input AddServiceAccountCredentialInput) (APICredentialIssuedMaterial, error) {
	return d.next.AddServiceAccountCredential(ctx, input)
}

func (d providerOrgDirectory) RemoveServiceAccountCredential(ctx context.Context, subjectID string, secret APICredentialSecret) error {
	return d.next.RemoveServiceAccountCredential(ctx, subjectID, secret)
}

func (d providerOrgDirectory) DeactivateServiceAccount(ctx context.Context, subjectID string) error {
	return d.next.DeactivateServiceAccount(ctx, subjectID)
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
	capabilities, err := s.memberCapabilities(ctx, principal.OrgID, principal.Subject)
	if err != nil {
		return Organization{}, err
	}
	members, err := s.members(ctx, principal.OrgID)
	if err != nil {
		return Organization{}, err
	}
	if err := s.reconcileOrganizationRoles(ctx, principal.OrgID, members, capabilities, "get-organization"); err != nil {
		return Organization{}, err
	}
	orgACL, err := store.GetOrgACLState(ctx, principal.OrgID, principal.Subject)
	if err != nil {
		return Organization{}, err
	}
	caller := callerMember(principal, members)
	return Organization{
		OrgID:              principal.OrgID,
		DisplayName:        profile.DisplayName,
		Slug:               profile.Slug,
		Version:            profile.Version,
		OrgACLVersion:      orgACL.Version,
		Caller:             caller,
		MemberCapabilities: capabilities,
		Permissions:        PermissionsForRoles(capabilities, caller.RoleKeys),
	}, nil
}

// AccessibleOrganizationsBySubject enumerates org metadata across the entire
// set of orgs the JWT proves the subject is a member of. The token-derived
// orgIDs list is itself the authorization boundary, so no single Principal
// is required (multi-org bearers have no "selected org").
func (s *Service) AccessibleOrganizationsBySubject(ctx context.Context, subject string, orgIDs []string) ([]OrganizationMetadata, error) {
	if strings.TrimSpace(subject) == "" {
		return nil, fmt.Errorf("%w: subject is required", ErrInvalidInput)
	}
	orgIDs = normalizeOrganizationIDs(orgIDs)
	if len(orgIDs) == 0 {
		return nil, fmt.Errorf("%w: organization role assignments are required", ErrInvalidInput)
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
		return nil, fmt.Errorf("%w: organization metadata is missing for one or more role assignments", ErrOrganizationMissing)
	}
	return organizations, nil
}

func (s *Service) AccessibleOrganizationsByProviderOrgIDs(ctx context.Context, subject string, providerOrgIDs []string) ([]OrganizationMetadata, error) {
	if strings.TrimSpace(subject) == "" {
		return nil, fmt.Errorf("%w: subject is required", ErrInvalidInput)
	}
	providerOrgIDs = normalizeOrganizationIDs(providerOrgIDs)
	if len(providerOrgIDs) == 0 {
		return nil, fmt.Errorf("%w: organization role assignments are required", ErrInvalidInput)
	}
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	organizations, err := store.ListOrganizationMetadataByProviderOrgIDs(ctx, providerOrgIDs)
	if err != nil {
		return nil, err
	}
	if len(organizations) != len(providerOrgIDs) {
		return nil, fmt.Errorf("%w: organization metadata is missing for one or more role assignments", ErrOrganizationMissing)
	}
	return organizations, nil
}

func (s *Service) ReconcileOrganizationAuthorization(ctx context.Context, orgID, actor, operation string) error {
	orgID = strings.TrimSpace(orgID)
	actor = strings.TrimSpace(actor)
	if orgID == "" {
		return fmt.Errorf("%w: org_id is required", ErrInvalidInput)
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrInvalidInput)
	}
	capabilities, err := s.memberCapabilities(ctx, orgID, actor)
	if err != nil {
		return err
	}
	members, err := s.members(ctx, orgID)
	if err != nil {
		return err
	}
	return s.reconcileOrganizationRoles(ctx, orgID, members, capabilities, operation)
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
	return visibleMembers(members), nil
}

// visibleMembers narrows the raw Zitadel directory listing to the rows the org
// console renders. Two classes are dropped:
//
//   - Machine users (Zitadel service accounts). These are the same Zitadel
//     primitive that backs API credentials; they belong on the API Credentials
//     surface, not the members table. Even though seed-system grants persona
//     machine users (assume-platform-admin and friends) project authorizations
//     directly, the table is for human seats.
//   - Owner-role users. Owner is the org singleton; the role cannot be assigned
//     or revoked through invite/role-update (validateRoleKeys rejects it), so
//     showing an owner row would be a UX dead end. The owner is communicated
//     elsewhere (e.g. the caller's "your roles" badge in the general section).
//
// Service.Organization() still resolves callerMember from the unfiltered set so
// an operator who is the owner can still see themselves in the general section.
func visibleMembers(members []Member) []Member {
	out := make([]Member, 0, len(members))
	for _, member := range members {
		if member.Type == MemberTypeMachine {
			continue
		}
		if containsRole(member.RoleKeys, RoleOwner) {
			continue
		}
		out = append(out, member)
	}
	return out
}

func containsRole(roles []string, target string) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}

func (s *Service) InviteMember(ctx context.Context, principal Principal, input InviteMemberRequest) (InviteMemberResult, error) {
	if err := principal.validate(); err != nil {
		return InviteMemberResult{}, err
	}
	if err := validateInvite(input); err != nil {
		return InviteMemberResult{}, err
	}
	if err := s.validateRoleKeys(input.RoleKeys); err != nil {
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
	result, err := directory.InviteMember(ctx, providerOrgID, s.ProjectID, normalizeInvite(input))
	if err != nil {
		return InviteMemberResult{}, err
	}
	if err := s.reconcileMemberRoles(ctx, principal.OrgID, Member{
		UserID:   result.UserID,
		Type:     MemberTypeHuman,
		RoleKeys: result.RoleKeys,
	}, "invite-member"); err != nil {
		return InviteMemberResult{}, err
	}
	return result, nil
}

func (s *Service) UpdateMemberRoles(ctx context.Context, principal Principal, command UpdateMemberRolesCommand) (UpdateMemberRolesResult, error) {
	if err := principal.validate(); err != nil {
		return UpdateMemberRolesResult{}, err
	}
	command.OrgID = principal.OrgID
	command.ActorID = principal.Subject
	command.UserID = strings.TrimSpace(command.UserID)
	command.OperationID = strings.TrimSpace(command.OperationID)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.UserID == "" {
		return UpdateMemberRolesResult{}, fmt.Errorf("%w: user_id is required", ErrInvalidInput)
	}
	command.RoleKeys = normalizeRoleKeys(command.RoleKeys)
	command.ExpectedRoleKeys = normalizeRoleKeys(command.ExpectedRoleKeys)
	if len(command.RoleKeys) == 0 {
		return UpdateMemberRolesResult{}, fmt.Errorf("%w: role_keys is required", ErrInvalidInput)
	}
	if len(command.ExpectedRoleKeys) == 0 {
		return UpdateMemberRolesResult{}, fmt.Errorf("%w: expected_role_keys is required", ErrInvalidInput)
	}
	if command.ExpectedOrgACLVersion <= 0 {
		return UpdateMemberRolesResult{}, fmt.Errorf("%w: expected_org_acl_version must be positive", ErrInvalidInput)
	}
	if command.OperationID == "" {
		return UpdateMemberRolesResult{}, fmt.Errorf("%w: operation_id is required", ErrInvalidInput)
	}
	if command.IdempotencyKey == "" {
		return UpdateMemberRolesResult{}, fmt.Errorf("%w: idempotency key is required", ErrInvalidInput)
	}
	if err := s.validateRoleKeys(command.RoleKeys); err != nil {
		return UpdateMemberRolesResult{}, err
	}
	if err := s.validateRoleKeys(command.ExpectedRoleKeys); err != nil {
		return UpdateMemberRolesResult{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return UpdateMemberRolesResult{}, err
	}
	providerOrgID, err := s.providerOrgID(ctx, principal.OrgID, principal.Subject)
	if err != nil {
		return UpdateMemberRolesResult{}, err
	}
	store, err := s.store()
	if err != nil {
		return UpdateMemberRolesResult{}, err
	}
	result, err := store.UpdateMemberRolesCommand(ctx, command, directoryForProviderOrgID(directory, providerOrgID), s.ProjectID)
	if err != nil {
		return UpdateMemberRolesResult{}, err
	}
	if err := s.reconcileMemberRoles(ctx, principal.OrgID, result.Member, "update-member-roles"); err != nil {
		return UpdateMemberRolesResult{}, err
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

func (s *Service) MemberCapabilities(ctx context.Context, principal Principal) (MemberCapabilitiesDocument, error) {
	if err := principal.validate(); err != nil {
		return MemberCapabilitiesDocument{}, err
	}
	return s.memberCapabilities(ctx, principal.OrgID, principal.Subject)
}

func (s *Service) PutMemberCapabilities(ctx context.Context, principal Principal, doc MemberCapabilitiesDocument) (MemberCapabilitiesDocument, error) {
	if err := principal.validate(); err != nil {
		return MemberCapabilitiesDocument{}, err
	}
	doc.OrgID = principal.OrgID
	doc.UpdatedBy = principal.Subject
	doc.UpdatedAt = s.now()
	doc.EnabledKeys = normalizeCapabilityKeys(doc.EnabledKeys)
	if err := ValidateMemberCapabilities(doc); err != nil {
		return MemberCapabilitiesDocument{}, err
	}
	store, err := s.store()
	if err != nil {
		return MemberCapabilitiesDocument{}, err
	}
	updated, err := store.PutMemberCapabilities(ctx, doc)
	if err != nil {
		return MemberCapabilitiesDocument{}, err
	}
	if err := s.reconcileCapabilityGrants(ctx, principal.OrgID, updated, "put-member-capabilities"); err != nil {
		return MemberCapabilitiesDocument{}, err
	}
	return updated, nil
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
	for i := range accounts {
		permissions, err := s.serviceAccountPermissions(ctx, accounts[i].OrgID, accounts[i].ServiceAccountID)
		if err != nil {
			return nil, err
		}
		accounts[i].Permissions = permissions
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
	account.Permissions, err = s.serviceAccountPermissions(ctx, account.OrgID, account.ServiceAccountID)
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
	if _, err := s.reconcileServiceAccountPermissions(ctx, principal.OrgID, serviceAccountID, nil, "disable-service-account"); err != nil {
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
	return s.credentialsWithPermissions(ctx, credentials)
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
	permissions, err := s.serviceAccountPermissions(ctx, credential.OrgID, credential.ServiceAccountID)
	if err != nil {
		return APICredential{}, err
	}
	credential.Permissions = permissions
	return credential, nil
}

func (s *Service) CreateAPICredential(ctx context.Context, principal Principal, input CreateAPICredentialRequest) (CreateAPICredentialResult, error) {
	if err := principal.validate(); err != nil {
		return CreateAPICredentialResult{}, err
	}
	input, capabilities, err := s.normalizeCreateAPICredentialRequest(ctx, principal, input)
	if err != nil {
		return CreateAPICredentialResult{}, err
	}
	authzGraph, err := s.authorizationGraph()
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
		CredentialID:         credentialID,
		ServiceAccountID:     serviceAccountID,
		OrgID:                principal.OrgID,
		ClientID:             clientID,
		DisplayName:          input.DisplayName,
		Status:               APICredentialStatusActive,
		AuthMethod:           input.AuthMethod,
		Permissions:          append([]string(nil), input.Permissions...),
		PolicyVersionAtIssue: capabilities.Version,
		CreatedAt:            now,
		CreatedBy:            principal.Subject,
		UpdatedAt:            now,
		ExpiresAt:            input.ExpiresAt,
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
	if _, err := authzGraph.ReconcileServiceAccountPermissions(ctx, principal.OrgID, serviceAccountID, input.Permissions, "create-api-credential"); err != nil {
		cleanupErr := cleanupIssuedCredential(ctx, directory, createServiceAccount, subjectID, secret)
		return CreateAPICredentialResult{}, errors.Join(err, cleanupErr)
	}
	if createServiceAccount {
		account, credential, err = store.CreateServiceAccount(ctx, account, credential, secret)
	} else {
		credential, err = store.CreateAPICredential(ctx, credential, secret)
	}
	if err != nil {
		cleanupErr := cleanupIssuedCredential(ctx, directory, createServiceAccount, subjectID, secret)
		reconcileErr := cleanupServiceAccountPermissions(ctx, s, principal.OrgID, serviceAccountID, createServiceAccount)
		return CreateAPICredentialResult{}, errors.Join(err, cleanupErr, reconcileErr)
	}
	credential.Permissions = append([]string(nil), input.Permissions...)
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
	credential.Permissions, err = s.serviceAccountPermissions(ctx, credential.OrgID, credential.ServiceAccountID)
	if err != nil {
		return RollAPICredentialResult{}, err
	}
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
	permissions, err := s.serviceAccountPermissions(ctx, result.OrgID, result.ServiceAccountID)
	if err != nil {
		return ResolveAPICredentialClaimsResult{}, err
	}
	result.Permissions = permissions
	result.OpenBaoRoles = OpenBaoRolesForPermissions(permissions)
	return result, nil
}

func (s *Service) memberCapabilities(ctx context.Context, orgID, actor string) (MemberCapabilitiesDocument, error) {
	store, err := s.store()
	if err != nil {
		return MemberCapabilitiesDocument{}, err
	}
	return store.GetMemberCapabilities(ctx, orgID, actor)
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
	return directory.ListMembers(ctx, providerOrgID, s.ProjectID)
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
	directory, err := s.directoryClient()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.ProjectID) == "" {
		return nil, ErrZitadelUnavailable
	}
	return directory, nil
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

func (s *Service) reconcileOrganizationRoles(ctx context.Context, orgID string, members []Member, capabilities MemberCapabilitiesDocument, operation string) error {
	if s == nil || s.AuthorizationGraph == nil {
		return nil
	}
	_, err := s.AuthorizationGraph.ReconcileOrganizationRoles(ctx, orgID, members, capabilities, operation)
	return err
}

func (s *Service) reconcileMemberRoles(ctx context.Context, orgID string, member Member, operation string) error {
	if s == nil || s.AuthorizationGraph == nil {
		return nil
	}
	_, err := s.AuthorizationGraph.ReconcileMemberRoles(ctx, orgID, member, operation)
	return err
}

func (s *Service) reconcileCapabilityGrants(ctx context.Context, orgID string, capabilities MemberCapabilitiesDocument, operation string) error {
	if s == nil || s.AuthorizationGraph == nil {
		return nil
	}
	_, err := s.AuthorizationGraph.ReconcileCapabilityGrants(ctx, orgID, capabilities, operation)
	return err
}

func (s *Service) reconcileServiceAccountPermissions(ctx context.Context, orgID, serviceAccountID string, permissions []string, operation string) (string, error) {
	graph, err := s.authorizationGraph()
	if err != nil {
		return "", err
	}
	return graph.ReconcileServiceAccountPermissions(ctx, orgID, serviceAccountID, permissions, operation)
}

func (s *Service) serviceAccountPermissions(ctx context.Context, orgID, serviceAccountID string) ([]string, error) {
	graph, err := s.authorizationGraph()
	if err != nil {
		return nil, err
	}
	allowed, _, err := graph.TestOrganizationPermissions(ctx, orgID, AuthorizationSubject{
		Kind: AuthorizationSubjectKindServiceAccount,
		ID:   serviceAccountID,
	}, sortedKeys(KnownPermissions()), "")
	if err != nil {
		return nil, err
	}
	return normalizePermissions(allowed), nil
}

func (s *Service) credentialsWithPermissions(ctx context.Context, credentials []APICredential) ([]APICredential, error) {
	for i := range credentials {
		if strings.TrimSpace(credentials[i].ServiceAccountID) == "" {
			continue
		}
		permissions, err := s.serviceAccountPermissions(ctx, credentials[i].OrgID, credentials[i].ServiceAccountID)
		if err != nil {
			return nil, err
		}
		credentials[i].Permissions = permissions
	}
	return credentials, nil
}

func (s *Service) validateRoleKeys(roleKeys []string) error {
	known := KnownRoleKeys()
	for _, role := range roleKeys {
		if _, ok := known[role]; !ok {
			return fmt.Errorf("%w: unknown role key %q", ErrInvalidCapabilities, role)
		}
		if role == RoleOwner {
			// Owner is the org-singleton role transferred via a different flow;
			// it cannot be granted through the standard invite/role-update path.
			return fmt.Errorf("%w: role key %q cannot be assigned through invite or role update", ErrInvalidCapabilities, role)
		}
	}
	return nil
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

func ValidateMemberCapabilities(doc MemberCapabilitiesDocument) error {
	if strings.TrimSpace(doc.OrgID) == "" {
		return fmt.Errorf("%w: org_id is required", ErrInvalidCapabilities)
	}
	if doc.Version < 0 {
		return fmt.Errorf("%w: version must be non-negative", ErrInvalidCapabilities)
	}
	seen := map[string]struct{}{}
	for _, key := range doc.EnabledKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("%w: enabled_keys must not contain empty entries", ErrInvalidCapabilities)
		}
		if _, ok := CapabilityForKey(key); !ok {
			return fmt.Errorf("%w: unknown capability key %q", ErrInvalidCapabilities, key)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("%w: duplicate capability key %q", ErrInvalidCapabilities, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateInvite(input InviteMemberRequest) error {
	if _, err := mail.ParseAddress(strings.TrimSpace(input.Email)); err != nil {
		return fmt.Errorf("%w: email is invalid", ErrInvalidInput)
	}
	if len(normalizeRoleKeys(input.RoleKeys)) == 0 {
		return fmt.Errorf("%w: role_keys is required", ErrInvalidInput)
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
	input.RoleKeys = normalizeRoleKeys(input.RoleKeys)
	return input
}

func normalizeRoleKeys(roleKeys []string) []string {
	if len(roleKeys) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(roleKeys))
	for _, role := range roleKeys {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	sort.Strings(out)
	return out
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

func normalizeCapabilityKeys(keys []string) []string {
	if len(keys) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func callerMember(principal Principal, members []Member) Member {
	for _, member := range members {
		if member.UserID == principal.Subject {
			return member
		}
	}
	return Member{
		UserID:   principal.Subject,
		Email:    principal.Email,
		RoleKeys: append([]string(nil), principal.Roles...),
	}
}

func (s *Service) normalizeCreateAPICredentialRequest(ctx context.Context, principal Principal, input CreateAPICredentialRequest) (CreateAPICredentialRequest, MemberCapabilitiesDocument, error) {
	input.ServiceAccountID = strings.TrimSpace(input.ServiceAccountID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, fmt.Errorf("%w: display_name is required", ErrInvalidInput)
	}
	if len(input.DisplayName) > 200 {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, fmt.Errorf("%w: display_name is too long", ErrInvalidInput)
	}
	input.Description = strings.TrimSpace(input.Description)
	if len(input.Description) > 1000 {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, fmt.Errorf("%w: description is too long", ErrInvalidInput)
	}
	input.AuthMethod = normalizeAuthMethod(string(input.AuthMethod))
	if err := validateAuthMethod(input.AuthMethod); err != nil {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, err
	}
	input.Permissions = normalizePermissions(input.Permissions)
	if len(input.Permissions) == 0 {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, fmt.Errorf("%w: permissions are required", ErrInvalidInput)
	}
	capabilities, err := s.memberCapabilities(ctx, principal.OrgID, principal.Subject)
	if err != nil {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, err
	}
	if err := s.validateCredentialPermissions(ctx, principal, input.Permissions); err != nil {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, err
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(s.now()) {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidInput)
	}
	return input, capabilities, nil
}

func (s *Service) validateCredentialPermissions(ctx context.Context, principal Principal, requested []string) error {
	known := KnownPermissions()
	for _, permission := range requested {
		if _, ok := known[permission]; !ok {
			return fmt.Errorf("%w: unknown permission %q", ErrInvalidCapabilities, permission)
		}
	}
	graph, err := s.authorizationGraph()
	if err != nil {
		return err
	}
	allowed, _, err := graph.TestOrganizationPermissions(ctx, principal.OrgID, principal.authorizationSubject(), requested, "")
	if err != nil {
		return err
	}
	allowedSet := map[string]struct{}{}
	for _, permission := range allowed {
		allowedSet[permission] = struct{}{}
	}
	for _, permission := range requested {
		if _, ok := allowedSet[permission]; !ok {
			return fmt.Errorf("%w: permission %q is not held by caller", ErrInvalidCapabilities, permission)
		}
	}
	return nil
}

func (p Principal) authorizationSubject() AuthorizationSubject {
	kind := p.SubjectKind
	if kind == "" {
		kind = AuthorizationSubjectKindUser
	}
	return AuthorizationSubject{Kind: kind, ID: p.Subject}
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

func cleanupServiceAccountPermissions(ctx context.Context, svc *Service, orgID, serviceAccountID string, clear bool) error {
	if !clear {
		return nil
	}
	_, err := svc.reconcileServiceAccountPermissions(ctx, orgID, serviceAccountID, nil, "cleanup-service-account-permissions")
	return err
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

func normalizePermissions(permissions []string) []string {
	if len(permissions) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}
		if _, duplicate := seen[permission]; duplicate {
			continue
		}
		seen[permission] = struct{}{}
		out = append(out, permission)
	}
	sort.Strings(out)
	return out
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
	return errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrInvalidCapabilities)
}
