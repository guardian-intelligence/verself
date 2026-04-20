package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/forge-metal/secrets-service/internal/secrets"
)

func RegisterRoutes(api huma.API, svc *secrets.Service) {
	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "put-secret",
		Method:        http.MethodPut,
		Path:          "/api/v1/secrets/{name}",
		Summary:       "Create or rotate a secret or variable",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:      permissionSecretWrite,
		TargetKind:      "secret",
		Action:          "write",
		OrgScope:        "token_org_id",
		RateLimitClass:  "secret_mutation",
		Idempotency:     idempotencyHeaderKey,
		AuditEvent:      "secrets.secret.write",
		BodyLimitBytes:  bodyLimitSmallJSON,
		SecretOperation: "write",
		OpenBaoRole:     "secrets-direct-put-secret",
		BillingSKU:      billingSKUSecretsKV,
	}), putSecret(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "read-secret",
		Method:      http.MethodGet,
		Path:        "/api/v1/secrets/{name}",
		Summary:     "Resolve and read a secret or variable",
	}, operationPolicy{
		Permission:      permissionSecretRead,
		TargetKind:      "secret",
		Action:          "read",
		OrgScope:        "token_org_id",
		RateLimitClass:  "read",
		AuditEvent:      "secrets.secret.read",
		RiskLevel:       "critical",
		SecretOperation: "read",
		OpenBaoRole:     "secrets-direct-read-secret",
		BillingSKU:      billingSKUSecretsKV,
	}), readSecret(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "list-secrets",
		Method:      http.MethodGet,
		Path:        "/api/v1/secrets",
		Summary:     "List secret or variable metadata",
	}, operationPolicy{
		Permission:      permissionSecretList,
		TargetKind:      "secret",
		Action:          "list",
		OrgScope:        "token_org_id",
		RateLimitClass:  "read",
		AuditEvent:      "secrets.secret.list",
		RiskLevel:       "high",
		SecretOperation: "list",
		OpenBaoRole:     "secrets-direct-list-secrets",
		BillingSKU:      billingSKUSecretsKV,
	}), listSecrets(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "delete-secret",
		Method:      http.MethodDelete,
		Path:        "/api/v1/secrets/{name}",
		Summary:     "Soft-delete a secret or variable",
	}, operationPolicy{
		Permission:      permissionSecretDelete,
		TargetKind:      "secret",
		Action:          "delete",
		OrgScope:        "token_org_id",
		RateLimitClass:  "secret_mutation",
		Idempotency:     idempotencyHeaderKey,
		AuditEvent:      "secrets.secret.delete",
		BodyLimitBytes:  bodyLimitSmallJSON,
		SecretOperation: "delete",
		OpenBaoRole:     "secrets-direct-delete-secret",
		BillingSKU:      billingSKUSecretsKV,
	}), deleteSecret(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "create-transit-key",
		Method:        http.MethodPost,
		Path:          "/api/v1/transit/keys",
		Summary:       "Create a transit key",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:      permissionTransitKeyCreate,
		TargetKind:      "transit_key",
		Action:          "create",
		OrgScope:        "token_org_id",
		RateLimitClass:  "key_mutation",
		Idempotency:     idempotencyHeaderKey,
		AuditEvent:      "secrets.transit_key.create",
		BodyLimitBytes:  bodyLimitSmallJSON,
		SecretOperation: "key_create",
		OpenBaoRole:     "secrets-direct-create-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), createTransitKey(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "rotate-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/rotate",
		Summary:     "Rotate a transit key",
	}, operationPolicy{
		Permission:      permissionTransitKeyRotate,
		TargetKind:      "transit_key",
		Action:          "rotate",
		OrgScope:        "token_org_id",
		RateLimitClass:  "key_mutation",
		Idempotency:     idempotencyHeaderKey,
		AuditEvent:      "secrets.transit_key.rotate",
		BodyLimitBytes:  bodyLimitSmallJSON,
		SecretOperation: "key_rotate",
		OpenBaoRole:     "secrets-direct-rotate-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), rotateTransitKey(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "encrypt-with-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/encrypt",
		Summary:     "Encrypt with a transit key",
	}, operationPolicy{
		Permission:      permissionTransitEncrypt,
		TargetKind:      "transit_key",
		Action:          "encrypt",
		OrgScope:        "token_org_id",
		RateLimitClass:  "crypto",
		AuditEvent:      "secrets.transit_key.encrypt",
		BodyLimitBytes:  bodyLimitCryptoJSON,
		SecretOperation: "encrypt",
		OpenBaoRole:     "secrets-direct-encrypt-with-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), encryptTransit(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "decrypt-with-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/decrypt",
		Summary:     "Decrypt with a transit key",
	}, operationPolicy{
		Permission:      permissionTransitDecrypt,
		TargetKind:      "transit_key",
		Action:          "decrypt",
		OrgScope:        "token_org_id",
		RateLimitClass:  "crypto",
		AuditEvent:      "secrets.transit_key.decrypt",
		RiskLevel:       "critical",
		BodyLimitBytes:  bodyLimitCryptoJSON,
		SecretOperation: "decrypt",
		OpenBaoRole:     "secrets-direct-decrypt-with-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), decryptTransit(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "sign-with-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/sign",
		Summary:     "Sign with a transit key",
	}, operationPolicy{
		Permission:      permissionTransitSign,
		TargetKind:      "transit_key",
		Action:          "sign",
		OrgScope:        "token_org_id",
		RateLimitClass:  "crypto",
		AuditEvent:      "secrets.transit_key.sign",
		BodyLimitBytes:  bodyLimitCryptoJSON,
		SecretOperation: "sign",
		OpenBaoRole:     "secrets-direct-sign-with-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), signTransit(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "verify-with-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/verify",
		Summary:     "Verify with a transit key",
	}, operationPolicy{
		Permission:      permissionTransitVerify,
		TargetKind:      "transit_key",
		Action:          "verify",
		OrgScope:        "token_org_id",
		RateLimitClass:  "crypto",
		AuditEvent:      "secrets.transit_key.verify",
		RiskLevel:       "high",
		BodyLimitBytes:  bodyLimitCryptoJSON,
		SecretOperation: "verify",
		OpenBaoRole:     "secrets-direct-verify-with-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), verifyTransit(svc))
}

type secretScopeQuery struct {
	Kind       string `query:"kind,omitempty" enum:"secret,variable"`
	ScopeLevel string `query:"scope_level,omitempty" enum:"org,source,environment,branch"`
	SourceID   string `query:"source_id,omitempty" maxLength:"255"`
	EnvID      string `query:"env_id,omitempty" maxLength:"255"`
	Branch     string `query:"branch,omitempty" maxLength:"255"`
}

type putSecretInput struct {
	Name string `path:"name" minLength:"1" maxLength:"255"`
	Body putSecretBody
}

type putSecretBody struct {
	Kind       string `json:"kind,omitempty" enum:"secret,variable"`
	ScopeLevel string `json:"scope_level,omitempty" enum:"org,source,environment,branch"`
	SourceID   string `json:"source_id,omitempty" maxLength:"255"`
	EnvID      string `json:"env_id,omitempty" maxLength:"255"`
	Branch     string `json:"branch,omitempty" maxLength:"255"`
	Value      string `json:"value" maxLength:"65536"`
}

type readSecretInput struct {
	Name string `path:"name" minLength:"1" maxLength:"255"`
	secretScopeQuery
}

type listSecretsInput struct {
	Kind  string `query:"kind,omitempty" enum:"secret,variable"`
	Limit int    `query:"limit,omitempty" minimum:"1" maximum:"200"`
}

type deleteSecretInput struct {
	Name       string    `path:"name" minLength:"1" maxLength:"255"`
	Kind       string    `query:"kind,omitempty" enum:"secret,variable"`
	ScopeLevel string    `query:"scope_level,omitempty" enum:"org,source,environment,branch"`
	SourceID   string    `query:"source_id,omitempty" maxLength:"255"`
	EnvID      string    `query:"env_id,omitempty" maxLength:"255"`
	Branch     string    `query:"branch,omitempty" maxLength:"255"`
	Body       *struct{} `json:"-"`
}

type secretOutput struct {
	Body SecretDTO
}

type secretValueOutput struct {
	Body SecretValueDTO
}

type secretsOutput struct {
	Body SecretsDTO
}

type SecretDTO struct {
	SecretID       string `json:"secret_id"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	ScopeLevel     string `json:"scope_level"`
	SourceID       string `json:"source_id,omitempty"`
	EnvID          string `json:"env_id,omitempty"`
	Branch         string `json:"branch,omitempty"`
	CurrentVersion string `json:"current_version"`
	CreatedAt      string `json:"created_at" format:"date-time"`
	UpdatedAt      string `json:"updated_at" format:"date-time"`
}

type SecretValueDTO struct {
	SecretDTO
	Value string `json:"value"`
}

type SecretsDTO struct {
	Secrets []SecretDTO `json:"secrets"`
}

func putSecret(svc *secrets.Service) func(context.Context, secrets.Principal, *putSecretInput) (*secretOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *putSecretInput) (*secretOutput, error) {
		record, err := svc.PutSecret(ctx, principal, secrets.PutSecretRequest{
			Kind: input.Body.Kind,
			Name: input.Name,
			Scope: secrets.Scope{
				Level:    input.Body.ScopeLevel,
				SourceID: input.Body.SourceID,
				EnvID:    input.Body.EnvID,
				Branch:   input.Body.Branch,
			},
			Value: input.Body.Value,
		})
		if err != nil {
			return nil, err
		}
		return &secretOutput{Body: secretDTO(record)}, nil
	}
}

func readSecret(svc *secrets.Service) func(context.Context, secrets.Principal, *readSecretInput) (*secretValueOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *readSecretInput) (*secretValueOutput, error) {
		value, err := svc.ReadSecret(ctx, principal, input.Kind, input.Name, secrets.Scope{
			Level:    input.ScopeLevel,
			SourceID: input.SourceID,
			EnvID:    input.EnvID,
			Branch:   input.Branch,
		})
		if err != nil {
			return nil, err
		}
		dto := secretDTO(value.Record)
		return &secretValueOutput{Body: SecretValueDTO{SecretDTO: dto, Value: value.Value}}, nil
	}
}

func listSecrets(svc *secrets.Service) func(context.Context, secrets.Principal, *listSecretsInput) (*secretsOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *listSecretsInput) (*secretsOutput, error) {
		records, err := svc.ListSecrets(ctx, principal, input.Kind, input.Limit)
		if err != nil {
			return nil, err
		}
		out := SecretsDTO{Secrets: make([]SecretDTO, 0, len(records))}
		for _, record := range records {
			out.Secrets = append(out.Secrets, secretDTO(record))
		}
		return &secretsOutput{Body: out}, nil
	}
}

func deleteSecret(svc *secrets.Service) func(context.Context, secrets.Principal, *deleteSecretInput) (*secretOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *deleteSecretInput) (*secretOutput, error) {
		record, err := svc.DeleteSecret(ctx, principal, input.Kind, input.Name, secrets.Scope{
			Level:    input.ScopeLevel,
			SourceID: input.SourceID,
			EnvID:    input.EnvID,
			Branch:   input.Branch,
		})
		if err != nil {
			return nil, err
		}
		return &secretOutput{Body: secretDTO(record)}, nil
	}
}

type createTransitKeyInput struct {
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"255"`
	}
}

type transitKeyOutput struct {
	Body TransitKeyDTO
}

type TransitKeyDTO struct {
	KeyID          string `json:"key_id"`
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version"`
	PublicKey      string `json:"public_key"`
	CreatedAt      string `json:"created_at" format:"date-time"`
	UpdatedAt      string `json:"updated_at" format:"date-time"`
}

func createTransitKey(svc *secrets.Service) func(context.Context, secrets.Principal, *createTransitKeyInput) (*transitKeyOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *createTransitKeyInput) (*transitKeyOutput, error) {
		key, err := svc.CreateTransitKey(ctx, principal, input.Body.Name)
		if err != nil {
			return nil, err
		}
		return &transitKeyOutput{Body: transitKeyDTO(key)}, nil
	}
}

type rotateTransitKeyInput struct {
	KeyName string `path:"key_name" minLength:"1" maxLength:"255"`
}

func rotateTransitKey(svc *secrets.Service) func(context.Context, secrets.Principal, *rotateTransitKeyInput) (*transitKeyOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *rotateTransitKeyInput) (*transitKeyOutput, error) {
		key, err := svc.RotateTransitKey(ctx, principal, input.KeyName)
		if err != nil {
			return nil, err
		}
		return &transitKeyOutput{Body: transitKeyDTO(key)}, nil
	}
}

type transitPayloadInput struct {
	KeyName string `path:"key_name" minLength:"1" maxLength:"255"`
	Body    struct {
		PlaintextBase64 string `json:"plaintext_base64,omitempty" maxLength:"262144"`
		Ciphertext      string `json:"ciphertext,omitempty" maxLength:"262144"`
		MessageBase64   string `json:"message_base64,omitempty" maxLength:"262144"`
		Signature       string `json:"signature,omitempty" maxLength:"262144"`
	}
}

type encryptOutput struct {
	Body struct {
		Ciphertext string `json:"ciphertext"`
		Version    string `json:"version"`
	}
}

type decryptOutput struct {
	Body struct {
		PlaintextBase64 string `json:"plaintext_base64"`
	}
}

type signOutput struct {
	Body struct {
		Signature string `json:"signature"`
	}
}

type verifyOutput struct {
	Body struct {
		Valid bool `json:"valid"`
	}
}

func encryptTransit(svc *secrets.Service) func(context.Context, secrets.Principal, *transitPayloadInput) (*encryptOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *transitPayloadInput) (*encryptOutput, error) {
		plaintext, err := base64.StdEncoding.DecodeString(input.Body.PlaintextBase64)
		if err != nil {
			return nil, secrets.ErrInvalidArgument
		}
		ciphertext, err := svc.TransitEncrypt(ctx, principal, input.KeyName, plaintext)
		if err != nil {
			return nil, err
		}
		out := &encryptOutput{}
		out.Body.Ciphertext = ciphertext.Ciphertext
		out.Body.Version = strconv.FormatUint(ciphertext.Version, 10)
		return out, nil
	}
}

func decryptTransit(svc *secrets.Service) func(context.Context, secrets.Principal, *transitPayloadInput) (*decryptOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *transitPayloadInput) (*decryptOutput, error) {
		plaintext, _, err := svc.TransitDecrypt(ctx, principal, input.KeyName, input.Body.Ciphertext)
		if err != nil {
			return nil, err
		}
		out := &decryptOutput{}
		out.Body.PlaintextBase64 = base64.StdEncoding.EncodeToString(plaintext)
		return out, nil
	}
}

func signTransit(svc *secrets.Service) func(context.Context, secrets.Principal, *transitPayloadInput) (*signOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *transitPayloadInput) (*signOutput, error) {
		message, err := base64.StdEncoding.DecodeString(input.Body.MessageBase64)
		if err != nil {
			return nil, secrets.ErrInvalidArgument
		}
		signature, _, err := svc.TransitSign(ctx, principal, input.KeyName, message)
		if err != nil {
			return nil, err
		}
		out := &signOutput{}
		out.Body.Signature = signature
		return out, nil
	}
}

func verifyTransit(svc *secrets.Service) func(context.Context, secrets.Principal, *transitPayloadInput) (*verifyOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *transitPayloadInput) (*verifyOutput, error) {
		message, err := base64.StdEncoding.DecodeString(input.Body.MessageBase64)
		if err != nil {
			return nil, secrets.ErrInvalidArgument
		}
		valid, _, err := svc.TransitVerify(ctx, principal, input.KeyName, message, input.Body.Signature)
		if err != nil {
			return nil, err
		}
		out := &verifyOutput{}
		out.Body.Valid = valid
		return out, nil
	}
}

func secretDTO(record secrets.SecretRecord) SecretDTO {
	return SecretDTO{
		SecretID:       record.SecretID,
		Kind:           record.Kind,
		Name:           record.Name,
		ScopeLevel:     record.Scope.Level,
		SourceID:       record.Scope.SourceID,
		EnvID:          record.Scope.EnvID,
		Branch:         record.Scope.Branch,
		CurrentVersion: strconv.FormatUint(record.CurrentVersion, 10),
		CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func transitKeyDTO(key secrets.TransitKey) TransitKeyDTO {
	return TransitKeyDTO{
		KeyID:          key.KeyID,
		Name:           key.Name,
		CurrentVersion: strconv.FormatUint(key.CurrentVersion, 10),
		PublicKey:      key.PublicKey,
		CreatedAt:      key.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      key.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}
