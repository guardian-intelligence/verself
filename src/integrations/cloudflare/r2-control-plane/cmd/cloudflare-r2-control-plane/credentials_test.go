package main

import "testing"

func TestParentCredentialsDeriveS3SecretFromAPIToken(t *testing.T) {
	creds, err := parentCredentialsFromValues(map[string]string{
		"access_key_id": "token-id",
		"api_token":     "token-value",
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if creds.SecretAccessKey != sha256Hex([]byte("token-value")) {
		t.Fatalf("secret access key = %q", creds.SecretAccessKey)
	}
}

func TestParseEnvFile(t *testing.T) {
	values, err := parseEnvFile([]byte(`
# comment
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
