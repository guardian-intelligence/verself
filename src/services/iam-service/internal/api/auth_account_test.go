package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/contractapi"
	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/iam-service/migrations"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/pgtest"
)

func TestCreateDeviceSessionRequiresExplicitLinkForVerifiedEmailCollision(t *testing.T) {
	ctx, handlers := newAuthAccountTestHandlers(t)

	first := authIdentity("https://issuer.example", "subject-a", "abc@xyz.com", true)
	firstSession, err := handlers.CreateDeviceSession(auth.WithIdentity(ctx, first), createDeviceSessionInput("collision-first"))
	if err != nil {
		t.Fatalf("create first device session: %v", err)
	}
	if firstSession.Body.Account.AccountID == "" || firstSession.Body.Session.SessionID == "" {
		t.Fatalf("first auth context missing account/session: %#v", firstSession.Body)
	}

	second := authIdentity("https://issuer.example", "subject-b", "abc+signup@xyz.com", true)
	_, err = handlers.CreateDeviceSession(auth.WithIdentity(ctx, second), createDeviceSessionInput("collision-second"))
	assertProblem(t, err, http.StatusConflict, "auth.account_link_required")
}

func TestCreateDeviceSessionRejectsUnverifiedProviderEmail(t *testing.T) {
	ctx, handlers := newAuthAccountTestHandlers(t)

	identity := authIdentity("https://issuer.example", "subject-unverified", "abc@xyz.com", false)
	_, err := handlers.CreateDeviceSession(auth.WithIdentity(ctx, identity), createDeviceSessionInput("unverified-email"))
	assertProblem(t, err, http.StatusForbidden, "auth.provider_email_unverified")
}

func newAuthAccountTestHandlers(t *testing.T) (context.Context, publicHandlers) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
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
	t.Cleanup(pg.Close)
	return ctx, publicHandlers{
		service: &identity.Service{Store: identity.SQLStore{PG: pg}},
		authz:   authz.New(staticAuthzBackend{}),
	}
}

func createDeviceSessionInput(key string) *contractapi.CreateDeviceSessionInput {
	return &contractapi.CreateDeviceSessionInput{
		IdempotencyKey: contractapi.IdempotencyKey("test-" + key),
		Body: contractapi.CreateDeviceSessionInputBody{
			Channel:     contractapi.AuthChannel("cli"),
			DeviceLabel: contractapi.DisplayName("Test CLI"),
		},
	}
}

func authIdentity(issuer, subject, email string, emailVerified bool) *auth.Identity {
	return &auth.Identity{
		Subject: subject,
		Email:   email,
		Raw: map[string]any{
			"iss":            issuer,
			"sub":            subject,
			"email":          email,
			"email_verified": emailVerified,
			"name":           email,
			"iat":            float64(time.Now().UTC().Unix()),
		},
	}
}

func assertProblem(t *testing.T, err error, status int, code string) {
	t.Helper()
	var problem *verselfProblem
	if !errors.As(err, &problem) {
		t.Fatalf("error = %T %v, want verselfProblem", err, err)
	}
	if problem.Status != status || problem.Code != code {
		t.Fatalf("problem status/code = %d/%s, want %d/%s: %#v", problem.Status, problem.Code, status, code, problem)
	}
	if got, want := problem.Type, authProblemDocsURL+authProblemAnchor(code); got != want {
		t.Fatalf("problem type = %q, want %q", got, want)
	}
}
