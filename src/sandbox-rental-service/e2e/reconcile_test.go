package e2e_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	billingclient "github.com/forge-metal/billing-service/client"
	"github.com/google/uuid"
)

func TestReconcile_ReservedAttemptVoidsWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	billingHTTPClient, err := billingclient.New(env.billingServer.URL)
	if err != nil {
		t.Fatalf("create billing client: %v", err)
	}

	executionID := uuid.New()
	attemptID := uuid.New()
	balanceBefore, _, err := env.billingServer.GetBalance(env.ctx, testOrgID)
	if err != nil {
		t.Fatalf("balance before reserve: %v", err)
	}
	reservation, err := billingHTTPClient.Reserve(
		env.ctx,
		9991,
		testOrgID,
		"sandbox",
		testUserID,
		1,
		"execution_attempt",
		attemptID.String(),
		map[string]float64{"vcpu": 2, "gib": 2},
	)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := insertReconcileAttemptRows(env.ctx, env.pg.rentalDB, executionID, attemptID, reconcileInsertSpec{
		Kind:           "direct",
		ExecutionState: "reserved",
		AttemptState:   "reserved",
		Reservation:    reservation,
		UpdatedAt:      time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("insert reserved reconciliation rows: %v", err)
	}

	if err := env.rentalServer.Reconcile(env.ctx); err != nil {
		t.Fatalf("reconcile reserved attempt: %v", err)
	}

	var (
		executionState string
		attemptState   string
		windowState    string
	)
	if err := env.pg.rentalDB.QueryRowContext(env.ctx, `
		SELECT e.status, a.state, w.state
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN execution_billing_windows w ON w.attempt_id = a.attempt_id AND w.window_seq = 0
		WHERE e.execution_id = $1
	`, executionID).Scan(&executionState, &attemptState, &windowState); err != nil {
		t.Fatalf("query reconciled reserved attempt: %v", err)
	}
	if executionState != "failed" || attemptState != "failed" || windowState != "voided" {
		t.Fatalf("unexpected reserved reconciliation state: execution=%s attempt=%s window=%s", executionState, attemptState, windowState)
	}

	balanceAfter, _, err := env.billingServer.GetBalance(env.ctx, testOrgID)
	if err != nil {
		t.Fatalf("balance after reconcile: %v", err)
	}
	if balanceAfter != balanceBefore {
		t.Fatalf("expected void to restore balance to %d, got %d", balanceBefore, balanceAfter)
	}

	flushBillingMetering(t, env.ctx, env.billingServer)
	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := env.queryCHConn.QueryRow(env.ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND source_ref = $2",
		orgIDStr, attemptID.String(),
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query reserved metering count: %v", err)
	}
	if meteringCount != 0 {
		t.Fatalf("expected 0 metering rows for voided attempt, got %d", meteringCount)
	}
}

func TestReconcile_FinalizingAttemptSettlesWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	billingHTTPClient, err := billingclient.New(env.billingServer.URL)
	if err != nil {
		t.Fatalf("create billing client: %v", err)
	}

	executionID := uuid.New()
	attemptID := uuid.New()
	reservation, err := billingHTTPClient.Reserve(
		env.ctx,
		9992,
		testOrgID,
		"sandbox",
		testUserID,
		1,
		"execution_attempt",
		attemptID.String(),
		map[string]float64{"vcpu": 2, "gib": 2},
	)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	startedAt := time.Now().UTC().Add(-time.Second)
	completedAt := time.Now().UTC()
	if err := insertReconcileAttemptRows(env.ctx, env.pg.rentalDB, executionID, attemptID, reconcileInsertSpec{
		Kind:           "repo_exec",
		ExecutionState: "finalizing",
		AttemptState:   "finalizing",
		Reservation:    reservation,
		UpdatedAt:      completedAt,
		StartedAt:      &startedAt,
		CompletedAt:    &completedAt,
		DurationMs:     1000,
		ExitCode:       0,
	}); err != nil {
		t.Fatalf("insert finalizing reconciliation rows: %v", err)
	}

	if err := env.rentalServer.Reconcile(env.ctx); err != nil {
		t.Fatalf("reconcile finalizing attempt: %v", err)
	}

	assertWarmGoldenBillingWindow(t, env.ctx, env.pg.rentalDB, attemptID.String())

	var executionState string
	if err := env.pg.rentalDB.QueryRowContext(env.ctx, `
		SELECT status FROM executions WHERE execution_id = $1
	`, executionID).Scan(&executionState); err != nil {
		t.Fatalf("query reconciled finalizing execution: %v", err)
	}
	if executionState != "succeeded" {
		t.Fatalf("expected reconciled finalizing execution succeeded, got %q", executionState)
	}

	flushBillingMetering(t, env.ctx, env.billingServer)
	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := env.queryCHConn.QueryRow(env.ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND source_ref = $2",
		orgIDStr, attemptID.String(),
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query finalizing metering count: %v", err)
	}
	if meteringCount != 1 {
		t.Fatalf("expected 1 metering row for reconciled finalizing attempt, got %d", meteringCount)
	}
}

type reconcileInsertSpec struct {
	Kind           string
	ExecutionState string
	AttemptState   string
	Reservation    billingclient.Reservation
	UpdatedAt      time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	DurationMs     int64
	ExitCode       int
}

func insertReconcileAttemptRows(ctx context.Context, db *sql.DB, executionID, attemptID uuid.UUID, spec reconcileInsertSpec) error {
	reservationJSON, err := json.Marshal(spec.Reservation)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO executions (
			execution_id, org_id, actor_id, kind, provider, product_id, status,
			correlation_id, latest_attempt_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, '', 'sandbox', $5, '', $6, $7, $7)
	`, executionID, int64(testOrgID), testUserID, spec.Kind, spec.ExecutionState, attemptID, spec.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO execution_attempts (
			attempt_id, execution_id, attempt_seq, state, billing_job_id, exit_code,
			duration_ms, started_at, completed_at, created_at, updated_at
		) VALUES ($1, $2, 1, $3, $4, $5, $6, $7, $8, $9, $9)
	`, attemptID, executionID, spec.AttemptState, spec.Reservation.JobId, spec.ExitCode, spec.DurationMs, spec.StartedAt, spec.CompletedAt, spec.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO execution_billing_windows (
			attempt_id, window_seq, reservation, window_seconds, pricing_phase, state, created_at
		) VALUES ($1, 0, $2::jsonb, $3, $4, 'reserved', $5)
	`, attemptID, string(reservationJSON), spec.Reservation.WindowSecs, spec.Reservation.PricingPhase, spec.UpdatedAt); err != nil {
		return err
	}

	return tx.Commit()
}
