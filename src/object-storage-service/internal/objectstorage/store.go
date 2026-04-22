package objectstorage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	ErrNotFound     = errors.New("object-storage: not found")
	ErrConflict     = errors.New("object-storage: conflict")
	ErrUnauthorized = errors.New("object-storage: unauthorized")
)

type Store struct {
	DB *sql.DB
}

func (s *Store) Ready(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("object-storage store unavailable")
	}
	return s.DB.PingContext(ctx)
}

func (s *Store) CreateBucket(ctx context.Context, bucket Bucket) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("object-storage store unavailable")
	}
	if len(bucket.LifecycleJSON) == 0 {
		bucket.LifecycleJSON = json.RawMessage("[]")
	}
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO object_storage_buckets (
    bucket_id, org_id, bucket_name, garage_bucket_id, quota_bytes, quota_objects,
    lifecycle_json, created_at, created_by, updated_at, updated_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11)
`, bucket.BucketID, bucket.OrgID, bucket.BucketName, bucket.GarageBucketID,
		bucket.QuotaBytes, bucket.QuotaObjects, []byte(bucket.LifecycleJSON),
		bucket.CreatedAt, bucket.CreatedBy, bucket.UpdatedAt, bucket.UpdatedBy)
	if err != nil {
		return classifyStoreError("create bucket", err)
	}
	return nil
}

func (s *Store) UpdateBucket(ctx context.Context, bucket Bucket) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("object-storage store unavailable")
	}
	if len(bucket.LifecycleJSON) == 0 {
		bucket.LifecycleJSON = json.RawMessage("[]")
	}
	result, err := s.DB.ExecContext(ctx, `
UPDATE object_storage_buckets
SET quota_bytes = $2,
    quota_objects = $3,
    lifecycle_json = $4::jsonb,
    updated_at = $5,
    updated_by = $6
WHERE bucket_id = $1
`, bucket.BucketID, bucket.QuotaBytes, bucket.QuotaObjects, []byte(bucket.LifecycleJSON), bucket.UpdatedAt, bucket.UpdatedBy)
	if err != nil {
		return classifyStoreError("update bucket", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) BucketByID(ctx context.Context, bucketID uuid.UUID) (Bucket, error) {
	return s.bucketByQuery(ctx, `
SELECT bucket_id, org_id, bucket_name, garage_bucket_id, quota_bytes, quota_objects,
       lifecycle_json, created_at, created_by, updated_at, updated_by
FROM object_storage_buckets
WHERE bucket_id = $1
`, bucketID)
}

func (s *Store) BucketByName(ctx context.Context, bucketName string) (Bucket, error) {
	return s.bucketByQuery(ctx, `
SELECT bucket_id, org_id, bucket_name, garage_bucket_id, quota_bytes, quota_objects,
       lifecycle_json, created_at, created_by, updated_at, updated_by
FROM object_storage_buckets
WHERE bucket_name = $1
`, bucketName)
}

func (s *Store) ListBuckets(ctx context.Context) ([]Bucket, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT bucket_id, org_id, bucket_name, garage_bucket_id, quota_bytes, quota_objects,
       lifecycle_json, created_at, created_by, updated_at, updated_by
FROM object_storage_buckets
ORDER BY created_at DESC, bucket_id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}
	defer rows.Close()
	var out []Bucket
	for rows.Next() {
		bucket, err := scanBucket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list buckets rows: %w", err)
	}
	return out, nil
}

func (s *Store) CreateAlias(ctx context.Context, alias BucketAlias) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO object_storage_bucket_aliases (alias, bucket_id, prefix, service_tag, created_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
`, alias.Alias, alias.BucketID, alias.Prefix, alias.ServiceTag, alias.CreatedAt, alias.CreatedBy)
	if err != nil {
		return classifyStoreError("create alias", err)
	}
	return nil
}

func (s *Store) DeleteAlias(ctx context.Context, alias string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM object_storage_bucket_aliases WHERE alias = $1`, alias)
	if err != nil {
		return fmt.Errorf("delete alias: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AliasesByBucket(ctx context.Context, bucketID uuid.UUID) ([]BucketAlias, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT alias, bucket_id, prefix, service_tag, created_at, created_by
FROM object_storage_bucket_aliases
WHERE bucket_id = $1
ORDER BY alias
`, bucketID)
	if err != nil {
		return nil, fmt.Errorf("list aliases: %w", err)
	}
	defer rows.Close()
	var out []BucketAlias
	for rows.Next() {
		var alias BucketAlias
		if err := rows.Scan(&alias.Alias, &alias.BucketID, &alias.Prefix, &alias.ServiceTag, &alias.CreatedAt, &alias.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan alias: %w", err)
		}
		out = append(out, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list aliases rows: %w", err)
	}
	return out, nil
}

func (s *Store) ResolveBucketAlias(ctx context.Context, alias string) (Bucket, BucketAlias, bool, error) {
	query := `
SELECT b.bucket_id, b.org_id, b.bucket_name, b.garage_bucket_id, b.quota_bytes, b.quota_objects,
       b.lifecycle_json, b.created_at, b.created_by, b.updated_at, b.updated_by,
       a.alias, a.bucket_id, a.prefix, a.service_tag, a.created_at, a.created_by
FROM object_storage_bucket_aliases a
JOIN object_storage_buckets b ON b.bucket_id = a.bucket_id
WHERE a.alias = $1
`
	var (
		bucket            Bucket
		resolved          BucketAlias
		lifecycleRaw      []byte
		quotaBytes        sql.NullInt64
		quotaObjects      sql.NullInt64
		resolvedCreatedAt time.Time
	)
	err := s.DB.QueryRowContext(ctx, query, alias).Scan(
		&bucket.BucketID, &bucket.OrgID, &bucket.BucketName, &bucket.GarageBucketID, &quotaBytes, &quotaObjects,
		&lifecycleRaw, &bucket.CreatedAt, &bucket.CreatedBy, &bucket.UpdatedAt, &bucket.UpdatedBy,
		&resolved.Alias, &resolved.BucketID, &resolved.Prefix, &resolved.ServiceTag, &resolvedCreatedAt, &resolved.CreatedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Bucket{}, BucketAlias{}, false, nil
	}
	if err != nil {
		return Bucket{}, BucketAlias{}, false, fmt.Errorf("resolve bucket alias: %w", err)
	}
	if quotaBytes.Valid {
		v := quotaBytes.Int64
		bucket.QuotaBytes = &v
	}
	if quotaObjects.Valid {
		v := quotaObjects.Int64
		bucket.QuotaObjects = &v
	}
	bucket.LifecycleJSON = lifecycleRaw
	resolved.CreatedAt = resolvedCreatedAt
	return bucket, resolved, true, nil
}

func (s *Store) CreateCredential(ctx context.Context, credential Credential) error {
	// lib/pq maps nil []byte to SQL NULL; normalize to empty bytea so the
	// auth_mode-specific CHECK constraints can distinguish empty from missing.
	if credential.SecretCiphertext == nil {
		credential.SecretCiphertext = []byte{}
	}
	if credential.SecretNonce == nil {
		credential.SecretNonce = []byte{}
	}
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO object_storage_credentials (
    credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
    secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status,
    expires_at, created_at, created_by, revoked_at, revoked_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
`, credential.CredentialID, credential.BucketID, credential.AuthMode, credential.DisplayName,
		credential.AccessKeyID, credential.SPIFFESubject, credential.SecretHash,
		credential.SecretFingerprint, credential.SecretCiphertext, credential.SecretNonce,
		credential.Status, credential.ExpiresAt, credential.CreatedAt, credential.CreatedBy,
		credential.RevokedAt, credential.RevokedBy)
	if err != nil {
		return classifyStoreError("create credential", err)
	}
	return nil
}

func (s *Store) DeleteCredential(ctx context.Context, credentialID uuid.UUID) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM object_storage_credentials WHERE credential_id = $1`, credentialID)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokeCredentialByAccessKey(ctx context.Context, accessKeyID, actor string, now time.Time) error {
	result, err := s.DB.ExecContext(ctx, `
UPDATE object_storage_credentials
SET status = $2, revoked_at = $3, revoked_by = $4
WHERE access_key_id = $1 AND auth_mode = $5 AND status = $6
`, accessKeyID, CredentialStatusRevoked, now, actor, AuthModeSigV4Static, CredentialStatusActive)
	if err != nil {
		return classifyStoreError("revoke credential", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ActiveCredentialByAccessKeyID(ctx context.Context, accessKeyID string) (Credential, error) {
	return s.credentialByQuery(ctx, `
SELECT credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
       secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status, expires_at,
       created_at, created_by, revoked_at, revoked_by
FROM object_storage_credentials
WHERE access_key_id = $1 AND auth_mode = $2 AND status = $3
`, accessKeyID, AuthModeSigV4Static, CredentialStatusActive)
}

func (s *Store) CredentialByID(ctx context.Context, credentialID uuid.UUID) (Credential, error) {
	return s.credentialByQuery(ctx, `
SELECT credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
       secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status, expires_at,
       created_at, created_by, revoked_at, revoked_by
FROM object_storage_credentials
WHERE credential_id = $1
`, credentialID)
}

func (s *Store) ActiveCredentialBySPIFFE(ctx context.Context, spiffeSubject string) (Credential, error) {
	return s.credentialByQuery(ctx, `
SELECT credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
       secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status, expires_at,
       created_at, created_by, revoked_at, revoked_by
FROM object_storage_credentials
WHERE spiffe_subject = $1 AND auth_mode = $2 AND status = $3
`, spiffeSubject, AuthModeSPIFFEMTLS, CredentialStatusActive)
}

func (s *Store) CredentialsByBucket(ctx context.Context, bucketID uuid.UUID) ([]Credential, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT credential_id, bucket_id, auth_mode, display_name, access_key_id, spiffe_subject,
       secret_hash, secret_fingerprint, secret_ciphertext, secret_nonce, status, expires_at,
       created_at, created_by, revoked_at, revoked_by
FROM object_storage_credentials
WHERE bucket_id = $1
ORDER BY created_at DESC, credential_id DESC
`, bucketID)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		credential, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, credential)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list credentials rows: %w", err)
	}
	return out, nil
}

func (s *Store) DeleteBucket(ctx context.Context, bucketID uuid.UUID) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM object_storage_buckets WHERE bucket_id = $1`, bucketID)
	if err != nil {
		return fmt.Errorf("delete bucket: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokeCredentialByID(ctx context.Context, credentialID uuid.UUID, actor string, now time.Time) error {
	result, err := s.DB.ExecContext(ctx, `
UPDATE object_storage_credentials
SET status = $2, revoked_at = $3, revoked_by = $4
WHERE credential_id = $1 AND status = $5
`, credentialID, CredentialStatusRevoked, now, actor, CredentialStatusActive)
	if err != nil {
		return classifyStoreError("revoke credential", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) bucketByQuery(ctx context.Context, query string, args ...any) (Bucket, error) {
	if s == nil || s.DB == nil {
		return Bucket{}, fmt.Errorf("object-storage store unavailable")
	}
	row := s.DB.QueryRowContext(ctx, query, args...)
	bucket, err := scanBucket(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Bucket{}, ErrNotFound
	}
	if err != nil {
		return Bucket{}, err
	}
	return bucket, nil
}

func (s *Store) credentialByQuery(ctx context.Context, query string, args ...any) (Credential, error) {
	if s == nil || s.DB == nil {
		return Credential{}, fmt.Errorf("object-storage store unavailable")
	}
	row := s.DB.QueryRowContext(ctx, query, args...)
	credential, err := scanCredential(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, err
	}
	return credential, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanBucket(row scanner) (Bucket, error) {
	var (
		bucket       Bucket
		lifecycleRaw []byte
		quotaBytes   sql.NullInt64
		quotaObjects sql.NullInt64
	)
	if err := row.Scan(
		&bucket.BucketID, &bucket.OrgID, &bucket.BucketName, &bucket.GarageBucketID,
		&quotaBytes, &quotaObjects, &lifecycleRaw,
		&bucket.CreatedAt, &bucket.CreatedBy, &bucket.UpdatedAt, &bucket.UpdatedBy,
	); err != nil {
		return Bucket{}, fmt.Errorf("scan bucket: %w", err)
	}
	if quotaBytes.Valid {
		v := quotaBytes.Int64
		bucket.QuotaBytes = &v
	}
	if quotaObjects.Valid {
		v := quotaObjects.Int64
		bucket.QuotaObjects = &v
	}
	bucket.LifecycleJSON = lifecycleRaw
	return bucket, nil
}

func scanCredential(row scanner) (Credential, error) {
	var (
		credential Credential
		expiresAt  sql.NullTime
		revokedAt  sql.NullTime
	)
	if err := row.Scan(
		&credential.CredentialID, &credential.BucketID, &credential.AuthMode,
		&credential.DisplayName, &credential.AccessKeyID, &credential.SPIFFESubject,
		&credential.SecretHash, &credential.SecretFingerprint, &credential.SecretCiphertext,
		&credential.SecretNonce, &credential.Status, &expiresAt, &credential.CreatedAt,
		&credential.CreatedBy, &revokedAt, &credential.RevokedBy,
	); err != nil {
		return Credential{}, fmt.Errorf("scan credential: %w", err)
	}
	if expiresAt.Valid {
		v := expiresAt.Time
		credential.ExpiresAt = &v
	}
	if revokedAt.Valid {
		v := revokedAt.Time
		credential.RevokedAt = &v
	}
	return credential, nil
}

func classifyStoreError(op string, err error) error {
	if err == nil {
		return nil
	}
	if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
		return fmt.Errorf("%s: %w", op, ErrConflict)
	}
	return fmt.Errorf("%s: %w", op, err)
}
