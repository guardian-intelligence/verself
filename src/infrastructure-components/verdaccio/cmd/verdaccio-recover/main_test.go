package main

import "testing"

func TestValidateConfigRejectsInvalidPort(t *testing.T) {
	cfg := validConfig()
	cfg.Port = 0
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestValidateConfigRejectsRelativeConfigPath(t *testing.T) {
	cfg := validConfig()
	cfg.ConfigPath = "etc/verdaccio/config.yaml"
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected absolute config path error")
	}
}

func TestSafeTarTargetAllowsRootDirectoryEntry(t *testing.T) {
	dest := t.TempDir()
	target, err := safeTarTarget(dest, "./")
	if err != nil {
		t.Fatalf("safeTarTarget returned error: %v", err)
	}
	if target != dest {
		t.Fatalf("target = %q, want %q", target, dest)
	}
}

func TestSafeTarTargetRejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	if _, err := safeTarTarget(dest, "../escape"); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestSafeLinkTargetAllowsInTreeRelativeLink(t *testing.T) {
	dest := t.TempDir()
	target, err := safeTarTarget(dest, "node_modules/.bin/verdaccio")
	if err != nil {
		t.Fatalf("safeTarTarget returned error: %v", err)
	}
	if _, err := safeLinkTarget(dest, target, "../verdaccio/bin/verdaccio"); err != nil {
		t.Fatalf("safeLinkTarget returned error: %v", err)
	}
}

func TestSafeLinkTargetRejectsEscape(t *testing.T) {
	dest := t.TempDir()
	target, err := safeTarTarget(dest, "node_modules/.bin/escape")
	if err != nil {
		t.Fatalf("safeTarTarget returned error: %v", err)
	}
	if _, err := safeLinkTarget(dest, target, "../../../escape"); err == nil {
		t.Fatal("expected escaping symlink error")
	}
}

func validConfig() config {
	return config{
		RuntimeArtifact: "bazel-bin/src/infrastructure-components/verdaccio/verdaccio-runtime.tar",
		RuntimeRoot:     "/var/lib/verdaccio/runtime",
		ConfigPath:      "/etc/verdaccio/config.yaml",
		StorageDir:      "/var/lib/verdaccio/storage",
		HtpasswdPath:    "/var/lib/verdaccio/htpasswd",
		ReportPath:      "/run/verself/recovery/verdaccio/report.json",
		User:            "verdaccio",
		Group:           "verdaccio",
		Host:            "127.0.0.1",
		Port:            4873,
		MaxBodySize:     "100mb",
		Uplink: uplinkConfig{
			Name:      "npmjs",
			URL:       "https://registry.npmjs.org/",
			Cache:     true,
			MaxAge:    "30m",
			StrictSSL: true,
		},
		PackageFilter: packageFilterConfig{MinAgeDays: 3},
		Log:           logConfig{Level: "http"},
	}
}
