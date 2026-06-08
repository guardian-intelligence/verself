package main

import (
	"errors"
	"os"
	"testing"
)

func TestParseOptionsDefaultsResourceGraph(t *testing.T) {
	opts, err := parseOptions([]string{"--repo-root=/tmp/repo"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if opts.resourceName != "spire" {
		t.Fatalf("resourceName = %q, want spire", opts.resourceName)
	}
	if opts.resourceGraph != "/tmp/repo/workspace/.guardian/fly/document.json" {
		t.Fatalf("resourceGraph = %q", opts.resourceGraph)
	}
}

func TestValidateConfigRejectsAbsoluteArtifacts(t *testing.T) {
	cfg := validConfig()
	cfg.RuntimeArtifact = "/tmp/spire-runtime.tar"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("validateConfig accepted absolute runtime artifact")
	}
}

func TestValidateIdentityRejectsUnsupportedSelector(t *testing.T) {
	id := identity{
		EntryID:            "entry",
		Group:              "svc",
		Key:                "svc.default",
		Path:               "/svc/test",
		Selector:           "unix:pid",
		UIDPolicy:          uidPolicy{Kind: "allocated"},
		User:               "svc",
		X509SVIDTTLSeconds: 3600,
	}
	if err := validateIdentity(id); err != nil {
		t.Fatal(err)
	}
	if _, err := identitySelector(id); err == nil {
		t.Fatal("identitySelector accepted unsupported selector")
	}
}

func TestClearJoinTokenRemovesStaleToken(t *testing.T) {
	cfg := validConfig()
	cfg.JoinTokenPath = t.TempDir() + "/join-token"
	if err := os.WriteFile(cfg.JoinTokenPath, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearJoinToken(cfg); err != nil {
		t.Fatal(err)
	}
	if err := clearJoinToken(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.JoinTokenPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale token to be removed, stat err = %v", err)
	}
}

func validConfig() config {
	return config{
		RuntimeArtifact:          "bazel-bin/src/infrastructure-components/spire/spire-runtime.tar",
		IdentityRegistryArtifact: "bazel-bin/src/infrastructure-components/spire/identity_registry.spire_identity_registry.json",
		RuntimeRoot:              "/var/lib/spire/runtime",
		ServerConfigPath:         "/etc/spire/server.conf",
		AgentConfigPath:          "/etc/spire/agent.conf",
		ServerDataDir:            "/var/lib/spire/server",
		AgentDataDir:             "/var/lib/spire/agent",
		ServerSocketPath:         "/run/spire-server/private/api.sock",
		AgentSocketPath:          "/run/spire-agent/sockets/agent.sock",
		JoinTokenPath:            "/run/verself/recovery/spire/join-token",
		ReportPath:               "/run/verself/recovery/spire/report.json",
		TrustDomain:              "gamma.verself.sh",
		AgentSPIFFEID:            "spiffe://gamma.verself.sh/spire/agent/gamma-primary",
		ServerAddress:            "127.0.0.1",
		ServerPort:               8081,
		ServerUser:               "spire",
		ServerGroup:              "spire",
		WorkloadGroup:            "spire_workload",
		RegistrarIntervalSeconds: 5,
	}
}
