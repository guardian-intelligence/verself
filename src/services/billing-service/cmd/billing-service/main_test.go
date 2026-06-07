package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	workloadauth "github.com/verself/service-runtime/workload"
)

func TestBillingInternalPeerServices(t *testing.T) {
	got := billingInternalPeerServices()
	want := []string{
		workloadauth.ServiceIAM,
		workloadauth.ServiceSandboxRental,
		workloadauth.ServiceSecrets,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("billing internal peer services = %v, want %v", got, want)
	}
}

func TestLoadBillingRuntimeConfig(t *testing.T) {
	path := writeBillingGraph(t, validBillingGraph())
	cfg, err := loadBillingRuntimeConfig(path, defaultBillingServiceResource)
	if err != nil {
		t.Fatalf("load billing runtime config: %v", err)
	}
	if cfg.InstallationID != "inst_gamma_01JZ0000000000000000000000" {
		t.Fatalf("installation id = %q", cfg.InstallationID)
	}
	if cfg.AuthAudienceName != "iam-service.zitadel.auth_audience" {
		t.Fatalf("auth audience = %q", cfg.AuthAudienceName)
	}
	if !slices.Equal(cfg.TigerBeetleAddresses, []string{"127.0.0.1:3320"}) {
		t.Fatalf("tigerbeetle addresses = %v", cfg.TigerBeetleAddresses)
	}
	if !slices.Equal(cfg.BillingReturnOrigins, []string{"https://gamma.verself.sh"}) {
		t.Fatalf("return origins = %v", cfg.BillingReturnOrigins)
	}
}

func TestLoadBillingRuntimeConfigRejectsMissingStripeRef(t *testing.T) {
	graph := strings.Replace(validBillingGraph(), `"name": "billing-service.stripe.secret_key"`, `"name": ""`, 1)
	path := writeBillingGraph(t, graph)
	_, err := loadBillingRuntimeConfig(path, defaultBillingServiceResource)
	if err == nil {
		t.Fatal("expected missing stripe secret ref error")
	}
	if !strings.Contains(err.Error(), "stripe.secretKeyRef.name is required") {
		t.Fatalf("error = %v", err)
	}
}

func writeBillingGraph(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "document.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	return path
}

func validBillingGraph() string {
	return `{
  "entrypoint": {
    "apiVersion": "guardian.guardianintelligence.org/v1alpha1",
    "kind": "FlyProcedure",
    "name": "gamma"
  },
  "resources": [
    {
      "apiVersion": "billing.guardianintelligence.org/v1alpha1",
      "kind": "BillingService",
      "metadata": {"name": "billing-service"},
      "spec": {
        "installationID": "inst_gamma_01JZ0000000000000000000000",
        "auth": {
          "issuerURL": "https://gamma.verself.sh",
          "audienceRef": {
            "apiVersion": "openbao.guardianintelligence.org/v1alpha1",
            "kind": "SecretPath",
            "name": "iam-service.zitadel.auth_audience"
          }
        },
        "postgres": {
          "dsn": "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable",
          "maxConns": 12,
          "minConns": 1,
          "maxConnLifetimeSeconds": 1800,
          "maxConnIdleSeconds": 300
        },
        "clickhouse": {
          "address": "127.0.0.1:9440",
          "user": "billing_service",
          "caCertPath": "/etc/verself/clickhouse/server-ca.pem"
        },
        "tigerbeetle": {
          "addresses": ["127.0.0.1:3320"],
          "clusterID": 0
        },
        "billing": {
          "returnOrigins": ["https://gamma.verself.sh"]
        },
        "stripe": {
          "secretKeyRef": {
            "apiVersion": "openbao.guardianintelligence.org/v1alpha1",
            "kind": "SecretPath",
            "name": "billing-service.stripe.secret_key"
          },
          "webhookSecretRef": {
            "apiVersion": "openbao.guardianintelligence.org/v1alpha1",
            "kind": "SecretPath",
            "name": "billing-service.stripe.webhook_secret"
          }
        },
        "spiffe": {
          "endpointSocket": "unix:///run/spire-agent/sockets/agent.sock"
        }
      }
    }
  ]
}`
}
