package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	ocidigest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
)

const (
	mediaTypeInTotoStatement    = "application/vnd.in-toto+json"
	inTotoStatementV1           = "https://in-toto.io/Statement/v1"
	deploymentRulesOCIBuildType = "https://bazel.build/rules_oci/oci_image"
	maxDeploymentEvidenceBytes  = 1 << 20
)

type DeploymentOCIEvidenceVerifier struct{}

func (DeploymentOCIEvidenceVerifier) VerifyDeploymentEvidence(ctx context.Context, req AdmitArtifactRequest) ([]Evidence, error) {
	store, err := deploymentEvidenceRepository(req.OriginRegistryURL, req.OCIRepository)
	if err != nil {
		return nil, err
	}
	return verifyDeploymentEvidenceFromStore(ctx, store, req)
}

type deploymentEvidenceStore interface {
	Predecessors(context.Context, v1.Descriptor) ([]v1.Descriptor, error)
	Fetch(context.Context, v1.Descriptor) (io.ReadCloser, error)
}

func deploymentEvidenceRepository(registryURL, repository string) (*remote.Repository, error) {
	parsed, err := url.Parse(strings.TrimSpace(registryURL))
	if err != nil {
		return nil, fmt.Errorf("parse deployment OCI registry URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("deployment OCI registry URL must be http or https")
	}
	if parsed.Host == "" || strings.Trim(parsed.Path, "/") != "" {
		return nil, fmt.Errorf("deployment OCI registry URL must not include a repository path")
	}
	repository = strings.Trim(repository, "/")
	if repository == "" {
		return nil, fmt.Errorf("deployment OCI repository is required")
	}
	target, err := remote.NewRepository(parsed.Host + "/" + repository)
	if err != nil {
		return nil, fmt.Errorf("create deployment OCI repository: %w", err)
	}
	target.PlainHTTP = parsed.Scheme == "http"
	if err := target.SetReferrersCapability(true); err != nil {
		return nil, fmt.Errorf("enable deployment OCI referrers: %w", err)
	}
	return target, nil
}

func verifyDeploymentEvidenceFromStore(ctx context.Context, store deploymentEvidenceStore, req AdmitArtifactRequest) ([]Evidence, error) {
	if store == nil {
		return nil, fmt.Errorf("deployment evidence store is required")
	}
	subjectDigest, err := ocidigest.Parse(req.OCIDigest)
	if err != nil {
		return nil, fmt.Errorf("parse deployment OCI subject digest: %w", err)
	}
	subject := v1.Descriptor{
		MediaType: req.OCIMediaType,
		Digest:    subjectDigest,
		Size:      req.OCISizeBytes,
	}
	referrers, err := store.Predecessors(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("list deployment OCI referrers for %s: %w", req.OCIDigest, err)
	}
	candidates := deploymentSLSAReferrerCandidates(referrers)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: missing deployment SLSA referrer for subject %s", ErrMissingReferrers, req.OCIDigest)
	}
	var firstErr error
	for _, referrer := range candidates {
		evidence, err := verifyDeploymentSLSAReferrer(ctx, store, req, referrer)
		if err == nil {
			return []Evidence{evidence}, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, fmt.Errorf("%w: no deployment SLSA referrer matched policy for subject %s: %w", ErrSourcePolicyFailure, req.OCIDigest, firstErr)
}

func verifyDeploymentSLSAReferrer(ctx context.Context, store deploymentEvidenceStore, req AdmitArtifactRequest, referrer v1.Descriptor) (Evidence, error) {
	manifestBody, err := fetchDescriptorBytes(ctx, store, referrer)
	if err != nil {
		return Evidence{}, fmt.Errorf("fetch deployment SLSA referrer manifest %s: %w", referrer.Digest, err)
	}
	var manifest v1.Manifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return Evidence{}, fmt.Errorf("decode deployment SLSA referrer manifest %s: %w", referrer.Digest, err)
	}
	if err := validateDeploymentSLSAManifest(req, manifest); err != nil {
		return Evidence{}, err
	}
	statementDescriptor := manifest.Layers[0]
	statementBody, err := fetchDescriptorBytes(ctx, store, statementDescriptor)
	if err != nil {
		return Evidence{}, fmt.Errorf("fetch deployment SLSA statement %s: %w", statementDescriptor.Digest, err)
	}
	if err := validateDeploymentSLSAStatement(req, statementBody); err != nil {
		return Evidence{}, err
	}
	return Evidence{
		EvidenceKind:      EvidenceSLSA,
		PredicateType:     PredicateSLSAProvenance,
		SubjectDigest:     req.OCIDigest,
		DocumentDigest:    statementDescriptor.Digest.String(),
		OCIReferrerDigest: referrer.Digest.String(),
	}, nil
}

func deploymentSLSAReferrerCandidates(descriptors []v1.Descriptor) []v1.Descriptor {
	explicit := make([]v1.Descriptor, 0, len(descriptors))
	unspecified := make([]v1.Descriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		switch descriptor.ArtifactType {
		case mediaTypeInTotoStatement:
			explicit = append(explicit, descriptor)
		case "":
			unspecified = append(unspecified, descriptor)
		}
	}
	if len(explicit) > 0 {
		sortDescriptorsByDigest(explicit)
		return explicit
	}
	sortDescriptorsByDigest(unspecified)
	return unspecified
}

func sortDescriptorsByDigest(descriptors []v1.Descriptor) {
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Digest.String() < descriptors[j].Digest.String()
	})
}

func fetchDescriptorBytes(ctx context.Context, store deploymentEvidenceStore, descriptor v1.Descriptor) ([]byte, error) {
	if err := descriptor.Digest.Validate(); err != nil {
		return nil, fmt.Errorf("deployment evidence descriptor digest %q is invalid: %w", descriptor.Digest, err)
	}
	if descriptor.Digest.Algorithm() != ocidigest.SHA256 {
		return nil, fmt.Errorf("%w: deployment evidence descriptor digest algorithm %q is not sha256", ErrDigestMismatch, descriptor.Digest.Algorithm())
	}
	reader, err := store.Fetch(ctx, descriptor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(io.LimitReader(reader, maxDeploymentEvidenceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDeploymentEvidenceBytes {
		return nil, fmt.Errorf("deployment evidence descriptor %s exceeds %d bytes", descriptor.Digest, maxDeploymentEvidenceBytes)
	}
	verifier := descriptor.Digest.Verifier()
	if _, err := verifier.Write(body); err != nil {
		return nil, fmt.Errorf("verify deployment evidence descriptor %s: %w", descriptor.Digest, err)
	}
	if !verifier.Verified() {
		return nil, fmt.Errorf("%w: deployment evidence descriptor %s bytes do not match digest", ErrDigestMismatch, descriptor.Digest)
	}
	return body, nil
}

func validateDeploymentSLSAManifest(req AdmitArtifactRequest, manifest v1.Manifest) error {
	if manifest.ArtifactType != mediaTypeInTotoStatement {
		return fmt.Errorf("%w: deployment SLSA manifest artifactType=%q", ErrSourcePolicyFailure, manifest.ArtifactType)
	}
	if manifest.Subject == nil {
		return fmt.Errorf("%w: deployment SLSA manifest missing subject", ErrMissingReferrers)
	}
	if manifest.Subject.Digest.String() != req.OCIDigest {
		return fmt.Errorf("%w: deployment SLSA manifest subject %s does not match artifact %s", ErrDigestMismatch, manifest.Subject.Digest, req.OCIDigest)
	}
	if manifest.Subject.MediaType != "" && manifest.Subject.MediaType != req.OCIMediaType {
		return fmt.Errorf("%w: deployment SLSA manifest subject mediaType=%q", ErrDigestMismatch, manifest.Subject.MediaType)
	}
	if manifest.Subject.Size != 0 && manifest.Subject.Size != req.OCISizeBytes {
		return fmt.Errorf("%w: deployment SLSA manifest subject size=%d", ErrDigestMismatch, manifest.Subject.Size)
	}
	if len(manifest.Layers) != 1 {
		return fmt.Errorf("%w: deployment SLSA manifest must contain exactly one statement layer", ErrMissingReferrers)
	}
	if manifest.Layers[0].MediaType != mediaTypeInTotoStatement {
		return fmt.Errorf("%w: deployment SLSA layer mediaType=%q", ErrSourcePolicyFailure, manifest.Layers[0].MediaType)
	}
	return nil
}

type deploymentSLSAStatement struct {
	Type          string                  `json:"_type"`
	Subject       []deploymentSLSASubject `json:"subject"`
	PredicateType string                  `json:"predicateType"`
	Predicate     deploymentSLSAPredicate `json:"predicate"`
}

type deploymentSLSASubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type deploymentSLSAPredicate struct {
	BuildDefinition deploymentSLSABuildDefinition `json:"buildDefinition"`
	RunDetails      deploymentSLSARunDetails      `json:"runDetails"`
}

type deploymentSLSABuildDefinition struct {
	BuildType            string                             `json:"buildType"`
	ExternalParameters   deploymentSLSAExternalParameters   `json:"externalParameters"`
	ResolvedDependencies []deploymentSLSAResolvedDependency `json:"resolvedDependencies"`
}

type deploymentSLSAExternalParameters struct {
	BazelTarget     string `json:"bazelTarget"`
	BazelPushTarget string `json:"bazelPushTarget"`
	OCIRepository   string `json:"ociRepository"`
	Site            string `json:"site"`
}

type deploymentSLSAResolvedDependency struct {
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest"`
}

type deploymentSLSARunDetails struct {
	Builder struct {
		ID string `json:"id"`
	} `json:"builder"`
	Metadata struct {
		InvocationID string `json:"invocationId"`
	} `json:"metadata"`
}

func validateDeploymentSLSAStatement(req AdmitArtifactRequest, body []byte) error {
	var statement deploymentSLSAStatement
	if err := json.Unmarshal(body, &statement); err != nil {
		return fmt.Errorf("decode deployment SLSA statement: %w", err)
	}
	if statement.Type != inTotoStatementV1 {
		return fmt.Errorf("%w: deployment statement _type=%q", ErrSourcePolicyFailure, statement.Type)
	}
	if statement.PredicateType != PredicateSLSAProvenance {
		return fmt.Errorf("%w: deployment statement predicateType=%q", ErrSourcePolicyFailure, statement.PredicateType)
	}
	if len(statement.Subject) != 1 {
		return fmt.Errorf("%w: deployment statement must contain exactly one subject", ErrMissingReferrers)
	}
	if statement.Subject[0].Digest["sha256"] != strings.TrimPrefix(req.OCIDigest, "sha256:") {
		return fmt.Errorf("%w: deployment statement subject digest does not match artifact digest", ErrDigestMismatch)
	}
	build := statement.Predicate.BuildDefinition
	if build.BuildType != deploymentRulesOCIBuildType {
		return fmt.Errorf("%w: deployment statement buildType=%q", ErrSourcePolicyFailure, build.BuildType)
	}
	params := build.ExternalParameters
	if params.BazelTarget == "" || params.BazelPushTarget == "" {
		return fmt.Errorf("%w: deployment statement missing Bazel target metadata", ErrSourcePolicyFailure)
	}
	if params.OCIRepository != req.OCIRepository {
		return fmt.Errorf("%w: deployment statement ociRepository=%q", ErrSourcePolicyFailure, params.OCIRepository)
	}
	if params.Site != req.Flavor {
		return fmt.Errorf("%w: deployment statement site=%q", ErrSourcePolicyFailure, params.Site)
	}
	if statement.Predicate.RunDetails.Builder.ID != req.BuilderID {
		return fmt.Errorf("%w: deployment statement builder=%q", ErrUntrustedBuilder, statement.Predicate.RunDetails.Builder.ID)
	}
	if statement.Predicate.RunDetails.Metadata.InvocationID == "" {
		return fmt.Errorf("%w: deployment statement missing invocation id", ErrSourcePolicyFailure)
	}
	if !deploymentStatementHasSource(build.ResolvedDependencies, req.SourceRepository, req.SourceCommit) {
		return fmt.Errorf("%w: deployment statement missing source dependency for %s@%s", ErrSourcePolicyFailure, req.SourceRepository, req.SourceCommit)
	}
	return nil
}

func deploymentStatementHasSource(dependencies []deploymentSLSAResolvedDependency, repository, commit string) bool {
	wantURI := "git+https://github.com/" + repository + "@" + commit
	for _, dependency := range dependencies {
		if dependency.URI == wantURI && dependency.Digest["gitCommit"] == commit {
			return true
		}
	}
	return false
}
