package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/secrets-service/internal/contractapi"
	"github.com/verself/secrets-service/internal/secrets"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func RegisterRoutes(api huma.API, svc *secrets.Service, authorizer runtimeiam.OperationAuthorizer, installationID string) {
	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID:   "put-secret",
		Method:        http.MethodPut,
		Path:          "/api/v1/secrets/{name}",
		Summary:       "Create or rotate a retrievable secret",
		DefaultStatus: 200,
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionSecretWrite,
			Resource:       "secret",
			Action:         runtimeiam.ActionWrite,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "secret_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "secrets.secret.write",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
		SecretOperation: "write",
		OpenBaoRole:     "secrets-direct-put-secret",
		BillingSKU:      billingSKUSecretsKV,
	}), putSecret(svc, secrets.KindSecret, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "read-secret",
		Method:      http.MethodGet,
		Path:        "/api/v1/secrets/{name}",
		Summary:     "Resolve and read a retrievable secret",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionSecretRead,
			Resource:       "secret",
			Action:         runtimeiam.ActionRead,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "read",
			AuditEvent:     "secrets.secret.read",
		},
		SecretOperation: "read",
		OpenBaoRole:     "secrets-direct-read-secret",
		BillingSKU:      billingSKUSecretsKV,
	}), readSecret(svc, secrets.KindSecret, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "list-secrets",
		Method:      http.MethodGet,
		Path:        "/api/v1/secrets",
		Summary:     "List retrievable secret metadata",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionSecretList,
			Resource:       "secret",
			Action:         runtimeiam.ActionList,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "read",
			AuditEvent:     "secrets.secret.list",
		},
		SecretOperation: "list",
		OpenBaoRole:     "secrets-direct-list-secrets",
		BillingSKU:      billingSKUSecretsKV,
	}), listSecrets(svc, secrets.KindSecret, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "resolve-secrets",
		Method:      http.MethodPost,
		Path:        "/api/v1/secrets:resolve",
		Summary:     "Resolve scoped secret values",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionSecretRead,
			Resource:       "secret",
			Action:         runtimeiam.ActionRead,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "read",
			AuditEvent:     "secrets.secret.resolve",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
		SecretOperation: "read",
		OpenBaoRole:     "secrets-direct-read-secret",
		BillingSKU:      billingSKUSecretsKV,
	}), resolveSecrets(svc, secrets.KindSecret, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "delete-secret",
		Method:      http.MethodDelete,
		Path:        "/api/v1/secrets/{name}",
		Summary:     "Soft-delete a retrievable secret",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionSecretDelete,
			Resource:       "secret",
			Action:         runtimeiam.ActionDelete,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "secret_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "secrets.secret.delete",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
		SecretOperation: "delete",
		OpenBaoRole:     "secrets-direct-delete-secret",
		BillingSKU:      billingSKUSecretsKV,
	}), deleteSecret(svc, secrets.KindSecret, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID:   "put-variable",
		Method:        http.MethodPut,
		Path:          "/api/v1/variables/{name}",
		Summary:       "Create or rotate a non-secret config variable",
		DefaultStatus: 200,
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionVariableWrite,
			Resource:       "variable",
			Action:         runtimeiam.ActionWrite,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "secret_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "secrets.variable.write",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
		SecretOperation: "write",
		OpenBaoRole:     "secrets-direct-put-variable",
		BillingSKU:      billingSKUSecretsKV,
	}), putVariable(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "read-variable",
		Method:      http.MethodGet,
		Path:        "/api/v1/variables/{name}",
		Summary:     "Resolve and read a non-secret config variable",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionVariableRead,
			Resource:       "variable",
			Action:         runtimeiam.ActionRead,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "read",
			AuditEvent:     "secrets.variable.read",
		},
		SecretOperation: "read",
		OpenBaoRole:     "secrets-direct-read-variable",
		BillingSKU:      billingSKUSecretsKV,
	}), readVariable(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "list-variables",
		Method:      http.MethodGet,
		Path:        "/api/v1/variables",
		Summary:     "List non-secret config variable metadata",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionVariableList,
			Resource:       "variable",
			Action:         runtimeiam.ActionList,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "read",
			AuditEvent:     "secrets.variable.list",
		},
		SecretOperation: "list",
		OpenBaoRole:     "secrets-direct-list-variables",
		BillingSKU:      billingSKUSecretsKV,
	}), listVariables(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "delete-variable",
		Method:      http.MethodDelete,
		Path:        "/api/v1/variables/{name}",
		Summary:     "Soft-delete a non-secret config variable",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionVariableDelete,
			Resource:       "variable",
			Action:         runtimeiam.ActionDelete,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "secret_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "secrets.variable.delete",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
		SecretOperation: "delete",
		OpenBaoRole:     "secrets-direct-delete-variable",
		BillingSKU:      billingSKUSecretsKV,
	}), deleteVariable(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID:   "create-opaque-credential",
		Method:        http.MethodPost,
		Path:          "/api/v1/credentials",
		Summary:       "Create an opaque credential",
		DefaultStatus: http.StatusCreated,
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionCredentialCreate,
			Resource:       "opaque_credential",
			Action:         runtimeiam.ActionCreate,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "credential_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "secrets.credential.create",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
		SecretOperation: "credential_create",
		OpenBaoRole:     "secrets-direct-create-credential",
		BillingSKU:      billingSKUSecretsCredential,
	}), createOpaqueCredential(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "get-opaque-credential",
		Method:      http.MethodGet,
		Path:        "/api/v1/credentials/{credential_id}",
		Summary:     "Read opaque credential metadata",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionCredentialRead,
			Resource:       "opaque_credential",
			Action:         runtimeiam.ActionRead,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "read",
			AuditEvent:     "secrets.credential.read",
		},
		SecretOperation: "credential_read",
		OpenBaoRole:     "secrets-direct-read-credential",
		BillingSKU:      billingSKUSecretsCredential,
	}), getOpaqueCredential(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "list-opaque-credentials",
		Method:      http.MethodGet,
		Path:        "/api/v1/credentials",
		Summary:     "List opaque credential metadata",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionCredentialList,
			Resource:       "opaque_credential",
			Action:         runtimeiam.ActionList,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "read",
			AuditEvent:     "secrets.credential.list",
		},
		SecretOperation: "credential_list",
		OpenBaoRole:     "secrets-direct-list-credentials",
		BillingSKU:      billingSKUSecretsCredential,
	}), listOpaqueCredentials(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "roll-opaque-credential",
		Method:      http.MethodPost,
		Path:        "/api/v1/credentials/{credential_id}/roll",
		Summary:     "Roll an opaque credential",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionCredentialRoll,
			Resource:       "opaque_credential",
			Action:         runtimeiam.ActionRoll,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "credential_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "secrets.credential.roll",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
		SecretOperation: "credential_roll",
		OpenBaoRole:     "secrets-direct-roll-credential",
		BillingSKU:      billingSKUSecretsCredential,
	}), rollOpaqueCredential(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "revoke-opaque-credential",
		Method:      http.MethodPost,
		Path:        "/api/v1/credentials/{credential_id}/revoke",
		Summary:     "Revoke an opaque credential",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionCredentialRevoke,
			Resource:       "opaque_credential",
			Action:         runtimeiam.ActionRevoke,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "credential_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "secrets.credential.revoke",
			BodyLimitBytes: bodyLimitNoBody,
		},
		SecretOperation: "credential_revoke",
		OpenBaoRole:     "secrets-direct-revoke-credential",
		BillingSKU:      billingSKUSecretsCredential,
	}), revokeOpaqueCredential(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID:   "create-transit-key",
		Method:        http.MethodPost,
		Path:          "/api/v1/transit/keys",
		Summary:       "Create a transit key",
		DefaultStatus: 201,
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionTransitKeyCreate,
			Resource:       "transit_key",
			Action:         runtimeiam.ActionCreate,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "key_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "secrets.transit_key.create",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
		SecretOperation: "key_create",
		OpenBaoRole:     "secrets-direct-create-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), createTransitKey(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "rotate-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/rotate",
		Summary:     "Rotate a transit key",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionTransitKeyRotate,
			Resource:       "transit_key",
			Action:         runtimeiam.ActionRotate,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "key_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "secrets.transit_key.rotate",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
		SecretOperation: "key_rotate",
		OpenBaoRole:     "secrets-direct-rotate-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), rotateTransitKey(svc, installationID))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "encrypt-with-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/encrypt",
		Summary:     "Encrypt with a transit key",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionTransitEncrypt,
			Resource:       "transit_key",
			Action:         runtimeiam.ActionEncrypt,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "crypto",
			AuditEvent:     "secrets.transit_key.encrypt",
			BodyLimitBytes: bodyLimitCryptoJSON,
		},
		SecretOperation: "encrypt",
		OpenBaoRole:     "secrets-direct-encrypt-with-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), encryptTransit(svc))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "decrypt-with-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/decrypt",
		Summary:     "Decrypt with a transit key",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionTransitDecrypt,
			Resource:       "transit_key",
			Action:         runtimeiam.ActionDecrypt,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "crypto",
			AuditEvent:     "secrets.transit_key.decrypt",
			BodyLimitBytes: bodyLimitCryptoJSON,
		},
		SecretOperation: "decrypt",
		OpenBaoRole:     "secrets-direct-decrypt-with-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), decryptTransit(svc))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "sign-with-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/sign",
		Summary:     "Sign with a transit key",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionTransitSign,
			Resource:       "transit_key",
			Action:         runtimeiam.ActionSign,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "crypto",
			AuditEvent:     "secrets.transit_key.sign",
			BodyLimitBytes: bodyLimitCryptoJSON,
		},
		SecretOperation: "sign",
		OpenBaoRole:     "secrets-direct-sign-with-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), signTransit(svc))

	registerSecured(api, svc, authorizer, secured(huma.Operation{
		OperationID: "verify-with-transit-key",
		Method:      http.MethodPost,
		Path:        "/api/v1/transit/keys/{key_name}/verify",
		Summary:     "Verify with a transit key",
	}, secretsOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionTransitVerify,
			Resource:       "transit_key",
			Action:         runtimeiam.ActionVerify,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "crypto",
			AuditEvent:     "secrets.transit_key.verify",
			BodyLimitBytes: bodyLimitCryptoJSON,
		},
		SecretOperation: "verify",
		OpenBaoRole:     "secrets-direct-verify-with-transit-key",
		BillingSKU:      billingSKUSecretsTransit,
	}), verifyTransit(svc))
}

type (
	putSecretInput      = contractapi.SecretMutationInput
	putSecretBody       = contractapi.SecretMutationInputBody
	readSecretInput     = contractapi.SecretPathInput
	listSecretsInput    = contractapi.SecretListInput
	resolveSecretsInput = contractapi.ResolveSecretsInput
	deleteSecretInput   = contractapi.SecretDeleteInput
)

type (
	secretOutput          = contractapi.SecretOutput
	secretValueOutput     = contractapi.SecretValueOutput
	secretsOutput         = contractapi.SecretsOutput
	resolvedSecretsOutput = contractapi.ResolvedSecretsOutput
	variableOutput        = contractapi.VariableOutput
	variableValueOutput   = contractapi.VariableValueOutput
	variablesOutput       = contractapi.VariablesOutput
)

type (
	createOpaqueCredentialInput = contractapi.CreateOpaqueCredentialInput
	opaqueCredentialPathInput   = contractapi.OpaqueCredentialPathInput
	listOpaqueCredentialsInput  = contractapi.ListOpaqueCredentialsInput
	rollOpaqueCredentialInput   = contractapi.RollOpaqueCredentialInput
	revokeOpaqueCredentialInput = contractapi.RevokeOpaqueCredentialInput
)

type (
	opaqueCredentialOutput         = contractapi.OpaqueCredentialOutput
	opaqueCredentialMaterialOutput = contractapi.OpaqueCredentialMaterialOutput
	opaqueCredentialsOutput        = contractapi.OpaqueCredentialsOutput
)

type (
	SecretDTO                   = contractapi.SecretDTO
	SecretValueDTO              = contractapi.SecretValueDTO
	SecretsDTO                  = contractapi.SecretsDTO
	ResolvedSecretsDTO          = contractapi.ResolvedSecretsDTO
	VariableDTO                 = contractapi.VariableDTO
	VariableValueDTO            = contractapi.VariableValueDTO
	VariablesDTO                = contractapi.VariablesDTO
	OpaqueCredentialDTO         = contractapi.OpaqueCredentialDTO
	OpaqueCredentialMaterialDTO = contractapi.OpaqueCredentialMaterialDTO
	OpaqueCredentialsDTO        = contractapi.OpaqueCredentialsDTO
)

func stringFromContractPtr[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func contractStringPtr[T ~string](value string) *T {
	if value == "" {
		return nil
	}
	converted := T(value)
	return &converted
}

func stringSliceFromContract[T ~string](values []T) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

func stringSliceFromContractPtr[T ~[]E, E ~string](values *T) []string {
	if values == nil {
		return nil
	}
	return stringSliceFromContract([]E(*values))
}

func intFromContractPtr[T ~int](value *T) int {
	if value == nil {
		return 0
	}
	return int(*value)
}

func stringMapFromContractPtr[T ~map[string]string](value *T) map[string]string {
	if value == nil || len(*value) == 0 {
		return nil
	}
	return copyStringMap(map[string]string(*value))
}

func contractScopes(values []string) contractapi.CredentialScopes {
	if len(values) == 0 {
		return contractapi.CredentialScopes{}
	}
	out := make(contractapi.CredentialScopes, 0, len(values))
	for _, value := range values {
		out = append(out, contractapi.CredentialScope(value))
	}
	return out
}

func putSecret(svc *secrets.Service, kind string, installationID string) func(context.Context, secrets.Principal, *putSecretInput) (*secretOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *putSecretInput) (*secretOutput, error) {
		record, err := svc.PutSecret(ctx, principal, secrets.PutSecretRequest{
			Kind: kind,
			Name: string(input.Name),
			Scope: secrets.Scope{
				Level:    stringFromContractPtr(input.Body.ScopeLevel),
				SourceID: stringFromContractPtr(input.Body.SourceID),
				EnvID:    stringFromContractPtr(input.Body.EnvID),
				Branch:   stringFromContractPtr(input.Body.Branch),
			},
			Value: string(input.Body.Value),
		})
		if err != nil {
			return nil, err
		}
		return &secretOutput{Body: secretDTO(record, installationID)}, nil
	}
}

func readSecret(svc *secrets.Service, kind string, installationID string) func(context.Context, secrets.Principal, *readSecretInput) (*secretValueOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *readSecretInput) (*secretValueOutput, error) {
		value, err := svc.ReadSecret(ctx, principal, kind, string(input.Name), secrets.Scope{
			Level:    string(input.ScopeLevel),
			SourceID: string(input.SourceID),
			EnvID:    string(input.EnvID),
			Branch:   string(input.Branch),
		})
		if err != nil {
			return nil, err
		}
		return &secretValueOutput{Body: secretValueDTO(value, installationID)}, nil
	}
}

func listSecrets(svc *secrets.Service, kind string, installationID string) func(context.Context, secrets.Principal, *listSecretsInput) (*secretsOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *listSecretsInput) (*secretsOutput, error) {
		records, err := svc.ListSecrets(ctx, principal, kind, int(input.Limit))
		if err != nil {
			return nil, err
		}
		out := SecretsDTO{Secrets: make([]SecretDTO, 0, len(records))}
		for _, record := range records {
			out.Secrets = append(out.Secrets, secretDTO(record, installationID))
		}
		return &secretsOutput{Body: out}, nil
	}
}

func resolveSecrets(svc *secrets.Service, kind string, installationID string) func(context.Context, secrets.Principal, *resolveSecretsInput) (*resolvedSecretsOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *resolveSecretsInput) (*resolvedSecretsOutput, error) {
		values, err := svc.ResolveSecrets(ctx, principal, kind, secrets.Scope{
			Level:    stringFromContractPtr(input.Body.ScopeLevel),
			SourceID: stringFromContractPtr(input.Body.SourceID),
			EnvID:    stringFromContractPtr(input.Body.EnvID),
			Branch:   stringFromContractPtr(input.Body.Branch),
		}, stringSliceFromContractPtr(input.Body.Names), intFromContractPtr(input.Body.Limit))
		if err != nil {
			return nil, err
		}
		out := ResolvedSecretsDTO{Values: make([]SecretValueDTO, 0, len(values))}
		for _, value := range values {
			out.Values = append(out.Values, secretValueDTO(value, installationID))
		}
		sort.Slice(out.Values, func(i, j int) bool {
			return out.Values[i].Name < out.Values[j].Name
		})
		return &resolvedSecretsOutput{Body: out}, nil
	}
}

func deleteSecret(svc *secrets.Service, kind string, installationID string) func(context.Context, secrets.Principal, *deleteSecretInput) (*secretOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *deleteSecretInput) (*secretOutput, error) {
		record, err := svc.DeleteSecret(ctx, principal, kind, string(input.Name), secrets.Scope{
			Level:    string(input.ScopeLevel),
			SourceID: string(input.SourceID),
			EnvID:    string(input.EnvID),
			Branch:   string(input.Branch),
		})
		if err != nil {
			return nil, err
		}
		return &secretOutput{Body: secretDTO(record, installationID)}, nil
	}
}

func putVariable(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *putSecretInput) (*variableOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *putSecretInput) (*variableOutput, error) {
		record, err := svc.PutSecret(ctx, principal, secrets.PutSecretRequest{
			Kind: secrets.KindVariable,
			Name: string(input.Name),
			Scope: secrets.Scope{
				Level:    stringFromContractPtr(input.Body.ScopeLevel),
				SourceID: stringFromContractPtr(input.Body.SourceID),
				EnvID:    stringFromContractPtr(input.Body.EnvID),
				Branch:   stringFromContractPtr(input.Body.Branch),
			},
			Value: string(input.Body.Value),
		})
		if err != nil {
			return nil, err
		}
		return &variableOutput{Body: variableDTO(record, installationID)}, nil
	}
}

func readVariable(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *readSecretInput) (*variableValueOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *readSecretInput) (*variableValueOutput, error) {
		value, err := svc.ReadSecret(ctx, principal, secrets.KindVariable, string(input.Name), secrets.Scope{
			Level:    string(input.ScopeLevel),
			SourceID: string(input.SourceID),
			EnvID:    string(input.EnvID),
			Branch:   string(input.Branch),
		})
		if err != nil {
			return nil, err
		}
		return &variableValueOutput{Body: variableValueDTO(value, installationID)}, nil
	}
}

func listVariables(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *listSecretsInput) (*variablesOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *listSecretsInput) (*variablesOutput, error) {
		records, err := svc.ListSecrets(ctx, principal, secrets.KindVariable, int(input.Limit))
		if err != nil {
			return nil, err
		}
		out := VariablesDTO{Variables: make([]VariableDTO, 0, len(records))}
		for _, record := range records {
			out.Variables = append(out.Variables, variableDTO(record, installationID))
		}
		return &variablesOutput{Body: out}, nil
	}
}

func deleteVariable(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *deleteSecretInput) (*variableOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *deleteSecretInput) (*variableOutput, error) {
		record, err := svc.DeleteSecret(ctx, principal, secrets.KindVariable, string(input.Name), secrets.Scope{
			Level:    string(input.ScopeLevel),
			SourceID: string(input.SourceID),
			EnvID:    string(input.EnvID),
			Branch:   string(input.Branch),
		})
		if err != nil {
			return nil, err
		}
		return &variableOutput{Body: variableDTO(record, installationID)}, nil
	}
}

func createOpaqueCredential(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *createOpaqueCredentialInput) (*opaqueCredentialMaterialOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *createOpaqueCredentialInput) (*opaqueCredentialMaterialOutput, error) {
		expiresAt := time.Time{}
		if input.Body.ExpiresInSeconds != nil && *input.Body.ExpiresInSeconds > 0 {
			expiresAt = time.Now().UTC().Add(time.Duration(*input.Body.ExpiresInSeconds) * time.Second)
		}
		material, err := svc.CreateOpaqueCredential(ctx, principal, secrets.CreateOpaqueCredentialRequest{
			Kind:        string(input.Body.Kind),
			Subject:     stringFromContractPtr(input.Body.Subject),
			DisplayName: stringFromContractPtr(input.Body.DisplayName),
			Scopes:      stringSliceFromContract(input.Body.Scopes),
			Metadata:    stringMapFromContractPtr(input.Body.Metadata),
			ExpiresAt:   expiresAt,
		})
		if err != nil {
			return nil, err
		}
		return &opaqueCredentialMaterialOutput{Body: opaqueCredentialMaterialDTO(material, installationID)}, nil
	}
}

func getOpaqueCredential(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *opaqueCredentialPathInput) (*opaqueCredentialOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *opaqueCredentialPathInput) (*opaqueCredentialOutput, error) {
		credential, err := svc.GetOpaqueCredential(ctx, principal, string(input.CredentialID))
		if err != nil {
			return nil, err
		}
		return &opaqueCredentialOutput{Body: opaqueCredentialDTO(credential, installationID)}, nil
	}
}

func listOpaqueCredentials(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *listOpaqueCredentialsInput) (*opaqueCredentialsOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *listOpaqueCredentialsInput) (*opaqueCredentialsOutput, error) {
		credentials, err := svc.ListOpaqueCredentials(ctx, principal, string(input.Kind), int(input.Limit))
		if err != nil {
			return nil, err
		}
		out := OpaqueCredentialsDTO{Credentials: make([]OpaqueCredentialDTO, 0, len(credentials))}
		for _, credential := range credentials {
			out.Credentials = append(out.Credentials, opaqueCredentialDTO(credential, installationID))
		}
		return &opaqueCredentialsOutput{Body: out}, nil
	}
}

func rollOpaqueCredential(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *rollOpaqueCredentialInput) (*opaqueCredentialMaterialOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *rollOpaqueCredentialInput) (*opaqueCredentialMaterialOutput, error) {
		expiresAt := time.Time{}
		if input.Body.ExpiresInSeconds != nil && *input.Body.ExpiresInSeconds > 0 {
			expiresAt = time.Now().UTC().Add(time.Duration(*input.Body.ExpiresInSeconds) * time.Second)
		}
		material, err := svc.RollOpaqueCredential(ctx, principal, secrets.RollOpaqueCredentialRequest{
			CredentialID: string(input.CredentialID),
			ExpiresAt:    expiresAt,
		})
		if err != nil {
			return nil, err
		}
		return &opaqueCredentialMaterialOutput{Body: opaqueCredentialMaterialDTO(material, installationID)}, nil
	}
}

func revokeOpaqueCredential(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *revokeOpaqueCredentialInput) (*opaqueCredentialOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *revokeOpaqueCredentialInput) (*opaqueCredentialOutput, error) {
		credential, err := svc.RevokeOpaqueCredential(ctx, principal, string(input.CredentialID))
		if err != nil {
			return nil, err
		}
		return &opaqueCredentialOutput{Body: opaqueCredentialDTO(credential, installationID)}, nil
	}
}

type (
	createTransitKeyInput = contractapi.CreateTransitKeyInput
	transitKeyOutput      = contractapi.TransitKeyOutput
	TransitKeyDTO         = contractapi.TransitKeyDTO
)

func createTransitKey(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *createTransitKeyInput) (*transitKeyOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *createTransitKeyInput) (*transitKeyOutput, error) {
		key, err := svc.CreateTransitKey(ctx, principal, string(input.Body.Name))
		if err != nil {
			return nil, err
		}
		return &transitKeyOutput{Body: transitKeyDTO(key, installationID)}, nil
	}
}

type rotateTransitKeyInput = contractapi.TransitKeyIdempotentInput

func rotateTransitKey(svc *secrets.Service, installationID string) func(context.Context, secrets.Principal, *rotateTransitKeyInput) (*transitKeyOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *rotateTransitKeyInput) (*transitKeyOutput, error) {
		key, err := svc.RotateTransitKey(ctx, principal, string(input.KeyName))
		if err != nil {
			return nil, err
		}
		return &transitKeyOutput{Body: transitKeyDTO(key, installationID)}, nil
	}
}

type (
	encryptInput  = contractapi.TransitEncryptInput
	encryptOutput = contractapi.TransitCiphertextOutput
	decryptInput  = contractapi.TransitDecryptInput
	decryptOutput = contractapi.TransitPlaintextOutput
	signInput     = contractapi.TransitSignInput
	signOutput    = contractapi.TransitSignatureOutput
	verifyInput   = contractapi.TransitVerifyInput
	verifyOutput  = contractapi.TransitVerifyOutput
)

func encryptTransit(svc *secrets.Service) func(context.Context, secrets.Principal, *encryptInput) (*encryptOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *encryptInput) (*encryptOutput, error) {
		plaintext, err := base64.StdEncoding.DecodeString(string(input.Body.PlaintextBase64))
		if err != nil {
			return nil, secrets.ErrInvalidArgument
		}
		ciphertext, err := svc.TransitEncrypt(ctx, principal, string(input.KeyName), plaintext)
		if err != nil {
			return nil, err
		}
		out := &encryptOutput{}
		out.Body.Ciphertext = contractapi.Ciphertext(ciphertext.Ciphertext)
		out.Body.Version = contractapi.SecretVersion(strconv.FormatUint(ciphertext.Version, 10))
		return out, nil
	}
}

func decryptTransit(svc *secrets.Service) func(context.Context, secrets.Principal, *decryptInput) (*decryptOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *decryptInput) (*decryptOutput, error) {
		plaintext, _, err := svc.TransitDecrypt(ctx, principal, string(input.KeyName), string(input.Body.Ciphertext))
		if err != nil {
			return nil, err
		}
		out := &decryptOutput{}
		out.Body.PlaintextBase64 = contractapi.PlaintextBase64(base64.StdEncoding.EncodeToString(plaintext))
		return out, nil
	}
}

func signTransit(svc *secrets.Service) func(context.Context, secrets.Principal, *signInput) (*signOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *signInput) (*signOutput, error) {
		message, err := base64.StdEncoding.DecodeString(string(input.Body.MessageBase64))
		if err != nil {
			return nil, secrets.ErrInvalidArgument
		}
		signature, _, err := svc.TransitSign(ctx, principal, string(input.KeyName), message)
		if err != nil {
			return nil, err
		}
		out := &signOutput{}
		out.Body.Signature = contractapi.Signature(signature)
		return out, nil
	}
}

func verifyTransit(svc *secrets.Service) func(context.Context, secrets.Principal, *verifyInput) (*verifyOutput, error) {
	return func(ctx context.Context, principal secrets.Principal, input *verifyInput) (*verifyOutput, error) {
		message, err := base64.StdEncoding.DecodeString(string(input.Body.MessageBase64))
		if err != nil {
			return nil, secrets.ErrInvalidArgument
		}
		valid, _, err := svc.TransitVerify(ctx, principal, string(input.KeyName), message, string(input.Body.Signature))
		if err != nil {
			return nil, err
		}
		out := &verifyOutput{}
		out.Body.Valid = valid
		return out, nil
	}
}

func secretDTO(record secrets.SecretRecord, installationID string) SecretDTO {
	return SecretDTO{
		SecretID:       record.SecretID,
		ResourceName:   secretResourceName(record, installationID),
		Kind:           record.Kind,
		Name:           contractapi.SecretName(record.Name),
		ScopeLevel:     contractapi.ScopeLevel(record.Scope.Level),
		SourceID:       contractStringPtr[contractapi.SourceID](record.Scope.SourceID),
		EnvID:          contractStringPtr[contractapi.EnvID](record.Scope.EnvID),
		Branch:         contractStringPtr[contractapi.Branch](record.Scope.Branch),
		CurrentVersion: contractapi.SecretVersion(strconv.FormatUint(record.CurrentVersion, 10)),
		CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func secretValueDTO(value secrets.SecretValue, installationID string) SecretValueDTO {
	record := value.Record
	return SecretValueDTO{
		SecretID:       record.SecretID,
		ResourceName:   secretResourceName(record, installationID),
		Kind:           record.Kind,
		Name:           contractapi.SecretName(record.Name),
		ScopeLevel:     contractapi.ScopeLevel(record.Scope.Level),
		SourceID:       contractStringPtr[contractapi.SourceID](record.Scope.SourceID),
		EnvID:          contractStringPtr[contractapi.EnvID](record.Scope.EnvID),
		Branch:         contractStringPtr[contractapi.Branch](record.Scope.Branch),
		CurrentVersion: contractapi.SecretVersion(strconv.FormatUint(record.CurrentVersion, 10)),
		CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Value:          contractapi.SecretValue(value.Value),
	}
}

func variableDTO(record secrets.SecretRecord, installationID string) VariableDTO {
	return VariableDTO{
		VariableID:     record.SecretID,
		ResourceName:   optionalResourceName(installationID, record.OrgID, record.SecretID, "variables"),
		Kind:           record.Kind,
		Name:           contractapi.SecretName(record.Name),
		ScopeLevel:     contractapi.ScopeLevel(record.Scope.Level),
		SourceID:       contractStringPtr[contractapi.SourceID](record.Scope.SourceID),
		EnvID:          contractStringPtr[contractapi.EnvID](record.Scope.EnvID),
		Branch:         contractStringPtr[contractapi.Branch](record.Scope.Branch),
		CurrentVersion: contractapi.SecretVersion(strconv.FormatUint(record.CurrentVersion, 10)),
		CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func variableValueDTO(value secrets.SecretValue, installationID string) VariableValueDTO {
	record := value.Record
	return VariableValueDTO{
		VariableID:     record.SecretID,
		ResourceName:   optionalResourceName(installationID, record.OrgID, record.SecretID, "variables"),
		Kind:           record.Kind,
		Name:           contractapi.SecretName(record.Name),
		ScopeLevel:     contractapi.ScopeLevel(record.Scope.Level),
		SourceID:       contractStringPtr[contractapi.SourceID](record.Scope.SourceID),
		EnvID:          contractStringPtr[contractapi.EnvID](record.Scope.EnvID),
		Branch:         contractStringPtr[contractapi.Branch](record.Scope.Branch),
		CurrentVersion: contractapi.SecretVersion(strconv.FormatUint(record.CurrentVersion, 10)),
		CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      record.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Value:          contractapi.SecretValue(value.Value),
	}
}

func transitKeyDTO(key secrets.TransitKey, installationID string) TransitKeyDTO {
	return TransitKeyDTO{
		KeyID:          key.KeyID,
		ResourceName:   optionalResourceName(installationID, key.OrgID, key.KeyID, "transitKeys"),
		Name:           contractapi.KeyName(key.Name),
		CurrentVersion: contractapi.SecretVersion(strconv.FormatUint(key.CurrentVersion, 10)),
		PublicKey:      key.PublicKey,
		CreatedAt:      key.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      key.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func opaqueCredentialMaterialDTO(material secrets.OpaqueCredentialMaterial, installationID string) OpaqueCredentialMaterialDTO {
	return OpaqueCredentialMaterialDTO{
		Credential: opaqueCredentialDTO(material.Credential, installationID),
		Token:      contractapi.SecretValue(material.Token),
	}
}

func opaqueCredentialDTO(credential secrets.OpaqueCredential, installationID string) OpaqueCredentialDTO {
	return OpaqueCredentialDTO{
		CredentialID:   contractapi.CredentialID(credential.CredentialID),
		ResourceName:   optionalResourceName(installationID, credential.OrgID, credential.CredentialID, "credentials"),
		OrgID:          contractapi.OrgID(credential.OrgID),
		Kind:           contractapi.CredentialKind(credential.Kind),
		Subject:        credential.Subject,
		DisplayName:    credential.DisplayName,
		Status:         contractapi.CredentialStatus(credential.Status),
		TokenPrefix:    credential.TokenPrefix,
		Scopes:         contractScopes(credential.Scopes),
		Metadata:       contractapi.CredentialMetadata(copyStringMap(credential.Metadata)),
		CurrentVersion: contractapi.SecretVersion(strconv.FormatUint(credential.CurrentVersion, 10)),
		ExpiresAt:      credential.ExpiresAt.UTC().Format(time.RFC3339Nano),
		LastUsedAt:     formatOptionalDTOTime(credential.LastUsedAt),
		CreatedAt:      credential.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      credential.UpdatedAt.UTC().Format(time.RFC3339Nano),
		RevokedAt:      formatOptionalDTOTime(credential.RevokedAt),
	}
}

func secretResourceName(record secrets.SecretRecord, installationID string) *contractapi.ResourceName {
	switch record.Kind {
	case secrets.KindVariable:
		return optionalResourceName(installationID, record.OrgID, record.SecretID, "variables")
	default:
		return optionalResourceName(installationID, record.OrgID, record.SecretID, "secrets")
	}
}

func optionalResourceName(installationID, orgID, id string, collection string) *contractapi.ResourceName {
	if installationID == "" || orgID == "" || id == "" {
		return nil
	}
	value := contractapi.ResourceName("urn:verself:" + installationID + ":orgs/" + orgID + "/" + collection + "/" + id)
	return &value
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func formatOptionalDTOTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}
