package api

import (
	"time"

	"github.com/verself/distribution-service/internal/distribution"
	distributionreleaseattest "github.com/verself/distribution-service/internal/releaseattest"
)

type checkUpdateBody struct {
	PackageName      string `json:"package_name"`
	ChannelName      string `json:"channel_name"`
	PlatformOS       string `json:"platform_os"`
	PlatformArch     string `json:"platform_arch"`
	Flavor           string `json:"flavor"`
	InstalledDigest  string `json:"installed_digest,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

type upgradeVerificationBody struct {
	PackageName      string `json:"package_name"`
	ChannelName      string `json:"channel_name"`
	PlatformOS       string `json:"platform_os"`
	PlatformArch     string `json:"platform_arch"`
	Flavor           string `json:"flavor"`
	ArtifactDigest   string `json:"artifact_digest"`
	LayerDigest      string `json:"layer_digest"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

type admitArtifactBody struct {
	PackageName       string `json:"package_name"`
	PackageVersion    string `json:"package_version"`
	ChannelName       string `json:"channel_name"`
	PlatformOS        string `json:"platform_os"`
	PlatformArch      string `json:"platform_arch"`
	Flavor            string `json:"flavor"`
	OriginRegistryURL string `json:"origin_registry_url"`
	PublicRegistryURL string `json:"public_registry_url"`
	OCIRepository     string `json:"oci_repository"`
	OCIDigest         string `json:"oci_digest"`
	OCIMediaType      string `json:"oci_media_type"`
	OCISizeBytes      int64  `json:"oci_size_bytes"`
	BuilderID         string `json:"builder_id"`
	// Release channels only; must be absent on the deployment channel.
	SignerIdentity     string                   `json:"signer_identity,omitempty"`
	SourceRepository   string                   `json:"source_repository"`
	SourceCommit       string                   `json:"source_commit"`
	SourceRef          string                   `json:"source_ref"`
	PolicyRef          string                   `json:"policy_ref"`
	Evidence           []evidenceRecord         `json:"evidence"`
	ReleaseAttestation releaseAttestationRecord `json:"release_attestation"`
	SubmittedBy        string                   `json:"submitted_by"`
}

type promoteTargetBody struct {
	ArtifactDigest string `json:"artifact_digest"`
	PackageVersion string `json:"package_version"`
	PlatformOS     string `json:"platform_os"`
	PlatformArch   string `json:"platform_arch"`
	Flavor         string `json:"flavor"`
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
	SigningKeyID      string `json:"signing_key_id,omitempty"`
}

type releaseAttestationRecord struct {
	DistributionChallenge      string            `json:"distribution_challenge"`
	ReleaseInputDigest         string            `json:"release_input_digest"`
	ArtifactDigest             string            `json:"artifact_digest"`
	ProvenanceDigest           string            `json:"provenance_digest"`
	SBOMDigest                 string            `json:"sbom_digest"`
	TPMReleasePublicName       string            `json:"tpm_release_public_name"`
	TPMReleasePublicBlobDigest string            `json:"tpm_release_public_blob_digest"`
	PolicyID                   string            `json:"policy_id"`
	TPM                        tpmEvidenceRecord `json:"tpm"`
}

type tpmEvidenceRecord struct {
	AKPublic                []byte                        `json:"ak_public"`
	Quotes                  []tpmQuoteRecord              `json:"quotes"`
	PCRs                    []tpmPCRRecord                `json:"pcrs"`
	EventLog                []byte                        `json:"event_log"`
	ReleasePublicName       string                        `json:"release_public_name"`
	ReleasePublicBlob       []byte                        `json:"release_public_blob"`
	ReleasePublicBlobDigest string                        `json:"release_public_blob_digest"`
	ReleaseKeyCertification releaseKeyCertificationRecord `json:"release_key_certification"`
}

type tpmQuoteRecord struct {
	Quote     []byte `json:"quote"`
	Signature []byte `json:"signature"`
}

type tpmPCRRecord struct {
	Index     int    `json:"index"`
	Digest    string `json:"digest"`
	DigestAlg string `json:"digest_alg"`
}

type releaseKeyCertificationRecord struct {
	Public            []byte `json:"public"`
	CreateData        []byte `json:"create_data"`
	CreateAttestation []byte `json:"create_attestation"`
	CreateSignature   []byte `json:"create_signature"`
}

type verificationRecord struct {
	Decision         string           `json:"decision"`
	Reason           string           `json:"reason"`
	BuilderID        string           `json:"builder_id"`
	SignerIdentity   string           `json:"signer_identity,omitempty"`
	SigningKeyID     string           `json:"signing_key_id,omitempty"`
	SourceRepository string           `json:"source_repository"`
	SourceCommit     string           `json:"source_commit"`
	SourceRef        string           `json:"source_ref"`
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
	ChannelName        string             `json:"channel_name"`
	PlatformOS         string             `json:"platform_os"`
	PlatformArch       string             `json:"platform_arch"`
	Flavor             string             `json:"flavor"`
	OCIRepository      string             `json:"oci_repository"`
	PublicOCIReference string             `json:"public_oci_reference"`
	OCIMediaType       string             `json:"oci_media_type"`
	OCISizeBytes       int64              `json:"oci_size_bytes"`
	PolicyRef          string             `json:"policy_ref"`
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
	Flavor             string  `json:"flavor"`
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
			SigningKeyID:      item.SigningKeyID,
		})
	}
	return out
}

func releaseAttestationFromDTO(input releaseAttestationRecord) distribution.ReleaseAttestation {
	quotes := make([]distributionreleaseattest.Quote, 0, len(input.TPM.Quotes))
	for _, quote := range input.TPM.Quotes {
		quotes = append(quotes, distributionreleaseattest.Quote{
			Quote:     quote.Quote,
			Signature: quote.Signature,
		})
	}
	pcrs := make([]distributionreleaseattest.PCR, 0, len(input.TPM.PCRs))
	for _, pcr := range input.TPM.PCRs {
		pcrs = append(pcrs, distributionreleaseattest.PCR{
			Index:     pcr.Index,
			Digest:    pcr.Digest,
			DigestAlg: pcr.DigestAlg,
		})
	}
	return distribution.ReleaseAttestation{
		DistributionChallenge:      input.DistributionChallenge,
		ReleaseInputDigest:         input.ReleaseInputDigest,
		ArtifactDigest:             input.ArtifactDigest,
		ProvenanceDigest:           input.ProvenanceDigest,
		SBOMDigest:                 input.SBOMDigest,
		TPMReleasePublicName:       input.TPMReleasePublicName,
		TPMReleasePublicBlobDigest: input.TPMReleasePublicBlobDigest,
		PolicyID:                   input.PolicyID,
		TPM: distributionreleaseattest.TPMEvidence{
			AKPublic:                input.TPM.AKPublic,
			Quotes:                  quotes,
			PCRs:                    pcrs,
			EventLog:                input.TPM.EventLog,
			ReleasePublicName:       input.TPM.ReleasePublicName,
			ReleasePublicBlob:       input.TPM.ReleasePublicBlob,
			ReleasePublicBlobDigest: input.TPM.ReleasePublicBlobDigest,
			ReleaseKeyCertification: distributionreleaseattest.ReleaseKeyCertification{
				Public:            input.TPM.ReleaseKeyCertification.Public,
				CreateData:        input.TPM.ReleaseKeyCertification.CreateData,
				CreateAttestation: input.TPM.ReleaseKeyCertification.CreateAttestation,
				CreateSignature:   input.TPM.ReleaseKeyCertification.CreateSignature,
			},
		},
	}
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
			SigningKeyID:      item.SigningKeyID,
		})
	}
	out := artifactRecord{
		ArtifactDigest:     artifact.OCIDigest,
		ArtifactDigestRef:  digestRef(artifact.OCIDigest),
		ResourceName:       artifactResourceName(installationID, artifact),
		PackageName:        artifact.PackageName,
		PackageVersion:     artifact.PackageVersion,
		ChannelName:        artifact.ChannelName,
		PlatformOS:         artifact.PlatformOS,
		PlatformArch:       artifact.PlatformArch,
		Flavor:             artifact.Flavor,
		OCIRepository:      artifact.OCIRepository,
		PublicOCIReference: artifact.PublicOCIReference,
		OCIMediaType:       artifact.OCIMediaType,
		OCISizeBytes:       artifact.OCISizeBytes,
		PolicyRef:          artifact.PolicyRef,
		State:              artifact.State,
		RetentionClass:     artifact.RetentionClass,
		Verification: verificationRecord{
			Decision:         artifact.Verification.Decision,
			Reason:           artifact.Verification.Reason,
			BuilderID:        artifact.Verification.BuilderID,
			SignerIdentity:   artifact.Verification.SignerIdentity,
			SigningKeyID:     artifact.Verification.SigningKeyID,
			SourceRepository: artifact.Verification.SourceRepository,
			SourceCommit:     artifact.Verification.SourceCommit,
			SourceRef:        artifact.Verification.SourceRef,
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
		Flavor:             target.Flavor,
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
	return "urn:verself:" + installationID + ":distribution/packages/" + artifact.PackageName + "/versions/" + artifact.PackageVersion + "/artifacts/" + digestRef(artifact.OCIDigest)
}

func targetResourceName(installationID string, target distribution.Target) string {
	return "urn:verself:" + installationID + ":distribution/packages/" + target.PackageName + "/channels/" + target.ChannelName + "/versions/" + target.PackageVersion + "/targets/" + digestRef(target.ArtifactDigest)
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
