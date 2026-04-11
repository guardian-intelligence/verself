package e2e_test

import (
	"context"
	"database/sql"
	"errors"
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

	env := startSandboxE2EEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
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
		WindowState:    "reserved",
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

	env := startSandboxE2EEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
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
		Kind:           "direct",
		ExecutionState: "finalizing",
		AttemptState:   "finalizing",
		Reservation:    reservation,
		WindowState:    "reserved",
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

	assertBillingWindow(t, env.ctx, env.pg.rentalDB, attemptID.String())

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

func TestReconcile_FinalizingAttemptSettleFailureVoidsWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startSandboxE2EEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	executionID := uuid.New()
	attemptID := uuid.New()
	balanceBefore, _, err := env.billingServer.GetBalance(env.ctx, testOrgID)
	if err != nil {
		t.Fatalf("balance before reserve: %v", err)
	}
	reservation, err := env.billingClient.Reserve(
		env.ctx,
		9995,
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
	env.rentalServer.SetBillingClient(&settleFailingBillingClient{
		inner:           env.billingClient,
		failedSourceRef: attemptID.String(),
	})

	startedAt := time.Now().UTC().Add(-time.Second)
	completedAt := time.Now().UTC()
	if err := insertReconcileAttemptRows(env.ctx, env.pg.rentalDB, executionID, attemptID, reconcileInsertSpec{
		Kind:           "direct",
		ExecutionState: "finalizing",
		AttemptState:   "finalizing",
		Reservation:    reservation,
		WindowState:    "reserved",
		UpdatedAt:      completedAt,
		StartedAt:      &startedAt,
		CompletedAt:    &completedAt,
		DurationMs:     1000,
		ExitCode:       0,
	}); err != nil {
		t.Fatalf("insert settle-failure reconciliation rows: %v", err)
	}

	if err := env.rentalServer.Reconcile(env.ctx); err != nil {
		t.Fatalf("reconcile settle-failure attempt: %v", err)
	}

	var executionState, attemptState, windowState, failureReason string
	if err := env.pg.rentalDB.QueryRowContext(env.ctx, `
		SELECT e.status, a.state, w.state, a.failure_reason
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN execution_billing_windows w ON w.attempt_id = a.attempt_id AND w.window_seq = 0
		WHERE e.execution_id = $1
	`, executionID).Scan(&executionState, &attemptState, &windowState, &failureReason); err != nil {
		t.Fatalf("query reconciled settle-failure attempt: %v", err)
	}
	if executionState != "failed" || attemptState != "failed" || windowState != "voided" {
		t.Fatalf("unexpected settle-failure reconciliation state: execution=%s attempt=%s window=%s", executionState, attemptState, windowState)
	}
	if failureReason != "billing_settle_failed" {
		t.Fatalf("expected failure_reason=billing_settle_failed, got %q", failureReason)
	}

	balanceAfter, _, err := env.billingServer.GetBalance(env.ctx, testOrgID)
	if err != nil {
		t.Fatalf("balance after reconcile: %v", err)
	}
	if balanceAfter != balanceBefore {
		t.Fatalf("expected settle-failure void to restore balance to %d, got %d", balanceBefore, balanceAfter)
	}

	flushBillingMetering(t, env.ctx, env.billingServer)
	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := env.queryCHConn.QueryRow(env.ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND source_ref = $2",
		orgIDStr, attemptID.String(),
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query settle-failure metering count: %v", err)
	}
	if meteringCount != 0 {
		t.Fatalf("expected 0 metering rows for settle-failure voided attempt, got %d", meteringCount)
	}
}

func TestReconcile_FinalizingSettledAttemptMarksTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startSandboxE2EEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	billingHTTPClient, err := billingclient.New(env.billingServer.URL)
	if err != nil {
		t.Fatalf("create billing client: %v", err)
	}

	executionID := uuid.New()
	attemptID := uuid.New()
	reservation, err := billingHTTPClient.Reserve(
		env.ctx,
		9993,
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
		Kind:           "direct",
		ExecutionState: "finalizing",
		AttemptState:   "finalizing",
		Reservation:    reservation,
		WindowState:    "settled",
		UpdatedAt:      completedAt,
		StartedAt:      &startedAt,
		CompletedAt:    &completedAt,
		DurationMs:     1000,
		ExitCode:       0,
	}); err != nil {
		t.Fatalf("insert settled finalizing rows: %v", err)
	}

	if err := env.rentalServer.Reconcile(env.ctx); err != nil {
		t.Fatalf("reconcile settled finalizing attempt: %v", err)
	}

	var executionState, attemptState, windowState string
	if err := env.pg.rentalDB.QueryRowContext(env.ctx, `
		SELECT e.status, a.state, w.state
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN execution_billing_windows w ON w.attempt_id = a.attempt_id AND w.window_seq = 0
		WHERE e.execution_id = $1
	`, executionID).Scan(&executionState, &attemptState, &windowState); err != nil {
		t.Fatalf("query reconciled settled attempt: %v", err)
	}
	if executionState != "succeeded" || attemptState != "succeeded" || windowState != "settled" {
		t.Fatalf("unexpected settled reconciliation state: execution=%s attempt=%s window=%s", executionState, attemptState, windowState)
	}
}

func TestReconcile_FinalizingVoidedAttemptMarksFailed(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startSandboxE2EEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	billingHTTPClient, err := billingclient.New(env.billingServer.URL)
	if err != nil {
		t.Fatalf("create billing client: %v", err)
	}

	executionID := uuid.New()
	attemptID := uuid.New()
	reservation, err := billingHTTPClient.Reserve(
		env.ctx,
		9994,
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
		Kind:           "direct",
		ExecutionState: "finalizing",
		AttemptState:   "finalizing",
		Reservation:    reservation,
		WindowState:    "voided",
		UpdatedAt:      completedAt,
		StartedAt:      &startedAt,
		CompletedAt:    &completedAt,
		DurationMs:     1000,
		ExitCode:       0,
		FailureReason:  "billing_settle_failed",
	}); err != nil {
		t.Fatalf("insert voided finalizing rows: %v", err)
	}

	if err := env.rentalServer.Reconcile(env.ctx); err != nil {
		t.Fatalf("reconcile voided finalizing attempt: %v", err)
	}

	var executionState, attemptState, windowState, failureReason string
	if err := env.pg.rentalDB.QueryRowContext(env.ctx, `
		SELECT e.status, a.state, w.state, a.failure_reason
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		JOIN execution_billing_windows w ON w.attempt_id = a.attempt_id AND w.window_seq = 0
		WHERE e.execution_id = $1
	`, executionID).Scan(&executionState, &attemptState, &windowState, &failureReason); err != nil {
		t.Fatalf("query reconciled voided attempt: %v", err)
	}
	if executionState != "failed" || attemptState != "failed" || windowState != "voided" {
		t.Fatalf("unexpected voided reconciliation state: execution=%s attempt=%s window=%s", executionState, attemptState, windowState)
	}
	if failureReason != "billing_settle_failed" {
		t.Fatalf("expected failure_reason=billing_settle_failed, got %q", failureReason)
	}
}

type reconcileInsertSpec struct {
	Kind           string
	ExecutionState string
	AttemptState   string
	Reservation    billingclient.Reservation
	WindowState    string
	UpdatedAt      time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	DurationMs     int64
	ExitCode       int
	FailureReason  string
}

type settleFailingBillingClient struct {
	inner           *billingclient.ServiceClient
	failedSourceRef string
}

func (c *settleFailingBillingClient) Reserve(
	ctx context.Context,
	jobID int64,
	orgID uint64,
	productID string,
	actorID string,
	concurrentCount uint64,
	sourceType string,
	sourceRef string,
	allocation map[string]float64,
	reqEditors ...billingclient.RequestEditorFn,
) (billingclient.Reservation, error) {
	return c.inner.Reserve(
		ctx,
		jobID,
		orgID,
		productID,
		actorID,
		concurrentCount,
		sourceType,
		sourceRef,
		allocation,
		reqEditors...,
	)
}

func (c *settleFailingBillingClient) Settle(
	ctx context.Context,
	reservation billingclient.Reservation,
	actualSeconds uint32,
	reqEditors ...billingclient.RequestEditorFn,
) error {
	if reservation.SourceRef == c.failedSourceRef {
		return errors.New("forced settle failure")
	}
	return c.inner.Settle(ctx, reservation, actualSeconds, reqEditors...)
}

func (c *settleFailingBillingClient) Void(
	ctx context.Context,
	reservation billingclient.Reservation,
	reqEditors ...billingclient.RequestEditorFn,
) error {
	return c.inner.Void(ctx, reservation, reqEditors...)
}

func insertReconcileAttemptRows(ctx context.Context, db *sql.DB, executionID, attemptID uuid.UUID, spec reconcileInsertSpec) error {
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
			attempt_id, execution_id, attempt_seq, state, billing_job_id, failure_reason,
			exit_code, duration_ms, started_at, completed_at, created_at, updated_at
		) VALUES ($1, $2, 1, $3, $4, $5, $6, $7, $8, $9, $10, $10)
	`, attemptID, executionID, spec.AttemptState, spec.Reservation.JobId, spec.FailureReason, spec.ExitCode, spec.DurationMs, spec.StartedAt, spec.CompletedAt, spec.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO execution_billing_windows (
			attempt_id, billing_window_id, window_seq, reservation_shape,
			reserved_quantity, pricing_phase, state, window_start, created_at
		) VALUES ($1, $2, 0, $3, $4, $5, $6, $7, $8)
	`, attemptID, spec.Reservation.WindowId, spec.Reservation.ReservationShape, spec.Reservation.WindowSecs, spec.Reservation.PricingPhase, spec.WindowState, spec.Reservation.WindowStart, spec.UpdatedAt); err != nil {
		return err
	}

	return tx.Commit()
}
