package main

import "testing"

func TestOIDCConfigBodies(t *testing.T) {
	browser := browserOIDCConfigBody([]string{"https://verself.sh/api/v1/auth/callback"}, []string{"https://verself.sh"})
	if got := browser["authMethodType"]; got != "OIDC_AUTH_METHOD_TYPE_POST" {
		t.Fatalf("browser auth method = %v", got)
	}
	if got := browser["appType"]; got != "OIDC_APP_TYPE_WEB" {
		t.Fatalf("browser app type = %v", got)
	}
	native := nativeOIDCConfigBody()
	if got := native["authMethodType"]; got != "OIDC_AUTH_METHOD_TYPE_NONE" {
		t.Fatalf("native auth method = %v", got)
	}
	if got := native["appType"]; got != "OIDC_APP_TYPE_NATIVE" {
		t.Fatalf("native app type = %v", got)
	}
}

func TestProductTokenClaimsTargetBody(t *testing.T) {
	body := productTokenClaimsTargetBody("target", "https://verself.sh/internal/zitadel/actions/product-token-claims")
	if body["name"] != "target" {
		t.Fatalf("name = %v", body["name"])
	}
	if body["endpoint"] != "https://verself.sh/internal/zitadel/actions/product-token-claims" {
		t.Fatalf("endpoint = %v", body["endpoint"])
	}
	if body["timeout"] != "1s" {
		t.Fatalf("timeout = %v", body["timeout"])
	}
}
