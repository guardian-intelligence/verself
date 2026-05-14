package objectstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("object-storage-service/internal/objectstorage")

type Config struct {
	ServiceName      string
	Environment      string
	ServiceVersion   string
	WriterInstanceID string
	ProxyAccessKeyID string
	ProxyRegion      string
}

type Service struct {
	CH      clickhouse.Conn
	Store   *Store
	Garage  GarageAdmin
	Secrets *SecretBox
	Logger  *slog.Logger
	Config  Config

	auditSink AuditSink
}

type CreateBucketInput struct {
	OrgID         string
	BucketName    string
	QuotaBytes    *int64
	QuotaObjects  *int64
	LifecycleJSON json.RawMessage
	Actor         string
}

type CreateAliasInput struct {
	BucketID   uuid.UUID
	Alias      string
	Prefix     string
	ServiceTag string
	Actor      string
}

type CreateStaticCredentialInput struct {
	BucketID    uuid.UUID
	DisplayName string
	ExpiresAt   *time.Time
	Actor       string
}

type CreateSPIFFECredentialInput struct {
	BucketID      uuid.UUID
	DisplayName   string
	SPIFFESubject string
	Actor         string
}

type StaticCredentialSecret struct {
	AccessKeyID     string
	SecretAccessKey string
	Fingerprint     string
}

type staticCredentialMaterial struct {
	credential Credential
	secret     StaticCredentialSecret
}

func (s *Service) Ready(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("object-storage service is nil")
	}
	if s.Store == nil {
		return fmt.Errorf("object-storage store is nil")
	}
	if err := s.Store.Ready(ctx); err != nil {
		return err
	}
	if s.Secrets == nil {
		return fmt.Errorf("object-storage secret box is nil")
	}
	if s.Garage == nil && s.CH == nil {
		return fmt.Errorf("garage admin client or clickhouse connection is required")
	}
	if s.Garage != nil {
		if err := s.Garage.Health(ctx); err != nil {
			return err
		}
	}
	if s.CH != nil {
		if err := s.CH.Ping(ctx); err != nil {
			return fmt.Errorf("object-storage clickhouse ping: %w", err)
		}
	}
	return nil
}

func (s *Service) AdminReady(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("object-storage service is nil")
	}
	if s.Garage == nil {
		return fmt.Errorf("garage admin client is nil")
	}
	return s.Ready(ctx)
}

func (s *Service) DataReady(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("object-storage service is nil")
	}
	if s.CH == nil {
		return fmt.Errorf("clickhouse connection is nil")
	}
	return s.Ready(ctx)
}

func (s *Service) SetAuditSink(fn AuditSink) {
	s.auditSink = fn
}

func (s *Service) CreateBucket(ctx context.Context, input CreateBucketInput) (Bucket, error) {
	ctx, span := tracer.Start(ctx, "object_storage.bucket.create")
	defer span.End()

	input.OrgID = strings.TrimSpace(input.OrgID)
	input.BucketName = normalizeBucketName(input.BucketName)
	input.Actor = strings.TrimSpace(input.Actor)
	if input.OrgID == "" || input.BucketName == "" || input.Actor == "" {
		return Bucket{}, invalidArgument(span, "org_id, bucket_name, and actor are required")
	}

	lifecycleJSON := normalizeLifecycleJSON(input.LifecycleJSON)
	now := time.Now().UTC()
	bucket := Bucket{
		BucketID:      uuid.New(),
		OrgID:         input.OrgID,
		BucketName:    input.BucketName,
		QuotaBytes:    input.QuotaBytes,
		QuotaObjects:  input.QuotaObjects,
		LifecycleJSON: lifecycleJSON,
		CreatedAt:     now,
		CreatedBy:     input.Actor,
		UpdatedAt:     now,
		UpdatedBy:     input.Actor,
	}
	record := s.baseAPIActivity(ctx, bucket.OrgID, input.Actor)
	record.APIOperation = "object_storage.bucket.create"
	record.APIEventCode = "object_storage.bucket.create"
	record.Permission = "object-storage:bucket:write"
	record.ResourceType = "bucket"
	record.ResourceUID = bucket.BucketID.String()

	garageBucket, err := s.Garage.CreateBucket(ctx, bucket.BucketName)
	if err != nil {
		return Bucket{}, s.finishAdminOp(ctx, record, fmt.Errorf("create garage bucket: %w", err))
	}
	bucket.GarageBucketID = garageBucket.ID
	record.Unmapped = compactAuditDetail(map[string]any{
		"verself.content_sha256": hashForAudit(bucket.BucketName + "\x00" + bucket.GarageBucketID),
	})

	cleanup := func() error {
		var cleanupErr error
		if bucket.GarageBucketID != "" {
			cleanupErr = s.Garage.DeleteBucket(ctx, bucket.GarageBucketID)
		}
		return cleanupErr
	}

	if _, err := s.Garage.UpdateBucket(ctx, bucket.GarageBucketID, GarageQuotas{MaxSize: bucket.QuotaBytes, MaxObjects: bucket.QuotaObjects}, bucket.LifecycleJSON); err != nil {
		_ = cleanup()
		return Bucket{}, s.finishAdminOp(ctx, record, fmt.Errorf("configure garage bucket: %w", err))
	}
	if err := s.Garage.AllowBucketKey(ctx, bucket.GarageBucketID, s.Config.ProxyAccessKeyID, GarageBucketPermissions{Read: true, Write: true, Owner: false}); err != nil {
		_ = cleanup()
		return Bucket{}, s.finishAdminOp(ctx, record, fmt.Errorf("grant garage proxy key: %w", err))
	}
	if err := s.Store.CreateBucket(ctx, bucket); err != nil {
		_ = cleanup()
		return Bucket{}, s.finishAdminOp(ctx, record, err)
	}
	if err := s.finishAdminOp(ctx, record, nil); err != nil {
		_ = s.Store.DeleteBucket(ctx, bucket.BucketID)
		_ = cleanup()
		return Bucket{}, err
	}

	span.SetAttributes(
		attribute.String("verself.org_id", bucket.OrgID),
		attribute.String("verself.bucket_id", bucket.BucketID.String()),
		attribute.String("verself.bucket_name", bucket.BucketName),
	)
	return bucket, nil
}

func (s *Service) UpdateBucket(ctx context.Context, bucketID uuid.UUID, quotaBytes, quotaObjects *int64, lifecycleJSON json.RawMessage, actor string) (Bucket, error) {
	ctx, span := tracer.Start(ctx, "object_storage.bucket.update")
	defer span.End()

	actor = strings.TrimSpace(actor)
	if actor == "" {
		return Bucket{}, invalidArgument(span, "actor is required")
	}
	bucket, err := s.Store.BucketByID(ctx, bucketID)
	if err != nil {
		return Bucket{}, err
	}
	if len(lifecycleJSON) == 0 {
		lifecycleJSON = bucket.LifecycleJSON
	}
	lifecycleJSON = normalizeLifecycleJSON(lifecycleJSON)

	record := s.baseAPIActivity(ctx, bucket.OrgID, actor)
	record.APIOperation = "object_storage.bucket.update"
	record.APIEventCode = "object_storage.bucket.update"
	record.Permission = "object-storage:bucket:write"
	record.ResourceType = "bucket"
	record.ResourceUID = bucket.BucketID.String()
	record.Unmapped = compactAuditDetail(map[string]any{
		"verself.content_sha256": hashForAudit(bucket.BucketName + "\x00" + string(lifecycleJSON)),
	})

	garageBucket, err := s.Garage.UpdateBucket(ctx, bucket.GarageBucketID, GarageQuotas{MaxSize: quotaBytes, MaxObjects: quotaObjects}, lifecycleJSON)
	if err != nil {
		return Bucket{}, s.finishAdminOp(ctx, record, fmt.Errorf("update garage bucket: %w", err))
	}
	now := time.Now().UTC()
	bucket.QuotaBytes = garageBucket.QuotaBytes
	bucket.QuotaObjects = garageBucket.QuotaObjects
	bucket.LifecycleJSON = normalizeLifecycleJSON(garageBucket.LifecycleRules)
	bucket.UpdatedAt = now
	bucket.UpdatedBy = actor
	if err := s.Store.UpdateBucket(ctx, bucket); err != nil {
		return Bucket{}, s.finishAdminOp(ctx, record, err)
	}
	if err := s.finishAdminOp(ctx, record, nil); err != nil {
		return Bucket{}, err
	}
	return bucket, nil
}

func (s *Service) DeleteBucket(ctx context.Context, bucketID uuid.UUID, actor string) error {
	ctx, span := tracer.Start(ctx, "object_storage.bucket.delete")
	defer span.End()

	actor = strings.TrimSpace(actor)
	if actor == "" {
		return invalidArgument(span, "actor is required")
	}
	bucket, err := s.Store.BucketByID(ctx, bucketID)
	if err != nil {
		return err
	}
	record := s.baseAPIActivity(ctx, bucket.OrgID, actor)
	record.APIOperation = "object_storage.bucket.delete"
	record.APIEventCode = "object_storage.bucket.delete"
	record.Permission = "object-storage:bucket:write"
	record.ResourceType = "bucket"
	record.ResourceUID = bucket.BucketID.String()
	record.Unmapped = compactAuditDetail(map[string]any{
		"verself.content_sha256": hashForAudit(bucket.BucketName + "\x00" + bucket.GarageBucketID),
	})

	if err := s.Garage.DeleteBucket(ctx, bucket.GarageBucketID); err != nil {
		return s.finishAdminOp(ctx, record, fmt.Errorf("delete garage bucket: %w", err))
	}
	if err := s.Store.DeleteBucket(ctx, bucket.BucketID); err != nil {
		return s.finishAdminOp(ctx, record, err)
	}
	return s.finishAdminOp(ctx, record, nil)
}

func (s *Service) CreateAlias(ctx context.Context, input CreateAliasInput) (BucketAlias, error) {
	ctx, span := tracer.Start(ctx, "object_storage.bucket_alias.create")
	defer span.End()

	input.Alias = normalizeBucketName(input.Alias)
	input.Prefix = normalizePrefix(input.Prefix)
	input.Actor = strings.TrimSpace(input.Actor)
	if input.Alias == "" || input.Actor == "" {
		return BucketAlias{}, invalidArgument(span, "alias and actor are required")
	}
	bucket, err := s.Store.BucketByID(ctx, input.BucketID)
	if err != nil {
		return BucketAlias{}, err
	}
	alias := BucketAlias{
		Alias:      input.Alias,
		BucketID:   bucket.BucketID,
		Prefix:     input.Prefix,
		ServiceTag: strings.TrimSpace(input.ServiceTag),
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  input.Actor,
	}
	record := s.baseAPIActivity(ctx, bucket.OrgID, input.Actor)
	record.APIOperation = "object_storage.bucket_alias.create"
	record.APIEventCode = "object_storage.bucket_alias.create"
	record.Permission = "object-storage:bucket:write"
	record.ResourceType = "bucket_alias"
	record.ResourceUID = alias.Alias
	record.Unmapped = compactAuditDetail(map[string]any{
		"verself.content_sha256": hashForAudit(alias.Alias + "\x00" + alias.Prefix),
	})

	if err := s.Store.CreateAlias(ctx, alias); err != nil {
		return BucketAlias{}, s.finishAdminOp(ctx, record, err)
	}
	if err := s.finishAdminOp(ctx, record, nil); err != nil {
		_ = s.Store.DeleteAlias(ctx, alias.Alias)
		return BucketAlias{}, err
	}
	return alias, nil
}

func (s *Service) DeleteAlias(ctx context.Context, bucketID uuid.UUID, aliasName, actor string) error {
	ctx, span := tracer.Start(ctx, "object_storage.bucket_alias.delete")
	defer span.End()

	aliasName = normalizeBucketName(aliasName)
	actor = strings.TrimSpace(actor)
	if aliasName == "" || actor == "" {
		return invalidArgument(span, "alias and actor are required")
	}
	bucket, alias, ok, err := s.Store.ResolveBucketAlias(ctx, aliasName)
	if err != nil {
		return err
	}
	if !ok || bucket.BucketID != bucketID {
		return ErrNotFound
	}
	record := s.baseAPIActivity(ctx, bucket.OrgID, actor)
	record.APIOperation = "object_storage.bucket_alias.delete"
	record.APIEventCode = "object_storage.bucket_alias.delete"
	record.Permission = "object-storage:bucket:write"
	record.ResourceType = "bucket_alias"
	record.ResourceUID = alias.Alias
	record.Unmapped = compactAuditDetail(map[string]any{
		"verself.content_sha256": hashForAudit(alias.Alias + "\x00" + alias.Prefix),
	})
	if err := s.Store.DeleteAlias(ctx, alias.Alias); err != nil {
		return s.finishAdminOp(ctx, record, err)
	}
	return s.finishAdminOp(ctx, record, nil)
}

func (s *Service) CreateStaticCredential(ctx context.Context, input CreateStaticCredentialInput) (Credential, StaticCredentialSecret, error) {
	ctx, span := tracer.Start(ctx, "object_storage.access_key.create")
	defer span.End()

	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Actor = strings.TrimSpace(input.Actor)
	if input.DisplayName == "" || input.Actor == "" {
		return Credential{}, StaticCredentialSecret{}, invalidArgument(span, "display_name and actor are required")
	}
	bucket, err := s.Store.BucketByID(ctx, input.BucketID)
	if err != nil {
		return Credential{}, StaticCredentialSecret{}, err
	}

	material, err := s.issueStaticCredential(ctx, bucket, input.DisplayName, input.ExpiresAt, input.Actor)
	if err != nil {
		return Credential{}, StaticCredentialSecret{}, err
	}
	record := s.baseAPIActivity(ctx, bucket.OrgID, input.Actor)
	record.APIOperation = "object_storage.access_key.create"
	record.APIEventCode = "object_storage.access_key.create"
	record.Permission = "object-storage:access-key:write"
	record.ResourceType = "access_key"
	record.ResourceUID = material.credential.AccessKeyID
	record.CredentialUID = material.credential.AccessKeyID
	record.Unmapped = compactAuditDetail(map[string]any{
		"credential_fingerprint": material.secret.Fingerprint,
		"verself.content_sha256": hashForAudit(material.credential.AccessKeyID + "\x00" + material.secret.Fingerprint),
	})
	if err := s.finishAdminOp(ctx, record, nil); err != nil {
		_ = s.Store.DeleteCredential(ctx, material.credential.CredentialID)
		return Credential{}, StaticCredentialSecret{}, err
	}
	return material.credential, material.secret, nil
}

func (s *Service) CreateSPIFFECredential(ctx context.Context, input CreateSPIFFECredentialInput) (Credential, error) {
	ctx, span := tracer.Start(ctx, "object_storage.mtls_principal.create")
	defer span.End()

	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.SPIFFESubject = strings.TrimSpace(input.SPIFFESubject)
	input.Actor = strings.TrimSpace(input.Actor)
	if input.DisplayName == "" || input.SPIFFESubject == "" || input.Actor == "" {
		return Credential{}, invalidArgument(span, "display_name, spiffe_subject, and actor are required")
	}
	bucket, err := s.Store.BucketByID(ctx, input.BucketID)
	if err != nil {
		return Credential{}, err
	}
	now := time.Now().UTC()
	credential := Credential{
		CredentialID:  uuid.New(),
		BucketID:      bucket.BucketID,
		AuthMode:      AuthModeSPIFFEMTLS,
		DisplayName:   input.DisplayName,
		SPIFFESubject: input.SPIFFESubject,
		Status:        CredentialStatusActive,
		CreatedAt:     now,
		CreatedBy:     input.Actor,
	}
	record := s.baseAPIActivity(ctx, bucket.OrgID, input.Actor)
	record.APIOperation = "object_storage.mtls_principal.create"
	record.APIEventCode = "object_storage.mtls_principal.create"
	record.Permission = "object-storage:access-key:write"
	record.ResourceType = "mtls_principal"
	record.ResourceUID = input.SPIFFESubject
	record.CredentialUID = credential.CredentialID.String()
	record.Unmapped = compactAuditDetail(map[string]any{
		"verself.content_sha256": hashForAudit(input.SPIFFESubject),
	})

	if err := s.Store.CreateCredential(ctx, credential); err != nil {
		return Credential{}, s.finishAdminOp(ctx, record, err)
	}
	if err := s.finishAdminOp(ctx, record, nil); err != nil {
		_ = s.Store.DeleteCredential(ctx, credential.CredentialID)
		return Credential{}, err
	}
	return credential, nil
}

func (s *Service) RollStaticCredential(ctx context.Context, accessKeyID, actor string) (Credential, StaticCredentialSecret, error) {
	ctx, span := tracer.Start(ctx, "object_storage.access_key.roll")
	defer span.End()

	actor = strings.TrimSpace(actor)
	if actor == "" {
		return Credential{}, StaticCredentialSecret{}, invalidArgument(span, "actor is required")
	}
	current, err := s.Store.ActiveCredentialByAccessKeyID(ctx, strings.TrimSpace(accessKeyID))
	if err != nil {
		return Credential{}, StaticCredentialSecret{}, err
	}
	if isCredentialExpired(current, time.Now().UTC()) {
		return Credential{}, StaticCredentialSecret{}, ErrUnauthorized
	}
	bucket, err := s.Store.BucketByID(ctx, current.BucketID)
	if err != nil {
		return Credential{}, StaticCredentialSecret{}, err
	}
	material, err := s.issueStaticCredential(ctx, bucket, current.DisplayName, current.ExpiresAt, actor)
	if err != nil {
		return Credential{}, StaticCredentialSecret{}, err
	}
	if err := s.Store.RevokeCredentialByAccessKey(ctx, current.AccessKeyID, actor, time.Now().UTC()); err != nil {
		_ = s.Store.DeleteCredential(ctx, material.credential.CredentialID)
		return Credential{}, StaticCredentialSecret{}, err
	}
	record := s.baseAPIActivity(ctx, bucket.OrgID, actor)
	record.APIOperation = "object_storage.access_key.roll"
	record.APIEventCode = "object_storage.access_key.roll"
	record.Permission = "object-storage:access-key:write"
	record.ResourceType = "access_key"
	record.ResourceUID = material.credential.AccessKeyID
	record.CredentialUID = material.credential.AccessKeyID
	record.Unmapped = compactAuditDetail(map[string]any{
		"previous_access_key_id": current.AccessKeyID,
		"credential_fingerprint": material.secret.Fingerprint,
		"verself.content_sha256": hashForAudit(current.AccessKeyID + "\x00" + material.credential.AccessKeyID),
	})
	if err := s.finishAdminOp(ctx, record, nil); err != nil {
		return Credential{}, StaticCredentialSecret{}, err
	}
	return material.credential, material.secret, nil
}

func (s *Service) RevokeStaticCredential(ctx context.Context, accessKeyID, actor string) error {
	ctx, span := tracer.Start(ctx, "object_storage.access_key.revoke")
	defer span.End()

	actor = strings.TrimSpace(actor)
	if actor == "" {
		return invalidArgument(span, "actor is required")
	}
	current, err := s.Store.ActiveCredentialByAccessKeyID(ctx, strings.TrimSpace(accessKeyID))
	if err != nil {
		return err
	}
	bucket, err := s.Store.BucketByID(ctx, current.BucketID)
	if err != nil {
		return err
	}
	record := s.baseAPIActivity(ctx, bucket.OrgID, actor)
	record.APIOperation = "object_storage.access_key.revoke"
	record.APIEventCode = "object_storage.access_key.revoke"
	record.Permission = "object-storage:access-key:write"
	record.ResourceType = "access_key"
	record.ResourceUID = current.AccessKeyID
	record.CredentialUID = current.AccessKeyID
	record.Unmapped = compactAuditDetail(map[string]any{
		"verself.content_sha256": hashForAudit(current.AccessKeyID),
	})

	if err := s.Store.RevokeCredentialByAccessKey(ctx, current.AccessKeyID, actor, time.Now().UTC()); err != nil {
		return s.finishAdminOp(ctx, record, err)
	}
	return s.finishAdminOp(ctx, record, nil)
}

func (s *Service) RevokeSPIFFECredential(ctx context.Context, credentialID uuid.UUID, actor string) error {
	ctx, span := tracer.Start(ctx, "object_storage.mtls_principal.revoke")
	defer span.End()

	actor = strings.TrimSpace(actor)
	if actor == "" {
		return invalidArgument(span, "actor is required")
	}
	current, err := s.Store.CredentialByID(ctx, credentialID)
	if err != nil {
		return err
	}
	if current.AuthMode != AuthModeSPIFFEMTLS {
		return invalidArgument(span, "credential is not an mTLS principal")
	}
	if current.Status != CredentialStatusActive {
		return ErrNotFound
	}
	bucket, err := s.Store.BucketByID(ctx, current.BucketID)
	if err != nil {
		return err
	}
	record := s.baseAPIActivity(ctx, bucket.OrgID, actor)
	record.APIOperation = "object_storage.mtls_principal.revoke"
	record.APIEventCode = "object_storage.mtls_principal.revoke"
	record.Permission = "object-storage:access-key:write"
	record.ResourceType = "mtls_principal"
	record.ResourceUID = current.SPIFFESubject
	record.CredentialUID = current.CredentialID.String()
	record.Unmapped = compactAuditDetail(map[string]any{
		"verself.content_sha256": hashForAudit(current.SPIFFESubject),
	})

	if err := s.Store.RevokeCredentialByID(ctx, current.CredentialID, actor, time.Now().UTC()); err != nil {
		return s.finishAdminOp(ctx, record, err)
	}
	return s.finishAdminOp(ctx, record, nil)
}

func (s *Service) ResolvePrincipalBySPIFFE(ctx context.Context, spiffeSubject string) (AccessPrincipal, error) {
	credential, err := s.Store.ActiveCredentialBySPIFFE(ctx, strings.TrimSpace(spiffeSubject))
	if err != nil {
		return AccessPrincipal{}, err
	}
	bucket, err := s.Store.BucketByID(ctx, credential.BucketID)
	if err != nil {
		return AccessPrincipal{}, err
	}
	return AccessPrincipal{Bucket: bucket, Credential: credential}, nil
}

func (s *Service) ResolvePrincipalByAccessKey(ctx context.Context, accessKeyID string) (AccessPrincipal, string, error) {
	credential, err := s.Store.ActiveCredentialByAccessKeyID(ctx, strings.TrimSpace(accessKeyID))
	if err != nil {
		return AccessPrincipal{}, "", err
	}
	if isCredentialExpired(credential, time.Now().UTC()) {
		return AccessPrincipal{}, "", ErrUnauthorized
	}
	secret, err := s.Secrets.Decrypt(credential.SecretCiphertext, credential.SecretNonce)
	if err != nil {
		return AccessPrincipal{}, "", fmt.Errorf("decrypt access key %s: %w", accessKeyID, err)
	}
	bucket, err := s.Store.BucketByID(ctx, credential.BucketID)
	if err != nil {
		return AccessPrincipal{}, "", err
	}
	return AccessPrincipal{Bucket: bucket, Credential: credential}, secret, nil
}

func (s *Service) ResolveBucketTarget(ctx context.Context, aliasOrBucket string) (Bucket, BucketAlias, bool, error) {
	aliasOrBucket = normalizeBucketName(aliasOrBucket)
	bucket, alias, ok, err := s.Store.ResolveBucketAlias(ctx, aliasOrBucket)
	if err != nil {
		return Bucket{}, BucketAlias{}, false, err
	}
	if ok {
		return bucket, alias, true, nil
	}
	bucket, err = s.Store.BucketByName(ctx, aliasOrBucket)
	if err != nil {
		return Bucket{}, BucketAlias{}, false, err
	}
	return bucket, BucketAlias{}, false, nil
}

func (s *Service) RecordAccessEvent(ctx context.Context, event ObjectAccessEvent) error {
	if s == nil || s.CH == nil {
		return fmt.Errorf("object-storage clickhouse connection unavailable")
	}
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	if event.EventDate.IsZero() {
		event.EventDate = event.RecordedAt.UTC().Truncate(24 * time.Hour)
	}
	if sc := oteltrace.SpanContextFromContext(ctx); sc.HasTraceID() && event.TraceID == "" {
		event.TraceID = sc.TraceID().String()
	}
	if sc := oteltrace.SpanContextFromContext(ctx); sc.HasSpanID() && event.SpanID == "" {
		event.SpanID = sc.SpanID().String()
	}
	event.Environment = firstNonEmpty(s.Config.Environment, event.Environment)
	event.ServiceVersion = firstNonEmpty(s.Config.ServiceVersion, event.ServiceVersion)
	event.WriterInstanceID = firstNonEmpty(s.Config.WriterInstanceID, event.WriterInstanceID)
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO verself.object_access_events")
	if err != nil {
		return fmt.Errorf("prepare object access event batch: %w", err)
	}
	if err := appendAccessEvent(batch, event); err != nil {
		return err
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send object access event batch: %w", err)
	}
	return nil
}

func appendAccessEvent(batch chdriver.Batch, event ObjectAccessEvent) error {
	if err := batch.AppendStruct(&event); err != nil {
		return fmt.Errorf("append object access event: %w", err)
	}
	return nil
}

func (s *Service) issueStaticCredential(ctx context.Context, bucket Bucket, displayName string, expiresAt *time.Time, actor string) (staticCredentialMaterial, error) {
	accessKeyID, err := NewAccessKeyID()
	if err != nil {
		return staticCredentialMaterial{}, err
	}
	secretAccessKey, err := NewSecretAccessKey()
	if err != nil {
		return staticCredentialMaterial{}, err
	}
	ciphertext, nonce, err := s.Secrets.Encrypt(secretAccessKey)
	if err != nil {
		return staticCredentialMaterial{}, fmt.Errorf("encrypt secret access key: %w", err)
	}
	now := time.Now().UTC()
	credential := Credential{
		CredentialID:      uuid.New(),
		BucketID:          bucket.BucketID,
		AuthMode:          AuthModeSigV4Static,
		DisplayName:       displayName,
		AccessKeyID:       accessKeyID,
		SecretHash:        hashForAudit(secretAccessKey),
		SecretFingerprint: SecretFingerprint(secretAccessKey),
		SecretCiphertext:  ciphertext,
		SecretNonce:       nonce,
		Status:            CredentialStatusActive,
		ExpiresAt:         expiresAt,
		CreatedAt:         now,
		CreatedBy:         actor,
	}
	if err := s.Store.CreateCredential(ctx, credential); err != nil {
		return staticCredentialMaterial{}, err
	}
	return staticCredentialMaterial{
		credential: credential,
		secret: StaticCredentialSecret{
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
			Fingerprint:     credential.SecretFingerprint,
		},
	}, nil
}

func (s *Service) baseAPIActivity(ctx context.Context, orgID, actor string) APIActivity {
	serviceName := strings.TrimSpace(s.Config.ServiceName)
	if serviceName == "" {
		serviceName = "object-storage-service"
	}
	record := APIActivity{
		OrgID:      strings.TrimSpace(orgID),
		APIService: serviceName,
		ActorType:  "service_workload",
		ActorUID:   strings.TrimSpace(actor),
		Decision:   "Allowed",
		Status:     "Success",
		HTTPStatus: 200,
	}
	if sc := oteltrace.SpanContextFromContext(ctx); sc.HasTraceID() {
		record.TraceUID = sc.TraceID().String()
	}
	return record
}

func (s *Service) finishAdminOp(ctx context.Context, record APIActivity, opErr error) error {
	if record.APIAction == "" {
		record.APIAction = apiActionFromOperation(record.APIOperation)
	}
	if opErr != nil {
		record.Decision = "Allowed"
		record.Status = "Failure"
		record.HTTPStatus = 500
		record.StatusCode = classifyError(opErr)
	}
	if auditErr := s.emitAPIActivity(ctx, record); auditErr != nil {
		if opErr != nil {
			return errors.Join(opErr, auditErr)
		}
		return auditErr
	}
	return opErr
}

func compactAuditDetail(values map[string]any) map[string]any {
	detail := make(map[string]any, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				detail[key] = typed
			}
		case nil:
		default:
			detail[key] = value
		}
	}
	return detail
}

func (s *Service) emitAPIActivity(ctx context.Context, record APIActivity) error {
	if s == nil || s.auditSink == nil {
		return nil
	}
	return s.auditSink(ctx, record)
}

func apiActionFromOperation(operation string) string {
	parts := strings.Split(strings.TrimSpace(operation), ".")
	if len(parts) == 0 {
		return "update"
	}
	switch parts[len(parts)-1] {
	case "create", "update", "delete", "roll", "revoke":
		return parts[len(parts)-1]
	default:
		return "update"
	}
}

func normalizeBucketName(raw string) string {
	return strings.TrimSpace(strings.ToLower(raw))
}

func normalizePrefix(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	return raw + "/"
}

func normalizeLifecycleJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("[]")
	}
	return json.RawMessage(append([]byte(nil), raw...))
}

func invalidArgument(span oteltrace.Span, message string) error {
	err := fmt.Errorf("object-storage invalid argument: %s", message)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

func hashForAudit(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func classifyError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrConflict):
		return "conflict"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrUnauthorized):
		return "unauthorized"
	default:
		return "internal"
	}
}

func isCredentialExpired(credential Credential, now time.Time) bool {
	return credential.ExpiresAt != nil && !credential.ExpiresAt.After(now.UTC())
}
