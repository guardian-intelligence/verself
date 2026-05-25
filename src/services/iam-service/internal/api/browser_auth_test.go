package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	identitystore "github.com/verself/iam-service/internal/store"
	"github.com/verself/iam-service/migrations"
	"github.com/verself/service-runtime/pgtest"
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
		ClientHash:           "client-hash",
		ClientHandle:         "bc_handle",
		AccountHandle:        "ba_handle",
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

func TestSyncSessionDoesNotObserveBrowserClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	t.Setenv("VERSELF_PGTEST_ROOT", filepath.Join(os.TempDir(), "verself-pgtest-iam-api"))

	db := pgtest.Acquire(ctx, t, pgtest.Template{
		Service:     "iam-service",
		Fingerprint: migrations.Fingerprint(),
		Migrate: func(ctx context.Context, dsn string) error {
			pg, err := pgxpool.New(ctx, dsn)
			if err != nil {
				return err
			}
			defer pg.Close()
			if _, err := pg.Exec(ctx, `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'iam_service') THEN CREATE ROLE iam_service; END IF; END $$;`); err != nil {
				return err
			}
			return migrations.UpDSN(ctx, "iam-service", dsn)
		},
	})
	pg, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("open iam postgres: %v", err)
	}
	defer pg.Close()

	q := identitystore.New(pg)
	clientSecret := "sync-client-secret"
	clientHash := hashToken(clientSecret)
	if err := q.UpsertBrowserClient(ctx, identitystore.UpsertBrowserClientParams{
		ClientHash:           clientHash,
		ClientHandle:         browserClientHandle(clientHash),
		ClientCachePartition: "partition",
		ExpiresAt:            timestamptz(time.Now().UTC().Add(time.Hour)),
		ClientIp:             "203.0.113.10",
		ClientIpTrusted:      true,
		ClientIpSource:       "test",
		EdgePeerIp:           "203.0.113.1",
		UserAgent:            "test-agent",
		DeviceLabel:          "Test Browser",
		DeviceKind:           "desktop",
		BrowserName:          "Test",
		OsName:               "Linux",
		GeoCountryCode:       "US",
		GeoRegion:            "CA",
		GeoCity:              "San Francisco",
	}); err != nil {
		t.Fatalf("seed browser client: %v", err)
	}
	seenAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := pg.Exec(ctx, `UPDATE iam_browser_clients SET last_seen_at = $1, updated_at = $1 WHERE client_hash = $2`, seenAt, clientHash); err != nil {
		t.Fatalf("seed seen timestamp: %v", err)
	}

	auth := &BrowserAuth{q: q}
	req := httptest.NewRequest(http.MethodGet, "/sync-session", nil)
	req.AddCookie(&http.Cookie{Name: browserAuthCookieName, Value: clientSecret})
	res := httptest.NewRecorder()
	auth.handleSyncSession(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("sync session status = %d, body = %q", res.Code, res.Body.String())
	}

	var after time.Time
	if err := pg.QueryRow(ctx, `SELECT last_seen_at FROM iam_browser_clients WHERE client_hash = $1`, clientHash).Scan(&after); err != nil {
		t.Fatalf("read seen timestamp: %v", err)
	}
	if !after.Equal(seenAt) {
		t.Fatalf("sync session moved last_seen_at: got %s want %s", after.Format(time.RFC3339Nano), seenAt.Format(time.RFC3339Nano))
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
	if got := browserLoginPrompt("select_account"); got != "select_account" {
		t.Fatalf("browserLoginPrompt(select_account) = %q", got)
	}
}

func TestSameEmailIdentityAllowsSubaddressAndIDNVariants(t *testing.T) {
	if !sameEmailIdentity("Founder+signup@Bücher.Example", "founder@xn--bcher-kva.example") {
		t.Fatal("expected plus-address and IDN variants to match")
	}
	if sameEmailIdentity("founder@example.test", "founder+tag@other.example") {
		t.Fatal("expected different domains to mismatch")
	}
	if sameEmailIdentity("Founder <founder@example.test>", "founder@example.test") {
		t.Fatal("expected display-name mailbox syntax to mismatch")
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
