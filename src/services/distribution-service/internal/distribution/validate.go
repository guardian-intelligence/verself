package distribution

import (
	"fmt"
	"regexp"
)

var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var digestRefPattern = regexp.MustCompile(`^sha256-[a-f0-9]{64}$`)

func validateDigest(value string) error {
	if !digestPattern.MatchString(clean(value)) {
		return fmt.Errorf("%w: digest must match sha256:<64 lowercase hex>", ErrInvalid)
	}
	return nil
}

func validateDigestRef(value string) error {
	if !digestRefPattern.MatchString(clean(value)) {
		return fmt.Errorf("%w: digest ref must match sha256-<64 lowercase hex>", ErrInvalid)
	}
	return nil
}

func validateAdmit(req AdmitArtifactRequest, trustedBuilders map[string]struct{}, trustedSigners map[string]struct{}) error {
	if req.PackageName == "" || req.PackageVersion == "" || req.ChannelName == "" || req.PlatformOS == "" || req.PlatformArch == "" || req.Flavor == "" {
		return fmt.Errorf("%w: package, channel, platform, flavor, and version are required", ErrInvalid)
	}
	if req.OriginRegistryURL == "" || req.PublicRegistryURL == "" || req.OCIRepository == "" || req.OCIMediaType == "" {
		return fmt.Errorf("%w: oci registry, repository, digest, and media type are required", ErrInvalid)
	}
	if req.OCISizeBytes <= 0 {
		return fmt.Errorf("%w: oci_size_bytes must be positive", ErrInvalid)
	}
	if err := validateDigest(req.OCIDigest); err != nil {
		return err
	}
	if _, ok := trustedBuilders[req.BuilderID]; !ok {
		return fmt.Errorf("%w: builder_id %q is not trusted", ErrUntrustedBuilder, req.BuilderID)
	}
	if req.ChannelName == ChannelDeployment {
		return validateDeploymentAdmit(req)
	}
	if _, ok := trustedSigners[req.SignerIdentity]; !ok {
		return fmt.Errorf("%w: signer_identity %q is not trusted", ErrUntrustedSigner, req.SignerIdentity)
	}
	if req.SourceRepository == "" || req.SourceCommit == "" || req.SourceRef == "" || req.PolicyRef == "" {
		return fmt.Errorf("%w: source and policy fields are required", ErrSourcePolicyFailure)
	}
	if req.SubmittedBy == "" {
		return fmt.Errorf("%w: submitted_by is required", ErrInvalid)
	}
	required := map[string]bool{
		EvidenceCosign: false,
		EvidenceSLSA:   false,
		EvidenceSBOM:   false,
	}
	if req.ChannelName == "stable" || req.ChannelName == "rc" {
		required[EvidenceTest] = false
	}
	for _, evidence := range req.Evidence {
		if err := validateEvidence(req.OCIDigest, evidence); err != nil {
			return err
		}
		if _, ok := required[evidence.EvidenceKind]; ok {
			required[evidence.EvidenceKind] = true
		}
	}
	for kind, present := range required {
		if !present {
			return fmt.Errorf("%w: missing %s evidence", ErrMissingReferrers, kind)
		}
	}
	return nil
}

func validateDeploymentAdmit(req AdmitArtifactRequest) error {
	// Deployment signer identity is derived from sigstore-bundle verification;
	// a self-asserted identity on this channel is a contract violation.
	if req.SignerIdentity != "" {
		return fmt.Errorf("%w: signer_identity must be absent on the deployment channel", ErrInvalid)
	}
	if req.SourceRepository == "" || req.SourceCommit == "" || req.SourceRef == "" {
		return fmt.Errorf("%w: source fields are required", ErrSourcePolicyFailure)
	}
	if req.PolicyRef != PolicyDeploymentOCI {
		return fmt.Errorf("%w: deployment OCI policy ref is required", ErrSourcePolicyFailure)
	}
	if req.SubmittedBy == "" {
		return fmt.Errorf("%w: submitted_by is required", ErrInvalid)
	}
	return nil
}

func validateEvidence(artifactDigest string, evidence Evidence) error {
	// signing_key_id is derived by deployment-channel bundle verification;
	// it is never accepted from requests on any channel.
	if evidence.SigningKeyID != "" {
		return fmt.Errorf("%w: evidence signing_key_id must be absent", ErrInvalid)
	}
	if evidence.EvidenceKind == "" || evidence.PredicateType == "" || evidence.DocumentDigest == "" || evidence.OCIReferrerDigest == "" {
		return fmt.Errorf("%w: evidence kind, predicate, document digest, and referrer digest are required", ErrMissingReferrers)
	}
	if evidence.SubjectDigest != artifactDigest {
		return fmt.Errorf("%w: evidence subject digest %q does not match artifact digest %q", ErrDigestMismatch, evidence.SubjectDigest, artifactDigest)
	}
	if err := validateDigest(evidence.SubjectDigest); err != nil {
		return err
	}
	if err := validateDigest(evidence.DocumentDigest); err != nil {
		return err
	}
	if err := validateDigest(evidence.OCIReferrerDigest); err != nil {
		return err
	}
	if evidence.EvidenceKind == EvidenceSLSA && evidence.PredicateType != PredicateSLSAProvenance {
		return fmt.Errorf("%w: SLSA provenance predicate type is required", ErrSourcePolicyFailure)
	}
	return nil
}

func validatePromote(req PromoteTargetRequest) error {
	if req.PackageName == "" || req.PackageVersion == "" || req.ChannelName == "" || req.PlatformOS == "" || req.PlatformArch == "" || req.Flavor == "" {
		return fmt.Errorf("%w: package, version, channel, platform, and flavor are required", ErrInvalid)
	}
	if err := validateDigest(req.ArtifactDigest); err != nil {
		return err
	}
	if req.PolicyRef == "" || req.PromotedBy == "" || req.Reason == "" {
		return fmt.Errorf("%w: policy_ref, promoted_by, and reason are required", ErrChannelPolicyFailure)
	}
	return nil
}
