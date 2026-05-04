package dto

import "time"

type IAMOrganization struct {
	OrgID              OrgID                         `json:"org_id"`
	DisplayName        string                        `json:"display_name"`
	Slug               string                        `json:"slug"`
	Version            int32                         `json:"version" minimum:"0" maximum:"2147483647"`
	OrgACLVersion      int32                         `json:"org_acl_version" minimum:"0" maximum:"2147483647"`
	Caller             IAMMember                     `json:"caller"`
	MemberCapabilities IAMMemberCapabilitiesDocument `json:"member_capabilities"`
	Permissions        []string                      `json:"permissions"`
}

type IAMOrganizationMetadata struct {
	OrgID       OrgID  `json:"org_id"`
	DisplayName string `json:"display_name"`
	Slug        string `json:"slug"`
}

type IAMUpdateOrganizationRequest struct {
	Version     int32  `json:"version" required:"true" minimum:"0" maximum:"2147483647"`
	DisplayName string `json:"display_name,omitempty" maxLength:"120"`
	Slug        string `json:"slug,omitempty" maxLength:"80"`
}

type IAMOrganizationProfile struct {
	OrgID          OrgID     `json:"org_id"`
	DisplayName    string    `json:"display_name"`
	Slug           string    `json:"slug"`
	State          string    `json:"state"`
	Version        int32     `json:"version" minimum:"0" maximum:"2147483647"`
	UpdatedAt      time.Time `json:"updated_at"`
	RedirectedFrom string    `json:"redirected_from,omitempty"`
}

type IAMResolveOrganizationRequest struct {
	OrgID         OrgID  `json:"org_id,omitempty"`
	Slug          string `json:"slug,omitempty" maxLength:"80"`
	RequireActive bool   `json:"require_active"`
}

type IAMResolveOrganizationResponse struct {
	Organization IAMOrganizationProfile `json:"organization"`
}

type IAMMember struct {
	UserID      string   `json:"user_id"`
	Email       string   `json:"email"`
	LoginName   string   `json:"login_name"`
	DisplayName string   `json:"display_name"`
	State       string   `json:"state"`
	RoleKeys    []string `json:"role_keys"`
}

type IAMMembers struct {
	Members []IAMMember `json:"members"`
}

type IAMInviteMemberRequest struct {
	Email      string   `json:"email" required:"true" maxLength:"320"`
	GivenName  string   `json:"given_name,omitempty" maxLength:"100"`
	FamilyName string   `json:"family_name,omitempty" maxLength:"100"`
	RoleKeys   []string `json:"role_keys" required:"true" minItems:"1" maxItems:"16"`
}

type IAMInviteMemberResponse struct {
	UserID   string   `json:"user_id"`
	Email    string   `json:"email"`
	RoleKeys []string `json:"role_keys"`
	Status   string   `json:"status"`
}

type IAMUpdateMemberRolesRequest struct {
	RoleKeys              []string `json:"role_keys" required:"true" minItems:"1" maxItems:"16"`
	ExpectedRoleKeys      []string `json:"expected_role_keys" required:"true" minItems:"1" maxItems:"16"`
	ExpectedOrgACLVersion int32    `json:"expected_org_acl_version" required:"true" minimum:"0" maximum:"2147483647"`
}

type IAMMemberCapabilitiesDocument struct {
	OrgID       OrgID     `json:"org_id"`
	Version     int32     `json:"version" minimum:"0" maximum:"2147483647"`
	EnabledKeys []string  `json:"enabled_keys"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
}

// IAMMemberCapability is one row in the static, code-owned capability
// catalog. The catalog is read-only over the wire — admins toggle Document.EnabledKeys, never the catalog itself.
type IAMMemberCapability struct {
	Key            string   `json:"key"`
	Label          string   `json:"label"`
	Description    string   `json:"description"`
	DefaultEnabled bool     `json:"default_enabled"`
	Permissions    []string `json:"permissions"`
}

type IAMMemberCapabilities struct {
	Document IAMMemberCapabilitiesDocument `json:"document"`
	Catalog  []IAMMemberCapability         `json:"catalog"`
}

type IAMPutMemberCapabilitiesRequest struct {
	Version     int32    `json:"version" minimum:"0" maximum:"2147483647"`
	EnabledKeys []string `json:"enabled_keys" required:"true" minItems:"0" maxItems:"32"`
}

type IAMPolicy struct {
	Resource string             `json:"resource,omitempty" maxLength:"256"`
	Version  int32              `json:"version" minimum:"0" maximum:"2147483647"`
	Etag     string             `json:"etag,omitempty" maxLength:"128"`
	Bindings []IAMPolicyBinding `json:"bindings"`
	ZedToken string             `json:"zed_token,omitempty" maxLength:"1024"`
}

type IAMPolicyBinding struct {
	Role    string   `json:"role" required:"true" maxLength:"128"`
	Members []string `json:"members" required:"true" minItems:"1" maxItems:"1024"`
}

type IAMSetPolicyRequest struct {
	Policy IAMPolicy `json:"policy" required:"true"`
}

type IAMTestPermissionsRequest struct {
	Permissions []string `json:"permissions" required:"true" minItems:"1" maxItems:"256"`
}

type IAMTestPermissionsResponse struct {
	Permissions []string `json:"permissions"`
	ZedToken    string   `json:"zed_token,omitempty" maxLength:"1024"`
}

type IAMAPICredential struct {
	CredentialID         string     `json:"credential_id"`
	OrgID                OrgID      `json:"org_id"`
	SubjectID            string     `json:"subject_id"`
	ClientID             string     `json:"client_id"`
	DisplayName          string     `json:"display_name"`
	Status               string     `json:"status"`
	AuthMethod           string     `json:"auth_method"`
	Fingerprint          string     `json:"fingerprint"`
	Permissions          []string   `json:"permissions"`
	PolicyVersionAtIssue int32      `json:"policy_version_at_issue" minimum:"0" maximum:"2147483647"`
	CreatedAt            time.Time  `json:"created_at"`
	CreatedBy            string     `json:"created_by"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	RevokedBy            string     `json:"revoked_by,omitempty"`
	LastUsedAt           *time.Time `json:"last_used_at,omitempty"`
}

type IAMAPICredentials struct {
	Credentials []IAMAPICredential `json:"credentials"`
}

type IAMAPICredentialIssuedMaterial struct {
	AuthMethod   string `json:"auth_method"`
	ClientID     string `json:"client_id"`
	TokenURL     string `json:"token_url"`
	KeyID        string `json:"key_id,omitempty"`
	KeyContent   string `json:"key_content,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Fingerprint  string `json:"fingerprint"`
}

type IAMCreateAPICredentialRequest struct {
	DisplayName string     `json:"display_name" required:"true" maxLength:"200"`
	AuthMethod  string     `json:"auth_method,omitempty" enum:"private_key_jwt,client_secret"`
	Permissions []string   `json:"permissions" required:"true" minItems:"1" maxItems:"256"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type IAMCreateAPICredentialResponse struct {
	Credential     IAMAPICredential               `json:"credential"`
	IssuedMaterial IAMAPICredentialIssuedMaterial `json:"issued_material"`
}

type IAMRollAPICredentialRequest struct {
	AuthMethod string `json:"auth_method,omitempty" enum:"private_key_jwt,client_secret"`
}

type IAMRollAPICredentialResponse struct {
	Credential     IAMAPICredential               `json:"credential"`
	IssuedMaterial IAMAPICredentialIssuedMaterial `json:"issued_material"`
}

type IAMUpdateHumanProfileRequest struct {
	GivenName   string  `json:"given_name" required:"true" maxLength:"100"`
	FamilyName  string  `json:"family_name" required:"true" maxLength:"100"`
	DisplayName *string `json:"display_name,omitempty" maxLength:"200"`
}

type IAMUpdateHumanProfileResponse struct {
	SubjectID   string    `json:"subject_id"`
	Email       string    `json:"email"`
	GivenName   string    `json:"given_name"`
	FamilyName  string    `json:"family_name"`
	DisplayName string    `json:"display_name"`
	SyncedAt    time.Time `json:"synced_at"`
}
