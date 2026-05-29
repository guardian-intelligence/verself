package identity

import (
	"crypto/hmac"
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

type SignupIntentState string

const (
	SignupIntentStatePendingVerification SignupIntentState = "pending_verification"
	SignupIntentStateMaterializing       SignupIntentState = "materializing"
	SignupIntentStateCompleted           SignupIntentState = "completed"
	SignupIntentStateExpired             SignupIntentState = "expired"
	SignupIntentStateFailedRetryable     SignupIntentState = "failed_retryable"
	SignupIntentStateFailedTerminal      SignupIntentState = "failed_terminal"
)

type SignupIntent struct {
	SignupIntentID              string
	IdempotencyKey              string
	RequestHash                 []byte
	EmailDelivery               string
	EmailIdentityKey            string
	EmailIdentityHash           []byte
	EmailIdentityHashKeyID      string
	OrganizationDisplayName     string
	RequestedOrganizationSlug   string
	OrganizationSlug            string
	GivenName                   string
	FamilyName                  string
	VerificationTokenHash       []byte
	State                       SignupIntentState
	MaterializationStep         string
	MaterializationAttempts     int32
	MaterializationLastError    string
	MaterializationLeaseExpires *time.Time
	VerifyIdempotencyKey        string
	VerifyRequestHash           []byte
	OrgID                       string
	AccountID                   string
	IdentityProviderOrgID       string
	IdentityProviderUserID      string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
	VerificationExpiresAt       time.Time
	VerifiedAt                  *time.Time
	CompletedAt                 *time.Time
}

type SignupStartOutcome string

const (
	SignupStartOutcomeCreated                  SignupStartOutcome = "created"
	SignupStartOutcomeVerificationResent       SignupStartOutcome = "verification_resent"
	SignupStartOutcomeVerificationRecentlySent SignupStartOutcome = "verification_recently_sent"
	SignupStartOutcomeAccountExistsNotice      SignupStartOutcome = "account_exists_notice"
	SignupStartOutcomeInFlightSuppressed       SignupStartOutcome = "inflight_suppressed"
)

type AccountState string

const (
	AccountStateProvisioning AccountState = "provisioning"
	AccountStateActive       AccountState = "active"
	AccountStateDisabled     AccountState = "disabled"
)

type SignupAccountEmail struct {
	AccountID        string
	EmailDelivery    string
	EmailIdentityKey string
	AccountState     AccountState
}

type SignupStartDecision struct {
	Intent       SignupIntent
	AccountEmail SignupAccountEmail
	Outcome      SignupStartOutcome
}

type StartSignupRequest struct {
	Email            string
	OrganizationName string
	OrganizationSlug string
	GivenName        string
	FamilyName       string
	IdempotencyKey   string
}

type StartSignupResult struct {
	Intent            SignupIntent
	VerificationToken string
	AccountNotice     SignupAccountNotice
	Outcome           SignupStartOutcome
	ResponseExpiresAt time.Time
}

func (r StartSignupResult) SendVerificationEmail() bool {
	return r.Outcome == SignupStartOutcomeCreated || r.Outcome == SignupStartOutcomeVerificationResent
}

func (r StartSignupResult) SendAccountExistsEmail() bool {
	return r.Outcome == SignupStartOutcomeAccountExistsNotice
}

func (r StartSignupResult) CreatedIntent() bool {
	return r.Outcome == SignupStartOutcomeCreated
}

type SignupAccountNotice struct {
	EmailDelivery  string
	IdempotencyKey string
}

type VerifySignupRequest struct {
	SignupIntentID          string
	VerificationToken       string
	InitialPassword         string
	OrganizationDisplayName string
	OrganizationSlug        string
	IdempotencyKey          string
}

type VerifySignupResult struct {
	Intent       SignupIntent
	Organization Organization
	Warnings     []AuthWarning
}

type AuthWarning struct {
	Code    string
	Message string
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

type DirectoryCreateSignupUserRequest struct {
	OrgID      string
	Email      string
	GivenName  string
	FamilyName string
	Password   string
}

type DirectoryCreateSignupUserResult struct {
	UserID string
}

// DirectoryCreateHumanWithIDPLinkRequest creates a Zitadel human in an
// organization with a pre-verified email and an external identity-provider link
// already attached, for just-in-time provisioning from a completed IdP intent. No
// password is set; the IdP is the credential.
type DirectoryCreateHumanWithIDPLinkRequest struct {
	OrgID            string
	Email            string
	GivenName        string
	FamilyName       string
	IDPID            string
	ExternalUserID   string
	ExternalUserName string
}

// GithubSignupRequest provisions a brand-new account from a verified GitHub
// identity: a Zitadel organization, an IdP-linked human, and a starter Verself
// organization (owner + billing). The account graph itself is materialized by the
// OIDC relying-party callback once the session subject is known.
type GithubSignupRequest struct {
	Email            string
	DisplayName      string
	IDPID            string
	ExternalUserID   string
	ExternalUserName string
}

type GithubSignupResult struct {
	UserID string
	OrgID  string
}

type OrganizationOwnerPolicyRequest struct {
	OrgID       string
	OwnerUserID string
	OperationID string
}

type BillingOrganizationProvisioningRequest struct {
	OrgID       string
	DisplayName string
	TrustTier   string
}

type SignupAccountEmailReservation struct {
	AccountID        string
	EmailDelivery    string
	EmailIdentityKey string
	DisplayName      string
}

type SignupAccountCompletion struct {
	AccountID        string
	Issuer           string
	Subject          string
	EmailDelivery    string
	EmailIdentityKey string
	DisplayName      string
	ConnectionID     string
}

type IAMEvent struct {
	EventID                string
	EventType              string
	EventVersion           uint16
	AggregateType          string
	AggregateID            string
	SignupIntentID         string
	OrgID                  string
	IdentityProviderOrgID  string
	IdentityProviderUserID string
	Step                   string
	State                  SignupIntentState
	Outcome                string
	Retryable              bool
	Attempt                uint32
	ErrorKind              string
	ErrorMessage           string
	OccurredAt             time.Time
	Payload                map[string]any
	IdempotencyKeyHash     string
	CorrelationID          string
	TraceID                string
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
	Password   string
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
}

type CompleteMemberInviteRequest struct {
	AcceptanceToken string
}

type DirectoryCompleteMemberInviteRequest struct {
	UserID                string
	EmailVerificationCode string
}

type MemberInviteAcceptance struct {
	TokenHash             string
	OrgID                 string
	UserID                string
	Email                 string
	EmailVerificationCode string
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

type LoginSessionInput struct {
	LoginName string
	Password  string
	UserAgent string
	IP        string
	Lifetime  time.Duration
}

type LoginSession struct {
	SessionID    string
	SessionToken string
	ExpiresAt    time.Time
}

type PasswordResetUser struct {
	UserID    string
	Email     string
	LoginName string
}

type OIDCAuthRequest struct {
	ID          string
	ClientID    string
	Scopes      []string
	RedirectURI string
	LoginHint   string
}

// IDPIntentStart is the result of starting an external identity provider intent
// against Zitadel's session API. AuthURL is the provider authorization URL the
// browser must be redirected to.
type IDPIntentStart struct {
	AuthURL string
}

// IDPIntentResult is the external identity Zitadel resolved for a completed IdP
// intent. UserID is the linked Zitadel user when the external identity is
// already (or auto-) linked; it is empty when no Verself/Zitadel user is bound
// yet. ExternalSubject/Username/Email are provider truth used for telemetry and
// later explicit-link decisions.
type IDPIntentResult struct {
	IDPID           string
	UserID          string
	ExternalSubject string
	Username        string
	Email           string
	EmailVerified   bool
}

// AddIDPLinkInput binds an external identity provider identity to an existing
// Zitadel human. UserID is the Zitadel user; ExternalUserID/ExternalUserName are
// provider truth from a completed IdP intent. Used to auto-link a verified GitHub
// identity to an existing account by verified email over the headless Session API
// (which, unlike the hosted login UI, never auto-links).
type AddIDPLinkInput struct {
	UserID           string
	IDPID            string
	ExternalUserID   string
	ExternalUserName string
}

type StartDeviceAuthorizationInput struct {
	ClientID string
	Scopes   []string
}

type DeviceAuthorization struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               time.Duration
	Interval                time.Duration
	Request                 DeviceAuthorizationRequest
}

type DeviceAuthorizationRequest struct {
	ID          string
	ClientID    string
	Scopes      []string
	AppName     string
	ProjectName string
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

func HMACSHA256(key []byte, value string) (fingerprint string, raw []byte) {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	sum := mac.Sum(nil)
	return "hmac-sha256:" + hex.EncodeToString(sum), sum
}
