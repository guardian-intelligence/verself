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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	profilestore "github.com/verself/profile-service/internal/store"
)

type SQLStore struct {
	PG  *pgxpool.Pool
	Now func() time.Time
}

func (s SQLStore) q() *profilestore.Queries {
	return profilestore.New(s.PG)
}

func (s SQLStore) Ready(ctx context.Context) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if _, err := s.q().Ping(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) Snapshot(ctx context.Context, principal Principal) (Snapshot, error) {
	q := s.q()
	if err := s.ensureSubject(ctx, q, principal); err != nil {
		return Snapshot{}, err
	}
	return s.loadSnapshot(ctx, q, principal.Subject)
}

func (s SQLStore) UpdateIdentity(ctx context.Context, principal Principal, input UpdateIdentityRequest, bearerToken string, writer IdentityWriter) (Snapshot, []string, error) {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := profilestore.New(tx)
	if err := s.ensureSubject(ctx, q, principal); err != nil {
		return Snapshot{}, nil, err
	}
	old, err := s.loadIdentityForUpdate(ctx, q, principal.Subject)
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
	if err := q.UpdateIdentityCache(ctx, profilestore.UpdateIdentityCacheParams{
		SubjectID:        principal.Subject,
		EmailCache:       updated.Email,
		GivenNameCache:   updated.GivenName,
		FamilyNameCache:  updated.FamilyName,
		DisplayNameCache: updated.DisplayName,
		IdentityVersion:  nextVersion,
		IdentitySyncedAt: timestamptz(*updated.SyncedAt),
		OrgID:            principal.OrgID,
		UpdatedAt:        timestamptz(now),
	}); err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	changed := changedIdentityFields(old, updated)
	if err := s.insertOutbox(ctx, q, "events.profile.subject.updated", principal.Subject, nextVersion, map[string]any{
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
	snapshot, err := s.loadSnapshot(ctx, s.q(), principal.Subject)
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
	q := profilestore.New(tx)
	if err := s.ensureSubject(ctx, q, principal); err != nil {
		return Snapshot{}, nil, err
	}
	old, err := s.loadPreferences(ctx, q, principal.Subject)
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
	var rowsAffected int64
	if input.Version == 0 {
		if old.Version != 0 {
			return Snapshot{}, nil, ErrConflict
		}
		rowsAffected, err = q.InsertPreferences(ctx, profilestore.InsertPreferencesParams{
			SubjectID:      principal.Subject,
			Version:        next.Version,
			Locale:         next.Locale,
			Timezone:       next.Timezone,
			TimeDisplay:    next.TimeDisplay,
			Theme:          next.Theme,
			DefaultSurface: next.DefaultSurface,
			UpdatedAt:      timestamptz(next.UpdatedAt),
			UpdatedBy:      next.UpdatedBy,
		})
	} else {
		if old.Version != input.Version {
			return Snapshot{}, nil, ErrConflict
		}
		rowsAffected, err = q.UpdatePreferences(ctx, profilestore.UpdatePreferencesParams{
			SubjectID:      principal.Subject,
			Locale:         next.Locale,
			Timezone:       next.Timezone,
			TimeDisplay:    next.TimeDisplay,
			Theme:          next.Theme,
			DefaultSurface: next.DefaultSurface,
			UpdatedAt:      timestamptz(next.UpdatedAt),
			UpdatedBy:      next.UpdatedBy,
			Version:        input.Version,
		})
	}
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if rowsAffected != 1 {
		return Snapshot{}, nil, ErrConflict
	}
	changed := changedPreferenceFields(old, next)
	if err := s.insertOutbox(ctx, q, "events.profile.preferences.updated", principal.Subject, next.Version, map[string]any{
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
	snapshot, err := s.loadSnapshot(ctx, s.q(), principal.Subject)
	if err != nil {
		return Snapshot{}, nil, err
	}
	return snapshot, changed, nil
}

func (s SQLStore) OrgExport(ctx context.Context, input DataRightsRequest) (DataRightsManifest, error) {
	q := s.q()
	if manifest, ok, err := s.loadExistingDataRights(ctx, q, input.RequestID); ok || err != nil {
		return manifest, err
	}
	subjectRows, err := q.OrgExportSubjects(ctx, profilestore.OrgExportSubjectsParams{OrgID: input.OrgID})
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	subjects, err := exportRowsToJSONL(subjectRows, orgExportSubjectRecord)
	if err != nil {
		return DataRightsManifest{}, err
	}
	preferenceRows, err := q.OrgExportPreferences(ctx, profilestore.OrgExportPreferencesParams{OrgID: input.OrgID})
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	preferences, err := exportRowsToJSONL(preferenceRows, orgExportPreferenceRecord)
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
	q := s.q()
	if manifest, ok, err := s.loadExistingDataRights(ctx, q, input.RequestID); ok || err != nil {
		return manifest, err
	}
	subjectRows, err := q.SubjectExportSubjects(ctx, profilestore.SubjectExportSubjectsParams{SubjectID: input.SubjectID})
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	subjects, err := exportRowsToJSONL(subjectRows, subjectExportSubjectRecord)
	if err != nil {
		return DataRightsManifest{}, err
	}
	preferenceRows, err := q.SubjectExportPreferences(ctx, profilestore.SubjectExportPreferencesParams{SubjectID: input.SubjectID})
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	preferences, err := exportRowsToJSONL(preferenceRows, subjectExportPreferenceRecord)
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
	q := s.q()
	if manifest, ok, err := s.loadExistingDataRights(ctx, q, input.RequestID); ok || err != nil {
		return manifest, err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q = profilestore.New(tx)
	deletedPreferences, err := q.DeletePreferencesForSubject(ctx, profilestore.DeletePreferencesForSubjectParams{SubjectID: input.SubjectID})
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	now := s.now()
	tombstonedSubjects, err := q.TombstoneSubject(ctx, profilestore.TombstoneSubjectParams{
		SubjectID:          input.SubjectID,
		UpdatedAt:          timestamptz(now),
		TombstoneRequestID: input.RequestID,
		TombstonedBy:       input.RequestedBy,
	})
	if err != nil {
		return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if tombstonedSubjects == 0 {
		if err := q.InsertTombstonedSubject(ctx, profilestore.InsertTombstonedSubjectParams{
			SubjectID:          input.SubjectID,
			CreatedAt:          timestamptz(now),
			TombstoneRequestID: input.RequestID,
			TombstonedBy:       input.RequestedBy,
		}); err != nil {
			return DataRightsManifest{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
	}
	manifest := DataRightsManifest{
		RequestID:   input.RequestID,
		RequestType: "subject_erasure",
		Status:      "completed",
		SubjectID:   input.SubjectID,
		ErasureActions: []DataRightsErasureAction{
			{Name: "delete_profile_preferences", Rows: strconv.FormatInt(deletedPreferences, 10)},
			{Name: "tombstone_profile_subject", Rows: "1"},
		},
		RetainedCategories: []DataRightsRetainedCategory{
			{Category: "governance_audit", Reason: "Immutable audit rows are retained by governance-service and are not rewritten by profile-service."},
		},
		RecordCounts: map[string]string{
			"deleted_preferences": strconv.FormatInt(deletedPreferences, 10),
			"tombstoned_subjects": "1",
		},
		CompletedAt: now,
	}
	if err := s.insertDataRightsTx(ctx, q, input, manifest); err != nil {
		return DataRightsManifest{}, err
	}
	if err := s.insertOutbox(ctx, q, "events.profile.subject.tombstoned", input.SubjectID, 0, map[string]any{
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
	manifest, ok, err := s.loadExistingDataRights(ctx, s.q(), requestID)
	if err != nil {
		return DataRightsManifest{}, err
	}
	if !ok {
		return DataRightsManifest{}, ErrNotFound
	}
	return manifest, nil
}

func (s SQLStore) ensureSubject(ctx context.Context, q *profilestore.Queries, principal Principal) error {
	now := s.now()
	if err := q.EnsureSubject(ctx, profilestore.EnsureSubjectParams{
		SubjectID:  principal.Subject,
		OrgID:      principal.OrgID,
		EmailCache: principal.Email,
		CreatedAt:  timestamptz(now),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) loadSnapshot(ctx context.Context, q *profilestore.Queries, subjectID string) (Snapshot, error) {
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

func (s SQLStore) loadIdentity(ctx context.Context, q *profilestore.Queries, subjectID string) (IdentitySummary, string, error) {
	row, err := q.GetIdentity(ctx, profilestore.GetIdentityParams{SubjectID: subjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return IdentitySummary{}, "", ErrNotFound
	}
	if err != nil {
		return IdentitySummary{}, "", fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return identityFromRow(row), row.OrgID, nil
}

func (s SQLStore) loadIdentityForUpdate(ctx context.Context, q *profilestore.Queries, subjectID string) (IdentitySummary, error) {
	row, err := q.GetIdentityForUpdate(ctx, profilestore.GetIdentityForUpdateParams{SubjectID: subjectID})
	if err != nil {
		return IdentitySummary{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return identityFromLockedRow(row), nil
}

func (s SQLStore) loadPreferences(ctx context.Context, q *profilestore.Queries, subjectID string) (Preferences, error) {
	row, err := q.GetPreferences(ctx, profilestore.GetPreferencesParams{SubjectID: subjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultPreferences(s.now()), nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return preferencesFromRow(row)
}

func identityFromRow(row profilestore.GetIdentityRow) IdentitySummary {
	return identityFromFields(
		row.EmailCache,
		row.GivenNameCache,
		row.FamilyNameCache,
		row.DisplayNameCache,
		row.IdentityVersion,
		row.IdentitySyncedAt,
	)
}

func identityFromLockedRow(row profilestore.GetIdentityForUpdateRow) IdentitySummary {
	return identityFromFields(
		row.EmailCache,
		row.GivenNameCache,
		row.FamilyNameCache,
		row.DisplayNameCache,
		row.IdentityVersion,
		row.IdentitySyncedAt,
	)
}

func identityFromFields(email, givenName, familyName, displayName string, version int32, syncedAt pgtype.Timestamptz) IdentitySummary {
	summary := IdentitySummary{
		Version:     version,
		Email:       email,
		GivenName:   givenName,
		FamilyName:  familyName,
		DisplayName: displayName,
	}
	if syncedAt.Valid {
		t := syncedAt.Time.UTC()
		summary.SyncedAt = &t
	}
	return summary
}

func preferencesFromRow(row profilestore.GetPreferencesRow) (Preferences, error) {
	updatedAt, err := requiredTime(row.UpdatedAt)
	if err != nil {
		return Preferences{}, err
	}
	return Preferences{
		Version:        row.Version,
		Locale:         row.Locale,
		Timezone:       row.Timezone,
		TimeDisplay:    row.TimeDisplay,
		Theme:          row.Theme,
		DefaultSurface: row.DefaultSurface,
		UpdatedAt:      updatedAt,
		UpdatedBy:      row.UpdatedBy,
	}, nil
}

type exportData struct {
	rows    int
	content []byte
}

func exportRowsToJSONL[T any](rows []T, record func(T) map[string]any) (exportData, error) {
	var buf bytes.Buffer
	for _, row := range rows {
		line, err := json.Marshal(record(row))
		if err != nil {
			return exportData{}, fmt.Errorf("%w: marshal data rights row: %v", ErrStoreUnavailable, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return exportData{rows: len(rows), content: buf.Bytes()}, nil
}

func orgExportSubjectRecord(row profilestore.OrgExportSubjectsRow) map[string]any {
	return exportSubjectRecord(
		row.SubjectID,
		row.OrgID,
		row.EmailCache,
		row.GivenNameCache,
		row.FamilyNameCache,
		row.DisplayNameCache,
		row.IdentityVersion,
		row.IdentitySyncedAt,
		row.CreatedAt,
		row.UpdatedAt,
		row.TombstonedAt,
	)
}

func subjectExportSubjectRecord(row profilestore.SubjectExportSubjectsRow) map[string]any {
	return exportSubjectRecord(
		row.SubjectID,
		row.OrgID,
		row.EmailCache,
		row.GivenNameCache,
		row.FamilyNameCache,
		row.DisplayNameCache,
		row.IdentityVersion,
		row.IdentitySyncedAt,
		row.CreatedAt,
		row.UpdatedAt,
		row.TombstonedAt,
	)
}

func exportSubjectRecord(subjectID, orgID, email, givenName, familyName, displayName string, identityVersion int32, identitySyncedAt, createdAt, updatedAt, tombstonedAt pgtype.Timestamptz) map[string]any {
	return map[string]any{
		"subject_id":         subjectID,
		"org_id":             orgID,
		"email_cache":        email,
		"given_name_cache":   givenName,
		"family_name_cache":  familyName,
		"display_name_cache": displayName,
		"identity_version":   identityVersion,
		"identity_synced_at": exportValue(identitySyncedAt),
		"created_at":         exportValue(createdAt),
		"updated_at":         exportValue(updatedAt),
		"tombstoned_at":      exportValue(tombstonedAt),
	}
}

func orgExportPreferenceRecord(row profilestore.OrgExportPreferencesRow) map[string]any {
	return map[string]any{
		"subject_id":      row.SubjectID,
		"org_id":          row.OrgID,
		"version":         row.Version,
		"locale":          row.Locale,
		"timezone":        row.Timezone,
		"time_display":    row.TimeDisplay,
		"theme":           row.Theme,
		"default_surface": row.DefaultSurface,
		"updated_at":      exportValue(row.UpdatedAt),
		"updated_by":      row.UpdatedBy,
	}
}

func subjectExportPreferenceRecord(row profilestore.ProfilePreference) map[string]any {
	return map[string]any{
		"subject_id":      row.SubjectID,
		"version":         row.Version,
		"locale":          row.Locale,
		"timezone":        row.Timezone,
		"time_display":    row.TimeDisplay,
		"theme":           row.Theme,
		"default_surface": row.DefaultSurface,
		"updated_at":      exportValue(row.UpdatedAt),
		"updated_by":      row.UpdatedBy,
	}
}

func exportValue(value any) any {
	switch v := value.(type) {
	case pgtype.Timestamptz:
		if !v.Valid {
			return ""
		}
		return v.Time.UTC().Format(time.RFC3339Nano)
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case nil:
		return ""
	default:
		return v
	}
}

func (s SQLStore) loadExistingDataRights(ctx context.Context, q *profilestore.Queries, requestID string) (DataRightsManifest, bool, error) {
	data, err := q.GetDataRightsManifest(ctx, profilestore.GetDataRightsManifestParams{RequestID: requestID})
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
	return s.insertDataRightsTx(ctx, s.q(), input, manifest)
}

func (s SQLStore) insertDataRightsTx(ctx context.Context, q *profilestore.Queries, input DataRightsRequest, manifest DataRightsManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("%w: marshal data rights manifest: %v", ErrStoreUnavailable, err)
	}
	now := s.now()
	rowsAffected, err := q.InsertDataRightsRequest(ctx, profilestore.InsertDataRightsRequestParams{
		RequestID:   manifest.RequestID,
		RequestType: manifest.RequestType,
		OrgID:       firstNonEmpty(manifest.OrgID, input.OrgID),
		SubjectID:   firstNonEmpty(manifest.SubjectID, input.SubjectID),
		RequestedAt: timestamptz(input.RequestedAt.UTC()),
		RequestedBy: input.RequestedBy,
		Status:      manifest.Status,
		Manifest:    data,
		CreatedAt:   timestamptz(now),
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if rowsAffected == 0 {
		return nil
	}
	eventSubject := "events.profile.data_rights.exported"
	if manifest.RequestType == "subject_erasure" {
		eventSubject = "events.profile.data_rights.erased"
	}
	aggregateID := firstNonEmpty(manifest.SubjectID, input.SubjectID, manifest.OrgID, input.OrgID)
	if err := s.insertOutbox(ctx, q, eventSubject, aggregateID, 0, map[string]any{
		"occurred_at":  now.Format(time.RFC3339Nano),
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

func (s SQLStore) insertOutbox(ctx context.Context, q *profilestore.Queries, subject string, aggregateSubjectID string, aggregateVersion int32, payload map[string]any) error {
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
	if err := q.InsertOutboxEvent(ctx, profilestore.InsertOutboxEventParams{
		EventID:            eventID,
		AggregateSubjectID: aggregateSubjectID,
		AggregateVersion:   aggregateVersion,
		Subject:            subject,
		Payload:            data,
		Traceparent:        traceparentFromContext(ctx),
		CreatedAt:          timestamptz(s.now()),
	}); err != nil {
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

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func requiredTime(value pgtype.Timestamptz) (time.Time, error) {
	if !value.Valid {
		return time.Time{}, fmt.Errorf("%w: required timestamp was null", ErrStoreUnavailable)
	}
	return value.Time.UTC(), nil
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}
