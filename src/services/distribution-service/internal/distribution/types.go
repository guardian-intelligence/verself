package distribution

import "time"

const ServiceName = "distribution-service"

const (
	StateSubmitted   = "submitted"
	StateVerifying   = "verifying"
	StateAdmitted    = "admitted"
	StateAvailable   = "available"
	StateRejected    = "rejected"
	StateSuperseded  = "superseded"
	StateQuarantined = "quarantined"

	TargetStatePublished  = "published"
	TargetStateSuperseded = "superseded"

	DecisionPending = "pending"
	DecisionAllowed = "allowed"
	DecisionDenied  = "denied"

	ReplicationPending   = "pending"
	ReplicationAvailable = "available"

	RetentionCanary           = "canary"
	RetentionNightly          = "nightly"
	RetentionRelease          = "release"
	RetentionStable           = "stable"
	RetentionSecurityRetained = "security_retained"

	EvidenceCosign = "cosign_signature"
	EvidenceSLSA   = "slsa_provenance"
	EvidenceSBOM   = "sbom"
	EvidenceTest   = "test"

	PredicateSLSAProvenance = "https://slsa.dev/provenance/v1"
)

type Principal struct {
	Actor string
}

type Evidence struct {
	EvidenceKind      string
	PredicateType     string
	SubjectDigest     string
	DocumentDigest    string
	OCIReferrerDigest string
	CreatedAt         time.Time
}

type Verification struct {
	Decision         string
	Reason           string
	BuilderID        string
	SignerIdentity   string
	SourceRepository string
	SourceCommit     string
	SourceRef        string
	BazelTarget      string
	Evidence         []Evidence
	CheckedAt        time.Time
}

type Replication struct {
	State             string
	OriginRegistryURL string
	PublicRegistryURL string
	CheckedAt         time.Time
}

type Artifact struct {
	ArtifactID         string
	PackageName        string
	PackageVersion     string
	ChannelName        string
	PlatformOS         string
	PlatformArch       string
	OriginRegistryURL  string
	PublicRegistryURL  string
	OCIRepository      string
	OCIDigest          string
	OCIMediaType       string
	OCISizeBytes       int64
	PublicOCIReference string
	BuilderID          string
	SignerIdentity     string
	SourceRepository   string
	SourceCommit       string
	SourceRef          string
	BazelTarget        string
	PolicyRef          string
	State              string
	Verification       Verification
	RetentionClass     string
	Replication        Replication
	SubmittedBy        string
	AdmittedAt         time.Time
	AvailableAt        time.Time
	QuarantinedAt      time.Time
	QuarantineReason   string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Target struct {
	TargetID            string
	PackageName         string
	ChannelName         string
	PlatformOS          string
	PlatformArch        string
	ArtifactID          string
	ArtifactDigest      string
	PackageVersion      string
	State               string
	PublicOCIReference  string
	DownloadURL         string
	TUFMetadataURL      string
	TUFTargetsVersion   int64
	TUFSnapshotVersion  int64
	TUFTimestampVersion int64
	PolicyRef           string
	PromotedBy          string
	Reason              string
	PublishedAt         time.Time
	SupersededAt        time.Time
	SupersededByDigest  string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type AdmitArtifactRequest struct {
	PackageName       string
	PackageVersion    string
	ChannelName       string
	PlatformOS        string
	PlatformArch      string
	OriginRegistryURL string
	PublicRegistryURL string
	OCIRepository     string
	OCIDigest         string
	OCIMediaType      string
	OCISizeBytes      int64
	BuilderID         string
	SignerIdentity    string
	SourceRepository  string
	SourceCommit      string
	SourceRef         string
	BazelTarget       string
	PolicyRef         string
	Evidence          []Evidence
	SubmittedBy       string
	IdempotencyKey    string
}

type PromoteTargetRequest struct {
	PackageName    string
	ChannelName    string
	ArtifactDigest string
	PlatformOS     string
	PlatformArch   string
	PolicyRef      string
	PromotedBy     string
	Reason         string
	IdempotencyKey string
}

type QuarantineArtifactRequest struct {
	ArtifactDigestRef string
	ActorID           string
	Reason            string
	IdempotencyKey    string
}

type EnsureReplicationRequest struct {
	ArtifactDigestRef string
	ActorID           string
	Reason            string
	IdempotencyKey    string
}

type ResolveTargetRequest struct {
	PackageName  string
	ChannelName  string
	PlatformOS   string
	PlatformArch string
}

type CheckUpdateRequest struct {
	PackageName      string
	ChannelName      string
	PlatformOS       string
	PlatformArch     string
	InstalledDigest  string
	InstalledVersion string
}

type UpgradeVerificationRequest struct {
	PackageName      string
	ChannelName      string
	PlatformOS       string
	PlatformArch     string
	ArtifactDigest   string
	LayerDigest      string
	InstalledVersion string
}

type TUFMetadata struct {
	PackageName string
	Role        string
	Version     int64
	Body        []byte
	BodySHA256  string
	BodyLength  int64
	SignedAt    time.Time
	ExpiresAt   time.Time
}
