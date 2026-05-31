package releaseattest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"regexp"
	"strings"
)

const ReleaseInputDomain = "sh.verself.release.input.v1"

var digestRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type ReleaseInput struct {
	DistributionChallenge      string
	Package                    string
	Version                    string
	SourceCommit               string
	Platform                   string
	Flavor                     string
	OCIManifestDigest          string
	ArtifactDigest             string
	ProvenanceDigest           string
	SBOMDigest                 string
	TPMReleasePublicName       string
	TPMReleasePublicBlobDigest string
}

func (input ReleaseInput) Digest() (string, error) {
	input = input.normalized()
	if err := input.Validate(); err != nil {
		return "", err
	}
	h := sha256.New()
	writeField(h, "domain_separator", ReleaseInputDomain)
	writeField(h, "distribution_challenge", input.DistributionChallenge)
	writeField(h, "package", input.Package)
	writeField(h, "version", input.Version)
	writeField(h, "source_commit", input.SourceCommit)
	writeField(h, "platform", input.Platform)
	writeField(h, "flavor", input.Flavor)
	writeField(h, "oci_manifest_digest", input.OCIManifestDigest)
	writeField(h, "artifact_digest", input.ArtifactDigest)
	writeField(h, "provenance_digest", input.ProvenanceDigest)
	writeField(h, "sbom_digest", input.SBOMDigest)
	writeField(h, "tpm_release_public_name", input.TPMReleasePublicName)
	writeField(h, "tpm_release_public_blob_digest", input.TPMReleasePublicBlobDigest)
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func (input ReleaseInput) Validate() error {
	missing := []string{}
	required := []struct {
		name  string
		value string
	}{
		{"distribution_challenge", input.DistributionChallenge},
		{"package", input.Package},
		{"version", input.Version},
		{"source_commit", input.SourceCommit},
		{"platform", input.Platform},
		{"flavor", input.Flavor},
		{"oci_manifest_digest", input.OCIManifestDigest},
		{"artifact_digest", input.ArtifactDigest},
		{"provenance_digest", input.ProvenanceDigest},
		{"sbom_digest", input.SBOMDigest},
		{"tpm_release_public_name", input.TPMReleasePublicName},
		{"tpm_release_public_blob_digest", input.TPMReleasePublicBlobDigest},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("release input missing required fields: %s", strings.Join(missing, ", "))
	}
	digests := []struct {
		name  string
		value string
	}{
		{"oci_manifest_digest", input.OCIManifestDigest},
		{"artifact_digest", input.ArtifactDigest},
		{"provenance_digest", input.ProvenanceDigest},
		{"sbom_digest", input.SBOMDigest},
		{"tpm_release_public_blob_digest", input.TPMReleasePublicBlobDigest},
	}
	for _, field := range digests {
		if !digestRE.MatchString(field.value) {
			return fmt.Errorf("release input %s must match sha256:<64 lowercase hex>", field.name)
		}
	}
	return nil
}

func DigestBytes(digest string) ([]byte, error) {
	digest = strings.TrimSpace(digest)
	if !digestRE.MatchString(digest) {
		return nil, fmt.Errorf("digest must match sha256:<64 lowercase hex>")
	}
	out, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (input ReleaseInput) normalized() ReleaseInput {
	input.DistributionChallenge = strings.TrimSpace(input.DistributionChallenge)
	input.Package = strings.TrimSpace(input.Package)
	input.Version = strings.TrimSpace(input.Version)
	input.SourceCommit = strings.TrimSpace(input.SourceCommit)
	input.Platform = strings.TrimSpace(input.Platform)
	input.Flavor = strings.TrimSpace(input.Flavor)
	input.OCIManifestDigest = strings.TrimSpace(input.OCIManifestDigest)
	input.ArtifactDigest = strings.TrimSpace(input.ArtifactDigest)
	input.ProvenanceDigest = strings.TrimSpace(input.ProvenanceDigest)
	input.SBOMDigest = strings.TrimSpace(input.SBOMDigest)
	input.TPMReleasePublicName = strings.TrimSpace(input.TPMReleasePublicName)
	input.TPMReleasePublicBlobDigest = strings.TrimSpace(input.TPMReleasePublicBlobDigest)
	return input
}

func writeField(h hash.Hash, name string, value string) {
	writeString(h, name)
	writeString(h, value)
}

func writeString(h hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(value))
}
