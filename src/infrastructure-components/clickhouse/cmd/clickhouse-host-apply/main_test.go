package main

import (
	"strings"
	"testing"
)

func TestRenderServerConfigBindsOperatorSPIFFEID(t *testing.T) {
	cfg := defaultConfig()
	cfg.spiffeServicePrefix = "spiffe://gamma.verself.sh/svc"

	body := renderServerConfig(cfg)
	for _, want := range []string{
		"<tcp_port_secure>9440</tcp_port_secure>",
		"<subject_alt_name>URI:spiffe://gamma.verself.sh/svc/clickhouse-operator</subject_alt_name>",
		"<access_management>1</access_management>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("server config missing %q:\n%s", want, body)
		}
	}
}

func TestBootstrapFingerprintIncludesSPIFFEPrefix(t *testing.T) {
	migration := []byte("CREATE USER x SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/x';\n")
	a := bootstrapFingerprint(migration, "spiffe://gamma.verself.sh/svc")
	b := bootstrapFingerprint(migration, "spiffe://prod.verself.sh/svc")

	if a == b {
		t.Fatal("fingerprint must change when SPIFFE service prefix changes")
	}
}
