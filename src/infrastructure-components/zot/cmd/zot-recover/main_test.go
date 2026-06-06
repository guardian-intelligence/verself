package main

import (
	"strings"
	"testing"
)

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

func TestListenerInodesFromTCPTableReturnsOnlyRequestedListeningPort(t *testing.T) {
	table := strings.Join([]string{
		"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode",
		"   0: 0100007F:13D8 00000000:0000 0A 00000000:00000000 00:00000000 00000000   101        0 12345 1 0000000000000000 100 0 0 10 0",
		"   1: 0100007F:13D8 0100007F:C001 01 00000000:00000000 00:00000000 00000000   101        0 54321 1 0000000000000000 100 0 0 10 0",
		"   2: 0100007F:13D9 00000000:0000 0A 00000000:00000000 00:00000000 00000000   101        0 67890 1 0000000000000000 100 0 0 10 0",
	}, "\n")
	inodes, err := listenerInodesFromTCPTable(strings.NewReader(table), 5080)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := inodes["12345"]; !ok {
		t.Fatalf("expected requested listener inode, got %#v", inodes)
	}
	if _, ok := inodes["54321"]; ok {
		t.Fatalf("did not expect established socket inode, got %#v", inodes)
	}
	if _, ok := inodes["67890"]; ok {
		t.Fatalf("did not expect other port inode, got %#v", inodes)
	}
}

func TestIsZotProcessMatchesRuntimeBinary(t *testing.T) {
	if !isZotProcess("/var/lib/zot/runtime/current/bin/zot serve /etc/zot/config.json") {
		t.Fatal("expected zot runtime process to match")
	}
	if isZotProcess("/usr/bin/python3 -m http.server 5080") {
		t.Fatal("did not expect unrelated process to match")
	}
}

func TestProcessStateFromStatHandlesProcessNamesWithSpaces(t *testing.T) {
	state, ok := processStateFromStat("1234 (zot worker) Z 1 2 3 4")
	if !ok {
		t.Fatal("expected process state")
	}
	if state != "Z" {
		t.Fatalf("expected Z, got %q", state)
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
