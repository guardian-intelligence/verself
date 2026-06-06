package main

import "testing"

func TestValidateConfigRejectsInvalidPort(t *testing.T) {
	cfg := validConfig()
	cfg.Port = 0
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestValidateConfigRejectsInvalidPublisherUser(t *testing.T) {
	cfg := validConfig()
	cfg.PublisherUser = "bad:user"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected invalid publisher user error")
	}
}

func validConfig() config {
	return config{
		RuntimeArtifact: "bazel-bin/src/infrastructure-components/zot/zot-runtime.tar",
		RuntimeRoot:     "/var/lib/zot/runtime",
		ConfigPath:      "/etc/zot/config.json",
		StorageDir:      "/var/lib/zot/storage",
		HtpasswdPath:    "/etc/zot/htpasswd",
		ReportPath:      "/run/verself/recovery/zot/report.json",
		User:            "zot",
		Group:           "zot",
		Host:            "127.0.0.1",
		Port:            5080,
		Realm:           "verself-artifacts",
		LogLevel:        "info",
		PublisherUser:   "artifact-publisher",
	}
}
