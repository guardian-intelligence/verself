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

func TestSecretsServicePublicBackendUsesPlainHTTP(t *testing.T) {
	runtimeTemplate := readRepoFile(t, "src/infrastructure-components/haproxy/nomad.hcl")
	runtimeBackend := backendBlock(t, runtimeTemplate, "backend be_route_product_secrets_api_secrets_service_public_api")
	requireContains(t, runtimeBackend, `[[ with nomadService "secrets-service-public-http" ]]`)
	requireContains(t, runtimeBackend, "http-request return status 404 unless { path_beg /api/v1 }")
	if strings.Contains(runtimeBackend, "proto h2") {
		t.Fatalf("secrets-service backend is plain HTTP/1.1 until the service explicitly supports h2c")
	}
}

func TestProjectsServicePublicBackendUsesPlainHTTP(t *testing.T) {
	runtimeTemplate := readRepoFile(t, "src/infrastructure-components/haproxy/nomad.hcl")
	runtimeBackend := backendBlock(t, runtimeTemplate, "backend be_route_product_projects_api_projects_service_public_api")
	requireContains(t, runtimeBackend, `[[ with nomadService "projects-service-public-http" ]]`)
	requireContains(t, runtimeBackend, "http-request return status 404 unless { path_beg /api/v1 }")
	if strings.Contains(runtimeBackend, "proto h2") {
		t.Fatalf("projects-service backend is plain HTTP/1.1 until the service explicitly supports h2c")
	}
}

func TestProfileServicePublicBackendUsesPlainHTTP(t *testing.T) {
	runtimeTemplate := readRepoFile(t, "src/infrastructure-components/haproxy/nomad.hcl")
	runtimeBackend := backendBlock(t, runtimeTemplate, "backend be_route_product_profile_api_profile_service_public_api")
	requireContains(t, runtimeBackend, `[[ with nomadService "profile-service-public-http" ]]`)
	requireContains(t, runtimeBackend, "http-request return status 404 unless { path_beg /api/v1 }")
	if strings.Contains(runtimeBackend, "proto h2") {
		t.Fatalf("profile-service backend is plain HTTP/1.1 until the service explicitly supports h2c")
	}
}

func TestGovernanceServicePublicBackendUsesPlainHTTP(t *testing.T) {
	runtimeTemplate := readRepoFile(t, "src/infrastructure-components/haproxy/nomad.hcl")
	runtimeBackend := backendBlock(t, runtimeTemplate, "backend be_route_product_governance_api_governance_service_public_api")
	requireContains(t, runtimeBackend, `[[ with nomadService "governance-service-public-http" ]]`)
	requireContains(t, runtimeBackend, "http-request return status 404 unless { path_beg /api/v1 }")
	if strings.Contains(runtimeBackend, "proto h2") {
		t.Fatalf("governance-service backend is plain HTTP/1.1 until the service explicitly supports h2c")
	}
}

func TestBillingPublicBackendsUsePlainHTTP(t *testing.T) {
	runtimeTemplate := readRepoFile(t, "src/infrastructure-components/haproxy/nomad.hcl")
	for _, backend := range []string{
		"backend be_route_product_billing_api_billing_public_api",
		"backend be_billing_stripe_webhook",
	} {
		runtimeBackend := backendBlock(t, runtimeTemplate, backend)
		requireContains(t, runtimeBackend, `[[ with nomadService "billing-public-http" ]]`)
		if strings.Contains(runtimeBackend, "proto h2") {
			t.Fatalf("%s is plain HTTP/1.1 until the service explicitly supports h2c", backend)
		}
	}
}

func TestSourceCodeHostingPublicBackendsUsePlainHTTP(t *testing.T) {
	runtimeTemplate := readRepoFile(t, "src/infrastructure-components/haproxy/nomad.hcl")
	for _, backend := range []string{
		"backend be_route_product_git_source_code_hosting_service_git_smart_http",
		"backend be_route_product_source_api_source_code_hosting_service_public_api",
		"backend be_source_forgejo_webhook",
	} {
		runtimeBackend := backendBlock(t, runtimeTemplate, backend)
		requireContains(t, runtimeBackend, `[[ with nomadService "source-code-hosting-service-public-http" ]]`)
		if strings.Contains(runtimeBackend, "proto h2") {
			t.Fatalf("%s is plain HTTP/1.1 until the service explicitly supports h2c", backend)
		}
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
