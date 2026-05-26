package release

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	serviceTracer = otel.Tracer("release-service/release")
	digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type Service struct {
	Store        SQLStore
	Runtime      *Runtime
	Distribution Distribution
	CH           driver.Conn
	Logger       *slog.Logger
	Now          func() time.Time
}

func (s *Service) Ready(ctx context.Context) error {
	if err := s.Store.Ready(ctx); err != nil {
		return err
	}
	if s.CH != nil {
		var one uint8
		if err := s.CH.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
			return fmt.Errorf("%w: clickhouse readiness: %v", ErrStoreUnavailable, err)
		}
	}
	return nil
}

func (s *Service) SetRuntime(runtime *Runtime) {
	s.Runtime = runtime
}

func (s *Service) RegisterPackage(ctx context.Context, principal Principal, req RegisterPackageRequest) (pkg Package, err error) {
	ctx, span := startSpan(ctx, "release.package.register", principal, attribute.String("release.package_name", req.PackageName))
	defer finishSpan(span, err)
	req = normalizeRegisterPackage(req)
	if err := validateRegisterPackage(req); err != nil {
		return Package{}, err
	}
	pkg, err = s.Store.CreatePackage(ctx, req, s.now())
	if err == nil {
		row := releaseDecisionEvent{EventType: "package_registered", Actor: principal.Actor, Decision: "recorded", Reason: "package metadata registered"}
		row.addPackage(pkg)
		s.emitReleaseEvent(ctx, row)
	}
	return pkg, err
}

func (s *Service) CreateVersion(ctx context.Context, principal Principal, req CreateVersionRequest) (version Version, err error) {
	ctx, span := startSpan(ctx, "release.version.create", principal, attribute.String("release.package_id", req.PackageID.String()), attribute.String("release.version", req.CanonicalVersion))
	defer finishSpan(span, err)
	req = normalizeCreateVersion(req)
	if err := validateCreateVersion(req); err != nil {
		return Version{}, err
	}
	version, err = s.Store.CreateVersion(ctx, req, s.now())
	if err == nil {
		row := releaseDecisionEvent{EventType: "version_created", Actor: principal.Actor, Decision: "recorded", Reason: "release version created"}
		if releaseCtx, ok := s.releaseContextForEvent(ctx, version.ReleaseVersionID); ok {
			row.addReleaseContext(releaseCtx)
		} else {
			row.addVersion(version)
		}
		s.emitReleaseEvent(ctx, row)
	}
	return version, err
}

func (s *Service) GetRelease(ctx context.Context, principal Principal, releaseVersionID uuid.UUID) (release ReleaseContext, err error) {
	ctx, span := startSpan(ctx, "release.version.get", principal, attribute.String("release.version_id", releaseVersionID.String()))
	defer finishSpan(span, err)
	if releaseVersionID == uuid.Nil {
		return ReleaseContext{}, fmt.Errorf("%w: release_version_id is required", ErrInvalid)
	}
	return s.Store.GetRelease(ctx, releaseVersionID)
}

func (s *Service) AttachArtifact(ctx context.Context, principal Principal, req AttachArtifactRequest) (artifact Artifact, err error) {
	ctx, span := startSpan(ctx, "release.artifact.attach", principal, attribute.String("release.version_id", req.ReleaseVersionID.String()), attribute.String("release.oci_digest", req.OCIDigest))
	defer finishSpan(span, err)
	req = normalizeAttachArtifact(req)
	if err := validateAttachArtifact(req); err != nil {
		return Artifact{}, err
	}
	artifact, err = s.Store.AttachArtifact(ctx, req, s.now())
	if err == nil {
		row := releaseDecisionEvent{EventType: "artifact_attached", Actor: principal.Actor, Decision: "recorded", Reason: "release artifact attached"}
		if releaseCtx, ok := s.releaseContextForEvent(ctx, artifact.ReleaseVersionID); ok {
			row.addReleaseContext(releaseCtx)
		}
		row.addArtifact(artifact)
		s.emitReleaseEvent(ctx, row)
	}
	return artifact, err
}

func (s *Service) AttachEvidence(ctx context.Context, principal Principal, req AttachEvidenceRequest) (evidence Evidence, err error) {
	ctx, span := startSpan(ctx, "release.evidence.attach", principal, attribute.String("release.version_id", req.ReleaseVersionID.String()), attribute.String("release.evidence_kind", req.EvidenceKind))
	defer finishSpan(span, err)
	req = normalizeAttachEvidence(req)
	if err := validateAttachEvidence(req); err != nil {
		return Evidence{}, err
	}
	evidence, err = s.Store.AttachEvidence(ctx, req, s.now())
	if err == nil {
		row := releaseDecisionEvent{EventType: "evidence_attached", Actor: principal.Actor, ArtifactID: evidence.ArtifactID, ArtifactDigest: evidence.SubjectDigest, Decision: evidence.VerificationState, Reason: evidence.EvidenceKind + " evidence attached"}
		if releaseCtx, ok := s.releaseContextForEvent(ctx, evidence.ReleaseVersionID); ok {
			row.addReleaseContext(releaseCtx)
		} else {
			row.ReleaseVersionID = evidence.ReleaseVersionID
		}
		row.addAttributes(map[string]string{"evidence_kind": evidence.EvidenceKind, "predicate_type": evidence.PredicateType, "document_digest": evidence.DocumentDigest, "oci_referrer_digest": evidence.OCIReferrerDigest})
		s.emitReleaseEvent(ctx, row)
	}
	return evidence, err
}

func (s *Service) Verify(ctx context.Context, principal Principal, req VerifyRequest) (result VerifyResult, err error) {
	ctx, span := startSpan(ctx, "release.version.verify", principal, attribute.String("release.version_id", req.ReleaseVersionID.String()))
	defer finishSpan(span, err)
	req = normalizeVerify(req)
	if req.CheckedBy == "" {
		req.CheckedBy = principal.Actor
	}
	if err := validateVerify(req); err != nil {
		return VerifyResult{}, err
	}
	result, err = s.Store.Verify(ctx, req, s.now())
	if err == nil {
		span.SetAttributes(attribute.String("release.policy_decision", result.Decision))
		row := releaseDecisionEvent{EventType: "verification_checked", Actor: req.CheckedBy, Decision: result.Decision, Reason: result.Reason, PolicyRef: req.PolicyRef, PolicyDigest: req.PolicyDigest, BuilderID: req.BuilderID, SignerIdentity: req.SignerIdentity}
		if releaseCtx, ok := s.releaseContextForEvent(ctx, result.Version.ReleaseVersionID); ok {
			row.addReleaseContext(releaseCtx)
		} else {
			row.addVersion(result.Version)
		}
		s.emitReleaseEvent(ctx, row)
	}
	return result, err
}

func (s *Service) Approve(ctx context.Context, principal Principal, req ApproveRequest) (version Version, err error) {
	ctx, span := startSpan(ctx, "release.version.approve", principal, attribute.String("release.version_id", req.ReleaseVersionID.String()))
	defer finishSpan(span, err)
	req.ActorID = clean(req.ActorID)
	if req.ActorID == "" {
		req.ActorID = principal.Actor
	}
	req.Reason = clean(req.Reason)
	if req.ReleaseVersionID == uuid.Nil || req.ActorID == "" || req.Reason == "" {
		return Version{}, fmt.Errorf("%w: release_version_id, actor_id, and reason are required", ErrInvalid)
	}
	version, err = s.Store.Approve(ctx, req, s.now())
	if err == nil {
		row := releaseDecisionEvent{EventType: "release_approved", Actor: req.ActorID, Decision: "approved", Reason: req.Reason}
		if releaseCtx, ok := s.releaseContextForEvent(ctx, version.ReleaseVersionID); ok {
			row.addReleaseContext(releaseCtx)
		} else {
			row.addVersion(version)
		}
		s.emitReleaseEvent(ctx, row)
	}
	return version, err
}

func (s *Service) PromoteChannel(ctx context.Context, principal Principal, req PromoteChannelRequest) (channel Channel, err error) {
	ctx, span := startSpan(ctx, "release.channel.promote", principal, attribute.String("release.package_id", req.PackageID.String()), attribute.String("release.channel", req.ChannelName))
	defer finishSpan(span, err)
	req = normalizePromote(req)
	if req.ActorID == "" {
		req.ActorID = principal.Actor
	}
	if err := validatePromote(req); err != nil {
		return Channel{}, err
	}
	channel, err = s.Store.PromoteChannel(ctx, req, s.now())
	if err == nil {
		row := releaseDecisionEvent{EventType: "channel_promoted", Actor: req.ActorID, Decision: "promoted", Reason: req.Reason, PolicyRef: req.PolicyRef}
		if releaseCtx, ok := s.releaseContextForEvent(ctx, req.ReleaseVersionID); ok {
			row.addReleaseContext(releaseCtx)
		} else {
			row.PackageID = req.PackageID
			row.ReleaseVersionID = req.ReleaseVersionID
		}
		row.addAttributes(map[string]string{"channel_name": channel.ChannelName, "channel_kind": channel.ChannelKind, "visibility": channel.Visibility})
		s.emitReleaseEvent(ctx, row)
	}
	return channel, err
}

func (s *Service) ListInstallOptions(ctx context.Context, principal Principal, releaseVersionID uuid.UUID) (options []InstallOption, err error) {
	ctx, span := startSpan(ctx, "release.install_options.list", principal, attribute.String("release.version_id", releaseVersionID.String()))
	defer finishSpan(span, err)
	if releaseVersionID == uuid.Nil {
		return nil, fmt.Errorf("%w: release_version_id is required", ErrInvalid)
	}
	return s.Store.ListInstallOptions(ctx, releaseVersionID)
}

func (s *Service) CreatePublication(ctx context.Context, principal Principal, req CreatePublicationRequest) (publication Publication, err error) {
	ctx, span := startSpan(ctx, "release.publication.create", principal, attribute.String("release.version_id", req.ReleaseVersionID.String()), attribute.String("release.provider_kind", req.ProviderKind))
	defer finishSpan(span, err)
	req = normalizeCreatePublication(req)
	if req.RequestedBy == "" {
		req.RequestedBy = principal.Actor
	}
	if err := validateCreatePublication(req); err != nil {
		return Publication{}, err
	}
	pub, err := s.Store.CreatePublication(ctx, req, s.now())
	if err != nil {
		return Publication{}, err
	}
	row := releaseDecisionEvent{EventType: "publication_created", Actor: req.RequestedBy, Decision: "queued", Reason: "release publication requested"}
	row.addPublication(pub)
	if releaseCtx, ok := s.releaseContextForEvent(ctx, pub.ReleaseVersionID); ok {
		row.addReleaseContext(releaseCtx)
	}
	s.emitReleaseEvent(ctx, row)
	if s.Runtime != nil && req.ProviderKind == ProviderOCI {
		if err := s.Runtime.EnqueuePublicationDispatch(ctx, pub.PublicationID.String(), ""); err != nil {
			return Publication{}, err
		}
	}
	return pub, nil
}

func (s *Service) DispatchPublication(ctx context.Context, principal Principal, publicationID uuid.UUID) (publication Publication, err error) {
	ctx, span := startSpan(ctx, "release.publication.dispatch", principal, attribute.String("release.publication_id", publicationID.String()))
	defer finishSpan(span, err)
	if publicationID == uuid.Nil {
		return Publication{}, fmt.Errorf("%w: publication_id is required", ErrInvalid)
	}
	publication, err = s.Store.DispatchPublication(ctx, publicationID, s.Distribution, principal.Actor, s.now())
	if err == nil || publication.PublicationID != uuid.Nil {
		row := releaseDecisionEvent{EventType: "publication_dispatched", Actor: principal.Actor, Decision: publication.State, Reason: "release publication dispatched"}
		if err != nil {
			row.Reason = err.Error()
		}
		row.addPublication(publication)
		if releaseCtx, ok := s.releaseContextForEvent(ctx, publication.ReleaseVersionID); ok {
			row.addReleaseContext(releaseCtx)
		}
		s.emitReleaseEvent(ctx, row)
	}
	return publication, err
}

func (s *Service) RecordReceipt(ctx context.Context, principal Principal, req RecordReceiptRequest) (publication Publication, err error) {
	ctx, span := startSpan(ctx, "release.publication.receipt.record", principal, attribute.String("release.publication_id", req.PublicationID.String()))
	defer finishSpan(span, err)
	req = normalizeRecordReceipt(req)
	if err := validateRecordReceipt(req); err != nil {
		return Publication{}, err
	}
	publication, err = s.Store.RecordReceipt(ctx, req, s.now())
	if err == nil {
		row := releaseDecisionEvent{EventType: "receipt_recorded", Actor: principal.Actor, Decision: publication.State, Reason: "provider receipt recorded", ProviderVersion: req.ProviderVersion}
		row.addPublication(publication)
		if releaseCtx, ok := s.releaseContextForEvent(ctx, publication.ReleaseVersionID); ok {
			row.addReleaseContext(releaseCtx)
		}
		row.addAttributes(map[string]string{"provider_resource_url": req.ProviderResourceURL, "provider_digest": req.ProviderDigest, "receipt_digest": req.ReceiptDigest})
		s.emitReleaseEvent(ctx, row)
	}
	return publication, err
}

func (s *Service) ReconcilePublication(ctx context.Context, principal Principal, publicationID uuid.UUID) (publication Publication, err error) {
	ctx, span := startSpan(ctx, "release.publication.reconcile", principal, attribute.String("release.publication_id", publicationID.String()))
	defer finishSpan(span, err)
	if publicationID == uuid.Nil {
		return Publication{}, fmt.Errorf("%w: publication_id is required", ErrInvalid)
	}
	publication, err = s.Store.ReconcilePublication(ctx, publicationID, s.now())
	if err == nil {
		row := releaseDecisionEvent{EventType: "publication_reconciled", Actor: principal.Actor, Decision: publication.State, Reason: "provider receipt state reconciled"}
		row.addPublication(publication)
		if releaseCtx, ok := s.releaseContextForEvent(ctx, publication.ReleaseVersionID); ok {
			row.addReleaseContext(releaseCtx)
		}
		s.emitReleaseEvent(ctx, row)
	}
	return publication, err
}

func (s *Service) Reconcile(ctx context.Context, limit int) error {
	ctx, span := serviceTracer.Start(ctx, "release.reconcile", trace.WithAttributes(attribute.Int("release.limit", limit)))
	defer span.End()
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	publications, err := s.Store.ListDuePublications(ctx, int32(limit))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	for _, publication := range publications {
		if _, err := s.DispatchPublication(ctx, Principal{Actor: "release-reconciler"}, publication.PublicationID); err != nil {
			span.RecordError(err)
			return err
		}
	}
	span.SetAttributes(attribute.Int("release.publications_reconciled", len(publications)))
	return nil
}

func (s *Service) EnsureMkskSeed(ctx context.Context, orgID string, sourceCommit string) error {
	orgID = clean(orgID)
	sourceCommit = clean(sourceCommit)
	if orgID == "" {
		return fmt.Errorf("%w: platform org id is required", ErrInvalid)
	}
	if sourceCommit == "" {
		sourceCommit = "unknown"
	}
	pkg, err := s.Store.GetPackageByName(ctx, orgID, "mksk")
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		pkg, err = s.Store.CreatePackage(ctx, RegisterPackageRequest{
			OrgID:                orgID,
			PackageName:          "mksk",
			PackageKind:          PackageKindCLI,
			RepoPath:             "src/make-skill",
			BazelTarget:          "//src/make-skill:mksk",
			OwnerTeam:            "platform",
			DefaultVersionScheme: VersionSchemeSemver,
			Visibility:           VisibilityInternal,
			PolicyRef:            "release-policies/mksk/v1",
		}, s.now())
		if err != nil {
			return err
		}
	}
	_, err = s.Store.CreateVersion(ctx, CreateVersionRequest{
		PackageID:        pkg.PackageID,
		CanonicalVersion: "0.1.0",
		VersionScheme:    VersionSchemeSemver,
		VersionKind:      VersionKindStable,
		ReleaseSequence:  1,
		SourceRepository: "guardian-intelligence/verself",
		SourceCommit:     sourceCommit,
		SourceRef:        "src/make-skill",
	}, s.now())
	if err != nil && !errors.Is(err, ErrConflict) {
		return err
	}
	return nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func startSpan(ctx context.Context, name string, principal Principal, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	all := []attribute.KeyValue{}
	if principal.Actor != "" {
		all = append(all, attribute.String("release.actor", principal.Actor))
	}
	all = append(all, attrs...)
	return serviceTracer.Start(ctx, name, trace.WithAttributes(all...))
}

func finishSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

func normalizeRegisterPackage(req RegisterPackageRequest) RegisterPackageRequest {
	req.OrgID = clean(req.OrgID)
	req.PackageName = cleanLower(req.PackageName)
	req.PackageKind = cleanLower(req.PackageKind)
	req.RepoPath = strings.Trim(clean(req.RepoPath), "/")
	req.BazelTarget = clean(req.BazelTarget)
	req.OwnerTeam = clean(req.OwnerTeam)
	req.DefaultVersionScheme = cleanLower(req.DefaultVersionScheme)
	req.Visibility = cleanLower(req.Visibility)
	req.PolicyRef = clean(req.PolicyRef)
	return req
}

func validateRegisterPackage(req RegisterPackageRequest) error {
	if req.OrgID == "" || req.PackageName == "" || req.RepoPath == "" || req.BazelTarget == "" || req.OwnerTeam == "" || req.PolicyRef == "" {
		return fmt.Errorf("%w: package identity fields are required", ErrInvalid)
	}
	if !oneOf(req.PackageKind, PackageKindCLI, "npm", "container", "library", "website", "image") {
		return fmt.Errorf("%w: unsupported package kind", ErrInvalid)
	}
	if !oneOf(req.DefaultVersionScheme, VersionSchemeSemver, "pep440", "debian", "calendar", "opaque") {
		return fmt.Errorf("%w: unsupported version scheme", ErrInvalid)
	}
	if !oneOf(req.Visibility, VisibilityInternal, "private", "public") {
		return fmt.Errorf("%w: unsupported visibility", ErrInvalid)
	}
	return nil
}

func normalizeCreateVersion(req CreateVersionRequest) CreateVersionRequest {
	req.CanonicalVersion = clean(req.CanonicalVersion)
	req.VersionScheme = cleanLower(req.VersionScheme)
	req.VersionKind = cleanLower(req.VersionKind)
	req.SourceRepository = clean(req.SourceRepository)
	req.SourceCommit = clean(req.SourceCommit)
	req.SourceRef = clean(req.SourceRef)
	return req
}

func validateCreateVersion(req CreateVersionRequest) error {
	if req.PackageID == uuid.Nil || req.CanonicalVersion == "" || req.SourceRepository == "" || req.SourceCommit == "" || req.SourceRef == "" {
		return fmt.Errorf("%w: release version identity fields are required", ErrInvalid)
	}
	if req.ReleaseSequence < 0 {
		return fmt.Errorf("%w: release sequence must be non-negative", ErrInvalid)
	}
	if !oneOf(req.VersionScheme, VersionSchemeSemver, "pep440", "debian", "calendar", "opaque") {
		return fmt.Errorf("%w: unsupported version scheme", ErrInvalid)
	}
	if !oneOf(req.VersionKind, VersionKindStable, VersionKindRC, VersionKindNightly, "canary") {
		return fmt.Errorf("%w: unsupported version kind", ErrInvalid)
	}
	return nil
}

func normalizeAttachArtifact(req AttachArtifactRequest) AttachArtifactRequest {
	req.ArtifactRole = cleanLower(req.ArtifactRole)
	req.PlatformOS = cleanLower(req.PlatformOS)
	req.PlatformArch = cleanLower(req.PlatformArch)
	req.OCIRepository = strings.Trim(clean(req.OCIRepository), "/")
	req.OCIDigest = cleanLower(req.OCIDigest)
	req.OCIMediaType = clean(req.OCIMediaType)
	req.RegistryURL = strings.TrimRight(clean(req.RegistryURL), "/")
	return req
}

func validateAttachArtifact(req AttachArtifactRequest) error {
	if req.ReleaseVersionID == uuid.Nil || req.PlatformOS == "" || req.PlatformArch == "" || req.OCIRepository == "" || req.OCIMediaType == "" || req.RegistryURL == "" {
		return fmt.Errorf("%w: artifact fields are required", ErrInvalid)
	}
	if !oneOf(req.ArtifactRole, ArtifactRoleArchive, "binary", "container", "sbom", "metadata") {
		return fmt.Errorf("%w: unsupported artifact role", ErrInvalid)
	}
	if !digestPattern.MatchString(req.OCIDigest) {
		return fmt.Errorf("%w: OCI digest must be sha256", ErrInvalid)
	}
	if req.OCISizeBytes < 0 {
		return fmt.Errorf("%w: OCI size must be non-negative", ErrInvalid)
	}
	return nil
}

func normalizeAttachEvidence(req AttachEvidenceRequest) AttachEvidenceRequest {
	req.EvidenceKind = cleanLower(req.EvidenceKind)
	req.PredicateType = clean(req.PredicateType)
	req.SubjectDigest = cleanLower(req.SubjectDigest)
	req.DocumentDigest = clean(req.DocumentDigest)
	req.OCIReferrerDigest = cleanLower(req.OCIReferrerDigest)
	return req
}

func validateAttachEvidence(req AttachEvidenceRequest) error {
	if req.ReleaseVersionID == uuid.Nil || req.ArtifactID == uuid.Nil || req.EvidenceKind == "" || req.PredicateType == "" || req.DocumentDigest == "" {
		return fmt.Errorf("%w: evidence fields are required", ErrInvalid)
	}
	if !oneOf(req.EvidenceKind, EvidenceCosign, EvidenceSLSA, EvidenceSBOM, "test", "vsa", "receipt", "vex") {
		return fmt.Errorf("%w: unsupported evidence kind", ErrInvalid)
	}
	if !digestPattern.MatchString(req.SubjectDigest) || !digestPattern.MatchString(req.OCIReferrerDigest) {
		return fmt.Errorf("%w: evidence subject and referrer digests must be sha256", ErrInvalid)
	}
	return nil
}

func normalizeVerify(req VerifyRequest) VerifyRequest {
	req.PolicyRef = clean(req.PolicyRef)
	req.PolicyDigest = clean(req.PolicyDigest)
	req.BuilderID = clean(req.BuilderID)
	req.SignerIdentity = clean(req.SignerIdentity)
	req.CheckedBy = clean(req.CheckedBy)
	return req
}

func validateVerify(req VerifyRequest) error {
	if req.ReleaseVersionID == uuid.Nil || req.PolicyRef == "" || req.PolicyDigest == "" || req.BuilderID == "" || req.SignerIdentity == "" || req.CheckedBy == "" {
		return fmt.Errorf("%w: verification fields are required", ErrInvalid)
	}
	return nil
}

func normalizePromote(req PromoteChannelRequest) PromoteChannelRequest {
	req.ChannelName = cleanLower(req.ChannelName)
	req.ChannelKind = cleanLower(req.ChannelKind)
	req.Visibility = cleanLower(req.Visibility)
	req.PolicyRef = clean(req.PolicyRef)
	req.ActorID = clean(req.ActorID)
	req.Reason = clean(req.Reason)
	return req
}

func validatePromote(req PromoteChannelRequest) error {
	if req.PackageID == uuid.Nil || req.ReleaseVersionID == uuid.Nil || req.ChannelName == "" || req.PolicyRef == "" || req.ActorID == "" || req.Reason == "" {
		return fmt.Errorf("%w: channel promotion fields are required", ErrInvalid)
	}
	if !oneOf(req.ChannelKind, VersionKindStable, VersionKindRC, VersionKindNightly, "canary") {
		return fmt.Errorf("%w: unsupported channel kind", ErrInvalid)
	}
	if !oneOf(req.Visibility, VisibilityInternal, "private", "public") {
		return fmt.Errorf("%w: unsupported visibility", ErrInvalid)
	}
	return nil
}

func normalizeCreatePublication(req CreatePublicationRequest) CreatePublicationRequest {
	req.ProviderKind = cleanLower(req.ProviderKind)
	req.ProviderPackage = clean(req.ProviderPackage)
	req.ProviderChannel = clean(req.ProviderChannel)
	req.RequestedBy = clean(req.RequestedBy)
	req.IdempotencyKey = clean(req.IdempotencyKey)
	return req
}

func validateCreatePublication(req CreatePublicationRequest) error {
	if req.ReleaseVersionID == uuid.Nil || req.ProviderKind == "" || req.ProviderPackage == "" || req.ProviderChannel == "" || req.RequestedBy == "" || req.IdempotencyKey == "" {
		return fmt.Errorf("%w: publication fields are required", ErrInvalid)
	}
	if !oneOf(req.ProviderKind, ProviderOCI, "zot", "npm", "pypi", "crates", ProviderHomebrew, "apt", "github", "newsroom") {
		return fmt.Errorf("%w: unsupported provider kind", ErrInvalid)
	}
	return nil
}

func normalizeRecordReceipt(req RecordReceiptRequest) RecordReceiptRequest {
	req.ProviderResourceURL = clean(req.ProviderResourceURL)
	req.ProviderVersion = clean(req.ProviderVersion)
	req.ProviderDigest = clean(req.ProviderDigest)
	req.ReceiptDigest = clean(req.ReceiptDigest)
	return req
}

func validateRecordReceipt(req RecordReceiptRequest) error {
	if req.PublicationID == uuid.Nil || req.ProviderResourceURL == "" || req.ProviderVersion == "" || req.ProviderDigest == "" || req.ReceiptDigest == "" {
		return fmt.Errorf("%w: receipt fields are required", ErrInvalid)
	}
	return nil
}

func clean(value string) string {
	return strings.TrimSpace(value)
}

func cleanLower(value string) string {
	return strings.ToLower(clean(value))
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
