package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReplicationRoles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roles.json")
	if err := os.WriteFile(path, []byte(`{"roles":[{"name":"electric_iam","connection_limit":15,"password_env":"VERSELF_POSTGRESQL_REPLICATION_ROLE_PASSWORD_ELECTRIC_IAM"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	roles, err := loadReplicationRoles(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Name != "electric_iam" {
		t.Fatalf("roles = %#v", roles)
	}
}

func TestLoadReplicationRolesRejectsInvalidRoleName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roles.json")
	if err := os.WriteFile(path, []byte(`{"roles":[{"name":"electric-iam","connection_limit":15,"password_env":"VERSELF_POSTGRESQL_REPLICATION_ROLE_PASSWORD_ELECTRIC_IAM"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadReplicationRoles(path)
	if err == nil || !strings.Contains(err.Error(), "roles[0].name") {
		t.Fatalf("err = %v", err)
	}
}
