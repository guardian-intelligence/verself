package main

import (
	"strings"
	"testing"
)

func TestNormalizeAuthEdgeHAProxy(t *testing.T) {
	input := `
frontend fe_https
  acl host_verself var(txn.host) -m str verself.sh
  acl host_zitadel var(txn.host) -m str auth.verself.sh
  acl host_zitadel_actions var(txn.host) -m str zitadel-actions.verself.sh
  acl stripe_source src 3.18.12.63
  acl auth_path path -i /api/v1/auth/login /api/v1/auth/callback /api/v1/auth/session /api/v1/auth/organization /api/v1/auth/resource-token /api/v1/auth/logout /api/v1/auth/sessions
  http-request return status 405 if host_sandbox { path -i /github/installations/callback } !method_get
  http-request return status 405 if host_zitadel zitadel_product_token_claims !method_post
  http-request return status 405 if host_zitadel_actions zitadel_product_token_claims !method_post
  use_backend be_source_forgejo_webhook if host_source { path -i /webhooks/forgejo }
  use_backend be_zitadel_product_token_claims if host_zitadel method_post zitadel_product_token_claims
  use_backend be_zitadel_product_token_claims if host_zitadel_actions method_post zitadel_product_token_claims
  acl route_1_path path -i /api/v1/auth/login /api/v1/auth/callback /api/v1/auth/session /api/v1/auth/organization /api/v1/auth/resource-token /api/v1/auth/logout /api/v1/auth/sessions
`
	gotBytes, err := normalizeAuthEdgeHAProxy([]byte(input), "verself.sh")
	if err != nil {
		t.Fatalf("normalize auth edge: %v", err)
	}
	got := string(gotBytes)
	rejected := []string{
		"host_zitadel var",
		"host_zitadel_actions",
		"if host_zitadel ",
		"if host_zitadel_actions ",
	}
	for _, needle := range rejected {
		if strings.Contains(got, needle) {
			t.Fatalf("normalized config still contains %q:\n%s", needle, got)
		}
	}
	required := []string{
		"acl zitadel_oidc_path path -i /.well-known/openid-configuration /.well-known/webfinger /robots.txt",
		"acl zitadel_oidc_path path_beg /oauth/ /oauth/v2/ /oidc/ /oidc/v1/ /saml/ /ui/login /ui/console",
		"acl auth_path path -i /api/v1/auth/login /api/v1/auth/callback /api/v1/auth/session /api/v1/auth/organization /api/v1/auth/resource-token /api/v1/auth/logout /api/v1/auth/sessions /api/v1/auth/invites/accept",
		"http-request return status 405 if host_verself zitadel_product_token_claims !method_post",
		"use_backend be_zitadel_product_token_claims if host_verself method_post zitadel_product_token_claims",
		"use_backend be_route_product_auth_zitadel_oidc if host_verself zitadel_oidc_path",
		"acl route_1_path path -i /api/v1/auth/login /api/v1/auth/callback /api/v1/auth/session /api/v1/auth/organization /api/v1/auth/resource-token /api/v1/auth/logout /api/v1/auth/sessions /api/v1/auth/invites/accept",
	}
	for _, needle := range required {
		if !strings.Contains(got, needle) {
			t.Fatalf("normalized config missing %q:\n%s", needle, got)
		}
	}

	againBytes, err := normalizeAuthEdgeHAProxy(gotBytes, "verself.sh")
	if err != nil {
		t.Fatalf("normalize auth edge twice: %v", err)
	}
	if string(againBytes) != got {
		t.Fatalf("normalization is not idempotent:\nonce:\n%s\n\ntwice:\n%s", got, string(againBytes))
	}
}

func TestNormalizeAuthEdgePublicHosts(t *testing.T) {
	input := []byte("auth.verself.sh be_route_product_auth_zitadel_oidc\nverself.sh be_route_product_apex_verself_web_frontend\nzitadel-actions.verself.sh be_zitadel_product_token_claims\n")
	got := string(normalizeAuthEdgePublicHosts(input, "verself.sh"))
	if strings.Contains(got, "auth.verself.sh") || strings.Contains(got, "zitadel-actions.verself.sh") {
		t.Fatalf("legacy auth hosts remained:\n%s", got)
	}
	if !strings.Contains(got, "verself.sh be_route_product_apex_verself_web_frontend") {
		t.Fatalf("product apex mapping missing:\n%s", got)
	}
}
