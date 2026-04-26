-- name: GetIdentity :one
SELECT org_id, email_cache, given_name_cache, family_name_cache, display_name_cache, identity_version, identity_synced_at
FROM profile_subjects
WHERE subject_id = $1;

-- name: GetIdentityForUpdate :one
SELECT email_cache, given_name_cache, family_name_cache, display_name_cache, identity_version, identity_synced_at
FROM profile_subjects
WHERE subject_id = $1
FOR UPDATE;

-- name: GetPreferences :one
SELECT version, locale, timezone, time_display, theme, default_surface, updated_at, updated_by
FROM profile_preferences
WHERE subject_id = $1;

-- name: GetDataRightsManifest :one
SELECT manifest
FROM profile_data_rights_requests
WHERE request_id = $1;

-- name: OrgExportSubjects :many
SELECT subject_id, org_id, email_cache, given_name_cache, family_name_cache, display_name_cache, identity_version, identity_synced_at, created_at, updated_at, tombstoned_at
FROM profile_subjects
WHERE org_id = $1
ORDER BY subject_id;

-- name: OrgExportPreferences :many
SELECT p.subject_id, s.org_id, p.version, p.locale, p.timezone, p.time_display, p.theme, p.default_surface, p.updated_at, p.updated_by
FROM profile_preferences p
JOIN profile_subjects s ON s.subject_id = p.subject_id
WHERE s.org_id = $1
ORDER BY p.subject_id;

-- name: SubjectExportSubjects :many
SELECT subject_id, org_id, email_cache, given_name_cache, family_name_cache, display_name_cache, identity_version, identity_synced_at, created_at, updated_at, tombstoned_at
FROM profile_subjects
WHERE subject_id = $1
ORDER BY subject_id;

-- name: SubjectExportPreferences :many
SELECT subject_id, version, locale, timezone, time_display, theme, default_surface, updated_at, updated_by
FROM profile_preferences
WHERE subject_id = $1
ORDER BY subject_id;
