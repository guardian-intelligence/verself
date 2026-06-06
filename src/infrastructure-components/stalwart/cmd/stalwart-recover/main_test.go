package main

import (
	"strings"
	"testing"
)

func TestRenderConfigUsesCRDValues(t *testing.T) {
	cfg := config{
		DataDir: "/var/lib/stalwart",
		Server: serverConfig{
			Hostname:     "mail.gamma.verself.sh",
			BaseURL:      "https://mail.gamma.verself.sh",
			HTTPAddr:     "127.0.0.1",
			HTTPPort:     8090,
			SMTPAddr:     "127.0.0.1",
			SMTPPort:     25,
			OTLPEndpoint: "http://127.0.0.1:4317",
		},
		Database: databaseConfig{
			Host: "/var/run/postgresql",
			Name: "stalwart",
			User: "stalwart",
		},
	}

	rendered := renderConfig(cfg, "$6$hash")
	for _, want := range []string{
		`hostname = "mail.gamma.verself.sh"`,
		`bind = ["127.0.0.1:8090"]`,
		`bind = ["127.0.0.1:25"]`,
		`database = "stalwart"`,
		`secret = "$6$hash"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, rendered)
		}
	}
	for _, old := range []string{"__VERSELF_", "__STALWART_"} {
		if strings.Contains(rendered, old) {
			t.Fatalf("rendered config kept placeholder %q:\n%s", old, rendered)
		}
	}
}

func TestValidateConfigRejectsRelativeStatePaths(t *testing.T) {
	cfg := config{
		RuntimeArtifact: "bazel-bin/src/infrastructure-components/stalwart/stalwart-runtime.tar",
		RuntimeRoot:     "var/lib/stalwart/runtime",
		ConfigPath:      "/etc/stalwart/config.toml",
		DataDir:         "/var/lib/stalwart",
		ReportPath:      "/run/verself/recovery/stalwart/report.json",
		User:            "stalwart",
		Group:           "stalwart",
		Server: serverConfig{
			Hostname: "mail.gamma.verself.sh",
			BaseURL:  "https://mail.gamma.verself.sh",
			HTTPAddr: "127.0.0.1",
			HTTPPort: 8090,
			SMTPAddr: "127.0.0.1",
			SMTPPort: 25,
		},
		Database: databaseConfig{
			Host: "/var/run/postgresql",
			Name: "stalwart",
			User: "stalwart",
		},
		OpenBao: openBaoConfig{
			Address: "https://127.0.0.1:8200",
			CACert:  "/etc/verself/openbao/ca.pem",
		},
	}
	if err := validateConfig(cfg); err == nil {
		t.Fatalf("validateConfig accepted relative runtimeRoot")
	}
}
