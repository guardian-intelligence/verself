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
	if decision := apiOperationRateLimiter.Allow(runtimeiam.RateLimitClass(desc.RateLimitBucket), contractRateLimitKey(ctx, authIdentity, desc, orgIDs), time.Now()); !decision.Allowed {
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
	case "request_subject":
		if r.authz == nil {
			return nil, authz.ErrUnavailable
		}
		orgIDs, _, err := r.authz.LookupOrganizations(ctx, authzSubjectFromIdentity(authIdentity), "read", "")
		return orgIDs, err
	case "input_member":
		publicOrgID := stringFromInputMember(input, desc.Authorization.OrganizationMember)
		if publicOrgID == "" {
			return nil, badRequest(ctx, "organization-required", "path orgId is required", nil)
		}
		return []string{publicOrgID}, nil
	default:
		return nil, internalFailure(ctx, "unsupported-organization-scope", "unsupported organization scope", nil)
	}
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

func (r publicRuntime) identityHasContractPermission(ctx context.Context, authIdentity *auth.Identity, desc contractapi.OperationDescriptor, orgIDs []string) (bool, error) {
	if authIdentity == nil || desc.Authorization.Permission == "" {
		return false, nil
	}
	if r.authz == nil {
		return false, authz.ErrUnavailable
	}
	if desc.Authorization.OrganizationSource == "request_subject" {
		return true, nil
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
	case "request_subject":
		return string(runtimeiam.OrgScopeRequestSubject)
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
	organizations, err := h.service.AccessibleOrganizations(ctx, authzSubjectFromIdentity(authIdentity))
	if err != nil {
		return nil, identityError(ctx, err)
	}
	return &contractapi.ListOrganizationsOutput{
		Body: contractapi.ListOrganizationsOutputBody{
			Organizations: h.organizationSummariesFromMetadata(organizations),
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

func (h publicHandlers) GetIamPolicy(ctx context.Context, input *contractapi.GetIamPolicyInput) (*contractapi.GetIamPolicyOutput, error) {
	if _, err := h.principalForPublicOrg(ctx, input.OrgID); err != nil {
		return nil, err
	}
	if h.authz == nil {
		return nil, authzError(ctx, authz.ErrUnavailable)
	}
	policy, err := h.authz.GetOrganizationPolicy(ctx, string(input.OrgID))
	if err != nil {
		return nil, authzError(ctx, err)
	}
	return &contractapi.GetIamPolicyOutput{Body: contractPolicy(policy)}, nil
}

func (h publicHandlers) SetIamPolicy(ctx context.Context, input *contractapi.SetIamPolicyInput) (*contractapi.SetIamPolicyOutput, error) {
	if _, err := h.principalForPublicOrg(ctx, input.OrgID); err != nil {
		return nil, err
	}
	if h.authz == nil {
		return nil, authzError(ctx, authz.ErrUnavailable)
	}
	policy, err := authzPolicy(input.Body.Policy)
	if err != nil {
		return nil, badRequest(ctx, "invalid-iam-policy", "IAM policy is invalid", err)
	}
	out, err := h.authz.SetOrganizationPolicy(ctx, string(input.OrgID), policy, contractapi.SetIamPolicy.Descriptor.OperationID)
	if err != nil {
		return nil, authzError(ctx, err)
	}
	return &contractapi.SetIamPolicyOutput{Body: contractPolicy(out)}, nil
}

func (h publicHandlers) TestIamPermissions(ctx context.Context, input *contractapi.TestIamPermissionsInput) (*contractapi.TestIamPermissionsOutput, error) {
	if _, err := h.principalForPublicOrg(ctx, input.OrgID); err != nil {
		return nil, err
	}
	if h.authz == nil {
		return nil, authzError(ctx, authz.ErrUnavailable)
	}
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	permissions := make([]string, 0, len(input.Body.Permissions))
	for _, permission := range input.Body.Permissions {
		permissions = append(permissions, string(permission))
	}
	allowed, _, err := h.authz.TestOrganizationPermissions(ctx, string(input.OrgID), authzSubjectFromIdentity(authIdentity), permissions, "")
	if err != nil {
		return nil, authzError(ctx, err)
	}
	out := make(contractapi.Permissions, 0, len(allowed))
	for _, permission := range allowed {
		out = append(out, contractapi.PermissionName(permission))
	}
	return &contractapi.TestIamPermissionsOutput{Body: contractapi.TestIamPermissionsOutputBody{Permissions: out}}, nil
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
	if publicOrgID == "" {
		return identity.Principal{}, badRequest(ctx, "organization-required", "path orgId is required", nil)
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

func (h publicHandlers) organizationSummariesFromMetadata(organizations []identity.OrganizationMetadata) []contractapi.OrganizationSummary {
	out := make([]contractapi.OrganizationSummary, 0, len(organizations))
	for _, organization := range organizations {
		publicOrgID := contractapi.OrgID(organization.OrgID)
		out = append(out, contractapi.OrganizationSummary{
			OrgID:        publicOrgID,
			ResourceName: publicOrganizationResourceName(h.installationID, publicOrgID),
			Slug:         optionalContractValue[contractapi.OrgSlug](organization.Slug),
			DisplayName:  contractapi.DisplayName(organization.DisplayName),
			Version:      contractapi.OrganizationVersion(organization.Version),
		})
	}
	return out
}

func (h publicHandlers) organizationSummary(org identity.Organization) contractapi.OrganizationSummary {
	publicOrgID := contractapi.OrgID(org.OrgID)
	return contractapi.OrganizationSummary{
		OrgID:        publicOrgID,
		ResourceName: publicOrganizationResourceName(h.installationID, publicOrgID),
		Slug:         optionalContractValue[contractapi.OrgSlug](org.Slug),
		DisplayName:  contractapi.DisplayName(org.DisplayName),
		Version:      contractapi.OrganizationVersion(org.Version),
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
	}
}

func contractPolicy(policy authz.Policy) contractapi.IAMPolicy {
	bindings := make(contractapi.IAMPolicyBindings, 0, len(policy.Bindings))
	for _, binding := range policy.Bindings {
		members := make([]contractapi.IAMMemberName, 0, len(binding.Members))
		for _, member := range binding.Members {
			members = append(members, contractapi.IAMMemberName(member))
		}
		bindings = append(bindings, contractapi.IAMPolicyBinding{
			Role:    contractapi.IAMRoleName(binding.Role),
			Members: members,
		})
	}
	var etag *contractapi.PolicyEtag
	if strings.TrimSpace(policy.Etag) != "" {
		value := contractapi.PolicyEtag(policy.Etag)
		etag = &value
	}
	return contractapi.IAMPolicy{
		Version:  contractapi.PolicyVersion(policy.Version),
		Bindings: bindings,
		Etag:     etag,
	}
}

func authzPolicy(policy contractapi.IAMPolicy) (authz.Policy, error) {
	if policy.Version <= 0 {
		return authz.Policy{}, fmt.Errorf("%w: policy version must be positive", authz.ErrInvalid)
	}
	version, err := int32FromInt64(int64(policy.Version), "policy version")
	if err != nil {
		return authz.Policy{}, fmt.Errorf("%w: %v", authz.ErrInvalid, err)
	}
	bindings := make([]authz.PolicyBinding, 0, len(policy.Bindings))
	for _, binding := range policy.Bindings {
		members := make([]string, 0, len(binding.Members))
		for _, member := range binding.Members {
			members = append(members, string(member))
		}
		bindings = append(bindings, authz.PolicyBinding{
			Role:    string(binding.Role),
			Members: members,
		})
	}
	etag := ""
	if policy.Etag != nil {
		etag = string(*policy.Etag)
	}
	return authz.Policy{
		Version:  version,
		Etag:     etag,
		Bindings: bindings,
	}, nil
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
