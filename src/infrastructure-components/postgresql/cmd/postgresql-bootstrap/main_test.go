package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReplicationRoles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roles.json")
	if err := os.WriteFile(path, []byte(`{"roles":[{"name":"electric_iam","connection_limit":15,"password_env":"VERSELF_POSTGRESQL_REPLICATION_ROLE_PASSWORD_ELECTRIC_IAM"}],"publications":[{"database":"iam_service","publication":"electric_publication_iam","publication_owner":"electric_iam","table_owner":"iam_service","replication_role":"electric_iam","tables":["iam_web_auth_clients"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadReplicationConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Roles) != 1 || cfg.Roles[0].Name != "electric_iam" {
		t.Fatalf("roles = %#v", cfg.Roles)
	}
	if len(cfg.Publications) != 1 || cfg.Publications[0].Publication != "electric_publication_iam" {
		t.Fatalf("publications = %#v", cfg.Publications)
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
