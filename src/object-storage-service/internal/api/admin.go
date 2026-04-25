package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/apiwire"
	workloadauth "github.com/verself/auth-middleware/workload"
	"github.com/verself/object-storage-service/internal/objectstorage"
)

type bucketListOutput struct {
	Body []bucketView
}

type bucketOutput struct {
	Body bucketView
}

type createBucketInput struct {
	Body struct {
		OrgID        string                 `json:"org_id"`
		BucketName   string                 `json:"bucket_name"`
		QuotaBytes   *apiwire.DecimalUint64 `json:"quota_bytes,omitempty"`
		QuotaObjects *apiwire.DecimalUint64 `json:"quota_objects,omitempty"`
		Lifecycle    json.RawMessage        `json:"lifecycle_rules,omitempty"`
	}
}

type bucketPath struct {
	BucketID string `path:"bucket_id"`
}

type updateBucketInput struct {
	BucketID string `path:"bucket_id"`
	Body     struct {
		QuotaBytes   *apiwire.DecimalUint64 `json:"quota_bytes,omitempty"`
		QuotaObjects *apiwire.DecimalUint64 `json:"quota_objects,omitempty"`
		Lifecycle    json.RawMessage        `json:"lifecycle_rules,omitempty"`
	}
}

type createAliasInput struct {
	BucketID string `path:"bucket_id"`
	Body     struct {
		Alias      string `json:"alias"`
		Prefix     string `json:"prefix,omitempty"`
		ServiceTag string `json:"service_tag,omitempty"`
	}
}

type aliasPath struct {
	BucketID string `path:"bucket_id"`
	Alias    string `path:"alias"`
}

type aliasOutput struct {
	Body aliasView
}

type aliasListOutput struct {
	Body []aliasView
}

type credentialsOutput struct {
	Body []credentialView
}

type createStaticCredentialInput struct {
	BucketID string `path:"bucket_id"`
	Body     struct {
		DisplayName string  `json:"display_name"`
		ExpiresAt   *string `json:"expires_at,omitempty"`
	}
}

type createStaticCredentialOutput struct {
	Body credentialSecretView
}

type createMTLSCredentialInput struct {
	BucketID string `path:"bucket_id"`
	Body     struct {
		DisplayName   string `json:"display_name"`
		SPIFFESubject string `json:"spiffe_subject"`
	}
}

type mtlsCredentialPath struct {
	BucketID     string `path:"bucket_id"`
	CredentialID string `path:"credential_id"`
}

type accessKeyPath struct {
	AccessKeyID string `path:"access_key_id"`
}

type credentialSecretView struct {
	AccessKeyID     string         `json:"access_key_id"`
	SecretAccessKey string         `json:"secret_access_key"`
	Fingerprint     string         `json:"credential_fingerprint"`
	Credential      credentialView `json:"credential"`
}

type bucketView struct {
	BucketID       string                 `json:"bucket_id"`
	OrgID          string                 `json:"org_id"`
	BucketName     string                 `json:"bucket_name"`
	GarageBucketID string                 `json:"garage_bucket_id"`
	QuotaBytes     *apiwire.DecimalUint64 `json:"quota_bytes,omitempty"`
	QuotaObjects   *apiwire.DecimalUint64 `json:"quota_objects,omitempty"`
	Lifecycle      json.RawMessage        `json:"lifecycle_rules"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
}

type aliasView struct {
	Alias      string `json:"alias"`
	BucketID   string `json:"bucket_id"`
	Prefix     string `json:"prefix"`
	ServiceTag string `json:"service_tag"`
	CreatedAt  string `json:"created_at"`
}

type credentialView struct {
	CredentialID          string  `json:"credential_id"`
	BucketID              string  `json:"bucket_id"`
	AuthMode              string  `json:"auth_mode"`
	DisplayName           string  `json:"display_name"`
	AccessKeyID           string  `json:"access_key_id,omitempty"`
	SPIFFESubject         string  `json:"spiffe_subject,omitempty"`
	CredentialFingerprint string  `json:"credential_fingerprint,omitempty"`
	Status                string  `json:"status"`
	ExpiresAt             *string `json:"expires_at,omitempty"`
	CreatedAt             string  `json:"created_at"`
	RevokedAt             *string `json:"revoked_at,omitempty"`
}

func RegisterAdminRoutes(api huma.API, svc *objectstorage.Service) {
	huma.Get(api, "/api/v1/buckets", func(ctx context.Context, _ *struct{}) (*bucketListOutput, error) {
		buckets, err := svc.Store.ListBuckets(ctx)
		if err != nil {
			return nil, problem(ctx, http.StatusInternalServerError, "list-buckets-failed", "list buckets failed", err)
		}
		out := make([]bucketView, 0, len(buckets))
		for _, bucket := range buckets {
			out = append(out, toBucketView(bucket))
		}
		return &bucketListOutput{Body: out}, nil
	})

	huma.Post(api, "/api/v1/buckets", func(ctx context.Context, input *createBucketInput) (*bucketOutput, error) {
		quotaBytes, err := quotaFromWire(input.Body.QuotaBytes)
		if err != nil {
			return nil, badRequest(ctx, "invalid-quota-bytes", err.Error())
		}
		quotaObjects, err := quotaFromWire(input.Body.QuotaObjects)
		if err != nil {
			return nil, badRequest(ctx, "invalid-quota-objects", err.Error())
		}
		bucket, err := svc.CreateBucket(ctx, objectstorage.CreateBucketInput{
			OrgID:         strings.TrimSpace(input.Body.OrgID),
			BucketName:    input.Body.BucketName,
			QuotaBytes:    quotaBytes,
			QuotaObjects:  quotaObjects,
			LifecycleJSON: input.Body.Lifecycle,
			Actor:         actorFromContext(ctx),
		})
		if err != nil {
			return nil, toHumaError(ctx, "create bucket failed", err)
		}
		return &bucketOutput{Body: toBucketView(bucket)}, nil
	})

	huma.Get(api, "/api/v1/buckets/{bucket_id}", func(ctx context.Context, input *bucketPath) (*bucketOutput, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		bucket, err := svc.Store.BucketByID(ctx, bucketID)
		if err != nil {
			return nil, problem(ctx, http.StatusNotFound, "bucket-not-found", "bucket not found", err)
		}
		return &bucketOutput{Body: toBucketView(bucket)}, nil
	})

	huma.Put(api, "/api/v1/buckets/{bucket_id}", func(ctx context.Context, input *updateBucketInput) (*bucketOutput, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		quotaBytes, err := quotaFromWire(input.Body.QuotaBytes)
		if err != nil {
			return nil, badRequest(ctx, "invalid-quota-bytes", err.Error())
		}
		quotaObjects, err := quotaFromWire(input.Body.QuotaObjects)
		if err != nil {
			return nil, badRequest(ctx, "invalid-quota-objects", err.Error())
		}
		bucket, err := svc.UpdateBucket(ctx, bucketID, quotaBytes, quotaObjects, input.Body.Lifecycle, actorFromContext(ctx))
		if err != nil {
			return nil, toHumaError(ctx, "update bucket failed", err)
		}
		return &bucketOutput{Body: toBucketView(bucket)}, nil
	})

	huma.Post(api, "/api/v1/buckets/{bucket_id}/aliases", func(ctx context.Context, input *createAliasInput) (*aliasOutput, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		alias, err := svc.CreateAlias(ctx, objectstorage.CreateAliasInput{
			BucketID:   bucketID,
			Alias:      input.Body.Alias,
			Prefix:     input.Body.Prefix,
			ServiceTag: input.Body.ServiceTag,
			Actor:      actorFromContext(ctx),
		})
		if err != nil {
			return nil, toHumaError(ctx, "create alias failed", err)
		}
		return &aliasOutput{Body: toAliasView(alias)}, nil
	})

	huma.Get(api, "/api/v1/buckets/{bucket_id}/aliases", func(ctx context.Context, input *bucketPath) (*aliasListOutput, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		aliases, err := svc.Store.AliasesByBucket(ctx, bucketID)
		if err != nil {
			return nil, problem(ctx, http.StatusInternalServerError, "list-aliases-failed", "list aliases failed", err)
		}
		out := make([]aliasView, 0, len(aliases))
		for _, alias := range aliases {
			out = append(out, toAliasView(alias))
		}
		return &aliasListOutput{Body: out}, nil
	})

	huma.Delete(api, "/api/v1/buckets/{bucket_id}/aliases/{alias}", func(ctx context.Context, input *aliasPath) (*struct{}, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if err := svc.DeleteAlias(ctx, bucketID, input.Alias, actorFromContext(ctx)); err != nil {
			return nil, toHumaError(ctx, "delete alias failed", err)
		}
		return &struct{}{}, nil
	})

	huma.Get(api, "/api/v1/buckets/{bucket_id}/credentials", func(ctx context.Context, input *bucketPath) (*credentialsOutput, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		credentials, err := svc.Store.CredentialsByBucket(ctx, bucketID)
		if err != nil {
			return nil, problem(ctx, http.StatusInternalServerError, "list-credentials-failed", "list credentials failed", err)
		}
		out := make([]credentialView, 0, len(credentials))
		for _, credential := range credentials {
			out = append(out, toCredentialView(credential))
		}
		return &credentialsOutput{Body: out}, nil
	})

	huma.Post(api, "/api/v1/buckets/{bucket_id}/access-keys", func(ctx context.Context, input *createStaticCredentialInput) (*createStaticCredentialOutput, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		var expiresAt *time.Time
		if input.Body.ExpiresAt != nil && strings.TrimSpace(*input.Body.ExpiresAt) != "" {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*input.Body.ExpiresAt))
			if err != nil {
				return nil, badRequest(ctx, "invalid-expires-at", "expires_at must be RFC3339")
			}
			expiresAt = &parsed
		}
		credential, secret, err := svc.CreateStaticCredential(ctx, objectstorage.CreateStaticCredentialInput{
			BucketID:    bucketID,
			DisplayName: input.Body.DisplayName,
			ExpiresAt:   expiresAt,
			Actor:       actorFromContext(ctx),
		})
		if err != nil {
			return nil, toHumaError(ctx, "create access key failed", err)
		}
		return &createStaticCredentialOutput{Body: credentialSecretView{
			AccessKeyID:     secret.AccessKeyID,
			SecretAccessKey: secret.SecretAccessKey,
			Fingerprint:     secret.Fingerprint,
			Credential:      toCredentialView(credential),
		}}, nil
	})

	huma.Post(api, "/api/v1/buckets/{bucket_id}/mtls-principals", func(ctx context.Context, input *createMTLSCredentialInput) (*bucketOutput, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if _, err := svc.CreateSPIFFECredential(ctx, objectstorage.CreateSPIFFECredentialInput{
			BucketID:      bucketID,
			DisplayName:   input.Body.DisplayName,
			SPIFFESubject: input.Body.SPIFFESubject,
			Actor:         actorFromContext(ctx),
		}); err != nil {
			return nil, toHumaError(ctx, "create mTLS principal failed", err)
		}
		bucket, err := svc.Store.BucketByID(ctx, bucketID)
		if err != nil {
			return nil, problem(ctx, http.StatusInternalServerError, "bucket-reload-failed", "reload bucket failed", err)
		}
		return &bucketOutput{Body: toBucketView(bucket)}, nil
	})

	huma.Delete(api, "/api/v1/buckets/{bucket_id}/mtls-principals/{credential_id}", func(ctx context.Context, input *mtlsCredentialPath) (*struct{}, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		credentialID, err := uuid.Parse(strings.TrimSpace(input.CredentialID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-credential-id", "credential_id must be a UUID")
		}
		credential, err := svc.Store.CredentialByID(ctx, credentialID)
		if err != nil {
			return nil, toHumaError(ctx, "revoke mTLS principal failed", err)
		}
		if credential.BucketID != bucketID {
			return nil, problem(ctx, http.StatusNotFound, "not-found", "revoke mTLS principal failed", objectstorage.ErrNotFound)
		}
		if err := svc.RevokeSPIFFECredential(ctx, credentialID, actorFromContext(ctx)); err != nil {
			return nil, toHumaError(ctx, "revoke mTLS principal failed", err)
		}
		return &struct{}{}, nil
	})

	huma.Post(api, "/api/v1/access-keys/{access_key_id}/roll", func(ctx context.Context, input *accessKeyPath) (*createStaticCredentialOutput, error) {
		credential, secret, err := svc.RollStaticCredential(ctx, input.AccessKeyID, actorFromContext(ctx))
		if err != nil {
			return nil, toHumaError(ctx, "roll access key failed", err)
		}
		return &createStaticCredentialOutput{Body: credentialSecretView{
			AccessKeyID:     secret.AccessKeyID,
			SecretAccessKey: secret.SecretAccessKey,
			Fingerprint:     secret.Fingerprint,
			Credential:      toCredentialView(credential),
		}}, nil
	})

	huma.Delete(api, "/api/v1/access-keys/{access_key_id}", func(ctx context.Context, input *accessKeyPath) (*struct{}, error) {
		if err := svc.RevokeStaticCredential(ctx, input.AccessKeyID, actorFromContext(ctx)); err != nil {
			return nil, toHumaError(ctx, "revoke access key failed", err)
		}
		return &struct{}{}, nil
	})

	huma.Delete(api, "/api/v1/buckets/{bucket_id}", func(ctx context.Context, input *bucketPath) (*struct{}, error) {
		bucketID, err := uuid.Parse(strings.TrimSpace(input.BucketID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if err := svc.DeleteBucket(ctx, bucketID, actorFromContext(ctx)); err != nil {
			return nil, toHumaError(ctx, "delete bucket failed", err)
		}
		return &struct{}{}, nil
	})
}

func toHumaError(ctx context.Context, message string, err error) error {
	switch {
	case errors.Is(err, objectstorage.ErrNotFound):
		return problem(ctx, http.StatusNotFound, "not-found", message, err)
	case errors.Is(err, objectstorage.ErrConflict):
		return problem(ctx, http.StatusConflict, "conflict", message, err)
	case errors.Is(err, objectstorage.ErrUnauthorized):
		return problem(ctx, http.StatusForbidden, "forbidden", message, err)
	case strings.Contains(strings.ToLower(err.Error()), "invalid argument"):
		return problem(ctx, http.StatusBadRequest, "invalid-request", message, err)
	default:
		return problem(ctx, http.StatusInternalServerError, "internal-error", message, err)
	}
}

func actorFromContext(ctx context.Context) string {
	if peerID, ok := workloadauth.PeerIDFromContext(ctx); ok {
		return peerID.String()
	}
	return "unknown"
}

func toBucketView(bucket objectstorage.Bucket) bucketView {
	return bucketView{
		BucketID:       bucket.BucketID.String(),
		OrgID:          bucket.OrgID,
		BucketName:     bucket.BucketName,
		GarageBucketID: bucket.GarageBucketID,
		QuotaBytes:     quotaToWire(bucket.QuotaBytes),
		QuotaObjects:   quotaToWire(bucket.QuotaObjects),
		Lifecycle:      json.RawMessage(append([]byte(nil), bucket.LifecycleJSON...)),
		CreatedAt:      bucket.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      bucket.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toAliasView(alias objectstorage.BucketAlias) aliasView {
	return aliasView{
		Alias:      alias.Alias,
		BucketID:   alias.BucketID.String(),
		Prefix:     alias.Prefix,
		ServiceTag: alias.ServiceTag,
		CreatedAt:  alias.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toCredentialView(credential objectstorage.Credential) credentialView {
	out := credentialView{
		CredentialID:          credential.CredentialID.String(),
		BucketID:              credential.BucketID.String(),
		AuthMode:              credential.AuthMode,
		DisplayName:           credential.DisplayName,
		AccessKeyID:           credential.AccessKeyID,
		SPIFFESubject:         credential.SPIFFESubject,
		CredentialFingerprint: credential.SecretFingerprint,
		Status:                credential.Status,
		CreatedAt:             credential.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if credential.ExpiresAt != nil {
		value := credential.ExpiresAt.UTC().Format(time.RFC3339Nano)
		out.ExpiresAt = &value
	}
	if credential.RevokedAt != nil {
		value := credential.RevokedAt.UTC().Format(time.RFC3339Nano)
		out.RevokedAt = &value
	}
	return out
}

func quotaFromWire(value *apiwire.DecimalUint64) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	if value.Uint64() > math.MaxInt64 {
		return nil, errors.New("quota exceeds int64 storage range")
	}
	parsed := int64(value.Uint64())
	return &parsed, nil
}

func quotaToWire(value *int64) *apiwire.DecimalUint64 {
	if value == nil {
		return nil
	}
	wire := apiwire.Uint64(uint64(*value))
	return &wire
}

func problem(ctx context.Context, status int, code, detail string, cause error) error {
	if cause != nil {
		trace.SpanFromContext(ctx).RecordError(cause)
	}
	instance := ""
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
		instance = "urn:verself:trace:" + spanContext.TraceID().String()
	}
	return &huma.ErrorModel{
		Status:   status,
		Type:     "urn:verself:problem:" + code,
		Title:    http.StatusText(status),
		Detail:   detail,
		Instance: instance,
		Errors: []*huma.ErrorDetail{{
			Message:  code,
			Location: "code",
			Value:    code,
		}},
	}
}

func badRequest(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusBadRequest, code, detail, nil)
}
