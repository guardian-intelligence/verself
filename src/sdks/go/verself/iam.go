package verself

import (
	"context"
	"fmt"
	"strings"
	"time"

	iamcore "github.com/verself/verself-go/internal/generated/iam"
)

type APICredentialAuthMethod string

const (
	APICredentialAuthMethodPrivateKeyJWT APICredentialAuthMethod = "private_key_jwt"
	APICredentialAuthMethodClientSecret  APICredentialAuthMethod = "client_secret"
)

type Organization struct {
	OrgID              string                     `json:"org_id"`
	ResourceName       string                     `json:"resourceName"`
	DisplayName        string                     `json:"display_name"`
	Slug               string                     `json:"slug"`
	Version            int32                      `json:"version"`
	OrgACLVersion      int32                      `json:"org_acl_version"`
	Caller             Member                     `json:"caller"`
	MemberCapabilities MemberCapabilitiesDocument `json:"member_capabilities"`
	Permissions        []string                   `json:"permissions"`
}

type OrganizationMetadata struct {
	OrgID        string `json:"org_id"`
	ResourceName string `json:"resourceName"`
	DisplayName  string `json:"display_name"`
	Slug         string `json:"slug"`
}

type Member struct {
	UserID       string   `json:"user_id"`
	ResourceName string   `json:"resourceName,omitempty"`
	Email        string   `json:"email"`
	LoginName    string   `json:"login_name"`
	DisplayName  string   `json:"display_name"`
	State        string   `json:"state"`
	RoleKeys     []string `json:"role_keys"`
}

type MemberCapabilitiesDocument struct {
	OrgID       string    `json:"org_id"`
	Version     int32     `json:"version"`
	EnabledKeys []string  `json:"enabled_keys"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
}

type MemberCapability struct {
	Key            string   `json:"key"`
	Label          string   `json:"label"`
	Description    string   `json:"description"`
	DefaultEnabled bool     `json:"default_enabled"`
	Permissions    []string `json:"permissions"`
}

type MemberCapabilities struct {
	Document MemberCapabilitiesDocument `json:"document"`
	Catalog  []MemberCapability         `json:"catalog"`
}

type APICredential struct {
	CredentialID         string     `json:"credential_id"`
	ResourceName         string     `json:"resourceName"`
	OrgID                string     `json:"org_id"`
	SubjectID            string     `json:"subject_id"`
	ClientID             string     `json:"client_id"`
	DisplayName          string     `json:"display_name"`
	Status               string     `json:"status"`
	AuthMethod           string     `json:"auth_method"`
	Fingerprint          string     `json:"fingerprint"`
	Permissions          []string   `json:"permissions"`
	PolicyVersionAtIssue int32      `json:"policy_version_at_issue"`
	CreatedAt            time.Time  `json:"created_at"`
	CreatedBy            string     `json:"created_by"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	RevokedBy            string     `json:"revoked_by,omitempty"`
	LastUsedAt           *time.Time `json:"last_used_at,omitempty"`
}

type ServiceAccount struct {
	ServiceAccountID string     `json:"service_account_id"`
	ResourceName     string     `json:"resourceName"`
	OrgID            string     `json:"org_id"`
	SubjectID        string     `json:"subject_id"`
	ClientID         string     `json:"client_id"`
	DisplayName      string     `json:"display_name"`
	Description      string     `json:"description,omitempty"`
	Status           string     `json:"status"`
	Permissions      []string   `json:"permissions"`
	CreatedAt        time.Time  `json:"created_at"`
	CreatedBy        string     `json:"created_by"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DisabledAt       *time.Time `json:"disabled_at,omitempty"`
	DisabledBy       string     `json:"disabled_by,omitempty"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
}

type APICredentialIssuedMaterial struct {
	AuthMethod   string `json:"auth_method"`
	ClientID     string `json:"client_id"`
	TokenURL     string `json:"token_url"`
	KeyID        string `json:"key_id,omitempty"`
	KeyContent   string `json:"key_content,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Fingerprint  string `json:"fingerprint"`
}

type CreateAPICredentialInput struct {
	DisplayName    string
	AuthMethod     APICredentialAuthMethod
	Permissions    []string
	ExpiresAt      *time.Time
	IdempotencyKey string
}

type CreateAPICredentialResult struct {
	Credential     APICredential               `json:"credential"`
	IssuedMaterial APICredentialIssuedMaterial `json:"issued_material"`
}

type RollAPICredentialInput struct {
	CredentialID   string
	AuthMethod     APICredentialAuthMethod
	IdempotencyKey string
}

type RollAPICredentialResult struct {
	Credential     APICredential               `json:"credential"`
	IssuedMaterial APICredentialIssuedMaterial `json:"issued_material"`
}

type UpdateOrganizationInput struct {
	Version        int32
	DisplayName    *string
	Slug           *string
	IdempotencyKey string
}

type PutMemberCapabilitiesInput struct {
	Version        int32
	EnabledKeys    []string
	IdempotencyKey string
}

type InviteMemberInput struct {
	Email          string
	GivenName      string
	FamilyName     string
	RoleKeys       []string
	IdempotencyKey string
}

type UpdateMemberRolesInput struct {
	UserID                string
	RoleKeys              []string
	ExpectedRoleKeys      []string
	ExpectedOrgACLVersion int32
	IdempotencyKey        string
}

type RevokeAPICredentialInput struct {
	CredentialID   string
	IdempotencyKey string
}

type DisableServiceAccountInput struct {
	ServiceAccountID string
	IdempotencyKey   string
}

type IAMClient struct {
	client *iamcore.ClientWithResponses
}

func (c *IAMClient) GetOrganization(ctx context.Context) (Organization, error) {
	if c == nil || c.client == nil {
		return Organization{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	response, err := c.client.GetOrganizationWithResponse(ctx)
	if err != nil {
		return Organization{}, err
	}
	if response.JSON200 == nil {
		return Organization{}, iamAPIError("get organization", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return organizationFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) ListMyOrganizations(ctx context.Context) ([]OrganizationMetadata, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	response, err := c.client.ListMyOrganizationsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if response.JSON200 == nil {
		return nil, iamAPIError("list my organizations", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return organizationMetadataFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) UpdateOrganization(ctx context.Context, input UpdateOrganizationInput) (Organization, error) {
	if c == nil || c.client == nil {
		return Organization{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	key, err := mutationKey("iam-organization", input.IdempotencyKey)
	if err != nil {
		return Organization{}, err
	}
	body := iamcore.IAMUpdateOrganizationRequest{
		Version: input.Version,
	}
	if input.DisplayName != nil {
		body.DisplayName = trimStringPointer(input.DisplayName)
	}
	if input.Slug != nil {
		body.Slug = trimStringPointer(input.Slug)
	}
	response, err := c.client.PatchOrganizationWithResponse(ctx, &iamcore.PatchOrganizationParams{IdempotencyKey: key}, body)
	if err != nil {
		return Organization{}, err
	}
	if response.JSON200 == nil {
		return Organization{}, iamAPIError("update organization", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return organizationFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) ListMembers(ctx context.Context) ([]Member, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	response, err := c.client.ListOrganizationMembersWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if response.JSON200 == nil {
		return nil, iamAPIError("list organization members", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return membersFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) InviteMember(ctx context.Context, input InviteMemberInput) (iamcore.IAMInviteMemberResponse, error) {
	if c == nil || c.client == nil {
		return iamcore.IAMInviteMemberResponse{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	key, err := mutationKey("iam-member-invite", input.IdempotencyKey)
	if err != nil {
		return iamcore.IAMInviteMemberResponse{}, err
	}
	roleKeys := compactStringSlice(input.RoleKeys)
	body := iamcore.IAMInviteMemberRequest{
		Email:    strings.TrimSpace(input.Email),
		RoleKeys: &roleKeys,
	}
	if givenName := strings.TrimSpace(input.GivenName); givenName != "" {
		body.GivenName = &givenName
	}
	if familyName := strings.TrimSpace(input.FamilyName); familyName != "" {
		body.FamilyName = &familyName
	}
	response, err := c.client.InviteOrganizationMemberWithResponse(ctx, &iamcore.InviteOrganizationMemberParams{IdempotencyKey: key}, body)
	if err != nil {
		return iamcore.IAMInviteMemberResponse{}, err
	}
	if response.JSON201 == nil {
		return iamcore.IAMInviteMemberResponse{}, iamAPIError("invite organization member", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return *response.JSON201, nil
}

func (c *IAMClient) UpdateMemberRoles(ctx context.Context, input UpdateMemberRolesInput) (Member, error) {
	if c == nil || c.client == nil {
		return Member{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	key, err := mutationKey("iam-member-roles", input.IdempotencyKey)
	if err != nil {
		return Member{}, err
	}
	roleKeys := compactStringSlice(input.RoleKeys)
	expectedRoleKeys := compactStringSlice(input.ExpectedRoleKeys)
	body := iamcore.IAMUpdateMemberRolesRequest{
		ExpectedOrgAclVersion: input.ExpectedOrgACLVersion,
		ExpectedRoleKeys:      &expectedRoleKeys,
		RoleKeys:              &roleKeys,
	}
	userID := strings.TrimSpace(input.UserID)
	response, err := c.client.UpdateOrganizationMemberRolesWithResponse(ctx, userID, &iamcore.UpdateOrganizationMemberRolesParams{IdempotencyKey: key}, body)
	if err != nil {
		return Member{}, err
	}
	if response.JSON200 == nil {
		return Member{}, iamAPIError("update organization member roles", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return memberFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) GetMemberCapabilities(ctx context.Context) (MemberCapabilities, error) {
	if c == nil || c.client == nil {
		return MemberCapabilities{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	response, err := c.client.GetOrganizationMemberCapabilitiesWithResponse(ctx)
	if err != nil {
		return MemberCapabilities{}, err
	}
	if response.JSON200 == nil {
		return MemberCapabilities{}, iamAPIError("get member capabilities", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return memberCapabilitiesFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) PutMemberCapabilities(ctx context.Context, input PutMemberCapabilitiesInput) (MemberCapabilities, error) {
	if c == nil || c.client == nil {
		return MemberCapabilities{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	key, err := mutationKey("iam-member-capabilities", input.IdempotencyKey)
	if err != nil {
		return MemberCapabilities{}, err
	}
	enabledKeys := compactStringSlice(input.EnabledKeys)
	response, err := c.client.PutOrganizationMemberCapabilitiesWithResponse(ctx, &iamcore.PutOrganizationMemberCapabilitiesParams{IdempotencyKey: key}, iamcore.IAMPutMemberCapabilitiesRequest{
		EnabledKeys: &enabledKeys,
		Version:     input.Version,
	})
	if err != nil {
		return MemberCapabilities{}, err
	}
	if response.JSON200 == nil {
		return MemberCapabilities{}, iamAPIError("put member capabilities", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return memberCapabilitiesFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) ListServiceAccounts(ctx context.Context) ([]ServiceAccount, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	response, err := c.client.ListServiceAccountsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if response.JSON200 == nil {
		return nil, iamAPIError("list service accounts", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return serviceAccountsFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) GetServiceAccount(ctx context.Context, serviceAccountID string) (ServiceAccount, error) {
	if c == nil || c.client == nil {
		return ServiceAccount{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	id := strings.TrimSpace(serviceAccountID)
	response, err := c.client.GetServiceAccountWithResponse(ctx, id)
	if err != nil {
		return ServiceAccount{}, err
	}
	if response.JSON200 == nil {
		return ServiceAccount{}, iamAPIError("get service account", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return serviceAccountFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) DisableServiceAccount(ctx context.Context, input DisableServiceAccountInput) (ServiceAccount, error) {
	if c == nil || c.client == nil {
		return ServiceAccount{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	key, err := mutationKey("iam-service-account-disable", input.IdempotencyKey)
	if err != nil {
		return ServiceAccount{}, err
	}
	id := strings.TrimSpace(input.ServiceAccountID)
	response, err := c.client.DisableServiceAccountWithResponse(ctx, id, &iamcore.DisableServiceAccountParams{IdempotencyKey: key})
	if err != nil {
		return ServiceAccount{}, err
	}
	if response.JSON200 == nil {
		return ServiceAccount{}, iamAPIError("disable service account", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return serviceAccountFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) ListAPICredentials(ctx context.Context) ([]APICredential, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	response, err := c.client.ListApiCredentialsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if response.JSON200 == nil {
		return nil, iamAPIError("list api credentials", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return apiCredentialsFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) CreateAPICredential(ctx context.Context, input CreateAPICredentialInput) (CreateAPICredentialResult, error) {
	if c == nil || c.client == nil {
		return CreateAPICredentialResult{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	key, err := mutationKey("iam-api-credential", input.IdempotencyKey)
	if err != nil {
		return CreateAPICredentialResult{}, err
	}
	permissions := compactStringSlice(input.Permissions)
	body := iamcore.IAMCreateAPICredentialRequest{
		DisplayName: strings.TrimSpace(input.DisplayName),
		ExpiresAt:   input.ExpiresAt,
		Permissions: &permissions,
	}
	if input.AuthMethod != "" {
		authMethod := iamcore.IAMCreateAPICredentialRequestAuthMethod(input.AuthMethod)
		body.AuthMethod = &authMethod
	}
	response, err := c.client.CreateApiCredentialWithResponse(ctx, &iamcore.CreateApiCredentialParams{IdempotencyKey: key}, body)
	if err != nil {
		return CreateAPICredentialResult{}, err
	}
	if response.JSON201 == nil {
		return CreateAPICredentialResult{}, iamAPIError("create api credential", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return createAPICredentialResultFromGenerated(*response.JSON201), nil
}

func (c *IAMClient) GetAPICredential(ctx context.Context, credentialID string) (APICredential, error) {
	if c == nil || c.client == nil {
		return APICredential{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	id := strings.TrimSpace(credentialID)
	response, err := c.client.GetApiCredentialWithResponse(ctx, id)
	if err != nil {
		return APICredential{}, err
	}
	if response.JSON200 == nil {
		return APICredential{}, iamAPIError("get api credential", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return apiCredentialFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) RollAPICredential(ctx context.Context, input RollAPICredentialInput) (RollAPICredentialResult, error) {
	if c == nil || c.client == nil {
		return RollAPICredentialResult{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	key, err := mutationKey("iam-api-credential-roll", input.IdempotencyKey)
	if err != nil {
		return RollAPICredentialResult{}, err
	}
	body := iamcore.IAMRollAPICredentialRequest{}
	if input.AuthMethod != "" {
		authMethod := iamcore.IAMRollAPICredentialRequestAuthMethod(input.AuthMethod)
		body.AuthMethod = &authMethod
	}
	id := strings.TrimSpace(input.CredentialID)
	response, err := c.client.RollApiCredentialWithResponse(ctx, id, &iamcore.RollApiCredentialParams{IdempotencyKey: key}, body)
	if err != nil {
		return RollAPICredentialResult{}, err
	}
	if response.JSON200 == nil {
		return RollAPICredentialResult{}, iamAPIError("roll api credential", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return rollAPICredentialResultFromGenerated(*response.JSON200), nil
}

func (c *IAMClient) RevokeAPICredential(ctx context.Context, input RevokeAPICredentialInput) (APICredential, error) {
	if c == nil || c.client == nil {
		return APICredential{}, fmt.Errorf("verself sdk: iam client is not initialized")
	}
	key, err := mutationKey("iam-api-credential-revoke", input.IdempotencyKey)
	if err != nil {
		return APICredential{}, err
	}
	id := strings.TrimSpace(input.CredentialID)
	response, err := c.client.RevokeApiCredentialWithResponse(ctx, id, &iamcore.RevokeApiCredentialParams{IdempotencyKey: key})
	if err != nil {
		return APICredential{}, err
	}
	if response.JSON200 == nil {
		return APICredential{}, iamAPIError("revoke api credential", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return apiCredentialFromGenerated(*response.JSON200), nil
}

func organizationFromGenerated(input iamcore.IAMOrganization) Organization {
	return Organization{
		OrgID:              input.OrgId,
		ResourceName:       input.ResourceName,
		DisplayName:        input.DisplayName,
		Slug:               input.Slug,
		Version:            input.Version,
		OrgACLVersion:      input.OrgAclVersion,
		Caller:             memberFromGenerated(input.Caller),
		MemberCapabilities: memberCapabilitiesDocumentFromGenerated(input.MemberCapabilities),
		Permissions:        stringSliceValue(input.Permissions),
	}
}

func organizationMetadataFromGenerated(input []iamcore.IAMOrganizationMetadata) []OrganizationMetadata {
	out := make([]OrganizationMetadata, 0, len(input))
	for _, organization := range input {
		out = append(out, OrganizationMetadata{
			OrgID:        organization.OrgId,
			ResourceName: organization.ResourceName,
			DisplayName:  organization.DisplayName,
			Slug:         organization.Slug,
		})
	}
	return out
}

func membersFromGenerated(input iamcore.IAMMembers) []Member {
	if input.Members == nil {
		return nil
	}
	out := make([]Member, 0, len(*input.Members))
	for _, member := range *input.Members {
		out = append(out, memberFromGenerated(member))
	}
	return out
}

func memberFromGenerated(input iamcore.IAMMember) Member {
	return Member{
		UserID:       input.UserId,
		ResourceName: stringValue(input.ResourceName),
		Email:        input.Email,
		LoginName:    input.LoginName,
		DisplayName:  input.DisplayName,
		State:        input.State,
		RoleKeys:     stringSliceValue(input.RoleKeys),
	}
}

func memberCapabilitiesDocumentFromGenerated(input iamcore.IAMMemberCapabilitiesDocument) MemberCapabilitiesDocument {
	return MemberCapabilitiesDocument{
		OrgID:       input.OrgId,
		Version:     input.Version,
		EnabledKeys: stringSliceValue(input.EnabledKeys),
		UpdatedAt:   input.UpdatedAt,
		UpdatedBy:   input.UpdatedBy,
	}
}

func memberCapabilitiesFromGenerated(input iamcore.IAMMemberCapabilities) MemberCapabilities {
	out := MemberCapabilities{
		Document: memberCapabilitiesDocumentFromGenerated(input.Document),
	}
	if input.Catalog != nil {
		out.Catalog = make([]MemberCapability, 0, len(*input.Catalog))
		for _, capability := range *input.Catalog {
			out.Catalog = append(out.Catalog, MemberCapability{
				Key:            capability.Key,
				Label:          capability.Label,
				Description:    capability.Description,
				DefaultEnabled: capability.DefaultEnabled,
				Permissions:    stringSliceValue(capability.Permissions),
			})
		}
	}
	return out
}

func apiCredentialsFromGenerated(input iamcore.IAMAPICredentials) []APICredential {
	if input.Credentials == nil {
		return nil
	}
	out := make([]APICredential, 0, len(*input.Credentials))
	for _, credential := range *input.Credentials {
		out = append(out, apiCredentialFromGenerated(credential))
	}
	return out
}

func serviceAccountsFromGenerated(input iamcore.IAMServiceAccounts) []ServiceAccount {
	if input.ServiceAccounts == nil {
		return nil
	}
	out := make([]ServiceAccount, 0, len(*input.ServiceAccounts))
	for _, account := range *input.ServiceAccounts {
		out = append(out, serviceAccountFromGenerated(account))
	}
	return out
}

func serviceAccountFromGenerated(input iamcore.IAMServiceAccount) ServiceAccount {
	return ServiceAccount{
		ServiceAccountID: input.ServiceAccountId,
		ResourceName:     input.ResourceName,
		OrgID:            input.OrgId,
		SubjectID:        input.SubjectId,
		ClientID:         input.ClientId,
		DisplayName:      input.DisplayName,
		Description:      stringValue(input.Description),
		Status:           input.Status,
		Permissions:      stringSliceValue(input.Permissions),
		CreatedAt:        input.CreatedAt,
		CreatedBy:        input.CreatedBy,
		UpdatedAt:        input.UpdatedAt,
		DisabledAt:       input.DisabledAt,
		DisabledBy:       stringValue(input.DisabledBy),
		LastUsedAt:       input.LastUsedAt,
	}
}

func apiCredentialFromGenerated(input iamcore.IAMAPICredential) APICredential {
	return APICredential{
		CredentialID:         input.CredentialId,
		ResourceName:         input.ResourceName,
		OrgID:                input.OrgId,
		SubjectID:            input.SubjectId,
		ClientID:             input.ClientId,
		DisplayName:          input.DisplayName,
		Status:               input.Status,
		AuthMethod:           input.AuthMethod,
		Fingerprint:          input.Fingerprint,
		Permissions:          stringSliceValue(input.Permissions),
		PolicyVersionAtIssue: input.PolicyVersionAtIssue,
		CreatedAt:            input.CreatedAt,
		CreatedBy:            input.CreatedBy,
		UpdatedAt:            input.UpdatedAt,
		ExpiresAt:            input.ExpiresAt,
		RevokedAt:            input.RevokedAt,
		RevokedBy:            stringValue(input.RevokedBy),
		LastUsedAt:           input.LastUsedAt,
	}
}

func createAPICredentialResultFromGenerated(input iamcore.IAMCreateAPICredentialResponse) CreateAPICredentialResult {
	return CreateAPICredentialResult{
		Credential:     apiCredentialFromGenerated(input.Credential),
		IssuedMaterial: issuedMaterialFromGenerated(input.IssuedMaterial),
	}
}

func rollAPICredentialResultFromGenerated(input iamcore.IAMRollAPICredentialResponse) RollAPICredentialResult {
	return RollAPICredentialResult{
		Credential:     apiCredentialFromGenerated(input.Credential),
		IssuedMaterial: issuedMaterialFromGenerated(input.IssuedMaterial),
	}
}

func issuedMaterialFromGenerated(input iamcore.IAMAPICredentialIssuedMaterial) APICredentialIssuedMaterial {
	return APICredentialIssuedMaterial{
		AuthMethod:   input.AuthMethod,
		ClientID:     input.ClientId,
		TokenURL:     input.TokenUrl,
		KeyID:        stringValue(input.KeyId),
		KeyContent:   stringValue(input.KeyContent),
		ClientSecret: stringValue(input.ClientSecret),
		Fingerprint:  input.Fingerprint,
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

func stringSliceValue(input *[]string) []string {
	if input == nil {
		return nil
	}
	out := make([]string, len(*input))
	copy(out, *input)
	return out
}

func compactStringSlice(input []string) []string {
	out := make([]string, 0, len(input))
	seen := map[string]struct{}{}
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func stringValue(input *string) string {
	if input == nil {
		return ""
	}
	return strings.TrimSpace(*input)
}
