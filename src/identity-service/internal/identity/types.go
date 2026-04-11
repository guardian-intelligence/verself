package identity

import "time"

const (
	RoleForgeOrgOwner = "forge_org_owner"
	RoleOrgAdmin      = "identity_org_admin"
	RoleOrgMember     = "identity_org_member"
)

type Principal struct {
	Subject string
	OrgID   string
	Roles   []string
	Email   string
}

type Organization struct {
	OrgID       string
	Name        string
	Caller      Member
	Policy      PolicyDocument
	Permissions []string
}

type Member struct {
	UserID      string
	Email       string
	LoginName   string
	DisplayName string
	State       string
	RoleKeys    []string
}

type InviteMemberRequest struct {
	Email      string
	GivenName  string
	FamilyName string
	RoleKeys   []string
}

type InviteMemberResult struct {
	UserID   string
	Email    string
	RoleKeys []string
	Status   string
}

type PolicyDocument struct {
	OrgID     string
	Version   int32
	Roles     []PolicyRole
	UpdatedAt time.Time
	UpdatedBy string
}

type PolicyRole struct {
	RoleKey     string
	DisplayName string
	Permissions []string
}

type Operations struct {
	Services []ServiceOperations
}

type ServiceOperations struct {
	Service    string
	Operations []Operation
}

type Operation struct {
	OperationID string
	Permission  string
	Resource    string
	Action      string
	OrgScope    string
}
