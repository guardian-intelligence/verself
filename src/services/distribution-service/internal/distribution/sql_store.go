package distribution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	distributionstore "github.com/verself/distribution-service/internal/store"
)

type SQLStore struct {
	PG *pgxpool.Pool
}

func (s SQLStore) Ready(ctx context.Context) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if _, err := distributionstore.New(s.PG).Ping(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) AdmitArtifact(ctx context.Context, req AdmitArtifactRequest, payloadDigest string, now time.Time) (Artifact, error) {
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Artifact{}, err
	}
	defer rollback(ctx, tx)
	artifactID := uuid.New()
	if err := claimIdempotency(ctx, q, "admit-artifact", req.IdempotencyKey, payloadDigest, "artifact", req.OCIDigest, now); err != nil {
		if errors.Is(err, ErrIdempotencyMismatch) {
			return Artifact{}, err
		}
		existing, getErr := q.GetArtifactByDigest(ctx, distributionstore.GetArtifactByDigestParams{OciDigest: req.OCIDigest})
		if getErr != nil {
			return Artifact{}, storeError(getErr)
		}
		return s.artifactWithEvidence(ctx, q, existing)
	}
	row, err := q.CreateArtifact(ctx, distributionstore.CreateArtifactParams{
		ArtifactID:           artifactID,
		PackageName:          req.PackageName,
		PackageVersion:       req.PackageVersion,
		ChannelName:          req.ChannelName,
		PlatformOs:           req.PlatformOS,
		PlatformArch:         req.PlatformArch,
		OriginRegistryUrl:    req.OriginRegistryURL,
		PublicRegistryUrl:    req.PublicRegistryURL,
		OciRepository:        req.OCIRepository,
		OciDigest:            req.OCIDigest,
		OciMediaType:         req.OCIMediaType,
		OciSizeBytes:         req.OCISizeBytes,
		PublicOciReference:   publicOCIReference(req.PublicRegistryURL, req.OCIRepository, req.OCIDigest),
		BuilderID:            req.BuilderID,
		SignerIdentity:       req.SignerIdentity,
		SourceRepository:     req.SourceRepository,
		SourceCommit:         req.SourceCommit,
		SourceRef:            req.SourceRef,
		BazelTarget:          req.BazelTarget,
		PolicyRef:            req.PolicyRef,
		State:                StateAvailable,
		VerificationDecision: DecisionAllowed,
		VerificationReason:   "required OCI referrers and policy inputs verified",
		RetentionClass:       retentionClass(req.ChannelName),
		ReplicationState:     ReplicationAvailable,
		SubmittedBy:          req.SubmittedBy,
		AdmittedAt:           pgTime(now),
		AvailableAt:          pgTime(now),
		CreatedAt:            pgTime(now),
		UpdatedAt:            pgTime(now),
	})
	if err != nil {
		return Artifact{}, storeError(err)
	}
	for _, evidence := range req.Evidence {
		if _, err := q.CreateEvidence(ctx, distributionstore.CreateEvidenceParams{
			EvidenceID:        uuid.New(),
			ArtifactID:        row.ArtifactID,
			EvidenceKind:      evidence.EvidenceKind,
			PredicateType:     evidence.PredicateType,
			SubjectDigest:     evidence.SubjectDigest,
			DocumentDigest:    evidence.DocumentDigest,
			OciReferrerDigest: evidence.OCIReferrerDigest,
			CreatedAt:         pgTime(now),
		}); err != nil {
			return Artifact{}, storeError(err)
		}
	}
	artifact, err := s.artifactWithEvidence(ctx, q, row)
	if err != nil {
		return Artifact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return artifact, nil
}

func (s SQLStore) GetArtifactByDigest(ctx context.Context, digest string) (Artifact, error) {
	q, err := s.queries()
	if err != nil {
		return Artifact{}, err
	}
	row, err := q.GetArtifactByDigest(ctx, distributionstore.GetArtifactByDigestParams{OciDigest: digest})
	if err != nil {
		return Artifact{}, storeError(err)
	}
	return s.artifactWithEvidence(ctx, q, row)
}

func (s SQLStore) GetArtifactByRepositoryDigest(ctx context.Context, repository string, digest string) (Artifact, error) {
	q, err := s.queries()
	if err != nil {
		return Artifact{}, err
	}
	row, err := q.GetArtifactByRepositoryDigest(ctx, distributionstore.GetArtifactByRepositoryDigestParams{OciRepository: repository, OciDigest: digest})
	if err != nil {
		return Artifact{}, storeError(err)
	}
	return s.artifactWithEvidence(ctx, q, row)
}

func (s SQLStore) ListArtifactsByRepository(ctx context.Context, repository string) ([]Artifact, error) {
	if s.PG == nil {
		return nil, ErrStoreUnavailable
	}
	rows, err := s.PG.Query(ctx, `
SELECT artifact_id, package_name, package_version, channel_name, platform_os, platform_arch,
       origin_registry_url, public_registry_url, oci_repository, oci_digest, oci_media_type,
       oci_size_bytes, public_oci_reference, builder_id, signer_identity, source_repository,
       source_commit, source_ref, bazel_target, policy_ref, state, verification_decision,
       verification_reason, retention_class, replication_state, submitted_by, admitted_at,
       available_at, quarantined_at, quarantine_reason, created_at, updated_at
FROM distribution_artifacts
WHERE oci_repository = $1 AND state = $2
ORDER BY updated_at DESC
LIMIT 100`, clean(repository), StateAvailable)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var row distributionstore.DistributionArtifact
		if err := rows.Scan(
			&row.ArtifactID,
			&row.PackageName,
			&row.PackageVersion,
			&row.ChannelName,
			&row.PlatformOs,
			&row.PlatformArch,
			&row.OriginRegistryUrl,
			&row.PublicRegistryUrl,
			&row.OciRepository,
			&row.OciDigest,
			&row.OciMediaType,
			&row.OciSizeBytes,
			&row.PublicOciReference,
			&row.BuilderID,
			&row.SignerIdentity,
			&row.SourceRepository,
			&row.SourceCommit,
			&row.SourceRef,
			&row.BazelTarget,
			&row.PolicyRef,
			&row.State,
			&row.VerificationDecision,
			&row.VerificationReason,
			&row.RetentionClass,
			&row.ReplicationState,
			&row.SubmittedBy,
			&row.AdmittedAt,
			&row.AvailableAt,
			&row.QuarantinedAt,
			&row.QuarantineReason,
			&row.CreatedAt,
			&row.UpdatedAt,
		); err != nil {
			return nil, storeError(err)
		}
		out = append(out, artifactFromStore(row, nil))
	}
	if err := rows.Err(); err != nil {
		return nil, storeError(err)
	}
	return out, nil
}

func (s SQLStore) QuarantineArtifact(ctx context.Context, req QuarantineArtifactRequest, payloadDigest string, now time.Time) (Artifact, error) {
	if err := validateDigestRef(req.ArtifactDigestRef); err != nil {
		return Artifact{}, err
	}
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Artifact{}, err
	}
	defer rollback(ctx, tx)
	digest := digestFromRef(req.ArtifactDigestRef)
	if err := claimIdempotency(ctx, q, "quarantine-artifact", req.IdempotencyKey, payloadDigest, "artifact", digest, now); err != nil && !errors.Is(err, ErrConflict) {
		return Artifact{}, err
	}
	row, err := q.MarkArtifactQuarantined(ctx, distributionstore.MarkArtifactQuarantinedParams{
		OciDigest:        digest,
		QuarantineReason: req.Reason,
		QuarantinedAt:    pgTime(now),
		UpdatedAt:        pgTime(now),
	})
	if err != nil {
		return Artifact{}, storeError(err)
	}
	artifact, err := s.artifactWithEvidence(ctx, q, row)
	if err != nil {
		return Artifact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return artifact, nil
}

func (s SQLStore) EnsureReplication(ctx context.Context, req EnsureReplicationRequest, payloadDigest string, now time.Time) (Artifact, error) {
	if err := validateDigestRef(req.ArtifactDigestRef); err != nil {
		return Artifact{}, err
	}
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Artifact{}, err
	}
	defer rollback(ctx, tx)
	digest := digestFromRef(req.ArtifactDigestRef)
	if err := claimIdempotency(ctx, q, "ensure-replication", req.IdempotencyKey, payloadDigest, "artifact", digest, now); err != nil && !errors.Is(err, ErrConflict) {
		return Artifact{}, err
	}
	row, err := q.EnsureArtifactReplication(ctx, distributionstore.EnsureArtifactReplicationParams{
		OciDigest: digest,
		UpdatedAt: pgTime(now),
	})
	if err != nil {
		return Artifact{}, storeError(err)
	}
	artifact, err := s.artifactWithEvidence(ctx, q, row)
	if err != nil {
		return Artifact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return artifact, nil
}

func (s SQLStore) LatestTUFVersion(ctx context.Context, packageName, role string) (int64, error) {
	q, err := s.queries()
	if err != nil {
		return 0, err
	}
	version, err := q.LatestTUFVersion(ctx, distributionstore.LatestTUFVersionParams{PackageName: packageName, Role: role})
	if err != nil {
		return 0, storeError(err)
	}
	return version, nil
}

func (s SQLStore) PublishTarget(ctx context.Context, req PromoteTargetRequest, artifact Artifact, metadata []TUFMetadata, publicBaseURL string, payloadDigest string, now time.Time) (Target, error) {
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Target{}, err
	}
	defer rollback(ctx, tx)
	if err := claimIdempotency(ctx, q, "promote-target", req.IdempotencyKey, payloadDigest, "artifact", req.ArtifactDigest, now); err != nil {
		if errors.Is(err, ErrConflict) {
			return s.GetCurrentTarget(ctx, ResolveTargetRequest{
				PackageName:  req.PackageName,
				ChannelName:  req.ChannelName,
				PlatformOS:   req.PlatformOS,
				PlatformArch: req.PlatformArch,
			})
		}
		return Target{}, err
	}
	for _, meta := range metadata {
		if _, err := q.CreateTUFMetadata(ctx, distributionstore.CreateTUFMetadataParams{
			TufMetadataID: uuid.New(),
			PackageName:   meta.PackageName,
			Role:          meta.Role,
			Version:       meta.Version,
			Body:          meta.Body,
			BodySha256:    meta.BodySHA256,
			BodyLength:    meta.BodyLength,
			SignedAt:      pgTime(meta.SignedAt),
			ExpiresAt:     pgTime(meta.ExpiresAt),
		}); err != nil {
			return Target{}, storeError(err)
		}
	}
	if err := q.SupersedeCurrentTargets(ctx, distributionstore.SupersedeCurrentTargetsParams{
		PackageName:        req.PackageName,
		ChannelName:        req.ChannelName,
		PlatformOs:         req.PlatformOS,
		PlatformArch:       req.PlatformArch,
		SupersededByDigest: req.ArtifactDigest,
		SupersededAt:       pgTime(now),
		UpdatedAt:          pgTime(now),
	}); err != nil {
		return Target{}, storeError(err)
	}
	versions := tufVersionsFromMetadata(metadata)
	row, err := q.UpsertPublishedTarget(ctx, distributionstore.UpsertPublishedTargetParams{
		TargetID:            uuid.New(),
		PackageName:         req.PackageName,
		ChannelName:         req.ChannelName,
		PlatformOs:          req.PlatformOS,
		PlatformArch:        req.PlatformArch,
		ArtifactID:          uuid.MustParse(artifact.ArtifactID),
		ArtifactDigest:      artifact.OCIDigest,
		PackageVersion:      artifact.PackageVersion,
		State:               TargetStatePublished,
		PublicOciReference:  artifact.PublicOCIReference,
		DownloadUrl:         downloadURL(artifact.PublicRegistryURL, artifact.OCIRepository, artifact.OCIDigest),
		TufMetadataUrl:      tufMetadataURL(publicBaseURL, artifact.PackageName),
		TufTargetsVersion:   versions.Targets,
		TufSnapshotVersion:  versions.Snapshot,
		TufTimestampVersion: versions.Timestamp,
		PolicyRef:           req.PolicyRef,
		PromotedBy:          req.PromotedBy,
		Reason:              req.Reason,
		PublishedAt:         pgTime(now),
		CreatedAt:           pgTime(now),
		UpdatedAt:           pgTime(now),
	})
	if err != nil {
		return Target{}, storeError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Target{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return targetFromStore(row), nil
}

func (s SQLStore) GetCurrentTarget(ctx context.Context, req ResolveTargetRequest) (Target, error) {
	q, err := s.queries()
	if err != nil {
		return Target{}, err
	}
	row, err := q.GetCurrentTarget(ctx, distributionstore.GetCurrentTargetParams{
		PackageName:  req.PackageName,
		ChannelName:  req.ChannelName,
		PlatformOs:   req.PlatformOS,
		PlatformArch: req.PlatformArch,
	})
	if err != nil {
		return Target{}, storeError(err)
	}
	return targetFromStore(row), nil
}

func (s SQLStore) ListTargets(ctx context.Context, packageName, channelName, platformOS, platformArch string, limit int32) ([]Target, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListChannelTargets(ctx, distributionstore.ListChannelTargetsParams{
		PackageName:  packageName,
		ChannelName:  channelName,
		PlatformOs:   platformOS,
		PlatformArch: platformArch,
		LimitCount:   limit,
	})
	if err != nil {
		return nil, storeError(err)
	}
	targets := make([]Target, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, targetFromStore(row))
	}
	return targets, nil
}

func (s SQLStore) LatestTUFMetadata(ctx context.Context, packageName, role string) (TUFMetadata, error) {
	q, err := s.queries()
	if err != nil {
		return TUFMetadata{}, err
	}
	row, err := q.GetLatestTUFMetadata(ctx, distributionstore.GetLatestTUFMetadataParams{PackageName: packageName, Role: role})
	if err != nil {
		return TUFMetadata{}, storeError(err)
	}
	return TUFMetadata{
		PackageName: row.PackageName,
		Role:        row.Role,
		Version:     row.Version,
		Body:        row.Body,
		BodySHA256:  row.BodySha256,
		BodyLength:  row.BodyLength,
		SignedAt:    timeFromPG(row.SignedAt),
		ExpiresAt:   timeFromPG(row.ExpiresAt),
	}, nil
}

func (s SQLStore) artifactWithEvidence(ctx context.Context, q *distributionstore.Queries, row distributionstore.DistributionArtifact) (Artifact, error) {
	evidenceRows, err := q.ListEvidenceForArtifact(ctx, distributionstore.ListEvidenceForArtifactParams{ArtifactID: row.ArtifactID})
	if err != nil {
		return Artifact{}, storeError(err)
	}
	evidence := make([]Evidence, 0, len(evidenceRows))
	for _, item := range evidenceRows {
		evidence = append(evidence, Evidence{
			EvidenceKind:      item.EvidenceKind,
			PredicateType:     item.PredicateType,
			SubjectDigest:     item.SubjectDigest,
			DocumentDigest:    item.DocumentDigest,
			OCIReferrerDigest: item.OciReferrerDigest,
			CreatedAt:         timeFromPG(item.CreatedAt),
		})
	}
	return artifactFromStore(row, evidence), nil
}

func (s SQLStore) queries() (*distributionstore.Queries, error) {
	if s.PG == nil {
		return nil, ErrStoreUnavailable
	}
	return distributionstore.New(s.PG), nil
}

func (s SQLStore) begin(ctx context.Context) (pgx.Tx, *distributionstore.Queries, error) {
	if s.PG == nil {
		return nil, nil, ErrStoreUnavailable
	}
	tx, err := s.PG.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return tx, distributionstore.New(s.PG).WithTx(tx), nil
}

func rollback(ctx context.Context, tx pgx.Tx) {
	if tx != nil {
		_ = tx.Rollback(ctx)
	}
}

func claimIdempotency(ctx context.Context, q *distributionstore.Queries, scope, key, payloadDigest, resourceKind, resourceID string, now time.Time) error {
	if clean(key) == "" {
		return fmt.Errorf("%w: idempotency key is required", ErrInvalid)
	}
	_, err := q.CreateIdempotencyKey(ctx, distributionstore.CreateIdempotencyKeyParams{
		Scope:          scope,
		IdempotencyKey: key,
		PayloadSha256:  payloadDigest,
		ResourceKind:   resourceKind,
		ResourceID:     resourceID,
		CreatedAt:      pgTime(now),
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return storeError(err)
	}
	row, err := q.GetIdempotencyKey(ctx, distributionstore.GetIdempotencyKeyParams{Scope: scope, IdempotencyKey: key})
	if err != nil {
		return storeError(err)
	}
	if row.PayloadSha256 != payloadDigest {
		return ErrIdempotencyMismatch
	}
	return ErrConflict
}

func storeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
		case "23503":
			return fmt.Errorf("%w: %s", ErrNotFound, pgErr.ConstraintName)
		case "23514":
			return fmt.Errorf("%w: %s", ErrInvalid, pgErr.ConstraintName)
		}
	}
	return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
}

func artifactFromStore(row distributionstore.DistributionArtifact, evidence []Evidence) Artifact {
	checkedAt := timeFromPG(row.UpdatedAt)
	return Artifact{
		ArtifactID:         row.ArtifactID.String(),
		PackageName:        row.PackageName,
		PackageVersion:     row.PackageVersion,
		ChannelName:        row.ChannelName,
		PlatformOS:         row.PlatformOs,
		PlatformArch:       row.PlatformArch,
		OriginRegistryURL:  row.OriginRegistryUrl,
		PublicRegistryURL:  row.PublicRegistryUrl,
		OCIRepository:      row.OciRepository,
		OCIDigest:          row.OciDigest,
		OCIMediaType:       row.OciMediaType,
		OCISizeBytes:       row.OciSizeBytes,
		PublicOCIReference: row.PublicOciReference,
		BuilderID:          row.BuilderID,
		SignerIdentity:     row.SignerIdentity,
		SourceRepository:   row.SourceRepository,
		SourceCommit:       row.SourceCommit,
		SourceRef:          row.SourceRef,
		BazelTarget:        row.BazelTarget,
		PolicyRef:          row.PolicyRef,
		State:              row.State,
		Verification: Verification{
			Decision:         row.VerificationDecision,
			Reason:           row.VerificationReason,
			BuilderID:        row.BuilderID,
			SignerIdentity:   row.SignerIdentity,
			SourceRepository: row.SourceRepository,
			SourceCommit:     row.SourceCommit,
			SourceRef:        row.SourceRef,
			BazelTarget:      row.BazelTarget,
			Evidence:         evidence,
			CheckedAt:        checkedAt,
		},
		RetentionClass: row.RetentionClass,
		Replication: Replication{
			State:             row.ReplicationState,
			OriginRegistryURL: row.OriginRegistryUrl,
			PublicRegistryURL: row.PublicRegistryUrl,
			CheckedAt:         checkedAt,
		},
		SubmittedBy:      row.SubmittedBy,
		AdmittedAt:       timeFromPG(row.AdmittedAt),
		AvailableAt:      timeFromPG(row.AvailableAt),
		QuarantinedAt:    timeFromPG(row.QuarantinedAt),
		QuarantineReason: row.QuarantineReason,
		CreatedAt:        timeFromPG(row.CreatedAt),
		UpdatedAt:        timeFromPG(row.UpdatedAt),
	}
}

func targetFromStore(row distributionstore.DistributionChannelTarget) Target {
	return Target{
		TargetID:            row.TargetID.String(),
		PackageName:         row.PackageName,
		ChannelName:         row.ChannelName,
		PlatformOS:          row.PlatformOs,
		PlatformArch:        row.PlatformArch,
		ArtifactID:          row.ArtifactID.String(),
		ArtifactDigest:      row.ArtifactDigest,
		PackageVersion:      row.PackageVersion,
		State:               row.State,
		PublicOCIReference:  row.PublicOciReference,
		DownloadURL:         row.DownloadUrl,
		TUFMetadataURL:      row.TufMetadataUrl,
		TUFTargetsVersion:   row.TufTargetsVersion,
		TUFSnapshotVersion:  row.TufSnapshotVersion,
		TUFTimestampVersion: row.TufTimestampVersion,
		PolicyRef:           row.PolicyRef,
		PromotedBy:          row.PromotedBy,
		Reason:              row.Reason,
		PublishedAt:         timeFromPG(row.PublishedAt),
		SupersededAt:        timeFromPG(row.SupersededAt),
		SupersededByDigest:  row.SupersededByDigest,
		CreatedAt:           timeFromPG(row.CreatedAt),
		UpdatedAt:           timeFromPG(row.UpdatedAt),
	}
}

type tufVersions struct {
	Targets   int64
	Snapshot  int64
	Timestamp int64
}

func tufVersionsFromMetadata(metadata []TUFMetadata) tufVersions {
	versions := tufVersions{}
	for _, item := range metadata {
		switch item.Role {
		case "targets":
			versions.Targets = item.Version
		case "snapshot":
			versions.Snapshot = item.Version
		case "timestamp":
			versions.Timestamp = item.Version
		}
	}
	return versions
}

func retentionClass(channel string) string {
	switch strings.ToLower(channel) {
	case "stable":
		return RetentionStable
	case "rc":
		return RetentionRelease
	case "nightly":
		return RetentionNightly
	default:
		return RetentionCanary
	}
}
