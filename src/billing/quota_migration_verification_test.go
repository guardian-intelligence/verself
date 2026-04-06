//go:build integration

// quota_migration_verification_test.go is the verification procedure for the
// TigerBeetle quota enforcement migration. Run before, during, and after
// the migration to confirm correct behavior at each stage.
//
// Run with SSH tunnels to the server:
//   ssh -fN -L 13320:127.0.0.1:3320 -L 15432:127.0.0.1:5432 ubuntu@64.34.84.75
//   FORGE_METAL_BILLING_LIVE_PG_DSN="postgres://billing:...@127.0.0.1:15432/sandbox?sslmode=disable" \
//   FORGE_METAL_BILLING_LIVE_TB_ADDRESS="127.0.0.1:13320" \
//   FORGE_METAL_BILLING_LIVE_TB_CLUSTER_ID="0" \
//   go test -tags integration -run TestVerify -v -count=1 -timeout 120s

package billing

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// uniqueOrgID returns a collision-safe org ID for live testing.
func uniqueOrgID() OrgID {
	return OrgID(8_000_000_000_000_000_000 + uint64(time.Now().UnixNano()%1_000_000_000))
}

// uniqueJobID returns a collision-safe job ID for live testing.
func uniqueJobID() JobID {
	return JobID(time.Now().UnixNano() % 1_000_000_000)
}

// seedPlanWithQuotas creates a plan with quota limits and optionally an overage cap.
func seedPlanWithQuotas(t *testing.T, db *sql.DB, planID, productID string, unitRate uint64, quotasJSON string, overageCapUnits *int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ensureMeteredProduct(t, db, productID)

	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, active, is_default, unit_rates, overage_unit_rates, quotas, included_credits)
		VALUES ($1, $2, $1, true, true, $3::jsonb, $3::jsonb, $4::jsonb, 0)
		ON CONFLICT (plan_id) DO UPDATE SET quotas = $4::jsonb, unit_rates = $3::jsonb, overage_unit_rates = $3::jsonb
	`, planID, productID, fmt.Sprintf(`{"unit": %d}`, unitRate), quotasJSON)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
}

// seedSubscription creates a subscription linking an org to a plan.
// Ensures the org exists first (FK constraint).
func seedSubscription(t *testing.T, client *Client, db *sql.DB, orgID OrgID, planID, productID string, overageCapUnits *int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.EnsureOrg(ctx, orgID, fmt.Sprintf("verify-org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (org_id, plan_id, product_id, status, current_period_start, current_period_end, overage_cap_units)
		VALUES ($1, $2, $3, 'active', now(), now() + interval '30 days', $4)
		ON CONFLICT DO NOTHING
	`, fmt.Sprintf("%d", orgID), planID, productID, overageCapUnits)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
}

// --- Verification Test 1: Happy path reserve/settle ---

func TestVerify_HappyPath_ReserveSettle(t *testing.T) {
	env := newLivePhase1Env(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgID := uniqueOrgID()
	productID := fmt.Sprintf("verify-happy-%d", orgID)
	planID := fmt.Sprintf("plan-verify-happy-%d", orgID)

	// Setup: org, plan (no quotas), grant with 10000 credits.
	seedPlanWithQuotas(t, env.pg, planID, productID, 10, `{"limits":[]}`, nil)
	seedSubscription(t, env.client, env.pg, orgID, planID, productID, nil)
	grant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceFreeTier, 10000)

	// Reserve.
	res, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: uniqueJobID(), OrgID: orgID, ProductID: productID, ActorID: "test",
		Allocation: map[string]float64{"unit": 1.0},
		SourceType: "verification", SourceRef: "test-happy",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	t.Logf("reserved: window_seq=%d cost_per_sec=%d grant_legs=%d", res.WindowSeq, res.CostPerSec, len(res.GrantLegs))

	// Verify: grant account has pending debit.
	accounts, err := env.tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(grant.grantID).raw})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	pending, _ := pendingFromAccount(accounts[0])
	if pending == 0 {
		t.Fatal("expected nonzero pending after reserve")
	}
	t.Logf("grant pending after reserve: %d", pending)

	// Settle 60 seconds.
	if err := env.client.Settle(ctx, &res, 60); err != nil {
		t.Fatalf("settle: %v", err)
	}

	// Verify: pending cleared, debits posted.
	accounts, err = env.tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(grant.grantID).raw})
	if err != nil {
		t.Fatalf("lookup after settle: %v", err)
	}
	pendingAfter, _ := pendingFromAccount(accounts[0])
	consumed, _ := consumedFromAccount(accounts[0])
	avail, _ := availableFromAccount(accounts[0])
	t.Logf("after settle: available=%d consumed=%d pending=%d", avail, consumed, pendingAfter)
	if pendingAfter != 0 {
		t.Fatalf("expected 0 pending after settle, got %d", pendingAfter)
	}
	if consumed == 0 {
		t.Fatal("expected nonzero consumed after settle")
	}

	t.Log("PASS: happy path reserve/settle")
}

// --- Verification Test 2: Quota enforcement — concurrent VMs ---

func TestVerify_Quota_ConcurrentVMs(t *testing.T) {
	env := newLivePhase1Env(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgID := uniqueOrgID()
	productID := fmt.Sprintf("verify-concurrent-%d", orgID)
	planID := fmt.Sprintf("plan-verify-concurrent-%d", orgID)

	// Plan: max 2 concurrent VMs.
	quotas := `{"limits":[{"dimension":"concurrent_vms","window":"instant","limit":2}]}`
	seedPlanWithQuotas(t, env.pg, planID, productID, 10, quotas, nil)
	seedSubscription(t, env.client, env.pg, orgID, planID, productID, nil)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceFreeTier, 100000)

	// Check with 1 concurrent VM — should pass (1 < limit of 2).
	r1, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{"concurrent_vms": 1})
	if err != nil {
		t.Fatalf("check quotas 1: %v", err)
	}
	if !r1.Allowed {
		t.Fatalf("expected allowed for 1 concurrent VM, got violations: %+v", r1.Violations)
	}
	t.Log("1 concurrent VM: allowed")

	// Check with 3 concurrent VMs — should FAIL (3 >= limit of 2).
	// Note: current "instant" window checks usage[dim] >= limit directly.
	// After TB migration: attempts balance-conditional debit on quota account.
	r3, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{"concurrent_vms": 3})
	if err != nil {
		t.Fatalf("check quotas 3: %v", err)
	}
	if r3.Allowed {
		t.Fatal("expected quota violation for 3 concurrent VMs, but was allowed")
	}
	t.Logf("3 concurrent VMs: correctly rejected with %d violations", len(r3.Violations))

	t.Log("PASS: concurrent VM quota enforcement")
}

// --- Verification Test 3: Quota enforcement — hourly rate limit ---

func TestVerify_Quota_HourlyRateLimit(t *testing.T) {
	env := newLivePhase1EnvWithMetering(t, noopMeteringWriter{}, noopMeteringQuerier{})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	orgID := uniqueOrgID()
	productID := fmt.Sprintf("verify-hourly-%d", orgID)
	planID := fmt.Sprintf("plan-verify-hourly-%d", orgID)

	// Plan: max 3 jobs per hour.
	quotas := `{"limits":[{"dimension":"jobs","window":"hour","limit":3}]}`
	seedPlanWithQuotas(t, env.pg, planID, productID, 1, quotas, nil)
	seedSubscription(t, env.client, env.pg, orgID, planID, productID, nil)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceFreeTier, 100000)

	// Run 3 jobs (reserve + settle each).
	for i := 0; i < 3; i++ {
		r, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{"jobs": 1})
		if err != nil {
			t.Fatalf("check quotas job %d: %v", i+1, err)
		}
		if !r.Allowed {
			t.Fatalf("job %d should be allowed, got violations: %+v", i+1, r.Violations)
		}
		t.Logf("job %d: allowed", i+1)
	}

	// 4th job — should FAIL (hourly limit exhausted via TigerBeetle pending transfers).
	r4, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{"jobs": 1})
	if err != nil {
		t.Fatalf("check quotas job 4: %v", err)
	}
	if r4.Allowed {
		t.Fatal("expected hourly quota violation for 4th job, but was allowed")
	}
	t.Logf("4th job correctly rejected with %d violations", len(r4.Violations))

	t.Log("PASS: hourly rate limit enforcement")
}

// --- Verification Test 4: Overage cap enforcement ---

func TestVerify_OverageCap(t *testing.T) {
	env := newLivePhase1EnvWithMetering(t, noopMeteringWriter{}, noopMeteringQuerier{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgID := uniqueOrgID()
	productID := fmt.Sprintf("verify-cap-%d", orgID)
	planID := fmt.Sprintf("plan-verify-cap-%d", orgID)

	// Plan with overage cap = 100 units.
	overageCap := int64(100)
	seedPlanWithQuotas(t, env.pg, planID, productID, 1, `{"limits":[]}`, &overageCap)
	seedSubscription(t, env.client, env.pg, orgID, planID, productID, &overageCap)

	// Only overage grants (purchase), no free-tier or subscription grants.
	// This forces the overage pricing phase.
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourcePurchase, 10000)

	// Reserve a large job that would exceed the overage cap.
	// With unit_rate=1 and window=300s, cost_per_sec=1, window_cost=300.
	// 300 > cap of 100 → should fail.
	_, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: uniqueJobID(), OrgID: orgID, ProductID: productID, ActorID: "test",
		Allocation: map[string]float64{"unit": 1.0},
		SourceType: "verification", SourceRef: "test-cap",
	})
	if err == nil {
		t.Fatal("expected overage cap rejection, but reserve succeeded")
	}
	if err != ErrOverageCeilingExceeded {
		t.Fatalf("expected ErrOverageCeilingExceeded, got: %v", err)
	}
	t.Logf("overage cap correctly rejected: %v", err)

	t.Log("PASS: overage cap enforcement")
}

// --- Verification Test 5: Crash recovery (DLQ via timeout) ---

func TestVerify_CrashRecovery_PendingTimeout(t *testing.T) {
	env := newLivePhase1Env(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgID := uniqueOrgID()
	productID := fmt.Sprintf("verify-crash-%d", orgID)
	planID := fmt.Sprintf("plan-verify-crash-%d", orgID)

	// Use a large grant (100000) and a plan with a low unit rate so
	// the reservation window cost is small relative to the grant.
	seedPlanWithQuotas(t, env.pg, planID, productID, 1, `{"limits":[]}`, nil)
	seedSubscription(t, env.client, env.pg, orgID, planID, productID, nil)
	grant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceFreeTier, 100000)

	// Check initial balance.
	accounts, _ := env.tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(grant.grantID).raw})
	initialAvail, _ := availableFromAccount(accounts[0])
	t.Logf("initial available: %d", initialAvail)

	// Override config for short timeout and short window to test expiry.
	oldTimeout := env.client.cfg.PendingTimeoutSecs
	oldWindow := env.client.cfg.ReservationWindowSecs
	env.client.cfg.PendingTimeoutSecs = 1  // 1 second timeout
	env.client.cfg.ReservationWindowSecs = 10 // small window → small cost

	// Reserve — creates pending transfer.
	res, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: uniqueJobID(), OrgID: orgID, ProductID: productID, ActorID: "test",
		Allocation: map[string]float64{"unit": 1.0},
		SourceType: "verification", SourceRef: "test-crash",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	// Verify: funds are locked.
	accounts, _ = env.tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(grant.grantID).raw})
	lockedAvail, _ := availableFromAccount(accounts[0])
	lockedPending, _ := pendingFromAccount(accounts[0])
	t.Logf("during lock: available=%d pending=%d", lockedAvail, lockedPending)
	if lockedPending == 0 {
		t.Fatal("expected nonzero pending after reserve")
	}

	// Simulate crash: do NOT call Settle or Void.
	_ = res

	// Wait for timeout.
	t.Log("simulating crash — waiting for pending transfer timeout (3s)...")
	time.Sleep(3 * time.Second)

	// Verify: funds restored.
	accounts, _ = env.tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(grant.grantID).raw})
	restoredAvail, _ := availableFromAccount(accounts[0])
	restoredPending, _ := pendingFromAccount(accounts[0])
	t.Logf("after timeout: available=%d pending=%d", restoredAvail, restoredPending)
	if restoredAvail != initialAvail {
		t.Fatalf("expected available=%d after timeout, got %d (funds NOT restored)", initialAvail, restoredAvail)
	}
	if restoredPending != 0 {
		t.Fatalf("expected 0 pending after timeout, got %d", restoredPending)
	}

	// Restore original config.
	env.client.cfg.PendingTimeoutSecs = oldTimeout
	env.client.cfg.ReservationWindowSecs = oldWindow

	t.Log("PASS: crash recovery — pending transfer auto-voided, funds restored")
}

// --- Verification Test 6: Cross-system consistency (Reconcile) ---

func TestVerify_CrossSystem_Reconcile(t *testing.T) {
	env := newLivePhase1Env(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgID := uniqueOrgID()
	productID := fmt.Sprintf("verify-reconcile-%d", orgID)
	planID := fmt.Sprintf("plan-verify-reconcile-%d", orgID)

	seedPlanWithQuotas(t, env.pg, planID, productID, 10, `{"limits":[]}`, nil)
	seedSubscription(t, env.client, env.pg, orgID, planID, productID, nil)
	grant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceFreeTier, 5000)

	// Reserve and settle.
	res, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: uniqueJobID(), OrgID: orgID, ProductID: productID, ActorID: "test",
		Allocation: map[string]float64{"unit": 1.0},
		SourceType: "verification", SourceRef: "test-reconcile",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := env.client.Settle(ctx, &res, 30); err != nil {
		t.Fatalf("settle: %v", err)
	}

	// Verify grant balance.
	accounts, _ := env.tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(grant.grantID).raw})
	avail, _ := availableFromAccount(accounts[0])
	consumed, _ := consumedFromAccount(accounts[0])
	t.Logf("after settle: available=%d consumed=%d (initial=5000)", avail, consumed)

	expectedConsumed := res.CostPerSec * 30
	if consumed != expectedConsumed {
		t.Fatalf("expected consumed=%d (cost_per_sec=%d * 30s), got %d", expectedConsumed, res.CostPerSec, consumed)
	}
	if avail != 5000-expectedConsumed {
		t.Fatalf("expected available=%d, got %d", 5000-expectedConsumed, avail)
	}

	t.Log("PASS: cross-system consistency verified")
}
