package deployengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"

	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/verself/attestation/bundle"
	"github.com/verself/attestation/predicate"
	"github.com/verself/attestation/transit"
)

// TransitEvidencePublisher signs deployment SLSA provenance with an OpenBao
// Transit ECDSA P-256 key and attaches it as a sigstore DSSE bundle OCI
// referrer next to the pushed workload image. It is the production
// OCIEvidencePublisher; there is no unsigned path.
//
// The Transit signer is resolved per publish: the service can boot before
// OpenBao is reachable, and a deployment-signing key rotation is picked up by
// the next publish without a restart. Resolution failure fails the publish
// loudly and the next publish retries.
type TransitEvidencePublisher struct {
	// KeyRef is a hashivault:// reference, e.g. "hashivault://deployment-signing".
	KeyRef string
}

// NewTransitEvidencePublisher constructs a publisher for the given Transit key
// reference. Connection/auth come from BAO_ADDR/BAO_TOKEN in the environment.
func NewTransitEvidencePublisher(keyRef string) (*TransitEvidencePublisher, error) {
	if strings.TrimSpace(keyRef) == "" {
		return nil, fmt.Errorf("deployment evidence signing key reference is required")
	}
	return &TransitEvidencePublisher{KeyRef: keyRef}, nil
}

// PublishDeploymentEvidence builds, Transit-signs, and attaches the SLSA
// provenance bundle, returning the evidence descriptors for admission.
func (p *TransitEvidencePublisher) PublishDeploymentEvidence(ctx context.Context, req DeploymentEvidencePublishRequest) ([]DeploymentImageEvidence, error) {
	if err := validateDeploymentEvidenceRequest(req); err != nil {
		return nil, err
	}
	signer, err := transit.New(ctx, p.KeyRef)
	if err != nil {
		return nil, fmt.Errorf("resolve deployment signing key %s: %w", p.KeyRef, err)
	}
	target, err := newDeploymentEvidenceTarget(req)
	if err != nil {
		return nil, err
	}
	subject := v1.Descriptor{
		MediaType: req.MediaType,
		Digest:    digest.Digest(req.Digest),
		Size:      req.SizeBytes,
	}
	provenance := deploymentProvenance(req)
	if existing, ok, err := reusableEvidence(ctx, signer, target, subject, provenance, req); err != nil {
		return nil, err
	} else if ok {
		return existing, nil
	}
	statement, err := predicate.NewProvenance(
		[]predicate.Subject{{
			Name:   req.PullReference,
			SHA256: strings.TrimPrefix(req.Digest, "sha256:"),
		}},
		provenance,
	)
	if err != nil {
		return nil, fmt.Errorf("build deployment provenance: %w", err)
	}
	// Deployment channel is offline: no Rekor transparency log.
	envelope, err := bundle.Sign(ctx, statement, signer, bundle.SignOptions{})
	if err != nil {
		return nil, fmt.Errorf("sign deployment provenance: %w", err)
	}
	referrer, err := bundle.Attach(ctx, target, subject, envelope)
	if err != nil {
		return nil, fmt.Errorf("attach deployment provenance referrer: %w", err)
	}
	return []DeploymentImageEvidence{deploymentEvidence(req.Digest, envelope.Bytes(), referrer.Digest.String())}, nil
}

func deploymentProvenance(req DeploymentEvidencePublishRequest) predicate.SLSAProvenance {
	return predicate.SLSAProvenance{
		BuildType: predicate.BazelOCIBuild,
		ExternalParameters: predicate.ExternalParameters{
			BazelTarget:     req.ImageLabel,
			BazelPushTarget: req.PushLabel,
			OCIRepository:   req.Repository,
			Site:            req.Site,
		},
		ResolvedDependencies: []predicate.ResolvedDependency{{
			URI:       "git+https://github.com/" + req.SourceRepository + "@" + req.SHA,
			GitCommit: req.SHA,
		}},
		BuilderID:    req.BuilderID,
		InvocationID: req.DeployRunKey,
	}
}

func deploymentEvidence(subjectDigest string, envelopeBytes []byte, referrerDigest string) DeploymentImageEvidence {
	documentSum := sha256.Sum256(envelopeBytes)
	return DeploymentImageEvidence{
		EvidenceKind:      "slsa_provenance",
		PredicateType:     predicateSLSAProvenance,
		SubjectDigest:     subjectDigest,
		DocumentDigest:    "sha256:" + hex.EncodeToString(documentSum[:]),
		OCIReferrerDigest: referrerDigest,
	}
}

// reusableEvidence finds a prior referrer bundle for this subject that
// verifies under the current key and whose statement matches everything this
// deploy would sign except the per-run invocation id, so repeated deploys of
// an unchanged digest reuse evidence instead of growing the referrer set by
// one bundle per deploy run. The full-predicate match keeps reuse exactly as
// strict as admission: a bundle from a different commit, builder, site, or
// repository is never reused, since admission would reject it forever.
func reusableEvidence(ctx context.Context, signer *transit.Signer, store bundle.Store, subject v1.Descriptor, want predicate.SLSAProvenance, req DeploymentEvidencePublishRequest) ([]DeploymentImageEvidence, bool, error) {
	ring, err := bundle.NewRingFromPEM(signer.PublicKeyPEM())
	if err != nil {
		return nil, false, fmt.Errorf("build signer ring for evidence reuse: %w", err)
	}
	discoveries, err := bundle.Discover(ctx, store, subject)
	if err != nil {
		return nil, false, fmt.Errorf("discover existing deployment evidence for %s: %w", req.Digest, err)
	}
	subjectHex := strings.TrimPrefix(req.Digest, "sha256:")
	for _, d := range discoveries {
		stmt, _, err := bundle.Verify(ctx, d.Envelope, ring, subjectHex, bundle.VerifyOptions{})
		if err != nil {
			// Foreign or stale-key bundles never block fresh signing.
			continue
		}
		prov, ok := stmt.Predicate().(predicate.SLSAProvenance)
		if !ok {
			continue
		}
		prov.InvocationID = want.InvocationID
		if !reflect.DeepEqual(prov, want) {
			continue
		}
		return []DeploymentImageEvidence{deploymentEvidence(req.Digest, d.Envelope.Bytes(), d.ReferrerDigest)}, true, nil
	}
	return nil, false, nil
}
