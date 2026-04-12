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
	GetMemberCapabilities(ctx context.Context, orgID, actor string) (MemberCapabilitiesDocument, error)
	PutMemberCapabilities(ctx context.Context, doc MemberCapabilitiesDocument) (MemberCapabilitiesDocument, error)
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
	CreateServiceAccountCredential(ctx context.Context, orgID string, input ServiceAccountCredentialInput) (subjectID string, material APICredentialIssuedMaterial, err error)
	AddServiceAccountCredential(ctx context.Context, input AddServiceAccountCredentialInput) (APICredentialIssuedMaterial, error)
	RemoveServiceAccountCredential(ctx context.Context, subjectID string, secret APICredentialSecret) error
	DeactivateServiceAccount(ctx context.Context, subjectID string) error
}

type Service struct {
	Store     Store
	Directory Directory
	ProjectID string
	Now       func() time.Time
}

func (s *Service) Organization(ctx context.Context, principal Principal) (Organization, error) {
	if err := principal.validate(); err != nil {
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
	caller := callerMember(principal, members)
	return Organization{
		OrgID:              principal.OrgID,
		Name:               organizationName(principal),
		Caller:             caller,
		MemberCapabilities: capabilities,
		Permissions:        PermissionsForRoles(capabilities, caller.RoleKeys),
	}, nil
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
	return directory.InviteMember(ctx, principal.OrgID, s.ProjectID, normalizeInvite(input))
}

func (s *Service) UpdateMemberRoles(ctx context.Context, principal Principal, userID string, roleKeys []string) (Member, error) {
	if err := principal.validate(); err != nil {
		return Member{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Member{}, fmt.Errorf("%w: user_id is required", ErrInvalidInput)
	}
	roleKeys = normalizeRoleKeys(roleKeys)
	if len(roleKeys) == 0 {
		return Member{}, fmt.Errorf("%w: role_keys is required", ErrInvalidInput)
	}
	if err := s.validateRoleKeys(roleKeys); err != nil {
		return Member{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return Member{}, err
	}
	return directory.UpdateMemberRoles(ctx, principal.OrgID, s.ProjectID, userID, roleKeys)
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
	return store.PutMemberCapabilities(ctx, doc)
}

func (s *Service) ListAPICredentials(ctx context.Context, principal Principal) ([]APICredential, error) {
	if err := principal.validate(); err != nil {
		return nil, err
	}
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	return store.ListAPICredentials(ctx, principal.OrgID)
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
	return store.GetAPICredential(ctx, principal.OrgID, credentialID)
}

func (s *Service) CreateAPICredential(ctx context.Context, principal Principal, input CreateAPICredentialRequest) (CreateAPICredentialResult, error) {
	if err := principal.validate(); err != nil {
		return CreateAPICredentialResult{}, err
	}
	input, capabilities, err := s.normalizeCreateAPICredentialRequest(ctx, principal, input)
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
	clientID := "fm-api-" + strings.ReplaceAll(credentialID, "-", "")
	credential := APICredential{
		CredentialID:         credentialID,
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
	subjectID, material, err := directory.CreateServiceAccountCredential(ctx, principal.OrgID, ServiceAccountCredentialInput{
		CredentialID: credentialID,
		ClientID:     clientID,
		DisplayName:  input.DisplayName,
		AuthMethod:   input.AuthMethod,
		ExpiresAt:    input.ExpiresAt,
	})
	if err != nil {
		return CreateAPICredentialResult{}, err
	}
	if err := validateIssuedMaterial(input.AuthMethod, material); err != nil {
		cleanupErr := directory.DeactivateServiceAccount(ctx, subjectID)
		return CreateAPICredentialResult{}, errors.Join(err, cleanupErr)
	}
	credential.SubjectID = subjectID
	credential.ClientID = firstNonEmpty(material.ClientID, clientID)
	secret := credentialSecretFromMaterial(credential, material, principal.Subject, now, input.ExpiresAt)
	credential.Fingerprint = secret.Fingerprint
	credential, err = store.CreateAPICredential(ctx, credential, secret)
	if err != nil {
		cleanupErr := directory.DeactivateServiceAccount(ctx, subjectID)
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
	if err := directory.DeactivateServiceAccount(ctx, credential.SubjectID); err != nil {
		return APICredential{}, err
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
	return store.ResolveAPICredentialClaims(ctx, subjectID, s.now())
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
	return directory.ListMembers(ctx, orgID, s.ProjectID)
}

func (s *Service) store() (Store, error) {
	if s == nil || s.Store == nil {
		return nil, ErrStoreUnavailable
	}
	return s.Store, nil
}

func (s *Service) directory() (Directory, error) {
	if s == nil || s.Directory == nil || strings.TrimSpace(s.ProjectID) == "" {
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

func organizationName(principal Principal) string {
	return principal.OrgID
}

func (s *Service) normalizeCreateAPICredentialRequest(ctx context.Context, principal Principal, input CreateAPICredentialRequest) (CreateAPICredentialRequest, MemberCapabilitiesDocument, error) {
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, fmt.Errorf("%w: display_name is required", ErrInvalidInput)
	}
	if len(input.DisplayName) > 200 {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, fmt.Errorf("%w: display_name is too long", ErrInvalidInput)
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
	if err := validateCredentialPermissions(capabilities, principal, input.Permissions); err != nil {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, err
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(s.now()) {
		return CreateAPICredentialRequest{}, MemberCapabilitiesDocument{}, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidInput)
	}
	return input, capabilities, nil
}

func validateCredentialPermissions(capabilities MemberCapabilitiesDocument, principal Principal, requested []string) error {
	known := KnownPermissions()
	granted := permissionSet(effectivePrincipalPermissions(capabilities, principal))
	for _, permission := range requested {
		if _, ok := known[permission]; !ok {
			return fmt.Errorf("%w: unknown permission %q", ErrInvalidCapabilities, permission)
		}
		if _, ok := granted[permission]; !ok {
			return fmt.Errorf("%w: permission %q is not held by caller", ErrInvalidCapabilities, permission)
		}
	}
	return nil
}

func effectivePrincipalPermissions(capabilities MemberCapabilitiesDocument, principal Principal) []string {
	out := append([]string(nil), PermissionsForRoles(capabilities, principal.Roles)...)
	out = append(out, principal.DirectPermissions...)
	return normalizePermissions(out)
}

func permissionSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
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
