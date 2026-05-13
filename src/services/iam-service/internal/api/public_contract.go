package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/contractapi"
	"github.com/verself/iam-service/internal/identity"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

const (
	crockfordAlphabet       = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	publicIDPayloadLength   = 26
	contractIdempotencyNone = ""
)

type publicOperationIdentity struct {
	Auth         *auth.Identity
	PublicOrgIDs []string
}

type publicRuntime struct {
	service *identity.Service
	authz   *authz.Service
}

func (r publicRuntime) PrepareOperation(desc contractapi.OperationDescriptor, op huma.Operation) huma.Operation {
	if err := validateGeneratedOperation(desc); err != nil {
		panic(err)
	}
	if desc.RequestBodyMaxBytes > 0 {
		op.MaxBodyBytes = desc.RequestBodyMaxBytes
	}
	op.Errors = contractProblemStatuses(desc.Problems)
	if desc.Idempotency.Policy == string(runtimeiam.IdempotencyHeaderKey) {
		op.Parameters = appendIdempotencyKeyHeaderParameter(op.Parameters)
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-verself-contract"] = map[string]any{
		"shape_id":               desc.ShapeID,
		"operation_id":           desc.OperationID,
		"identity":               desc.Identity.Mode,
		"audience":               desc.Identity.Audience,
		"permission":             desc.Authorization.Permission,
		"organization_source":    desc.Authorization.OrganizationSource,
		"organization_member":    desc.Authorization.OrganizationMember,
		"audit_event":            desc.Audit.Event,
		"resource":               desc.Audit.Resource,
		"action":                 desc.Audit.Action,
		"rate_limit_bucket":      desc.RateLimitBucket,
		"request_body_max_bytes": desc.RequestBodyMaxBytes,
		"idempotency":            desc.Idempotency.Policy,
	}
	op.Security = []map[string][]string{{"bearerAuth": {}}}
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	return op
}

func (r publicRuntime) BeforeOperation(ctx context.Context, desc contractapi.OperationDescriptor, input any) (any, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	orgIDs, err := r.contractOrganizationScope(ctx, desc, authIdentity, input)
	if err != nil {
		return authIdentity, err
	}
	if err := r.synchronizeAuthorization(ctx, authIdentity, desc, orgIDs); err != nil {
		if errorsIsAuthz(err) {
			return authIdentity, authzError(ctx, err)
		}
		return authIdentity, identityError(ctx, err)
	}
	allowed, err := r.identityHasContractPermission(ctx, authIdentity, desc, orgIDs)
	if err != nil {
		if errorsIsAuthz(err) {
			return authIdentity, authzError(ctx, err)
		}
		return authIdentity, identityError(ctx, err)
	}
	if !allowed {
		return authIdentity, forbidden(ctx, "permission-denied", fmt.Sprintf("missing required permission %q", desc.Authorization.Permission))
	}
	if err := requireContractIdempotency(ctx, desc); err != nil {
		return authIdentity, err
	}
	if decision := apiOperationRateLimiter.allow(runtimeiam.RateLimitClass(desc.RateLimitBucket), contractRateLimitKey(ctx, authIdentity, desc, orgIDs), time.Now()); !decision.Allowed {
		return authIdentity, rateLimitExceeded(ctx, decision.RetryAfter)
	}
	return publicOperationIdentity{Auth: authIdentity, PublicOrgIDs: orgIDs}, nil
}

func (r publicRuntime) AfterOperation(ctx context.Context, desc contractapi.OperationDescriptor, authIdentity any, input any, output any, err error) {
	outcome := "allowed"
	if err != nil {
		outcome = "error"
		var statusErr huma.StatusError
		if errors.As(err, &statusErr) {
			switch statusErr.GetStatus() {
			case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
				outcome = "denied"
			}
		}
	}
	identityValue := publicAuditIdentity(authIdentity)
	auditOperation(ctx, huma.Operation{OperationID: desc.OperationID}, operationPolicyFromContract(desc), identityValue, input, output, outcome, err)
}

func validateGeneratedOperation(desc contractapi.OperationDescriptor) error {
	if strings.TrimSpace(desc.OperationID) == "" || strings.TrimSpace(desc.Method) == "" || strings.TrimSpace(desc.Path) == "" {
		return fmt.Errorf("generated operation missing id, method, or path: %#v", desc)
	}
	if desc.Identity.Mode != "bearer" {
		return fmt.Errorf("%s: unsupported public identity mode %q", desc.OperationID, desc.Identity.Mode)
	}
	if desc.Identity.Audience != "iam-service" {
		return fmt.Errorf("%s: unexpected audience %q", desc.OperationID, desc.Identity.Audience)
	}
	if desc.Authorization.Permission == "" || desc.Audit.Event == "" || desc.Audit.Resource == "" || desc.Audit.Action == "" {
		return fmt.Errorf("%s: incomplete generated IAM metadata", desc.OperationID)
	}
	if runtimeiam.OperationRequiresBodyBudget(desc.Method) && desc.RequestBodyMaxBytes <= 0 {
		return fmt.Errorf("%s: mutating operation missing request body budget", desc.OperationID)
	}
	switch desc.Idempotency.Policy {
	case contractIdempotencyNone:
	case string(runtimeiam.IdempotencyHeaderKey):
		if !strings.EqualFold(desc.Idempotency.Header, "Idempotency-Key") {
			return fmt.Errorf("%s: unsupported idempotency header %q", desc.OperationID, desc.Idempotency.Header)
		}
	default:
		return fmt.Errorf("%s: unsupported idempotency policy %q", desc.OperationID, desc.Idempotency.Policy)
	}
	return nil
}

func (r publicRuntime) contractOrganizationScope(ctx context.Context, desc contractapi.OperationDescriptor, authIdentity *auth.Identity, input any) ([]string, error) {
	switch desc.Authorization.OrganizationSource {
	case "token_role_assignments":
		providerOrgIDs, err := roleAssignmentOrgIDs(ctx, authIdentity)
		if err != nil {
			return nil, err
		}
		organizations, err := r.service.AccessibleOrganizationsByProviderOrgIDs(ctx, authIdentity.Subject, providerOrgIDs)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return organizationMetadataOrgIDs(organizations), nil
	case "input_member":
		publicOrgID := stringFromInputMember(input, desc.Authorization.OrganizationMember)
		selectedPublicOrgID, err := r.publicOrgIDForProviderOrgID(ctx, authIdentity.OrgID)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		if publicOrgID != selectedPublicOrgID {
			return nil, forbidden(ctx, "organization-scope-mismatch", "path orgId must match the selected organization")
		}
		return []string{publicOrgID}, nil
	default:
		return nil, internalFailure(ctx, "unsupported-organization-scope", "unsupported organization scope", nil)
	}
}

func (r publicRuntime) publicOrgIDForProviderOrgID(ctx context.Context, providerOrgID string) (string, error) {
	if r.service == nil {
		return "", identity.ErrStoreUnavailable
	}
	profile, err := r.service.ResolveOrganization(ctx, identity.ResolveOrganizationRequest{
		IdentityProviderOrgID: strings.TrimSpace(providerOrgID),
		RequireActive:         true,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(profile.OrgID), nil
}

func stringFromInputMember(input any, member string) string {
	member = contractInputFieldName(member)
	value := reflectValue(input)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return ""
	}
	field := value.FieldByName(member)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return strings.TrimSpace(field.String())
}

func contractInputFieldName(member string) string {
	switch strings.TrimSpace(member) {
	case "orgId":
		return "OrgID"
	case "memberId":
		return "MemberID"
	default:
		if member == "" {
			return ""
		}
		return strings.ToUpper(member[:1]) + member[1:]
	}
}

func (r publicRuntime) synchronizeAuthorization(ctx context.Context, authIdentity *auth.Identity, desc contractapi.OperationDescriptor, orgIDs []string) error {
	if r.service == nil || authIdentity == nil {
		return identity.ErrStoreUnavailable
	}
	actor := authorizationActor(authIdentity)
	if actor == "" {
		return fmt.Errorf("%w: actor is required", identity.ErrInvalidInput)
	}
	for _, orgID := range orgIDs {
		if err := r.service.ReconcileOrganizationAuthorization(ctx, orgID, actor, "authorize-"+desc.Authorization.Permission); err != nil {
			return err
		}
	}
	return nil
}

func (r publicRuntime) identityHasContractPermission(ctx context.Context, authIdentity *auth.Identity, desc contractapi.OperationDescriptor, orgIDs []string) (bool, error) {
	if authIdentity == nil || desc.Authorization.Permission == "" {
		return false, nil
	}
	if r.authz == nil {
		return false, authz.ErrUnavailable
	}
	subject := authzSubjectFromIdentity(authIdentity)
	for _, orgID := range orgIDs {
		allowed, _, err := r.authz.TestOrganizationPermissions(ctx, orgID, subject, []string{desc.Authorization.Permission}, "")
		if err != nil {
			return false, err
		}
		if stringSliceContains(allowed, desc.Authorization.Permission) {
			return true, nil
		}
	}
	return false, nil
}

func requireContractIdempotency(ctx context.Context, desc contractapi.OperationDescriptor) error {
	if desc.Idempotency.Policy == "" {
		return nil
	}
	value := operationRequestInfoFromContext(ctx).IdempotencyKey
	value = strings.TrimSpace(value)
	if value == "" {
		return badRequest(ctx, "idempotency-key-required", "Idempotency-Key is required for this operation", nil)
	}
	if len(value) > maxIdempotencyKeyLength {
		return badRequest(ctx, "idempotency-key-too-long", "Idempotency-Key is too long", nil)
	}
	if strings.ContainsAny(value, "\x00\r\n\t") {
		return badRequest(ctx, "idempotency-key-invalid", "Idempotency-Key contains unsupported characters", nil)
	}
	return nil
}

func contractRateLimitKey(ctx context.Context, identity *auth.Identity, desc contractapi.OperationDescriptor, publicOrgIDs []string) string {
	info := operationRequestInfoFromContext(ctx)
	orgKey := strings.Join(publicOrgIDs, ",")
	return strings.Join([]string{
		desc.RateLimitBucket,
		desc.Authorization.Permission,
		orgKey,
		identity.Subject,
		info.ClientIP,
	}, "\x00")
}

func publicAuditIdentity(value any) *auth.Identity {
	switch typed := value.(type) {
	case publicOperationIdentity:
		if typed.Auth == nil {
			return nil
		}
		clone := *typed.Auth
		if len(typed.PublicOrgIDs) > 0 {
			clone.OrgID = typed.PublicOrgIDs[0]
		}
		return &clone
	case *auth.Identity:
		return typed
	default:
		return nil
	}
}

func operationPolicyFromContract(desc contractapi.OperationDescriptor) runtimeiam.OperationPolicy {
	return runtimeiam.OperationPolicy{
		Permission:     runtimeiam.Permission(desc.Authorization.Permission),
		Resource:       runtimeiam.ResourceKind(desc.Audit.Resource),
		Action:         runtimeiam.Action(desc.Audit.Action),
		OrgScope:       runtimeiam.OrgScope(runtimeOrgScopeFromContract(desc.Authorization.OrganizationSource)),
		RateLimitClass: runtimeiam.RateLimitClass(desc.RateLimitBucket),
		Idempotency:    runtimeiam.IdempotencyPolicy(desc.Idempotency.Policy),
		AuditEvent:     runtimeiam.AuditEvent(desc.Audit.Event),
		BodyLimitBytes: desc.RequestBodyMaxBytes,
	}
}

func contractProblemStatuses(problems []contractapi.ProblemDescriptor) []int {
	statuses := make([]int, 0, len(problems))
	for _, problem := range problems {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	return uniqueSortedStatusCodes(statuses)
}

func uniqueSortedStatusCodes(statuses []int) []int {
	sort.Ints(statuses)
	out := statuses[:0]
	previous := 0
	for _, status := range statuses {
		if status == previous {
			continue
		}
		out = append(out, status)
		previous = status
	}
	return out
}

func runtimeOrgScopeFromContract(source string) string {
	switch source {
	case "token_role_assignments":
		return string(runtimeiam.OrgScopeTokenRoleAssignmentOrgIDs)
	case "input_member":
		return string(runtimeiam.OrgScopePathOrgID)
	default:
		return source
	}
}

type publicHandlers struct {
	service        *identity.Service
	authz          *authz.Service
	installationID string
}

func (h publicHandlers) ListOrganizations(ctx context.Context, _ *contractapi.ListOrganizationsInput) (*contractapi.ListOrganizationsOutput, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	providerOrgIDs, err := roleAssignmentOrgIDs(ctx, authIdentity)
	if err != nil {
		return nil, err
	}
	organizations, err := h.service.AccessibleOrganizationsByProviderOrgIDs(ctx, authIdentity.Subject, providerOrgIDs)
	if err != nil {
		return nil, identityError(ctx, err)
	}
	authorizedPublicOrgIDs, err := authorizedOrgIDs(ctx, h.authz, authIdentity, organizationMetadataOrgIDs(organizations), runtimeiam.Permission(contractapi.ListOrganizations.Descriptor.Authorization.Permission))
	if err != nil {
		return nil, authzError(ctx, err)
	}
	organizations = filterOrganizationMetadataByOrgID(organizations, authorizedPublicOrgIDs)
	return &contractapi.ListOrganizationsOutput{
		Body: contractapi.ListOrganizationsOutputBody{
			Organizations: h.organizationSummariesFromMetadata(organizations, authIdentity),
		},
	}, nil
}

func (h publicHandlers) GetOrganization(ctx context.Context, input *contractapi.GetOrganizationInput) (*contractapi.GetOrganizationOutput, error) {
	org, err := h.organizationForPublicOrg(ctx, input.OrgID)
	if err != nil {
		return nil, err
	}
	return &contractapi.GetOrganizationOutput{Body: h.organizationSummary(org)}, nil
}

func (h publicHandlers) UpdateOrganization(ctx context.Context, input *contractapi.UpdateOrganizationInput) (*contractapi.UpdateOrganizationOutput, error) {
	principal, err := h.principalForPublicOrg(ctx, input.OrgID)
	if err != nil {
		return nil, err
	}
	displayName := contractString(input.Body.DisplayName)
	slug := contractString(input.Body.Slug)
	if strings.TrimSpace(displayName) == "" || strings.TrimSpace(slug) == "" {
		current, err := h.service.Organization(ctx, principal)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		if strings.TrimSpace(displayName) == "" {
			displayName = current.DisplayName
		}
		if strings.TrimSpace(slug) == "" {
			slug = current.Slug
		}
	}
	version, err := int32FromInt64(int64(input.Body.Version), "organization version")
	if err != nil {
		return nil, badRequest(ctx, "organization-version-out-of-range", "organization version is outside the supported range", err)
	}
	org, err := h.service.UpdateOrganization(ctx, principal, identity.UpdateOrganizationRequest{
		Version:     version,
		DisplayName: displayName,
		Slug:        slug,
	})
	if err != nil {
		return nil, identityError(ctx, err)
	}
	return &contractapi.UpdateOrganizationOutput{Body: h.organizationSummary(org)}, nil
}

func (h publicHandlers) ListMembers(ctx context.Context, input *contractapi.ListMembersInput) (*contractapi.ListMembersOutput, error) {
	principal, err := h.principalForPublicOrg(ctx, input.OrgID)
	if err != nil {
		return nil, err
	}
	members, err := h.service.Members(ctx, principal)
	if err != nil {
		return nil, identityError(ctx, err)
	}
	return &contractapi.ListMembersOutput{
		Body: contractapi.ListMembersOutputBody{
			Members: h.memberSummaries(principal.OrgID, members),
		},
	}, nil
}

func (h publicHandlers) GetMember(ctx context.Context, input *contractapi.GetMemberInput) (*contractapi.GetMemberOutput, error) {
	principal, member, err := h.memberForPublicID(ctx, input.OrgID, input.MemberID)
	if err != nil {
		return nil, err
	}
	return &contractapi.GetMemberOutput{Body: h.memberSummary(principal.OrgID, member)}, nil
}

func (h publicHandlers) UpdateMemberRole(ctx context.Context, input *contractapi.UpdateMemberRoleInput) (*contractapi.UpdateMemberRoleOutput, error) {
	principal, member, err := h.memberForPublicID(ctx, input.OrgID, input.MemberID)
	if err != nil {
		return nil, err
	}
	expectedOrgACLVersion, err := int32FromInt64(int64(input.Body.ExpectedOrgAclVersion), "expected organization ACL version")
	if err != nil {
		return nil, badRequest(ctx, "organization-acl-version-out-of-range", "organization ACL version is outside the supported range", err)
	}
	result, err := h.service.UpdateMemberRoles(ctx, principal, identity.UpdateMemberRolesCommand{
		UserID:                member.UserID,
		RoleKeys:              []string{string(input.Body.Role)},
		ExpectedRoleKeys:      []string{string(input.Body.ExpectedRole)},
		ExpectedOrgACLVersion: expectedOrgACLVersion,
		OperationID:           contractapi.UpdateMemberRole.Descriptor.OperationID,
		IdempotencyKey:        string(input.IdempotencyKey),
	})
	if err != nil {
		return nil, identityError(ctx, err)
	}
	return &contractapi.UpdateMemberRoleOutput{Body: h.memberSummary(principal.OrgID, result.Member)}, nil
}

func (h publicHandlers) organizationForPublicOrg(ctx context.Context, orgID contractapi.OrgID) (identity.Organization, error) {
	principal, err := h.principalForPublicOrg(ctx, orgID)
	if err != nil {
		return identity.Organization{}, err
	}
	org, err := h.service.Organization(ctx, principal)
	if err != nil {
		return identity.Organization{}, identityError(ctx, err)
	}
	return org, nil
}

func (h publicHandlers) principalForPublicOrg(ctx context.Context, orgID contractapi.OrgID) (identity.Principal, error) {
	publicOrgID := strings.TrimSpace(string(orgID))
	principal, err := principalFromContext(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	profile, err := h.service.ResolveOrganization(ctx, identity.ResolveOrganizationRequest{
		IdentityProviderOrgID: strings.TrimSpace(authIdentity.OrgID),
		RequireActive:         true,
	})
	if err != nil {
		return identity.Principal{}, identityError(ctx, err)
	}
	if publicOrgID != profile.OrgID {
		return identity.Principal{}, forbidden(ctx, "organization-scope-mismatch", "path orgId must match the selected organization")
	}
	principal.OrgID = publicOrgID
	return principal, nil
}

func (h publicHandlers) memberForPublicID(ctx context.Context, orgID contractapi.OrgID, memberID contractapi.MemberID) (identity.Principal, identity.Member, error) {
	principal, err := h.principalForPublicOrg(ctx, orgID)
	if err != nil {
		return identity.Principal{}, identity.Member{}, err
	}
	members, err := h.service.Members(ctx, principal)
	if err != nil {
		return identity.Principal{}, identity.Member{}, identityError(ctx, err)
	}
	target := strings.TrimSpace(string(memberID))
	for _, member := range members {
		if string(publicMemberID(member.UserID)) == target {
			return principal, member, nil
		}
	}
	return identity.Principal{}, identity.Member{}, notFound(ctx, "member-not-found", "organization member not found")
}

func (h publicHandlers) organizationSummariesFromMetadata(organizations []identity.OrganizationMetadata, authIdentity *auth.Identity) []contractapi.OrganizationSummary {
	callerRoles := callerRoleByPublicOrgID(authIdentity, organizations)
	out := make([]contractapi.OrganizationSummary, 0, len(organizations))
	for _, organization := range organizations {
		publicOrgID := contractapi.OrgID(organization.OrgID)
		callerRole := callerRoles[organization.OrgID]
		if callerRole == "" {
			callerRole = contractapi.OrganizationRoleMember
		}
		out = append(out, contractapi.OrganizationSummary{
			OrgID:         publicOrgID,
			ResourceName:  publicOrganizationResourceName(h.installationID, publicOrgID),
			Slug:          optionalContractValue[contractapi.OrgSlug](organization.Slug),
			DisplayName:   contractapi.DisplayName(organization.DisplayName),
			CallerRole:    callerRole,
			Version:       contractapi.OrganizationVersion(organization.Version),
			OrgAclVersion: contractapi.OrgAclVersion(organization.OrgACLVersion),
		})
	}
	return out
}

func callerRoleByPublicOrgID(authIdentity *auth.Identity, organizations []identity.OrganizationMetadata) map[string]contractapi.OrganizationRole {
	if authIdentity == nil {
		return nil
	}
	publicByProvider := make(map[string]string, len(organizations))
	for _, organization := range organizations {
		publicByProvider[strings.TrimSpace(organization.IdentityProviderOrgID)] = strings.TrimSpace(organization.OrgID)
	}
	rolesByOrgID := map[string][]string{}
	for _, assignment := range authIdentity.RoleAssignments {
		orgID := publicByProvider[strings.TrimSpace(assignment.OrganizationID)]
		role := strings.TrimSpace(assignment.Role)
		if orgID == "" || role == "" {
			continue
		}
		rolesByOrgID[orgID] = append(rolesByOrgID[orgID], role)
	}
	out := make(map[string]contractapi.OrganizationRole, len(rolesByOrgID))
	for orgID, roles := range rolesByOrgID {
		out[orgID] = contractapi.OrganizationRole(roleFromKeys(roles))
	}
	return out
}

func (h publicHandlers) organizationSummary(org identity.Organization) contractapi.OrganizationSummary {
	publicOrgID := contractapi.OrgID(org.OrgID)
	return contractapi.OrganizationSummary{
		OrgID:         publicOrgID,
		ResourceName:  publicOrganizationResourceName(h.installationID, publicOrgID),
		Slug:          optionalContractValue[contractapi.OrgSlug](org.Slug),
		DisplayName:   contractapi.DisplayName(org.DisplayName),
		CallerRole:    contractapi.OrganizationRole(roleFromKeys(org.Caller.RoleKeys)),
		Version:       contractapi.OrganizationVersion(org.Version),
		OrgAclVersion: contractapi.OrgAclVersion(org.OrgACLVersion),
	}
}

func (h publicHandlers) memberSummaries(orgID string, members []identity.Member) []contractapi.MemberSummary {
	out := make([]contractapi.MemberSummary, 0, len(members))
	for _, member := range members {
		out = append(out, h.memberSummary(orgID, member))
	}
	return out
}

func (h publicHandlers) memberSummary(orgID string, member identity.Member) contractapi.MemberSummary {
	publicOrgID := contractapi.OrgID(orgID)
	publicMemberID := publicMemberID(member.UserID)
	return contractapi.MemberSummary{
		OrgID:        publicOrgID,
		MemberID:     publicMemberID,
		ResourceName: publicMemberResourceName(h.installationID, publicOrgID, publicMemberID),
		Email:        contractapi.EmailAddress(member.Email),
		DisplayName:  contractapi.DisplayName(member.DisplayName),
		Role:         contractapi.OrganizationRole(roleFromKeys(member.RoleKeys)),
	}
}

func roleFromKeys(keys []string) string {
	for _, role := range []string{identity.RoleOwner, identity.RoleAdmin, identity.RoleMember} {
		if hasRole(keys, role) {
			return role
		}
	}
	return identity.RoleMember
}

func hasRole(keys []string, target string) bool {
	for _, key := range keys {
		if key == target {
			return true
		}
	}
	return false
}

func publicOrganizationResourceName(installationID string, orgID contractapi.OrgID) contractapi.OrganizationResourceName {
	return contractapi.OrganizationResourceName(fmt.Sprintf("urn:verself:%s:orgs/%s", strings.TrimSpace(installationID), orgID))
}

func publicMemberResourceName(installationID string, orgID contractapi.OrgID, memberID contractapi.MemberID) contractapi.MemberResourceName {
	return contractapi.MemberResourceName(fmt.Sprintf("urn:verself:%s:orgs/%s/members/%s", strings.TrimSpace(installationID), orgID, memberID))
}

func organizationMetadataOrgIDs(organizations []identity.OrganizationMetadata) []string {
	out := make([]string, 0, len(organizations))
	for _, organization := range organizations {
		if orgID := strings.TrimSpace(organization.OrgID); orgID != "" {
			out = append(out, orgID)
		}
	}
	return out
}

func filterOrganizationMetadataByOrgID(organizations []identity.OrganizationMetadata, orgIDs []string) []identity.OrganizationMetadata {
	if len(organizations) == 0 || len(orgIDs) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(orgIDs))
	for _, orgID := range orgIDs {
		if orgID = strings.TrimSpace(orgID); orgID != "" {
			allowed[orgID] = struct{}{}
		}
	}
	out := make([]identity.OrganizationMetadata, 0, len(organizations))
	for _, organization := range organizations {
		if _, ok := allowed[strings.TrimSpace(organization.OrgID)]; ok {
			out = append(out, organization)
		}
	}
	return out
}

func publicMemberID(userID string) contractapi.MemberID {
	sum := sha256.Sum256([]byte("iam-member\x00" + strings.TrimSpace(userID)))
	return contractapi.MemberID("member_" + crockfordEncodeBytes(sum[:])[:publicIDPayloadLength])
}

func crockfordEncodeBytes(raw []byte) string {
	if len(raw) == 0 {
		return strings.Repeat("0", publicIDPayloadLength)
	}
	var out strings.Builder
	var buffer uint16
	var bits uint8
	for _, b := range raw {
		buffer = (buffer << 8) | uint16(b)
		bits += 8
		for bits >= 5 {
			index := byte((buffer >> (bits - 5)) & 0x1f)
			out.WriteByte(crockfordAlphabet[index])
			bits -= 5
		}
	}
	if bits > 0 {
		index := byte((buffer << (5 - bits)) & 0x1f)
		out.WriteByte(crockfordAlphabet[index])
	}
	return out.String()
}

func errorsIsAuthz(err error) bool {
	return errors.Is(err, authz.ErrInvalid) || errors.Is(err, authz.ErrUnavailable) || errors.Is(err, authz.ErrConflict)
}
