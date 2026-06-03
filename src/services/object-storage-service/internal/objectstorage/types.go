package objectstorage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	ProviderGarage       = "garage"
	ProviderCloudflareR2 = "cloudflare_r2"

	AuthModeSigV4Static = "sigv4_static"
	AuthModeSPIFFEMTLS  = "spiffe_mtls"

	CredentialStatusActive  = "active"
	CredentialStatusRevoked = "revoked"

	ObjectStorageCapabilityDeploymentArtifacts = "deployment_artifacts"
	ObjectStorageCapabilityProductObjects      = "product_objects"
	ObjectStorageCapabilityRecovery            = "recovery"

	ObjectWriteSessionStatusOpen      = "open"
	ObjectWriteSessionStatusCompleted = "completed"

	ObjectWriteActionPresent = "present"
	ObjectWriteActionPut     = "put"
)

type Bucket struct {
	BucketID         uuid.UUID
	OrgID            string
	BucketName       string
	Provider         string
	ProviderBucketID string
	QuotaBytes       *int64
	QuotaObjects     *int64
	LifecycleJSON    json.RawMessage
	CreatedAt        time.Time
	CreatedBy        string
	UpdatedAt        time.Time
	UpdatedBy        string
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

type ProviderBucket struct {
	ID             string
	LifecycleRules json.RawMessage
}

type ObjectWriteRequest struct {
	Name      string
	SHA256    string
	SizeBytes int64
}

type S3CompatibleObject struct {
	Method    string
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
	SHA256    string
}

type ObjectWriteSession struct {
	SessionID      string
	Site           string
	Capability     string
	Namespace      string
	BucketID       uuid.UUID
	IdempotencyKey string
	Status         string
	ExpiresAt      time.Time
	CreatedAt      time.Time
	CreatedBy      string
	CompletedAt    *time.Time
	Objects        []ObjectWriteSessionObject
}

type ObjectWriteSessionObject struct {
	SessionID string
	Name      string
	ObjectKey string
	SHA256    string
	SizeBytes int64
	Action    string
	Write     *S3CompatibleObject
	Read      *S3CompatibleObject
}

type CreateObjectWriteSessionInput struct {
	IdempotencyKey string
	Site           string
	Capability     string
	Namespace      string
	Objects        []ObjectWriteRequest
	TTL            time.Duration
	Actor          string
}

type BucketProviderInput struct {
	Name          string
	QuotaBytes    *int64
	QuotaObjects  *int64
	LifecycleJSON json.RawMessage
}

type BucketProviderUpdate struct {
	ProviderBucketID string
	QuotaBytes       *int64
	QuotaObjects     *int64
	LifecycleJSON    json.RawMessage
}

type BucketProvider interface {
	Kind() string
	Health(ctx context.Context) error
	CreateBucket(ctx context.Context, input BucketProviderInput) (ProviderBucket, error)
	UpdateBucket(ctx context.Context, input BucketProviderUpdate) (ProviderBucket, error)
	DeleteBucket(ctx context.Context, providerBucketID string) error
}

type ObjectTransferProvider interface {
	PresignObject(ctx context.Context, method, bucketName, objectKey, payloadSHA256 string, ttl time.Duration) (S3CompatibleObject, error)
	VerifyObject(ctx context.Context, bucketName, objectKey, expectedSHA256 string, expectedSizeBytes int64) error
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
