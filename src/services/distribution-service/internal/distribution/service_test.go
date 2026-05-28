package distribution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/verself/distribution-service/migrations"
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

func TestAdmissionRequiresCompleteTrustedEvidence(t *testing.T) {
	_, svc := newTestService(t)
	req := admitRequest("b")
	req.Evidence = req.Evidence[:2]

	_, err := svc.AdmitArtifact(context.Background(), Principal{Actor: "distribution-rehearsal"}, req)
	if !errors.Is(err, ErrMissingReferrers) {
		t.Fatalf("admit missing evidence error = %v, want %v", err, ErrMissingReferrers)
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
		InstallationID: "test",
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
	}
}

func admitRequest(digestFill string) AdmitArtifactRequest {
	digest := "sha256:" + sixtyFour(digestFill)
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
		SubmittedBy:    "distribution-rehearsal",
		IdempotencyKey: "admit-" + digestFill,
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

func sixtyFour(value string) string {
	out := ""
	for len(out) < 64 {
		out += value
	}
	return out[:64]
}
