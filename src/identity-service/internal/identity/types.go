package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type Principal struct {
	Subject           string
	OrgID             string
	Roles             []string
	DirectPermissions []string
	Email             string
}

type APICredentialAuthMethod string
type APICredentialStatus string

type Organization struct {
	OrgID              string
	Name               string
	Caller             Member
	MemberCapabilities MemberCapabilitiesDocument
	Permissions        []string
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

type MemberCapabilitiesDocument struct {
	OrgID       string
	Version     int32
	EnabledKeys []string
	UpdatedAt   time.Time
	UpdatedBy   string
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
	Permission     string
	Resource       string
	Action         string
	OrgScope       string
	MemberEligible bool
}

const (
	APICredentialAuthMethodPrivateKeyJWT APICredentialAuthMethod = "private_key_jwt"
	APICredentialAuthMethodClientSecret  APICredentialAuthMethod = "client_secret"

	APICredentialStatusActive  APICredentialStatus = "active"
	APICredentialStatusRevoked APICredentialStatus = "revoked"
)

type APICredential struct {
	CredentialID         string
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

type CreateAPICredentialRequest struct {
	DisplayName string
	AuthMethod  APICredentialAuthMethod
	Permissions []string
	ExpiresAt   *time.Time
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
	CredentialID string
	OrgID        string
	Permissions  []string
}

func SecretHash(secret string) (fingerprint string, raw []byte) {
	sum := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(sum[:]), sum[:]
}
