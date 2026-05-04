package identity

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	identitystore "github.com/verself/iam-service/internal/store"
)

func TestAPICredentialFromFieldsAcceptsNullRevokedBy(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	credential, err := apiCredentialFromFields(
		"credential-1",
		"org-1",
		"subject-1",
		"client-1",
		"CI bot",
		string(APICredentialStatusActive),
		string(APICredentialAuthMethodClientSecret),
		"sha256:test",
		7,
		timestamptz(now),
		"owner-1",
		timestamptz(now),
		pgtype.Timestamptz{},
		pgtype.Timestamptz{},
		"",
		pgtype.Timestamptz{},
	)
	if err != nil {
		t.Fatalf("convert credential: %v", err)
	}
	if credential.RevokedBy != "" || credential.RevokedAt != nil {
		t.Fatalf("unexpected revocation metadata: %#v", credential)
	}
	if credential.CredentialID != "credential-1" || credential.PolicyVersionAtIssue != 7 {
		t.Fatalf("unexpected credential: %#v", credential)
	}
}

func TestAPICredentialSecretFromRowAcceptsNullRevokedBy(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	secret, err := apiCredentialSecretFromRow(identitystore.ListActiveAPICredentialSecretsRow{
		SecretID:      "secret-1",
		CredentialID:  "credential-1",
		AuthMethod:    string(APICredentialAuthMethodClientSecret),
		ProviderKeyID: "key-1",
		Fingerprint:   "sha256:test",
		SecretHash:    []byte("hash"),
		HashAlgorithm: "argon2id",
		CreatedAt:     timestamptz(now),
		CreatedBy:     "owner-1",
		ExpiresAt:     pgtype.Timestamptz{},
		RevokedAt:     pgtype.Timestamptz{},
		RevokedBy:     "",
	})
	if err != nil {
		t.Fatalf("convert credential secret: %v", err)
	}
	if secret.RevokedBy != "" || secret.RevokedAt != nil {
		t.Fatalf("unexpected revocation metadata: %#v", secret)
	}
	if secret.SecretID != "secret-1" || secret.AuthMethod != APICredentialAuthMethodClientSecret {
		t.Fatalf("unexpected credential secret: %#v", secret)
	}
}

func TestNullableStringParamTreatsBlankValuesAsNull(t *testing.T) {
	if value := nullableStringParam(""); value != nil {
		t.Fatalf("expected empty string to become nil, got %#v", value)
	}
	if value := nullableStringParam("   "); value != nil {
		t.Fatalf("expected blank string to become nil, got %#v", value)
	}
	if value := nullableStringParam("owner-1"); value != "owner-1" {
		t.Fatalf("expected non-empty string to be preserved, got %#v", value)
	}
}
