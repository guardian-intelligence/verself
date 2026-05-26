package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var serviceTracer = otel.Tracer("distribution-service/distribution")

type Service struct {
	Store           SQLStore
	CH              driver.Conn
	Logger          *slog.Logger
	Signer          TUFSigner
	TrustedBuilders map[string]struct{}
	TrustedSigners  map[string]struct{}
	InstallationID  string
	PublicBaseURL   string
	Now             func() time.Time
}

func (s *Service) Ready(ctx context.Context) error {
	if err := s.Store.Ready(ctx); err != nil {
		return err
	}
	if err := s.Signer.Ready(); err != nil {
		return err
	}
	if clean(s.PublicBaseURL) == "" {
		return fmt.Errorf("%w: public base URL is required", ErrInvalid)
	}
	if len(s.TrustedBuilders) == 0 {
		return fmt.Errorf("%w: at least one trusted builder is required", ErrUntrustedBuilder)
	}
	if len(s.TrustedSigners) == 0 {
		return fmt.Errorf("%w: at least one trusted signer is required", ErrUntrustedSigner)
	}
	if s.CH != nil {
		var one uint8
		if err := s.CH.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
			return fmt.Errorf("%w: clickhouse readiness: %v", ErrStoreUnavailable, err)
		}
	}
	return nil
}

func (s *Service) AdmitArtifact(ctx context.Context, principal Principal, req AdmitArtifactRequest) (artifact Artifact, err error) {
	ctx, span := startSpan(ctx, "distribution.artifact.admit", principal, attribute.String("distribution.package", req.PackageName), attribute.String("distribution.digest", req.OCIDigest))
	defer finishSpan(span, err)
	req = normalizeAdmit(req)
	s.emit(ctx, event{EventType: "distribution.artifact.admit_requested", Decision: "requested", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, ArtifactDigest: req.OCIDigest, Actor: principal.Actor})
	if err := validateAdmit(req, s.TrustedBuilders, s.TrustedSigners); err != nil {
		s.emit(ctx, event{EventType: "distribution.artifact.verification_denied", Decision: "denied", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, ArtifactDigest: req.OCIDigest, Actor: principal.Actor, Reason: err.Error()})
		return Artifact{}, err
	}
	s.emit(ctx, event{EventType: "distribution.artifact.verification_started", Decision: "started", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, ArtifactDigest: req.OCIDigest, Actor: principal.Actor})
	artifact, err = s.Store.AdmitArtifact(ctx, req, requestDigest(req), s.now())
	if err == nil {
		s.emit(ctx, event{EventType: "distribution.artifact.verification_allowed", Decision: "allowed", PackageName: artifact.PackageName, ChannelName: artifact.ChannelName, PlatformOS: artifact.PlatformOS, PlatformArch: artifact.PlatformArch, ArtifactDigest: artifact.OCIDigest, Actor: principal.Actor})
		s.emit(ctx, event{EventType: "distribution.artifact.available", Decision: "available", PackageName: artifact.PackageName, ChannelName: artifact.ChannelName, PlatformOS: artifact.PlatformOS, PlatformArch: artifact.PlatformArch, ArtifactDigest: artifact.OCIDigest, Actor: principal.Actor})
	}
	return artifact, err
}

func (s *Service) PromoteTarget(ctx context.Context, principal Principal, req PromoteTargetRequest) (target Target, err error) {
	ctx, span := startSpan(ctx, "distribution.target.promote", principal, attribute.String("distribution.package", req.PackageName), attribute.String("distribution.channel", req.ChannelName), attribute.String("distribution.digest", req.ArtifactDigest))
	defer finishSpan(span, err)
	req = normalizePromote(req)
	s.emit(ctx, event{EventType: "distribution.target.promote_requested", Decision: "requested", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, ArtifactDigest: req.ArtifactDigest, Actor: principal.Actor})
	if err := validatePromote(req); err != nil {
		s.emit(ctx, event{EventType: "distribution.target.resolve_denied", Decision: "denied", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, ArtifactDigest: req.ArtifactDigest, Actor: principal.Actor, Reason: err.Error()})
		return Target{}, err
	}
	artifact, err := s.Store.GetArtifactByDigest(ctx, req.ArtifactDigest)
	if err != nil {
		return Target{}, err
	}
	if artifact.State == StateQuarantined {
		return Target{}, ErrQuarantined
	}
	if artifact.State != StateAvailable {
		return Target{}, fmt.Errorf("%w: artifact state %s is not promotable", ErrChannelPolicyFailure, artifact.State)
	}
	if artifact.PackageName != req.PackageName || artifact.PlatformOS != req.PlatformOS || artifact.PlatformArch != req.PlatformArch {
		return Target{}, fmt.Errorf("%w: artifact does not match requested package/platform", ErrChannelPolicyFailure)
	}
	s.emit(ctx, event{EventType: "distribution.target.policy_allowed", Decision: "allowed", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, ArtifactDigest: req.ArtifactDigest, Actor: principal.Actor})
	versions, err := s.nextTUFVersions(ctx, req.PackageName)
	if err != nil {
		return Target{}, err
	}
	metadata, err := s.Signer.buildSet(req.PackageName, artifact, versions, s.now())
	if err != nil {
		return Target{}, err
	}
	s.emit(ctx, event{EventType: "distribution.target.tuf_signed", Decision: "signed", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, ArtifactDigest: req.ArtifactDigest, Actor: principal.Actor})
	target, err = s.Store.PublishTarget(ctx, req, artifact, metadata, s.PublicBaseURL, requestDigest(req), s.now())
	if err == nil {
		s.emit(ctx, event{EventType: "distribution.target.published", Decision: "published", PackageName: target.PackageName, ChannelName: target.ChannelName, PlatformOS: target.PlatformOS, PlatformArch: target.PlatformArch, ArtifactDigest: target.ArtifactDigest, Actor: principal.Actor})
	}
	return target, err
}

func (s *Service) ResolveTarget(ctx context.Context, principal Principal, req ResolveTargetRequest) (target Target, err error) {
	ctx, span := startSpan(ctx, "distribution.target.resolve", principal, attribute.String("distribution.package", req.PackageName), attribute.String("distribution.channel", req.ChannelName))
	defer finishSpan(span, err)
	req = normalizeResolve(req)
	target, err = s.Store.GetCurrentTarget(ctx, req)
	if err != nil {
		s.emit(ctx, event{EventType: "distribution.target.resolve_denied", Decision: "denied", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, Actor: principal.Actor, Reason: err.Error()})
		return Target{}, err
	}
	artifact, err := s.Store.GetArtifactByDigest(ctx, target.ArtifactDigest)
	if err != nil {
		return Target{}, err
	}
	if artifact.State == StateQuarantined {
		s.emit(ctx, event{EventType: "distribution.target.resolve_denied", Decision: "denied", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, ArtifactDigest: target.ArtifactDigest, Actor: principal.Actor, Reason: ErrQuarantined.Error()})
		return Target{}, ErrQuarantined
	}
	s.emit(ctx, event{EventType: "distribution.target.resolve_allowed", Decision: "allowed", PackageName: target.PackageName, ChannelName: target.ChannelName, PlatformOS: target.PlatformOS, PlatformArch: target.PlatformArch, ArtifactDigest: target.ArtifactDigest, Actor: principal.Actor})
	return target, nil
}

func (s *Service) CheckUpdate(ctx context.Context, principal Principal, req CheckUpdateRequest) (Target, bool, error) {
	target, err := s.ResolveTarget(ctx, principal, ResolveTargetRequest{PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch})
	if err != nil {
		return Target{}, false, err
	}
	available := clean(req.InstalledDigest) == "" || clean(req.InstalledDigest) != target.ArtifactDigest
	s.emit(ctx, event{EventType: "distribution.upgrade.checked", Decision: boolDecision(available), PackageName: target.PackageName, ChannelName: target.ChannelName, PlatformOS: target.PlatformOS, PlatformArch: target.PlatformArch, ArtifactDigest: target.ArtifactDigest, Actor: principal.Actor})
	return target, available, nil
}

func (s *Service) RecordUpgradeDownloadVerified(ctx context.Context, principal Principal, req UpgradeVerificationRequest) error {
	req.PackageName = clean(req.PackageName)
	req.ChannelName = clean(req.ChannelName)
	req.PlatformOS = clean(req.PlatformOS)
	req.PlatformArch = clean(req.PlatformArch)
	req.ArtifactDigest = clean(req.ArtifactDigest)
	req.LayerDigest = clean(req.LayerDigest)
	req.InstalledVersion = clean(req.InstalledVersion)
	if req.PackageName == "" || req.ChannelName == "" || req.PlatformOS == "" || req.PlatformArch == "" {
		return fmt.Errorf("%w: package, channel, and platform are required", ErrInvalid)
	}
	if err := validateDigest(req.ArtifactDigest); err != nil {
		return err
	}
	if err := validateDigest(req.LayerDigest); err != nil {
		return err
	}
	s.emit(ctx, event{EventType: "distribution.upgrade.download_verified", Decision: "verified", PackageName: req.PackageName, ChannelName: req.ChannelName, PlatformOS: req.PlatformOS, PlatformArch: req.PlatformArch, ArtifactDigest: req.ArtifactDigest, Actor: principal.Actor, Reason: "layer_digest=" + req.LayerDigest + " installed_version=" + req.InstalledVersion})
	return nil
}

func (s *Service) GetArtifactByRef(ctx context.Context, principal Principal, ref string) (Artifact, error) {
	if err := validateDigestRef(ref); err != nil {
		return Artifact{}, err
	}
	artifact, err := s.Store.GetArtifactByDigest(ctx, digestFromRef(ref))
	if err != nil {
		return Artifact{}, err
	}
	s.emit(ctx, event{EventType: "distribution.artifact.get", Decision: "allowed", PackageName: artifact.PackageName, ChannelName: artifact.ChannelName, PlatformOS: artifact.PlatformOS, PlatformArch: artifact.PlatformArch, ArtifactDigest: artifact.OCIDigest, Actor: principal.Actor})
	return artifact, nil
}

func (s *Service) ListTargets(ctx context.Context, principal Principal, packageName, channelName, platformOS, platformArch string, limit int32) ([]Target, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	targets, err := s.Store.ListTargets(ctx, clean(packageName), clean(channelName), clean(platformOS), clean(platformArch), limit)
	if err == nil {
		s.emit(ctx, event{EventType: "distribution.channel.targets_listed", Decision: "allowed", PackageName: clean(packageName), ChannelName: clean(channelName), PlatformOS: clean(platformOS), PlatformArch: clean(platformArch), Actor: principal.Actor})
	}
	return targets, err
}

func (s *Service) QuarantineArtifact(ctx context.Context, principal Principal, req QuarantineArtifactRequest) (Artifact, error) {
	req.ArtifactDigestRef = clean(req.ArtifactDigestRef)
	req.ActorID = clean(req.ActorID)
	req.Reason = clean(req.Reason)
	if req.ActorID == "" {
		req.ActorID = principal.Actor
	}
	artifact, err := s.Store.QuarantineArtifact(ctx, req, requestDigest(req), s.now())
	if err == nil {
		s.emit(ctx, event{EventType: "distribution.artifact.quarantined", Decision: "quarantined", PackageName: artifact.PackageName, ChannelName: artifact.ChannelName, PlatformOS: artifact.PlatformOS, PlatformArch: artifact.PlatformArch, ArtifactDigest: artifact.OCIDigest, Actor: req.ActorID, Reason: req.Reason})
	}
	return artifact, err
}

func (s *Service) EnsureReplication(ctx context.Context, principal Principal, req EnsureReplicationRequest) (Artifact, error) {
	req.ArtifactDigestRef = clean(req.ArtifactDigestRef)
	req.ActorID = clean(req.ActorID)
	req.Reason = clean(req.Reason)
	if req.ActorID == "" {
		req.ActorID = principal.Actor
	}
	artifact, err := s.Store.EnsureReplication(ctx, req, requestDigest(req), s.now())
	if err == nil {
		s.emit(ctx, event{EventType: "distribution.artifact.replication_ensured", Decision: "available", PackageName: artifact.PackageName, ChannelName: artifact.ChannelName, PlatformOS: artifact.PlatformOS, PlatformArch: artifact.PlatformArch, ArtifactDigest: artifact.OCIDigest, Actor: req.ActorID, Reason: req.Reason})
	}
	return artifact, err
}

func (s *Service) TUFMetadata(ctx context.Context, packageName, role string) (TUFMetadata, error) {
	return s.Store.LatestTUFMetadata(ctx, clean(packageName), clean(role))
}

func (s *Service) RecordTUFMetadataServed(ctx context.Context, meta TUFMetadata) {
	s.emit(ctx, event{EventType: "distribution.tuf.metadata_served", Decision: "served", PackageName: meta.PackageName, ArtifactDigest: meta.BodySHA256, Actor: "distribution-service", Reason: "role=" + meta.Role})
}

func (s *Service) RecordOCIServed(ctx context.Context, artifact Artifact, kind string, reference string) {
	eventType := "distribution.oci.referrers_served"
	switch kind {
	case "manifests":
		eventType = "distribution.oci.manifest_served"
	case "blobs":
		eventType = "distribution.oci.blob_served"
	}
	s.emit(ctx, event{EventType: eventType, Decision: "served", PackageName: artifact.PackageName, ChannelName: artifact.ChannelName, PlatformOS: artifact.PlatformOS, PlatformArch: artifact.PlatformArch, ArtifactDigest: artifact.OCIDigest, Actor: "distribution-service", Reason: "reference=" + reference})
}

func (s *Service) nextTUFVersions(ctx context.Context, packageName string) (tufVersions, error) {
	targets, err := s.Store.LatestTUFVersion(ctx, packageName, "targets")
	if err != nil {
		return tufVersions{}, err
	}
	snapshot, err := s.Store.LatestTUFVersion(ctx, packageName, "snapshot")
	if err != nil {
		return tufVersions{}, err
	}
	timestamp, err := s.Store.LatestTUFVersion(ctx, packageName, "timestamp")
	if err != nil {
		return tufVersions{}, err
	}
	return tufVersions{Targets: targets + 1, Snapshot: snapshot + 1, Timestamp: timestamp + 1}, nil
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) log() *slog.Logger {
	if s != nil && s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func normalizeAdmit(req AdmitArtifactRequest) AdmitArtifactRequest {
	req.PackageName = clean(req.PackageName)
	req.PackageVersion = clean(req.PackageVersion)
	req.ChannelName = clean(req.ChannelName)
	req.PlatformOS = clean(req.PlatformOS)
	req.PlatformArch = clean(req.PlatformArch)
	req.OriginRegistryURL = clean(req.OriginRegistryURL)
	req.PublicRegistryURL = clean(req.PublicRegistryURL)
	req.OCIRepository = clean(req.OCIRepository)
	req.OCIDigest = clean(req.OCIDigest)
	req.OCIMediaType = clean(req.OCIMediaType)
	req.BuilderID = clean(req.BuilderID)
	req.SignerIdentity = clean(req.SignerIdentity)
	req.SourceRepository = clean(req.SourceRepository)
	req.SourceCommit = clean(req.SourceCommit)
	req.SourceRef = clean(req.SourceRef)
	req.BazelTarget = clean(req.BazelTarget)
	req.PolicyRef = clean(req.PolicyRef)
	req.SubmittedBy = clean(req.SubmittedBy)
	req.IdempotencyKey = clean(req.IdempotencyKey)
	for i := range req.Evidence {
		req.Evidence[i].EvidenceKind = clean(req.Evidence[i].EvidenceKind)
		req.Evidence[i].PredicateType = clean(req.Evidence[i].PredicateType)
		req.Evidence[i].SubjectDigest = clean(req.Evidence[i].SubjectDigest)
		req.Evidence[i].DocumentDigest = clean(req.Evidence[i].DocumentDigest)
		req.Evidence[i].OCIReferrerDigest = clean(req.Evidence[i].OCIReferrerDigest)
	}
	return req
}

func normalizePromote(req PromoteTargetRequest) PromoteTargetRequest {
	req.PackageName = clean(req.PackageName)
	req.ChannelName = clean(req.ChannelName)
	req.ArtifactDigest = clean(req.ArtifactDigest)
	req.PlatformOS = clean(req.PlatformOS)
	req.PlatformArch = clean(req.PlatformArch)
	req.PolicyRef = clean(req.PolicyRef)
	req.PromotedBy = clean(req.PromotedBy)
	req.Reason = clean(req.Reason)
	req.IdempotencyKey = clean(req.IdempotencyKey)
	return req
}

func normalizeResolve(req ResolveTargetRequest) ResolveTargetRequest {
	req.PackageName = clean(req.PackageName)
	req.ChannelName = clean(req.ChannelName)
	req.PlatformOS = clean(req.PlatformOS)
	req.PlatformArch = clean(req.PlatformArch)
	return req
}

func requestDigest(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		payload = []byte(fmt.Sprintf("%#v", value))
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func boolDecision(value bool) string {
	if value {
		return "update_available"
	}
	return "no_update"
}

func startSpan(ctx context.Context, name string, principal Principal, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := serviceTracer.Start(ctx, name)
	span.SetAttributes(attribute.String("distribution.actor", principal.Actor))
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	return ctx, span
}

func finishSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
