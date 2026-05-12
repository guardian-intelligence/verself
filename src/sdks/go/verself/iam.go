package verself

import (
	"context"
	"fmt"
	"strings"

	iamcore "github.com/verself/verself-go/internal/generated/iam"
)

type OrganizationRole string

const (
	OrganizationRoleOwner  OrganizationRole = "owner"
	OrganizationRoleAdmin  OrganizationRole = "admin"
	OrganizationRoleMember OrganizationRole = "member"
)

type Organization struct {
	OrgID         string `json:"orgId"`
	ResourceName  string `json:"resourceName"`
	DisplayName   string `json:"displayName"`
	Slug          string `json:"slug,omitempty"`
	CallerRole    string `json:"callerRole"`
	Version       int64  `json:"version"`
	OrgACLVersion int64  `json:"orgAclVersion"`
}

type OrganizationList struct {
	Organizations []Organization `json:"organizations"`
	NextPageToken string         `json:"nextPageToken,omitempty"`
}

type Member struct {
	OrgID        string `json:"orgId"`
	MemberID     string `json:"memberId"`
	ResourceName string `json:"resourceName"`
	Email        string `json:"email"`
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
}

type MemberList struct {
	Members       []Member `json:"members"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

type ListOrganizationsOptions struct {
	PageSize  int
	PageToken string
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

type UpdateMemberRoleInput struct {
	OrgID                 string
	MemberID              string
	Role                  OrganizationRole
	ExpectedRole          OrganizationRole
	ExpectedOrgACLVersion int64
	IdempotencyKey        string
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
		Version: input.Version,
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

func (c *IAMClient) UpdateMemberRole(ctx context.Context, input UpdateMemberRoleInput) (Member, error) {
	if c == nil || c.client == nil {
		return Member{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	orgID := strings.TrimSpace(input.OrgID)
	memberID := strings.TrimSpace(input.MemberID)
	if orgID == "" || memberID == "" {
		return Member{}, fmt.Errorf("verself sdk: org id and member id are required")
	}
	key, err := mutationKey("iam-member-role", input.IdempotencyKey)
	if err != nil {
		return Member{}, err
	}
	body := iamcore.UpdateMemberRoleInputBody{
		ExpectedOrgACLVersion: input.ExpectedOrgACLVersion,
		ExpectedRole:          iamcore.OrganizationRole(strings.TrimSpace(string(input.ExpectedRole))),
		Role:                  iamcore.OrganizationRole(strings.TrimSpace(string(input.Role))),
	}
	response, err := c.client.UpdateMemberRole(ctx, iamcore.UpdateMemberRoleRequest{
		OrgID:          orgID,
		MemberID:       memberID,
		IdempotencyKey: key,
		Body:           body,
	})
	if err != nil {
		return Member{}, err
	}
	if response.Result == nil {
		return Member{}, iamAPIError("update member role", response.StatusCode, response.Problem, response.Body)
	}
	return memberFromGenerated(*response.Result), nil
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
		OrgID:         input.OrgID,
		ResourceName:  input.ResourceName,
		DisplayName:   input.DisplayName,
		Slug:          stringValue(input.Slug),
		CallerRole:    string(input.CallerRole),
		Version:       input.Version,
		OrgACLVersion: input.OrgACLVersion,
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
		Role:         string(input.Role),
	}
}

func iamAPIError(operation string, statusCode int, model *iamcore.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	if model != nil {
		title = model.Title
		detail = model.Detail
	}
	return apiErrorFields("IAM API", operation, statusCode, title, detail, body)
}

func stringValue(input *string) string {
	if input == nil {
		return ""
	}
	return *input
}
