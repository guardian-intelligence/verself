package release

import (
	"time"

	"github.com/google/uuid"
)

const ServiceName = "release-service"

const (
	PackageKindCLI       = "cli"
	VersionSchemeSemver  = "semver"
	VisibilityInternal   = "internal"
	VersionKindStable    = "stable"
	VersionKindRC        = "rc"
	VersionKindNightly   = "nightly"
	StateDraft           = "draft"
	StateSubmitted       = "submitted"
	StateVerified        = "verified"
	StateApproved        = "approved"
	StatePublishing      = "publishing"
	StatePublished       = "published"
	StateDenied          = "denied"
	StateRetryableFailed = "retryable_failed"
	StateComplete        = "complete"
	LifecycleActive      = "active"
	ArtifactRoleArchive  = "archive"
	EvidenceCosign       = "cosign_signature"
	EvidenceSLSA         = "slsa_provenance"
	EvidenceSBOM         = "sbom"
	ProviderOCI          = "oci"
	ProviderHomebrew     = "homebrew"
)

type Principal struct {
	Actor string
}

type Package struct {
	PackageID            uuid.UUID
	OrgID                string
	PackageName          string
	PackageKind          string
	RepoPath             string
	BazelTarget          string
	OwnerTeam            string
	DefaultVersionScheme string
	Visibility           string
	PolicyRef            string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Version struct {
	ReleaseVersionID uuid.UUID
	PackageID        uuid.UUID
	CanonicalVersion string
	VersionScheme    string
	VersionKind      string
	ReleaseSequence  int64
	SourceRepository string
	SourceCommit     string
	SourceRef        string
	State            string
	LifecycleStatus  string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ReleaseContext struct {
	Package Package
	Version Version
}

type Artifact struct {
	ArtifactID       uuid.UUID
	ReleaseVersionID uuid.UUID
	ArtifactRole     string
	PlatformOS       string
	PlatformArch     string
	OCIRepository    string
	OCIDigest        string
	OCIMediaType     string
	OCISizeBytes     int64
	RegistryURL      string
	CreatedAt        time.Time
}

type Evidence struct {
	EvidenceID        uuid.UUID
	ReleaseVersionID  uuid.UUID
	ArtifactID        uuid.UUID
	EvidenceKind      string
	PredicateType     string
	SubjectDigest     string
	DocumentDigest    string
	OCIReferrerDigest string
	VerificationState string
	VerifiedAt        time.Time
	CreatedAt         time.Time
}

type InstallOption struct {
	InstallOptionID      uuid.UUID
	ReleaseVersionID     uuid.UUID
	OptionKind           string
	Status               string
	DisplayOrder         int32
	CommandTemplate      string
	VerificationTemplate string
	OCIRepository        string
	OCIDigest            string
	ProviderKind         string
	ProviderRef          string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Publication struct {
	PublicationID    uuid.UUID
	ReleaseVersionID uuid.UUID
	ProviderKind     string
	ProviderPackage  string
	ProviderChannel  string
	RequestedBy      string
	State            string
	IdempotencyKey   string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Channel struct {
	ChannelID               uuid.UUID
	PackageID               uuid.UUID
	ChannelName             string
	ChannelKind             string
	Visibility              string
	CurrentReleaseVersionID uuid.UUID
	PolicyRef               string
	UpdatedBy               string
	UpdatedAt               time.Time
	CreatedAt               time.Time
}

type RegisterPackageRequest struct {
	OrgID                string
	PackageName          string
	PackageKind          string
	RepoPath             string
	BazelTarget          string
	OwnerTeam            string
	DefaultVersionScheme string
	Visibility           string
	PolicyRef            string
}

type CreateVersionRequest struct {
	PackageID        uuid.UUID
	CanonicalVersion string
	VersionScheme    string
	VersionKind      string
	ReleaseSequence  int64
	SourceRepository string
	SourceCommit     string
	SourceRef        string
}

type AttachArtifactRequest struct {
	ReleaseVersionID uuid.UUID
	ArtifactRole     string
	PlatformOS       string
	PlatformArch     string
	OCIRepository    string
	OCIDigest        string
	OCIMediaType     string
	OCISizeBytes     int64
	RegistryURL      string
}

type AttachEvidenceRequest struct {
	ReleaseVersionID  uuid.UUID
	ArtifactID        uuid.UUID
	EvidenceKind      string
	PredicateType     string
	SubjectDigest     string
	DocumentDigest    string
	OCIReferrerDigest string
}

type VerifyRequest struct {
	ReleaseVersionID uuid.UUID
	PolicyRef        string
	PolicyDigest     string
	BuilderID        string
	SignerIdentity   string
	CheckedBy        string
}

type VerifyResult struct {
	Version  Version
	Decision string
	Reason   string
}

type ApproveRequest struct {
	ReleaseVersionID uuid.UUID
	ActorID          string
	Reason           string
}

type PromoteChannelRequest struct {
	PackageID        uuid.UUID
	ChannelName      string
	ReleaseVersionID uuid.UUID
	ChannelKind      string
	Visibility       string
	PolicyRef        string
	ActorID          string
	Reason           string
}

type CreatePublicationRequest struct {
	ReleaseVersionID uuid.UUID
	ProviderKind     string
	ProviderPackage  string
	ProviderChannel  string
	RequestedBy      string
	IdempotencyKey   string
}

type RecordReceiptRequest struct {
	PublicationID       uuid.UUID
	ProviderResourceURL string
	ProviderVersion     string
	ProviderDigest      string
	ReceiptDigest       string
}
