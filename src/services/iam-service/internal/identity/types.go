package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	runtimeiam "github.com/verself/service-runtime/iam"
)

// MemberType separates human directory users from Zitadel machine users.
type MemberType string

const (
	MemberTypeHuman   MemberType = "human"
	MemberTypeMachine MemberType = "machine"
)

type Principal struct {
	Subject     string
	SubjectKind AuthorizationSubjectKind
	OrgID       string
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
	OrgID                 string
	IdentityProviderOrgID string
	DisplayName           string
	Slug                  string
	State                 OrganizationProfileState
	Version               int32
	CreatedBy             string
	UpdatedBy             string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	RedirectedFrom        string
}

type Organization struct {
	OrgID       string
	DisplayName string
	Slug        string
	Version     int32
}

type OrganizationMetadata struct {
	OrgID                 string
	IdentityProviderOrgID string
	DisplayName           string
	Slug                  string
	Version               int32
}

type CreateOrganizationRequest struct {
	OrgID                 string
	IdentityProviderOrgID string
	DisplayName           string
	Slug                  string
	ActorID               string
}

type PublicCreateOrganizationRequest struct {
	DisplayName    string
	Slug           string
	IdempotencyKey string
}

type DirectoryCreateOrganizationRequest struct {
	Name        string
	AdminUserID string
}

type DirectoryCreateOrganizationResult struct {
	OrganizationID string
}

type UpdateOrganizationRequest struct {
	Version     int32
	DisplayName string
	Slug        string
}

type ResolveOrganizationRequest struct {
	OrgID                 string
	IdentityProviderOrgID string
	Slug                  string
	RequireActive         bool
}

type Member struct {
	UserID      string
	Type        MemberType
	Email       string
	LoginName   string
	DisplayName string
	State       string
}

type InviteMemberRequest struct {
	Email      string
	GivenName  string
	FamilyName string
	Roles      []string
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
	UserID              string
	Email               string
	Status              string
	Roles               []string
	AcceptanceToken     string
	AcceptanceExpiresAt time.Time
}

type DirectoryInviteMemberResult struct {
	UserID                string
	Email                 string
	Status                string
	EmailVerificationCode string
	PasswordResetCode     string
}

type CompleteMemberInviteRequest struct {
	AcceptanceToken string
	Password        string
}

type DirectoryCompleteMemberInviteRequest struct {
	UserID                string
	PasswordResetCode     string
	EmailVerificationCode string
	Password              string
}

type MemberInviteAcceptance struct {
	TokenHash             string
	OrgID                 string
	UserID                string
	Email                 string
	EmailVerificationCode string
	PasswordResetCode     string
	CreatedAt             time.Time
	ExpiresAt             time.Time
	AcceptedAt            *time.Time
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
	Permission  runtimeiam.Permission
	Resource    runtimeiam.ResourceKind
	Action      runtimeiam.Action
	OrgScope    runtimeiam.OrgScope
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
	CreatedAt        time.Time
	CreatedBy        string
	UpdatedAt        time.Time
	DisabledAt       *time.Time
	DisabledBy       string
	LastUsedAt       *time.Time
}

type APICredential struct {
	CredentialID     string
	ServiceAccountID string
	OrgID            string
	SubjectID        string
	ClientID         string
	DisplayName      string
	Status           APICredentialStatus
	AuthMethod       APICredentialAuthMethod
	Fingerprint      string
	CreatedAt        time.Time
	CreatedBy        string
	UpdatedAt        time.Time
	ExpiresAt        *time.Time
	RevokedAt        *time.Time
	RevokedBy        string
	LastUsedAt       *time.Time
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
}

func SecretHash(secret string) (fingerprint string, raw []byte) {
	sum := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(sum[:]), sum[:]
}
