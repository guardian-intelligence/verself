package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadIAMRuntimeConfig(t *testing.T) {
	path := writeIAMGuardianDocument(t, validIAMDocument())

	cfg, err := loadIAMRuntimeConfig(path, "iam-service")
	if err != nil {
		t.Fatalf("loadIAMRuntimeConfig: %v", err)
	}
	if cfg.InstallationID != "inst_gamma_01JZ0000000000000000000000" {
		t.Fatalf("InstallationID = %q", cfg.InstallationID)
	}
	if cfg.PostgresDSN != "postgres://iam_service@/iam_service?host=/var/run/postgresql&sslmode=disable" {
		t.Fatalf("PostgresDSN = %q", cfg.PostgresDSN)
	}
	if cfg.AuthAudienceName != "iam-service.zitadel.auth_audience" {
		t.Fatalf("AuthAudienceName = %q", cfg.AuthAudienceName)
	}
	if cfg.EmailIdentityHMACKeyName != "iam-service.email_identity.hmac_key" {
		t.Fatalf("EmailIdentityHMACKeyName = %q", cfg.EmailIdentityHMACKeyName)
	}
}

func TestLoadIAMRuntimeConfigRequiresCredentialRefs(t *testing.T) {
	doc := strings.ReplaceAll(validIAMDocument(), `"name":"iam-service.email_identity.hmac_key"`, `"name":""`)
	path := writeIAMGuardianDocument(t, doc)

	_, err := loadIAMRuntimeConfig(path, "iam-service")
	if err == nil {
		t.Fatal("loadIAMRuntimeConfig succeeded with an empty credential ref")
	}
	if !strings.Contains(err.Error(), "credentials.emailIdentityHMACKey") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadIAMRuntimeConfigAllowsMissingGithubLoginIDP(t *testing.T) {
	doc := strings.ReplaceAll(validIAMDocument(), `          "githubLoginIDPRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.zitadel.github_login_idp_id"},
`, "")
	path := writeIAMGuardianDocument(t, doc)

	cfg, err := loadIAMRuntimeConfig(path, "iam-service")
	if err != nil {
		t.Fatalf("loadIAMRuntimeConfig: %v", err)
	}
	if cfg.GithubLoginIDPName != "" {
		t.Fatalf("GithubLoginIDPName = %q", cfg.GithubLoginIDPName)
	}
}

func TestParseIAMMigrationOptions(t *testing.T) {
	opts, remaining, err := parseIAMMigrationOptions([]string{"--resource-graph=/tmp/document.json", "--resource-name=custom", "up"})
	if err != nil {
		t.Fatalf("parseIAMMigrationOptions: %v", err)
	}
	if opts.ResourceGraph != "/tmp/document.json" || opts.ResourceName != "custom" {
		t.Fatalf("opts = %#v", opts)
	}
	if len(remaining) != 1 || remaining[0] != "up" {
		t.Fatalf("remaining = %#v", remaining)
	}
}

func TestParseIAMRecoverOptions(t *testing.T) {
	opts, err := parseIAMRecoverOptions([]string{
		"--repo-root=/opt/guardian/repo/current",
		"--resource-graph=/opt/guardian/repo/current/workspace/.guardian/fly/document.json",
		"--resource-name=custom",
		"--runtime-root=/var/lib/iam-service/runtime",
		"--projected-graph=/run/verself/recovery/iam-service/document.json",
		"--migrate",
	})
	if err != nil {
		t.Fatalf("parseIAMRecoverOptions: %v", err)
	}
	if opts.RepoRoot != "/opt/guardian/repo/current" || opts.ResourceName != "custom" || !opts.Migrate {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseIAMRecoverOptionsRequiresAbsolutePaths(t *testing.T) {
	_, err := parseIAMRecoverOptions([]string{"--repo-root=relative"})
	if err == nil {
		t.Fatal("parseIAMRecoverOptions accepted relative repo root")
	}
	if !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("error = %v", err)
	}
}

func writeIAMGuardianDocument(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "document.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}
	return path
}

func validIAMDocument() string {
	return `{
  "entrypoint": {"apiVersion":"guardian.guardianintelligence.org/v1alpha1","kind":"FlyProcedure","name":"gamma"},
  "resources": [
    {
      "apiVersion":"iam.guardianintelligence.org/v1alpha1",
      "kind":"IAMService",
      "metadata":{"name":"iam-service"},
      "spec":{
        "installationID":"inst_gamma_01JZ0000000000000000000000",
        "auth":{
          "issuerURL":"https://gamma.verself.sh",
          "browserAuthPublicBaseURL":"https://gamma.verself.sh",
          "pwnedPasswordsRangeEndpoint":"https://api.pwnedpasswords.com"
        },
        "zitadel":{"host":"gamma.verself.sh"},
        "postgres":{"dsn":"postgres://iam_service@/iam_service?host=/var/run/postgresql&sslmode=disable","maxConns":8},
        "clickhouse":{"address":"127.0.0.1:9440","user":"iam_service","caCertPath":"/etc/verself/clickhouse/server-ca.pem"},
        "spiffe":{"endpointSocket":"unix:///run/spire-agent/sockets/agent.sock"},
        "credentials":{
          "zitadelActionSigningKeyRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.zitadel.action_signing_key"},
          "browserOIDCClientIDRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.zitadel.oidc_client_id"},
          "browserOIDCClientSecretRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.zitadel.oidc_client_secret"},
          "githubLoginIDPRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.zitadel.github_login_idp_id"},
          "authAudienceRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.zitadel.auth_audience"},
          "zitadelAdminTokenRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.zitadel.admin_token"},
          "spiceDBPresharedKeyRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.spicedb.grpc_preshared_key"},
          "emailIdentityHMACKeyRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.email_identity.hmac_key"}
        }
      }
    }
  ]
}`
}
