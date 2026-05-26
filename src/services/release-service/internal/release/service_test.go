package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/verself/release-service/migrations"
	"github.com/verself/service-runtime/pgtest"
)

func TestMakeSkillPublicationFlow(t *testing.T) {
	ctx, svc := newTestService(t)

	pkg, err := svc.RegisterPackage(ctx, Principal{Actor: "test"}, RegisterPackageRequest{
		OrgID:                "371564185181576922",
		PackageName:          "make-skill",
		PackageKind:          PackageKindCLI,
		RepoPath:             "src/make-skill",
		BazelTarget:          "//src/make-skill:mksk",
		OwnerTeam:            "platform",
		DefaultVersionScheme: VersionSchemeSemver,
		Visibility:           VisibilityInternal,
		PolicyRef:            "release-policies/make-skill/v1",
	})
	if err != nil {
		t.Fatalf("register package: %v", err)
	}

	version, err := svc.CreateVersion(ctx, Principal{Actor: "test"}, CreateVersionRequest{
		PackageID:        pkg.PackageID,
		CanonicalVersion: "0.1.0",
		VersionScheme:    VersionSchemeSemver,
		VersionKind:      VersionKindStable,
		ReleaseSequence:  1,
		SourceRepository: "guardian-intelligence/verself",
		SourceCommit:     "0123456789abcdef0123456789abcdef01234567",
		SourceRef:        "refs/heads/main",
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}

	options, err := svc.ListInstallOptions(ctx, Principal{Actor: "test"}, version.ReleaseVersionID)
	if err != nil {
		t.Fatalf("list initial install options: %v", err)
	}
	assertInstallOption(t, options, "source", "active")
	assertInstallOption(t, options, "third_party", "deferred")

	artifactDigest := "sha256:" + sixtyFour("a")
	artifact, err := svc.AttachArtifact(ctx, Principal{Actor: "test"}, AttachArtifactRequest{
		ReleaseVersionID: version.ReleaseVersionID,
		ArtifactRole:     ArtifactRoleArchive,
		PlatformOS:       "linux",
		PlatformArch:     "amd64",
		OCIRepository:    "verself/make-skill",
		OCIDigest:        artifactDigest,
		OCIMediaType:     "application/vnd.oci.image.manifest.v1+json",
		OCISizeBytes:     4096,
		RegistryURL:      "https://oci.verself.sh",
	})
	if err != nil {
		t.Fatalf("attach artifact: %v", err)
	}

	denied, err := svc.Verify(ctx, Principal{Actor: "test"}, verifyRequest(version.ReleaseVersionID))
	if err != nil {
		t.Fatalf("verify missing evidence: %v", err)
	}
	if denied.Decision != "denied" || denied.Version.State != StateDenied {
		t.Fatalf("missing-evidence decision = %s/%s, want denied/%s", denied.Decision, denied.Version.State, StateDenied)
	}

	for _, evidenceKind := range []string{EvidenceCosign, EvidenceSLSA, EvidenceSBOM} {
		if _, err := svc.AttachEvidence(ctx, Principal{Actor: "test"}, AttachEvidenceRequest{
			ReleaseVersionID:  version.ReleaseVersionID,
			ArtifactID:        artifact.ArtifactID,
			EvidenceKind:      evidenceKind,
			PredicateType:     "https://slsa.dev/provenance/v1",
			SubjectDigest:     artifactDigest,
			DocumentDigest:    "sha256:" + sixtyFour("b"),
			OCIReferrerDigest: "sha256:" + sixtyFour("c"),
		}); err != nil {
			t.Fatalf("attach %s evidence: %v", evidenceKind, err)
		}
	}

	verified, err := svc.Verify(ctx, Principal{Actor: "test"}, verifyRequest(version.ReleaseVersionID))
	if err != nil {
		t.Fatalf("verify complete evidence: %v", err)
	}
	if verified.Decision != "allowed" || verified.Version.State != StateVerified {
		t.Fatalf("complete-evidence decision = %s/%s, want allowed/%s", verified.Decision, verified.Version.State, StateVerified)
	}

	approved, err := svc.Approve(ctx, Principal{Actor: "test"}, ApproveRequest{
		ReleaseVersionID: version.ReleaseVersionID,
		Reason:           "release rehearsal",
	})
	if err != nil {
		t.Fatalf("approve release: %v", err)
	}
	if approved.State != StateApproved {
		t.Fatalf("approved state = %s, want %s", approved.State, StateApproved)
	}

	publication, err := svc.CreatePublication(ctx, Principal{Actor: "test"}, CreatePublicationRequest{
		ReleaseVersionID: version.ReleaseVersionID,
		ProviderKind:     ProviderOCI,
		ProviderPackage:  "verself/make-skill",
		ProviderChannel:  "stable",
		IdempotencyKey:   "test-oci-publication",
	})
	if err != nil {
		t.Fatalf("create publication: %v", err)
	}
	duplicatePublication, err := svc.CreatePublication(ctx, Principal{Actor: "test"}, CreatePublicationRequest{
		ReleaseVersionID: version.ReleaseVersionID,
		ProviderKind:     ProviderOCI,
		ProviderPackage:  "verself/make-skill",
		ProviderChannel:  "stable",
		IdempotencyKey:   "test-oci-publication",
	})
	if err != nil {
		t.Fatalf("create duplicate publication: %v", err)
	}
	if duplicatePublication.PublicationID != publication.PublicationID {
		t.Fatalf("duplicate publication id = %s, want %s", duplicatePublication.PublicationID, publication.PublicationID)
	}

	svc.Registry = nil
	if _, err := svc.DispatchPublication(ctx, Principal{Actor: "test"}, publication.PublicationID); !errors.Is(err, ErrRegistry) {
		t.Fatalf("dispatch without registry error = %v, want %v", err, ErrRegistry)
	}

	svc.Registry = failingRegistry{}
	failedPublication, err := svc.DispatchPublication(ctx, Principal{Actor: "test"}, publication.PublicationID)
	if err == nil {
		t.Fatalf("dispatch with missing manifest succeeded")
	}
	if failedPublication.State != StateRetryableFailed {
		t.Fatalf("failed publication state = %s, want %s", failedPublication.State, StateRetryableFailed)
	}

	svc.Registry = passingRegistry{}
	publication, err = svc.DispatchPublication(ctx, Principal{Actor: "test"}, publication.PublicationID)
	if err != nil {
		t.Fatalf("dispatch publication: %v", err)
	}
	if publication.State != StateComplete {
		t.Fatalf("publication state = %s, want %s", publication.State, StateComplete)
	}

	options, err = svc.ListInstallOptions(ctx, Principal{Actor: "test"}, version.ReleaseVersionID)
	if err != nil {
		t.Fatalf("list published install options: %v", err)
	}
	assertInstallOption(t, options, "oci", "active")
	assertInstallOption(t, options, "third_party", "deferred")
	ociOption := installOption(t, options, "oci")
	if strings.Contains(ociOption.CommandTemplate, "https://") {
		t.Fatalf("oci install command includes URL scheme: %s", ociOption.CommandTemplate)
	}
	if !strings.Contains(ociOption.CommandTemplate, "oci.verself.sh/verself/make-skill@"+artifactDigest) {
		t.Fatalf("oci install command = %s, want registry reference with digest %s", ociOption.CommandTemplate, artifactDigest)
	}

	channel, err := svc.PromoteChannel(ctx, Principal{Actor: "test"}, PromoteChannelRequest{
		PackageID:        pkg.PackageID,
		ChannelName:      "stable",
		ReleaseVersionID: version.ReleaseVersionID,
		ChannelKind:      VersionKindStable,
		Visibility:       VisibilityInternal,
		PolicyRef:        "release-policies/make-skill/v1",
		Reason:           "release rehearsal",
	})
	if err != nil {
		t.Fatalf("promote channel: %v", err)
	}
	if channel.CurrentReleaseVersionID != version.ReleaseVersionID {
		t.Fatalf("channel version = %s, want %s", channel.CurrentReleaseVersionID, version.ReleaseVersionID)
	}
}

func TestEnsureMakeSkillSeedIsIdempotent(t *testing.T) {
	ctx, svc := newTestService(t)

	for range 2 {
		if err := svc.EnsureMakeSkillSeed(ctx, "371564185181576922", "0123456789abcdef0123456789abcdef01234567"); err != nil {
			t.Fatalf("seed make-skill: %v", err)
		}
	}
	pkg, err := svc.Store.GetPackageByName(ctx, "371564185181576922", "make-skill")
	if err != nil {
		t.Fatalf("load seeded package: %v", err)
	}
	version, err := svc.CreateVersion(ctx, Principal{Actor: "test"}, CreateVersionRequest{
		PackageID:        pkg.PackageID,
		CanonicalVersion: "0.1.0",
		VersionScheme:    VersionSchemeSemver,
		VersionKind:      VersionKindStable,
		ReleaseSequence:  1,
		SourceRepository: "guardian-intelligence/verself",
		SourceCommit:     "0123456789abcdef0123456789abcdef01234567",
		SourceRef:        "src/make-skill",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate seeded version error = %v, want %v", err, ErrConflict)
	}
	if version.ReleaseVersionID != uuid.Nil {
		t.Fatalf("duplicate version returned id %s", version.ReleaseVersionID)
	}
}

func newTestService(t *testing.T) (context.Context, *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	t.Setenv("VERSELF_PGTEST_ROOT", filepath.Join(os.TempDir(), "verself-pgtest-release-service"))
	db := pgtest.Acquire(ctx, t, pgtest.Template{
		Service:     "release-service",
		Fingerprint: migrations.Fingerprint(),
		Migrate: func(ctx context.Context, dsn string) error {
			return migrations.UpDSN(ctx, "release-service", dsn)
		},
	})
	pg, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("open release postgres: %v", err)
	}
	t.Cleanup(pg.Close)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	return ctx, &Service{
		Store:    SQLStore{PG: pg},
		Registry: passingRegistry{},
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
	}
}

func verifyRequest(releaseVersionID uuid.UUID) VerifyRequest {
	return VerifyRequest{
		ReleaseVersionID: releaseVersionID,
		PolicyRef:        "release-policies/make-skill/v1",
		PolicyDigest:     "sha256:" + sixtyFour("d"),
		BuilderID:        "spiffe://prod.verself.sh/svc/release-builder",
		SignerIdentity:   "https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main",
		CheckedBy:        "test",
	}
}

func assertInstallOption(t *testing.T, options []InstallOption, kind string, status string) {
	t.Helper()
	option := installOption(t, options, kind)
	if option.Status != status {
		t.Fatalf("%s option status = %s, want %s", kind, option.Status, status)
	}
	if option.CommandTemplate == "" || option.VerificationTemplate == "" {
		t.Fatalf("%s option is missing command or verification template: %#v", kind, option)
	}
}

func installOption(t *testing.T, options []InstallOption, kind string) InstallOption {
	t.Helper()
	for _, option := range options {
		if option.OptionKind == kind {
			return option
		}
	}
	t.Fatalf("missing %s install option in %#v", kind, options)
	return InstallOption{}
}

func sixtyFour(value string) string {
	out := ""
	for len(out) < 64 {
		out += value
	}
	return out[:64]
}

type failingRegistry struct{}

func (failingRegistry) ManifestExists(context.Context, string, string, string) error {
	return errors.New("registry unavailable")
}

type passingRegistry struct{}

func (passingRegistry) ManifestExists(context.Context, string, string, string) error {
	return nil
}
