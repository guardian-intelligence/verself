package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Gap 1: OpRenew in the rapid state machine
// ---------------------------------------------------------------------------

var renewRapidRunCounter atomic.Uint64

// TestRenewInReservationLifecycleRapid exercises reserve→renew→settle
// sequences in rapid. Renew is the only operation that can transition
// pricing phases across windows (included→overage).
func TestRenewInReservationLifecycleRapid(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)

	rapid.Check(t, func(t *rapid.T) {
		runID := renewRapidRunCounter.Add(1)
		orgID := OrgID(8_500_000_000_000_000_000 + runID)
		productID, planID := uniqueCatalogIDs(fmt.Sprintf("renew-rapid-%d", runID))

		ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
			map[string]uint64{"unit": 1}, nil, false)

		grantCount := rapid.IntRange(1, 4).Draw(t, "grant_count")
		grantIDs := make([]GrantID, 0, grantCount)
		grantAmounts := make([]uint64, 0, grantCount)
		var total uint64
		for i := 0; i < grantCount; i++ {
			amount := rapid.Uint64Range(30, 180).Draw(t, fmt.Sprintf("grant_%d_amount", i))
			grant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
				orgID, productID, SourceSubscription, amount)
			grantIDs = append(grantIDs, grant.grantID)
			grantAmounts = append(grantAmounts, amount)
			total += amount
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		jobID := JobID(50000 + int64(runID))
		reservation, err := env.client.Reserve(ctx, ReserveRequest{
			JobID:      jobID,
			OrgID:      orgID,
			ProductID:  productID,
			ActorID:    "user-renew-rapid",
			Allocation: map[string]float64{"unit": 1},
			SourceType: "job",
			SourceRef:  strconv.FormatInt(int64(jobID), 10),
		})
		if total < 60 {
			if !errors.Is(err, ErrInsufficientBalance) {
				t.Fatalf("expected ErrInsufficientBalance, got %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}

		// Three-way draw: 0=void, 1=settle, 2=renew-then-settle
		action := rapid.IntRange(0, 2).Draw(t, "action")

		switch action {
		case 0: // Void
			if err := env.client.Void(ctx, &reservation); err != nil {
				t.Fatalf("void: %v", err)
			}
			for i, grantID := range grantIDs {
				requireGrantBalance(t, env.tbClient, grantID, grantAmounts[i], 0, 0)
			}

		case 1: // Settle
			actualSecs := uint32(rapid.IntRange(0, 60).Draw(t, "actual_seconds"))
			if err := env.client.Settle(ctx, &reservation, actualSecs); err != nil {
				t.Fatalf("settle: %v", err)
			}
			verifyTotalConsumed(t, env.tbClient, grantIDs, grantAmounts, uint64(actualSecs))

		case 2: // Renew then settle
			actualSecsW0 := uint32(rapid.IntRange(0, 60).Draw(t, "actual_seconds_w0"))
			err := env.client.Renew(ctx, &reservation, actualSecsW0)

			consumedW0 := uint64(actualSecsW0)
			if err != nil {
				// Renew settles w0 then may fail on w1 reservation.
				// w0's settle is committed regardless.
				if errors.Is(err, ErrInsufficientBalance) || errors.Is(err, ErrNoActiveSubscription) {
					verifyTotalConsumed(t, env.tbClient, grantIDs, grantAmounts, consumedW0)
					return
				}
				t.Fatalf("renew: %v", err)
			}

			if reservation.WindowSeq != 1 {
				t.Fatalf("expected window_seq=1, got %d", reservation.WindowSeq)
			}

			actualSecsW1 := uint32(rapid.IntRange(0, 60).Draw(t, "actual_seconds_w1"))
			if err := env.client.Settle(ctx, &reservation, actualSecsW1); err != nil {
				t.Fatalf("settle w1: %v", err)
			}

			totalConsumed := consumedW0 + uint64(actualSecsW1)
			verifyTotalConsumed(t, env.tbClient, grantIDs, grantAmounts, totalConsumed)
		}
	})
}

// verifyTotalConsumed checks that the sum of consumed across all grants equals
// expectedTotal, and that each grant's available + consumed = original amount
// (no pending after settle).
func verifyTotalConsumed(t fatalHelper, tbClient tb.Client, grantIDs []GrantID, grantAmounts []uint64, expectedTotal uint64) {
	t.Helper()

	accountIDs := make([]tbtypes.Uint128, len(grantIDs))
	for i, gid := range grantIDs {
		accountIDs[i] = GrantAccountID(gid).raw
	}

	accounts, err := tbClient.LookupAccounts(accountIDs)
	if err != nil {
		t.Fatalf("lookup accounts: %v", err)
	}
	if len(accounts) != len(grantIDs) {
		t.Fatalf("expected %d accounts, got %d", len(grantIDs), len(accounts))
	}

	byID := make(map[tbtypes.Uint128]tbtypes.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}

	var totalConsumed uint64
	for i, gid := range grantIDs {
		account := byID[GrantAccountID(gid).raw]

		avail, err := availableFromAccount(account)
		if err != nil {
			t.Fatalf("grant %x available: %v", gid, err)
		}
		pending, err := pendingFromAccount(account)
		if err != nil {
			t.Fatalf("grant %x pending: %v", gid, err)
		}
		consumed, err := consumedFromAccount(account)
		if err != nil {
			t.Fatalf("grant %x consumed: %v", gid, err)
		}

		// After settle, no pending should remain.
		if pending != 0 {
			t.Fatalf("grant %x: expected 0 pending, got %d", gid, pending)
		}
		// available + consumed = original amount.
		if avail+consumed != grantAmounts[i] {
			t.Fatalf("grant %x: avail(%d) + consumed(%d) != original(%d)",
				gid, avail, consumed, grantAmounts[i])
		}
		totalConsumed += consumed
	}

	if totalConsumed != expectedTotal {
		t.Fatalf("total consumed %d != expected %d", totalConsumed, expectedTotal)
	}
}

// ---------------------------------------------------------------------------
// Gap 2: Multi-product rapid test
// ---------------------------------------------------------------------------

var multiProductRunCounter atomic.Uint64

// TestMultiProductIsolationRapid verifies that no concurrent action sequence
// can cause a product to consume a grant belonging to a different product_id.
func TestMultiProductIsolationRapid(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)

	rapid.Check(t, func(t *rapid.T) {
		runID := multiProductRunCounter.Add(1)
		orgID := OrgID(8_600_000_000_000_000_000 + runID)

		productA, planA := uniqueCatalogIDs(fmt.Sprintf("multi-a-%d", runID))
		productB, planB := uniqueCatalogIDs(fmt.Sprintf("multi-b-%d", runID))

		ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productA, planA,
			map[string]uint64{"unit": 1}, nil, false)
		ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productB, planB,
			map[string]uint64{"unit": 2}, nil, false)

		// Seed grants for each product.
		amountA := rapid.Uint64Range(60, 200).Draw(t, "amount_a")
		amountB := rapid.Uint64Range(120, 400).Draw(t, "amount_b") // higher since unit cost is 2x
		grantA := seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
			orgID, productA, SourceSubscription, amountA)
		grantB := seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
			orgID, productB, SourceSubscription, amountB)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Reserve on product A.
		jobIDA := JobID(60000 + int64(runID)*2)
		resA, err := env.client.Reserve(ctx, ReserveRequest{
			JobID:      jobIDA,
			OrgID:      orgID,
			ProductID:  productA,
			ActorID:    "user-multi",
			Allocation: map[string]float64{"unit": 1},
			SourceType: "job",
			SourceRef:  strconv.FormatInt(int64(jobIDA), 10),
		})
		if err != nil {
			t.Fatalf("reserve A: %v", err)
		}

		// Reserve on product B.
		jobIDB := JobID(60000 + int64(runID)*2 + 1)
		resB, err := env.client.Reserve(ctx, ReserveRequest{
			JobID:      jobIDB,
			OrgID:      orgID,
			ProductID:  productB,
			ActorID:    "user-multi",
			Allocation: map[string]float64{"unit": 1},
			SourceType: "job",
			SourceRef:  strconv.FormatInt(int64(jobIDB), 10),
		})
		if err != nil {
			t.Fatalf("reserve B: %v", err)
		}

		// Settle both.
		actualA := uint32(rapid.IntRange(0, 60).Draw(t, "actual_a"))
		actualB := uint32(rapid.IntRange(0, 60).Draw(t, "actual_b"))

		if err := env.client.Settle(ctx, &resA, actualA); err != nil {
			t.Fatalf("settle A: %v", err)
		}
		if err := env.client.Settle(ctx, &resB, actualB); err != nil {
			t.Fatalf("settle B: %v", err)
		}

		// Invariant: product A's grant was only consumed by product A's
		// reservation, and product B's grant only by product B's.
		consumedA := uint64(actualA) * 1 // unit_rates={"unit":1}
		consumedB := uint64(actualB) * 2 // unit_rates={"unit":2}

		requireGrantBalance(t, env.tbClient, grantA.grantID,
			amountA-consumedA, 0, consumedA)
		requireGrantBalance(t, env.tbClient, grantB.grantID,
			amountB-consumedB, 0, consumedB)
	})
}

// ---------------------------------------------------------------------------
// Gap 3: Overage phase tests
// ---------------------------------------------------------------------------

// TestOveragePhaseSelection verifies that once subscription grants are
// depleted, the next reservation uses overage_unit_rates from prepaid grants.
func TestOveragePhaseSelection(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_700_000_000_000_000_001)
	productID, planID := uniqueCatalogIDs("overage-select")

	// Plan with both unit_rates and overage_unit_rates.
	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1},
		map[string]uint64{"unit": 3}, // overage costs 3x
		false,
	)

	// Seed a small subscription grant (will be fully consumed).
	subGrant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourceSubscription, 60)
	// Seed a prepaid grant for overage.
	prepaidGrant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourcePurchase, 500)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First reservation: should use included phase from subscription grant.
	res1, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      701,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-overage",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "701",
	})
	if err != nil {
		t.Fatalf("reserve1: %v", err)
	}
	if res1.PricingPhase != PricingPhaseIncluded {
		t.Fatalf("expected included phase, got %q", res1.PricingPhase)
	}
	if res1.CostPerSec != 1 {
		t.Fatalf("expected cost_per_sec=1, got %d", res1.CostPerSec)
	}

	// Settle the full window to consume the subscription grant entirely.
	if err := env.client.Settle(ctx, &res1, 60); err != nil {
		t.Fatalf("settle1: %v", err)
	}
	requireGrantBalance(t, env.tbClient, subGrant.grantID, 0, 0, 60)

	// Second reservation: subscription grant is depleted, should use overage
	// phase with prepaid grant at 3x rate.
	res2, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      702,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-overage",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "702",
	})
	if err != nil {
		t.Fatalf("reserve2: %v", err)
	}
	if res2.PricingPhase != PricingPhaseOverage {
		t.Fatalf("expected overage phase, got %q", res2.PricingPhase)
	}
	if res2.CostPerSec != 3 {
		t.Fatalf("expected cost_per_sec=3 (overage), got %d", res2.CostPerSec)
	}

	// Settle 30 seconds at 3 units/sec = 90 units from prepaid grant.
	if err := env.client.Settle(ctx, &res2, 30); err != nil {
		t.Fatalf("settle2: %v", err)
	}
	requireGrantBalance(t, env.tbClient, prepaidGrant.grantID, 500-90, 0, 90)
}

// TestOveragePhaseViaDefaultPlan verifies that when an org has no active
// subscription but has a default plan and prepaid grants, the overage phase
// uses the default plan's unit_rates.
func TestOveragePhaseViaDefaultPlan(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_700_000_000_000_000_002)
	productID, defaultPlanID := uniqueCatalogIDs("overage-default")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ensureMeteredProduct(t, env.pg, productID)
	if err := env.client.EnsureOrg(ctx, orgID, "overage-default-org"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	// Create a default plan (no subscription needed).
	unitRatesJSON, _ := json.Marshal(map[string]uint64{"unit": 2})
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, unit_rates, is_default, active)
		VALUES ($1, $2, $3, $4::jsonb, true, true)
		ON CONFLICT (plan_id) DO UPDATE
		SET unit_rates = EXCLUDED.unit_rates, is_default = EXCLUDED.is_default
	`, defaultPlanID, productID, "Default PAYG", string(unitRatesJSON)); err != nil {
		t.Fatalf("insert default plan: %v", err)
	}

	// No subscription — only prepaid grant.
	prepaidGrant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourcePurchase, 300)

	res, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      703,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-default-payg",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "703",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if res.PricingPhase != PricingPhaseOverage {
		t.Fatalf("expected overage phase, got %q", res.PricingPhase)
	}
	if res.CostPerSec != 2 {
		t.Fatalf("expected cost_per_sec=2 (default plan), got %d", res.CostPerSec)
	}

	if err := env.client.Settle(ctx, &res, 20); err != nil {
		t.Fatalf("settle: %v", err)
	}
	requireGrantBalance(t, env.tbClient, prepaidGrant.grantID, 300-40, 0, 40) // 20s * 2 = 40
}

// TestRenewIncludedToOverageTransition verifies that Renew correctly
// transitions from included phase to overage phase when subscription grants
// are exhausted during window 0.
func TestRenewIncludedToOverageTransition(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_700_000_000_000_000_003)
	productID, planID := uniqueCatalogIDs("renew-transition")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1},
		map[string]uint64{"unit": 2}, // overage
		false,
	)

	// Subscription grant: exactly 60 (one full window).
	subGrant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourceSubscription, 60)
	// Prepaid grant for overage.
	prepaidGrant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourcePurchase, 300)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Reserve window 0: included phase.
	res, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      704,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-transition",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "704",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if res.PricingPhase != PricingPhaseIncluded {
		t.Fatalf("w0: expected included, got %q", res.PricingPhase)
	}

	// Renew: settle 60s (full window), then reserve window 1.
	// Window 0 consumes all 60 subscription units.
	// Window 1 should be overage phase from prepaid grant at 2x rate.
	if err := env.client.Renew(ctx, &res, 60); err != nil {
		t.Fatalf("renew: %v", err)
	}

	if res.PricingPhase != PricingPhaseOverage {
		t.Fatalf("w1: expected overage, got %q", res.PricingPhase)
	}
	if res.CostPerSec != 2 {
		t.Fatalf("w1: expected cost_per_sec=2, got %d", res.CostPerSec)
	}
	if res.WindowSeq != 1 {
		t.Fatalf("expected window_seq=1, got %d", res.WindowSeq)
	}

	// Settle window 1: 30s at 2 units/sec = 60 from prepaid.
	if err := env.client.Settle(ctx, &res, 30); err != nil {
		t.Fatalf("settle w1: %v", err)
	}

	requireGrantBalance(t, env.tbClient, subGrant.grantID, 0, 0, 60)
	requireGrantBalance(t, env.tbClient, prepaidGrant.grantID, 300-60, 0, 60)
}

// TestInsufficientOverageWithNoOverageRates verifies that when subscription
// grants are exhausted and the plan has no overage_unit_rates, the reservation
// fails with ErrInsufficientBalance.
func TestInsufficientOverageWithNoOverageRates(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_700_000_000_000_000_004)
	productID, planID := uniqueCatalogIDs("no-overage")

	// Plan with no overage rates and no default plan.
	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1},
		nil, // no overage rates
		false,
	)

	// Only prepaid grants, no subscription grants.
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourcePurchase, 500)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Should fail: plan has no overage rates, no default plan, prepaid grants
	// can't be used without an overage rate card.
	_, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      705,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-no-overage",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "705",
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}
