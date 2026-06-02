package r2control

import (
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	values, err := ParseEnvFile([]byte(`
CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID='abc'
CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY="def"
`))
	if err != nil {
		t.Fatal(err)
	}
	if values["CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID"] != "abc" {
		t.Fatalf("access key = %q", values["CLOUDFLARE_R2_ADMIN_ACCESS_KEY_ID"])
	}
	if values["CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY"] != "def" {
		t.Fatalf("secret key = %q", values["CLOUDFLARE_R2_ADMIN_SECRET_ACCESS_KEY"])
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
