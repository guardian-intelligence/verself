package apiwire

import "time"

type IdentityOrganization struct {
	OrgID       OrgID                  `json:"org_id"`
	Name        string                 `json:"name"`
	Caller      IdentityMember         `json:"caller"`
	Policy      IdentityPolicyDocument `json:"policy"`
	Permissions []string               `json:"permissions"`
}

type IdentityMember struct {
	UserID      string   `json:"user_id"`
	Email       string   `json:"email"`
	LoginName   string   `json:"login_name"`
	DisplayName string   `json:"display_name"`
	State       string   `json:"state"`
	RoleKeys    []string `json:"role_keys"`
}

type IdentityMembers struct {
	Members []IdentityMember `json:"members"`
}

type IdentityInviteMemberRequest struct {
	Email      string   `json:"email" required:"true" maxLength:"320"`
	GivenName  string   `json:"given_name,omitempty" maxLength:"100"`
	FamilyName string   `json:"family_name,omitempty" maxLength:"100"`
	RoleKeys   []string `json:"role_keys" required:"true" minItems:"1" maxItems:"16"`
}

type IdentityInviteMemberResponse struct {
	UserID   string   `json:"user_id"`
	Email    string   `json:"email"`
	RoleKeys []string `json:"role_keys"`
	Status   string   `json:"status"`
}

type IdentityUpdateMemberRolesRequest struct {
	RoleKeys []string `json:"role_keys" required:"true" minItems:"1" maxItems:"16"`
}

type IdentityPolicyDocument struct {
	OrgID     OrgID                `json:"org_id"`
	Version   int32                `json:"version" minimum:"0" maximum:"2147483647"`
	Roles     []IdentityPolicyRole `json:"roles"`
	UpdatedAt time.Time            `json:"updated_at"`
	UpdatedBy string               `json:"updated_by"`
}

type IdentityPutPolicyRequest struct {
	Version int32                `json:"version" minimum:"0" maximum:"2147483647"`
	Roles   []IdentityPolicyRole `json:"roles" required:"true" minItems:"1" maxItems:"32"`
}

type IdentityPolicyRole struct {
	RoleKey     string   `json:"role_key" required:"true" maxLength:"100"`
	DisplayName string   `json:"display_name" required:"true" maxLength:"200"`
	Permissions []string `json:"permissions" required:"true" minItems:"1" maxItems:"256"`
}

type IdentityOperations struct {
	Services []IdentityServiceOperations `json:"services"`
}

type IdentityServiceOperations struct {
	Service    string              `json:"service"`
	Operations []IdentityOperation `json:"operations"`
}

type IdentityOperation struct {
	OperationID string `json:"operation_id"`
	Permission  string `json:"permission"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	OrgScope    string `json:"org_scope"`
}

type IdentityAPICredential struct {
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

type IdentityAPICredentials struct {
	Credentials []IdentityAPICredential `json:"credentials"`
}

type IdentityAPICredentialIssuedMaterial struct {
	AuthMethod   string `json:"auth_method"`
	ClientID     string `json:"client_id"`
	TokenURL     string `json:"token_url"`
	KeyID        string `json:"key_id,omitempty"`
	KeyContent   string `json:"key_content,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Fingerprint  string `json:"fingerprint"`
}

type IdentityCreateAPICredentialRequest struct {
	DisplayName string     `json:"display_name" required:"true" maxLength:"200"`
	AuthMethod  string     `json:"auth_method,omitempty" enum:"private_key_jwt,client_secret"`
	Permissions []string   `json:"permissions" required:"true" minItems:"1" maxItems:"256"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type IdentityCreateAPICredentialResponse struct {
	Credential     IdentityAPICredential               `json:"credential"`
	IssuedMaterial IdentityAPICredentialIssuedMaterial `json:"issued_material"`
}

type IdentityRollAPICredentialRequest struct {
	AuthMethod string `json:"auth_method,omitempty" enum:"private_key_jwt,client_secret"`
}

type IdentityRollAPICredentialResponse struct {
	Credential     IdentityAPICredential               `json:"credential"`
	IssuedMaterial IdentityAPICredentialIssuedMaterial `json:"issued_material"`
}
