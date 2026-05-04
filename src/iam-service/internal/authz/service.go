package authz

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/iam-service/internal/spicedb"
)

const (
	resourceTypeOrg  = "org"
	resourceTypeRole = "role"

	relationMember = "member"

	subjectTypeUser           = "user"
	subjectTypeServiceAccount = "service_account"
	subjectRelationMember     = "member"

	publicRoleOwner           = "roles/owner"
	publicRoleAdmin           = "roles/admin"
	publicRoleMember          = "roles/member"
	publicRoleExecutionViewer = "roles/executionViewer"
	publicRoleBillingViewer   = "roles/billingViewer"
	publicRoleSourceViewer    = "roles/sourceViewer"
	publicRoleSecretsUser     = "roles/secretsUser"
)

var (
	ErrUnavailable = errors.New("authorization graph unavailable")
	ErrInvalid     = errors.New("invalid authorization graph request")
	ErrConflict    = errors.New("authorization graph conflict")
)

type Backend interface {
	Check(ctx context.Context, resource spicedb.ResourceRef, permission string, subject spicedb.SubjectRef, minZedToken string) (bool, string, error)
	ReadResourceRelationships(ctx context.Context, resource spicedb.ResourceRef, relations map[string]struct{}) ([]spicedb.Relationship, string, error)
	ReplaceResourceRelationships(ctx context.Context, current []spicedb.Relationship, desired []spicedb.Relationship, metadata map[string]any) (string, error)
}

type Service struct {
	backend Backend
}

type SubjectKind string

const (
	SubjectKindUser           SubjectKind = "user"
	SubjectKindServiceAccount SubjectKind = "service_account"
)

type Subject struct {
	Kind SubjectKind
	ID   string
}

type Policy struct {
	Resource string
	Version  int32
	Etag     string
	Bindings []PolicyBinding
	ZedToken string
}

type PolicyBinding struct {
	Role    string
	Members []string
}

type roleDefinition struct {
	PublicRole string
	RoleKey    string
	Relation   string
}

var zitadelRoleDefinitions = []roleDefinition{
	{PublicRole: publicRoleOwner, RoleKey: identity.RoleOwner, Relation: "owner"},
	{PublicRole: publicRoleAdmin, RoleKey: identity.RoleAdmin, Relation: "admin"},
	{PublicRole: publicRoleMember, RoleKey: identity.RoleMember, Relation: "member"},
}

var policyRoleDefinitions = []roleDefinition{
	{PublicRole: publicRoleOwner, RoleKey: identity.RoleOwner, Relation: "owner"},
	{PublicRole: publicRoleAdmin, RoleKey: identity.RoleAdmin, Relation: "admin"},
	{PublicRole: publicRoleMember, RoleKey: identity.RoleMember, Relation: "member"},
	{PublicRole: publicRoleExecutionViewer, RoleKey: "execution_lister", Relation: "execution_lister"},
	{PublicRole: publicRoleBillingViewer, RoleKey: "billing_viewer", Relation: "billing_viewer"},
	{PublicRole: publicRoleSourceViewer, RoleKey: "source_viewer", Relation: "source_viewer"},
	{PublicRole: publicRoleSecretsUser, RoleKey: "secret_user", Relation: "secret_user"},
}

var capabilityGrantRelations = map[string]string{
	"deploy_executions": "execution_lister",
	"invite_members":    "member_inviter",
	"view_billing":      "billing_viewer",
	"view_source":       "source_viewer",
	"use_secrets":       "secret_user",
}

var orgPermissionByProductPermission = map[string]string{
	identity.PermissionOrganizationRead:              "read",
	identity.PermissionOrganizationWrite:             "manage_iam",
	identity.PermissionMemberRead:                    "read",
	identity.PermissionMemberInvite:                  "invite_members",
	identity.PermissionMemberRolesWrite:              "manage_members",
	identity.PermissionMemberCapabilitiesRead:        "read",
	identity.PermissionMemberCapabilitiesWrite:       "manage_iam",
	identity.PermissionIAMPolicyRead:                 "manage_iam",
	identity.PermissionIAMPolicySet:                  "manage_iam",
	identity.PermissionIAMPolicyTest:                 "read",
	identity.PermissionAPICredentialsRead:            "manage_api_credentials",
	identity.PermissionAPICredentialsCreate:          "manage_api_credentials",
	identity.PermissionAPICredentialsRoll:            "manage_api_credentials",
	identity.PermissionAPICredentialsRevoke:          "manage_api_credentials",
	identity.PermissionSandboxGitHubRead:             "list_executions",
	identity.PermissionSandboxGitHubWrite:            "manage_iam",
	identity.PermissionSandboxExecutionRead:          "list_executions",
	identity.PermissionSandboxExecutionScheduleRead:  "list_executions",
	identity.PermissionSandboxExecutionScheduleWrite: "list_executions",
	identity.PermissionSandboxLogsRead:               "list_executions",
	identity.PermissionSandboxAnalyticsRead:          "list_executions",
	identity.PermissionSandboxStickyDiskRead:         "list_executions",
	identity.PermissionSandboxStickyDiskWrite:        "manage_iam",
	identity.PermissionBillingRead:                   "view_billing",
	identity.PermissionBillingCheckout:               "manage_iam",
	identity.PermissionProjectRead:                   "view_source",
	identity.PermissionProjectWrite:                  "manage_iam",
	identity.PermissionProjectEnvironmentRead:        "view_source",
	identity.PermissionProjectEnvironmentWrite:       "manage_iam",
	identity.PermissionSourceRepoRead:                "view_source",
	identity.PermissionSourceRepoWrite:               "manage_iam",
	identity.PermissionSourceCheckoutWrite:           "manage_iam",
	identity.PermissionSourceIntegrationWrite:        "manage_iam",
	identity.PermissionSecretWrite:                   "manage_iam",
	identity.PermissionSecretRead:                    "use_secrets",
	identity.PermissionSecretList:                    "use_secrets",
	identity.PermissionSecretDelete:                  "manage_iam",
	identity.PermissionTransitKeyCreate:              "manage_iam",
	identity.PermissionTransitKeyRotate:              "manage_iam",
	identity.PermissionTransitEncrypt:                "use_secrets",
	identity.PermissionTransitDecrypt:                "use_secrets",
	identity.PermissionTransitSign:                   "use_secrets",
	identity.PermissionTransitVerify:                 "use_secrets",
}

func New(backend Backend) *Service {
	if backend == nil {
		return nil
	}
	return &Service{backend: backend}
}

func UserSubject(id string) Subject {
	return Subject{Kind: SubjectKindUser, ID: strings.TrimSpace(id)}
}

func ServiceAccountSubject(id string) Subject {
	return Subject{Kind: SubjectKindServiceAccount, ID: strings.TrimSpace(id)}
}

func (s *Service) ReconcileOrganizationRoles(ctx context.Context, orgID string, members []identity.Member, capabilities identity.MemberCapabilitiesDocument, operation string) (string, error) {
	if err := validateOrgID(orgID); err != nil {
		return "", err
	}
	current, _, err := s.currentLegacyManagedRelationships(ctx, orgID)
	if err != nil {
		return "", err
	}
	desired := desiredLegacyRoleMemberships(orgID, members)
	desired = append(desired, desiredLegacyOrgGrants(orgID, capabilities.EnabledKeys)...)
	return s.replace(ctx, current, desired, metadata(operation, orgID))
}

func (s *Service) ReconcileMemberRoles(ctx context.Context, orgID string, member identity.Member, operation string) (string, error) {
	if err := validateOrgID(orgID); err != nil {
		return "", err
	}
	subject := subjectForMember(member)
	if err := validateSubject(subject); err != nil {
		return "", err
	}
	current, _, err := s.currentMemberRoleRelationships(ctx, orgID, subject)
	if err != nil {
		return "", err
	}
	desired := desiredMemberRoleRelationships(orgID, subject, member.RoleKeys)
	desired = append(desired, desiredZitadelOrgRoleGrants(orgID)...)
	return s.replace(ctx, current, desired, metadata(operation, orgID))
}

func (s *Service) ReconcileCapabilityGrants(ctx context.Context, orgID string, capabilities identity.MemberCapabilitiesDocument, operation string) (string, error) {
	if err := validateOrgID(orgID); err != nil {
		return "", err
	}
	current, _, err := s.currentCapabilityGrantRelationships(ctx, orgID)
	if err != nil {
		return "", err
	}
	desired := desiredCapabilityOrgGrants(orgID, capabilities.EnabledKeys)
	return s.replace(ctx, current, desired, metadata(operation, orgID))
}

func (s *Service) GetOrganizationPolicy(ctx context.Context, orgID string) (Policy, error) {
	if err := validateOrgID(orgID); err != nil {
		return Policy{}, err
	}
	relationships, zedToken, err := s.currentPolicyRelationships(ctx, orgID)
	if err != nil {
		return Policy{}, err
	}
	policy := policyFromRelationships(orgID, relationships)
	policy.ZedToken = zedToken
	return policy, nil
}

func (s *Service) SetOrganizationPolicy(ctx context.Context, orgID string, policy Policy, operation string) (Policy, error) {
	if err := validateOrgID(orgID); err != nil {
		return Policy{}, err
	}
	desiredBindings, err := normalizePolicyBindings(policy.Bindings)
	if err != nil {
		return Policy{}, err
	}
	if err := validateOwnerBinding(desiredBindings); err != nil {
		return Policy{}, err
	}
	currentPolicy, err := s.GetOrganizationPolicy(ctx, orgID)
	if err != nil {
		return Policy{}, err
	}
	if policy.Etag != "" && policy.Etag != currentPolicy.Etag {
		return Policy{}, fmt.Errorf("%w: etag mismatch", ErrConflict)
	}
	current, _, err := s.currentPolicyRelationships(ctx, orgID)
	if err != nil {
		return Policy{}, err
	}
	desired, err := desiredPolicyRelationships(orgID, desiredBindings)
	if err != nil {
		return Policy{}, err
	}
	zedToken, err := s.replace(ctx, current, desired, metadata(operation, orgID))
	if err != nil {
		return Policy{}, err
	}
	out := policyFromRelationships(orgID, desired)
	out.ZedToken = zedToken
	return out, nil
}

func (s *Service) TestOrganizationPermissions(ctx context.Context, orgID string, subject Subject, permissions []string, minZedToken string) ([]string, string, error) {
	if err := validateOrgID(orgID); err != nil {
		return nil, "", err
	}
	if err := validateSubject(subject); err != nil {
		return nil, "", err
	}
	if s == nil || s.backend == nil {
		return nil, "", ErrUnavailable
	}
	allowed := []string{}
	checkedAt := ""
	for _, requested := range compactSorted(permissions) {
		spicePermission, ok := orgPermissionByProductPermission[requested]
		if !ok {
			continue
		}
		ok, token, err := s.backend.Check(ctx, orgResource(orgID), spicePermission, subjectRef(subject), minZedToken)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if token != "" {
			checkedAt = token
		}
		if ok {
			allowed = append(allowed, requested)
		}
	}
	return allowed, checkedAt, nil
}

func (s *Service) replace(ctx context.Context, current []spicedb.Relationship, desired []spicedb.Relationship, metadata map[string]any) (string, error) {
	if s == nil || s.backend == nil {
		return "", ErrUnavailable
	}
	token, err := s.backend.ReplaceResourceRelationships(ctx, current, desired, metadata)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return token, nil
}

func (s *Service) currentLegacyManagedRelationships(ctx context.Context, orgID string) ([]spicedb.Relationship, string, error) {
	current, zedToken, err := s.currentRoleMemberships(ctx, orgID, roleKeys(zitadelRoleDefinitions), nil)
	if err != nil {
		return nil, "", err
	}
	orgGrants, grantToken, err := s.currentZitadelOrgRoleGrants(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	capabilityGrants, capabilityToken, err := s.currentCapabilityGrantRelationships(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	return append(append(current, orgGrants...), capabilityGrants...), lastToken(zedToken, grantToken, capabilityToken), nil
}

func (s *Service) currentMemberRoleRelationships(ctx context.Context, orgID string, subject Subject) ([]spicedb.Relationship, string, error) {
	current, zedToken, err := s.currentRoleMemberships(ctx, orgID, roleKeys(zitadelRoleDefinitions), &subject)
	if err != nil {
		return nil, "", err
	}
	grants, grantToken, err := s.currentZitadelOrgRoleGrants(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	return append(current, grants...), lastToken(zedToken, grantToken), nil
}

func (s *Service) currentPolicyRelationships(ctx context.Context, orgID string) ([]spicedb.Relationship, string, error) {
	current, zedToken, err := s.currentRoleMemberships(ctx, orgID, roleKeys(policyRoleDefinitions), nil)
	if err != nil {
		return nil, "", err
	}
	grants, grantToken, err := s.currentPolicyOrgRoleGrants(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	return append(current, grants...), lastToken(zedToken, grantToken), nil
}

func (s *Service) currentRoleMemberships(ctx context.Context, orgID string, keys []string, subject *Subject) ([]spicedb.Relationship, string, error) {
	if s == nil || s.backend == nil {
		return nil, "", ErrUnavailable
	}
	out := []spicedb.Relationship{}
	zedToken := ""
	for _, key := range keys {
		relationships, token, err := s.backend.ReadResourceRelationships(ctx, roleResource(orgID, key), relationSet(relationMember))
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if token != "" {
			zedToken = token
		}
		for _, relationship := range relationships {
			if relationship.Relation != relationMember {
				continue
			}
			if subject != nil && !sameSubject(relationship.Subject, *subject) {
				continue
			}
			out = append(out, relationship)
		}
	}
	return out, zedToken, nil
}

func (s *Service) currentZitadelOrgRoleGrants(ctx context.Context, orgID string) ([]spicedb.Relationship, string, error) {
	return s.currentOrgRoleGrants(ctx, orgID, zitadelRoleDefinitions, nil)
}

func (s *Service) currentPolicyOrgRoleGrants(ctx context.Context, orgID string) ([]spicedb.Relationship, string, error) {
	return s.currentOrgRoleGrants(ctx, orgID, policyRoleDefinitions, nil)
}

func (s *Service) currentCapabilityGrantRelationships(ctx context.Context, orgID string) ([]spicedb.Relationship, string, error) {
	keys := []string{}
	for _, relation := range capabilityGrantRelations {
		keys = append(keys, relation)
	}
	sort.Strings(keys)
	definitions := make([]roleDefinition, 0, len(keys))
	for _, relation := range keys {
		definitions = append(definitions, roleDefinition{
			PublicRole: relation,
			RoleKey:    identity.RoleMember,
			Relation:   relation,
		})
	}
	return s.currentOrgRoleGrants(ctx, orgID, definitions, nil)
}

func (s *Service) currentOrgRoleGrants(ctx context.Context, orgID string, definitions []roleDefinition, subject *Subject) ([]spicedb.Relationship, string, error) {
	if s == nil || s.backend == nil {
		return nil, "", ErrUnavailable
	}
	wanted := map[string]string{}
	relations := map[string]struct{}{}
	for _, definition := range definitions {
		relations[definition.Relation] = struct{}{}
		wanted[orgGrantKey(definition.Relation, roleObjectID(orgID, definition.RoleKey))] = definition.RoleKey
	}
	relationships, zedToken, err := s.backend.ReadResourceRelationships(ctx, orgResource(orgID), relations)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	out := []spicedb.Relationship{}
	for _, relationship := range relationships {
		if relationship.Subject.Type != resourceTypeRole || relationship.Subject.Relation != subjectRelationMember {
			continue
		}
		if _, ok := wanted[orgGrantKey(relationship.Relation, relationship.Subject.ID)]; !ok {
			continue
		}
		if subject != nil && !sameSubject(relationship.Subject, *subject) {
			continue
		}
		out = append(out, relationship)
	}
	return out, zedToken, nil
}

func desiredLegacyRoleMemberships(orgID string, members []identity.Member) []spicedb.Relationship {
	out := []spicedb.Relationship{}
	for _, member := range members {
		subject := subjectForMember(member)
		if validateSubject(subject) != nil {
			continue
		}
		out = append(out, desiredMemberRoleRelationships(orgID, subject, member.RoleKeys)...)
	}
	return out
}

func desiredMemberRoleRelationships(orgID string, subject Subject, roles []string) []spicedb.Relationship {
	out := []spicedb.Relationship{}
	for _, role := range compactSorted(roles) {
		if !isZitadelRole(role) {
			continue
		}
		out = append(out, spicedb.Relationship{
			Resource: roleResource(orgID, role),
			Relation: relationMember,
			Subject:  subjectRef(subject),
		})
	}
	return out
}

func desiredLegacyOrgGrants(orgID string, enabledCapabilities []string) []spicedb.Relationship {
	out := desiredZitadelOrgRoleGrants(orgID)
	out = append(out, desiredCapabilityOrgGrants(orgID, enabledCapabilities)...)
	return out
}

func desiredZitadelOrgRoleGrants(orgID string) []spicedb.Relationship {
	out := make([]spicedb.Relationship, 0, len(zitadelRoleDefinitions))
	for _, definition := range zitadelRoleDefinitions {
		out = append(out, orgRoleGrant(orgID, definition.Relation, definition.RoleKey))
	}
	return out
}

func desiredCapabilityOrgGrants(orgID string, enabledCapabilities []string) []spicedb.Relationship {
	enabled := map[string]struct{}{}
	for _, key := range compactSorted(enabledCapabilities) {
		enabled[key] = struct{}{}
	}
	out := []spicedb.Relationship{}
	keys := make([]string, 0, len(capabilityGrantRelations))
	for key := range capabilityGrantRelations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := enabled[key]; !ok {
			continue
		}
		out = append(out, orgRoleGrant(orgID, capabilityGrantRelations[key], identity.RoleMember))
	}
	return out
}

func desiredPolicyRelationships(orgID string, bindings []PolicyBinding) ([]spicedb.Relationship, error) {
	definitionByRole := roleDefinitionByPublicRole()
	out := []spicedb.Relationship{}
	for _, binding := range bindings {
		definition, ok := definitionByRole[binding.Role]
		if !ok {
			return nil, fmt.Errorf("%w: unsupported role %q", ErrInvalid, binding.Role)
		}
		for _, member := range binding.Members {
			subject, err := parsePolicyMember(member)
			if err != nil {
				return nil, err
			}
			out = append(out, spicedb.Relationship{
				Resource: roleResource(orgID, definition.RoleKey),
				Relation: relationMember,
				Subject:  subjectRef(subject),
			})
		}
		if len(binding.Members) > 0 {
			out = append(out, orgRoleGrant(orgID, definition.Relation, definition.RoleKey))
		}
	}
	return out, nil
}

func policyFromRelationships(orgID string, relationships []spicedb.Relationship) Policy {
	definitionByKey := roleDefinitionByRoleKey()
	membersByRole := map[string][]string{}
	for _, relationship := range relationships {
		if relationship.Resource.Type != resourceTypeRole || relationship.Relation != relationMember {
			continue
		}
		roleKey, ok := roleKeyFromObjectID(orgID, relationship.Resource.ID)
		if !ok {
			continue
		}
		definition, ok := definitionByKey[roleKey]
		if !ok {
			continue
		}
		subject, ok := policyMemberFromRef(relationship.Subject)
		if !ok {
			continue
		}
		membersByRole[definition.PublicRole] = append(membersByRole[definition.PublicRole], subject)
	}
	roles := make([]string, 0, len(membersByRole))
	for role := range membersByRole {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	bindings := make([]PolicyBinding, 0, len(roles))
	for _, role := range roles {
		members := compactSorted(membersByRole[role])
		if len(members) == 0 {
			continue
		}
		bindings = append(bindings, PolicyBinding{Role: role, Members: members})
	}
	policy := Policy{
		Resource: "organizations/" + orgID,
		Version:  1,
		Bindings: bindings,
	}
	policy.Etag = etag(policy.Bindings)
	return policy
}

func normalizePolicyBindings(bindings []PolicyBinding) ([]PolicyBinding, error) {
	definitionByRole := roleDefinitionByPublicRole()
	merged := map[string][]string{}
	for _, binding := range bindings {
		role := strings.TrimSpace(binding.Role)
		if _, ok := definitionByRole[role]; !ok {
			return nil, fmt.Errorf("%w: unsupported role %q", ErrInvalid, binding.Role)
		}
		for _, member := range binding.Members {
			if _, err := parsePolicyMember(member); err != nil {
				return nil, err
			}
			merged[role] = append(merged[role], strings.TrimSpace(member))
		}
	}
	roles := make([]string, 0, len(merged))
	for role := range merged {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	out := make([]PolicyBinding, 0, len(roles))
	for _, role := range roles {
		members := compactSorted(merged[role])
		if len(members) == 0 {
			continue
		}
		out = append(out, PolicyBinding{Role: role, Members: members})
	}
	return out, nil
}

func validateOwnerBinding(bindings []PolicyBinding) error {
	for _, binding := range bindings {
		if binding.Role != publicRoleOwner {
			continue
		}
		for _, member := range binding.Members {
			subject, err := parsePolicyMember(member)
			if err != nil {
				return err
			}
			if subject.Kind == SubjectKindUser {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: policy must retain at least one human owner", ErrInvalid)
}

func subjectForMember(member identity.Member) Subject {
	if member.Type == identity.MemberTypeMachine {
		return ServiceAccountSubject(member.UserID)
	}
	return UserSubject(member.UserID)
}

func subjectRef(subject Subject) spicedb.SubjectRef {
	switch subject.Kind {
	case SubjectKindServiceAccount:
		return spicedb.SubjectRef{Type: subjectTypeServiceAccount, ID: encodeOpaqueObjectID(subject.ID)}
	default:
		return spicedb.SubjectRef{Type: subjectTypeUser, ID: encodeOpaqueObjectID(subject.ID)}
	}
}

func sameSubject(ref spicedb.SubjectRef, subject Subject) bool {
	return ref.Type == subjectRef(subject).Type && ref.ID == subjectRef(subject).ID
}

func parsePolicyMember(member string) (Subject, error) {
	member = strings.TrimSpace(member)
	switch {
	case strings.HasPrefix(member, "user:"):
		subject := UserSubject(strings.TrimPrefix(member, "user:"))
		return subject, validateSubject(subject)
	case strings.HasPrefix(member, "serviceAccount:"):
		subject := ServiceAccountSubject(strings.TrimPrefix(member, "serviceAccount:"))
		return subject, validateSubject(subject)
	default:
		return Subject{}, fmt.Errorf("%w: member must use user: or serviceAccount: prefix", ErrInvalid)
	}
}

func policyMemberFromRef(ref spicedb.SubjectRef) (string, bool) {
	id, ok := decodeOpaqueObjectID(ref.ID)
	if !ok {
		return "", false
	}
	switch ref.Type {
	case subjectTypeUser:
		return "user:" + id, true
	case subjectTypeServiceAccount:
		return "serviceAccount:" + id, true
	default:
		return "", false
	}
}

func validateOrgID(orgID string) error {
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("%w: org_id is required", ErrInvalid)
	}
	return nil
}

func validateSubject(subject Subject) error {
	if strings.TrimSpace(subject.ID) == "" {
		return fmt.Errorf("%w: subject id is required", ErrInvalid)
	}
	switch subject.Kind {
	case SubjectKindUser, SubjectKindServiceAccount:
		return nil
	default:
		return fmt.Errorf("%w: unsupported subject kind %q", ErrInvalid, subject.Kind)
	}
}

func isZitadelRole(role string) bool {
	for _, definition := range zitadelRoleDefinitions {
		if definition.RoleKey == role {
			return true
		}
	}
	return false
}

func roleDefinitionByPublicRole() map[string]roleDefinition {
	out := map[string]roleDefinition{}
	for _, definition := range policyRoleDefinitions {
		out[definition.PublicRole] = definition
	}
	return out
}

func roleDefinitionByRoleKey() map[string]roleDefinition {
	out := map[string]roleDefinition{}
	for _, definition := range policyRoleDefinitions {
		out[definition.RoleKey] = definition
	}
	return out
}

func roleKeys(definitions []roleDefinition) []string {
	out := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		out = append(out, definition.RoleKey)
	}
	return compactSorted(out)
}

func orgResource(orgID string) spicedb.ResourceRef {
	return spicedb.ResourceRef{Type: resourceTypeOrg, ID: strings.TrimSpace(orgID)}
}

func roleResource(orgID, roleKey string) spicedb.ResourceRef {
	return spicedb.ResourceRef{Type: resourceTypeRole, ID: roleObjectID(orgID, roleKey)}
}

func orgRoleGrant(orgID, relation, roleKey string) spicedb.Relationship {
	return spicedb.Relationship{
		Resource: orgResource(orgID),
		Relation: relation,
		Subject: spicedb.SubjectRef{
			Type:     resourceTypeRole,
			ID:       roleObjectID(orgID, roleKey),
			Relation: subjectRelationMember,
		},
	}
}

func roleObjectID(orgID, roleKey string) string {
	return "org_" + strings.TrimSpace(orgID) + "_role_" + strings.TrimSpace(roleKey)
}

func roleKeyFromObjectID(orgID, objectID string) (string, bool) {
	prefix := "org_" + strings.TrimSpace(orgID) + "_role_"
	if !strings.HasPrefix(objectID, prefix) {
		return "", false
	}
	return strings.TrimPrefix(objectID, prefix), true
}

func encodeOpaqueObjectID(value string) string {
	return "b64_" + base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(value)))
}

func decodeOpaqueObjectID(value string) (string, bool) {
	const prefix = "b64_"
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func relationSet(values ...string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func orgGrantKey(relation, roleID string) string {
	return relation + "\x00" + roleID
}

func metadata(operation, orgID string) map[string]any {
	return map[string]any{
		"operation": operation,
		"org_id":    strings.TrimSpace(orgID),
	}
}

func etag(bindings []PolicyBinding) string {
	parts := []string{}
	for _, binding := range bindings {
		for _, member := range compactSorted(binding.Members) {
			parts = append(parts, binding.Role+"\x00"+member)
		}
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return "sha256:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func compactSorted(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func lastToken(tokens ...string) string {
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i] != "" {
			return tokens[i]
		}
	}
	return ""
}
