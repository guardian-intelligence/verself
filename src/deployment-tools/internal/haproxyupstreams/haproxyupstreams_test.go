package haproxyupstreams

import (
	"strings"
	"testing"

	"github.com/verself/deployment-tools/internal/nomadclient"
)

func TestMapKey(t *testing.T) {
	cases := map[string]string{
		"billing-public-http":                        "VERSELF_UPSTREAM_BILLING_PUBLIC_HTTP",
		"sandbox-rental-public-http":                 "VERSELF_UPSTREAM_SANDBOX_RENTAL_PUBLIC_HTTP",
		"source-code-hosting-service-internal-https": "VERSELF_UPSTREAM_SOURCE_CODE_HOSTING_SERVICE_INTERNAL_HTTPS",
		"verself-web-http":                           "VERSELF_UPSTREAM_VERSELF_WEB_HTTP",
	}
	for in, want := range cases {
		if got := mapKey(in); got != want {
			t.Errorf("mapKey(%q): got %q, want %q", in, got, want)
		}
	}

	// Names containing characters outside [a-z0-9_-] are rejected so
	// the controller doesn't quietly write map keys HAProxy can't bind.
	for _, bad := range []string{"", "Billing-public", "billing.public", "billing public"} {
		if got := mapKey(bad); got != "" {
			t.Errorf("mapKey(%q): expected empty (rejected), got %q", bad, got)
		}
	}
}

func TestBuildMapFile_DeterministicOrder(t *testing.T) {
	body, err := buildMapFile([]nomadclient.ServiceAddress{
		{Name: "verself-web-http", Address: "127.0.0.1", Port: 21345},
		{Name: "billing-public-http", Address: "127.0.0.1", Port: 24501},
		{Name: "company-http", Address: "127.0.0.1", Port: 25789},
	})
	if err != nil {
		t.Fatalf("buildMapFile: %v", err)
	}

	wantLines := []string{
		"VERSELF_UPSTREAM_BILLING_PUBLIC_HTTP 127.0.0.1:24501",
		"VERSELF_UPSTREAM_COMPANY_HTTP 127.0.0.1:25789",
		"VERSELF_UPSTREAM_VERSELF_WEB_HTTP 127.0.0.1:21345",
	}
	got := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(got) != len(wantLines) {
		t.Fatalf("buildMapFile: %d lines, want %d\n%s", len(got), len(wantLines), body)
	}
	for i, want := range wantLines {
		if got[i] != want {
			t.Errorf("line %d: got %q, want %q", i, got[i], want)
		}
	}
}

// Internal-mTLS endpoints (e.g. billing-internal_https) end up in the
// map too; HAProxy ignores entries it doesn't reference, but the file
// must remain valid map syntax.
func TestBuildMapFile_IncludesInternalEndpoints(t *testing.T) {
	body, err := buildMapFile([]nomadclient.ServiceAddress{
		{Name: "billing-internal-https", Address: "127.0.0.1", Port: 31337},
	})
	if err != nil {
		t.Fatalf("buildMapFile: %v", err)
	}
	if !strings.Contains(body, "VERSELF_UPSTREAM_BILLING_INTERNAL_HTTPS 127.0.0.1:31337\n") {
		t.Errorf("internal endpoint dropped from upstreams.map: %q", body)
	}
}

func TestBuildMapFileRejectsNonLoopbackEndpoints(t *testing.T) {
	_, err := buildMapFile([]nomadclient.ServiceAddress{
		{Name: "billing-public-http", Address: "10.0.0.12", Port: 31337},
	})
	if err == nil {
		t.Fatal("buildMapFile accepted non-loopback service address")
	}
}
