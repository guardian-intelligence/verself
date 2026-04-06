package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v85"
	"pgregory.net/rapid"
)

var reservationRapidRunCounter atomic.Uint64

type phase2TestEnv struct {
	phase1TestEnv
	client *Client
}

func newPhase2TestEnv(t *testing.T) phase2TestEnv {
	t.Helper()

	return newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, noopMeteringQuerier{})
}

func newPhase2TestEnvWithMetering(t *testing.T, metering MeteringWriter, querier MeteringQuerier) phase2TestEnv {
	t.Helper()

	env := newPhase1TestEnv(t)

	cfg := DefaultConfig()
	cfg.ReservationWindowSecs = 60
	cfg.PendingTimeoutSecs = 600
	cfg.StripeSecretKey = "sk_test_placeholder"
	cfg.TigerBeetleAddresses = []string{env.tbAddress}
	cfg.TigerBeetleClusterID = env.clusterID

	client, err := NewClient(env.tbClient, env.pg, stripe.NewClient(cfg.StripeSecretKey), metering, querier, cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	return phase2TestEnv{
		phase1TestEnv: env,
		client:        client,
	}
}

func TestReserveDoesNotSplitAcrossPricingPhases(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_200_000_000_000_000_001)
	productID, planID := uniqueCatalogIDs("nosplit")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceFreeTier, 30)
	subscriptionGrant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 90)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      101,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-nosplit",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "101",
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}

	// Free-tier phase may not spill into subscription grants within the same window.
	requireGrantBalance(t, env.tbClient, subscriptionGrant.grantID, 90, 0, 0)
}

func TestReserveWaterfallsAcrossMultipleGrantAccounts(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_200_000_000_000_000_002)
	productID, planID := uniqueCatalogIDs("waterfall")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
	grantA := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 10)
	grantB := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 20)
	grantC := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 50)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reservation, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      102,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-waterfall",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "102",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	if reservation.PricingPhase != PricingPhaseIncluded {
		t.Fatalf("expected included pricing phase, got %q", reservation.PricingPhase)
	}
	if reservation.CostPerSec != 1 {
		t.Fatalf("expected cost_per_sec=1, got %d", reservation.CostPerSec)
	}
	if got, want := len(reservation.GrantLegs), 3; got != want {
		t.Fatalf("expected %d grant legs, got %d", want, got)
	}

	expected := []GrantLeg{
		{GrantID: grantA.grantID, TransferID: VMTransferID(102, 0, 0, KindReservation), Amount: 10, Source: SourceSubscription},
		{GrantID: grantB.grantID, TransferID: VMTransferID(102, 0, 1, KindReservation), Amount: 20, Source: SourceSubscription},
		{GrantID: grantC.grantID, TransferID: VMTransferID(102, 0, 2, KindReservation), Amount: 30, Source: SourceSubscription},
	}
	for i, want := range expected {
		if got := reservation.GrantLegs[i]; got != want {
			t.Fatalf("grant leg %d: expected %+v, got %+v", i, want, got)
		}
	}

	requireGrantBalance(t, env.tbClient, grantA.grantID, 0, 10, 0)
	requireGrantBalance(t, env.tbClient, grantB.grantID, 0, 20, 0)
	requireGrantBalance(t, env.tbClient, grantC.grantID, 20, 30, 0)
}

func TestSettleAndVoidOperatePerGrantLegAndAreIdempotent(t *testing.T) {
	t.Parallel()

	t.Run("settle", func(t *testing.T) {
		t.Parallel()

		env := newPhase2TestEnv(t)
		orgID := OrgID(8_200_000_000_000_000_003)
		productID, planID := uniqueCatalogIDs("settle")

		ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
		grantA := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 50)
		grantB := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 50)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		reservation, err := env.client.Reserve(ctx, ReserveRequest{
			JobID:      103,
			OrgID:      orgID,
			ProductID:  productID,
			ActorID:    "user-settle",
			Allocation: map[string]float64{"unit": 1},
			SourceType: "job",
			SourceRef:  "103",
		})
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}

		if err := env.client.Settle(ctx, &reservation, 55); err != nil {
			t.Fatalf("settle: %v", err)
		}
		if err := env.client.Settle(ctx, &reservation, 55); err != nil {
			t.Fatalf("settle idempotency: %v", err)
		}

		requireGrantBalance(t, env.tbClient, grantA.grantID, 0, 0, 50)
		requireGrantBalance(t, env.tbClient, grantB.grantID, 45, 0, 5)
	})

	t.Run("void", func(t *testing.T) {
		t.Parallel()

		env := newPhase2TestEnv(t)
		orgID := OrgID(8_200_000_000_000_000_004)
		productID, planID := uniqueCatalogIDs("void")

		ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
		grantA := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 50)
		grantB := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 50)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		reservation, err := env.client.Reserve(ctx, ReserveRequest{
			JobID:      104,
			OrgID:      orgID,
			ProductID:  productID,
			ActorID:    "user-void",
			Allocation: map[string]float64{"unit": 1},
			SourceType: "job",
			SourceRef:  "104",
		})
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}

		if err := env.client.Void(ctx, &reservation); err != nil {
			t.Fatalf("void: %v", err)
		}
		if err := env.client.Void(ctx, &reservation); err != nil {
			t.Fatalf("void idempotency: %v", err)
		}

		requireGrantBalance(t, env.tbClient, grantA.grantID, 50, 0, 0)
		requireGrantBalance(t, env.tbClient, grantB.grantID, 50, 0, 0)
	})
}

func TestRenewSettlesCurrentWindowAndReservesNext(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_200_000_000_000_000_005)
	productID, planID := uniqueCatalogIDs("renew")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
	grantA := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 60)
	grantB := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 60)
	grantC := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 60)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reservation, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      105,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-renew",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "105",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	firstWindowStart := reservation.WindowStart
	if err := env.client.Renew(ctx, &reservation, 60); err != nil {
		t.Fatalf("renew: %v", err)
	}

	if reservation.WindowSeq != 1 {
		t.Fatalf("expected window_seq=1, got %d", reservation.WindowSeq)
	}
	if reservation.WindowStart != firstWindowStart.Add(60*time.Second) {
		t.Fatalf("expected next window start %s, got %s", firstWindowStart.Add(60*time.Second), reservation.WindowStart)
	}
	if got, want := len(reservation.GrantLegs), 1; got != want {
		t.Fatalf("expected %d next-window leg, got %d", want, got)
	}
	if got, want := reservation.GrantLegs[0], (GrantLeg{GrantID: grantB.grantID, TransferID: VMTransferID(105, 1, 0, KindReservation), Amount: 60, Source: SourceSubscription}); got != want {
		t.Fatalf("unexpected renewed grant leg: expected %+v, got %+v", want, got)
	}

	requireGrantBalance(t, env.tbClient, grantA.grantID, 0, 0, 60)
	requireGrantBalance(t, env.tbClient, grantB.grantID, 0, 60, 0)
	requireGrantBalance(t, env.tbClient, grantC.grantID, 60, 0, 0)
}

func TestReservationLifecycleRapid(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)

	rapid.Check(t, func(t *rapid.T) {
		runID := reservationRapidRunCounter.Add(1)
		orgID := OrgID(8_300_000_000_000_000_000 + runID)
		productID, planID := uniqueCatalogIDs(fmt.Sprintf("rapid-%d", runID))

		ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)

		grantCount := rapid.IntRange(1, 4).Draw(t, "grant_count")
		grantIDs := make([]GrantID, 0, grantCount)
		grantAmounts := make([]uint64, 0, grantCount)
		var total uint64
		for i := 0; i < grantCount; i++ {
			amount := rapid.Uint64Range(1, 120).Draw(t, fmt.Sprintf("grant_%d_amount", i))
			grant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, amount)
			grantIDs = append(grantIDs, grant.grantID)
			grantAmounts = append(grantAmounts, amount)
			total += amount
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		jobID := JobID(5000 + runID)
		reservation, err := env.client.Reserve(ctx, ReserveRequest{
			JobID:      jobID,
			OrgID:      orgID,
			ProductID:  productID,
			ActorID:    "user-rapid",
			Allocation: map[string]float64{"unit": 1},
			SourceType: "job",
			SourceRef:  strconv.FormatInt(int64(jobID), 10),
		})
		if total < 60 {
			if !errors.Is(err, ErrInsufficientBalance) {
				t.Fatalf("expected ErrInsufficientBalance, got %v", err)
			}
			for i, grantID := range grantIDs {
				requireGrantBalance(t, env.tbClient, grantID, grantAmounts[i], 0, 0)
			}
			return
		}
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}

		if rapid.Bool().Draw(t, "void") {
			if err := env.client.Void(ctx, &reservation); err != nil {
				t.Fatalf("void: %v", err)
			}
			for i, grantID := range grantIDs {
				requireGrantBalance(t, env.tbClient, grantID, grantAmounts[i], 0, 0)
			}
			return
		}

		actualSeconds := uint32(rapid.IntRange(0, 60).Draw(t, "actual_seconds"))
		if err := env.client.Settle(ctx, &reservation, actualSeconds); err != nil {
			t.Fatalf("settle: %v", err)
		}

		remainingCost := uint64(actualSeconds)
		for i, grantID := range grantIDs {
			consumed := minUint64(grantAmounts[i], remainingCost)
			remainingCost -= consumed
			requireGrantBalance(t, env.tbClient, grantID, grantAmounts[i]-consumed, 0, consumed)
		}
	})
}

func ensureMeteredPlanForTest(t fatalHelper, client *Client, db *sql.DB, orgID OrgID, productID, planID string, unitRates, overageRates map[string]uint64, isDefault bool) int64 {
	t.Helper()

	ensureMeteredProduct(t, db, productID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org %d: %v", orgID, err)
	}

	unitRatesJSON, err := json.Marshal(unitRates)
	if err != nil {
		t.Fatalf("marshal unit rates: %v", err)
	}
	overageRatesJSON, err := json.Marshal(overageRates)
	if err != nil {
		t.Fatalf("marshal overage rates: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO plans (
			plan_id,
			product_id,
			display_name,
			included_credits,
			unit_rates,
			overage_unit_rates,
			is_default,
			active
		)
		VALUES ($1, $2, $3, 0, $4::jsonb, $5::jsonb, $6, true)
		ON CONFLICT (plan_id) DO UPDATE
		SET unit_rates = EXCLUDED.unit_rates,
		    overage_unit_rates = EXCLUDED.overage_unit_rates,
		    is_default = EXCLUDED.is_default,
		    active = EXCLUDED.active
	`, planID, productID, "Phase 2 Test Plan", string(unitRatesJSON), string(overageRatesJSON), isDefault); err != nil {
		t.Fatalf("insert plan %s: %v", planID, err)
	}

	var subscriptionID int64
	now := time.Now().UTC()
	if err := db.QueryRowContext(ctx, `
		INSERT INTO subscriptions (
			org_id,
			plan_id,
			product_id,
			cadence,
			current_period_start,
			current_period_end,
			status
		)
		VALUES ($1, $2, $3, 'monthly', $4, $5, 'active')
		RETURNING subscription_id
	`, strconv.FormatUint(uint64(orgID), 10), planID, productID, now, now.Add(30*24*time.Hour)).Scan(&subscriptionID); err != nil {
		t.Fatalf("insert subscription for %s: %v", planID, err)
	}

	return subscriptionID
}

func uniqueCatalogIDs(prefix string) (string, string) {
	now := time.Now().UTC().UnixNano()
	productID := fmt.Sprintf("billing-%s-%d", prefix, now)
	return productID, productID + "-plan"
}
