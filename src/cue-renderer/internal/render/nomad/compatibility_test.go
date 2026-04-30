package nomad

import (
	"strings"
	"testing"
)

func TestCheckUnitCompatibilityLoadCredentialsRecipes(t *testing.T) {
	unit := map[string]any{
		"name":  "sandbox-rental-service",
		"group": "sandbox_rental",
		"load_credentials": []any{
			map[string]any{
				"name": "forgejo-webhook-secret",
				"path": "/etc/credstore/sandbox-rental/forgejo-webhook-secret",
			},
			map[string]any{
				"name": "clickhouse-ca-cert",
				"path": "/etc/clickhouse-server/tls/server-ca.pem",
			},
			map[string]any{
				"name": "forgejo-token",
				"path": "/etc/credstore/forgejo/automation-token",
			},
		},
	}

	err := checkUnitCompatibility("sandbox_rental", unit)
	if err == nil {
		t.Fatal("expected load_credentials compatibility failure")
	}
	msg := err.Error()
	for _, want := range []string{
		`environment += {VERSELF_CRED_FORGEJO_WEBHOOK_SECRET: "/etc/credstore/sandbox-rental/forgejo-webhook-secret"}`,
		`secret_refs += {name: "clickhouse-ca-cert", path: "/etc/credstore/sandbox-rental/clickhouse-ca-cert", owner: "root", group: "sandbox_rental", mode: "0640", source: {kind: "remote_src", remote_src: "/etc/clickhouse-server/tls/server-ca.pem"}}`,
		`environment += {VERSELF_CRED_CLICKHOUSE_CA_CERT: "/etc/credstore/sandbox-rental/clickhouse-ca-cert"}`,
		`secret_refs += {name: "forgejo-token", path: "/etc/credstore/sandbox-rental/forgejo-token", owner: "root", group: "sandbox_rental", mode: "0640", source: {kind: "remote_src", remote_src: "/etc/credstore/forgejo/automation-token"}}`,
		`environment += {VERSELF_CRED_FORGEJO_TOKEN: "/etc/credstore/sandbox-rental/forgejo-token"}`,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("compatibility error missing %q:\n%s", want, msg)
		}
	}
}

func TestCredentialPathEnvNameSanitizesCredentialName(t *testing.T) {
	got := credentialPathEnvName("tenant.jwt-key/v1")
	want := "VERSELF_CRED_TENANT_JWT_KEY_V1"
	if got != want {
		t.Fatalf("credentialPathEnvName: got %q want %q", got, want)
	}
}
