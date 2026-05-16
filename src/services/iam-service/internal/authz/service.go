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
	resourceTypeOrg              = "org"
	resourceTypeRole             = "role"
	resourceTypeProject          = "project"
	resourceTypeAnalyticsDataset = "analytics_dataset"
	resourceTypeAPIActivity      = "api_activity"

	relationMember        = "member"
	relationParentProject = "parent_project"
	relationParentOrg     = "parent_org"

	subjectTypeUser           = "user"
	subjectTypeServiceAccount = "service_account"
	subjectTypeWorkload       = "workload"
	subjectRelationMember     = "member"

	publicRoleOwner           = "roles/owner"
	publicRoleAdmin           = "roles/admin"
	publicRoleMember          = "roles/member"
	publicRoleExecutionViewer = "roles/executionViewer"
	publicRoleBillingViewer   = "roles/billingViewer"
	publicRoleSourceViewer    = "roles/sourceViewer"
	publicRoleSecretsUser     = "roles/secretsUser"

	roleKeyOwner           = "owner"
	roleKeyAdmin           = "admin"
	roleKeyMember          = "member"
	roleKeyExecutionViewer = "execution_lister"
	roleKeyBillingViewer   = "billing_viewer"
	roleKeySourceViewer    = "source_viewer"
	roleKeySecretsUser     = "secret_user"
)

var (
	ErrUnavailable = errors.New("authorization graph unavailable")
	ErrInvalid     = errors.New("invalid authorization graph request")
	ErrConflict    = errors.New("authorization graph conflict")
)

type Backend interface {
	Check(ctx context.Context, resource spicedb.ResourceRef, permission string, subject spicedb.SubjectRef, minZedToken string) (bool, string, error)
	LookupResources(ctx context.Context, resourceType, permission string, subject spicedb.SubjectRef, limit uint32, minZedToken string) ([]string, string, error)
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
	SubjectKindWorkload       SubjectKind = "workload"
	SubjectKindRole           SubjectKind = "role"
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

type ResourceRef struct {
	Type string
	ID   string
}

type ResourceAuthorizationDecision struct {
	Allowed             bool
	OrgID               string
	Subject             Subject
	OperationPermission string
	Resource            ResourceRef
	ResourcePermission  string
	ZedToken            string
}

type ResourceParentEdge struct {
	Resource  ResourceRef
	Relation  string
	Parent    ResourceRef
	ZedToken  string
	Operation string
}

type roleDefinition struct {
	PublicRole string
	RoleKey    string
	Relation   string
}

var policyRoleDefinitions = []roleDefinition{
	{PublicRole: publicRoleOwner, RoleKey: roleKeyOwner, Relation: "owner"},
	{PublicRole: publicRoleAdmin, RoleKey: roleKeyAdmin, Relation: "admin"},
	{PublicRole: publicRoleMember, RoleKey: roleKeyMember, Relation: "member"},
	{PublicRole: publicRoleExecutionViewer, RoleKey: roleKeyExecutionViewer, Relation: "execution_lister"},
	{PublicRole: publicRoleBillingViewer, RoleKey: roleKeyBillingViewer, Relation: "billing_viewer"},
	{PublicRole: publicRoleSourceViewer, RoleKey: roleKeySourceViewer, Relation: "source_viewer"},
	{PublicRole: publicRoleSecretsUser, RoleKey: roleKeySecretsUser, Relation: "secret_user"},
}

var orgPermissionByProductPermission = map[string]string{
	identity.PermissionOrganizationList:              "read",
	identity.PermissionOrganizationRead:              "read",
	identity.PermissionOrganizationUpdate:            "manage_iam",
	identity.PermissionMemberList:                    "read",
	identity.PermissionMemberRead:                    "read",
	identity.PermissionMemberInvite:                  "invite_members",
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
	identity.PermissionSandboxCacheRead:              "list_executions",
	identity.PermissionSandboxCacheWrite:             "manage_iam",
	identity.PermissionBillingRead:                   "view_billing",
	identity.PermissionBillingCheckout:               "manage_iam",
	identity.PermissionProjectRead:                   "view_source",
	identity.PermissionProjectWrite:                  "manage_iam",
	identity.PermissionProjectEnvironmentRead:        "view_source",
	identity.PermissionProjectEnvironmentWrite:       "manage_iam",
	identity.PermissionProjectEventRead:              "view_source",
	identity.PermissionProjectResolve:                "view_source",
	identity.PermissionSourceRepoRead:                "view_source",
	identity.PermissionSourceRepoWrite:               "manage_iam",
	identity.PermissionSourceCheckoutWrite:           "manage_iam",
	identity.PermissionSourceIntegrationWrite:        "manage_iam",
	identity.PermissionSourceGitCredentialWrite:      "manage_iam",
	identity.PermissionSourceWorkflowRead:            "view_source",
	identity.PermissionSourceWorkflowWrite:           "manage_iam",
	identity.PermissionSecretWrite:                   "manage_iam",
	identity.PermissionSecretRead:                    "use_secrets",
	identity.PermissionSecretList:                    "use_secrets",
	identity.PermissionSecretDelete:                  "manage_iam",
	identity.PermissionVariableWrite:                 "manage_iam",
	identity.PermissionVariableRead:                  "use_secrets",
	identity.PermissionVariableList:                  "use_secrets",
	identity.PermissionVariableDelete:                "manage_iam",
	identity.PermissionCredentialCreate:              "manage_iam",
	identity.PermissionCredentialRead:                "manage_iam",
	identity.PermissionCredentialList:                "manage_iam",
	identity.PermissionCredentialRoll:                "manage_iam",
	identity.PermissionCredentialRevoke:              "manage_iam",
	identity.PermissionTransitKeyCreate:              "manage_iam",
	identity.PermissionTransitKeyRotate:              "manage_iam",
	identity.PermissionTransitEncrypt:                "use_secrets",
	identity.PermissionTransitDecrypt:                "use_secrets",
	identity.PermissionTransitSign:                   "use_secrets",
	identity.PermissionTransitVerify:                 "use_secrets",
	identity.PermissionAnalyticsDatasetRead:          "read",
	identity.PermissionAnalyticsDatasetReadRaw:       "manage_iam",
	identity.PermissionAnalyticsDatasetIngest:        "manage_iam",
	identity.PermissionAnalyticsDatasetManage:        "manage_iam",
	identity.PermissionGovernanceAPIActivityRead:     "manage_iam",
	identity.PermissionGovernanceAPIActivityExport:   "manage_iam",
	identity.PermissionProfileRead:                   "read",
	identity.PermissionProfileIdentityWrite:          "read",
	identity.PermissionProfilePreferencesWrite:       "read",
	identity.PermissionNotificationsRead:             "read",
	identity.PermissionNotificationsWrite:            "read",
	identity.PermissionNotificationsPreferencesWrite: "read",
	identity.PermissionNotificationsTest:             "read",
	identity.PermissionMailboxAccountRead:            "read",
	identity.PermissionMailboxMailRead:               "read",
	identity.PermissionMailboxMailWrite:              "read",
	identity.PermissionMailboxSyncStatusRead:         "read",
	identity.PermissionObjectStorageBucketRead:       "read",
	identity.PermissionObjectStorageBucketWrite:      "manage_iam",
	identity.PermissionObjectStorageAccessKeyRead:    "manage_iam",
	identity.PermissionObjectStorageAccessKeyWrite:   "manage_iam",
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

func WorkloadSubject(id string) Subject {
	return Subject{Kind: SubjectKindWorkload, ID: strings.TrimSpace(id)}
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
	if strings.TrimSpace(policy.Etag) == "" && len(currentPolicy.Bindings) > 0 {
		return Policy{}, fmt.Errorf("%w: etag is required", ErrConflict)
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

func (s *Service) TestOrganizationPermissions(ctx context.Context, orgID string, authSubject identity.AuthorizationSubject, permissions []string, minZedToken string) ([]string, string, error) {
	if err := validateOrgID(orgID); err != nil {
		return nil, "", err
	}
	subject := subjectFromAuthorizationSubject(authSubject)
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

func (s *Service) LookupOrganizations(ctx context.Context, authSubject identity.AuthorizationSubject, permission, minZedToken string) ([]string, string, error) {
	subject := subjectFromAuthorizationSubject(authSubject)
	if err := validateSubject(subject); err != nil {
		return nil, "", err
	}
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return nil, "", fmt.Errorf("%w: permission is required", ErrInvalid)
	}
	if s == nil || s.backend == nil {
		return nil, "", ErrUnavailable
	}
	orgIDs, zedToken, err := s.backend.LookupResources(ctx, resourceTypeOrg, permission, subjectRef(subject), 0, minZedToken)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return compactSorted(orgIDs), zedToken, nil
}

func (s *Service) CheckResourcePermission(ctx context.Context, orgID string, authSubject identity.AuthorizationSubject, resource ResourceRef, resourcePermission, operationPermission, minZedToken string) (ResourceAuthorizationDecision, error) {
	if err := validateOrgID(orgID); err != nil {
		return ResourceAuthorizationDecision{}, err
	}
	subject := subjectFromAuthorizationSubject(authSubject)
	if err := validateSubject(subject); err != nil {
		return ResourceAuthorizationDecision{}, err
	}
	resource = normalizeResourceRef(resource)
	resourcePermission = strings.TrimSpace(resourcePermission)
	operationPermission = strings.TrimSpace(operationPermission)
	if err := validateResourcePermission(resource, resourcePermission); err != nil {
		return ResourceAuthorizationDecision{}, err
	}
	if operationPermission != "" {
		if _, ok := identity.KnownPermissions()[operationPermission]; !ok {
			return ResourceAuthorizationDecision{}, fmt.Errorf("%w: unknown product permission %q", ErrInvalid, operationPermission)
		}
	}
	if s == nil || s.backend == nil {
		return ResourceAuthorizationDecision{}, ErrUnavailable
	}
	decision := ResourceAuthorizationDecision{
		OrgID:               strings.TrimSpace(orgID),
		Subject:             subject,
		OperationPermission: operationPermission,
		Resource:            resource,
		ResourcePermission:  resourcePermission,
	}
	ok, token, err := s.backend.Check(ctx, spicedbResourceRef(resource), resourcePermission, subjectRef(subject), strings.TrimSpace(minZedToken))
	if err != nil {
		return ResourceAuthorizationDecision{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if token != "" {
		decision.ZedToken = token
	}
	decision.Allowed = ok
	return decision, nil
}

func (s *Service) WriteResourceParentEdge(ctx context.Context, orgID string, resource ResourceRef, relation string, parent ResourceRef, operation string) (ResourceParentEdge, error) {
	if err := validateOrgID(orgID); err != nil {
		return ResourceParentEdge{}, err
	}
	resource = normalizeResourceRef(resource)
	parent = normalizeResourceRef(parent)
	relation = strings.TrimSpace(relation)
	if err := validateResourceParentEdge(orgID, resource, relation, parent); err != nil {
		return ResourceParentEdge{}, err
	}
	if s == nil || s.backend == nil {
		return ResourceParentEdge{}, ErrUnavailable
	}
	spiceResource := spicedbResourceRef(resource)
	current, _, err := s.backend.ReadResourceRelationships(ctx, spiceResource, relationSet(relation))
	if err != nil {
		return ResourceParentEdge{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	desired := []spicedb.Relationship{{
		Resource: spiceResource,
		Relation: relation,
		Subject: spicedb.SubjectRef{
			Type: spicedbResourceRef(parent).Type,
			ID:   spicedbResourceRef(parent).ID,
		},
	}}
	edge := ResourceParentEdge{
		Resource:  resource,
		Relation:  relation,
		Parent:    parent,
		Operation: strings.TrimSpace(operation),
	}
	zedToken, err := s.replace(ctx, current, desired, metadata(firstNonEmpty(operation, "iam.write_resource_parent_edge"), orgID))
	if err != nil {
		return ResourceParentEdge{}, err
	}
	edge.ZedToken = zedToken
	return edge, nil
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

func (s *Service) currentPolicyRelationships(ctx context.Context, orgID string) ([]spicedb.Relationship, string, error) {
	current, zedToken, err := s.currentRoleMemberships(ctx, orgID, policyRoleNames(policyRoleDefinitions), nil)
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

func (s *Service) currentPolicyOrgRoleGrants(ctx context.Context, orgID string) ([]spicedb.Relationship, string, error) {
	return s.currentOrgRoleGrants(ctx, orgID, policyRoleDefinitions, nil)
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

func subjectFromAuthorizationSubject(subject identity.AuthorizationSubject) Subject {
	switch subject.Kind {
	case identity.AuthorizationSubjectKindServiceAccount:
		return ServiceAccountSubject(subject.ID)
	case identity.AuthorizationSubjectKindWorkload:
		return WorkloadSubject(subject.ID)
	default:
		return UserSubject(subject.ID)
	}
}

func subjectRef(subject Subject) spicedb.SubjectRef {
	switch subject.Kind {
	case SubjectKindServiceAccount:
		return spicedb.SubjectRef{Type: subjectTypeServiceAccount, ID: encodeOpaqueObjectID(subject.ID)}
	case SubjectKindWorkload:
		return spicedb.SubjectRef{Type: subjectTypeWorkload, ID: encodeOpaqueObjectID(subject.ID)}
	case SubjectKindRole:
		return spicedb.SubjectRef{Type: resourceTypeRole, ID: subject.ID, Relation: subjectRelationMember}
	default:
		return spicedb.SubjectRef{Type: subjectTypeUser, ID: encodeOpaqueObjectID(subject.ID)}
	}
}

func sameSubject(ref spicedb.SubjectRef, subject Subject) bool {
	return ref.Type == subjectRef(subject).Type && ref.ID == subjectRef(subject).ID
}

func normalizeResourceRef(resource ResourceRef) ResourceRef {
	return ResourceRef{
		Type: strings.TrimSpace(resource.Type),
		ID:   strings.TrimSpace(resource.ID),
	}
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
	case strings.HasPrefix(member, "workload:"):
		subject := WorkloadSubject(strings.TrimPrefix(member, "workload:"))
		return subject, validateSubject(subject)
	case strings.HasPrefix(member, "principalSet://iam.verself.sh/organizations/"):
		subject, err := principalSetSubject(member)
		if err != nil {
			return Subject{}, err
		}
		return subject, validateSubject(subject)
	default:
		return Subject{}, fmt.Errorf("%w: member must use user:, serviceAccount:, workload:, or principalSet://iam.verself.sh/ prefix", ErrInvalid)
	}
}

func policyMemberFromRef(ref spicedb.SubjectRef) (string, bool) {
	id, ok := decodeOpaqueObjectID(ref.ID)
	switch ref.Type {
	case subjectTypeUser:
		if !ok {
			return "", false
		}
		return "user:" + id, true
	case subjectTypeServiceAccount:
		if !ok {
			return "", false
		}
		return "serviceAccount:" + id, true
	case subjectTypeWorkload:
		if !ok {
			return "", false
		}
		return "workload:" + id, true
	case resourceTypeRole:
		if ref.Relation != subjectRelationMember {
			return "", false
		}
		return principalSetMemberFromRoleObjectID(ref.ID)
	default:
		return "", false
	}
}

func principalSetSubject(member string) (Subject, error) {
	const prefix = "principalSet://iam.verself.sh/organizations/"
	rest := strings.TrimPrefix(strings.TrimSpace(member), prefix)
	parts := strings.Split(rest, "/roles/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return Subject{}, fmt.Errorf("%w: malformed principal set member %q", ErrInvalid, member)
	}
	orgID := strings.TrimSpace(parts[0])
	publicRole := "roles/" + strings.TrimSpace(parts[1])
	definition, ok := roleDefinitionByPublicRole()[publicRole]
	if !ok {
		return Subject{}, fmt.Errorf("%w: unsupported principal set role %q", ErrInvalid, publicRole)
	}
	return Subject{Kind: SubjectKindRole, ID: roleObjectID(orgID, definition.RoleKey)}, nil
}

func principalSetMemberFromRoleObjectID(roleID string) (string, bool) {
	const marker = "_role_"
	idx := strings.LastIndex(roleID, marker)
	if idx <= 0 || idx+len(marker) >= len(roleID) {
		return "", false
	}
	orgID := strings.TrimPrefix(roleID[:idx], "org_")
	roleKey := roleID[idx+len(marker):]
	definition, ok := roleDefinitionByRoleKey()[roleKey]
	if !ok {
		return "", false
	}
	return "principalSet://iam.verself.sh/organizations/" + orgID + "/" + definition.PublicRole, true
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
	case SubjectKindUser, SubjectKindServiceAccount, SubjectKindWorkload, SubjectKindRole:
		return nil
	default:
		return fmt.Errorf("%w: unsupported subject kind %q", ErrInvalid, subject.Kind)
	}
}

func validateResourcePermission(resource ResourceRef, permission string) error {
	if resource.Type == "" {
		return fmt.Errorf("%w: resource type is required", ErrInvalid)
	}
	if resource.ID == "" {
		return fmt.Errorf("%w: resource id is required", ErrInvalid)
	}
	if permission == "" {
		return fmt.Errorf("%w: resource permission is required", ErrInvalid)
	}
	switch resource.Type {
	case resourceTypeAnalyticsDataset:
		switch permission {
		case "read", "read_raw", "ingest", "manage":
			return nil
		default:
			return fmt.Errorf("%w: unsupported analytics_dataset permission %q", ErrInvalid, permission)
		}
	case resourceTypeAPIActivity:
		switch permission {
		case "read", "export", "append":
			return nil
		default:
			return fmt.Errorf("%w: unsupported api_activity permission %q", ErrInvalid, permission)
		}
	default:
		return fmt.Errorf("%w: unsupported resource type %q", ErrInvalid, resource.Type)
	}
}

func validateResourceParentEdge(orgID string, resource ResourceRef, relation string, parent ResourceRef) error {
	if relation == "" {
		return fmt.Errorf("%w: parent relation is required", ErrInvalid)
	}
	if resource.ID == "" || parent.ID == "" {
		return fmt.Errorf("%w: resource and parent ids are required", ErrInvalid)
	}
	switch {
	case resource.Type == resourceTypeProject && relation == relationParentOrg && parent.Type == resourceTypeOrg && parent.ID == strings.TrimSpace(orgID):
		return nil
	case resource.Type == resourceTypeAnalyticsDataset && relation == relationParentProject && parent.Type == resourceTypeProject:
		return nil
	case resource.Type == resourceTypeAPIActivity && relation == relationParentOrg && parent.Type == resourceTypeOrg && parent.ID == strings.TrimSpace(orgID) && resource.ID == strings.TrimSpace(orgID):
		return nil
	default:
		return fmt.Errorf("%w: unsupported resource parent edge %s#%s@%s", ErrInvalid, resource.Type, relation, parent.Type)
	}
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

func policyRoleNames(definitions []roleDefinition) []string {
	out := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		out = append(out, definition.RoleKey)
	}
	return compactSorted(out)
}

func spicedbResourceRef(resource ResourceRef) spicedb.ResourceRef {
	return spicedb.ResourceRef{Type: strings.TrimSpace(resource.Type), ID: strings.TrimSpace(resource.ID)}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func lastToken(tokens ...string) string {
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i] != "" {
			return tokens[i]
		}
	}
	return ""
}
