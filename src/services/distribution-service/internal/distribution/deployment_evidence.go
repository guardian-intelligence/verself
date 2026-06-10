package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	ocidigest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/verself/attestation/bundle"
	"github.com/verself/attestation/predicate"
)

// DeploymentOCIEvidenceVerifier admits deployment-channel artifacts from
// signed sigstore evidence bundles attached as OCI referrers. Signature trust
// is delegated to the attestation module (bundle.Verify against the pinned
// Ring); this type owns only the admission policy applied to the verified,
// typed statement.
type DeploymentOCIEvidenceVerifier struct {
	// Ring is the closed set of trusted deployment-signing public keys.
	Ring *bundle.Ring
}

func (v DeploymentOCIEvidenceVerifier) VerifyDeploymentEvidence(ctx context.Context, req AdmitArtifactRequest) ([]Evidence, error) {
	store, err := deploymentEvidenceRepository(req.OriginRegistryURL, req.OCIRepository)
	if err != nil {
		return nil, err
	}
	return verifyDeploymentEvidenceFromStore(ctx, store, v.Ring, req)
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

func verifyDeploymentEvidenceFromStore(ctx context.Context, store bundle.Store, ring *bundle.Ring, req AdmitArtifactRequest) ([]Evidence, error) {
	if store == nil {
		return nil, fmt.Errorf("deployment evidence store is required")
	}
	if ring == nil {
		return nil, bundle.ErrEmptyRing
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
	discoveries, err := bundle.Discover(ctx, store, subject)
	if err != nil {
		return nil, fmt.Errorf("discover deployment evidence bundles for %s: %w", req.OCIDigest, err)
	}
	if len(discoveries) == 0 {
		return nil, fmt.Errorf("%w: no sigstore evidence bundle referrer for subject %s", ErrMissingReferrers, req.OCIDigest)
	}
	var firstErr error
	for _, discovery := range discoveries {
		evidence, err := verifyDeploymentBundle(ctx, discovery, ring, req)
		if err == nil {
			return []Evidence{evidence}, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, fmt.Errorf("%w: no deployment evidence bundle admitted for subject %s: %w", ErrSourcePolicyFailure, req.OCIDigest, firstErr)
}

// verifyDeploymentBundle verifies one discovered bundle cryptographically and
// then applies deployment admission policy to the verified, typed statement.
// The order is fixed: no policy field is read before bundle.Verify succeeds.
func verifyDeploymentBundle(ctx context.Context, discovery bundle.Discovery, ring *bundle.Ring, req AdmitArtifactRequest) (Evidence, error) {
	// Deployment channel is offline key-only verification: zero VerifyOptions.
	statement, verification, err := bundle.Verify(ctx, discovery.Envelope, ring, strings.TrimPrefix(req.OCIDigest, "sha256:"), bundle.VerifyOptions{})
	if err != nil {
		return Evidence{}, err
	}
	provenance, ok := statement.Predicate().(predicate.SLSAProvenance)
	if !ok {
		return Evidence{}, fmt.Errorf("%w: deployment statement predicate is not SLSA provenance", ErrSourcePolicyFailure)
	}
	if provenance.BuildType != predicate.BazelOCIBuild {
		return Evidence{}, fmt.Errorf("%w: deployment statement buildType=%q", ErrSourcePolicyFailure, provenance.BuildType)
	}
	params := provenance.ExternalParameters
	if params.BazelTarget == "" || params.BazelPushTarget == "" {
		return Evidence{}, fmt.Errorf("%w: deployment statement missing Bazel target metadata", ErrSourcePolicyFailure)
	}
	if params.OCIRepository != req.OCIRepository {
		return Evidence{}, fmt.Errorf("%w: deployment statement ociRepository=%q", ErrSourcePolicyFailure, params.OCIRepository)
	}
	if params.Site != req.Flavor {
		return Evidence{}, fmt.Errorf("%w: deployment statement site=%q", ErrSourcePolicyFailure, params.Site)
	}
	if provenance.BuilderID != req.BuilderID {
		return Evidence{}, fmt.Errorf("%w: deployment statement builder=%q", ErrUntrustedBuilder, provenance.BuilderID)
	}
	if provenance.InvocationID == "" {
		return Evidence{}, fmt.Errorf("%w: deployment statement missing invocation id", ErrSourcePolicyFailure)
	}
	if !deploymentStatementHasSource(provenance.ResolvedDependencies, req.SourceRepository, req.SourceCommit) {
		return Evidence{}, fmt.Errorf("%w: deployment statement missing source dependency for %s@%s", ErrSourcePolicyFailure, req.SourceRepository, req.SourceCommit)
	}
	documentSum := sha256.Sum256(discovery.Envelope.Bytes())
	return Evidence{
		EvidenceKind:      EvidenceSLSA,
		PredicateType:     PredicateSLSAProvenance,
		SubjectDigest:     req.OCIDigest,
		DocumentDigest:    "sha256:" + hex.EncodeToString(documentSum[:]),
		OCIReferrerDigest: discovery.ReferrerDigest,
		SigningKeyID:      string(verification.KeyID),
	}, nil
}

func deploymentStatementHasSource(dependencies []predicate.ResolvedDependency, repository, commit string) bool {
	wantURI := "git+https://github.com/" + repository + "@" + commit
	for _, dependency := range dependencies {
		if dependency.URI == wantURI && dependency.GitCommit == commit {
			return true
		}
	}
	return false
}
