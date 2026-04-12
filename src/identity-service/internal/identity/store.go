package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type SQLStore struct {
	DB *sql.DB
}

type storedPolicyDocument struct {
	Roles []PolicyRole `json:"roles"`
}

func (s SQLStore) GetPolicy(ctx context.Context, orgID, actor string) (PolicyDocument, error) {
	if s.DB == nil {
		return PolicyDocument{}, ErrStoreUnavailable
	}
	var (
		raw       []byte
		version   int32
		updatedAt time.Time
		updatedBy string
	)
	err := s.DB.QueryRowContext(ctx, `
SELECT document, version, updated_at, updated_by
FROM identity_policy_documents
WHERE org_id = $1
`, orgID).Scan(&raw, &version, &updatedAt, &updatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		policy := DefaultPolicy(orgID, actor)
		policy.UpdatedAt = time.Now().UTC()
		return policy, nil
	}
	if err != nil {
		return PolicyDocument{}, fmt.Errorf("get identity policy: %w", err)
	}
	var stored storedPolicyDocument
	if err := json.Unmarshal(raw, &stored); err != nil {
		return PolicyDocument{}, fmt.Errorf("decode identity policy: %w", err)
	}
	return PolicyDocument{
		OrgID:     orgID,
		Version:   version,
		Roles:     clonePolicyRoles(stored.Roles),
		UpdatedAt: updatedAt,
		UpdatedBy: updatedBy,
	}, nil
}

func (s SQLStore) PutPolicy(ctx context.Context, policy PolicyDocument) (PolicyDocument, error) {
	if s.DB == nil {
		return PolicyDocument{}, ErrStoreUnavailable
	}
	if err := ValidatePolicy(policy); err != nil {
		return PolicyDocument{}, err
	}
	raw, err := json.Marshal(storedPolicyDocument{Roles: clonePolicyRoles(policy.Roles)})
	if err != nil {
		return PolicyDocument{}, fmt.Errorf("encode identity policy: %w", err)
	}
	query := `
UPDATE identity_policy_documents
SET document = $2::jsonb,
    version = version + 1,
    updated_at = now(),
    updated_by = $3
WHERE org_id = $1 AND version = $4
RETURNING version, updated_at
`
	args := []any{policy.OrgID, string(raw), policy.UpdatedBy, policy.Version}
	if policy.Version == 0 {
		query = `
INSERT INTO identity_policy_documents (org_id, document, version, updated_at, updated_by)
VALUES ($1, $2::jsonb, 1, now(), $3)
ON CONFLICT (org_id) DO NOTHING
RETURNING version, updated_at
`
		args = []any{policy.OrgID, string(raw), policy.UpdatedBy}
	}
	err = s.DB.QueryRowContext(ctx, query, args...).Scan(&policy.Version, &policy.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PolicyDocument{}, fmt.Errorf("%w: stale version for org %s", ErrPolicyConflict, policy.OrgID)
	}
	if err != nil {
		return PolicyDocument{}, fmt.Errorf("put identity policy: %w", err)
	}
	policy.Roles = clonePolicyRoles(policy.Roles)
	return policy, nil
}

type credentialScanner interface {
	Scan(dest ...any) error
}

func (s SQLStore) CreateAPICredential(ctx context.Context, credential APICredential, secret APICredentialSecret) (APICredential, error) {
	if s.DB == nil {
		return APICredential{}, ErrStoreUnavailable
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return APICredential{}, fmt.Errorf("begin create api credential: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO identity_api_credentials (
    credential_id, org_id, subject_id, client_id, display_name, auth_method, status,
    policy_version_at_issue, created_at, created_by, updated_at, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $9, $11)
`, credential.CredentialID, credential.OrgID, credential.SubjectID, credential.ClientID, credential.DisplayName, string(credential.AuthMethod), string(credential.Status), credential.PolicyVersionAtIssue, credential.CreatedAt, credential.CreatedBy, credential.ExpiresAt); err != nil {
		return APICredential{}, fmt.Errorf("insert api credential: %w", err)
	}
	for _, permission := range credential.Permissions {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO identity_api_credential_permissions (credential_id, permission, created_at)
VALUES ($1, $2, $3)
`, credential.CredentialID, permission, credential.CreatedAt); err != nil {
			return APICredential{}, fmt.Errorf("insert api credential permission: %w", err)
		}
	}
	if err := insertAPICredentialSecret(ctx, tx, secret); err != nil {
		return APICredential{}, err
	}
	if err := tx.Commit(); err != nil {
		return APICredential{}, fmt.Errorf("commit create api credential: %w", err)
	}
	return s.GetAPICredential(ctx, credential.OrgID, credential.CredentialID)
}

func (s SQLStore) ListAPICredentials(ctx context.Context, orgID string) ([]APICredential, error) {
	if s.DB == nil {
		return nil, ErrStoreUnavailable
	}
	rows, err := s.DB.QueryContext(ctx, apiCredentialSelectSQL()+`
WHERE c.org_id = $1
ORDER BY c.created_at DESC, c.credential_id DESC
`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list api credentials: %w", err)
	}
	defer rows.Close()
	credentials := []APICredential{}
	for rows.Next() {
		credential, err := scanAPICredential(rows)
		if err != nil {
			return nil, err
		}
		credential.Permissions, err = s.apiCredentialPermissions(ctx, credential.CredentialID)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list api credentials rows: %w", err)
	}
	return credentials, nil
}

func (s SQLStore) GetAPICredential(ctx context.Context, orgID, credentialID string) (APICredential, error) {
	if s.DB == nil {
		return APICredential{}, ErrStoreUnavailable
	}
	credential, err := scanAPICredential(s.DB.QueryRowContext(ctx, apiCredentialSelectSQL()+`
WHERE c.org_id = $1 AND c.credential_id = $2
`, orgID, credentialID))
	if errors.Is(err, sql.ErrNoRows) {
		return APICredential{}, ErrAPICredentialMissing
	}
	if err != nil {
		return APICredential{}, err
	}
	credential.Permissions, err = s.apiCredentialPermissions(ctx, credential.CredentialID)
	if err != nil {
		return APICredential{}, err
	}
	return credential, nil
}

func (s SQLStore) ActiveAPICredentialSecrets(ctx context.Context, orgID, credentialID string) ([]APICredentialSecret, error) {
	if s.DB == nil {
		return nil, ErrStoreUnavailable
	}
	rows, err := s.DB.QueryContext(ctx, `
SELECT s.secret_id, s.credential_id, s.auth_method, s.provider_key_id, s.fingerprint,
       s.secret_hash, s.hash_algorithm, s.created_at, s.created_by, s.expires_at, s.revoked_at, s.revoked_by
FROM identity_api_credential_secrets s
JOIN identity_api_credentials c ON c.credential_id = s.credential_id
WHERE c.org_id = $1 AND c.credential_id = $2 AND s.revoked_at IS NULL
ORDER BY s.created_at DESC
`, orgID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("list active api credential secrets: %w", err)
	}
	defer rows.Close()
	secrets := []APICredentialSecret{}
	for rows.Next() {
		secret, err := scanAPICredentialSecret(rows)
		if err != nil {
			return nil, err
		}
		secrets = append(secrets, secret)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active api credential secrets rows: %w", err)
	}
	return secrets, nil
}

func (s SQLStore) AddAPICredentialSecret(ctx context.Context, orgID, credentialID, actor string, secret APICredentialSecret) (APICredential, error) {
	if s.DB == nil {
		return APICredential{}, ErrStoreUnavailable
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return APICredential{}, fmt.Errorf("begin add api credential secret: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `
UPDATE identity_api_credential_secrets s
SET revoked_at = $4, revoked_by = $3
FROM identity_api_credentials c
WHERE c.credential_id = s.credential_id
  AND c.org_id = $1
  AND c.credential_id = $2
  AND s.revoked_at IS NULL
`, orgID, credentialID, actor, secret.CreatedAt); err != nil {
		return APICredential{}, fmt.Errorf("revoke previous api credential secrets: %w", err)
	}
	if err := insertAPICredentialSecret(ctx, tx, secret); err != nil {
		return APICredential{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE identity_api_credentials
SET auth_method = $3, updated_at = $4
WHERE org_id = $1 AND credential_id = $2 AND status = 'active'
`, orgID, credentialID, string(secret.AuthMethod), secret.CreatedAt)
	if err != nil {
		return APICredential{}, fmt.Errorf("update api credential after roll: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return APICredential{}, ErrAPICredentialMissing
	}
	if err := tx.Commit(); err != nil {
		return APICredential{}, fmt.Errorf("commit add api credential secret: %w", err)
	}
	return s.GetAPICredential(ctx, orgID, credentialID)
}

func (s SQLStore) RevokeAPICredential(ctx context.Context, orgID, credentialID, actor string, now time.Time) (APICredential, error) {
	if s.DB == nil {
		return APICredential{}, ErrStoreUnavailable
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return APICredential{}, fmt.Errorf("begin revoke api credential: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `
UPDATE identity_api_credential_secrets s
SET revoked_at = COALESCE(s.revoked_at, $4), revoked_by = COALESCE(s.revoked_by, $3)
FROM identity_api_credentials c
WHERE c.credential_id = s.credential_id
  AND c.org_id = $1
  AND c.credential_id = $2
  AND s.revoked_at IS NULL
`, orgID, credentialID, actor, now); err != nil {
		return APICredential{}, fmt.Errorf("revoke api credential secrets: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
UPDATE identity_api_credentials
SET status = 'revoked', revoked_at = $4, revoked_by = $3, updated_at = $4
WHERE org_id = $1 AND credential_id = $2 AND status = 'active'
`, orgID, credentialID, actor, now)
	if err != nil {
		return APICredential{}, fmt.Errorf("revoke api credential: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return APICredential{}, ErrAPICredentialMissing
	}
	if err := tx.Commit(); err != nil {
		return APICredential{}, fmt.Errorf("commit revoke api credential: %w", err)
	}
	return s.GetAPICredential(ctx, orgID, credentialID)
}

func (s SQLStore) ResolveAPICredentialClaims(ctx context.Context, subjectID string, usedAt time.Time) (ResolveAPICredentialClaimsResult, error) {
	if s.DB == nil {
		return ResolveAPICredentialClaimsResult{}, ErrStoreUnavailable
	}
	var credentialID, orgID string
	err := s.DB.QueryRowContext(ctx, `
SELECT credential_id, org_id
FROM identity_api_credentials
WHERE subject_id = $1
  AND status = 'active'
  AND (expires_at IS NULL OR expires_at > $2)
`, subjectID, usedAt).Scan(&credentialID, &orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolveAPICredentialClaimsResult{}, ErrAPICredentialMissing
	}
	if err != nil {
		return ResolveAPICredentialClaimsResult{}, fmt.Errorf("resolve api credential claims: %w", err)
	}
	permissions, err := s.apiCredentialPermissions(ctx, credentialID)
	if err != nil {
		return ResolveAPICredentialClaimsResult{}, err
	}
	if _, err := s.DB.ExecContext(ctx, `
UPDATE identity_api_credentials
SET last_used_at = $2, updated_at = $2
WHERE credential_id = $1
`, credentialID, usedAt); err != nil {
		return ResolveAPICredentialClaimsResult{}, fmt.Errorf("record api credential use: %w", err)
	}
	return ResolveAPICredentialClaimsResult{CredentialID: credentialID, OrgID: orgID, Permissions: permissions}, nil
}

func apiCredentialSelectSQL() string {
	return `
SELECT c.credential_id, c.org_id, c.subject_id, c.client_id, c.display_name, c.status,
       c.auth_method,
       COALESCE((
           SELECT s.fingerprint
           FROM identity_api_credential_secrets s
           WHERE s.credential_id = c.credential_id AND s.revoked_at IS NULL
           ORDER BY s.created_at DESC
           LIMIT 1
       ), '') AS fingerprint,
       c.policy_version_at_issue, c.created_at, c.created_by, c.updated_at,
       c.expires_at, c.revoked_at, c.revoked_by, c.last_used_at
FROM identity_api_credentials c
`
}

func scanAPICredential(scanner credentialScanner) (APICredential, error) {
	var credential APICredential
	var status, method string
	var expiresAt, revokedAt, lastUsedAt sql.NullTime
	err := scanner.Scan(
		&credential.CredentialID,
		&credential.OrgID,
		&credential.SubjectID,
		&credential.ClientID,
		&credential.DisplayName,
		&status,
		&method,
		&credential.Fingerprint,
		&credential.PolicyVersionAtIssue,
		&credential.CreatedAt,
		&credential.CreatedBy,
		&credential.UpdatedAt,
		&expiresAt,
		&revokedAt,
		&credential.RevokedBy,
		&lastUsedAt,
	)
	if err != nil {
		return APICredential{}, err
	}
	credential.Status = APICredentialStatus(status)
	credential.AuthMethod = APICredentialAuthMethod(method)
	credential.ExpiresAt = nullableTime(expiresAt)
	credential.RevokedAt = nullableTime(revokedAt)
	credential.LastUsedAt = nullableTime(lastUsedAt)
	return credential, nil
}

func (s SQLStore) apiCredentialPermissions(ctx context.Context, credentialID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT permission
FROM identity_api_credential_permissions
WHERE credential_id = $1
ORDER BY permission
`, credentialID)
	if err != nil {
		return nil, fmt.Errorf("list api credential permissions: %w", err)
	}
	defer rows.Close()
	permissions := []string{}
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, fmt.Errorf("scan api credential permission: %w", err)
		}
		permissions = append(permissions, permission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list api credential permission rows: %w", err)
	}
	return permissions, nil
}

func insertAPICredentialSecret(ctx context.Context, tx *sql.Tx, secret APICredentialSecret) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO identity_api_credential_secrets (
    secret_id, credential_id, auth_method, provider_key_id, fingerprint, secret_hash,
    hash_algorithm, created_at, created_by, expires_at, revoked_at, revoked_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`, secret.SecretID, secret.CredentialID, string(secret.AuthMethod), secret.ProviderKeyID, secret.Fingerprint, secret.SecretHash, secret.HashAlgorithm, secret.CreatedAt, secret.CreatedBy, secret.ExpiresAt, secret.RevokedAt, secret.RevokedBy); err != nil {
		return fmt.Errorf("insert api credential secret: %w", err)
	}
	return nil
}

func scanAPICredentialSecret(scanner credentialScanner) (APICredentialSecret, error) {
	var secret APICredentialSecret
	var method string
	var expiresAt, revokedAt sql.NullTime
	if err := scanner.Scan(
		&secret.SecretID,
		&secret.CredentialID,
		&method,
		&secret.ProviderKeyID,
		&secret.Fingerprint,
		&secret.SecretHash,
		&secret.HashAlgorithm,
		&secret.CreatedAt,
		&secret.CreatedBy,
		&expiresAt,
		&revokedAt,
		&secret.RevokedBy,
	); err != nil {
		return APICredentialSecret{}, fmt.Errorf("scan api credential secret: %w", err)
	}
	secret.AuthMethod = APICredentialAuthMethod(method)
	secret.ExpiresAt = nullableTime(expiresAt)
	secret.RevokedAt = nullableTime(revokedAt)
	return secret, nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	instant := value.Time
	return &instant
}

func rollback(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}
