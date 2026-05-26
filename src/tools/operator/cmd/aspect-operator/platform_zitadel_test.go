package main

import (
	"reflect"
	"testing"
)

func TestPlatformCompanyEmailLocalParts(t *testing.T) {
	want := []string{
		"anveio",
		"hello",
		"feedback",
		"sales",
		"support",
		"security",
		"abuse",
		"privacy",
		"legal",
		"billing",
		"careers",
		"press",
		"postmaster",
		"hostmaster",
		"webmaster",
		"noreply",
		"updates",
		"agents",
	}
	if got := platformCompanyEmailLocalParts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("platform company email local parts = %#v, want %#v", got, want)
	}
}

func TestPlatformRuntimeAuthAudienceSpecsUseProductAudience(t *testing.T) {
	credentialPaths := map[string]string{}
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
		if previous, ok := credentialPaths[spec.CredentialPath]; ok {
			t.Fatalf("duplicate runtime auth audience credential path %s for %s and %s", spec.CredentialPath, previous, spec.ComponentName)
		}
		credentialPaths[spec.CredentialPath] = spec.ComponentName
		if spec.ComponentName == "" || spec.CredentialPath == "" || spec.Group == "" {
			t.Fatalf("incomplete runtime auth audience spec: %#v", spec)
		}
	}
	if platformProductAPIProjectName != "verself-api" {
		t.Fatalf("product API project name = %q", platformProductAPIProjectName)
	}
}

func TestNativeOIDCConfigSupportsDeviceCodeWithoutClientSecret(t *testing.T) {
	body := nativeOIDCConfigBody()
	if got := body["appType"]; got != "OIDC_APP_TYPE_NATIVE" {
		t.Fatalf("native app type = %#v", got)
	}
	if got := body["authMethodType"]; got != "OIDC_AUTH_METHOD_TYPE_NONE" {
		t.Fatalf("native auth method = %#v", got)
	}
	if got := body["accessTokenType"]; got != "OIDC_TOKEN_TYPE_JWT" {
		t.Fatalf("native access token type = %#v", got)
	}
	grantTypes, ok := body["grantTypes"].([]string)
	if !ok {
		t.Fatalf("grantTypes = %#v", body["grantTypes"])
	}
	if !containsString(grantTypes, "OIDC_GRANT_TYPE_DEVICE_CODE") || !containsString(grantTypes, "OIDC_GRANT_TYPE_REFRESH_TOKEN") {
		t.Fatalf("native grant types = %#v", grantTypes)
	}
}

func TestRootOwnerDisplayNameComesFromAddressLocalPart(t *testing.T) {
	if got := rootOwnerDisplayName("anveio@verself.sh"); got != "Anveio" {
		t.Fatalf("root owner display name = %q", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
