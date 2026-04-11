package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"
)

type Store interface {
	GetPolicy(ctx context.Context, orgID, actor string) (PolicyDocument, error)
	PutPolicy(ctx context.Context, policy PolicyDocument) (PolicyDocument, error)
}

type Directory interface {
	ListMembers(ctx context.Context, orgID, projectID string) ([]Member, error)
	InviteMember(ctx context.Context, orgID, projectID string, input InviteMemberRequest) (InviteMemberResult, error)
	UpdateMemberRoles(ctx context.Context, orgID, projectID, userID string, roleKeys []string) (Member, error)
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
	policy, err := s.policy(ctx, principal.OrgID, principal.Subject)
	if err != nil {
		return Organization{}, err
	}
	members, err := s.members(ctx, principal.OrgID)
	if err != nil {
		return Organization{}, err
	}
	caller := callerMember(principal, members)
	return Organization{
		OrgID:       principal.OrgID,
		Name:        organizationName(principal),
		Caller:      caller,
		Policy:      policy,
		Permissions: PermissionsForRoles(policy, caller.RoleKeys),
	}, nil
}

func (s *Service) Members(ctx context.Context, principal Principal) ([]Member, error) {
	if err := principal.validate(); err != nil {
		return nil, err
	}
	return s.members(ctx, principal.OrgID)
}

func (s *Service) InviteMember(ctx context.Context, principal Principal, input InviteMemberRequest) (InviteMemberResult, error) {
	if err := principal.validate(); err != nil {
		return InviteMemberResult{}, err
	}
	if err := validateInvite(input); err != nil {
		return InviteMemberResult{}, err
	}
	if err := s.validateRoleKeys(ctx, principal.OrgID, principal.Subject, input.RoleKeys); err != nil {
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
	if err := s.validateRoleKeys(ctx, principal.OrgID, principal.Subject, roleKeys); err != nil {
		return Member{}, err
	}
	directory, err := s.directory()
	if err != nil {
		return Member{}, err
	}
	return directory.UpdateMemberRoles(ctx, principal.OrgID, s.ProjectID, userID, roleKeys)
}

func (s *Service) Policy(ctx context.Context, principal Principal) (PolicyDocument, error) {
	if err := principal.validate(); err != nil {
		return PolicyDocument{}, err
	}
	return s.policy(ctx, principal.OrgID, principal.Subject)
}

func (s *Service) PutPolicy(ctx context.Context, principal Principal, policy PolicyDocument) (PolicyDocument, error) {
	if err := principal.validate(); err != nil {
		return PolicyDocument{}, err
	}
	policy.OrgID = principal.OrgID
	policy.UpdatedBy = principal.Subject
	policy.UpdatedAt = s.now()
	if err := ValidatePolicy(policy); err != nil {
		return PolicyDocument{}, err
	}
	store, err := s.store()
	if err != nil {
		return PolicyDocument{}, err
	}
	return store.PutPolicy(ctx, policy)
}

func (s *Service) Operations(context.Context, Principal) Operations {
	return DefaultOperations()
}

func (s *Service) policy(ctx context.Context, orgID, actor string) (PolicyDocument, error) {
	store, err := s.store()
	if err != nil {
		return PolicyDocument{}, err
	}
	return store.GetPolicy(ctx, orgID, actor)
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

func (s *Service) validateRoleKeys(ctx context.Context, orgID, actor string, roleKeys []string) error {
	policy, err := s.policy(ctx, orgID, actor)
	if err != nil {
		return err
	}
	reserved := ReservedRoleKeys()
	allowed := map[string]struct{}{}
	for _, role := range policy.Roles {
		allowed[role.RoleKey] = struct{}{}
	}
	for _, role := range roleKeys {
		if _, ok := reserved[role]; ok {
			return fmt.Errorf("%w: reserved role key %q cannot be assigned through identity-service", ErrInvalidPolicy, role)
		}
		if _, ok := allowed[role]; !ok {
			return fmt.Errorf("%w: unknown role key %q", ErrInvalidPolicy, role)
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

func ValidatePolicy(policy PolicyDocument) error {
	if strings.TrimSpace(policy.OrgID) == "" {
		return fmt.Errorf("%w: org_id is required", ErrInvalidPolicy)
	}
	if policy.Version < 0 {
		return fmt.Errorf("%w: version must be non-negative", ErrInvalidPolicy)
	}
	known := KnownPermissions()
	knownRoles := KnownRoleKeys()
	reservedRoles := ReservedRoleKeys()
	seenRoles := map[string]struct{}{}
	if len(policy.Roles) == 0 {
		return fmt.Errorf("%w: at least one role is required", ErrInvalidPolicy)
	}
	for _, role := range policy.Roles {
		roleKey := strings.TrimSpace(role.RoleKey)
		if roleKey == "" {
			return fmt.Errorf("%w: role_key is required", ErrInvalidPolicy)
		}
		if _, ok := reservedRoles[roleKey]; ok {
			return fmt.Errorf("%w: reserved role key %q cannot be edited in policy documents", ErrInvalidPolicy, roleKey)
		}
		if _, ok := knownRoles[roleKey]; !ok {
			return fmt.Errorf("%w: unknown role key %q", ErrInvalidPolicy, roleKey)
		}
		if _, duplicate := seenRoles[roleKey]; duplicate {
			return fmt.Errorf("%w: duplicate role key %q", ErrInvalidPolicy, roleKey)
		}
		seenRoles[roleKey] = struct{}{}
		if strings.TrimSpace(role.DisplayName) == "" {
			return fmt.Errorf("%w: display_name is required for role %q", ErrInvalidPolicy, roleKey)
		}
		seenPermissions := map[string]struct{}{}
		if len(role.Permissions) == 0 {
			return fmt.Errorf("%w: permissions are required for role %q", ErrInvalidPolicy, roleKey)
		}
		for _, permission := range role.Permissions {
			permission = strings.TrimSpace(permission)
			if _, ok := known[permission]; !ok {
				return fmt.Errorf("%w: unknown permission %q", ErrInvalidPolicy, permission)
			}
			if _, duplicate := seenPermissions[permission]; duplicate {
				return fmt.Errorf("%w: duplicate permission %q", ErrInvalidPolicy, permission)
			}
			seenPermissions[permission] = struct{}{}
		}
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

func IsInvalid(err error) bool {
	return errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrInvalidPolicy)
}
