package haproxy_test

import (
	"os"
	"strings"
	"testing"
)

func TestZitadelProductTokenClaimsRouteIsLoopbackOnly(t *testing.T) {
	cfg := readTemplate(t, "haproxy.cfg.j2")
	requireContains(t, cfg, "acl zitadel_product_token_claims path -i /internal/zitadel/actions/product-token-claims")
	requireContains(t, cfg, "acl zitadel_product_token_claims_source src 127.0.0.1")
	requireContains(t, cfg, "http-request return status 403 if host_verself zitadel_product_token_claims !zitadel_product_token_claims_source")
	requireContains(t, cfg, "use_backend be_zitadel_product_token_claims if host_verself method_post zitadel_product_token_claims zitadel_product_token_claims_source")

	deny := strings.Index(cfg, "http-request return status 403 if host_verself zitadel_product_token_claims !zitadel_product_token_claims_source")
	route := strings.Index(cfg, "use_backend be_zitadel_product_token_claims if host_verself method_post zitadel_product_token_claims zitadel_product_token_claims_source")
	if deny < 0 || route < 0 || deny > route {
		t.Fatalf("Zitadel action deny rule must appear before backend routing")
	}

	upstreams := readTemplate(t, "nomad-upstreams.cfg.j2")
	publicIAMAPI := backendBlock(t, upstreams, "backend be_route_product_iam_api_iam_service_public_api")
	requireContains(t, publicIAMAPI, "http-request return status 404 unless { path_beg /api/v1 }")
	if strings.Contains(publicIAMAPI, "/internal/zitadel/actions/product-token-claims") {
		t.Fatalf("public IAM API backend must not allow the internal Zitadel action path")
	}
}

func readTemplate(t *testing.T, name string) string {
	t.Helper()
	rel := "src/infrastructure-components/haproxy/templates/" + name
	for _, path := range []string{
		rel,
		"_main/" + rel,
		"templates/" + name,
	} {
		if root := strings.TrimSpace(os.Getenv("TEST_SRCDIR")); root != "" {
			path = root + "/" + path
		}
		raw, err := os.ReadFile(path)
		if err == nil {
			return string(raw)
		}
	}
	t.Fatalf("read %s from runfiles or source tree", name)
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
