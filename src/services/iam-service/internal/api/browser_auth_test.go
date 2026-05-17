package api

import (
	"testing"
	"time"
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
