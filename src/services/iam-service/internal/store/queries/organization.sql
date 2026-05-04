-- name: Ping :one
SELECT 1::int AS one;

-- name: GetOrganizationProfile :one
SELECT org_id, display_name, slug, state, version, created_by, updated_by, created_at, updated_at, ''::text AS redirected_from
FROM iam_organizations
WHERE org_id = sqlc.arg(org_id);

-- name: ListOrganizationMetadataByOrgIDs :many
SELECT org_id, display_name, slug
FROM iam_organizations
WHERE org_id = ANY(sqlc.arg(org_ids)::text[])
ORDER BY display_name, org_id;

-- name: GetOrganizationProfileForUpdate :one
SELECT org_id, display_name, slug, state, version, created_by, updated_by, created_at, updated_at, ''::text AS redirected_from
FROM iam_organizations
WHERE org_id = sqlc.arg(org_id)
FOR UPDATE;

-- name: GetOrganizationProfileBySlug :one
SELECT org_id, display_name, slug, state, version, created_by, updated_by, created_at, updated_at, ''::text AS redirected_from
FROM iam_organizations
WHERE slug = sqlc.arg(slug);

-- name: GetOrganizationProfileByRedirectSlug :one
SELECT o.org_id, o.display_name, o.slug, o.state, o.version, o.created_by, o.updated_by, o.created_at, o.updated_at, r.slug AS redirected_from
FROM iam_organization_slug_redirects r
JOIN iam_organizations o ON o.org_id = r.org_id
WHERE r.slug = sqlc.arg(slug);

-- name: CreateOrganizationProfile :one
INSERT INTO iam_organizations (org_id, display_name, slug, state, version, created_by, updated_by, created_at, updated_at)
SELECT sqlc.arg(org_id), sqlc.arg(display_name), sqlc.arg(slug), 'active', 1, sqlc.arg(actor_id), sqlc.arg(actor_id), now(), now()
WHERE NOT EXISTS (
    SELECT 1 FROM iam_organization_slug_redirects WHERE slug = sqlc.arg(slug)
)
ON CONFLICT DO NOTHING
RETURNING org_id, display_name, slug, state, version, created_by, updated_by, created_at, updated_at, ''::text AS redirected_from;

-- name: UpdateOrganizationProfile :one
UPDATE iam_organizations
SET slug = sqlc.arg(slug),
    display_name = sqlc.arg(display_name),
    version = version + 1,
    updated_by = sqlc.arg(actor_id),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND version = sqlc.arg(version)
RETURNING org_id, display_name, slug, state, version, created_by, updated_by, created_at, updated_at, ''::text AS redirected_from;

-- name: InsertOrganizationSlugRedirect :exec
INSERT INTO iam_organization_slug_redirects (slug, org_id, created_by, created_at)
VALUES (sqlc.arg(slug), sqlc.arg(org_id), sqlc.arg(actor_id), now())
ON CONFLICT DO NOTHING;

-- name: OrganizationSlugUnavailable :one
SELECT EXISTS (
    SELECT 1 FROM iam_organizations o WHERE o.slug = sqlc.arg(candidate_slug) AND o.org_id <> sqlc.arg(current_org_id)
) OR EXISTS (
    SELECT 1 FROM iam_organization_slug_redirects r WHERE r.slug = sqlc.arg(candidate_slug)
) AS unavailable;
