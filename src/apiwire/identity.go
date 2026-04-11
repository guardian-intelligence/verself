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
