package main

import "testing"

func TestValidateConfigRejectsReplicaOutOfRange(t *testing.T) {
	cfg := validConfig()
	cfg.Replica = 1
	cfg.ReplicaCount = 1
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected replica range validation error")
	}
}

func TestValidateConfigRejectsInvalidAddress(t *testing.T) {
	cfg := validConfig()
	cfg.Addresses = []string{"127.0.0.1"}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected address validation error")
	}
}

func validConfig() config {
	return config{
		RuntimeArtifact: "bazel-bin/src/infrastructure-components/tigerbeetle/tigerbeetle-runtime.tar",
		RuntimeRoot:     "/var/lib/tigerbeetle/runtime",
		DataFile:        "/var/lib/tigerbeetle/data.tigerbeetle",
		ReportPath:      "/run/verself/recovery/tigerbeetle/report.json",
		User:            "tigerbeetle",
		Group:           "tigerbeetle",
		ClusterID:       0,
		Replica:         0,
		ReplicaCount:    1,
		Addresses:       []string{"127.0.0.1:3320"},
		LogLevel:        "info",
		StatsdAddress:   "127.0.0.1:8125",
		Experimental:    true,
	}
}
