package haproxy_test

import (
	"os"
	"strings"
	"testing"
)

func TestZitadelProductTokenClaimsBackendIsInternalOnly(t *testing.T) {
	runtimeTemplate := readRepoFile(t, "src/infrastructure-components/haproxy/nomad.hcl")
	publicIAMAPI := backendBlock(t, runtimeTemplate, "backend be_route_product_iam_api_iam_service_public_api")
	requireContains(t, publicIAMAPI, "http-request return status 404 unless { path_beg /api/v1 }")
	if strings.Contains(publicIAMAPI, "/internal/zitadel/actions/product-token-claims") {
		t.Fatalf("public IAM API backend must not allow the internal Zitadel action path")
	}
	internalAction := backendBlock(t, runtimeTemplate, "backend be_zitadel_product_token_claims")
	requireContains(t, internalAction, `[[ with nomadService "iam-service-public-http" ]]`)
	requireContains(t, internalAction, "http-request deny deny_status 413 if { req.body_size gt 65536 }")
}

func TestDeploymentServicePublicBackendAllowsOnlyAPIAndHealthz(t *testing.T) {
	runtimeTemplate := readRepoFile(t, "src/infrastructure-components/haproxy/nomad.hcl")
	runtimeBackend := backendBlock(t, runtimeTemplate, "backend be_route_product_deployments_api_deployment_service_public_api")
	requireContains(t, runtimeBackend, `[[ with nomadService "deployment-service-public-api" ]]`)
	requireContains(t, runtimeBackend, "acl deployment_service_allowed path -i /healthz")
	requireContains(t, runtimeBackend, "acl deployment_service_allowed path_beg /api/v1")
	if strings.Contains(runtimeBackend, "proto h2") {
		t.Fatalf("deployment-service backend is plain HTTP/1.1 until the service explicitly supports h2c")
	}
	if strings.Contains(runtimeBackend, "/readyz") {
		t.Fatalf("deployment-service readiness details must stay behind authenticated API checks")
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	for _, path := range []string{
		rel,
		"_main/" + rel,
		strings.TrimPrefix(rel, "src/infrastructure-components/haproxy/"),
	} {
		if root := strings.TrimSpace(os.Getenv("TEST_SRCDIR")); root != "" {
			path = root + "/" + path
		}
		raw, err := os.ReadFile(path)
		if err == nil {
			return string(raw)
		}
	}
	t.Fatalf("read %s from runfiles or source tree", rel)
	return ""
}

func backendBlock(t *testing.T, cfg, name string) string {
	t.Helper()
	start := strings.Index(cfg, name)
	if start < 0 {
		t.Fatalf("missing %q", name)
	}
	rest := cfg[start+len(name):]
	if next := strings.Index(rest, "\nbackend "); next >= 0 {
		return cfg[start : start+len(name)+next]
	}
	return cfg[start:]
}

func requireContains(t *testing.T, text, needle string) {
	t.Helper()
	if !strings.Contains(text, needle) {
		t.Fatalf("missing %q", needle)
	}
}
