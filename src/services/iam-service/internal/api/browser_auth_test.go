package api

import (
	"testing"
	"time"

	identitystore "github.com/verself/iam-service/internal/store"
)

func TestVerifySelectedOrganizationClaimsAllowsNoOrganizationContext(t *testing.T) {
	claims := map[string]any{"urn:zitadel:iam:org:id": "provider-org-1"}

	if err := verifySelectedOrganizationClaims(claims, ""); err != nil {
		t.Fatalf("verifySelectedOrganizationClaims with no selected org: %v", err)
	}
}

func TestSnapshotForSessionAllowsSignedInUserWithoutOrganization(t *testing.T) {
	now := time.Now().UTC()
	session := &browserSession{
		SessionHash:          "hash",
		SessionHandle:        "bs_handle",
		ClientCachePartition: "partition",
		AccessToken:          "access-token",
		ExpiresAt:            now.Add(time.Hour),
		LastSeenAt:           now,
		CreatedAt:            now,
		UpdatedAt:            now,
		User: browserUser{
			Sub: "user-1",
		},
	}

	snapshot := snapshotForSession(session)
	if !snapshot.IsSignedIn || !snapshot.Auth.IsAuthenticated {
		t.Fatalf("snapshot should remain signed in: %#v", snapshot)
	}
	if snapshot.Auth.SelectedOrgID != nil || snapshot.Auth.OrgID != nil {
		t.Fatalf("snapshot should not invent organization context: %#v", snapshot.Auth)
	}
	if snapshot.User == nil || snapshot.User.SelectedOrgID != nil || snapshot.User.OrgID != nil {
		t.Fatalf("user snapshot should not invent organization context: %#v", snapshot.User)
	}
}

func TestBrowserLoginPromptAllowsAccountSelectionPromptOnly(t *testing.T) {
	if got := browserLoginPrompt("login"); got != "login" {
		t.Fatalf("browserLoginPrompt(login) = %q", got)
	}
	if got := browserLoginPrompt("none"); got != "" {
		t.Fatalf("browserLoginPrompt(none) = %q", got)
	}
	if got := browserLoginPrompt(" login "); got != "login" {
		t.Fatalf("browserLoginPrompt trims login = %q", got)
	}
}

func TestBrowserOrganizationContextsIgnoreMissingMetadata(t *testing.T) {
	contexts, missing := browserOrganizationContextsFromMetadata(
		[]string{"org_missing", "org_live"},
		[]identitystore.ListOrganizationMetadataByOrgIDsRow{{
			OrgID:                 "org_live",
			IdentityProviderOrgID: "provider-live",
		}},
	)

	if len(contexts) != 1 || contexts[0].OrgID != "org_live" || contexts[0].IdentityProviderOrgID != "provider-live" {
		t.Fatalf("unexpected contexts: %#v", contexts)
	}
	if len(missing) != 1 || missing[0] != "org_missing" {
		t.Fatalf("unexpected missing org ids: %#v", missing)
	}
}
