package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDistributionRuntimeConfigReadsStaticAudience(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "document.json")
	if err := os.WriteFile(path, []byte(`{
		"entrypoint":{"apiVersion":"guardian.guardianintelligence.org/v1alpha1","kind":"FlyProcedure","name":"gamma"},
		"resources":[{
			"apiVersion":"distribution.guardianintelligence.org/v1alpha1",
			"kind":"DistributionService",
			"metadata":{"name":"distribution-service"},
			"spec":{
				"installationID":"inst_test",
				"auth":{"issuerURL":"https://gamma.verself.sh","audience":"aud_test"},
				"postgres":{"dsn":"postgres://distribution_service@/distribution_service?host=/var/run/postgresql&sslmode=disable","maxConns":8},
				"clickhouse":{"address":"127.0.0.1:9440","user":"distribution_service","caCertPath":"/etc/verself/clickhouse/server-ca.pem"},
				"attestation":{"trustedBuilders":["spiffe://gamma.verself.sh/svc/release-builder"],"trustedSigners":["https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main"]},
				"spiffe":{"endpointSocket":"unix:///run/spire-agent/sockets/agent.sock"}
			}
		}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadDistributionRuntimeConfig(path, "distribution-service")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthAudience != "aud_test" {
		t.Fatalf("AuthAudience = %q, want aud_test", cfg.AuthAudience)
	}
}

func TestLoadDistributionRuntimeConfigRejectsLegacyAudienceRef(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "document.json")
	if err := os.WriteFile(path, []byte(`{
		"entrypoint":{"apiVersion":"guardian.guardianintelligence.org/v1alpha1","kind":"FlyProcedure","name":"gamma"},
		"resources":[{
			"apiVersion":"distribution.guardianintelligence.org/v1alpha1",
			"kind":"DistributionService",
			"metadata":{"name":"distribution-service"},
			"spec":{
				"installationID":"inst_test",
				"auth":{"issuerURL":"https://gamma.verself.sh","audienceRef":{"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.zitadel.auth_audience"}},
				"postgres":{"dsn":"postgres://distribution_service@/distribution_service?host=/var/run/postgresql&sslmode=disable","maxConns":8},
				"clickhouse":{"address":"127.0.0.1:9440","user":"distribution_service","caCertPath":"/etc/verself/clickhouse/server-ca.pem"},
				"attestation":{"trustedBuilders":["spiffe://gamma.verself.sh/svc/release-builder"],"trustedSigners":["https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main"]},
				"spiffe":{"endpointSocket":"unix:///run/spire-agent/sockets/agent.sock"}
			}
		}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadDistributionRuntimeConfig(path, "distribution-service")
	if err == nil {
		t.Fatal("loadDistributionRuntimeConfig accepted legacy auth.audienceRef")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want unknown field", err)
	}
}
