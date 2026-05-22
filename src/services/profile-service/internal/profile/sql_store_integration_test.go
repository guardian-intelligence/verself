package profile

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/verself/profile-service/migrations"
	"github.com/verself/service-runtime/testpostgres"
)

type fakeIdentityWriter struct {
	email string
	now   time.Time
}

func (w fakeIdentityWriter) UpdateHumanProfile(_ context.Context, _ string, input UpdateIdentityRequest, _ string) (IdentitySummary, error) {
	return IdentitySummary{
		Email:       w.email,
		GivenName:   input.GivenName,
		FamilyName:  input.FamilyName,
		DisplayName: input.DisplayName,
		SyncedAt:    &w.now,
	}, nil
}

func TestSQLStoreLocalPostgresProfileLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cluster := testpostgres.Start(ctx, t, testpostgres.Options{Database: "profile_service_test"})
	t.Setenv("VERSELF_PG_DSN", cluster.DSN)
	if err := migrations.Up(ctx, "profile-service"); err != nil {
		t.Fatalf("migrate profile postgres: %v", err)
	}
	pg, err := pgxpool.New(ctx, cluster.DSN)
	if err != nil {
		t.Fatalf("open profile postgres: %v", err)
	}
	defer pg.Close()

	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	svc := Service{
		Store: SQLStore{
			PG:  pg,
			Now: func() time.Time { return now },
		},
		Identity: fakeIdentityWriter{email: "ada@example.com", now: now.Add(time.Second)},
	}
	principal := Principal{Subject: "subject_ada", OrgID: "org_platform", Email: "ada@example.com"}

	snapshot, err := svc.Snapshot(ctx, principal)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.SubjectID != principal.Subject || snapshot.OrgID != principal.OrgID {
		t.Fatalf("snapshot identity = (%s,%s), want (%s,%s)", snapshot.SubjectID, snapshot.OrgID, principal.Subject, principal.OrgID)
	}
	if snapshot.Preferences.Theme != DefaultTheme || snapshot.Preferences.Timezone != DefaultTimezone {
		t.Fatalf("default preferences = %#v", snapshot.Preferences)
	}

	snapshot, changed, err := svc.PutPreferences(ctx, principal, PutPreferencesRequest{
		Version:        0,
		Locale:         "en-US",
		Timezone:       "America/Los_Angeles",
		TimeDisplay:    TimeDisplayLocal,
		Theme:          ThemeDark,
		DefaultSurface: "profile",
	})
	if err != nil {
		t.Fatalf("put preferences: %v", err)
	}
	if snapshot.Preferences.Version != 1 || snapshot.Preferences.Theme != ThemeDark || snapshot.Preferences.Timezone != "America/Los_Angeles" {
		t.Fatalf("stored preferences = %#v", snapshot.Preferences)
	}
	for _, field := range []string{"timezone", "time_display", "theme", "default_surface"} {
		if !slices.Contains(changed, field) {
			t.Fatalf("changed fields %v missing %s", changed, field)
		}
	}

	snapshot, changed, err = svc.UpdateIdentity(ctx, principal, UpdateIdentityRequest{
		Version:     0,
		GivenName:   "Ada",
		FamilyName:  "Lovelace",
		DisplayName: "Ada Lovelace",
	}, "bearer-token")
	if err != nil {
		t.Fatalf("update identity: %v", err)
	}
	if snapshot.Identity.Version != 1 || snapshot.Identity.DisplayName != "Ada Lovelace" || snapshot.Identity.Email != "ada@example.com" {
		t.Fatalf("stored identity = %#v", snapshot.Identity)
	}
	for _, field := range []string{"given_name", "family_name", "display_name"} {
		if !slices.Contains(changed, field) {
			t.Fatalf("identity changed fields %v missing %s", changed, field)
		}
	}

	exported, err := svc.SubjectExport(ctx, DataRightsRequest{
		RequestID:   "profile-export-1",
		RequestedAt: now,
		RequestedBy: principal.Subject,
		SubjectID:   principal.Subject,
	})
	if err != nil {
		t.Fatalf("subject export: %v", err)
	}
	if exported.RecordCounts["subjects"] != "1" || exported.RecordCounts["preferences"] != "1" {
		t.Fatalf("subject export counts = %#v", exported.RecordCounts)
	}

	erased, err := svc.SubjectErasure(ctx, DataRightsRequest{
		RequestID:   "profile-erasure-1",
		RequestedAt: now,
		RequestedBy: principal.Subject,
		SubjectID:   principal.Subject,
	})
	if err != nil {
		t.Fatalf("subject erasure: %v", err)
	}
	if erased.RecordCounts["deleted_preferences"] != "1" || erased.RecordCounts["tombstoned_subjects"] != "1" {
		t.Fatalf("erasure counts = %#v", erased.RecordCounts)
	}
	if _, err := svc.DataRightsStatus(ctx, "profile-erasure-1"); err != nil {
		t.Fatalf("data rights status: %v", err)
	}

	var preferences int
	if err := pg.QueryRow(ctx, `SELECT count(*) FROM profile_preferences WHERE subject_id = $1`, principal.Subject).Scan(&preferences); err != nil {
		t.Fatalf("count profile preferences: %v", err)
	}
	if preferences != 0 {
		t.Fatalf("profile preferences after erasure = %d, want 0", preferences)
	}
	var tombstoned bool
	if err := pg.QueryRow(ctx, `SELECT tombstoned_at IS NOT NULL FROM profile_subjects WHERE subject_id = $1`, principal.Subject).Scan(&tombstoned); err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	if !tombstoned {
		t.Fatalf("profile subject was not tombstoned")
	}
	var outboxCount int
	if err := pg.QueryRow(ctx, `SELECT count(*) FROM profile_domain_event_outbox`).Scan(&outboxCount); err != nil {
		t.Fatalf("count profile outbox: %v", err)
	}
	if outboxCount != 5 {
		t.Fatalf("profile outbox events = %d, want 5", outboxCount)
	}
}
