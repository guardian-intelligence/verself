package api

import (
	"time"

	"github.com/verself/distribution-service/internal/distribution"
)

type checkUpdateBody struct {
	PackageName      string `json:"package_name"`
	ChannelName      string `json:"channel_name"`
	PlatformOS       string `json:"platform_os"`
	PlatformArch     string `json:"platform_arch"`
	InstalledDigest  string `json:"installed_digest,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

type upgradeVerificationBody struct {
	PackageName      string `json:"package_name"`
	ChannelName      string `json:"channel_name"`
	PlatformOS       string `json:"platform_os"`
	PlatformArch     string `json:"platform_arch"`
	ArtifactDigest   string `json:"artifact_digest"`
	LayerDigest      string `json:"layer_digest"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

type admitArtifactBody struct {
	PackageName       string           `json:"package_name"`
	PackageVersion    string           `json:"package_version"`
	ChannelName       string           `json:"channel_name"`
	PlatformOS        string           `json:"platform_os"`
	PlatformArch      string           `json:"platform_arch"`
	OriginRegistryURL string           `json:"origin_registry_url"`
	PublicRegistryURL string           `json:"public_registry_url"`
	OCIRepository     string           `json:"oci_repository"`
	OCIDigest         string           `json:"oci_digest"`
	OCIMediaType      string           `json:"oci_media_type"`
	OCISizeBytes      int64            `json:"oci_size_bytes"`
	BuilderID         string           `json:"builder_id"`
	SignerIdentity    string           `json:"signer_identity"`
	SourceRepository  string           `json:"source_repository"`
	SourceCommit      string           `json:"source_commit"`
	SourceRef         string           `json:"source_ref"`
	BazelTarget       string           `json:"bazel_target"`
	PolicyRef         string           `json:"policy_ref"`
	Evidence          []evidenceRecord `json:"evidence"`
	SubmittedBy       string           `json:"submitted_by"`
}

type promoteTargetBody struct {
	ArtifactDigest string `json:"artifact_digest"`
	PlatformOS     string `json:"platform_os"`
	PlatformArch   string `json:"platform_arch"`
	PolicyRef      string `json:"policy_ref"`
	PromotedBy     string `json:"promoted_by"`
	Reason         string `json:"reason"`
}

type actorReasonBody struct {
	ActorID string `json:"actor_id"`
	Reason  string `json:"reason"`
}

type evidenceRecord struct {
	EvidenceKind      string `json:"evidence_kind"`
	PredicateType     string `json:"predicate_type"`
	SubjectDigest     string `json:"subject_digest"`
	DocumentDigest    string `json:"document_digest"`
	OCIReferrerDigest string `json:"oci_referrer_digest"`
}

type verificationRecord struct {
	Decision         string           `json:"decision"`
	Reason           string           `json:"reason"`
	BuilderID        string           `json:"builder_id"`
	SignerIdentity   string           `json:"signer_identity"`
	SourceRepository string           `json:"source_repository"`
	SourceCommit     string           `json:"source_commit"`
	SourceRef        string           `json:"source_ref"`
	BazelTarget      string           `json:"bazel_target"`
	Evidence         []evidenceRecord `json:"evidence"`
	CheckedAt        string           `json:"checked_at"`
}

type replicationRecord struct {
	State             string `json:"state"`
	OriginRegistryURL string `json:"origin_registry_url"`
	PublicRegistryURL string `json:"public_registry_url"`
	CheckedAt         string `json:"checked_at"`
}

type artifactRecord struct {
	ArtifactDigest     string             `json:"artifact_digest"`
	ArtifactDigestRef  string             `json:"artifact_digest_ref"`
	ResourceName       string             `json:"resourceName"`
	PackageName        string             `json:"package_name"`
	PackageVersion     string             `json:"package_version"`
	PlatformOS         string             `json:"platform_os"`
	PlatformArch       string             `json:"platform_arch"`
	OCIRepository      string             `json:"oci_repository"`
	PublicOCIReference string             `json:"public_oci_reference"`
	OCIMediaType       string             `json:"oci_media_type"`
	OCISizeBytes       int64              `json:"oci_size_bytes"`
	State              string             `json:"state"`
	RetentionClass     string             `json:"retention_class"`
	Verification       verificationRecord `json:"verification"`
	Replication        replicationRecord  `json:"replication"`
	CreatedAt          string             `json:"created_at"`
	UpdatedAt          string             `json:"updated_at"`
	QuarantinedAt      *string            `json:"quarantined_at,omitempty"`
	QuarantineReason   *string            `json:"quarantine_reason,omitempty"`
}

type targetRecord struct {
	ResourceName       string  `json:"resourceName"`
	PackageName        string  `json:"package_name"`
	ChannelName        string  `json:"channel_name"`
	PlatformOS         string  `json:"platform_os"`
	PlatformArch       string  `json:"platform_arch"`
	ArtifactDigest     string  `json:"artifact_digest"`
	ArtifactDigestRef  string  `json:"artifact_digest_ref"`
	PackageVersion     string  `json:"package_version"`
	State              string  `json:"state"`
	PublicOCIReference string  `json:"public_oci_reference"`
	DownloadURL        string  `json:"download_url"`
	PublishedAt        string  `json:"published_at"`
	SupersededAt       *string `json:"superseded_at,omitempty"`
	SupersededByDigest *string `json:"superseded_by_digest,omitempty"`
}

func evidenceFromDTO(input []evidenceRecord) []distribution.Evidence {
	out := make([]distribution.Evidence, 0, len(input))
	for _, item := range input {
		out = append(out, distribution.Evidence{
			EvidenceKind:      item.EvidenceKind,
			PredicateType:     item.PredicateType,
			SubjectDigest:     item.SubjectDigest,
			DocumentDigest:    item.DocumentDigest,
			OCIReferrerDigest: item.OCIReferrerDigest,
		})
	}
	return out
}

func artifactDTO(installationID string, artifact distribution.Artifact) artifactRecord {
	evidence := make([]evidenceRecord, 0, len(artifact.Verification.Evidence))
	for _, item := range artifact.Verification.Evidence {
		evidence = append(evidence, evidenceRecord{
			EvidenceKind:      item.EvidenceKind,
			PredicateType:     item.PredicateType,
			SubjectDigest:     item.SubjectDigest,
			DocumentDigest:    item.DocumentDigest,
			OCIReferrerDigest: item.OCIReferrerDigest,
		})
	}
	out := artifactRecord{
		ArtifactDigest:     artifact.OCIDigest,
		ArtifactDigestRef:  digestRef(artifact.OCIDigest),
		ResourceName:       artifactResourceName(installationID, artifact),
		PackageName:        artifact.PackageName,
		PackageVersion:     artifact.PackageVersion,
		PlatformOS:         artifact.PlatformOS,
		PlatformArch:       artifact.PlatformArch,
		OCIRepository:      artifact.OCIRepository,
		PublicOCIReference: artifact.PublicOCIReference,
		OCIMediaType:       artifact.OCIMediaType,
		OCISizeBytes:       artifact.OCISizeBytes,
		State:              artifact.State,
		RetentionClass:     artifact.RetentionClass,
		Verification: verificationRecord{
			Decision:         artifact.Verification.Decision,
			Reason:           artifact.Verification.Reason,
			BuilderID:        artifact.Verification.BuilderID,
			SignerIdentity:   artifact.Verification.SignerIdentity,
			SourceRepository: artifact.Verification.SourceRepository,
			SourceCommit:     artifact.Verification.SourceCommit,
			SourceRef:        artifact.Verification.SourceRef,
			BazelTarget:      artifact.Verification.BazelTarget,
			Evidence:         evidence,
			CheckedAt:        formatTime(artifact.Verification.CheckedAt),
		},
		Replication: replicationRecord{
			State:             artifact.Replication.State,
			OriginRegistryURL: artifact.Replication.OriginRegistryURL,
			PublicRegistryURL: artifact.Replication.PublicRegistryURL,
			CheckedAt:         formatTime(artifact.Replication.CheckedAt),
		},
		CreatedAt: formatTime(artifact.CreatedAt),
		UpdatedAt: formatTime(artifact.UpdatedAt),
	}
	if !artifact.QuarantinedAt.IsZero() {
		out.QuarantinedAt = stringPtr(formatTime(artifact.QuarantinedAt))
	}
	if artifact.QuarantineReason != "" {
		out.QuarantineReason = stringPtr(artifact.QuarantineReason)
	}
	return out
}

func targetDTO(installationID string, target distribution.Target) targetRecord {
	out := targetRecord{
		ResourceName:       targetResourceName(installationID, target),
		PackageName:        target.PackageName,
		ChannelName:        target.ChannelName,
		PlatformOS:         target.PlatformOS,
		PlatformArch:       target.PlatformArch,
		ArtifactDigest:     target.ArtifactDigest,
		ArtifactDigestRef:  digestRef(target.ArtifactDigest),
		PackageVersion:     target.PackageVersion,
		State:              target.State,
		PublicOCIReference: target.PublicOCIReference,
		DownloadURL:        target.DownloadURL,
		PublishedAt:        formatTime(target.PublishedAt),
	}
	if !target.SupersededAt.IsZero() {
		out.SupersededAt = stringPtr(formatTime(target.SupersededAt))
	}
	if target.SupersededByDigest != "" {
		out.SupersededByDigest = stringPtr(target.SupersededByDigest)
	}
	return out
}

func artifactResourceName(installationID string, artifact distribution.Artifact) string {
	return "urn:verself:" + installationID + ":distribution/packages/" + artifact.PackageName + "/artifacts/" + digestRef(artifact.OCIDigest)
}

func targetResourceName(installationID string, target distribution.Target) string {
	return "urn:verself:" + installationID + ":distribution/packages/" + target.PackageName + "/channels/" + target.ChannelName + "/targets/" + digestRef(target.ArtifactDigest)
}

func digestRef(digest string) string {
	if len(digest) > len("sha256:") && digest[:7] == "sha256:" {
		return "sha256-" + digest[7:]
	}
	return digest
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func stringPtr(value string) *string {
	return &value
}
