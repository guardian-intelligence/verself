package distribution

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	ocidigest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	sigsig "github.com/sigstore/sigstore/pkg/signature"
	sigopts "github.com/sigstore/sigstore/pkg/signature/options"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"

	"github.com/verself/attestation/bundle"
	"github.com/verself/attestation/predicate"
	distributionreleaseattest "github.com/verself/distribution-service/internal/releaseattest"
	"github.com/verself/distribution-service/migrations"
	releaseinput "github.com/verself/releaseattest"
	"github.com/verself/service-runtime/pgtest"
)

func TestAdmissionPromotionAndQuarantineFlow(t *testing.T) {
	ctx, svc := newTestService(t)
	req := admitRequest("a")

	artifact, err := svc.AdmitArtifact(ctx, Principal{Actor: "distribution-rehearsal"}, req)
	if err != nil {
		t.Fatalf("admit artifact: %v", err)
	}
	if artifact.State != StateAvailable {
		t.Fatalf("artifact state = %s, want %s", artifact.State, StateAvailable)
	}
	if artifact.RetentionClass != RetentionStable {
		t.Fatalf("retention class = %s, want %s", artifact.RetentionClass, RetentionStable)
	}

	duplicate, err := svc.AdmitArtifact(ctx, Principal{Actor: "distribution-rehearsal"}, req)
	if err != nil {
		t.Fatalf("admit duplicate artifact: %v", err)
	}
	if duplicate.ArtifactID != artifact.ArtifactID {
		t.Fatalf("duplicate artifact id = %s, want %s", duplicate.ArtifactID, artifact.ArtifactID)
	}

	target, err := svc.PromoteTarget(ctx, Principal{Actor: "distribution-rehearsal"}, PromoteTargetRequest{
		PackageName:    req.PackageName,
		PackageVersion: req.PackageVersion,
		ChannelName:    req.ChannelName,
		ArtifactDigest: req.OCIDigest,
		PlatformOS:     req.PlatformOS,
		PlatformArch:   req.PlatformArch,
		Flavor:         req.Flavor,
		PolicyRef:      req.PolicyRef,
		PromotedBy:     "distribution-rehearsal",
		Reason:         "release rehearsal",
		IdempotencyKey: "promote-1",
	})
	if err != nil {
		t.Fatalf("promote target: %v", err)
	}
	if target.State != TargetStatePublished {
		t.Fatalf("target state = %s, want %s", target.State, TargetStatePublished)
	}

	replayed, err := svc.PromoteTarget(ctx, Principal{Actor: "distribution-rehearsal"}, PromoteTargetRequest{
		PackageName:    req.PackageName,
		PackageVersion: req.PackageVersion,
		ChannelName:    req.ChannelName,
		ArtifactDigest: req.OCIDigest,
		PlatformOS:     req.PlatformOS,
		PlatformArch:   req.PlatformArch,
		Flavor:         req.Flavor,
		PolicyRef:      req.PolicyRef,
		PromotedBy:     "distribution-rehearsal",
		Reason:         "release rehearsal",
		IdempotencyKey: "promote-1",
	})
	if err != nil {
		t.Fatalf("replay promote target: %v", err)
	}
	if replayed.TargetID != target.TargetID {
		t.Fatalf("replayed target id = %s, want %s", replayed.TargetID, target.TargetID)
	}

	resolved, err := svc.ResolveTarget(ctx, Principal{Actor: "cli"}, ResolveTargetRequest{
		PackageName:  req.PackageName,
		ChannelName:  req.ChannelName,
		PlatformOS:   req.PlatformOS,
		PlatformArch: req.PlatformArch,
		Flavor:       req.Flavor,
	})
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if resolved.ArtifactDigest != req.OCIDigest {
		t.Fatalf("resolved digest = %s, want %s", resolved.ArtifactDigest, req.OCIDigest)
	}
	if resolved.PublicOCIReference != "oci.verself.sh/verself/mksk@"+req.OCIDigest {
		t.Fatalf("resolved public OCI reference = %s", resolved.PublicOCIReference)
	}
	if resolved.DownloadURL != "https://oci.verself.sh/v2/verself/mksk/manifests/"+req.OCIDigest {
		t.Fatalf("resolved download URL = %s", resolved.DownloadURL)
	}

	_, updateAvailable, err := svc.CheckUpdate(ctx, Principal{Actor: "cli"}, CheckUpdateRequest{
		PackageName:     req.PackageName,
		ChannelName:     req.ChannelName,
		PlatformOS:      req.PlatformOS,
		PlatformArch:    req.PlatformArch,
		Flavor:          req.Flavor,
		InstalledDigest: req.OCIDigest,
	})
	if err != nil {
		t.Fatalf("check update: %v", err)
	}
	if updateAvailable {
		t.Fatalf("update_available = true, want false for installed digest")
	}

	_, err = svc.QuarantineArtifact(ctx, Principal{Actor: "governance-service"}, QuarantineArtifactRequest{
		ArtifactDigestRef: digestRef(req.OCIDigest),
		Reason:            "integrity incident",
		IdempotencyKey:    "quarantine-1",
	})
	if err != nil {
		t.Fatalf("quarantine artifact: %v", err)
	}
	_, err = svc.ResolveTarget(ctx, Principal{Actor: "cli"}, ResolveTargetRequest{
		PackageName:  req.PackageName,
		ChannelName:  req.ChannelName,
		PlatformOS:   req.PlatformOS,
		PlatformArch: req.PlatformArch,
		Flavor:       req.Flavor,
	})
	if !errors.Is(err, ErrQuarantined) {
		t.Fatalf("resolve quarantined target error = %v, want %v", err, ErrQuarantined)
	}
}

func TestDeploymentPromotionAllowsSameDigestAcrossVersions(t *testing.T) {
	ctx, svc := newTestService(t)
	req := deploymentAdmitRequest("c")
	svc.TrustedBuilders[req.BuilderID] = struct{}{}
	svc.ReleaseVerifier = failingReleaseVerifier{}

	first, err := svc.AdmitArtifact(ctx, Principal{Actor: req.BuilderID}, req)
	if err != nil {
		t.Fatalf("admit first deployment artifact: %v", err)
	}
	firstTarget, err := svc.PromoteTarget(ctx, Principal{Actor: "deployment-service"}, PromoteTargetRequest{
		PackageName:    req.PackageName,
		PackageVersion: req.PackageVersion,
		ChannelName:    req.ChannelName,
		ArtifactDigest: req.OCIDigest,
		PlatformOS:     req.PlatformOS,
		PlatformArch:   req.PlatformArch,
		Flavor:         req.Flavor,
		PolicyRef:      req.PolicyRef,
		PromotedBy:     "deployment-service",
		Reason:         "deployment rehearsal",
		IdempotencyKey: "promote-same-digest-first",
	})
	if err != nil {
		t.Fatalf("promote first deployment artifact: %v", err)
	}

	next := req
	next.PackageVersion = "fedcba9876543210fedcba9876543210fedcba98"
	next.SourceCommit = next.PackageVersion
	next.IdempotencyKey = "admit-same-digest-second"
	second, err := svc.AdmitArtifact(ctx, Principal{Actor: next.BuilderID}, next)
	if err != nil {
		t.Fatalf("admit second deployment artifact with same digest: %v", err)
	}
	if second.ArtifactID == first.ArtifactID {
		t.Fatalf("same-digest deployment versions reused artifact id %s", second.ArtifactID)
	}
	secondTarget, err := svc.PromoteTarget(ctx, Principal{Actor: "deployment-service"}, PromoteTargetRequest{
		PackageName:    next.PackageName,
		PackageVersion: next.PackageVersion,
		ChannelName:    next.ChannelName,
		ArtifactDigest: next.OCIDigest,
		PlatformOS:     next.PlatformOS,
		PlatformArch:   next.PlatformArch,
		Flavor:         next.Flavor,
		PolicyRef:      next.PolicyRef,
		PromotedBy:     "deployment-service",
		Reason:         "deployment rehearsal",
		IdempotencyKey: "promote-same-digest-second",
	})
	if err != nil {
		t.Fatalf("promote second deployment artifact with same digest: %v", err)
	}
	if secondTarget.TargetID == firstTarget.TargetID {
		t.Fatalf("same-digest deployment versions reused target id %s", secondTarget.TargetID)
	}
	if secondTarget.PackageVersion != next.PackageVersion {
		t.Fatalf("second target package version = %s, want %s", secondTarget.PackageVersion, next.PackageVersion)
	}

	current, err := svc.ResolveTarget(ctx, Principal{Actor: "deployment-service"}, ResolveTargetRequest{
		PackageName:  next.PackageName,
		ChannelName:  next.ChannelName,
		PlatformOS:   next.PlatformOS,
		PlatformArch: next.PlatformArch,
		Flavor:       next.Flavor,
	})
	if err != nil {
		t.Fatalf("resolve current target: %v", err)
	}
	if current.TargetID != secondTarget.TargetID {
		t.Fatalf("current target = %s, want %s", current.TargetID, secondTarget.TargetID)
	}
}

func TestAdmissionRequiresCompleteTrustedEvidence(t *testing.T) {
	_, svc := newTestService(t)
	req := admitRequest("b")
	req.Evidence = req.Evidence[:2]

	_, err := svc.AdmitArtifact(context.Background(), Principal{Actor: "distribution-rehearsal"}, req)
	if !errors.Is(err, ErrMissingReferrers) {
		t.Fatalf("admit missing evidence error = %v, want %v", err, ErrMissingReferrers)
	}
}

func TestDeploymentAdmissionUsesDeploymentPolicy(t *testing.T) {
	ctx, svc := newTestService(t)
	req := deploymentAdmitRequest("c")
	svc.TrustedBuilders[req.BuilderID] = struct{}{}
	svc.ReleaseVerifier = failingReleaseVerifier{}

	artifact, err := svc.AdmitArtifact(ctx, Principal{Actor: req.BuilderID}, req)
	if err != nil {
		t.Fatalf("admit deployment artifact: %v", err)
	}
	if artifact.ChannelName != ChannelDeployment || artifact.PackageName != "analytics-service" {
		t.Fatalf("artifact identity = %s/%s", artifact.PackageName, artifact.ChannelName)
	}
	if artifact.Verification.Decision != DecisionAllowed {
		t.Fatalf("verification decision = %s", artifact.Verification.Decision)
	}
	if len(artifact.Verification.Evidence) != 1 || artifact.Verification.Evidence[0].EvidenceKind != EvidenceSLSA {
		t.Fatalf("deployment evidence = %#v", artifact.Verification.Evidence)
	}
}

func TestDeploymentAdmissionDeniesFailedVerifier(t *testing.T) {
	ctx, svc := newTestService(t)
	req := deploymentAdmitRequest("d")
	svc.TrustedBuilders[req.BuilderID] = struct{}{}
	svc.DeploymentVerifier = failingDeploymentVerifier{err: ErrDigestMismatch}

	_, err := svc.AdmitArtifact(ctx, Principal{Actor: req.BuilderID}, req)
	if !errors.Is(err, ErrAttestationFailed) {
		t.Fatalf("admit deployment artifact error = %v, want %v", err, ErrAttestationFailed)
	}
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("admit deployment artifact error = %v, want wrapped %v", err, ErrDigestMismatch)
	}
}

func TestDeploymentAdmissionRejectsAssertedSignerIdentity(t *testing.T) {
	ctx, svc := newTestService(t)
	req := deploymentAdmitRequest("c")
	req.SignerIdentity = req.BuilderID
	svc.TrustedBuilders[req.BuilderID] = struct{}{}
	svc.ReleaseVerifier = failingReleaseVerifier{}

	_, err := svc.AdmitArtifact(ctx, Principal{Actor: req.BuilderID}, req)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("admit deployment artifact with asserted signer error = %v, want %v", err, ErrInvalid)
	}
}

func TestReleaseAdmissionRejectsAssertedSigningKeyID(t *testing.T) {
	ctx, svc := newTestService(t)
	req := admitRequest("d")
	req.Evidence[0].SigningKeyID = sixtyFour("ab")

	_, err := svc.AdmitArtifact(ctx, Principal{Actor: req.BuilderID}, req)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("admit release artifact with asserted signing_key_id error = %v, want %v", err, ErrInvalid)
	}
}

func TestDeploymentEvidenceVerifierAcceptsSignedBundle(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	req := pushDeploymentSubject(t, ctx, store, deploymentAdmitRequest("e"))
	kp := newDeployKeypair(t)
	expected := attachSignedDeploymentBundle(t, ctx, store, req, kp, req.SourceCommit)

	evidence, err := verifyDeploymentEvidenceFromStore(ctx, store, deployRing(t, kp), req)
	if err != nil {
		t.Fatalf("verify deployment evidence: %v", err)
	}
	if len(evidence) != 1 || evidence[0] != expected {
		t.Fatalf("verified evidence = %#v, want %#v", evidence, []Evidence{expected})
	}
	if evidence[0].SigningKeyID != string(kp.keyID) {
		t.Fatalf("evidence signing key id = %q, want %q", evidence[0].SigningKeyID, kp.keyID)
	}
}

func TestDeploymentEvidenceVerifierRejectsKeyOutsideRing(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	req := pushDeploymentSubject(t, ctx, store, deploymentAdmitRequest("e"))
	signer := newDeployKeypair(t)
	attachSignedDeploymentBundle(t, ctx, store, req, signer, req.SourceCommit)

	stranger := newDeployKeypair(t)
	_, err := verifyDeploymentEvidenceFromStore(ctx, store, deployRing(t, stranger), req)
	if !errors.Is(err, bundle.ErrVerification) {
		t.Fatalf("verify with foreign ring error = %v, want %v", err, bundle.ErrVerification)
	}
}

func TestDeploymentEvidenceVerifierRequiresBundleReferrer(t *testing.T) {
	ctx := context.Background()
	req := deploymentAdmitRequest("e")
	kp := newDeployKeypair(t)

	t.Run("no referrers at all", func(t *testing.T) {
		_, err := verifyDeploymentEvidenceFromStore(ctx, memory.New(), deployRing(t, kp), req)
		if !errors.Is(err, ErrMissingReferrers) {
			t.Fatalf("verify with empty store error = %v, want %v", err, ErrMissingReferrers)
		}
	})

	t.Run("old-style unsigned in-toto referrer is not a bundle", func(t *testing.T) {
		store := memory.New()
		attachUnsignedInTotoReferrer(t, ctx, store, req)
		_, err := verifyDeploymentEvidenceFromStore(ctx, store, deployRing(t, kp), req)
		if !errors.Is(err, ErrMissingReferrers) {
			t.Fatalf("verify with unsigned in-toto referrer error = %v, want %v", err, ErrMissingReferrers)
		}
	})
}

func TestDeploymentEvidenceVerifierEnforcesPolicyOnSignedBundle(t *testing.T) {
	ctx := context.Background()
	kp := newDeployKeypair(t)

	cases := []struct {
		name    string
		mutate  func(*AdmitArtifactRequest)
		wantErr error
	}{
		{
			name:    "wrong site",
			mutate:  func(req *AdmitArtifactRequest) { req.Flavor = "prod" },
			wantErr: ErrSourcePolicyFailure,
		},
		{
			name:    "wrong oci repository",
			mutate:  func(req *AdmitArtifactRequest) { req.OCIRepository = "verself/other-service" },
			wantErr: ErrSourcePolicyFailure,
		},
		{
			name:    "builder mismatch",
			mutate:  func(req *AdmitArtifactRequest) { req.BuilderID = "spiffe://prod.verself.sh/svc/impostor" },
			wantErr: ErrUntrustedBuilder,
		},
		{
			name:    "missing source dependency",
			mutate:  func(req *AdmitArtifactRequest) { req.SourceCommit = "fedcba9876543210fedcba9876543210fedcba98" },
			wantErr: ErrSourcePolicyFailure,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := memory.New()
			signed := pushDeploymentSubject(t, ctx, store, deploymentAdmitRequest("e"))
			attachSignedDeploymentBundle(t, ctx, store, signed, kp, signed.SourceCommit)
			admit := signed
			tc.mutate(&admit)
			_, err := verifyDeploymentEvidenceFromStore(ctx, store, deployRing(t, kp), admit)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("verify error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDeploymentEvidenceVerifierSkipsNonMatchingBundles(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	req := pushDeploymentSubject(t, ctx, store, deploymentAdmitRequest("f"))
	kp := newDeployKeypair(t)
	// A validly signed bundle for a different source commit must not satisfy
	// admission, and must not block the matching bundle next to it.
	attachSignedDeploymentBundle(t, ctx, store, req, kp, "fedcba9876543210fedcba9876543210fedcba98")
	expected := attachSignedDeploymentBundle(t, ctx, store, req, kp, req.SourceCommit)

	evidence, err := verifyDeploymentEvidenceFromStore(ctx, store, deployRing(t, kp), req)
	if err != nil {
		t.Fatalf("verify deployment evidence: %v", err)
	}
	if len(evidence) != 1 || evidence[0] != expected {
		t.Fatalf("verified evidence = %#v, want %#v", evidence, []Evidence{expected})
	}
}

func newTestService(t *testing.T) (context.Context, *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	t.Setenv("VERSELF_PGTEST_ROOT", filepath.Join(os.TempDir(), "verself-pgtest-distribution-service"))
	db := pgtest.Acquire(ctx, t, pgtest.Template{
		Service:     ServiceName,
		Fingerprint: migrations.Fingerprint(),
		Migrate: func(ctx context.Context, dsn string) error {
			return migrations.UpDSN(ctx, ServiceName, dsn)
		},
	})
	pg, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("open distribution postgres: %v", err)
	}
	t.Cleanup(pg.Close)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	return ctx, &Service{
		Store: SQLStore{PG: pg},
		TrustedBuilders: map[string]struct{}{
			"spiffe://prod.verself.sh/svc/release-builder": {},
		},
		TrustedSigners: map[string]struct{}{
			"https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main": {},
		},
		ReleaseVerifier:    testReleaseVerifier{},
		DeploymentVerifier: acceptingDeploymentVerifier{},
		InstallationID:     "test",
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
	}
}

func deploymentAdmitRequest(digestFill string) AdmitArtifactRequest {
	req := admitRequest(digestFill)
	req.PackageName = "analytics-service"
	req.PackageVersion = req.SourceCommit
	req.ChannelName = ChannelDeployment
	req.Flavor = "gamma"
	req.OCIRepository = "verself/analytics-service"
	req.BuilderID = "spiffe://prod.verself.sh/svc/deployment-service"
	// Deployment-channel signer identity is derived from bundle verification;
	// asserting one is a contract violation.
	req.SignerIdentity = ""
	req.PolicyRef = PolicyDeploymentOCI
	req.SubmittedBy = "deployment-service"
	req.ReleaseAttestation = ReleaseAttestation{}
	req.Evidence = nil
	return req
}

// deployKeypair is a hermetic sign.Keypair over an in-process ECDSA P-256 key,
// mirroring the production Transit signer's contract: GetHint() returns
// bundle.DeriveKeyID(pub) so the bundle hint matches a verifier ring.
type deployKeypair struct {
	sv     sigsig.SignerVerifier
	pub    crypto.PublicKey
	pubPEM []byte
	keyID  bundle.KeyID
}

func newDeployKeypair(t *testing.T) *deployKeypair {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sv, err := sigsig.LoadSignerVerifier(priv, crypto.SHA256)
	if err != nil {
		t.Fatalf("load signer: %v", err)
	}
	id, err := bundle.DeriveKeyID(&priv.PublicKey)
	if err != nil {
		t.Fatalf("derive key id: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return &deployKeypair{sv: sv, pub: &priv.PublicKey, pubPEM: pubPEM, keyID: id}
}

func (k *deployKeypair) GetHashAlgorithm() protocommon.HashAlgorithm {
	return protocommon.HashAlgorithm_SHA2_256
}

func (k *deployKeypair) GetSigningAlgorithm() protocommon.PublicKeyDetails {
	return protocommon.PublicKeyDetails_PKIX_ECDSA_P256_SHA_256
}

func (k *deployKeypair) GetHint() []byte                { return []byte(k.keyID) }
func (k *deployKeypair) GetKeyAlgorithm() string        { return "ECDSA" }
func (k *deployKeypair) GetPublicKey() crypto.PublicKey { return k.pub }

func (k *deployKeypair) GetPublicKeyPem() (string, error) {
	return string(k.pubPEM), nil
}

func (k *deployKeypair) SignData(ctx context.Context, data []byte) ([]byte, []byte, error) {
	sig, err := k.sv.SignMessage(bytes.NewReader(data), sigopts.WithContext(ctx))
	if err != nil {
		return nil, nil, err
	}
	d := sha256.Sum256(data)
	return sig, d[:], nil
}

func deployRing(t *testing.T, kp *deployKeypair) *bundle.Ring {
	t.Helper()
	pk, err := bundle.ParsePublicKeyPEM(kp.pubPEM)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	ring, err := bundle.NewRing(pk)
	if err != nil {
		t.Fatalf("new ring: %v", err)
	}
	return ring
}

// pushDeploymentSubject materializes a real subject manifest in the store and
// points the request at it. bundle.Attach copies the referrer graph, which
// requires the subject node to exist, unlike the old direct PackManifest path.
func pushDeploymentSubject(t *testing.T, ctx context.Context, store *memory.Store, req AdmitArtifactRequest) AdmitArtifactRequest {
	t.Helper()
	layer, err := oras.PushBytes(ctx, store, "application/vnd.oci.image.layer.v1.tar", []byte("subject-"+req.PackageName+"-"+req.PackageVersion))
	if err != nil {
		t.Fatalf("push subject layer: %v", err)
	}
	manifest, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/vnd.verself.deployment.image", oras.PackManifestOptions{
		Layers: []v1.Descriptor{layer},
	})
	if err != nil {
		t.Fatalf("pack subject manifest: %v", err)
	}
	req.OCIDigest = manifest.Digest.String()
	req.OCIMediaType = manifest.MediaType
	req.OCISizeBytes = manifest.Size
	return req
}

func deploymentSubject(t *testing.T, req AdmitArtifactRequest) v1.Descriptor {
	t.Helper()
	subjectDigest, err := ocidigest.Parse(req.OCIDigest)
	if err != nil {
		t.Fatalf("parse subject digest: %v", err)
	}
	return v1.Descriptor{
		MediaType: req.OCIMediaType,
		Digest:    subjectDigest,
		Size:      req.OCISizeBytes,
	}
}

// attachSignedDeploymentBundle mints a REAL signed sigstore bundle for the
// request's provenance (with sourceCommit as the attested source input) and
// attaches it as an OCI referrer. It returns the Evidence admission must emit.
func attachSignedDeploymentBundle(t *testing.T, ctx context.Context, store *memory.Store, req AdmitArtifactRequest, kp *deployKeypair, sourceCommit string) Evidence {
	t.Helper()
	statement, err := predicate.NewProvenance(
		[]predicate.Subject{{
			Name:   req.PublicRegistryURL + "/" + req.OCIRepository + "@" + req.OCIDigest,
			SHA256: req.OCIDigest[len("sha256:"):],
		}},
		predicate.SLSAProvenance{
			BuildType: predicate.BazelOCIBuild,
			ExternalParameters: predicate.ExternalParameters{
				BazelTarget:     "//src/services/analytics-service:analytics_service_image",
				BazelPushTarget: "//src/services/analytics-service:push_analytics_service_image",
				OCIRepository:   req.OCIRepository,
				Site:            req.Flavor,
			},
			ResolvedDependencies: []predicate.ResolvedDependency{{
				URI:       "git+https://github.com/" + req.SourceRepository + "@" + sourceCommit,
				GitCommit: sourceCommit,
			}},
			BuilderID:    req.BuilderID,
			InvocationID: "test-deploy-run-" + sourceCommit,
		},
	)
	if err != nil {
		t.Fatalf("build provenance: %v", err)
	}
	envelope, err := bundle.Sign(ctx, statement, kp, bundle.SignOptions{})
	if err != nil {
		t.Fatalf("sign deployment bundle: %v", err)
	}
	referrer, err := bundle.Attach(ctx, store, deploymentSubject(t, req), envelope)
	if err != nil {
		t.Fatalf("attach deployment bundle: %v", err)
	}
	documentSum := sha256.Sum256(envelope.Bytes())
	return Evidence{
		EvidenceKind:      EvidenceSLSA,
		PredicateType:     PredicateSLSAProvenance,
		SubjectDigest:     req.OCIDigest,
		DocumentDigest:    "sha256:" + hex.EncodeToString(documentSum[:]),
		OCIReferrerDigest: referrer.Digest.String(),
		SigningKeyID:      string(kp.keyID),
	}
}

// attachUnsignedInTotoReferrer reproduces the pre-cutover unsigned evidence
// shape: a bare in-toto statement layer behind an in-toto artifactType. It is
// not a sigstore bundle and must be invisible to discovery.
func attachUnsignedInTotoReferrer(t *testing.T, ctx context.Context, store *memory.Store, req AdmitArtifactRequest) {
	t.Helper()
	const mediaTypeInToto = "application/vnd.in-toto+json"
	layer, err := oras.PushBytes(ctx, store, mediaTypeInToto, []byte(`{"_type":"https://in-toto.io/Statement/v1"}`))
	if err != nil {
		t.Fatalf("push unsigned statement layer: %v", err)
	}
	subject := deploymentSubject(t, req)
	if _, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, mediaTypeInToto, oras.PackManifestOptions{
		Subject: &subject,
		Layers:  []v1.Descriptor{layer},
	}); err != nil {
		t.Fatalf("pack unsigned referrer manifest: %v", err)
	}
}

func admitRequest(digestFill string) AdmitArtifactRequest {
	digest := "sha256:" + sixtyFour(digestFill)
	releasePublicBlob := []byte("test-tpm-release-public-" + digestFill)
	releasePublicBlobDigest := digestForBytes(releasePublicBlob)
	attestation := ReleaseAttestation{
		DistributionChallenge:      "challenge-" + digestFill,
		ArtifactDigest:             "sha256:" + sixtyFour("a"),
		ProvenanceDigest:           "sha256:" + sixtyFour("d"),
		SBOMDigest:                 "sha256:" + sixtyFour("e"),
		TPMReleasePublicName:       "000b" + sixtyFour("f"),
		TPMReleasePublicBlobDigest: releasePublicBlobDigest,
		PolicyID:                   "release-builder-v1",
		TPM: distributionreleaseattest.TPMEvidence{
			ReleasePublicName:       "000b" + sixtyFour("f"),
			ReleasePublicBlob:       releasePublicBlob,
			ReleasePublicBlobDigest: releasePublicBlobDigest,
		},
	}
	releaseInput := releaseinput.ReleaseInput{
		DistributionChallenge:      attestation.DistributionChallenge,
		Package:                    "mksk",
		Version:                    "0.1.0",
		SourceCommit:               "0123456789abcdef0123456789abcdef01234567",
		Platform:                   "linux/amd64",
		Flavor:                     "default",
		OCIManifestDigest:          digest,
		ArtifactDigest:             attestation.ArtifactDigest,
		ProvenanceDigest:           attestation.ProvenanceDigest,
		SBOMDigest:                 attestation.SBOMDigest,
		TPMReleasePublicName:       attestation.TPMReleasePublicName,
		TPMReleasePublicBlobDigest: attestation.TPMReleasePublicBlobDigest,
	}
	releaseInputDigest, err := releaseInput.Digest()
	if err != nil {
		panic(err)
	}
	attestation.ReleaseInputDigest = releaseInputDigest
	return AdmitArtifactRequest{
		PackageName:       "mksk",
		PackageVersion:    "0.1.0",
		ChannelName:       "stable",
		PlatformOS:        "linux",
		PlatformArch:      "amd64",
		Flavor:            "default",
		OriginRegistryURL: "https://zot.service.consul",
		PublicRegistryURL: "https://oci.verself.sh",
		OCIRepository:     "verself/mksk",
		OCIDigest:         digest,
		OCIMediaType:      "application/vnd.oci.image.manifest.v1+json",
		OCISizeBytes:      4096,
		BuilderID:         "spiffe://prod.verself.sh/svc/release-builder",
		SignerIdentity:    "https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main",
		SourceRepository:  "guardian-intelligence/verself",
		SourceCommit:      "0123456789abcdef0123456789abcdef01234567",
		SourceRef:         "refs/heads/main",
		PolicyRef:         "distribution-policies/mksk/v1",
		Evidence: []Evidence{
			evidence(EvidenceCosign, "https://sigstore.dev/cosign/v1", digest, "c"),
			evidence(EvidenceSLSA, PredicateSLSAProvenance, digest, "d"),
			evidence(EvidenceSBOM, "https://spdx.dev/Document", digest, "e"),
			evidence(EvidenceTest, "https://verself.sh/test-evidence/v1", digest, "f"),
		},
		ReleaseAttestation: attestation,
		SubmittedBy:        "distribution-rehearsal",
		IdempotencyKey:     "admit-" + digestFill,
	}
}

func evidence(kind string, predicate string, subjectDigest string, referrerFill string) Evidence {
	return Evidence{
		EvidenceKind:      kind,
		PredicateType:     predicate,
		SubjectDigest:     subjectDigest,
		DocumentDigest:    "sha256:" + sixtyFour(referrerFill),
		OCIReferrerDigest: "sha256:" + sixtyFour(referrerFill),
	}
}

func digestForBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sixtyFour(value string) string {
	out := ""
	for len(out) < 64 {
		out += value
	}
	return out[:64]
}

type testReleaseVerifier struct{}

func (testReleaseVerifier) Verify(_ context.Context, req distributionreleaseattest.Request) (distributionreleaseattest.Result, error) {
	digest, err := req.Input.Digest()
	if err != nil {
		return distributionreleaseattest.Result{}, err
	}
	if req.ReleaseInputDigest != digest {
		return distributionreleaseattest.Result{}, ErrDigestMismatch
	}
	return distributionreleaseattest.Result{
		ReleaseInputDigest:     digest,
		PCRPolicyID:            req.PolicyID,
		TPMReleasePublicName:   req.TPM.ReleasePublicName,
		TPMReleasePublicDigest: req.TPM.ReleasePublicBlobDigest,
		CheckedAt:              req.CheckedAt,
	}, nil
}

type failingReleaseVerifier struct{}

func (failingReleaseVerifier) Verify(context.Context, distributionreleaseattest.Request) (distributionreleaseattest.Result, error) {
	return distributionreleaseattest.Result{}, errors.New("release verifier should not run")
}

type acceptingDeploymentVerifier struct{}

func (acceptingDeploymentVerifier) VerifyDeploymentEvidence(_ context.Context, req AdmitArtifactRequest) ([]Evidence, error) {
	return []Evidence{evidence(EvidenceSLSA, PredicateSLSAProvenance, req.OCIDigest, "d")}, nil
}

type failingDeploymentVerifier struct {
	err error
}

func (v failingDeploymentVerifier) VerifyDeploymentEvidence(context.Context, AdmitArtifactRequest) ([]Evidence, error) {
	return nil, v.err
}
