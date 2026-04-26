-- name: ExportIdentityMemberCapabilitiesJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT *
    FROM identity_member_capabilities
    WHERE org_id = sqlc.arg(org_id)
) t;

-- name: ExportIdentityAPICredentialsJSONL :many
SELECT row_to_json(t)::text AS row_json
FROM (
    SELECT c.credential_id,
           c.org_id,
           c.subject_id,
           c.client_id,
           c.display_name,
           c.auth_method,
           c.status,
           c.policy_version_at_issue,
           c.created_at,
           c.created_by,
           c.updated_at,
           c.expires_at,
           c.revoked_at,
           c.revoked_by,
           c.last_used_at,
           COALESCE(array_agg(p.permission ORDER BY p.permission) FILTER (WHERE p.permission IS NOT NULL), '{}') AS permissions
    FROM identity_api_credentials c
    LEFT JOIN identity_api_credential_permissions p ON p.credential_id = c.credential_id
    WHERE c.org_id = sqlc.arg(org_id)
    GROUP BY c.credential_id
    ORDER BY c.created_at,
             c.credential_id
) t;
