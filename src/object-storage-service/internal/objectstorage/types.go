package objectstorage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	AuthModeSigV4Static = "sigv4_static"
	AuthModeSPIFFEMTLS  = "spiffe_mtls"

	CredentialStatusActive  = "active"
	CredentialStatusRevoked = "revoked"
)

type Bucket struct {
	BucketID       uuid.UUID
	OrgID          string
	BucketName     string
	GarageBucketID string
	QuotaBytes     *int64
	QuotaObjects   *int64
	LifecycleJSON  json.RawMessage
	CreatedAt      time.Time
	CreatedBy      string
	UpdatedAt      time.Time
	UpdatedBy      string
}

type BucketAlias struct {
	Alias      string
	BucketID   uuid.UUID
	Prefix     string
	ServiceTag string
	CreatedAt  time.Time
	CreatedBy  string
}

type Credential struct {
	CredentialID      uuid.UUID
	BucketID          uuid.UUID
	AuthMode          string
	DisplayName       string
	AccessKeyID       string
	SPIFFESubject     string
	SecretHash        string
	SecretFingerprint string
	SecretCiphertext  []byte
	SecretNonce       []byte
	Status            string
	ExpiresAt         *time.Time
	CreatedAt         time.Time
	CreatedBy         string
	RevokedAt         *time.Time
	RevokedBy         string
}

type GarageBucket struct {
	ID             string
	GlobalAliases  []string
	QuotaBytes     *int64
	QuotaObjects   *int64
	LifecycleRules json.RawMessage
}

type GarageBucketPermissions struct {
	Read  bool `json:"read,omitempty"`
	Write bool `json:"write,omitempty"`
	Owner bool `json:"owner,omitempty"`
}

type GarageAdmin interface {
	Health(ctx context.Context) error
	CreateBucket(ctx context.Context, globalAlias string) (GarageBucket, error)
	UpdateBucket(ctx context.Context, bucketID string, quotas GarageQuotas, lifecycleJSON []byte) (GarageBucket, error)
	GetBucket(ctx context.Context, bucketID string) (GarageBucket, error)
	DeleteBucket(ctx context.Context, bucketID string) error
	AllowBucketKey(ctx context.Context, bucketID, accessKeyID string, perms GarageBucketPermissions) error
}

type GarageQuotas struct {
	MaxSize    *int64 `json:"maxSize"`
	MaxObjects *int64 `json:"maxObjects"`
}

type AccessPrincipal struct {
	Bucket         Bucket
	ResolvedAlias  string
	ResolvedPrefix string
	Credential     Credential
}
