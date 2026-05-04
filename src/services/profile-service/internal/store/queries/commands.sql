-- name: Ping :one
SELECT 1::int AS one;

-- name: EnsureSubject :exec
INSERT INTO profile_subjects (subject_id, org_id, email_cache, created_at, updated_at)
VALUES ($1, $2, $3, $4, $4)
ON CONFLICT (subject_id) DO UPDATE
SET org_id = CASE
        WHEN profile_subjects.org_id = '' THEN EXCLUDED.org_id
        ELSE profile_subjects.org_id
    END,
    updated_at = EXCLUDED.updated_at;

-- name: UpdateIdentityCache :exec
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
WHERE subject_id = $1;

-- name: InsertPreferences :execrows
INSERT INTO profile_preferences (
    subject_id, version, locale, timezone, time_display, theme, default_surface, updated_at, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: UpdatePreferences :execrows
UPDATE profile_preferences
SET version = version + 1,
    locale = $2,
    timezone = $3,
    time_display = $4,
    theme = $5,
    default_surface = $6,
    updated_at = $7,
    updated_by = $8
WHERE subject_id = $1 AND version = $9;

-- name: DeletePreferencesForSubject :execrows
DELETE FROM profile_preferences
WHERE subject_id = $1;

-- name: TombstoneSubject :execrows
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
WHERE subject_id = $1;

-- name: InsertTombstonedSubject :exec
INSERT INTO profile_subjects (
    subject_id, org_id, created_at, updated_at, tombstoned_at, tombstone_request_id, tombstoned_by
) VALUES ($1, '', $2, $2, $2, $3, $4);

-- name: InsertDataRightsRequest :execrows
INSERT INTO profile_data_rights_requests (
    request_id, request_type, org_id, subject_id, requested_at, requested_by, status, manifest, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
ON CONFLICT (request_id) DO NOTHING;

-- name: InsertOutboxEvent :exec
INSERT INTO profile_domain_event_outbox (
    event_id, aggregate_subject_id, aggregate_version, subject, payload, traceparent, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);
