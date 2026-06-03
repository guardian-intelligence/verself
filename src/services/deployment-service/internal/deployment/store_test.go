package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/verself/deployment-service/migrations"
	"github.com/verself/service-runtime/pgtest"
)

func TestStoreDeduplicatesGitHubRunAttempt(t *testing.T) {
	ctx, store := newTestStore(t)
	source := Source{
		Kind:       SourceGitHubActionsOIDC,
		Repository: "guardian-intelligence/verself",
		RunID:      "12345",
		RunAttempt: 1,
	}
	record := deploymentRecord("dep_a", source)
	if err := store.InsertQueued(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertQueued(ctx, deploymentRecord("dep_b", source)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate insert error = %v, want %v", err, ErrDuplicate)
	}
	found, ok, err := store.FindBySourceAttempt(ctx, "gamma", source)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("source attempt was not found")
	}
	if found.DeploymentID != record.DeploymentID {
		t.Fatalf("deployment id = %q, want %q", found.DeploymentID, record.DeploymentID)
	}
}

func TestServiceSubmitReturnsExistingGitHubAttemptBeforeReadiness(t *testing.T) {
	ctx, store := newTestStore(t)
	source := githubSource()
	record := deploymentRecord("dep_existing", source)
	if err := store.InsertQueued(ctx, record); err != nil {
		t.Fatal(err)
	}
	got, err := (&Service{
		Store:  store,
		Config: Config{Site: "gamma"},
	}).Submit(ctx, SubmitRequest{
		Site: "gamma",
		SHA:  record.SHA,
	}, source)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeploymentID != record.DeploymentID || got.DeployRunKey != record.DeployRunKey {
		t.Fatalf("deployment = %#v, want existing %#v", got, record)
	}
	events, err := store.ListEvents(ctx, record.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Code != "deployment.queued" {
		t.Fatalf("events = %#v, want only original queued event", events)
	}
}

func TestServiceSubmitRejectsGitHubAttemptSHAMismatchBeforeReadiness(t *testing.T) {
	ctx, store := newTestStore(t)
	source := githubSource()
	record := deploymentRecord("dep_existing", source)
	if err := store.InsertQueued(ctx, record); err != nil {
		t.Fatal(err)
	}
	source.SHA = "fedcba9876543210fedcba9876543210fedcba98"
	_, err := (&Service{
		Store:  store,
		Config: Config{Site: "gamma"},
	}).Submit(ctx, SubmitRequest{
		Site: "gamma",
		SHA:  source.SHA,
	}, source)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("error = %v, want duplicate", err)
	}
	if ErrorCode(err) != "deployment.source_attempt_sha_mismatch" {
		t.Fatalf("error code = %q, want deployment.source_attempt_sha_mismatch", ErrorCode(err))
	}
	events, err := store.ListEvents(ctx, record.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Code != "deployment.queued" {
		t.Fatalf("events = %#v, want only original queued event", events)
	}
}

func TestWriteErrorMapsDuplicateToConflict(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deployments", nil)
	writeError(rec, req, ErrDuplicate)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	var problem problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "deployment.duplicate" {
		t.Fatalf("code = %q, want deployment.duplicate", problem.Code)
	}
}

func TestStoreRejectsSecondActiveDeploymentForSite(t *testing.T) {
	ctx, store := newTestStore(t)
	first := deploymentRecord("dep_first", Source{})
	if err := store.InsertQueued(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := deploymentRecord("dep_second", Source{})
	if err := store.InsertQueued(ctx, second); !errors.Is(err, ErrBusy) {
		t.Fatalf("second active deployment error = %v, want %v", err, ErrBusy)
	}
	otherSite := deploymentRecord("dep_other_site", Source{})
	otherSite.Site = "prod"
	if err := store.InsertQueued(ctx, otherSite); err != nil {
		t.Fatalf("different site active deployment rejected: %v", err)
	}
	if err := store.MarkFailed(ctx, first.DeploymentID, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	third := deploymentRecord("dep_third", Source{})
	if err := store.InsertQueued(ctx, third); err != nil {
		t.Fatalf("new deployment after terminal state rejected: %v", err)
	}
}

func TestStoreMarksInterruptedDeploymentsFailed(t *testing.T) {
	ctx, store := newTestStore(t)
	queued := deploymentRecord("dep_queued", Source{})
	if err := store.InsertQueued(ctx, queued); err != nil {
		t.Fatal(err)
	}
	other := deploymentRecord("dep_other_site", Source{})
	other.Site = "prod"
	if err := store.InsertQueued(ctx, other); err != nil {
		t.Fatal(err)
	}
	count, err := store.MarkInterrupted(ctx, "gamma", "service restart")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("interrupted count = %d, want 1", count)
	}
	assertInterrupted(t, ctx, store, queued.DeploymentID)
	running := deploymentRecord("dep_running", Source{})
	if err := store.InsertQueued(ctx, running); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRunning(ctx, running.DeploymentID); err != nil {
		t.Fatal(err)
	}
	count, err = store.MarkInterrupted(ctx, "gamma", "service restart")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("running interrupted count = %d, want 1", count)
	}
	assertInterrupted(t, ctx, store, running.DeploymentID)
	otherRecord, err := store.Get(ctx, other.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if otherRecord.State != StateQueued {
		t.Fatalf("other site state = %s, want queued", otherRecord.State)
	}
	count, err = store.MarkInterrupted(ctx, "gamma", "service restart")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("second interrupted count = %d, want 0", count)
	}
}

func assertInterrupted(t *testing.T, ctx context.Context, store Store, deploymentID string) {
	t.Helper()
	record, err := store.Get(ctx, deploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != StateFailed || record.ErrorCode != "deployment.interrupted" {
		t.Fatalf("%s state/code = %s/%s, want failed/deployment.interrupted", deploymentID, record.State, record.ErrorCode)
	}
	events, err := store.ListEvents(ctx, deploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-1].Code != "deployment.interrupted" {
		t.Fatalf("%s last event = %s, want deployment.interrupted", deploymentID, events[len(events)-1].Code)
	}
}

func TestStoreRejectsBlankInterruptedSite(t *testing.T) {
	ctx, store := newTestStore(t)
	record := deploymentRecord("dep_queued", Source{})
	if err := store.InsertQueued(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkInterrupted(ctx, " ", "service restart"); err == nil {
		t.Fatal("blank site interrupted every deployment")
	}
	unchanged, err := store.Get(ctx, record.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.State != StateQueued {
		t.Fatalf("state = %s, want queued", unchanged.State)
	}
}

func TestStoreStateChangesAppendOrderedEvents(t *testing.T) {
	ctx, store := newTestStore(t)
	record := deploymentRecord("dep_events", Source{})
	if err := store.InsertQueued(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRunning(ctx, record.DeploymentID); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSucceeded(ctx, record.DeploymentID, DeploymentResult{
		NomadSubmittedJobs: 2,
	}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, record.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	codes := make([]string, 0, len(events))
	for _, event := range events {
		codes = append(codes, event.Code)
	}
	want := []string{"deployment.queued", "deployment.running", "deployment.succeeded"}
	if len(codes) != len(want) {
		t.Fatalf("event codes = %#v, want %#v", codes, want)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("event codes = %#v, want %#v", codes, want)
		}
	}
	current, err := store.Get(ctx, record.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != StateSucceeded ||
		current.NomadSubmittedJobs != 2 {
		t.Fatalf("record = %#v, want succeeded deployment result", current)
	}
}

func newTestStore(t *testing.T) (context.Context, Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	t.Setenv("VERSELF_PGTEST_ROOT", filepath.Join(os.TempDir(), "verself-pgtest-deployment-service"))
	db := pgtest.Acquire(ctx, t, pgtest.Template{
		Service:     ServiceName,
		Fingerprint: migrations.Fingerprint(),
		Migrate: func(ctx context.Context, dsn string) error {
			return migrations.UpDSN(ctx, ServiceName, dsn)
		},
	})
	pg, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("open deployment postgres: %v", err)
	}
	t.Cleanup(pg.Close)
	return ctx, Store{PG: pg}
}

func deploymentRecord(deploymentID string, source Source) Record {
	return Record{
		DeploymentID:       deploymentID,
		Site:               "gamma",
		SHA:                "0123456789abcdef0123456789abcdef01234567",
		DeployRunKey:       "deploy_" + deploymentID,
		SourceKind:         source.Kind,
		SourceSubject:      "repo:guardian-intelligence/verself:ref:refs/heads/main",
		Repository:         source.Repository,
		Ref:                "refs/heads/main",
		Workflow:           "Gamma Deploy",
		JobWorkflowRef:     "guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main",
		ProviderRunID:      source.RunID,
		ProviderRunAttempt: source.RunAttempt,
		State:              StateQueued,
	}
}

func githubSource() Source {
	return Source{
		Kind:           SourceGitHubActionsOIDC,
		Subject:        "repo:guardian-intelligence/verself:ref:refs/heads/main",
		Repository:     "guardian-intelligence/verself",
		Ref:            "refs/heads/main",
		SHA:            "0123456789abcdef0123456789abcdef01234567",
		RunID:          "12345",
		RunAttempt:     1,
		Workflow:       "Gamma Deploy",
		JobWorkflowRef: "guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main",
	}
}
