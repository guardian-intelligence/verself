package identity

import (
	"database/sql"
	"testing"
	"time"
)

type scannerFunc func(dest ...any) error

func (f scannerFunc) Scan(dest ...any) error {
	return f(dest...)
}

func TestScanAPICredentialAcceptsNullRevokedBy(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	credential, err := scanAPICredential(scannerFunc(func(dest ...any) error {
		if len(dest) != 16 {
			t.Fatalf("unexpected scan destination count: %d", len(dest))
		}
		*dest[0].(*string) = "credential-1"
		*dest[1].(*string) = "org-1"
		*dest[2].(*string) = "subject-1"
		*dest[3].(*string) = "client-1"
		*dest[4].(*string) = "CI bot"
		*dest[5].(*string) = string(APICredentialStatusActive)
		*dest[6].(*string) = string(APICredentialAuthMethodClientSecret)
		*dest[7].(*string) = "sha256:test"
		*dest[8].(*int32) = 7
		*dest[9].(*time.Time) = now
		*dest[10].(*string) = "owner-1"
		*dest[11].(*time.Time) = now
		*dest[12].(*sql.NullTime) = sql.NullTime{}
		*dest[13].(*sql.NullTime) = sql.NullTime{}
		*dest[14].(*sql.NullString) = sql.NullString{}
		*dest[15].(*sql.NullTime) = sql.NullTime{}
		return nil
	}))
	if err != nil {
		t.Fatalf("scan credential: %v", err)
	}
	if credential.RevokedBy != "" || credential.RevokedAt != nil {
		t.Fatalf("unexpected revocation metadata: %#v", credential)
	}
	if credential.CredentialID != "credential-1" || credential.PolicyVersionAtIssue != 7 {
		t.Fatalf("unexpected credential: %#v", credential)
	}
}

func TestScanAPICredentialSecretAcceptsNullRevokedBy(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	secret, err := scanAPICredentialSecret(scannerFunc(func(dest ...any) error {
		if len(dest) != 12 {
			t.Fatalf("unexpected scan destination count: %d", len(dest))
		}
		*dest[0].(*string) = "secret-1"
		*dest[1].(*string) = "credential-1"
		*dest[2].(*string) = string(APICredentialAuthMethodClientSecret)
		*dest[3].(*string) = "key-1"
		*dest[4].(*string) = "sha256:test"
		*dest[5].(*[]byte) = []byte("hash")
		*dest[6].(*string) = "argon2id"
		*dest[7].(*time.Time) = now
		*dest[8].(*string) = "owner-1"
		*dest[9].(*sql.NullTime) = sql.NullTime{}
		*dest[10].(*sql.NullTime) = sql.NullTime{}
		*dest[11].(*sql.NullString) = sql.NullString{}
		return nil
	}))
	if err != nil {
		t.Fatalf("scan credential secret: %v", err)
	}
	if secret.RevokedBy != "" || secret.RevokedAt != nil {
		t.Fatalf("unexpected revocation metadata: %#v", secret)
	}
	if secret.SecretID != "secret-1" || secret.AuthMethod != APICredentialAuthMethodClientSecret {
		t.Fatalf("unexpected credential secret: %#v", secret)
	}
}
