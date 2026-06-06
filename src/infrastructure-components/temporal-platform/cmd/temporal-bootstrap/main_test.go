package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSpecsReadsBootstrapFromFullTemporalPlatformResource(t *testing.T) {
	graph := `{
  "entrypoint": {"apiVersion":"guardian.guardianintelligence.org/v1alpha1","kind":"FlyProcedure","name":"gamma"},
  "resources": [
    {
      "apiVersion": "temporal.guardianintelligence.org/v1alpha1",
      "kind": "TemporalPlatform",
      "metadata": {"name": "temporal"},
      "spec": {
        "runtimeArtifact": "bazel-bin/src/infrastructure-components/temporal-platform/temporal-runtime.tar",
        "runtimeRoot": "/var/lib/temporal/runtime",
        "stateDir": "/var/lib/temporal",
        "authConfigPath": "/etc/temporal/auth.json",
        "reportPath": "/run/verself/recovery/temporal/report.json",
        "user": "temporal_server",
        "group": "temporal_server",
        "workloadGroup": "spire_workload",
        "persistence": {
          "user": "temporal",
          "socketDir": "/var/run/postgresql",
          "defaultDatabase": "temporal",
          "visibilityDatabase": "temporal_visibility",
          "defaultMaxConns": 20,
          "visibilityMaxConns": 10
        },
        "authorization": {
          "systemAdminIDs": ["spiffe://gamma.verself.sh/svc/temporal-server"]
        },
        "bootstrap": {
          "namespaces": [
            {"name": "billing-service", "retention": "24h"},
            {"name": "billing-service", "retention": "24h"},
            {"name": "distribution-service", "retention": "48h"}
          ]
        }
      }
    }
  ]
}`
	path := filepath.Join(t.TempDir(), "document.json")
	if err := os.WriteFile(path, []byte(graph), 0o644); err != nil {
		t.Fatal(err)
	}
	specs, err := loadSpecs(options{resourceGraph: path, resourceName: "temporal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("len(specs) = %d, want 2", len(specs))
	}
	if specs[0].Name != "billing-service" || specs[0].Retention != 24*time.Hour {
		t.Fatalf("first spec = %#v", specs[0])
	}
	if specs[1].Name != "distribution-service" || specs[1].Retention != 48*time.Hour {
		t.Fatalf("second spec = %#v", specs[1])
	}
}
