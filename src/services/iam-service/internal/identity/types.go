package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	runtimeiam "github.com/verself/service-runtime/iam"
)

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// MemberType separates a flesh-and-blood Zitadel user from a Zitadel machine
// user (service account). The members table renders only humans; machine users
// are surfaced via the API Credentials path even when they hold project
// authorizations directly.
type MemberType string

const (
	MemberTypeHuman   MemberType = "human"
	MemberTypeMachine MemberType = "machine"
)

type Principal struct {
	Subject     string
	SubjectKind AuthorizationSubjectKind
	OrgID       string
	Roles       []string
	Email       string
}

type AuthorizationSubjectKind string

const (
	AuthorizationSubjectKindUser           AuthorizationSubjectKind = "user"
	AuthorizationSubjectKindServiceAccount AuthorizationSubjectKind = "service_account"
	AuthorizationSubjectKindWorkload       AuthorizationSubjectKind = "workload"
)

type AuthorizationSubject struct {
	Kind AuthorizationSubjectKind
	ID   string
}

type OrganizationProfileState string

const (
	OrganizationProfileStateActive OrganizationProfileState = "active"
)

type (
	APICredentialAuthMethod string
	APICredentialStatus     string
	ServiceAccountStatus    string
)

type OrganizationProfile struct {
	OrgID          string
	DisplayName    string
	Slug           string
	State          OrganizationProfileState
	Version        int32
	CreatedBy      string
	UpdatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	RedirectedFrom string
}

type Organization struct {
	OrgID              string
	DisplayName        string
	Slug               string
	Version            int32
	OrgACLVersion      int32
	Caller             Member
	MemberCapabilities MemberCapabilitiesDocument
	Permissions        []string
}

type OrganizationMetadata struct {
	OrgID       string
	DisplayName string
	Slug        string
}

type UpdateOrganizationRequest struct {
	Version     int32
	DisplayName string
	Slug        string
}

type ResolveOrganizationRequest struct {
	OrgID         string
	Slug          string
	RequireActive bool
}

type Member struct {
	UserID      string
	Type        MemberType
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

type HumanProfileUpdate struct {
	GivenName   string
	FamilyName  string
	DisplayName *string
}

type HumanProfile struct {
	SubjectID   string
	Email       string
	GivenName   string
	FamilyName  string
	DisplayName string
	SyncedAt    time.Time
}

type InviteMemberResult struct {
	UserID   string
	Email    string
	RoleKeys []string
	Status   string
}

type MemberCapabilitiesDocument struct {
	OrgID       string
	Version     int32
	EnabledKeys []string
	UpdatedAt   time.Time
	UpdatedBy   string
}

type OrgACLState struct {
	OrgID     string
	Version   int32
	UpdatedAt time.Time
	UpdatedBy string
}

type UpdateMemberRolesCommand struct {
	OrgID                 string
	ActorID               string
	UserID                string
	RoleKeys              []string
	ExpectedRoleKeys      []string
	ExpectedOrgACLVersion int32
	OperationID           string
	IdempotencyKey        string
}

type UpdateMemberRolesResult struct {
	Member      Member
	OrgACLState OrgACLState
}

type Operations struct {
	Services []ServiceOperations
}

type ServiceOperations struct {
	Service    string
	Operations []Operation
}

type Operation struct {
	OperationID    string
	Permission     runtimeiam.Permission
	Resource       runtimeiam.ResourceKind
	Action         runtimeiam.Action
	OrgScope       runtimeiam.OrgScope
	MemberEligible bool
}

const (
	APICredentialAuthMethodPrivateKeyJWT APICredentialAuthMethod = "private_key_jwt"
	APICredentialAuthMethodClientSecret  APICredentialAuthMethod = "client_secret"

	APICredentialStatusActive  APICredentialStatus = "active"
	APICredentialStatusRevoked APICredentialStatus = "revoked"

	ServiceAccountStatusActive   ServiceAccountStatus = "active"
	ServiceAccountStatusDisabled ServiceAccountStatus = "disabled"
)

type ServiceAccount struct {
	ServiceAccountID string
	OrgID            string
	SubjectID        string
	ClientID         string
	DisplayName      string
	Description      string
	Status           ServiceAccountStatus
	Permissions      []string
	CreatedAt        time.Time
	CreatedBy        string
	UpdatedAt        time.Time
	DisabledAt       *time.Time
	DisabledBy       string
	LastUsedAt       *time.Time
}

type APICredential struct {
	CredentialID         string
	ServiceAccountID     string
	OrgID                string
	SubjectID            string
	ClientID             string
	DisplayName          string
	Status               APICredentialStatus
	AuthMethod           APICredentialAuthMethod
	Fingerprint          string
	Permissions          []string
	PolicyVersionAtIssue int32
	CreatedAt            time.Time
	CreatedBy            string
	UpdatedAt            time.Time
	ExpiresAt            *time.Time
	RevokedAt            *time.Time
	RevokedBy            string
	LastUsedAt           *time.Time
}

type APICredentialSecret struct {
	SecretID      string
	CredentialID  string
	AuthMethod    APICredentialAuthMethod
	ProviderKeyID string
	Fingerprint   string
	SecretHash    []byte
	HashAlgorithm string
	CreatedAt     time.Time
	CreatedBy     string
	ExpiresAt     *time.Time
	RevokedAt     *time.Time
	RevokedBy     string
}

type APICredentialIssuedMaterial struct {
	AuthMethod   APICredentialAuthMethod
	ClientID     string
	TokenURL     string
	KeyID        string
	KeyContent   string
	ClientSecret string
	Fingerprint  string
}

type ServiceAccountCredentialInput struct {
	CredentialID string
	ClientID     string
	DisplayName  string
	AuthMethod   APICredentialAuthMethod
	ExpiresAt    *time.Time
}

type AddServiceAccountCredentialInput struct {
	SubjectID  string
	ClientID   string
	AuthMethod APICredentialAuthMethod
	ExpiresAt  *time.Time
}

type CreateServiceAccountRequest struct {
	DisplayName string
	Description string
	Permissions []string
}

type DisableServiceAccountResult struct {
	ServiceAccount ServiceAccount
	Credentials    []APICredential
}

type CreateAPICredentialRequest struct {
	ServiceAccountID string
	DisplayName      string
	Description      string
	AuthMethod       APICredentialAuthMethod
	Permissions      []string
	ExpiresAt        *time.Time
}

type CreateAPICredentialResult struct {
	Credential     APICredential
	IssuedMaterial APICredentialIssuedMaterial
}

type RollAPICredentialRequest struct {
	AuthMethod APICredentialAuthMethod
}

type RollAPICredentialResult struct {
	Credential     APICredential
	IssuedMaterial APICredentialIssuedMaterial
}

type ResolveAPICredentialClaimsResult struct {
	CredentialID       string
	ServiceAccountID   string
	OrgID              string
	DisplayName        string
	ServiceAccountName string
	AuthMethod         APICredentialAuthMethod
	Fingerprint        string
	OwnerID            string
	OwnerDisplay       string
	Permissions        []string
	OpenBaoRoles       []string
}

func SecretHash(secret string) (fingerprint string, raw []byte) {
	sum := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(sum[:]), sum[:]
}
