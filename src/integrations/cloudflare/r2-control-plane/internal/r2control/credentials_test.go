package r2control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadParentCredentialsFromFiles(t *testing.T) {
	dir := t.TempDir()
	accessPath := filepath.Join(dir, "access-key-id")
	secretPath := filepath.Join(dir, "secret-access-key")
	if err := os.WriteFile(accessPath, []byte("abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("def\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadParentCredentials(context.Background(), ParentCredentialConfig{
		Source:              ParentCredentialSourceFiles,
		AccessKeyIDFile:     accessPath,
		SecretAccessKeyFile: secretPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "abc" {
		t.Fatalf("access key = %q", creds.AccessKeyID)
	}
	if creds.SecretAccessKey != "def" {
		t.Fatalf("secret key = %q", creds.SecretAccessKey)
	}
}

func TestParentCredentialsRequireS3Secret(t *testing.T) {
	creds, err := parentCredentialsFromValues(map[string]string{
		"token_id":          "token-id",
		"secret_access_key": "secret-value",
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessKeyID != "token-id" {
		t.Fatalf("access key = %q", creds.AccessKeyID)
	}
	if creds.SecretAccessKey != "secret-value" {
		t.Fatalf("secret access key = %q", creds.SecretAccessKey)
	}
	_, err = parentCredentialsFromValues(map[string]string{"token_id": "token-id"}, "test")
	if err == nil || !strings.Contains(err.Error(), "secret access key is required") {
		t.Fatalf("error = %v, want missing secret refusal", err)
	}
}
