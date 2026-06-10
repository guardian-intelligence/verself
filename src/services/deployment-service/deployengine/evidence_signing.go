package deployengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

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
// The Transit signer is resolved lazily on first publish so the service can boot
// before OpenBao is reachable. Only successful resolution is cached: a publish
// that races an OpenBao seal/restart fails loudly and the next publish retries.
type TransitEvidencePublisher struct {
	// KeyRef is a hashivault:// reference, e.g. "hashivault://deployment-signing".
	KeyRef string

	mu     sync.Mutex
	signer *transit.Signer
}

// NewTransitEvidencePublisher constructs a publisher for the given Transit key
// reference. Connection/auth come from BAO_ADDR/BAO_TOKEN in the environment.
func NewTransitEvidencePublisher(keyRef string) (*TransitEvidencePublisher, error) {
	if strings.TrimSpace(keyRef) == "" {
		return nil, fmt.Errorf("deployment evidence signing key reference is required")
	}
	return &TransitEvidencePublisher{KeyRef: keyRef}, nil
}

func (p *TransitEvidencePublisher) resolveSigner(ctx context.Context) (*transit.Signer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.signer != nil {
		return p.signer, nil
	}
	signer, err := transit.New(ctx, p.KeyRef)
	if err != nil {
		return nil, err
	}
	p.signer = signer
	return signer, nil
}

// PublishDeploymentEvidence builds, Transit-signs, and attaches the SLSA
// provenance bundle, returning the evidence descriptors for admission.
func (p *TransitEvidencePublisher) PublishDeploymentEvidence(ctx context.Context, req DeploymentEvidencePublishRequest) ([]DeploymentImageEvidence, error) {
	if err := validateDeploymentEvidenceRequest(req); err != nil {
		return nil, err
	}
	signer, err := p.resolveSigner(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve deployment signing key %s: %w", p.KeyRef, err)
	}
	target, err := newDeploymentEvidenceTarget(req)
	if err != nil {
		return nil, err
	}
	statement, err := predicate.NewProvenance(
		[]predicate.Subject{{
			Name:   req.PullReference,
			SHA256: strings.TrimPrefix(req.Digest, "sha256:"),
		}},
		predicate.SLSAProvenance{
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
		},
	)
	if err != nil {
		return nil, fmt.Errorf("build deployment provenance: %w", err)
	}
	// Deployment channel is offline: no Rekor transparency log.
	envelope, err := bundle.Sign(ctx, statement, signer, bundle.SignOptions{})
	if err != nil {
		return nil, fmt.Errorf("sign deployment provenance: %w", err)
	}
	subject := v1.Descriptor{
		MediaType: req.MediaType,
		Digest:    digest.Digest(req.Digest),
		Size:      req.SizeBytes,
	}
	referrer, err := bundle.Attach(ctx, target, subject, envelope)
	if err != nil {
		return nil, fmt.Errorf("attach deployment provenance referrer: %w", err)
	}
	documentSum := sha256.Sum256(envelope.Bytes())
	return []DeploymentImageEvidence{{
		EvidenceKind:      "slsa_provenance",
		PredicateType:     predicateSLSAProvenance,
		SubjectDigest:     req.Digest,
		DocumentDigest:    "sha256:" + hex.EncodeToString(documentSum[:]),
		OCIReferrerDigest: referrer.Digest.String(),
	}}, nil
}
