package verself

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	iamcore "github.com/verself/verself-go/internal/transport/iam"
)

type (
	IAMRoleName   string
	IAMMemberName string
)

const (
	IAMRoleOwner           IAMRoleName = "roles/owner"
	IAMRoleAdmin           IAMRoleName = "roles/admin"
	IAMRoleMember          IAMRoleName = "roles/member"
	IAMRoleExecutionViewer IAMRoleName = "roles/executionViewer"
	IAMRoleBillingViewer   IAMRoleName = "roles/billingViewer"
	IAMRoleSourceViewer    IAMRoleName = "roles/sourceViewer"
	IAMRoleSecretsUser     IAMRoleName = "roles/secretsUser"
)

type Organization struct {
	OrgID        string `json:"orgId"`
	ResourceName string `json:"resourceName"`
	DisplayName  string `json:"displayName"`
	Slug         string `json:"slug,omitempty"`
	Version      int64  `json:"version"`
}

type OrganizationList struct {
	Organizations []Organization `json:"organizations"`
	NextPageToken string         `json:"nextPageToken,omitempty"`
}

type OrganizationSlugAvailability struct {
	Slug      string `json:"slug"`
	Available bool   `json:"available"`
}

type SignupStartResult struct {
	Message               string `json:"message"`
	Status                string `json:"status"`
	VerificationExpiresAt string `json:"verificationExpiresAt"`
}

type Member struct {
	OrgID        string `json:"orgId"`
	MemberID     string `json:"memberId"`
	ResourceName string `json:"resourceName"`
	Email        string `json:"email"`
	DisplayName  string `json:"displayName"`
}

type MemberInvitation struct {
	OrgID        string        `json:"orgId"`
	MemberID     string        `json:"memberId"`
	ResourceName string        `json:"resourceName"`
	Email        string        `json:"email"`
	Status       string        `json:"status"`
	Roles        []IAMRoleName `json:"roles"`
}

type MemberList struct {
	Members       []Member `json:"members"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

type IAMPolicy struct {
	Version  int64              `json:"version"`
	Bindings []IAMPolicyBinding `json:"bindings"`
	Etag     string             `json:"etag,omitempty"`
}

type IAMPolicyBinding struct {
	Role    IAMRoleName     `json:"role"`
	Members []IAMMemberName `json:"members"`
}

type ListOrganizationsOptions struct {
	PageSize  int
	PageToken string
}

type CreateOrganizationInput struct {
	DisplayName    string
	Slug           *string
	IdempotencyKey string
}

type StartSignupInput struct {
	Email                   string
	OrganizationDisplayName string
	OrganizationSlug        *string
	GivenName               *string
	FamilyName              *string
	IdempotencyKey          string
}

type VerifySignupInput struct {
	SignupIntentID          string
	VerificationToken       string
	OrganizationDisplayName *string
	OrganizationSlug        *string
	IdempotencyKey          string
}

type SignupVerificationResult struct {
	Organization Organization                `json:"organization"`
	LoginURL     string                      `json:"loginUrl"`
	LoginIntent  *RequiredAccountLoginIntent `json:"loginIntent,omitempty"`
}

type RequiredAccountLoginIntent struct {
	LoginURL        string  `json:"loginUrl"`
	Purpose         string  `json:"purpose"`
	RequiredSubject string  `json:"requiredSubject"`
	RequiredEmail   string  `json:"requiredEmail"`
	RequiredOrgID   string  `json:"requiredOrgId"`
	RedirectTo      *string `json:"redirectTo,omitempty"`
}

type UpdateOrganizationInput struct {
	OrgID          string
	Version        int64
	DisplayName    *string
	Slug           *string
	IdempotencyKey string
}

type ListMembersOptions struct {
	OrgID     string
	PageSize  int
	PageToken string
}

type GetMemberInput struct {
	OrgID    string
	MemberID string
}

type InviteMemberInput struct {
	OrgID          string
	Email          string
	GivenName      *string
	FamilyName     *string
	Roles          []IAMRoleName
	IdempotencyKey string
}

type SetIamPolicyInput struct {
	OrgID          string
	Policy         IAMPolicy
	IdempotencyKey string
}

type TestIamPermissionsInput struct {
	OrgID       string
	Permissions []string
}

type IAMClient struct {
	client *iamcore.Client
}

func (c *IAMClient) ListOrganizations(ctx context.Context, options ListOrganizationsOptions) (OrganizationList, error) {
	if c == nil || c.client == nil {
		return OrganizationList{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	request := iamcore.ListOrganizationsRequest{}
	if options.PageSize > 0 {
		pageSize := iamcore.PageSize(options.PageSize)
		request.PageSize = &pageSize
	}
	if strings.TrimSpace(options.PageToken) != "" {
		pageToken := strings.TrimSpace(options.PageToken)
		request.PageToken = &pageToken
	}
	response, err := c.client.ListOrganizations(ctx, request)
	if err != nil {
		return OrganizationList{}, err
	}
	if response.Result == nil {
		return OrganizationList{}, iamAPIError("list organizations", response.StatusCode, response.Problem, response.Body)
	}
	return organizationListFromGenerated(*response.Result), nil
}

func (c *IAMClient) CreateOrganization(ctx context.Context, input CreateOrganizationInput) (Organization, error) {
	if c == nil || c.client == nil {
		return Organization{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return Organization{}, fmt.Errorf("verself sdk: display name is required")
	}
	key, err := mutationKey("iam-organization-create", input.IdempotencyKey)
	if err != nil {
		return Organization{}, err
	}
	body := iamcore.CreateOrganizationInputBody{
		DisplayName: iamcore.DisplayName(displayName),
	}
	if input.Slug != nil {
		body.Slug = trimStringPointer(input.Slug)
	}
	response, err := c.client.CreateOrganization(ctx, iamcore.CreateOrganizationRequest{
		IdempotencyKey: iamcore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return Organization{}, err
	}
	if response.Result == nil {
		return Organization{}, iamAPIError("create organization", response.StatusCode, response.Problem, response.Body)
	}
	return organizationFromGenerated(*response.Result), nil
}

func (c *IAMClient) CheckOrganizationSlugAvailability(ctx context.Context, slug string) (OrganizationSlugAvailability, error) {
	if c == nil || c.client == nil {
		return OrganizationSlugAvailability{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return OrganizationSlugAvailability{}, fmt.Errorf("verself sdk: organization slug is required")
	}
	response, err := c.client.CheckOrganizationSlugAvailability(ctx, iamcore.CheckOrganizationSlugAvailabilityRequest{Slug: iamcore.OrgSlug(slug)})
	if err != nil {
		return OrganizationSlugAvailability{}, err
	}
	if response.Result == nil {
		return OrganizationSlugAvailability{}, iamAPIError("check organization slug availability", response.StatusCode, response.Problem, response.Body)
	}
	return OrganizationSlugAvailability{
		Slug:      string(response.Result.Slug),
		Available: response.Result.Available,
	}, nil
}

func (c *IAMClient) StartSignup(ctx context.Context, input StartSignupInput) (SignupStartResult, error) {
	if c == nil || c.client == nil {
		return SignupStartResult{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	email := strings.TrimSpace(input.Email)
	displayName := strings.TrimSpace(input.OrganizationDisplayName)
	if email == "" || displayName == "" {
		return SignupStartResult{}, fmt.Errorf("verself sdk: email and organization display name are required")
	}
	key, err := deterministicMutationKey("iam-signup-start", input.IdempotencyKey, email, displayName, stringPointerValue(input.OrganizationSlug), stringPointerValue(input.GivenName), stringPointerValue(input.FamilyName))
	if err != nil {
		return SignupStartResult{}, err
	}
	body := iamcore.StartSignupInputBody{
		Email:                   iamcore.EmailAddress(email),
		OrganizationDisplayName: iamcore.DisplayName(displayName),
	}
	if input.OrganizationSlug != nil {
		body.OrganizationSlug = trimStringPointer(input.OrganizationSlug)
	}
	if input.GivenName != nil {
		body.GivenName = trimStringPointer(input.GivenName)
	}
	if input.FamilyName != nil {
		body.FamilyName = trimStringPointer(input.FamilyName)
	}
	response, err := c.client.StartSignup(ctx, iamcore.StartSignupRequest{
		IdempotencyKey: iamcore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return SignupStartResult{}, err
	}
	if response.Result == nil {
		return SignupStartResult{}, iamAPIError("start signup", response.StatusCode, response.Problem, response.Body)
	}
	return SignupStartResult{
		Message:               response.Result.Message,
		Status:                string(response.Result.Status),
		VerificationExpiresAt: response.Result.VerificationExpiresAt,
	}, nil
}

func (c *IAMClient) VerifySignup(ctx context.Context, input VerifySignupInput) (SignupVerificationResult, error) {
	if c == nil || c.client == nil {
		return SignupVerificationResult{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	signupIntentID := strings.TrimSpace(input.SignupIntentID)
	verificationToken := strings.TrimSpace(input.VerificationToken)
	if signupIntentID == "" || verificationToken == "" {
		return SignupVerificationResult{}, fmt.Errorf("verself sdk: signup intent id and verification token are required")
	}
	key, err := deterministicMutationKey("iam-signup-verify", input.IdempotencyKey, signupIntentID, verificationToken, stringPointerValue(input.OrganizationDisplayName), stringPointerValue(input.OrganizationSlug))
	if err != nil {
		return SignupVerificationResult{}, err
	}
	response, err := c.client.VerifySignup(ctx, iamcore.VerifySignupRequest{
		SignupIntentID: iamcore.SignupIntentId(signupIntentID),
		IdempotencyKey: iamcore.IdempotencyKey(key),
		Body: iamcore.VerifySignupInputBody{
			VerificationToken:       iamcore.SignupVerificationToken(verificationToken),
			OrganizationDisplayName: optionalCoreString[iamcore.DisplayName](input.OrganizationDisplayName),
			OrganizationSlug:        optionalCoreString[iamcore.OrgSlug](input.OrganizationSlug),
		},
	})
	if err != nil {
		return SignupVerificationResult{}, err
	}
	if response.Result == nil {
		return SignupVerificationResult{}, iamAPIError("verify signup", response.StatusCode, response.Problem, response.Body)
	}
	return signupVerificationResultFromGenerated(*response.Result), nil
}

func (c *IAMClient) GetOrganization(ctx context.Context, orgID string) (Organization, error) {
	if c == nil || c.client == nil {
		return Organization{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	id := strings.TrimSpace(orgID)
	if id == "" {
		return Organization{}, fmt.Errorf("verself sdk: org id is required")
	}
	response, err := c.client.GetOrganization(ctx, iamcore.GetOrganizationRequest{OrgID: id})
	if err != nil {
		return Organization{}, err
	}
	if response.Result == nil {
		return Organization{}, iamAPIError("get organization", response.StatusCode, response.Problem, response.Body)
	}
	return organizationFromGenerated(*response.Result), nil
}

func (c *IAMClient) UpdateOrganization(ctx context.Context, input UpdateOrganizationInput) (Organization, error) {
	if c == nil || c.client == nil {
		return Organization{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	orgID := strings.TrimSpace(input.OrgID)
	if orgID == "" {
		return Organization{}, fmt.Errorf("verself sdk: org id is required")
	}
	key, err := mutationKey("iam-organization", input.IdempotencyKey)
	if err != nil {
		return Organization{}, err
	}
	body := iamcore.UpdateOrganizationInputBody{
		Version: iamcore.OrganizationVersion(input.Version),
	}
	if input.DisplayName != nil {
		body.DisplayName = trimStringPointer(input.DisplayName)
	}
	if input.Slug != nil {
		body.Slug = trimStringPointer(input.Slug)
	}
	response, err := c.client.UpdateOrganization(ctx, iamcore.UpdateOrganizationRequest{
		OrgID:          orgID,
		IdempotencyKey: key,
		Body:           body,
	})
	if err != nil {
		return Organization{}, err
	}
	if response.Result == nil {
		return Organization{}, iamAPIError("update organization", response.StatusCode, response.Problem, response.Body)
	}
	return organizationFromGenerated(*response.Result), nil
}

func (c *IAMClient) ListMembers(ctx context.Context, options ListMembersOptions) (MemberList, error) {
	if c == nil || c.client == nil {
		return MemberList{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	orgID := strings.TrimSpace(options.OrgID)
	if orgID == "" {
		return MemberList{}, fmt.Errorf("verself sdk: org id is required")
	}
	request := iamcore.ListMembersRequest{OrgID: orgID}
	if options.PageSize > 0 {
		pageSize := iamcore.PageSize(options.PageSize)
		request.PageSize = &pageSize
	}
	if strings.TrimSpace(options.PageToken) != "" {
		pageToken := strings.TrimSpace(options.PageToken)
		request.PageToken = &pageToken
	}
	response, err := c.client.ListMembers(ctx, request)
	if err != nil {
		return MemberList{}, err
	}
	if response.Result == nil {
		return MemberList{}, iamAPIError("list members", response.StatusCode, response.Problem, response.Body)
	}
	return memberListFromGenerated(*response.Result), nil
}

func (c *IAMClient) GetMember(ctx context.Context, input GetMemberInput) (Member, error) {
	if c == nil || c.client == nil {
		return Member{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	orgID := strings.TrimSpace(input.OrgID)
	memberID := strings.TrimSpace(input.MemberID)
	if orgID == "" || memberID == "" {
		return Member{}, fmt.Errorf("verself sdk: org id and member id are required")
	}
	response, err := c.client.GetMember(ctx, iamcore.GetMemberRequest{
		OrgID:    orgID,
		MemberID: memberID,
	})
	if err != nil {
		return Member{}, err
	}
	if response.Result == nil {
		return Member{}, iamAPIError("get member", response.StatusCode, response.Problem, response.Body)
	}
	return memberFromGenerated(*response.Result), nil
}

func (c *IAMClient) InviteMember(ctx context.Context, input InviteMemberInput) (MemberInvitation, error) {
	if c == nil || c.client == nil {
		return MemberInvitation{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	orgID := strings.TrimSpace(input.OrgID)
	email := strings.TrimSpace(input.Email)
	if orgID == "" || email == "" {
		return MemberInvitation{}, fmt.Errorf("verself sdk: org id and email are required")
	}
	key, err := mutationKey("iam-member-invite", input.IdempotencyKey)
	if err != nil {
		return MemberInvitation{}, err
	}
	roles := make(iamcore.InviteMemberRoles, 0, len(input.Roles))
	for _, role := range input.Roles {
		role = IAMRoleName(strings.TrimSpace(string(role)))
		if role != "" {
			roles = append(roles, iamcore.IAMRoleName(role))
		}
	}
	body := iamcore.InviteMemberInputBody{
		Email: iamcore.EmailAddress(email),
		Roles: roles,
	}
	if input.GivenName != nil {
		body.GivenName = trimStringPointer(input.GivenName)
	}
	if input.FamilyName != nil {
		body.FamilyName = trimStringPointer(input.FamilyName)
	}
	response, err := c.client.InviteMember(ctx, iamcore.InviteMemberRequest{
		OrgID:          iamcore.OrgId(orgID),
		IdempotencyKey: iamcore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return MemberInvitation{}, err
	}
	if response.Result == nil {
		return MemberInvitation{}, iamAPIError("invite member", response.StatusCode, response.Problem, response.Body)
	}
	return memberInvitationFromGenerated(*response.Result), nil
}

func (c *IAMClient) GetIamPolicy(ctx context.Context, orgID string) (IAMPolicy, error) {
	if c == nil || c.client == nil {
		return IAMPolicy{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	id := strings.TrimSpace(orgID)
	if id == "" {
		return IAMPolicy{}, fmt.Errorf("verself sdk: org id is required")
	}
	response, err := c.client.GetIamPolicy(ctx, iamcore.GetIamPolicyRequest{OrgID: id})
	if err != nil {
		return IAMPolicy{}, err
	}
	if response.Result == nil {
		return IAMPolicy{}, iamAPIError("get iam policy", response.StatusCode, response.Problem, response.Body)
	}
	return policyFromGenerated(*response.Result), nil
}

func (c *IAMClient) SetIamPolicy(ctx context.Context, input SetIamPolicyInput) (IAMPolicy, error) {
	if c == nil || c.client == nil {
		return IAMPolicy{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	orgID := strings.TrimSpace(input.OrgID)
	if orgID == "" {
		return IAMPolicy{}, fmt.Errorf("verself sdk: org id is required")
	}
	key, err := mutationKey("iam-policy", input.IdempotencyKey)
	if err != nil {
		return IAMPolicy{}, err
	}
	response, err := c.client.SetIamPolicy(ctx, iamcore.SetIamPolicyRequest{
		OrgID:          orgID,
		IdempotencyKey: key,
		Body: iamcore.SetIamPolicyInputBody{
			Policy: policyToGenerated(input.Policy),
		},
	})
	if err != nil {
		return IAMPolicy{}, err
	}
	if response.Result == nil {
		return IAMPolicy{}, iamAPIError("set iam policy", response.StatusCode, response.Problem, response.Body)
	}
	return policyFromGenerated(*response.Result), nil
}

func (c *IAMClient) TestIamPermissions(ctx context.Context, input TestIamPermissionsInput) ([]string, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	orgID := strings.TrimSpace(input.OrgID)
	if orgID == "" {
		return nil, fmt.Errorf("verself sdk: org id is required")
	}
	permissions := make([]iamcore.PermissionName, 0, len(input.Permissions))
	for _, permission := range input.Permissions {
		permission = strings.TrimSpace(permission)
		if permission != "" {
			permissions = append(permissions, iamcore.PermissionName(permission))
		}
	}
	response, err := c.client.TestIamPermissions(ctx, iamcore.TestIamPermissionsRequest{
		OrgID: orgID,
		Body:  iamcore.TestIamPermissionsInputBody{Permissions: permissions},
	})
	if err != nil {
		return nil, err
	}
	if response.Result == nil {
		return nil, iamAPIError("test iam permissions", response.StatusCode, response.Problem, response.Body)
	}
	out := make([]string, 0, len(response.Result.Permissions))
	for _, permission := range response.Result.Permissions {
		out = append(out, string(permission))
	}
	return out, nil
}

func organizationListFromGenerated(input iamcore.ListOrganizationsOutputBody) OrganizationList {
	out := OrganizationList{}
	if input.NextPageToken != nil {
		out.NextPageToken = *input.NextPageToken
	}
	if input.Organizations != nil {
		out.Organizations = make([]Organization, 0, len(input.Organizations))
		for _, organization := range input.Organizations {
			out.Organizations = append(out.Organizations, organizationFromGenerated(organization))
		}
	}
	return out
}

func organizationFromGenerated(input iamcore.OrganizationSummary) Organization {
	return Organization{
		OrgID:        input.OrgID,
		ResourceName: input.ResourceName,
		DisplayName:  input.DisplayName,
		Slug:         stringValue(input.Slug),
		Version:      int64(input.Version),
	}
}

func deterministicMutationKey(namespace, explicit string, parts ...string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return mutationKey(namespace, explicit)
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(namespace))
	for _, part := range parts {
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(strings.TrimSpace(part)))
	}
	encoded := base64.RawURLEncoding.EncodeToString(digest.Sum(nil))
	return namespace + ":" + encoded[:32], nil
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func signupVerificationResultFromGenerated(input iamcore.VerifySignupResult) SignupVerificationResult {
	return SignupVerificationResult{
		Organization: organizationFromGenerated(input.Organization),
		LoginURL:     string(input.LoginURL),
		LoginIntent:  requiredAccountLoginIntentFromGenerated(input.LoginIntent),
	}
}

func requiredAccountLoginIntentFromGenerated(input *iamcore.RequiredAccountLoginIntent) *RequiredAccountLoginIntent {
	if input == nil {
		return nil
	}
	return &RequiredAccountLoginIntent{
		LoginURL:        string(input.LoginURL),
		Purpose:         input.Purpose,
		RequiredSubject: input.RequiredSubject,
		RequiredEmail:   input.RequiredEmail,
		RequiredOrgID:   string(input.RequiredOrgID),
		RedirectTo:      input.RedirectTo,
	}
}

func memberListFromGenerated(input iamcore.ListMembersOutputBody) MemberList {
	out := MemberList{}
	if input.NextPageToken != nil {
		out.NextPageToken = *input.NextPageToken
	}
	if input.Members != nil {
		out.Members = make([]Member, 0, len(input.Members))
		for _, member := range input.Members {
			out.Members = append(out.Members, memberFromGenerated(member))
		}
	}
	return out
}

func memberFromGenerated(input iamcore.MemberSummary) Member {
	return Member{
		OrgID:        input.OrgID,
		MemberID:     input.MemberID,
		ResourceName: input.ResourceName,
		Email:        input.Email,
		DisplayName:  input.DisplayName,
	}
}

func memberInvitationFromGenerated(input iamcore.MemberInvitationSummary) MemberInvitation {
	roles := make([]IAMRoleName, 0, len(input.Roles))
	for _, role := range input.Roles {
		roles = append(roles, IAMRoleName(role))
	}
	return MemberInvitation{
		OrgID:        input.OrgID,
		MemberID:     input.MemberID,
		ResourceName: input.ResourceName,
		Email:        input.Email,
		Status:       string(input.Status),
		Roles:        roles,
	}
}

func policyFromGenerated(input iamcore.IAMPolicy) IAMPolicy {
	out := IAMPolicy{
		Version: int64(input.Version),
	}
	if input.Etag != nil {
		out.Etag = string(*input.Etag)
	}
	if input.Bindings != nil {
		out.Bindings = make([]IAMPolicyBinding, 0, len(input.Bindings))
		for _, binding := range input.Bindings {
			members := make([]IAMMemberName, 0, len(binding.Members))
			for _, member := range binding.Members {
				members = append(members, IAMMemberName(member))
			}
			out.Bindings = append(out.Bindings, IAMPolicyBinding{
				Role:    IAMRoleName(binding.Role),
				Members: members,
			})
		}
	}
	return out
}

func policyToGenerated(input IAMPolicy) iamcore.IAMPolicy {
	out := iamcore.IAMPolicy{
		Version:  iamcore.PolicyVersion(input.Version),
		Bindings: make([]iamcore.IAMPolicyBinding, 0, len(input.Bindings)),
	}
	if strings.TrimSpace(input.Etag) != "" {
		etag := iamcore.PolicyEtag(strings.TrimSpace(input.Etag))
		out.Etag = &etag
	}
	for _, binding := range input.Bindings {
		members := make([]iamcore.IAMMemberName, 0, len(binding.Members))
		for _, member := range binding.Members {
			if strings.TrimSpace(string(member)) != "" {
				members = append(members, iamcore.IAMMemberName(strings.TrimSpace(string(member))))
			}
		}
		out.Bindings = append(out.Bindings, iamcore.IAMPolicyBinding{
			Role:    iamcore.IAMRoleName(strings.TrimSpace(string(binding.Role))),
			Members: members,
		})
	}
	return out
}

func iamAPIError(operation string, statusCode int, model *iamcore.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	var metadata apiErrorMetadata
	if model != nil {
		title = model.Title
		detail = model.Detail
		metadata = apiErrorMetadata{
			Code:        stringValue(model.Code),
			RequestID:   stringValue(model.RequestID),
			Traceparent: stringValue(model.Traceparent),
		}
	}
	return apiErrorFields("IAM API", operation, statusCode, title, detail, body, metadata)
}

func stringValue[T ~string](input *T) string {
	if input == nil {
		return ""
	}
	return string(*input)
}

func optionalCoreString[T ~string](input *string) *T {
	if input == nil {
		return nil
	}
	value := T(strings.TrimSpace(*input))
	if value == "" {
		return nil
	}
	return &value
}
