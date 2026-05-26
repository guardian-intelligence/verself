package release

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	releasestore "github.com/verself/release-service/internal/store"
)

type SQLStore struct {
	PG *pgxpool.Pool
}

func (s SQLStore) Ready(ctx context.Context) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if _, err := releasestore.New(s.PG).Ping(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) CreatePackage(ctx context.Context, req RegisterPackageRequest, now time.Time) (Package, error) {
	q, err := s.queries()
	if err != nil {
		return Package{}, err
	}
	row, err := q.CreatePackage(ctx, releasestore.CreatePackageParams{
		PackageID:            uuid.New(),
		OrgID:                req.OrgID,
		PackageName:          req.PackageName,
		PackageKind:          req.PackageKind,
		RepoPath:             req.RepoPath,
		BazelTarget:          req.BazelTarget,
		OwnerTeam:            req.OwnerTeam,
		DefaultVersionScheme: req.DefaultVersionScheme,
		Visibility:           req.Visibility,
		PolicyRef:            req.PolicyRef,
		CreatedAt:            pgTime(now),
		UpdatedAt:            pgTime(now),
	})
	if err != nil {
		return Package{}, storeError(err)
	}
	return packageFromStore(row), nil
}

func (s SQLStore) GetPackageByName(ctx context.Context, orgID string, packageName string) (Package, error) {
	q, err := s.queries()
	if err != nil {
		return Package{}, err
	}
	row, err := q.GetPackageByName(ctx, releasestore.GetPackageByNameParams{OrgID: orgID, PackageName: packageName})
	if err != nil {
		return Package{}, storeError(err)
	}
	return packageFromStore(row), nil
}

func (s SQLStore) CreateVersion(ctx context.Context, req CreateVersionRequest, now time.Time) (Version, error) {
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Version{}, err
	}
	defer rollback(ctx, tx)
	row, err := q.CreateReleaseVersion(ctx, releasestore.CreateReleaseVersionParams{
		ReleaseVersionID: uuid.New(),
		PackageID:        req.PackageID,
		CanonicalVersion: req.CanonicalVersion,
		VersionScheme:    req.VersionScheme,
		VersionKind:      req.VersionKind,
		ReleaseSequence:  req.ReleaseSequence,
		SourceRepository: req.SourceRepository,
		SourceCommit:     req.SourceCommit,
		SourceRef:        req.SourceRef,
		State:            StateSubmitted,
		LifecycleStatus:  LifecycleActive,
		CreatedAt:        pgTime(now),
		UpdatedAt:        pgTime(now),
	})
	if err != nil {
		return Version{}, storeError(err)
	}
	version := versionFromStore(row)
	pkg, err := q.GetPackage(ctx, releasestore.GetPackageParams{PackageID: req.PackageID})
	if err != nil {
		return Version{}, storeError(err)
	}
	if err := createDefaultInstallOptions(ctx, q, packageFromStore(pkg), version, now); err != nil {
		return Version{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return version, nil
}

func (s SQLStore) GetRelease(ctx context.Context, releaseVersionID uuid.UUID) (ReleaseContext, error) {
	q, err := s.queries()
	if err != nil {
		return ReleaseContext{}, err
	}
	row, err := q.GetReleaseContext(ctx, releasestore.GetReleaseContextParams{ReleaseVersionID: releaseVersionID})
	if err != nil {
		return ReleaseContext{}, storeError(err)
	}
	return ReleaseContext{
		Package: Package{
			PackageID:            row.PackageID,
			OrgID:                row.OrgID,
			PackageName:          row.PackageName,
			PackageKind:          row.PackageKind,
			RepoPath:             row.RepoPath,
			BazelTarget:          row.BazelTarget,
			OwnerTeam:            row.OwnerTeam,
			DefaultVersionScheme: row.DefaultVersionScheme,
			Visibility:           row.Visibility,
			PolicyRef:            row.PolicyRef,
			CreatedAt:            timeFromPG(row.PackageCreatedAt),
			UpdatedAt:            timeFromPG(row.PackageUpdatedAt),
		},
		Version: Version{
			ReleaseVersionID: row.ReleaseVersionID,
			PackageID:        row.PackageID,
			CanonicalVersion: row.CanonicalVersion,
			VersionScheme:    row.VersionScheme,
			VersionKind:      row.VersionKind,
			ReleaseSequence:  row.ReleaseSequence,
			SourceRepository: row.SourceRepository,
			SourceCommit:     row.SourceCommit,
			SourceRef:        row.SourceRef,
			State:            row.State,
			LifecycleStatus:  row.LifecycleStatus,
			CreatedAt:        timeFromPG(row.ReleaseCreatedAt),
			UpdatedAt:        timeFromPG(row.ReleaseUpdatedAt),
		},
	}, nil
}

func (s SQLStore) AttachArtifact(ctx context.Context, req AttachArtifactRequest, now time.Time) (Artifact, error) {
	q, err := s.queries()
	if err != nil {
		return Artifact{}, err
	}
	row, err := q.CreateArtifact(ctx, releasestore.CreateArtifactParams{
		ArtifactID:       uuid.New(),
		ReleaseVersionID: req.ReleaseVersionID,
		ArtifactRole:     req.ArtifactRole,
		PlatformOs:       req.PlatformOS,
		PlatformArch:     req.PlatformArch,
		OciRepository:    req.OCIRepository,
		OciDigest:        req.OCIDigest,
		OciMediaType:     req.OCIMediaType,
		OciSizeBytes:     req.OCISizeBytes,
		RegistryUrl:      req.RegistryURL,
		CreatedAt:        pgTime(now),
	})
	if err != nil {
		return Artifact{}, storeError(err)
	}
	return artifactFromStore(row), nil
}

func (s SQLStore) AttachEvidence(ctx context.Context, req AttachEvidenceRequest, now time.Time) (Evidence, error) {
	q, err := s.queries()
	if err != nil {
		return Evidence{}, err
	}
	artifact, err := q.GetArtifact(ctx, releasestore.GetArtifactParams{ArtifactID: req.ArtifactID})
	if err != nil {
		return Evidence{}, storeError(err)
	}
	if artifact.ReleaseVersionID != req.ReleaseVersionID {
		return Evidence{}, fmt.Errorf("%w: artifact belongs to a different release", ErrInvalid)
	}
	row, err := q.CreateEvidence(ctx, releasestore.CreateEvidenceParams{
		EvidenceID:        uuid.New(),
		ReleaseVersionID:  req.ReleaseVersionID,
		ArtifactID:        req.ArtifactID,
		EvidenceKind:      req.EvidenceKind,
		PredicateType:     req.PredicateType,
		SubjectDigest:     req.SubjectDigest,
		DocumentDigest:    req.DocumentDigest,
		OciReferrerDigest: req.OCIReferrerDigest,
		VerificationState: "verified",
		VerifiedAt:        pgTime(now),
		CreatedAt:         pgTime(now),
	})
	if err != nil {
		return Evidence{}, storeError(err)
	}
	return evidenceFromStore(row), nil
}

func (s SQLStore) Verify(ctx context.Context, req VerifyRequest, now time.Time) (VerifyResult, error) {
	tx, q, err := s.begin(ctx)
	if err != nil {
		return VerifyResult{}, err
	}
	defer rollback(ctx, tx)
	release, err := q.GetReleaseVersion(ctx, releasestore.GetReleaseVersionParams{ReleaseVersionID: req.ReleaseVersionID})
	if err != nil {
		return VerifyResult{}, storeError(err)
	}
	decision, reason := "allowed", "required release evidence is present"
	for _, kind := range []string{EvidenceCosign, EvidenceSLSA, EvidenceSBOM} {
		count, err := q.CountVerifiedEvidenceByKind(ctx, releasestore.CountVerifiedEvidenceByKindParams{ReleaseVersionID: req.ReleaseVersionID, EvidenceKind: kind})
		if err != nil {
			return VerifyResult{}, storeError(err)
		}
		if count == 0 {
			decision = "denied"
			reason = "missing verified " + kind
			break
		}
	}
	check, err := q.CreatePolicyCheck(ctx, releasestore.CreatePolicyCheckParams{
		PolicyCheckID:    uuid.New(),
		ReleaseVersionID: req.ReleaseVersionID,
		PolicyRef:        req.PolicyRef,
		PolicyDigest:     req.PolicyDigest,
		Decision:         decision,
		DecisionReason:   reason,
		BuilderID:        req.BuilderID,
		SignerIdentity:   req.SignerIdentity,
		SourceCommit:     release.SourceCommit,
		CheckedAt:        pgTime(now),
		CheckedBy:        req.CheckedBy,
		CreatedAt:        pgTime(now),
	})
	if err != nil {
		return VerifyResult{}, storeError(err)
	}
	nextState := StateVerified
	if check.Decision != "allowed" {
		nextState = StateDenied
	}
	updated, err := q.UpdateReleaseState(ctx, releasestore.UpdateReleaseStateParams{
		ReleaseVersionID: req.ReleaseVersionID,
		State:            nextState,
		UpdatedAt:        pgTime(now),
	})
	if err != nil {
		return VerifyResult{}, storeError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return VerifyResult{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return VerifyResult{Version: versionFromStore(updated), Decision: decision, Reason: reason}, nil
}

func (s SQLStore) Approve(ctx context.Context, req ApproveRequest, now time.Time) (Version, error) {
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Version{}, err
	}
	defer rollback(ctx, tx)
	release, err := q.GetReleaseVersion(ctx, releasestore.GetReleaseVersionParams{ReleaseVersionID: req.ReleaseVersionID})
	if err != nil {
		return Version{}, storeError(err)
	}
	if release.State != StateVerified && release.State != StateApproved {
		return Version{}, fmt.Errorf("%w: release must be verified before approval", ErrStateConflict)
	}
	if _, err := q.LatestAllowedPolicyCheck(ctx, releasestore.LatestAllowedPolicyCheckParams{ReleaseVersionID: req.ReleaseVersionID}); err != nil {
		return Version{}, storeError(err)
	}
	updated, err := q.UpdateReleaseState(ctx, releasestore.UpdateReleaseStateParams{
		ReleaseVersionID: req.ReleaseVersionID,
		State:            StateApproved,
		UpdatedAt:        pgTime(now),
	})
	if err != nil {
		return Version{}, storeError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return versionFromStore(updated), nil
}

func (s SQLStore) PromoteChannel(ctx context.Context, req PromoteChannelRequest, now time.Time) (Channel, error) {
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Channel{}, err
	}
	defer rollback(ctx, tx)
	release, err := q.GetReleaseVersion(ctx, releasestore.GetReleaseVersionParams{ReleaseVersionID: req.ReleaseVersionID})
	if err != nil {
		return Channel{}, storeError(err)
	}
	if release.PackageID != req.PackageID {
		return Channel{}, fmt.Errorf("%w: release belongs to a different package", ErrInvalid)
	}
	if release.State != StateApproved && release.State != StatePublished {
		return Channel{}, fmt.Errorf("%w: release must be approved before channel promotion", ErrStateConflict)
	}
	check, err := q.LatestAllowedPolicyCheck(ctx, releasestore.LatestAllowedPolicyCheckParams{ReleaseVersionID: req.ReleaseVersionID})
	if err != nil {
		return Channel{}, storeError(err)
	}
	from := req.ReleaseVersionID
	existing, err := q.GetChannelByPackageAndName(ctx, releasestore.GetChannelByPackageAndNameParams{PackageID: req.PackageID, ChannelName: req.ChannelName})
	if err == nil {
		from = existing.CurrentReleaseVersionID
	} else if !errors.Is(storeError(err), ErrNotFound) {
		return Channel{}, storeError(err)
	}
	row, err := q.UpsertChannel(ctx, releasestore.UpsertChannelParams{
		ChannelID:               uuid.New(),
		PackageID:               req.PackageID,
		ChannelName:             req.ChannelName,
		ChannelKind:             req.ChannelKind,
		Visibility:              req.Visibility,
		CurrentReleaseVersionID: req.ReleaseVersionID,
		PolicyRef:               req.PolicyRef,
		UpdatedBy:               req.ActorID,
		UpdatedAt:               pgTime(now),
		CreatedAt:               pgTime(now),
	})
	if err != nil {
		return Channel{}, storeError(err)
	}
	if _, err := q.CreateChannelEvent(ctx, releasestore.CreateChannelEventParams{
		ChannelEventID:       uuid.New(),
		ChannelID:            row.ChannelID,
		FromReleaseVersionID: from,
		ToReleaseVersionID:   req.ReleaseVersionID,
		EventKind:            "promoted",
		ReasonCode:           req.Reason,
		PolicyCheckID:        check.PolicyCheckID,
		Actor:                req.ActorID,
		CreatedAt:            pgTime(now),
	}); err != nil {
		return Channel{}, storeError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Channel{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return channelFromStore(row), nil
}

func (s SQLStore) ListInstallOptions(ctx context.Context, releaseVersionID uuid.UUID) ([]InstallOption, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListInstallOptions(ctx, releasestore.ListInstallOptionsParams{ReleaseVersionID: releaseVersionID})
	if err != nil {
		return nil, storeError(err)
	}
	options := make([]InstallOption, 0, len(rows))
	for _, row := range rows {
		options = append(options, installOptionFromStore(row))
	}
	return options, nil
}

func (s SQLStore) CreatePublication(ctx context.Context, req CreatePublicationRequest, now time.Time) (Publication, error) {
	q, err := s.queries()
	if err != nil {
		return Publication{}, err
	}
	release, err := q.GetReleaseVersion(ctx, releasestore.GetReleaseVersionParams{ReleaseVersionID: req.ReleaseVersionID})
	if err != nil {
		return Publication{}, storeError(err)
	}
	if release.State != StateApproved && release.State != StatePublished {
		return Publication{}, fmt.Errorf("%w: release must be approved before publication", ErrStateConflict)
	}
	row, err := q.CreatePublication(ctx, releasestore.CreatePublicationParams{
		PublicationID:    uuid.New(),
		ReleaseVersionID: req.ReleaseVersionID,
		ProviderKind:     req.ProviderKind,
		ProviderPackage:  req.ProviderPackage,
		ProviderChannel:  req.ProviderChannel,
		RequestedBy:      req.RequestedBy,
		State:            StateApproved,
		IdempotencyKey:   req.IdempotencyKey,
		CreatedAt:        pgTime(now),
		UpdatedAt:        pgTime(now),
	})
	if err != nil {
		return Publication{}, storeError(err)
	}
	return publicationFromCreateStore(row), nil
}

func (s SQLStore) DispatchPublication(ctx context.Context, publicationID uuid.UUID, distribution Distribution, actor string, now time.Time) (Publication, error) {
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Publication{}, err
	}
	defer rollback(ctx, tx)
	pubctx, err := q.GetPublicationContext(ctx, releasestore.GetPublicationContextParams{PublicationID: publicationID})
	if err != nil {
		return Publication{}, storeError(err)
	}
	if pubctx.ReleaseState != StateApproved && pubctx.ReleaseState != StatePublished {
		return Publication{}, fmt.Errorf("%w: release must be approved before publication dispatch", ErrStateConflict)
	}
	if pubctx.ProviderKind == ProviderOCI && distribution == nil {
		return Publication{}, fmt.Errorf("%w: distribution client is required", ErrDistribution)
	}
	if pubctx.PublicationState == StateComplete {
		row, err := q.GetPublication(ctx, releasestore.GetPublicationParams{PublicationID: publicationID})
		if err != nil {
			return Publication{}, storeError(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Publication{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		return publicationFromStore(row), nil
	}
	if _, err := q.UpdatePublicationState(ctx, releasestore.UpdatePublicationStateParams{PublicationID: publicationID, State: "dispatching", UpdatedAt: pgTime(now)}); err != nil {
		return Publication{}, storeError(err)
	}
	artifact, err := q.GetPrimaryArtifactForRelease(ctx, releasestore.GetPrimaryArtifactForReleaseParams{ReleaseVersionID: pubctx.ReleaseVersionID})
	if err != nil {
		return Publication{}, storeError(err)
	}
	if pubctx.ProviderKind == ProviderOCI {
		check, err := q.LatestAllowedPolicyCheck(ctx, releasestore.LatestAllowedPolicyCheckParams{ReleaseVersionID: pubctx.ReleaseVersionID})
		if err != nil {
			return Publication{}, storeError(err)
		}
		target, err := distribution.PromoteTarget(ctx, DistributionPromoteTargetRequest{
			PackageName:    pubctx.PackageName,
			ChannelName:    pubctx.ProviderChannel,
			ArtifactDigest: artifact.OciDigest,
			PlatformOS:     artifact.PlatformOs,
			PlatformArch:   artifact.PlatformArch,
			PolicyRef:      check.PolicyRef,
			PromotedBy:     actor,
			Reason:         "release publication dispatch",
			IdempotencyKey: "release-publication-" + publicationID.String(),
		})
		if err != nil {
			row, updateErr := q.UpdatePublicationState(ctx, releasestore.UpdatePublicationStateParams{PublicationID: publicationID, State: StateRetryableFailed, UpdatedAt: pgTime(now)})
			if updateErr != nil {
				return Publication{}, storeError(updateErr)
			}
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return Publication{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, commitErr)
			}
			return publicationFromStore(row), err
		}
		if _, err := q.CreatePublicationReceipt(ctx, releasestore.CreatePublicationReceiptParams{
			ReceiptID:           uuid.New(),
			PublicationID:       publicationID,
			ProviderKind:        pubctx.ProviderKind,
			ProviderResourceUrl: distributionProviderResourceURL(target),
			ProviderVersion:     pubctx.CanonicalVersion,
			ProviderDigest:      target.ArtifactDigest,
			ReceiptDigest:       target.ArtifactDigest,
			ReconciledAt:        pgTime(now),
			CreatedAt:           pgTime(now),
		}); err != nil {
			return Publication{}, storeError(err)
		}
	}
	row, err := q.UpdatePublicationState(ctx, releasestore.UpdatePublicationStateParams{PublicationID: publicationID, State: StateComplete, UpdatedAt: pgTime(now)})
	if err != nil {
		return Publication{}, storeError(err)
	}
	if _, err := q.UpdateReleaseState(ctx, releasestore.UpdateReleaseStateParams{ReleaseVersionID: pubctx.ReleaseVersionID, State: StatePublished, UpdatedAt: pgTime(now)}); err != nil {
		return Publication{}, storeError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Publication{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return publicationFromStore(row), nil
}

func (s SQLStore) RecordReceipt(ctx context.Context, req RecordReceiptRequest, now time.Time) (Publication, error) {
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Publication{}, err
	}
	defer rollback(ctx, tx)
	pub, err := q.GetPublication(ctx, releasestore.GetPublicationParams{PublicationID: req.PublicationID})
	if err != nil {
		return Publication{}, storeError(err)
	}
	if _, err := q.CreatePublicationReceipt(ctx, releasestore.CreatePublicationReceiptParams{
		ReceiptID:           uuid.New(),
		PublicationID:       req.PublicationID,
		ProviderKind:        pub.ProviderKind,
		ProviderResourceUrl: req.ProviderResourceURL,
		ProviderVersion:     req.ProviderVersion,
		ProviderDigest:      req.ProviderDigest,
		ReceiptDigest:       req.ReceiptDigest,
		ReconciledAt:        pgTime(now),
		CreatedAt:           pgTime(now),
	}); err != nil {
		return Publication{}, storeError(err)
	}
	updated, err := q.UpdatePublicationState(ctx, releasestore.UpdatePublicationStateParams{PublicationID: req.PublicationID, State: StateComplete, UpdatedAt: pgTime(now)})
	if err != nil {
		return Publication{}, storeError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Publication{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return publicationFromStore(updated), nil
}

func (s SQLStore) ReconcilePublication(ctx context.Context, publicationID uuid.UUID, now time.Time) (Publication, error) {
	tx, q, err := s.begin(ctx)
	if err != nil {
		return Publication{}, err
	}
	defer rollback(ctx, tx)
	count, err := q.CountReceiptsForPublication(ctx, releasestore.CountReceiptsForPublicationParams{PublicationID: publicationID})
	if err != nil {
		return Publication{}, storeError(err)
	}
	state := StateComplete
	if count == 0 {
		state = "provider_pending"
	}
	row, err := q.UpdatePublicationState(ctx, releasestore.UpdatePublicationStateParams{PublicationID: publicationID, State: state, UpdatedAt: pgTime(now)})
	if err != nil {
		return Publication{}, storeError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Publication{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return publicationFromStore(row), nil
}

func (s SQLStore) ListDuePublications(ctx context.Context, limit int32) ([]Publication, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListDuePublications(ctx, releasestore.ListDuePublicationsParams{LimitCount: limit})
	if err != nil {
		return nil, storeError(err)
	}
	out := make([]Publication, 0, len(rows))
	for _, row := range rows {
		out = append(out, publicationFromStore(row))
	}
	return out, nil
}

func (s SQLStore) begin(ctx context.Context) (pgx.Tx, *releasestore.Queries, error) {
	if s.PG == nil {
		return nil, nil, ErrStoreUnavailable
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return tx, releasestore.New(tx), nil
}

func (s SQLStore) queries() (*releasestore.Queries, error) {
	if s.PG == nil {
		return nil, ErrStoreUnavailable
	}
	return releasestore.New(s.PG), nil
}

func createDefaultInstallOptions(ctx context.Context, q *releasestore.Queries, pkg Package, version Version, now time.Time) error {
	sourceCommand := "bazelisk run //" + pkg.RepoPath + ":install -- --bin-dir=$HOME/.local/bin"
	if _, err := q.CreateInstallOption(ctx, releasestore.CreateInstallOptionParams{
		InstallOptionID:      uuid.New(),
		ReleaseVersionID:     version.ReleaseVersionID,
		OptionKind:           "source",
		Status:               "active",
		DisplayOrder:         1,
		CommandTemplate:      sourceCommand,
		VerificationTemplate: "bazelisk test //" + pkg.RepoPath + ":all",
		OciRepository:        "",
		OciDigest:            "",
		ProviderKind:         "",
		ProviderRef:          "",
		CreatedAt:            pgTime(now),
		UpdatedAt:            pgTime(now),
	}); err != nil {
		return storeError(err)
	}
	if _, err := q.CreateInstallOption(ctx, releasestore.CreateInstallOptionParams{
		InstallOptionID:      uuid.New(),
		ReleaseVersionID:     version.ReleaseVersionID,
		OptionKind:           "third_party",
		Status:               "deferred",
		DisplayOrder:         3,
		CommandTemplate:      "brew install verself/tap/" + pkg.PackageName,
		VerificationTemplate: "brew info verself/tap/" + pkg.PackageName,
		OciRepository:        "",
		OciDigest:            "",
		ProviderKind:         ProviderHomebrew,
		ProviderRef:          "verself/tap/" + pkg.PackageName,
		CreatedAt:            pgTime(now),
		UpdatedAt:            pgTime(now),
	}); err != nil {
		return storeError(err)
	}
	return nil
}

func distributionProviderResourceURL(target DistributionTarget) string {
	if strings.TrimSpace(target.DownloadURL) != "" {
		return strings.TrimSpace(target.DownloadURL)
	}
	return strings.TrimSpace(target.PublicOCIReference)
}

func rollback(ctx context.Context, tx pgx.Tx) {
	if tx != nil {
		_ = tx.Rollback(ctx)
	}
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

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func packageFromStore(row releasestore.ReleasePackage) Package {
	return Package{
		PackageID:            row.PackageID,
		OrgID:                row.OrgID,
		PackageName:          row.PackageName,
		PackageKind:          row.PackageKind,
		RepoPath:             row.RepoPath,
		BazelTarget:          row.BazelTarget,
		OwnerTeam:            row.OwnerTeam,
		DefaultVersionScheme: row.DefaultVersionScheme,
		Visibility:           row.Visibility,
		PolicyRef:            row.PolicyRef,
		CreatedAt:            timeFromPG(row.CreatedAt),
		UpdatedAt:            timeFromPG(row.UpdatedAt),
	}
}

func versionFromStore(row releasestore.ReleaseVersion) Version {
	return Version{
		ReleaseVersionID: row.ReleaseVersionID,
		PackageID:        row.PackageID,
		CanonicalVersion: row.CanonicalVersion,
		VersionScheme:    row.VersionScheme,
		VersionKind:      row.VersionKind,
		ReleaseSequence:  row.ReleaseSequence,
		SourceRepository: row.SourceRepository,
		SourceCommit:     row.SourceCommit,
		SourceRef:        row.SourceRef,
		State:            row.State,
		LifecycleStatus:  row.LifecycleStatus,
		CreatedAt:        timeFromPG(row.CreatedAt),
		UpdatedAt:        timeFromPG(row.UpdatedAt),
	}
}

func artifactFromStore(row releasestore.ReleaseArtifact) Artifact {
	return Artifact{
		ArtifactID:       row.ArtifactID,
		ReleaseVersionID: row.ReleaseVersionID,
		ArtifactRole:     row.ArtifactRole,
		PlatformOS:       row.PlatformOs,
		PlatformArch:     row.PlatformArch,
		OCIRepository:    row.OciRepository,
		OCIDigest:        row.OciDigest,
		OCIMediaType:     row.OciMediaType,
		OCISizeBytes:     row.OciSizeBytes,
		RegistryURL:      row.RegistryUrl,
		CreatedAt:        timeFromPG(row.CreatedAt),
	}
}

func evidenceFromStore(row releasestore.ReleaseEvidence) Evidence {
	return Evidence{
		EvidenceID:        row.EvidenceID,
		ReleaseVersionID:  row.ReleaseVersionID,
		ArtifactID:        row.ArtifactID,
		EvidenceKind:      row.EvidenceKind,
		PredicateType:     row.PredicateType,
		SubjectDigest:     row.SubjectDigest,
		DocumentDigest:    row.DocumentDigest,
		OCIReferrerDigest: row.OciReferrerDigest,
		VerificationState: row.VerificationState,
		VerifiedAt:        timeFromPG(row.VerifiedAt),
		CreatedAt:         timeFromPG(row.CreatedAt),
	}
}

func installOptionFromStore(row releasestore.ReleaseInstallOption) InstallOption {
	return InstallOption{
		InstallOptionID:      row.InstallOptionID,
		ReleaseVersionID:     row.ReleaseVersionID,
		OptionKind:           row.OptionKind,
		Status:               row.Status,
		DisplayOrder:         row.DisplayOrder,
		CommandTemplate:      row.CommandTemplate,
		VerificationTemplate: row.VerificationTemplate,
		OCIRepository:        row.OciRepository,
		OCIDigest:            row.OciDigest,
		ProviderKind:         row.ProviderKind,
		ProviderRef:          row.ProviderRef,
		CreatedAt:            timeFromPG(row.CreatedAt),
		UpdatedAt:            timeFromPG(row.UpdatedAt),
	}
}

func publicationFromStore(row releasestore.ReleasePublication) Publication {
	return Publication{
		PublicationID:    row.PublicationID,
		ReleaseVersionID: row.ReleaseVersionID,
		ProviderKind:     row.ProviderKind,
		ProviderPackage:  row.ProviderPackage,
		ProviderChannel:  row.ProviderChannel,
		RequestedBy:      row.RequestedBy,
		State:            row.State,
		IdempotencyKey:   row.IdempotencyKey,
		CreatedAt:        timeFromPG(row.CreatedAt),
		UpdatedAt:        timeFromPG(row.UpdatedAt),
	}
}

func publicationFromCreateStore(row releasestore.CreatePublicationRow) Publication {
	return Publication{
		PublicationID:    row.PublicationID,
		ReleaseVersionID: row.ReleaseVersionID,
		ProviderKind:     row.ProviderKind,
		ProviderPackage:  row.ProviderPackage,
		ProviderChannel:  row.ProviderChannel,
		RequestedBy:      row.RequestedBy,
		State:            row.State,
		IdempotencyKey:   row.IdempotencyKey,
		CreatedAt:        timeFromPG(row.CreatedAt),
		UpdatedAt:        timeFromPG(row.UpdatedAt),
	}
}

func channelFromStore(row releasestore.ReleaseChannel) Channel {
	return Channel{
		ChannelID:               row.ChannelID,
		PackageID:               row.PackageID,
		ChannelName:             row.ChannelName,
		ChannelKind:             row.ChannelKind,
		Visibility:              row.Visibility,
		CurrentReleaseVersionID: row.CurrentReleaseVersionID,
		PolicyRef:               row.PolicyRef,
		UpdatedBy:               row.UpdatedBy,
		UpdatedAt:               timeFromPG(row.UpdatedAt),
		CreatedAt:               timeFromPG(row.CreatedAt),
	}
}
