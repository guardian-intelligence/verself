package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v85"
)

// stubMeteringQuerier returns canned values for test assertions.
type stubMeteringQuerier struct {
	dimensionSums map[string]float64 // key: "orgID:productID:dimension"
	err           error              // if set, all calls return this error
}

func (s *stubMeteringQuerier) SumDimension(_ context.Context, orgID OrgID, productID string, dimension string, _ time.Time) (float64, error) {
	if s.err != nil {
		return 0, s.err
	}
	key := fmt.Sprintf("%d:%s:%s", orgID, productID, dimension)
	return s.dimensionSums[key], nil
}

// ---------------------------------------------------------------------------
// CheckQuotas tests
// ---------------------------------------------------------------------------

func TestNewClientRejectsNilQuerier(t *testing.T) {
	t.Parallel()

	env := newPhase1TestEnv(t)

	cfg := DefaultConfig()
	cfg.ReservationWindowSecs = 60
	cfg.PendingTimeoutSecs = 600
	cfg.StripeSecretKey = "sk_test_placeholder"
	cfg.TigerBeetleAddresses = []string{env.tbAddress}
	cfg.TigerBeetleClusterID = env.clusterID

	// NewClient must reject nil querier at construction time (fail closed).
	_, err := NewClient(env.tbClient, env.pg, stripe.NewClient(cfg.StripeSecretKey), noopMeteringWriter{}, nil, cfg)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for nil querier, got %v", err)
	}
}

func TestCheckQuotasNoLimitsAllowed(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, &stubMeteringQuerier{})

	orgID := OrgID(8_800_000_000_000_000_002)
	productID, planID := uniqueCatalogIDs("quota-none")

	// Plan with empty quotas (default '{}').
	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := env.client.CheckQuotas(ctx, orgID, productID, nil)
	if err != nil {
		t.Fatalf("check quotas: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected Allowed=true with no quota limits, got violations: %+v", result.Violations)
	}
}

func TestCheckQuotasInstantWindowViolation(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, &stubMeteringQuerier{})

	orgID := OrgID(8_800_000_000_000_000_003)
	productID, planID := uniqueCatalogIDs("quota-instant")

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set quota: concurrent_vms instant limit 5.
	setQuotasOnPlan(t, env.pg, planID, quotaPolicy{Limits: []quotaLimit{
		{Dimension: "concurrent_vms", Window: "instant", Limit: 5},
	}})
	_ = subID

	// Usage above limit.
	result, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{
		"concurrent_vms": 6,
	})
	if err != nil {
		t.Fatalf("check quotas: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected quota violation for concurrent_vms, got Allowed=true")
	}
	if len(result.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result.Violations))
	}
	v := result.Violations[0]
	if v.Dimension != "concurrent_vms" || v.Window != "instant" || v.Limit != 5 {
		t.Fatalf("unexpected violation: %+v", v)
	}
	// Current is implementation-defined: TigerBeetle reports "at capacity" (== limit),
	// not the caller-supplied usage value. Assert >= limit rather than exact value.
	if v.Current < v.Limit {
		t.Fatalf("expected Current >= Limit, got Current=%d Limit=%d", v.Current, v.Limit)
	}
}

func TestCheckQuotasInstantWindowAllowed(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, &stubMeteringQuerier{})

	orgID := OrgID(8_800_000_000_000_000_004)
	productID, planID := uniqueCatalogIDs("quota-instant-ok")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	setQuotasOnPlan(t, env.pg, planID, quotaPolicy{Limits: []quotaLimit{
		{Dimension: "concurrent_vms", Window: "instant", Limit: 10},
	}})

	result, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{
		"concurrent_vms": 3,
	})
	if err != nil {
		t.Fatalf("check quotas: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected Allowed=true, got violations: %+v", result.Violations)
	}
}

func TestCheckQuotasRollingWindowViolation(t *testing.T) {
	t.Parallel()

	orgID := OrgID(8_800_000_000_000_000_005)
	productID, planID := uniqueCatalogIDs("quota-rolling")

	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, &stubMeteringQuerier{})

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Hourly quota: max 3 tokens per hour. Enforced via TigerBeetle pending
	// transfers with timeout=3600s (leaky bucket).
	setQuotasOnPlan(t, env.pg, planID, quotaPolicy{Limits: []quotaLimit{
		{Dimension: "token", Window: "hour", Limit: 3},
	}})

	// Consume 3 tokens (exhaust the hourly quota).
	for i := 0; i < 3; i++ {
		r, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{"token": 1})
		if err != nil {
			t.Fatalf("check quotas %d: %v", i+1, err)
		}
		if !r.Allowed {
			t.Fatalf("token %d should be allowed", i+1)
		}
	}

	// 4th token should be rejected — hourly quota exhausted.
	result, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{"token": 1})
	if err != nil {
		t.Fatalf("check quotas 4th: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected rolling window violation, got Allowed=true")
	}
	if len(result.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result.Violations))
	}
	v := result.Violations[0]
	if v.Dimension != "token" || v.Window != "hour" {
		t.Fatalf("unexpected violation: %+v", v)
	}
}

func TestCheckQuotasRollingWindowAllowed(t *testing.T) {
	t.Parallel()

	orgID := OrgID(8_800_000_000_000_000_006)
	productID, planID := uniqueCatalogIDs("quota-rolling-ok")

	stub := &stubMeteringQuerier{
		dimensionSums: map[string]float64{
			fmt.Sprintf("%d:%s:token", orgID, productID): 30000,
		},
	}
	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, stub)

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	setQuotasOnPlan(t, env.pg, planID, quotaPolicy{Limits: []quotaLimit{
		{Dimension: "token", Window: "hour", Limit: 50000},
	}})

	result, err := env.client.CheckQuotas(ctx, orgID, productID, nil)
	if err != nil {
		t.Fatalf("check quotas: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected Allowed=true, got violations: %+v", result.Violations)
	}
}

func TestCheckQuotasClickHouseErrorFailsClosed(t *testing.T) {
	t.Parallel()

	stub := &stubMeteringQuerier{err: fmt.Errorf("connection refused")}
	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, stub)

	orgID := OrgID(8_800_000_000_000_000_007)
	productID, planID := uniqueCatalogIDs("quota-ch-err")

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use "week" window — this is the ClickHouse-backed path (hour/4h/instant use TigerBeetle).
	// The week window requires a subscription with current_period_start for windowSince().
	setQuotasOnPlan(t, env.pg, planID, quotaPolicy{Limits: []quotaLimit{
		{Dimension: "token", Window: "week", Limit: 50000},
	}})
	_ = subID

	_, err := env.client.CheckQuotas(ctx, orgID, productID, nil)
	if err == nil {
		t.Fatal("expected error on ClickHouse failure, got nil")
	}
}

// ---------------------------------------------------------------------------
// Overage cap enforcement tests
// ---------------------------------------------------------------------------

func TestOverageCapExceeded(t *testing.T) {
	t.Parallel()

	orgID := OrgID(8_800_000_000_000_000_010)
	productID, planID := uniqueCatalogIDs("overage-cap-exceed")

	// Overage cap is now enforced via TigerBeetle balance-conditional transfers,
	// not ClickHouse queries. The metering querier is unused for this path.
	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, &stubMeteringQuerier{})

	// Plan with overage rates.
	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1},
		map[string]uint64{"unit": 3},
		false,
	)

	// Set overage cap on the subscription.
	setOverageCapOnSubscription(t, env.pg, subID, 100)

	// Deplete the subscription grant so we enter overage phase.
	subGrant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourceSubscription, 60)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourcePurchase, 500)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Consume the subscription grant.
	res, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: 801, OrgID: orgID, ProductID: productID, ActorID: "user-cap",
		Allocation: map[string]float64{"unit": 1}, SourceType: "job", SourceRef: "801",
	})
	if err != nil {
		t.Fatalf("reserve1: %v", err)
	}
	if res.PricingPhase != PricingPhaseIncluded {
		t.Fatalf("expected included phase, got %q", res.PricingPhase)
	}
	if err := env.client.Settle(ctx, &res, 60); err != nil {
		t.Fatalf("settle1: %v", err)
	}
	requireGrantBalance(t, env.tbClient, subGrant.grantID, 0, 0, 60)

	// Now try to reserve again — should enter overage phase.
	// Window cost = 3 units/sec * 60 sec = 180 units.
	// currentOverage (80) + windowCost (180) = 260 > cap (100).
	_, err = env.client.Reserve(ctx, ReserveRequest{
		JobID: 802, OrgID: orgID, ProductID: productID, ActorID: "user-cap",
		Allocation: map[string]float64{"unit": 1}, SourceType: "job", SourceRef: "802",
	})
	if !errors.Is(err, ErrOverageCeilingExceeded) {
		t.Fatalf("expected ErrOverageCeilingExceeded, got %v", err)
	}
}

func TestOverageCapNotExceeded(t *testing.T) {
	t.Parallel()

	orgID := OrgID(8_800_000_000_000_000_011)
	productID, planID := uniqueCatalogIDs("overage-cap-ok")

	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, &stubMeteringQuerier{})

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1},
		map[string]uint64{"unit": 3},
		false,
	)

	// Cap is large enough: 500. Window cost = 180, current = 0.
	setOverageCapOnSubscription(t, env.pg, subID, 500)

	// Deplete subscription grant.
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourceSubscription, 60)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourcePurchase, 500)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: 803, OrgID: orgID, ProductID: productID, ActorID: "user-cap-ok",
		Allocation: map[string]float64{"unit": 1}, SourceType: "job", SourceRef: "803",
	})
	if err != nil {
		t.Fatalf("reserve1 (included): %v", err)
	}
	if err := env.client.Settle(ctx, &res, 60); err != nil {
		t.Fatalf("settle1: %v", err)
	}

	// Second reserve should succeed in overage phase under cap.
	res2, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: 804, OrgID: orgID, ProductID: productID, ActorID: "user-cap-ok",
		Allocation: map[string]float64{"unit": 1}, SourceType: "job", SourceRef: "804",
	})
	if err != nil {
		t.Fatalf("reserve2 (overage): %v", err)
	}
	if res2.PricingPhase != PricingPhaseOverage {
		t.Fatalf("expected overage phase, got %q", res2.PricingPhase)
	}

	if err := env.client.Void(ctx, &res2); err != nil {
		t.Fatalf("void: %v", err)
	}
}

func TestOverageCapZeroBlocksAllOverage(t *testing.T) {
	t.Parallel()

	orgID := OrgID(8_800_000_000_000_000_012)
	productID, planID := uniqueCatalogIDs("overage-cap-zero")

	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, &stubMeteringQuerier{})

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1},
		map[string]uint64{"unit": 3},
		false,
	)

	// Cap = 0 means org has self-disabled overage.
	setOverageCapOnSubscription(t, env.pg, subID, 0)

	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourceSubscription, 60)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourcePurchase, 500)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Consume subscription grant.
	res, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: 805, OrgID: orgID, ProductID: productID, ActorID: "user-cap-zero",
		Allocation: map[string]float64{"unit": 1}, SourceType: "job", SourceRef: "805",
	})
	if err != nil {
		t.Fatalf("reserve1 (included): %v", err)
	}
	if err := env.client.Settle(ctx, &res, 60); err != nil {
		t.Fatalf("settle1: %v", err)
	}

	// Overage should be blocked: windowCost (180) > cap (0).
	_, err = env.client.Reserve(ctx, ReserveRequest{
		JobID: 806, OrgID: orgID, ProductID: productID, ActorID: "user-cap-zero",
		Allocation: map[string]float64{"unit": 1}, SourceType: "job", SourceRef: "806",
	})
	if !errors.Is(err, ErrOverageCeilingExceeded) {
		t.Fatalf("expected ErrOverageCeilingExceeded, got %v", err)
	}
}

func TestOverageCapNullAllowsUnlimitedOverage(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_800_000_000_000_000_013)
	productID, planID := uniqueCatalogIDs("overage-cap-null")

	// ensureMeteredPlanForTest does not set overage_cap_units — it's NULL by default.
	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1},
		map[string]uint64{"unit": 3},
		false,
	)

	// No querier needed — NULL cap should skip enforcement entirely.
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourceSubscription, 60)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourcePurchase, 500)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Consume subscription grant.
	res, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: 807, OrgID: orgID, ProductID: productID, ActorID: "user-cap-null",
		Allocation: map[string]float64{"unit": 1}, SourceType: "job", SourceRef: "807",
	})
	if err != nil {
		t.Fatalf("reserve1: %v", err)
	}
	if err := env.client.Settle(ctx, &res, 60); err != nil {
		t.Fatalf("settle1: %v", err)
	}

	// Overage should proceed with no cap enforcement.
	res2, err := env.client.Reserve(ctx, ReserveRequest{
		JobID: 808, OrgID: orgID, ProductID: productID, ActorID: "user-cap-null",
		Allocation: map[string]float64{"unit": 1}, SourceType: "job", SourceRef: "808",
	})
	if err != nil {
		t.Fatalf("reserve2 (overage, no cap): %v", err)
	}
	if res2.PricingPhase != PricingPhaseOverage {
		t.Fatalf("expected overage phase, got %q", res2.PricingPhase)
	}
	if err := env.client.Void(ctx, &res2); err != nil {
		t.Fatalf("void: %v", err)
	}
}

func TestNewClientRejectsNilMeteringWriter(t *testing.T) {
	t.Parallel()

	env := newPhase1TestEnv(t)

	cfg := DefaultConfig()
	cfg.ReservationWindowSecs = 60
	cfg.PendingTimeoutSecs = 600
	cfg.StripeSecretKey = "sk_test_placeholder"
	cfg.TigerBeetleAddresses = []string{env.tbAddress}
	cfg.TigerBeetleClusterID = env.clusterID

	// NewClient must reject nil metering writer at construction time (fail closed).
	_, err := NewClient(env.tbClient, env.pg, stripe.NewClient(cfg.StripeSecretKey), nil, noopMeteringQuerier{}, cfg)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for nil metering writer, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setQuotasOnPlan(t fatalHelper, db *sql.DB, planID string, policy quotaPolicy) {
	t.Helper()

	quotasJSON, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal quotas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, `
		UPDATE plans SET quotas = $1::jsonb WHERE plan_id = $2
	`, string(quotasJSON), planID); err != nil {
		t.Fatalf("update quotas on plan %s: %v", planID, err)
	}
}

func setOverageCapOnSubscription(t fatalHelper, db *sql.DB, subscriptionID int64, cap int64) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, `
		UPDATE subscriptions SET overage_cap_units = $1 WHERE subscription_id = $2
	`, cap, subscriptionID); err != nil {
		t.Fatalf("update overage_cap_units on subscription %d: %v", subscriptionID, err)
	}
}
