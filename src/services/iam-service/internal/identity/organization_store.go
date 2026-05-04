package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	identitystore "github.com/verself/iam-service/internal/store"
)

var organizationStoreTracer = otel.Tracer("iam-service/internal/identity/organization-store")

func (s SQLStore) GetOrganizationProfile(ctx context.Context, orgID, actorID string) (profile OrganizationProfile, err error) {
	ctx, span := organizationStoreTracer.Start(ctx, "iam.pg.organization_profile.get")
	defer finishOrganizationSpan(span, orgID, profile, err)
	if s.PG == nil {
		return OrganizationProfile{}, ErrStoreUnavailable
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return OrganizationProfile{}, fmt.Errorf("%w: org_id is required", ErrInvalidInput)
	}
	row, err := s.q().GetOrganizationProfile(ctx, identitystore.GetOrganizationProfileParams{OrgID: orgID})
	if err == nil {
		return organizationProfileFromGetRow(row)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrganizationProfile{}, fmt.Errorf("%w: get organization profile: %v", ErrStoreUnavailable, err)
	}
	return s.createDefaultOrganizationProfile(ctx, orgID, actorID)
}

func (s SQLStore) ListOrganizationMetadataByOrgIDs(ctx context.Context, orgIDs []string) (organizations []OrganizationMetadata, err error) {
	ctx, span := organizationStoreTracer.Start(ctx, "iam.pg.organization_metadata.list")
	defer func() {
		span.SetAttributes(attribute.Int("iam.organization.count", len(organizations)))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if s.PG == nil {
		return nil, ErrStoreUnavailable
	}
	orgIDs = normalizeOrganizationIDs(orgIDs)
	if len(orgIDs) == 0 {
		return nil, fmt.Errorf("%w: org_ids is required", ErrInvalidInput)
	}
	rows, err := s.q().ListOrganizationMetadataByOrgIDs(ctx, identitystore.ListOrganizationMetadataByOrgIDsParams{OrgIds: orgIDs})
	if err != nil {
		return nil, fmt.Errorf("%w: list organization metadata: %v", ErrStoreUnavailable, err)
	}
	organizations = make([]OrganizationMetadata, 0, len(rows))
	for _, row := range rows {
		organizations = append(organizations, OrganizationMetadata{
			OrgID:       row.OrgID,
			DisplayName: row.DisplayName,
			Slug:        row.Slug,
		})
	}
	return organizations, nil
}

func (s SQLStore) UpdateOrganizationProfile(ctx context.Context, principal Principal, input UpdateOrganizationRequest) (profile OrganizationProfile, err error) {
	ctx, span := organizationStoreTracer.Start(ctx, "iam.pg.organization_profile.update")
	defer finishOrganizationSpan(span, principal.OrgID, profile, err)
	if s.PG == nil {
		return OrganizationProfile{}, ErrStoreUnavailable
	}
	if err := principal.validate(); err != nil {
		return OrganizationProfile{}, err
	}
	input, err = normalizeUpdateOrganizationRequest(input)
	if err != nil {
		return OrganizationProfile{}, err
	}
	if _, err := s.GetOrganizationProfile(ctx, principal.OrgID, principal.Subject); err != nil {
		return OrganizationProfile{}, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return OrganizationProfile{}, fmt.Errorf("%w: begin organization profile update: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	oldRow, err := q.GetOrganizationProfileForUpdate(ctx, identitystore.GetOrganizationProfileForUpdateParams{OrgID: principal.OrgID})
	if err != nil {
		return OrganizationProfile{}, organizationProfileLoadError(err)
	}
	old, err := organizationProfileFromForUpdateRow(oldRow)
	if err != nil {
		return OrganizationProfile{}, err
	}
	if old.Version != input.Version {
		return OrganizationProfile{}, fmt.Errorf("%w: stale organization profile version", ErrOrganizationConflict)
	}
	if input.DisplayName == "" {
		input.DisplayName = old.DisplayName
	}
	if input.Slug == "" {
		input.Slug = old.Slug
	}
	if input.Slug != old.Slug {
		available, err := organizationSlugAvailable(ctx, q, principal.OrgID, input.Slug)
		if err != nil {
			return OrganizationProfile{}, err
		}
		if !available {
			return OrganizationProfile{}, fmt.Errorf("%w: organization slug is unavailable", ErrOrganizationConflict)
		}
		if err := q.InsertOrganizationSlugRedirect(ctx, identitystore.InsertOrganizationSlugRedirectParams{
			Slug:    old.Slug,
			OrgID:   old.OrgID,
			ActorID: principal.Subject,
		}); err != nil {
			return OrganizationProfile{}, fmt.Errorf("%w: insert organization slug redirect: %v", ErrStoreUnavailable, err)
		}
	}
	row, err := q.UpdateOrganizationProfile(ctx, identitystore.UpdateOrganizationProfileParams{
		OrgID:       principal.OrgID,
		Slug:        input.Slug,
		DisplayName: input.DisplayName,
		ActorID:     principal.Subject,
		Version:     input.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return OrganizationProfile{}, fmt.Errorf("%w: stale organization profile version", ErrOrganizationConflict)
	}
	if err != nil {
		if uniqueViolation(err) {
			return OrganizationProfile{}, fmt.Errorf("%w: organization slug is unavailable", ErrOrganizationConflict)
		}
		return OrganizationProfile{}, fmt.Errorf("%w: update organization profile: %v", ErrStoreUnavailable, err)
	}
	profile, err = organizationProfileFromUpdateRow(row)
	if err != nil {
		return OrganizationProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrganizationProfile{}, fmt.Errorf("%w: commit organization profile update: %v", ErrStoreUnavailable, err)
	}
	return profile, nil
}

func (s SQLStore) ResolveOrganizationProfile(ctx context.Context, input ResolveOrganizationRequest) (profile OrganizationProfile, err error) {
	ctx, span := organizationStoreTracer.Start(ctx, "iam.pg.organization_profile.resolve")
	defer finishOrganizationSpan(span, input.OrgID, profile, err)
	if s.PG == nil {
		return OrganizationProfile{}, ErrStoreUnavailable
	}
	input.OrgID = strings.TrimSpace(input.OrgID)
	input.Slug = normalizeSlug(input.Slug)
	if input.OrgID == "" && input.Slug == "" {
		return OrganizationProfile{}, fmt.Errorf("%w: org_id or slug is required", ErrInvalidInput)
	}
	if input.OrgID != "" {
		profile, err = s.GetOrganizationProfile(ctx, input.OrgID, "system:identity-resolve")
		if err != nil {
			return OrganizationProfile{}, err
		}
	} else {
		row, err := s.q().GetOrganizationProfileBySlug(ctx, identitystore.GetOrganizationProfileBySlugParams{Slug: input.Slug})
		if err == nil {
			profile, err = organizationProfileFromSlugRow(row)
		} else if errors.Is(err, pgx.ErrNoRows) {
			redirectRow, redirectErr := s.q().GetOrganizationProfileByRedirectSlug(ctx, identitystore.GetOrganizationProfileByRedirectSlugParams{Slug: input.Slug})
			if redirectErr != nil {
				return OrganizationProfile{}, organizationProfileLoadError(redirectErr)
			}
			profile, err = organizationProfileFromRedirectRow(redirectRow)
		} else {
			return OrganizationProfile{}, organizationProfileLoadError(err)
		}
		if err != nil {
			return OrganizationProfile{}, err
		}
	}
	if input.RequireActive && profile.State != OrganizationProfileStateActive {
		return OrganizationProfile{}, ErrOrganizationMissing
	}
	return profile, nil
}

func (s SQLStore) createDefaultOrganizationProfile(ctx context.Context, orgID, actorID string) (OrganizationProfile, error) {
	actorID = firstNonEmpty(actorID, "system:identity")
	displayName := "Organization " + orgID
	baseSlug := normalizeSlug(displayName)
	if baseSlug == "" {
		baseSlug = "organization"
	}
	suffix := shortStableSuffix(orgID)
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return OrganizationProfile{}, fmt.Errorf("%w: begin organization profile create: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	for attempt := 0; attempt < 16; attempt++ {
		slug := slugCandidate(baseSlug, suffix, attempt)
		row, err := q.CreateOrganizationProfile(ctx, identitystore.CreateOrganizationProfileParams{
			OrgID:       orgID,
			DisplayName: displayName,
			Slug:        slug,
			ActorID:     actorID,
		})
		if err == nil {
			profile, err := organizationProfileFromCreateRow(row)
			if err != nil {
				return OrganizationProfile{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return OrganizationProfile{}, fmt.Errorf("%w: commit organization profile create: %v", ErrStoreUnavailable, err)
			}
			return profile, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return OrganizationProfile{}, organizationProfileLoadError(err)
		}
		existingRow, loadErr := q.GetOrganizationProfile(ctx, identitystore.GetOrganizationProfileParams{OrgID: orgID})
		if loadErr == nil {
			existing, err := organizationProfileFromGetRow(existingRow)
			if err != nil {
				return OrganizationProfile{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return OrganizationProfile{}, fmt.Errorf("%w: commit existing organization profile: %v", ErrStoreUnavailable, err)
			}
			return existing, nil
		}
		if !errors.Is(loadErr, pgx.ErrNoRows) {
			return OrganizationProfile{}, organizationProfileLoadError(loadErr)
		}
	}
	return OrganizationProfile{}, fmt.Errorf("%w: unable to allocate organization slug", ErrOrganizationConflict)
}

func organizationProfileFromGetRow(row identitystore.GetOrganizationProfileRow) (OrganizationProfile, error) {
	return organizationProfileFromFields(row.OrgID, row.DisplayName, row.Slug, row.State, row.Version, row.CreatedBy, row.UpdatedBy, row.CreatedAt, row.UpdatedAt, row.RedirectedFrom)
}

func organizationProfileFromForUpdateRow(row identitystore.GetOrganizationProfileForUpdateRow) (OrganizationProfile, error) {
	return organizationProfileFromFields(row.OrgID, row.DisplayName, row.Slug, row.State, row.Version, row.CreatedBy, row.UpdatedBy, row.CreatedAt, row.UpdatedAt, row.RedirectedFrom)
}

func organizationProfileFromSlugRow(row identitystore.GetOrganizationProfileBySlugRow) (OrganizationProfile, error) {
	return organizationProfileFromFields(row.OrgID, row.DisplayName, row.Slug, row.State, row.Version, row.CreatedBy, row.UpdatedBy, row.CreatedAt, row.UpdatedAt, row.RedirectedFrom)
}

func organizationProfileFromRedirectRow(row identitystore.GetOrganizationProfileByRedirectSlugRow) (OrganizationProfile, error) {
	return organizationProfileFromFields(row.OrgID, row.DisplayName, row.Slug, row.State, row.Version, row.CreatedBy, row.UpdatedBy, row.CreatedAt, row.UpdatedAt, row.RedirectedFrom)
}

func organizationProfileFromCreateRow(row identitystore.CreateOrganizationProfileRow) (OrganizationProfile, error) {
	return organizationProfileFromFields(row.OrgID, row.DisplayName, row.Slug, row.State, row.Version, row.CreatedBy, row.UpdatedBy, row.CreatedAt, row.UpdatedAt, row.RedirectedFrom)
}

func organizationProfileFromUpdateRow(row identitystore.UpdateOrganizationProfileRow) (OrganizationProfile, error) {
	return organizationProfileFromFields(row.OrgID, row.DisplayName, row.Slug, row.State, row.Version, row.CreatedBy, row.UpdatedBy, row.CreatedAt, row.UpdatedAt, row.RedirectedFrom)
}

func organizationProfileFromFields(orgID, displayName, slug, state string, version int32, createdBy, updatedBy string, createdAt, updatedAt pgtype.Timestamptz, redirectedFrom string) (OrganizationProfile, error) {
	created, err := requiredTime(createdAt, "iam_organizations.created_at")
	if err != nil {
		return OrganizationProfile{}, err
	}
	updated, err := requiredTime(updatedAt, "iam_organizations.updated_at")
	if err != nil {
		return OrganizationProfile{}, err
	}
	return OrganizationProfile{
		OrgID:          orgID,
		DisplayName:    displayName,
		Slug:           slug,
		State:          OrganizationProfileState(state),
		Version:        version,
		CreatedBy:      createdBy,
		UpdatedBy:      updatedBy,
		CreatedAt:      created,
		UpdatedAt:      updated,
		RedirectedFrom: redirectedFrom,
	}, nil
}

func organizationProfileLoadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOrganizationMissing
	}
	return fmt.Errorf("%w: scan organization profile: %v", ErrStoreUnavailable, err)
}

func normalizeUpdateOrganizationRequest(input UpdateOrganizationRequest) (UpdateOrganizationRequest, error) {
	input.DisplayName = normalizeHumanText(input.DisplayName)
	input.Slug = normalizeSlug(input.Slug)
	if input.Version <= 0 {
		return UpdateOrganizationRequest{}, fmt.Errorf("%w: version must be positive", ErrInvalidInput)
	}
	if input.DisplayName != "" {
		if err := validateHumanText("display_name", input.DisplayName, 1, 120, 240); err != nil {
			return UpdateOrganizationRequest{}, err
		}
	}
	if input.Slug != "" {
		if err := validateSlug("slug", input.Slug); err != nil {
			return UpdateOrganizationRequest{}, err
		}
	}
	return input, nil
}

func normalizeHumanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	previousDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			previousDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !previousDash && b.Len() > 0 {
				b.WriteByte('-')
				previousDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func validateSlug(field, value string) error {
	if len(value) < 1 {
		return fmt.Errorf("%w: %s is required", ErrInvalidInput, field)
	}
	if len(value) > 80 {
		return fmt.Errorf("%w: %s is too long", ErrInvalidInput, field)
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("%w: %s contains unsupported characters", ErrInvalidInput, field)
	}
	return nil
}

func validateHumanText(field, value string, minRunes, maxRunes, maxBytes int) error {
	runes := []rune(value)
	if len(runes) < minRunes {
		return fmt.Errorf("%w: %s is required", ErrInvalidInput, field)
	}
	if len(runes) > maxRunes || len(value) > maxBytes {
		return fmt.Errorf("%w: %s is too long", ErrInvalidInput, field)
	}
	for _, r := range runes {
		if unicode.IsControl(r) || isBidiOverride(r) {
			return fmt.Errorf("%w: %s contains unsupported control text", ErrInvalidInput, field)
		}
	}
	return nil
}

func isBidiOverride(r rune) bool {
	return r == '\u202a' || r == '\u202b' || r == '\u202c' || r == '\u202d' || r == '\u202e' || r == '\u2066' || r == '\u2067' || r == '\u2068' || r == '\u2069'
}

func organizationSlugAvailable(ctx context.Context, q *identitystore.Queries, currentOrgID, slug string) (bool, error) {
	unavailable, err := q.OrganizationSlugUnavailable(ctx, identitystore.OrganizationSlugUnavailableParams{
		CandidateSlug: slug,
		CurrentOrgID:  currentOrgID,
	})
	if err != nil {
		return false, fmt.Errorf("%w: check organization slug: %v", ErrStoreUnavailable, err)
	}
	return !unavailable.Bool, nil
}

func slugCandidate(baseSlug, suffix string, attempt int) string {
	baseSlug = strings.Trim(baseSlug, "-")
	if attempt == 0 {
		if len(baseSlug) <= 80 {
			return baseSlug
		}
		return strings.Trim(baseSlug[:80], "-")
	}
	tail := "-" + suffix
	if attempt > 1 {
		tail = fmt.Sprintf("-%s-%d", suffix, attempt)
	}
	maxBase := 80 - len(tail)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(baseSlug) > maxBase {
		baseSlug = strings.Trim(baseSlug[:maxBase], "-")
	}
	return baseSlug + tail
}

func shortStableSuffix(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

func uniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func finishOrganizationSpan(span trace.Span, orgID string, profile OrganizationProfile, err error) {
	if span == nil {
		return
	}
	if orgID != "" {
		span.SetAttributes(attribute.String("verself.org_id", orgID))
	}
	if profile.OrgID != "" {
		span.SetAttributes(
			attribute.String("iam.org_slug", profile.Slug),
			attribute.String("iam.org_state", string(profile.State)),
			attribute.Int("iam.org_profile_version", int(profile.Version)),
		)
		if profile.RedirectedFrom != "" {
			span.SetAttributes(attribute.String("iam.org_slug_redirected_from", profile.RedirectedFrom))
		}
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
