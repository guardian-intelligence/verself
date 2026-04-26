package objectstorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	storegen "github.com/verself/object-storage-service/internal/store"
)

var (
	ErrNotFound     = errors.New("object-storage: not found")
	ErrConflict     = errors.New("object-storage: conflict")
	ErrUnauthorized = errors.New("object-storage: unauthorized")
)

type Store struct {
	pool    *pgxpool.Pool
	queries *storegen.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		pool:    pool,
		queries: storegen.New(pool),
	}
}

func (s *Store) Ready(ctx context.Context) error {
	if s == nil || s.pool == nil || s.queries == nil {
		return fmt.Errorf("object-storage store unavailable")
	}
	return s.pool.Ping(ctx)
}

func (s *Store) CreateBucket(ctx context.Context, bucket Bucket) error {
	q, err := s.readyQueries()
	if err != nil {
		return err
	}
	if len(bucket.LifecycleJSON) == 0 {
		bucket.LifecycleJSON = json.RawMessage("[]")
	}
	err = q.CreateBucket(ctx, storegen.CreateBucketParams{
		BucketID:       bucket.BucketID,
		OrgID:          bucket.OrgID,
		BucketName:     bucket.BucketName,
		GarageBucketID: bucket.GarageBucketID,
		QuotaBytes:     pgInt8(bucket.QuotaBytes),
		QuotaObjects:   pgInt8(bucket.QuotaObjects),
		LifecycleJson:  []byte(bucket.LifecycleJSON),
		CreatedAt:      pgTimestamptz(bucket.CreatedAt),
		CreatedBy:      bucket.CreatedBy,
		UpdatedAt:      pgTimestamptz(bucket.UpdatedAt),
		UpdatedBy:      bucket.UpdatedBy,
	})
	if err != nil {
		return classifyStoreError("create bucket", err)
	}
	return nil
}

func (s *Store) UpdateBucket(ctx context.Context, bucket Bucket) error {
	q, err := s.readyQueries()
	if err != nil {
		return err
	}
	if len(bucket.LifecycleJSON) == 0 {
		bucket.LifecycleJSON = json.RawMessage("[]")
	}
	_, err = q.UpdateBucket(ctx, storegen.UpdateBucketParams{
		QuotaBytes:    pgInt8(bucket.QuotaBytes),
		QuotaObjects:  pgInt8(bucket.QuotaObjects),
		LifecycleJson: []byte(bucket.LifecycleJSON),
		UpdatedAt:     pgTimestamptz(bucket.UpdatedAt),
		UpdatedBy:     bucket.UpdatedBy,
		BucketID:      bucket.BucketID,
	})
	if err != nil {
		return classifyMutationError("update bucket", err)
	}
	return nil
}

func (s *Store) BucketByID(ctx context.Context, bucketID uuid.UUID) (Bucket, error) {
	q, err := s.readyQueries()
	if err != nil {
		return Bucket{}, err
	}
	row, err := q.BucketByID(ctx, storegen.BucketByIDParams{BucketID: bucketID})
	if err != nil {
		return Bucket{}, classifyLookupError("bucket by id", err)
	}
	return bucketFromRow(row), nil
}

func (s *Store) BucketByName(ctx context.Context, bucketName string) (Bucket, error) {
	q, err := s.readyQueries()
	if err != nil {
		return Bucket{}, err
	}
	row, err := q.BucketByName(ctx, storegen.BucketByNameParams{BucketName: bucketName})
	if err != nil {
		return Bucket{}, classifyLookupError("bucket by name", err)
	}
	return bucketFromRow(row), nil
}

func (s *Store) ListBuckets(ctx context.Context) ([]Bucket, error) {
	q, err := s.readyQueries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListBuckets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}
	out := make([]Bucket, 0, len(rows))
	for _, row := range rows {
		out = append(out, bucketFromRow(row))
	}
	return out, nil
}

func (s *Store) CreateAlias(ctx context.Context, alias BucketAlias) error {
	q, err := s.readyQueries()
	if err != nil {
		return err
	}
	err = q.CreateAlias(ctx, storegen.CreateAliasParams{
		Alias:      alias.Alias,
		BucketID:   alias.BucketID,
		Prefix:     alias.Prefix,
		ServiceTag: alias.ServiceTag,
		CreatedAt:  pgTimestamptz(alias.CreatedAt),
		CreatedBy:  alias.CreatedBy,
	})
	if err != nil {
		return classifyStoreError("create alias", err)
	}
	return nil
}

func (s *Store) DeleteAlias(ctx context.Context, alias string) error {
	q, err := s.readyQueries()
	if err != nil {
		return err
	}
	if _, err := q.DeleteAlias(ctx, storegen.DeleteAliasParams{Alias: alias}); err != nil {
		return classifyMutationError("delete alias", err)
	}
	return nil
}

func (s *Store) AliasesByBucket(ctx context.Context, bucketID uuid.UUID) ([]BucketAlias, error) {
	q, err := s.readyQueries()
	if err != nil {
		return nil, err
	}
	rows, err := q.AliasesByBucket(ctx, storegen.AliasesByBucketParams{BucketID: bucketID})
	if err != nil {
		return nil, fmt.Errorf("list aliases: %w", err)
	}
	out := make([]BucketAlias, 0, len(rows))
	for _, row := range rows {
		out = append(out, aliasFromRow(row))
	}
	return out, nil
}

func (s *Store) ResolveBucketAlias(ctx context.Context, alias string) (Bucket, BucketAlias, bool, error) {
	q, err := s.readyQueries()
	if err != nil {
		return Bucket{}, BucketAlias{}, false, err
	}
	row, err := q.ResolveBucketAlias(ctx, storegen.ResolveBucketAliasParams{Alias: alias})
	if errors.Is(err, pgx.ErrNoRows) {
		return Bucket{}, BucketAlias{}, false, nil
	}
	if err != nil {
		return Bucket{}, BucketAlias{}, false, fmt.Errorf("resolve bucket alias: %w", err)
	}
	return bucketFromAliasRow(row), aliasFromAliasRow(row), true, nil
}

func (s *Store) CreateCredential(ctx context.Context, credential Credential) error {
	q, err := s.readyQueries()
	if err != nil {
		return err
	}
	// pgx encodes nil []byte as SQL NULL; credential constraints distinguish empty bytea from NULL.
	if credential.SecretCiphertext == nil {
		credential.SecretCiphertext = []byte{}
	}
	if credential.SecretNonce == nil {
		credential.SecretNonce = []byte{}
	}
	err = q.CreateCredential(ctx, storegen.CreateCredentialParams{
		CredentialID:      credential.CredentialID,
		BucketID:          credential.BucketID,
		AuthMode:          credential.AuthMode,
		DisplayName:       credential.DisplayName,
		AccessKeyID:       credential.AccessKeyID,
		SpiffeSubject:     credential.SPIFFESubject,
		SecretHash:        credential.SecretHash,
		SecretFingerprint: credential.SecretFingerprint,
		SecretCiphertext:  credential.SecretCiphertext,
		SecretNonce:       credential.SecretNonce,
		Status:            credential.Status,
		ExpiresAt:         pgNullableTimestamptz(credential.ExpiresAt),
		CreatedAt:         pgTimestamptz(credential.CreatedAt),
		CreatedBy:         credential.CreatedBy,
		RevokedAt:         pgNullableTimestamptz(credential.RevokedAt),
		RevokedBy:         credential.RevokedBy,
	})
	if err != nil {
		return classifyStoreError("create credential", err)
	}
	return nil
}

func (s *Store) DeleteCredential(ctx context.Context, credentialID uuid.UUID) error {
	q, err := s.readyQueries()
	if err != nil {
		return err
	}
	if _, err := q.DeleteCredential(ctx, storegen.DeleteCredentialParams{CredentialID: credentialID}); err != nil {
		return classifyMutationError("delete credential", err)
	}
	return nil
}

func (s *Store) RevokeCredentialByAccessKey(ctx context.Context, accessKeyID, actor string, now time.Time) error {
	q, err := s.readyQueries()
	if err != nil {
		return err
	}
	_, err = q.RevokeCredentialByAccessKey(ctx, storegen.RevokeCredentialByAccessKeyParams{
		RevokedStatus: CredentialStatusRevoked,
		RevokedAt:     pgTimestamptz(now),
		RevokedBy:     actor,
		AccessKeyID:   accessKeyID,
		AuthMode:      AuthModeSigV4Static,
		ActiveStatus:  CredentialStatusActive,
	})
	if err != nil {
		return classifyMutationError("revoke credential", err)
	}
	return nil
}

func (s *Store) ActiveCredentialByAccessKeyID(ctx context.Context, accessKeyID string) (Credential, error) {
	q, err := s.readyQueries()
	if err != nil {
		return Credential{}, err
	}
	row, err := q.ActiveCredentialByAccessKeyID(ctx, storegen.ActiveCredentialByAccessKeyIDParams{
		AccessKeyID: accessKeyID,
		AuthMode:    AuthModeSigV4Static,
		Status:      CredentialStatusActive,
	})
	if err != nil {
		return Credential{}, classifyLookupError("active credential by access key", err)
	}
	return credentialFromRow(row), nil
}

func (s *Store) CredentialByID(ctx context.Context, credentialID uuid.UUID) (Credential, error) {
	q, err := s.readyQueries()
	if err != nil {
		return Credential{}, err
	}
	row, err := q.CredentialByID(ctx, storegen.CredentialByIDParams{CredentialID: credentialID})
	if err != nil {
		return Credential{}, classifyLookupError("credential by id", err)
	}
	return credentialFromRow(row), nil
}

func (s *Store) ActiveCredentialBySPIFFE(ctx context.Context, spiffeSubject string) (Credential, error) {
	q, err := s.readyQueries()
	if err != nil {
		return Credential{}, err
	}
	row, err := q.ActiveCredentialBySPIFFE(ctx, storegen.ActiveCredentialBySPIFFEParams{
		SpiffeSubject: spiffeSubject,
		AuthMode:      AuthModeSPIFFEMTLS,
		Status:        CredentialStatusActive,
	})
	if err != nil {
		return Credential{}, classifyLookupError("active credential by spiffe", err)
	}
	return credentialFromRow(row), nil
}

func (s *Store) CredentialsByBucket(ctx context.Context, bucketID uuid.UUID) ([]Credential, error) {
	q, err := s.readyQueries()
	if err != nil {
		return nil, err
	}
	rows, err := q.CredentialsByBucket(ctx, storegen.CredentialsByBucketParams{BucketID: bucketID})
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	out := make([]Credential, 0, len(rows))
	for _, row := range rows {
		out = append(out, credentialFromRow(row))
	}
	return out, nil
}

func (s *Store) DeleteBucket(ctx context.Context, bucketID uuid.UUID) error {
	q, err := s.readyQueries()
	if err != nil {
		return err
	}
	if _, err := q.DeleteBucket(ctx, storegen.DeleteBucketParams{BucketID: bucketID}); err != nil {
		return classifyMutationError("delete bucket", err)
	}
	return nil
}

func (s *Store) RevokeCredentialByID(ctx context.Context, credentialID uuid.UUID, actor string, now time.Time) error {
	q, err := s.readyQueries()
	if err != nil {
		return err
	}
	_, err = q.RevokeCredentialByID(ctx, storegen.RevokeCredentialByIDParams{
		RevokedStatus: CredentialStatusRevoked,
		RevokedAt:     pgTimestamptz(now),
		RevokedBy:     actor,
		CredentialID:  credentialID,
		ActiveStatus:  CredentialStatusActive,
	})
	if err != nil {
		return classifyMutationError("revoke credential", err)
	}
	return nil
}

func (s *Store) readyQueries() (*storegen.Queries, error) {
	if s == nil || s.pool == nil || s.queries == nil {
		return nil, fmt.Errorf("object-storage store unavailable")
	}
	return s.queries, nil
}

func bucketFromRow(row storegen.ObjectStorageBucket) Bucket {
	return Bucket{
		BucketID:       row.BucketID,
		OrgID:          row.OrgID,
		BucketName:     row.BucketName,
		GarageBucketID: row.GarageBucketID,
		QuotaBytes:     int64Ptr(row.QuotaBytes),
		QuotaObjects:   int64Ptr(row.QuotaObjects),
		LifecycleJSON:  json.RawMessage(row.LifecycleJson),
		CreatedAt:      timeFromPG(row.CreatedAt),
		CreatedBy:      row.CreatedBy,
		UpdatedAt:      timeFromPG(row.UpdatedAt),
		UpdatedBy:      row.UpdatedBy,
	}
}

func aliasFromRow(row storegen.ObjectStorageBucketAlias) BucketAlias {
	return BucketAlias{
		Alias:      row.Alias,
		BucketID:   row.BucketID,
		Prefix:     row.Prefix,
		ServiceTag: row.ServiceTag,
		CreatedAt:  timeFromPG(row.CreatedAt),
		CreatedBy:  row.CreatedBy,
	}
}

func credentialFromRow(row storegen.ObjectStorageCredential) Credential {
	return Credential{
		CredentialID:      row.CredentialID,
		BucketID:          row.BucketID,
		AuthMode:          row.AuthMode,
		DisplayName:       row.DisplayName,
		AccessKeyID:       row.AccessKeyID,
		SPIFFESubject:     row.SpiffeSubject,
		SecretHash:        row.SecretHash,
		SecretFingerprint: row.SecretFingerprint,
		SecretCiphertext:  row.SecretCiphertext,
		SecretNonce:       row.SecretNonce,
		Status:            row.Status,
		ExpiresAt:         timePtrFromPG(row.ExpiresAt),
		CreatedAt:         timeFromPG(row.CreatedAt),
		CreatedBy:         row.CreatedBy,
		RevokedAt:         timePtrFromPG(row.RevokedAt),
		RevokedBy:         row.RevokedBy,
	}
}

func bucketFromAliasRow(row storegen.ResolveBucketAliasRow) Bucket {
	return Bucket{
		BucketID:       row.BucketID,
		OrgID:          row.OrgID,
		BucketName:     row.BucketName,
		GarageBucketID: row.GarageBucketID,
		QuotaBytes:     int64Ptr(row.QuotaBytes),
		QuotaObjects:   int64Ptr(row.QuotaObjects),
		LifecycleJSON:  json.RawMessage(row.LifecycleJson),
		CreatedAt:      timeFromPG(row.CreatedAt),
		CreatedBy:      row.CreatedBy,
		UpdatedAt:      timeFromPG(row.UpdatedAt),
		UpdatedBy:      row.UpdatedBy,
	}
}

func aliasFromAliasRow(row storegen.ResolveBucketAliasRow) BucketAlias {
	return BucketAlias{
		Alias:      row.Alias,
		BucketID:   row.AliasBucketID,
		Prefix:     row.Prefix,
		ServiceTag: row.ServiceTag,
		CreatedAt:  timeFromPG(row.AliasCreatedAt),
		CreatedBy:  row.AliasCreatedBy,
	}
}

func pgInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func int64Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func pgTimestamptz(v time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: v.UTC(), Valid: true}
}

func pgNullableTimestamptz(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgTimestamptz(*v)
}

func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}

func timePtrFromPG(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	out := v.Time
	return &out
}

func classifyLookupError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}

func classifyMutationError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return classifyStoreError(op, err)
}

func classifyStoreError(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%s: %w", op, ErrConflict)
	}
	return fmt.Errorf("%s: %w", op, err)
}
