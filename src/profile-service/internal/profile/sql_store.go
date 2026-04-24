package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
)

type SQLStore struct {
	PG  *pgxpool.Pool
	Now func() time.Time
}

func (s SQLStore) Ready(ctx context.Context) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	var one int
	if err := s.PG.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) Snapshot(ctx context.Context, principal Principal) (Snapshot, error) {
	if err := s.ensureSubject(ctx, s.PG, principal); err != nil {
		return Snapshot{}, err
	}
	return s.loadSnapshot(ctx, s.PG, principal.Subject)
}

func (s SQLStore) UpdateIdentity(ctx context.Context, principal Principal, input UpdateIdentityRequest, bearerToken string, writer IdentityWriter) (Snapshot, []string, error) {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	if err := s.ensureSubject(ctx, tx, principal); err != nil {
		return Snapshot{}, nil, err
	}
	old, err := s.loadIdentityForUpdate(ctx, tx, principal.Subject)
	if err != nil {
		return Snapshot{}, nil, err
	}
	if old.Version != input.Version {
		return Snapshot{}, nil, ErrConflict
	}

	// The row lock intentionally spans the identity write so two profile PATCHes cannot race across Zitadel.
	updated, err := writer.UpdateHumanProfile(ctx, principal.Subject, input, bearerToken)
	if err != nil {
		return Snapshot{}, nil, err
	}
	nextVersion := old.Version + 1
	if strings.TrimSpace(updated.Email) == "" {
		updated.Email = firstNonEmpty(old.Email, principal.Email)
	}
	updated.GivenName = input.GivenName
	updated.FamilyName = input.FamilyName
	updated.DisplayName = input.DisplayName
	updated.Version = nextVersion
	now := s.now()
	if updated.SyncedAt == nil {
		syncedAt := now
		updated.SyncedAt = &syncedAt
	}
	_, err = tx.Exec(ctx, `
UPDATE profile_subjects
SET email_cache = $2,
    given_name_cache = $3,
    family_name_cache = $4,
    display_name_cache = $5,
    identity_version = $6,
    identity_synced_at = $7,
    org_id = $8,
    updated_at = $9,
    tombstoned_at = NULL,
    tombstone_request_id = '',
    tombstoned_by = ''
WHERE subject_id = $1`,
		principal.Subject,
		updated.Email,
		updated.GivenName,
		updated.FamilyName,
		updated.DisplayName,
		nextVersion,
		*updated.SyncedAt,
		principal.OrgID,
		now,
	)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	changed := changedIdentityFields(old, updated)
	if err := s.insertOutbox(ctx, tx, "events.profile.subject.updated", principal.Subject, nextVersion, map[string]any{
		"occurred_at":    now.Format(time.RFC3339Nano),
		"subject_id":     principal.Subject,
		"org_id":         principal.OrgID,
		"version":        nextVersion,
		"changed_fields": changed,
	}); err != nil {
		return Snapshot{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	snapshot, err := s.loadSnapshot(ctx, s.PG, principal.Subject)
	if err != nil {
		return Snapshot{}, nil, err
	}
	return snapshot, changed, nil
}

func (s SQLStore) PutPreferences(ctx context.Context, principal Principal, input PutPreferencesRequest) (Snapshot, []string, error) {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	if err := s.ensureSubject(ctx, tx, principal); err != nil {
		return Snapshot{}, nil, err
	}
	old, err := s.loadPreferences(ctx, tx, principal.Subject)
	if err != nil {
		return Snapshot{}, nil, err
	}
	now := s.now()
	next := Preferences{
		Version:        old.Version + 1,
		Locale:         input.Locale,
		Timezone:       input.Timezone,
		TimeDisplay:    input.TimeDisplay,
		Theme:          input.Theme,
		DefaultSurface: input.DefaultSurface,
		UpdatedAt:      now,
		UpdatedBy:      principal.Subject,
	}
	var tag pgconn.CommandTag
	if input.Version == 0 {
		if old.Version != 0 {
			return Snapshot{}, nil, ErrConflict
		}
		tag, err = tx.Exec(ctx, `
INSERT INTO profile_preferences (
    subject_id, version, locale, timezone, time_display, theme, default_surface, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			principal.Subject,
			next.Version,
			next.Locale,
			next.Timezone,
			next.TimeDisplay,
			next.Theme,
			next.DefaultSurface,
			next.UpdatedAt,
			next.UpdatedBy,
		)
	} else {
		if old.Version != input.Version {
			return Snapshot{}, nil, ErrConflict
		}
		tag, err = tx.Exec(ctx, `
UPDATE profile_preferences
SET version = version + 1,
    locale = $2,
    timezone = $3,
    time_display = $4,
    theme = $5,
    default_surface = $6,
    updated_at = $7,
    updated_by = $8
WHERE subject_id = $1 AND version = $9`,
			principal.Subject,
			next.Locale,
			next.Timezone,
			next.TimeDisplay,
			next.Theme,
			next.DefaultSurface,
			next.UpdatedAt,
			next.UpdatedBy,
			input.Version,
		)
	}
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Snapshot{}, nil, ErrConflict
	}
	changed := changedPreferenceFields(old, next)
	if err := s.insertOutbox(ctx, tx, "events.profile.preferences.updated", principal.Subject, next.Version, map[string]any{
		"occurred_at":    now.Format(time.RFC3339Nano),
		"subject_id":     principal.Subject,
		"org_id":         principal.OrgID,
		"version":        next.Version,
		"changed_fields": changed,
	}); err != nil {
		return Snapshot{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	snapshot, err := s.loadSnapshot(ctx, s.PG, principal.Subject)
	if err != nil {
		return Snapshot{}, nil, err
	}
	return snapshot, changed, nil
}

func (s SQLStore) OrgExport(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error) {
	if manifest, ok, err := s.loadExistingDataRights(ctx, input.RequestID); ok || err != nil {
		return manifest, err
	}
	subjects, err := s.exportRows(ctx, `
SELECT subject_id, org_id, email_cache, given_name_cache, family_name_cache, display_name_cache, identity_version, identity_synced_at, created_at, updated_at, tombstoned_at
FROM profile_subjects
WHERE org_id = $1
ORDER BY subject_id`, input.OrgID)
	if err != nil {
		return DataRightsManifest{}, err
	}
	preferences, err := s.exportRows(ctx, `
SELECT p.subject_id, s.org_id, p.version, p.locale, p.timezone, p.time_display, p.theme, p.default_surface, p.updated_at, p.updated_by
FROM profile_preferences p
JOIN profile_subjects s ON s.subject_id = p.subject_id
WHERE s.org_id = $1
ORDER BY p.subject_id`, input.OrgID)
	if err != nil {
		return DataRightsManifest{}, err
	}
	manifest := DataRightsManifest{
		RequestID:   input.RequestID,
		RequestType: "org_export",
		Status:      "completed",
		OrgID:       input.OrgID,
		Artifacts: []DataRightsArtifact{
			artifactFor("profile/subjects.jsonl", subjects.rows, subjects.content),
			artifactFor("profile/preferences.jsonl", preferences.rows, preferences.content),
		},
		RecordCounts: map[string]string{
			"subjects":    strconv.Itoa(subjects.rows),
			"preferences": strconv.Itoa(preferences.rows),
		},
		CompletedAt: s.now(),
	}
	return manifest, s.insertDataRights(ctx, input, manifest)
}

func (s SQLStore) SubjectExport(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error) {
	if manifest, ok, err := s.loadExistingDataRights(ctx, input.RequestID); ok || err != nil {
		return manifest, err
	}
	subjects, err := s.exportRows(ctx, `
SELECT subject_id, org_id, email_cache, given_name_cache, family_name_cache, display_name_cache, identity_version, identity_synced_at, created_at, updated_at, tombstoned_at
FROM profile_subjects
WHERE subject_id = $1
ORDER BY subject_id`, input.SubjectID)
	if err != nil {
		return DataRightsManifest{}, err
	}
	preferences, err := s.exportRows(ctx, `
SELECT subject_id, version, locale, timezone, time_display, theme, default_surface, updated_at, updated_by
FROM profile_preferences
WHERE subject_id = $1
ORDER BY subject_id`, input.SubjectID)
	if err != nil {
		return DataRightsManifest{}, err
	}
	manifest := DataRightsManifest{
		RequestID:   input.RequestID,
		RequestType: "subject_export",
		Status:      "completed",
		SubjectID:   input.SubjectID,
		Artifacts: []DataRightsArtifact{
			artifactFor("profile/subjects.jsonl", subjects.rows, subjects.content),
			artifactFor("profile/preferences.jsonl", preferences.rows, preferences.content),
		},
		RecordCounts: map[string]string{
			"subjects":    strconv.Itoa(subjects.rows),
			"preferences": strconv.Itoa(preferences.rows),
		},
		CompletedAt: s.now(),
	}
	return manifest, s.insertDataRights(ctx, input, manifest)
}

func (s SQLStore) SubjectErasure(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error) {
	if manifest, ok, err := s.loadExistingDataRights(ctx, input.RequestID); ok || err != nil {
		return manifest, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	prefsTag, err := tx.Exec(ctx, `DELETE FROM profile_preferences WHERE subject_id = $1`, input.SubjectID)
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	now := s.now()
	subjectTag, err := tx.Exec(ctx, `
UPDATE profile_subjects
SET email_cache = '',
    given_name_cache = '',
    family_name_cache = '',
    display_name_cache = '',
    identity_synced_at = NULL,
    updated_at = $2,
    tombstoned_at = $2,
    tombstone_request_id = $3,
    tombstoned_by = $4
WHERE subject_id = $1`,
		input.SubjectID, now, input.RequestID, input.RequestedBy)
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if subjectTag.RowsAffected() == 0 {
		_, err = tx.Exec(ctx, `
INSERT INTO profile_subjects (
    subject_id, org_id, created_at, updated_at, tombstoned_at, tombstone_request_id, tombstoned_by
) VALUES ($1, '', $2, $2, $2, $3, $4)`,
			input.SubjectID, now, input.RequestID, input.RequestedBy)
		if err != nil {
			return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
	}
	manifest := DataRightsManifest{
		RequestID:   input.RequestID,
		RequestType: "subject_erasure",
		Status:      "completed",
		SubjectID:   input.SubjectID,
		ErasureActions: []DataRightsErasureAction{
			{Name: "delete_profile_preferences", Rows: strconv.FormatInt(prefsTag.RowsAffected(), 10)},
			{Name: "tombstone_profile_subject", Rows: "1"},
		},
		RetainedCategories: []DataRightsRetainedCategory{
			{Category: "governance_audit", Reason: "Immutable audit rows are retained by governance-service and are not rewritten by profile-service."},
		},
		RecordCounts: map[string]string{
			"deleted_preferences": strconv.FormatInt(prefsTag.RowsAffected(), 10),
			"tombstoned_subjects": "1",
		},
		CompletedAt: now,
	}
	if err := s.insertDataRightsTx(ctx, tx, input, manifest); err != nil {
		return DataRightsManifest{}, err
	}
	if err := s.insertOutbox(ctx, tx, "events.profile.subject.tombstoned", input.SubjectID, 0, map[string]any{
		"occurred_at": now.Format(time.RFC3339Nano),
		"subject_id":  input.SubjectID,
		"request_id":  input.RequestID,
	}); err != nil {
		return DataRightsManifest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return manifest, nil
}

func (s SQLStore) DataRightsStatus(ctx context.Context, requestID string) (DataRightsManifest, error) {
	manifest, ok, err := s.loadExistingDataRights(ctx, requestID)
	if err != nil {
		return DataRightsManifest{}, err
	}
	if !ok {
		return DataRightsManifest{}, ErrNotFound
	}
	return manifest, nil
}

type queryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s SQLStore) ensureSubject(ctx context.Context, q queryer, principal Principal) error {
	now := s.now()
	_, err := q.Exec(ctx, `
INSERT INTO profile_subjects (subject_id, org_id, email_cache, created_at, updated_at)
VALUES ($1, $2, $3, $4, $4)
ON CONFLICT (subject_id) DO UPDATE
SET org_id = CASE
        WHEN profile_subjects.org_id = '' THEN EXCLUDED.org_id
        ELSE profile_subjects.org_id
    END,
    updated_at = EXCLUDED.updated_at`,
		principal.Subject, principal.OrgID, principal.Email, now)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) loadSnapshot(ctx context.Context, q queryer, subjectID string) (Snapshot, error) {
	identity, orgID, err := s.loadIdentity(ctx, q, subjectID)
	if err != nil {
		return Snapshot{}, err
	}
	preferences, err := s.loadPreferences(ctx, q, subjectID)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		SubjectID:   subjectID,
		OrgID:       orgID,
		Identity:    identity,
		Preferences: preferences,
	}, nil
}

func (s SQLStore) loadIdentity(ctx context.Context, q queryer, subjectID string) (IdentitySummary, string, error) {
	var (
		orgID    string
		summary  IdentitySummary
		syncedAt pgtype.Timestamptz
	)
	err := q.QueryRow(ctx, `
SELECT org_id, email_cache, given_name_cache, family_name_cache, display_name_cache, identity_version, identity_synced_at
FROM profile_subjects
WHERE subject_id = $1`, subjectID).Scan(
		&orgID,
		&summary.Email,
		&summary.GivenName,
		&summary.FamilyName,
		&summary.DisplayName,
		&summary.Version,
		&syncedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return IdentitySummary{}, "", ErrNotFound
	}
	if err != nil {
		return IdentitySummary{}, "", fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if syncedAt.Valid {
		t := syncedAt.Time.UTC()
		summary.SyncedAt = &t
	}
	return summary, orgID, nil
}

func (s SQLStore) loadIdentityForUpdate(ctx context.Context, q queryer, subjectID string) (IdentitySummary, error) {
	var (
		summary  IdentitySummary
		syncedAt pgtype.Timestamptz
	)
	err := q.QueryRow(ctx, `
SELECT email_cache, given_name_cache, family_name_cache, display_name_cache, identity_version, identity_synced_at
FROM profile_subjects
WHERE subject_id = $1
FOR UPDATE`, subjectID).Scan(
		&summary.Email,
		&summary.GivenName,
		&summary.FamilyName,
		&summary.DisplayName,
		&summary.Version,
		&syncedAt,
	)
	if err != nil {
		return IdentitySummary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if syncedAt.Valid {
		t := syncedAt.Time.UTC()
		summary.SyncedAt = &t
	}
	return summary, nil
}

func (s SQLStore) loadPreferences(ctx context.Context, q queryer, subjectID string) (Preferences, error) {
	var preferences Preferences
	err := q.QueryRow(ctx, `
SELECT version, locale, timezone, time_display, theme, default_surface, updated_at, updated_by
FROM profile_preferences
WHERE subject_id = $1`, subjectID).Scan(
		&preferences.Version,
		&preferences.Locale,
		&preferences.Timezone,
		&preferences.TimeDisplay,
		&preferences.Theme,
		&preferences.DefaultSurface,
		&preferences.UpdatedAt,
		&preferences.UpdatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultPreferences(s.now()), nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	preferences.UpdatedAt = preferences.UpdatedAt.UTC()
	return preferences, nil
}

type exportData struct {
	rows    int
	content []byte
}

func (s SQLStore) exportRows(ctx context.Context, query string, args ...any) (exportData, error) {
	rows, err := s.PG.Query(ctx, query, args...)
	if err != nil {
		return exportData{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	var buf bytes.Buffer
	count := 0
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return exportData{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		fields := rows.FieldDescriptions()
		record := make(map[string]any, len(fields))
		for i, field := range fields {
			record[string(field.Name)] = normalizeExportValue(values[i])
		}
		line, err := json.Marshal(record)
		if err != nil {
			return exportData{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
		count++
	}
	if err := rows.Err(); err != nil {
		return exportData{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return exportData{rows: count, content: buf.Bytes()}, nil
}

func normalizeExportValue(value any) any {
	switch v := value.(type) {
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case nil:
		return ""
	default:
		return v
	}
}

func (s SQLStore) loadExistingDataRights(ctx context.Context, requestID string) (DataRightsManifest, bool, error) {
	var data []byte
	err := s.PG.QueryRow(ctx, `SELECT manifest FROM profile_data_rights_requests WHERE request_id = $1`, requestID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return DataRightsManifest{}, false, nil
	}
	if err != nil {
		return DataRightsManifest{}, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	var manifest DataRightsManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return DataRightsManifest{}, false, fmt.Errorf("%w: decode data rights manifest: %v", ErrStoreUnavailable, err)
	}
	return manifest, true, nil
}

func (s SQLStore) insertDataRights(ctx context.Context, input DataRightsRequest, manifest DataRightsManifest) error {
	return s.insertDataRightsTx(ctx, s.PG, input, manifest)
}

func (s SQLStore) insertDataRightsTx(ctx context.Context, q queryer, input DataRightsRequest, manifest DataRightsManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("%w: marshal data rights manifest: %v", ErrStoreUnavailable, err)
	}
	tag, err := q.Exec(ctx, `
INSERT INTO profile_data_rights_requests (
    request_id, request_type, org_id, subject_id, requested_at, requested_by, status, manifest, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
ON CONFLICT (request_id) DO NOTHING`,
		manifest.RequestID,
		manifest.RequestType,
		firstNonEmpty(manifest.OrgID, input.OrgID),
		firstNonEmpty(manifest.SubjectID, input.SubjectID),
		input.RequestedAt.UTC(),
		input.RequestedBy,
		manifest.Status,
		data,
		s.now(),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	eventSubject := "events.profile.data_rights.exported"
	if manifest.RequestType == "subject_erasure" {
		eventSubject = "events.profile.data_rights.erased"
	}
	aggregateID := firstNonEmpty(manifest.SubjectID, input.SubjectID, manifest.OrgID, input.OrgID)
	if err := s.insertOutbox(ctx, q, eventSubject, aggregateID, 0, map[string]any{
		"occurred_at":  s.now().Format(time.RFC3339Nano),
		"request_id":   manifest.RequestID,
		"request_type": manifest.RequestType,
		"org_id":       firstNonEmpty(manifest.OrgID, input.OrgID),
		"subject_id":   firstNonEmpty(manifest.SubjectID, input.SubjectID),
		"status":       manifest.Status,
	}); err != nil {
		return err
	}
	return nil
}

func (s SQLStore) insertOutbox(ctx context.Context, q queryer, subject string, aggregateSubjectID string, aggregateVersion int32, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	eventID := uuid.New()
	payload["event_id"] = eventID.String()
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		payload["trace_id"] = sc.TraceID().String()
		payload["span_id"] = sc.SpanID().String()
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%w: marshal profile outbox payload: %v", ErrStoreUnavailable, err)
	}
	_, err = q.Exec(ctx, `
INSERT INTO profile_domain_event_outbox (
    event_id, aggregate_subject_id, aggregate_version, subject, payload, traceparent, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		eventID.String(),
		aggregateSubjectID,
		aggregateVersion,
		subject,
		data,
		traceparentFromContext(ctx),
		s.now(),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func traceparentFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	flags := "00"
	if sc.TraceFlags().IsSampled() {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", sc.TraceID().String(), sc.SpanID().String(), flags)
}

func (s SQLStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func changedIdentityFields(old, next IdentitySummary) []string {
	fields := []string{}
	if old.GivenName != next.GivenName {
		fields = append(fields, "given_name")
	}
	if old.FamilyName != next.FamilyName {
		fields = append(fields, "family_name")
	}
	if old.DisplayName != next.DisplayName {
		fields = append(fields, "display_name")
	}
	if old.Email != next.Email {
		fields = append(fields, "email_cache")
	}
	return fields
}

func changedPreferenceFields(old, next Preferences) []string {
	fields := []string{}
	if old.Locale != next.Locale {
		fields = append(fields, "locale")
	}
	if old.Timezone != next.Timezone {
		fields = append(fields, "timezone")
	}
	if old.TimeDisplay != next.TimeDisplay {
		fields = append(fields, "time_display")
	}
	if old.Theme != next.Theme {
		fields = append(fields, "theme")
	}
	if old.DefaultSurface != next.DefaultSurface {
		fields = append(fields, "default_surface")
	}
	return fields
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}
