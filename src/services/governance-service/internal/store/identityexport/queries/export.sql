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
	           c.created_at,
	           c.created_by,
	           c.updated_at,
	           c.expires_at,
	           c.revoked_at,
	           c.revoked_by,
	           c.last_used_at
	    FROM iam_api_credentials c
	    WHERE c.org_id = sqlc.arg(org_id)
	    ORDER BY c.created_at,
	             c.credential_id
) t;
