package postgresruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPlanCollectsOwnerLocalPostgresDeclarations(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/services/billing-service/deploy/postgres.yml", `
postgresql_service_databases:
  - { name: billing, owner: billing }
postgresql_peer_mappings:
  - { system_user: billing, pg_user: billing }
`)
	writeFile(t, root, "src/infrastructure-components/electric/deploy/postgres.yml", `
postgresql_replication_roles:
  - name: electric
    connection_limit: 25
    openbao_secret: electric.pg.password
`)

	plan, err := LoadPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Databases) != 1 {
		t.Fatalf("len(databases) = %d, want 1", len(plan.Databases))
	}
	if len(plan.ReplicationRoles) != 1 {
		t.Fatalf("len(replication roles) = %d, want 1", len(plan.ReplicationRoles))
	}
	if len(plan.PeerMappings) != 2 {
		t.Fatalf("len(peer mappings) = %d, want 2 including postgres base mapping", len(plan.PeerMappings))
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
