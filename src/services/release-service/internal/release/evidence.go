package release

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type releaseDecisionEvent struct {
	RecordedAt       time.Time `ch:"recorded_at"`
	EventDate        time.Time `ch:"event_date"`
	OccurredAt       time.Time `ch:"occurred_at"`
	EventType        string    `ch:"event_type"`
	OrgID            string    `ch:"org_id"`
	PackageID        uuid.UUID `ch:"package_id"`
	PackageName      string    `ch:"package_name"`
	ReleaseVersionID uuid.UUID `ch:"release_version_id"`
	CanonicalVersion string    `ch:"canonical_version"`
	VersionKind      string    `ch:"version_kind"`
	ArtifactID       uuid.UUID `ch:"artifact_id"`
	ArtifactDigest   string    `ch:"artifact_digest"`
	OCIRepository    string    `ch:"oci_repository"`
	SourceCommit     string    `ch:"source_commit"`
	BazelTarget      string    `ch:"bazel_target"`
	SignerIdentity   string    `ch:"signer_identity"`
	BuilderID        string    `ch:"builder_id"`
	PolicyRef        string    `ch:"policy_ref"`
	PolicyDigest     string    `ch:"policy_digest"`
	LifecycleStatus  string    `ch:"lifecycle_status"`
	ProviderKind     string    `ch:"provider_kind"`
	ProviderPackage  string    `ch:"provider_package"`
	ProviderVersion  string    `ch:"provider_version"`
	ProviderChannel  string    `ch:"provider_channel"`
	PublicationID    uuid.UUID `ch:"publication_id"`
	Decision         string    `ch:"decision"`
	Reason           string    `ch:"reason"`
	Actor            string    `ch:"actor"`
	RequestID        string    `ch:"request_id"`
	TraceID          string    `ch:"trace_id"`
	SpanID           string    `ch:"span_id"`
	AttributesJSON   string    `ch:"attributes_json"`
}

func (s *Service) emitReleaseEvent(ctx context.Context, row releaseDecisionEvent) {
	if s == nil || s.CH == nil {
		return
	}
	now := s.now()
	if row.OccurredAt.IsZero() {
		row.OccurredAt = now
	}
	row.RecordedAt = now
	row.EventDate = now
	if row.Actor == "" {
		row.Actor = "unknown"
	}
	if row.AttributesJSON == "" {
		row.AttributesJSON = "{}"
	}
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		row.TraceID = spanContext.TraceID().String()
		row.SpanID = spanContext.SpanID().String()
	}
	insertCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	batch, err := s.CH.PrepareBatch(insertCtx, "INSERT INTO verself.release_decision_events")
	if err != nil {
		s.log().WarnContext(ctx, "release clickhouse batch failed", "event_type", row.EventType, "error", err)
		return
	}
	if err := batch.AppendStruct(&row); err != nil {
		s.log().WarnContext(ctx, "release clickhouse append failed", "event_type", row.EventType, "error", err)
		return
	}
	if err := batch.Send(); err != nil {
		s.log().WarnContext(ctx, "release clickhouse insert failed", "event_type", row.EventType, "error", err)
	}
}

func (s *Service) releaseContextForEvent(ctx context.Context, releaseVersionID uuid.UUID) (ReleaseContext, bool) {
	if releaseVersionID == uuid.Nil {
		return ReleaseContext{}, false
	}
	releaseCtx, err := s.Store.GetRelease(ctx, releaseVersionID)
	if err != nil {
		s.log().WarnContext(ctx, "release event context lookup failed", "release_version_id", releaseVersionID.String(), "error", err)
		return ReleaseContext{}, false
	}
	return releaseCtx, true
}

func (row *releaseDecisionEvent) addPackage(pkg Package) {
	row.OrgID = pkg.OrgID
	row.PackageID = pkg.PackageID
	row.PackageName = pkg.PackageName
	row.BazelTarget = pkg.BazelTarget
	if row.PolicyRef == "" {
		row.PolicyRef = pkg.PolicyRef
	}
}

func (row *releaseDecisionEvent) addVersion(version Version) {
	row.ReleaseVersionID = version.ReleaseVersionID
	row.PackageID = version.PackageID
	row.CanonicalVersion = version.CanonicalVersion
	row.VersionKind = version.VersionKind
	row.SourceCommit = version.SourceCommit
	row.LifecycleStatus = version.LifecycleStatus
}

func (row *releaseDecisionEvent) addReleaseContext(releaseCtx ReleaseContext) {
	row.addPackage(releaseCtx.Package)
	row.addVersion(releaseCtx.Version)
}

func (row *releaseDecisionEvent) addArtifact(artifact Artifact) {
	row.ArtifactID = artifact.ArtifactID
	row.ArtifactDigest = artifact.OCIDigest
	row.OCIRepository = artifact.OCIRepository
	row.ReleaseVersionID = artifact.ReleaseVersionID
}

func (row *releaseDecisionEvent) addPublication(publication Publication) {
	row.PublicationID = publication.PublicationID
	row.ReleaseVersionID = publication.ReleaseVersionID
	row.ProviderKind = publication.ProviderKind
	row.ProviderPackage = publication.ProviderPackage
	row.ProviderChannel = publication.ProviderChannel
}

func (row *releaseDecisionEvent) addAttributes(value map[string]string) {
	if len(value) == 0 {
		row.AttributesJSON = "{}"
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		row.AttributesJSON = "{}"
		return
	}
	row.AttributesJSON = string(payload)
}

func (s *Service) log() *slog.Logger {
	if s != nil && s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
