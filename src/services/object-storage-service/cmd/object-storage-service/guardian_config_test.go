package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadObjectStorageRuntimeConfig(t *testing.T) {
	path := writeGuardianDocument(t, validObjectStorageDocument())

	cfg, err := loadObjectStorageRuntimeConfig(path, "object-storage")
	if err != nil {
		t.Fatalf("loadObjectStorageRuntimeConfig: %v", err)
	}
	if cfg.Site != "gamma" {
		t.Fatalf("Site = %q, want gamma", cfg.Site)
	}
	if cfg.R2Endpoint != "https://example.r2.cloudflarestorage.com" {
		t.Fatalf("R2Endpoint = %q", cfg.R2Endpoint)
	}
	if cfg.CredentialKEKName != "object-storage-service.credential_kek" {
		t.Fatalf("CredentialKEKName = %q", cfg.CredentialKEKName)
	}
	if cfg.DeploymentArtifactsBucket != "verself-deployment-artifacts" {
		t.Fatalf("DeploymentArtifactsBucket = %q", cfg.DeploymentArtifactsBucket)
	}
	if cfg.AuthAudienceCredentialName != "iam-service.zitadel.auth_audience" {
		t.Fatalf("AuthAudienceCredentialName = %q", cfg.AuthAudienceCredentialName)
	}
}

func TestLoadObjectStorageRuntimeConfigRequiresDeploymentArtifactsBucket(t *testing.T) {
	doc := validObjectStorageDocument()
	doc = strings.ReplaceAll(doc, `"purpose":"deploymentArtifacts"`, `"purpose":"recovery"`)
	path := writeGuardianDocument(t, doc)

	_, err := loadObjectStorageRuntimeConfig(path, "object-storage")
	if err == nil {
		t.Fatalf("loadObjectStorageRuntimeConfig succeeded without a deploymentArtifacts bucket")
	}
}

func TestParseRecoverOptionsDefaultsResourceGraphFromRepoRoot(t *testing.T) {
	opts, err := parseRecoverOptions([]string{"--repo-root=/tmp/repo"})
	if err != nil {
		t.Fatalf("parseRecoverOptions: %v", err)
	}
	want := "/tmp/repo/workspace/.guardian/fly/document.json"
	if opts.ResourceGraph != want {
		t.Fatalf("ResourceGraph = %q, want %q", opts.ResourceGraph, want)
	}
	if !opts.Migrate {
		t.Fatalf("Migrate = false, want true")
	}
}

func TestParseRecoverOptionsRejectsAbsoluteRuntimeTar(t *testing.T) {
	_, err := parseRecoverOptions([]string{"--runtime-tar=/tmp/object-storage-service.tar"})
	if err == nil {
		t.Fatalf("parseRecoverOptions accepted absolute runtime tar")
	}
}

func writeGuardianDocument(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "document.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}
	return path
}

func validObjectStorageDocument() string {
	return `{
  "entrypoint": {"apiVersion":"guardian.guardianintelligence.org/v1alpha1","kind":"FlyProcedure","name":"gamma"},
  "resources": [
    {
      "apiVersion": "objectstorage.guardianintelligence.org/v1alpha1",
      "kind": "ObjectStorageService",
      "metadata": {"name": "object-storage"},
      "spec": {
        "site": "gamma",
        "credentials": {
          "credentialKEKRef": {"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"object-storage-service.credential_kek"}
        },
        "provider": {
          "cloudflareR2": {
            "endpoint": "https://example.r2.cloudflarestorage.com",
            "region": "auto",
            "authorityRef": {"apiVersion":"cloudflare.guardianintelligence.org/v1alpha1","kind":"AccountAuthority","name":"cloudflare-account-admin"},
            "credentials": {
              "adminAccessKeyIDRef": {"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"object-storage-service.r2.admin_access_key_id"},
              "adminSecretAccessKeyRef": {"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"object-storage-service.r2.admin_secret_access_key"},
              "proxyAccessKeyIDRef": {"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"object-storage-service.r2.proxy_access_key_id"},
              "proxySecretAccessKeyRef": {"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"object-storage-service.r2.proxy_secret_access_key"}
            }
          }
        },
        "buckets": [
          {"name":"deployment-artifacts","providerName":"verself-deployment-artifacts","purpose":"deploymentArtifacts"}
        ],
        "auth": {
          "issuerURL": "https://gamma.verself.sh",
          "audienceRef": {"apiVersion":"openbao.guardianintelligence.org/v1alpha1","kind":"SecretPath","name":"iam-service.zitadel.auth_audience"}
        },
        "postgres": {"dsn":"postgres://object_storage_service@/object_storage_service?host=/var/run/postgresql&sslmode=disable"},
        "clickhouse": {"address":"127.0.0.1:9440","user":"object_storage_service","caCertPath":"/etc/verself/clickhouse/server-ca.pem"},
        "spiffe": {"endpointSocket":"unix:///run/spire-agent/sockets/agent.sock"}
      }
    }
  ]
}
`
}
