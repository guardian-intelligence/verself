package main

import (
	"fmt"
	"testing"

	iamclient "github.com/verself/iam-service/client"
)

func TestAuthorizationProbeOrgIDUsesResolvedCanonicalOrgID(t *testing.T) {
	resp := &iamclient.ResolveOrganizationResponse{
		Result: &iamclient.ResolveOrganizationOutputBody{
			Organization: iamclient.OrganizationProfile{
				OrgID: "org_B7HWGKW0SH7G4EXW9XT8TCT60C",
			},
		},
	}

	got, ok := authorizationProbeOrgID("org_00000000000000000000000000", resp)
	if !ok {
		t.Fatal("authorization probe org id was not accepted")
	}
	if got != "org_B7HWGKW0SH7G4EXW9XT8TCT60C" {
		t.Fatalf("authorization probe org id = %q", got)
	}
}

func TestAuthorizationProbeOrgIDFallsBackWhenResolutionHasNoResult(t *testing.T) {
	got, ok := authorizationProbeOrgID("  org_00000000000000000000000000  ", &iamclient.ResolveOrganizationResponse{})
	if !ok {
		t.Fatal("authorization probe fallback was not accepted")
	}
	if got != "org_00000000000000000000000000" {
		t.Fatalf("authorization probe fallback org id = %q", got)
	}
}

func TestAuthorizationProbeOrgIDRejectsProviderOrgFallback(t *testing.T) {
	if got, ok := authorizationProbeOrgID("42", &iamclient.ResolveOrganizationResponse{}); ok {
		t.Fatalf("provider fallback accepted as org id: %q", got)
	}
}

func TestAuthorizationProbeNotFoundClassifiesMissingOrganizationAsDenied(t *testing.T) {
	err := iamclient.AuthorizationError{
		StatusCode: 404,
		Body:       `{"code":"resource.not_found","detail":"organization not found"}`,
	}

	if !authorizationProbeNotFound(err) {
		t.Fatal("missing organization should classify as denied")
	}
}

func TestAuthorizationProbeNotFoundRejectsOtherAuthorizationErrors(t *testing.T) {
	err := iamclient.AuthorizationError{
		StatusCode: 404,
		Body:       `{"code":"resource.not_found","detail":"route not found"}`,
	}
	if authorizationProbeNotFound(err) {
		t.Fatal("route-level 404 should remain an authz error")
	}
	if authorizationProbeNotFound(fmt.Errorf("wrapped: %w", iamclient.AuthorizationError{StatusCode: 500})) {
		t.Fatal("5xx authorization failure should remain an authz error")
	}
}
