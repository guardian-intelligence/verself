package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	ocidigest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"

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

func TestDeploymentEvidenceVerifierAcceptsOCIReferrerStatement(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	req := deploymentAdmitRequest("e")
	req, expected := attachDeploymentSLSAReferrer(t, ctx, store, req)

	evidence, err := verifyDeploymentEvidenceFromStore(ctx, store, req)
	if err != nil {
		t.Fatalf("verify deployment evidence: %v", err)
	}
	if len(evidence) != 1 || evidence[0] != expected {
		t.Fatalf("verified evidence = %#v, want %#v", evidence, []Evidence{expected})
	}

	badSource := req
	badSource.SourceCommit = "fedcba9876543210fedcba9876543210fedcba98"
	_, err = verifyDeploymentEvidenceFromStore(ctx, store, badSource)
	if !errors.Is(err, ErrSourcePolicyFailure) {
		t.Fatalf("verify deployment evidence with wrong source error = %v, want %v", err, ErrSourcePolicyFailure)
	}
}

func TestDeploymentEvidenceVerifierSkipsNonMatchingReferrers(t *testing.T) {
	ctx := context.Background()
	req := deploymentAdmitRequest("f")
	store, req, expected := storeWithStaleDeploymentSLSAReferrerFirst(t, ctx, req)

	evidence, err := verifyDeploymentEvidenceFromStore(ctx, store, req)
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
	req.SignerIdentity = req.BuilderID
	req.PolicyRef = PolicyDeploymentOCI
	req.SubmittedBy = "deployment-service"
	req.ReleaseAttestation = ReleaseAttestation{}
	req.Evidence = nil
	return req
}

func attachDeploymentSLSAReferrer(t *testing.T, ctx context.Context, store *memory.Store, req AdmitArtifactRequest) (AdmitArtifactRequest, Evidence) {
	return attachDeploymentSLSAReferrerWithInvocation(t, ctx, store, req, "test-deploy-run")
}

func attachDeploymentSLSAReferrerWithInvocation(t *testing.T, ctx context.Context, store *memory.Store, req AdmitArtifactRequest, invocationID string) (AdmitArtifactRequest, Evidence) {
	t.Helper()
	statement := deploymentStatementBody(t, req, invocationID)
	layer, err := oras.PushBytes(ctx, store, mediaTypeInTotoStatement, statement)
	if err != nil {
		t.Fatalf("push SLSA statement layer: %v", err)
	}
	layer.Annotations = map[string]string{
		v1.AnnotationTitle: "deployment.slsa.intoto.json",
	}
	subjectDigest, err := ocidigest.Parse(req.OCIDigest)
	if err != nil {
		t.Fatalf("parse subject digest: %v", err)
	}
	subject := v1.Descriptor{
		MediaType: req.OCIMediaType,
		Digest:    subjectDigest,
		Size:      req.OCISizeBytes,
	}
	referrer, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, mediaTypeInTotoStatement, oras.PackManifestOptions{
		Subject: &subject,
		Layers:  []v1.Descriptor{layer},
		ManifestAnnotations: map[string]string{
			v1.AnnotationTitle:                "deployment.slsa.intoto.json",
			"sh.verself.deploy.evidence.kind": EvidenceSLSA,
		},
	})
	if err != nil {
		t.Fatalf("pack SLSA referrer manifest: %v", err)
	}
	expected := Evidence{
		EvidenceKind:      EvidenceSLSA,
		PredicateType:     PredicateSLSAProvenance,
		SubjectDigest:     req.OCIDigest,
		DocumentDigest:    layer.Digest.String(),
		OCIReferrerDigest: referrer.Digest.String(),
	}
	return req, expected
}

func storeWithStaleDeploymentSLSAReferrerFirst(t *testing.T, ctx context.Context, req AdmitArtifactRequest) (*memory.Store, AdmitArtifactRequest, Evidence) {
	t.Helper()
	stale := req
	stale.SourceCommit = "fedcba9876543210fedcba9876543210fedcba98"
	for i := 0; i < 256; i++ {
		store := memory.New()
		_, staleEvidence := attachDeploymentSLSAReferrerWithInvocation(t, ctx, store, stale, fmt.Sprintf("stale-deploy-run-%d", i))
		req, expected := attachDeploymentSLSAReferrerWithInvocation(t, ctx, store, req, fmt.Sprintf("test-deploy-run-%d", i))
		if staleEvidence.OCIReferrerDigest < expected.OCIReferrerDigest {
			return store, req, expected
		}
	}
	t.Fatal("could not generate stale referrer that sorts before matching referrer")
	return nil, AdmitArtifactRequest{}, Evidence{}
}

func deploymentStatementBody(t *testing.T, req AdmitArtifactRequest, invocationID string) []byte {
	t.Helper()
	statement := map[string]any{
		"_type": inTotoStatementV1,
		"subject": []map[string]any{
			{
				"name": req.PublicRegistryURL + "/" + req.OCIRepository + "@" + req.OCIDigest,
				"digest": map[string]string{
					"sha256": req.OCIDigest[len("sha256:"):],
				},
			},
		},
		"predicateType": PredicateSLSAProvenance,
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType": deploymentRulesOCIBuildType,
				"externalParameters": map[string]any{
					"bazelTarget":     "//src/services/analytics-service:analytics_service_image",
					"bazelPushTarget": "//src/services/analytics-service:push_analytics_service_image",
					"ociRepository":   req.OCIRepository,
					"site":            req.Flavor,
				},
				"resolvedDependencies": []map[string]any{
					{
						"uri": "git+https://github.com/" + req.SourceRepository + "@" + req.SourceCommit,
						"digest": map[string]string{
							"gitCommit": req.SourceCommit,
						},
					},
				},
			},
			"runDetails": map[string]any{
				"builder": map[string]any{
					"id": req.BuilderID,
				},
				"metadata": map[string]any{
					"invocationId": invocationID,
				},
			},
		},
	}
	body, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal SLSA statement: %v", err)
	}
	return body
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
