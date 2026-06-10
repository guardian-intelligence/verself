package distribution

import (
	"context"
	"time"

	"github.com/verself/distribution-service/internal/releaseattest"
)

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

	ChannelDeployment = "deployment"

	EvidenceCosign = "cosign_signature"
	EvidenceSLSA   = "slsa_provenance"
	EvidenceSBOM   = "sbom"
	EvidenceTest   = "test"

	PredicateSLSAProvenance = "https://slsa.dev/provenance/v1"

	PolicyDeploymentOCI = "distribution-policies/deployments/oci-digest-v1"
)

type ReleaseAttestationVerifier interface {
	Verify(context.Context, releaseattest.Request) (releaseattest.Result, error)
}

type DeploymentEvidenceVerifier interface {
	VerifyDeploymentEvidence(context.Context, AdmitArtifactRequest) ([]Evidence, error)
}

type Principal struct {
	Actor string
}

type Evidence struct {
	EvidenceKind      string
	PredicateType     string
	SubjectDigest     string
	DocumentDigest    string
	OCIReferrerDigest string
	// SigningKeyID is the derived hex(sha256(DER-SPKI)) identity of the ring
	// key that verified the evidence bundle. Deployment channel only; empty for
	// release evidence.
	SigningKeyID string
	CreatedAt    time.Time
}

type Verification struct {
	Decision  string
	Reason    string
	BuilderID string
	// SignerIdentity is the asserted, trusted-signer-checked workflow identity.
	// Release channels only; empty on the deployment channel.
	SignerIdentity string
	// SigningKeyID is the derived identity of the key that verified deployment
	// evidence. Deployment channel only.
	SigningKeyID     string
	SourceRepository string
	SourceCommit     string
	SourceRef        string
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
	Flavor             string
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
	TargetID           string
	PackageName        string
	ChannelName        string
	PlatformOS         string
	PlatformArch       string
	Flavor             string
	ArtifactID         string
	ArtifactDigest     string
	PackageVersion     string
	State              string
	PublicOCIReference string
	DownloadURL        string
	PolicyRef          string
	PromotedBy         string
	Reason             string
	PublishedAt        time.Time
	SupersededAt       time.Time
	SupersededByDigest string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type AdmitArtifactRequest struct {
	PackageName        string
	PackageVersion     string
	ChannelName        string
	PlatformOS         string
	PlatformArch       string
	Flavor             string
	OriginRegistryURL  string
	PublicRegistryURL  string
	OCIRepository      string
	OCIDigest          string
	OCIMediaType       string
	OCISizeBytes       int64
	BuilderID          string
	SignerIdentity     string
	SourceRepository   string
	SourceCommit       string
	SourceRef          string
	PolicyRef          string
	Evidence           []Evidence
	ReleaseAttestation ReleaseAttestation
	SubmittedBy        string
	IdempotencyKey     string
}

type ReleaseAttestation struct {
	DistributionChallenge      string
	ReleaseInputDigest         string
	ArtifactDigest             string
	ProvenanceDigest           string
	SBOMDigest                 string
	TPMReleasePublicName       string
	TPMReleasePublicBlobDigest string
	PolicyID                   string
	TPM                        releaseattest.TPMEvidence
}

type PromoteTargetRequest struct {
	PackageName    string
	PackageVersion string
	ChannelName    string
	ArtifactDigest string
	PlatformOS     string
	PlatformArch   string
	Flavor         string
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
	Flavor       string
}

type CheckUpdateRequest struct {
	PackageName      string
	ChannelName      string
	PlatformOS       string
	PlatformArch     string
	Flavor           string
	InstalledDigest  string
	InstalledVersion string
}

type UpgradeVerificationRequest struct {
	PackageName      string
	ChannelName      string
	PlatformOS       string
	PlatformArch     string
	Flavor           string
	ArtifactDigest   string
	LayerDigest      string
	InstalledVersion string
}
