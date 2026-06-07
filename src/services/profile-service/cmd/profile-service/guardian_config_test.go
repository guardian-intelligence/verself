package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProfileRuntimeConfigUsesStaticAudience(t *testing.T) {
	dir := t.TempDir()
	graph := filepath.Join(dir, "document.json")
	if err := os.WriteFile(graph, []byte(`{
		"entrypoint": {},
		"resources": [{
			"apiVersion": "profile.guardianintelligence.org/v1alpha1",
			"kind": "ProfileService",
			"metadata": {"name": "profile-service"},
			"spec": {
				"auth": {"issuerURL": "https://gamma.verself.sh", "audience": "376309723453988548"},
				"postgres": {"dsn": "postgres://profile_service@/profile?host=/var/run/postgresql&sslmode=disable", "maxConns": 8},
				"spiffe": {"endpointSocket": "unix:///run/spire-agent/sockets/agent.sock"}
			}
		}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadProfileRuntimeConfig(graph, "profile-service")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthAudience != "376309723453988548" {
		t.Fatalf("AuthAudience = %q", cfg.AuthAudience)
	}
}

func TestLoadProfileRuntimeConfigRejectsUnknownAuthField(t *testing.T) {
	dir := t.TempDir()
	graph := filepath.Join(dir, "document.json")
	if err := os.WriteFile(graph, []byte(`{
		"entrypoint": {},
		"resources": [{
			"apiVersion": "profile.guardianintelligence.org/v1alpha1",
			"kind": "ProfileService",
			"metadata": {"name": "profile-service"},
			"spec": {
				"auth": {
					"issuerURL": "https://gamma.verself.sh",
					"audience": "376309723453988548",
					"unsupported": "value"
				},
				"postgres": {"dsn": "postgres://profile_service@/profile?host=/var/run/postgresql&sslmode=disable", "maxConns": 8},
				"spiffe": {"endpointSocket": "unix:///run/spire-agent/sockets/agent.sock"}
			}
		}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadProfileRuntimeConfig(graph, "profile-service"); err == nil {
		t.Fatal("loadProfileRuntimeConfig accepted unknown auth field")
	}
}
