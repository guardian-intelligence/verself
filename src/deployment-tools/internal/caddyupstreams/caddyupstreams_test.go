package caddyupstreams

import (
	"strings"
	"testing"

	"github.com/verself/deployment-tools/internal/nomadclient"
)

func TestEnvVarName(t *testing.T) {
	cases := map[string]string{
		"billing-public-http":                        "VERSELF_UPSTREAM_BILLING_PUBLIC_HTTP",
		"sandbox-rental-public-http":                 "VERSELF_UPSTREAM_SANDBOX_RENTAL_PUBLIC_HTTP",
		"source-code-hosting-service-internal-https": "VERSELF_UPSTREAM_SOURCE_CODE_HOSTING_SERVICE_INTERNAL_HTTPS",
		"verself-web-http":                           "VERSELF_UPSTREAM_VERSELF_WEB_HTTP",
	}
	for in, want := range cases {
		if got := envVarName(in); got != want {
			t.Errorf("envVarName(%q): got %q, want %q", in, got, want)
		}
	}

	// Names containing characters outside [a-z0-9_-] are rejected so
	// the controller doesn't quietly write env vars Caddy can't bind.
	for _, bad := range []string{"", "Billing-public", "billing.public", "billing public"} {
		if got := envVarName(bad); got != "" {
			t.Errorf("envVarName(%q): expected empty (rejected), got %q", bad, got)
		}
	}
}

func TestBuildEnvFile_DeterministicOrder(t *testing.T) {
	body := buildEnvFile([]nomadclient.ServiceAddress{
		{Name: "verself-web-http", Address: "127.0.0.1", Port: 21345},
		{Name: "billing-public-http", Address: "127.0.0.1", Port: 24501},
		{Name: "company-http", Address: "127.0.0.1", Port: 25789},
	})

	wantLines := []string{
		"VERSELF_UPSTREAM_BILLING_PUBLIC_HTTP=127.0.0.1:24501",
		"VERSELF_UPSTREAM_COMPANY_HTTP=127.0.0.1:25789",
		"VERSELF_UPSTREAM_VERSELF_WEB_HTTP=127.0.0.1:21345",
	}
	got := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(got) != len(wantLines) {
		t.Fatalf("buildEnvFile: %d lines, want %d\n%s", len(got), len(wantLines), body)
	}
	for i, want := range wantLines {
		if got[i] != want {
			t.Errorf("line %d: got %q, want %q", i, got[i], want)
		}
	}
}

// Internal-mTLS endpoints (e.g. billing-internal_https) end up in the
// upstreams.env too — Caddy ignores entries it doesn't reference, but
// the file must remain valid env-file syntax.
func TestBuildEnvFile_IncludesInternalEndpoints(t *testing.T) {
	body := buildEnvFile([]nomadclient.ServiceAddress{
		{Name: "billing-internal-https", Address: "127.0.0.1", Port: 31337},
	})
	if !strings.Contains(body, "VERSELF_UPSTREAM_BILLING_INTERNAL_HTTPS=127.0.0.1:31337\n") {
		t.Errorf("internal endpoint dropped from upstreams.env: %q", body)
	}
}
