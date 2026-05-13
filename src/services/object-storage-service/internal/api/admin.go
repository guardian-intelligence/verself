package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/object-storage-service/internal/contractapi"
	"github.com/verself/object-storage-service/internal/objectstorage"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func RegisterAdminRoutes(api huma.API, svc *objectstorage.Service, authorizer runtimeiam.OperationAuthorizer) {
	op, policy := objectStorageContract(contractapi.ListObjectStorageBuckets.Descriptor, "List object storage buckets")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, _ *contractapi.EmptyInput) (*contractapi.ListObjectStorageBucketsOutput, error) {
		buckets, err := svc.Store.ListBucketsByOrg(ctx, principal.OrgID)
		if err != nil {
			return nil, problem(ctx, http.StatusInternalServerError, "list-buckets-failed", "list buckets failed", err)
		}
		out, err := bucketViewsFromDomain(buckets)
		if err != nil {
			return nil, problem(ctx, http.StatusInternalServerError, "invalid-lifecycle-rules", "stored lifecycle_rules were invalid", err)
		}
		return &contractapi.ListObjectStorageBucketsOutput{Body: out}, nil
	})

	op, policy = objectStorageContract(contractapi.CreateObjectStorageBucket.Descriptor, "Create object storage bucket")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.CreateObjectStorageBucketInput) (*contractapi.BucketOutput, error) {
		quotaBytes, err := quotaFromWire(input.Body.QuotaBytes)
		if err != nil {
			return nil, badRequest(ctx, "invalid-quota-bytes", err.Error())
		}
		quotaObjects, err := quotaFromWire(input.Body.QuotaObjects)
		if err != nil {
			return nil, badRequest(ctx, "invalid-quota-objects", err.Error())
		}
		lifecycleRules, err := lifecycleRulesFromContract(input.Body.LifecycleRules)
		if err != nil {
			return nil, badRequest(ctx, "invalid-lifecycle-rules", "lifecycle_rules must be JSON lifecycle rule objects")
		}
		bucket, err := svc.CreateBucket(ctx, objectstorage.CreateBucketInput{
			OrgID:         principal.OrgID,
			BucketName:    string(input.Body.BucketName),
			QuotaBytes:    quotaBytes,
			QuotaObjects:  quotaObjects,
			LifecycleJSON: lifecycleRules,
			Actor:         principal.Actor,
		})
		if err != nil {
			return nil, toHumaError(ctx, "create bucket failed", err)
		}
		return bucketOutputFromDomain(ctx, bucket)
	})

	op, policy = objectStorageContract(contractapi.GetObjectStorageBucket.Descriptor, "Get object storage bucket")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.BucketPathInput) (*contractapi.BucketOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		bucket, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID)
		if err != nil {
			return nil, err
		}
		return bucketOutputFromDomain(ctx, bucket)
	})

	op, policy = objectStorageContract(contractapi.UpdateObjectStorageBucket.Descriptor, "Update object storage bucket")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.UpdateObjectStorageBucketInput) (*contractapi.BucketOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
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
		lifecycleRules, err := lifecycleRulesFromContract(input.Body.LifecycleRules)
		if err != nil {
			return nil, badRequest(ctx, "invalid-lifecycle-rules", "lifecycle_rules must be JSON lifecycle rule objects")
		}
		if _, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID); err != nil {
			return nil, err
		}
		bucket, err := svc.UpdateBucket(ctx, bucketID, quotaBytes, quotaObjects, lifecycleRules, principal.Actor)
		if err != nil {
			return nil, toHumaError(ctx, "update bucket failed", err)
		}
		return bucketOutputFromDomain(ctx, bucket)
	})

	op, policy = objectStorageContract(contractapi.CreateObjectStorageBucketAlias.Descriptor, "Create object storage bucket alias")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.CreateObjectStorageBucketAliasInput) (*contractapi.BucketAliasOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if _, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID); err != nil {
			return nil, err
		}
		alias, err := svc.CreateAlias(ctx, objectstorage.CreateAliasInput{
			BucketID:   bucketID,
			Alias:      string(input.Body.Alias),
			Prefix:     objectPrefixToString(input.Body.Prefix),
			ServiceTag: serviceTagToString(input.Body.ServiceTag),
			Actor:      principal.Actor,
		})
		if err != nil {
			return nil, toHumaError(ctx, "create alias failed", err)
		}
		return &contractapi.BucketAliasOutput{Body: toAliasView(alias)}, nil
	})

	op, policy = objectStorageContract(contractapi.ListObjectStorageBucketAliases.Descriptor, "List object storage bucket aliases")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.BucketPathInput) (*contractapi.ListObjectStorageBucketAliasesOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if _, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID); err != nil {
			return nil, err
		}
		aliases, err := svc.Store.AliasesByBucket(ctx, bucketID)
		if err != nil {
			return nil, problem(ctx, http.StatusInternalServerError, "list-aliases-failed", "list aliases failed", err)
		}
		out := make(contractapi.BucketAliases, 0, len(aliases))
		for _, alias := range aliases {
			out = append(out, toAliasView(alias))
		}
		return &contractapi.ListObjectStorageBucketAliasesOutput{Body: out}, nil
	})

	op, policy = objectStorageContract(contractapi.DeleteObjectStorageBucketAlias.Descriptor, "Delete object storage bucket alias")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.DeleteObjectStorageBucketAliasInput) (*contractapi.EmptyOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if _, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID); err != nil {
			return nil, err
		}
		if err := svc.DeleteAlias(ctx, bucketID, string(input.Alias), principal.Actor); err != nil {
			return nil, toHumaError(ctx, "delete alias failed", err)
		}
		return emptyOutput(), nil
	})

	op, policy = objectStorageContract(contractapi.ListObjectStorageCredentials.Descriptor, "List object storage credentials")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.BucketPathInput) (*contractapi.ListObjectStorageCredentialsOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if _, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID); err != nil {
			return nil, err
		}
		credentials, err := svc.Store.CredentialsByBucket(ctx, bucketID)
		if err != nil {
			return nil, problem(ctx, http.StatusInternalServerError, "list-credentials-failed", "list credentials failed", err)
		}
		out := make(contractapi.ObjectStorageCredentials, 0, len(credentials))
		for _, credential := range credentials {
			out = append(out, toCredentialView(credential))
		}
		return &contractapi.ListObjectStorageCredentialsOutput{Body: out}, nil
	})

	op, policy = objectStorageContract(contractapi.CreateObjectStorageAccessKey.Descriptor, "Create object storage access key")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.CreateObjectStorageAccessKeyInput) (*contractapi.ObjectStorageAccessKeySecretOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if _, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID); err != nil {
			return nil, err
		}
		expiresAt, err := parseOptionalRFC3339(input.Body.ExpiresAt)
		if err != nil {
			return nil, badRequest(ctx, "invalid-expires-at", "expires_at must be RFC3339")
		}
		credential, secret, err := svc.CreateStaticCredential(ctx, objectstorage.CreateStaticCredentialInput{
			BucketID:    bucketID,
			DisplayName: string(input.Body.DisplayName),
			ExpiresAt:   expiresAt,
			Actor:       principal.Actor,
		})
		if err != nil {
			return nil, toHumaError(ctx, "create access key failed", err)
		}
		return credentialSecretOutput(credential, secret), nil
	})

	op, policy = objectStorageContract(contractapi.CreateObjectStorageMtlsPrincipal.Descriptor, "Create object storage mTLS principal")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.CreateObjectStorageMTLSPrincipalInput) (*contractapi.BucketOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if _, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID); err != nil {
			return nil, err
		}
		if _, err := svc.CreateSPIFFECredential(ctx, objectstorage.CreateSPIFFECredentialInput{
			BucketID:      bucketID,
			DisplayName:   string(input.Body.DisplayName),
			SPIFFESubject: string(input.Body.SpiffeSubject),
			Actor:         principal.Actor,
		}); err != nil {
			return nil, toHumaError(ctx, "create mTLS principal failed", err)
		}
		bucket, err := svc.Store.BucketByID(ctx, bucketID)
		if err != nil {
			return nil, problem(ctx, http.StatusInternalServerError, "bucket-reload-failed", "reload bucket failed", err)
		}
		return bucketOutputFromDomain(ctx, bucket)
	})

	op, policy = objectStorageContract(contractapi.DeleteObjectStorageMtlsPrincipal.Descriptor, "Delete object storage mTLS principal")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.DeleteObjectStorageMTLSPrincipalInput) (*contractapi.EmptyOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		credentialID, err := credentialIDFromContract(input.CredentialID)
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
		if _, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID); err != nil {
			return nil, err
		}
		if err := svc.RevokeSPIFFECredential(ctx, credentialID, principal.Actor); err != nil {
			return nil, toHumaError(ctx, "revoke mTLS principal failed", err)
		}
		return emptyOutput(), nil
	})

	op, policy = objectStorageContract(contractapi.RollObjectStorageAccessKey.Descriptor, "Roll object storage access key")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.AccessKeyIdempotentInput) (*contractapi.ObjectStorageAccessKeySecretOutput, error) {
		if err := requireAccessKeyInOrg(ctx, svc, string(input.AccessKeyID), principal.OrgID); err != nil {
			return nil, err
		}
		credential, secret, err := svc.RollStaticCredential(ctx, string(input.AccessKeyID), principal.Actor)
		if err != nil {
			return nil, toHumaError(ctx, "roll access key failed", err)
		}
		return credentialSecretOutput(credential, secret), nil
	})

	op, policy = objectStorageContract(contractapi.DeleteObjectStorageAccessKey.Descriptor, "Delete object storage access key")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.AccessKeyIdempotentInput) (*contractapi.EmptyOutput, error) {
		if err := requireAccessKeyInOrg(ctx, svc, string(input.AccessKeyID), principal.OrgID); err != nil {
			return nil, err
		}
		if err := svc.RevokeStaticCredential(ctx, string(input.AccessKeyID), principal.Actor); err != nil {
			return nil, toHumaError(ctx, "revoke access key failed", err)
		}
		return emptyOutput(), nil
	})

	op, policy = objectStorageContract(contractapi.DeleteObjectStorageBucket.Descriptor, "Delete object storage bucket")
	registerAdminRoute(api, authorizer, op, policy, func(ctx context.Context, principal operationPrincipal, input *contractapi.BucketIdempotentInput) (*contractapi.EmptyOutput, error) {
		bucketID, err := bucketIDFromContract(input.BucketID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-bucket-id", "bucket_id must be a UUID")
		}
		if _, err := requireBucketInOrg(ctx, svc, bucketID, principal.OrgID); err != nil {
			return nil, err
		}
		if err := svc.DeleteBucket(ctx, bucketID, principal.Actor); err != nil {
			return nil, toHumaError(ctx, "delete bucket failed", err)
		}
		return emptyOutput(), nil
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

func requireBucketInOrg(ctx context.Context, svc *objectstorage.Service, bucketID uuid.UUID, orgID string) (objectstorage.Bucket, error) {
	bucket, err := svc.Store.BucketByID(ctx, bucketID)
	if err != nil {
		return objectstorage.Bucket{}, toHumaError(ctx, "bucket not found", err)
	}
	if bucket.OrgID != strings.TrimSpace(orgID) {
		return objectstorage.Bucket{}, problem(ctx, http.StatusNotFound, "not-found", "bucket not found", objectstorage.ErrNotFound)
	}
	return bucket, nil
}

func requireAccessKeyInOrg(ctx context.Context, svc *objectstorage.Service, accessKeyID, orgID string) error {
	credential, err := svc.Store.ActiveCredentialByAccessKeyID(ctx, strings.TrimSpace(accessKeyID))
	if err != nil {
		return toHumaError(ctx, "access key not found", err)
	}
	_, err = requireBucketInOrg(ctx, svc, credential.BucketID, orgID)
	return err
}

func bucketOutputFromDomain(ctx context.Context, bucket objectstorage.Bucket) (*contractapi.BucketOutput, error) {
	view, err := toBucketView(bucket)
	if err != nil {
		return nil, problem(ctx, http.StatusInternalServerError, "invalid-lifecycle-rules", "stored lifecycle_rules were invalid", err)
	}
	return &contractapi.BucketOutput{Body: view}, nil
}

func bucketViewsFromDomain(buckets []objectstorage.Bucket) (contractapi.Buckets, error) {
	out := make(contractapi.Buckets, 0, len(buckets))
	for _, bucket := range buckets {
		view, err := toBucketView(bucket)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

func toBucketView(bucket objectstorage.Bucket) (contractapi.BucketView, error) {
	lifecycleRules, err := lifecycleRulesToContract(bucket.LifecycleJSON)
	if err != nil {
		return contractapi.BucketView{}, err
	}
	return contractapi.BucketView{
		BucketID:       contractapi.BucketID(bucket.BucketID.String()),
		ResourceName:   bucketResourceName(bucket.BucketID),
		OrgID:          contractapi.OrgID(bucket.OrgID),
		BucketName:     contractapi.BucketName(bucket.BucketName),
		QuotaBytes:     quotaToWire(bucket.QuotaBytes),
		QuotaObjects:   quotaToWire(bucket.QuotaObjects),
		LifecycleRules: lifecycleRules,
		CreatedAt:      bucket.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      bucket.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func toAliasView(alias objectstorage.BucketAlias) contractapi.BucketAliasView {
	return contractapi.BucketAliasView{
		Alias:      contractapi.BucketAliasName(alias.Alias),
		BucketID:   contractapi.BucketID(alias.BucketID.String()),
		Prefix:     objectPrefixPtr(alias.Prefix),
		ServiceTag: serviceTagPtr(alias.ServiceTag),
		CreatedAt:  alias.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toCredentialView(credential objectstorage.Credential) contractapi.ObjectStorageCredentialView {
	out := contractapi.ObjectStorageCredentialView{
		CredentialID:          contractapi.CredentialID(credential.CredentialID.String()),
		BucketID:              contractapi.BucketID(credential.BucketID.String()),
		AuthMode:              contractapi.AuthMode(credential.AuthMode),
		DisplayName:           contractapi.DisplayName(credential.DisplayName),
		AccessKeyID:           accessKeyIDPtr(credential.AccessKeyID),
		SpiffeSubject:         spiffeSubjectPtr(credential.SPIFFESubject),
		CredentialFingerprint: credentialFingerprintPtr(credential.SecretFingerprint),
		Status:                contractapi.CredentialStatus(credential.Status),
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

func credentialSecretOutput(credential objectstorage.Credential, secret objectstorage.StaticCredentialSecret) *contractapi.ObjectStorageAccessKeySecretOutput {
	return &contractapi.ObjectStorageAccessKeySecretOutput{Body: contractapi.ObjectStorageAccessKeySecret{
		AccessKeyID:           contractapi.AccessKeyID(secret.AccessKeyID),
		SecretAccessKey:       contractapi.SecretAccessKey(secret.SecretAccessKey),
		CredentialFingerprint: contractapi.CredentialFingerprint(secret.Fingerprint),
		Credential:            toCredentialView(credential),
	}}
}

func quotaFromWire(value *contractapi.DecimalUint64) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(string(*value)), 10, 64)
	if err != nil {
		return nil, errors.New("quota must be an unsigned decimal integer")
	}
	if parsed > math.MaxInt64 {
		return nil, errors.New("quota exceeds int64 storage range")
	}
	out := int64FromUint64(parsed, "quota")
	return &out, nil
}

func quotaToWire(value *int64) *contractapi.DecimalUint64 {
	if value == nil {
		return nil
	}
	wire := contractapi.DecimalUint64(strconv.FormatUint(uint64FromInt64(*value, "quota"), 10))
	return &wire
}

func lifecycleRulesFromContract(value *contractapi.ObjectStorageLifecycleRules) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(*value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func lifecycleRulesToContract(raw json.RawMessage) (contractapi.ObjectStorageLifecycleRules, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return contractapi.ObjectStorageLifecycleRules{}, nil
	}
	var rules contractapi.ObjectStorageLifecycleRules
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, err
	}
	if rules == nil {
		return contractapi.ObjectStorageLifecycleRules{}, nil
	}
	return rules, nil
}

func bucketIDFromContract(value contractapi.BucketID) (uuid.UUID, error) {
	return uuid.Parse(strings.TrimSpace(string(value)))
}

func credentialIDFromContract(value contractapi.CredentialID) (uuid.UUID, error) {
	return uuid.Parse(strings.TrimSpace(string(value)))
}

func bucketResourceName(bucketID uuid.UUID) contractapi.ResourceName {
	return contractapi.ResourceName("urn:verself:object-storage:bucket:" + bucketID.String())
}

func parseOptionalRFC3339(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*value))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func emptyOutput() *contractapi.EmptyOutput {
	return &contractapi.EmptyOutput{Body: contractapi.EmptyOutputBody{}}
}

func objectPrefixToString(value *contractapi.ObjectPrefix) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func serviceTagToString(value *contractapi.ServiceTag) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func objectPrefixPtr(value string) *contractapi.ObjectPrefix {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := contractapi.ObjectPrefix(value)
	return &out
}

func serviceTagPtr(value string) *contractapi.ServiceTag {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := contractapi.ServiceTag(value)
	return &out
}

func accessKeyIDPtr(value string) *contractapi.AccessKeyID {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := contractapi.AccessKeyID(value)
	return &out
}

func spiffeSubjectPtr(value string) *contractapi.SPIFFESubject {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := contractapi.SPIFFESubject(value)
	return &out
}

func credentialFingerprintPtr(value string) *contractapi.CredentialFingerprint {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := contractapi.CredentialFingerprint(value)
	return &out
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
