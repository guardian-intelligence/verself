package main

import (
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

	got := authorizationProbeOrgID("371564185181576922", resp)
	if got != "org_B7HWGKW0SH7G4EXW9XT8TCT60C" {
		t.Fatalf("authorization probe org id = %q", got)
	}
}

func TestAuthorizationProbeOrgIDFallsBackWhenResolutionHasNoResult(t *testing.T) {
	got := authorizationProbeOrgID("  org_FALLBACK000000000000000000  ", &iamclient.ResolveOrganizationResponse{})
	if got != "org_FALLBACK000000000000000000" {
		t.Fatalf("authorization probe fallback org id = %q", got)
	}
}
