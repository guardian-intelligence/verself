//go:build integration

package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// TestCheckQuotasAgainstLiveHost exercises CheckQuotas end-to-end:
// seeds a metered plan with quota limits, settles a reservation to produce
// a metering row in ClickHouse, then verifies that CheckQuotas reads the
// rolling-window sum correctly and enforces instant limits.
func TestCheckQuotasAgainstLiveHost(t *testing.T) {
	t.Parallel()

	chAddr := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_ADDR")
	chPassword := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	if chAddr == "" || chPassword == "" {
		t.Skip("set FORGE_METAL_BILLING_LIVE_CH_ADDR and FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	}

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddr},
		Auth: clickhouse.Auth{
			Database: "forge_metal",
			Username: "default",
			Password: chPassword,
		},
	})
	if err != nil {
		t.Fatalf("open clickhouse: %v", err)
	}
	t.Cleanup(func() { _ = chConn.Close() })

	env := newLivePhase1EnvWithMetering(t, NewClickHouseMeteringWriter(chConn, "forge_metal"), NewClickHouseMeteringQuerier(chConn, "forge_metal"))
	env.client.cfg.ReservationWindowSecs = 60
	env.client.cfg.PendingTimeoutSecs = 600

	orgID := OrgID(7_700_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-quotas")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set quota: unit dimension, hour window, limit 100.
	quotasJSON, _ := json.Marshal(quotaPolicy{Limits: []quotaLimit{
		{Dimension: "unit", Window: "hour", Limit: 100},
		{Dimension: "concurrent_vms", Window: "instant", Limit: 5},
	}})
	if _, err := env.pg.ExecContext(ctx, `
		UPDATE plans SET quotas = $1::jsonb WHERE plan_id = $2
	`, string(quotasJSON), planID); err != nil {
		t.Fatalf("update quotas: %v", err)
	}

	// Seed grants and produce a metering row.
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 500)

	jobID := JobID(time.Now().UTC().UnixNano() % 1_000_000_000)
	reservation, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      jobID,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-live-quotas",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  strconv.FormatInt(int64(jobID), 10),
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	// Settle 30 seconds: produces metering row with dimensions={"unit":1}, charge_units=30.
	if err := env.client.Settle(ctx, &reservation, 30); err != nil {
		t.Fatalf("settle: %v", err)
	}

	// Verify the metering row exists before checking quotas.
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	var rowCount uint64
	if err := chConn.QueryRow(ctx, `
		SELECT count()
		FROM forge_metal.metering
		WHERE org_id = $1 AND product_id = $2
	`, orgIDStr, productID).Scan(&rowCount); err != nil {
		t.Fatalf("count metering rows: %v", err)
	}
	if rowCount == 0 {
		t.Fatal("no metering rows found after settle")
	}

	// CheckQuotas: rolling window should pass (30 < 100), instant should pass if concurrent_vms < 5.
	result, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{
		"concurrent_vms": 3,
	})
	if err != nil {
		t.Fatalf("check quotas (should pass): %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected quota check to pass, got violations: %+v", result.Violations)
	}

	// CheckQuotas with instant violation: concurrent_vms = 6 > 5.
	result, err = env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{
		"concurrent_vms": 6,
	})
	if err != nil {
		t.Fatalf("check quotas (instant violation): %v", err)
	}
	if result.Allowed {
		t.Fatal("expected instant quota violation, got Allowed=true")
	}

	found := false
	for _, v := range result.Violations {
		if v.Dimension == "concurrent_vms" && v.Window == "instant" && v.Limit == 5 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected concurrent_vms/instant violation, got %+v", result.Violations)
	}

	// Verify SumDimension actually reads from ClickHouse.
	querier := NewClickHouseMeteringQuerier(chConn, "forge_metal")
	since := time.Now().UTC().Add(-1 * time.Hour)
	unitSum, err := querier.SumDimension(ctx, orgID, productID, "unit", since)
	if err != nil {
		t.Fatalf("sum dimension: %v", err)
	}
	// We settled 30 seconds with allocation {"unit": 1}, so each metering row has dimensions["unit"]=1.
	// The sum depends on how many windows (we did 1 settle), so it should be >= 1.
	if unitSum < 1 {
		t.Fatalf("expected unit dimension sum >= 1, got %f", unitSum)
	}

	t.Logf("verified live CheckQuotas: org_id=%s product_id=%s unit_sum=%.1f violations=%d",
		orgIDStr, productID, unitSum, len(result.Violations))
}

// TestOverageCapEnforcementAgainstLiveHost exercises the overage cap enforcement
// path end-to-end: deplete subscription grant, set an overage cap, and verify
// that Reserve returns ErrOverageCeilingExceeded when projected overage exceeds cap.
func TestOverageCapEnforcementAgainstLiveHost(t *testing.T) {
	t.Parallel()

	chAddr := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_ADDR")
	chPassword := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	if chAddr == "" || chPassword == "" {
		t.Skip("set FORGE_METAL_BILLING_LIVE_CH_ADDR and FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	}

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddr},
		Auth: clickhouse.Auth{
			Database: "forge_metal",
			Username: "default",
			Password: chPassword,
		},
	})
	if err != nil {
		t.Fatalf("open clickhouse: %v", err)
	}
	t.Cleanup(func() { _ = chConn.Close() })

	env := newLivePhase1EnvWithMetering(t, NewClickHouseMeteringWriter(chConn, "forge_metal"), NewClickHouseMeteringQuerier(chConn, "forge_metal"))
	env.client.cfg.ReservationWindowSecs = 60
	env.client.cfg.PendingTimeoutSecs = 600

	orgID := OrgID(7_800_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-overage-cap")

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1},
		map[string]uint64{"unit": 3}, // overage = 3x
		false,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Seed subscription grant (exactly 1 window = 60 units) and prepaid grant.
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourceSubscription, 60)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient,
		orgID, productID, SourcePurchase, 1000)

	// Deplete subscription grant.
	jobID1 := JobID(time.Now().UTC().UnixNano()%1_000_000_000 + 1)
	res1, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      jobID1,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-live-cap",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  strconv.FormatInt(int64(jobID1), 10),
	})
	if err != nil {
		t.Fatalf("reserve1 (included): %v", err)
	}
	if res1.PricingPhase != PricingPhaseIncluded {
		t.Fatalf("expected included phase, got %q", res1.PricingPhase)
	}
	if err := env.client.Settle(ctx, &res1, 60); err != nil {
		t.Fatalf("settle1: %v", err)
	}

	// First overage reservation (no cap yet) — should succeed.
	jobID2 := JobID(time.Now().UTC().UnixNano()%1_000_000_000 + 2)
	res2, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      jobID2,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-live-cap",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  strconv.FormatInt(int64(jobID2), 10),
	})
	if err != nil {
		t.Fatalf("reserve2 (overage, no cap): %v", err)
	}
	if res2.PricingPhase != PricingPhaseOverage {
		t.Fatalf("expected overage phase, got %q", res2.PricingPhase)
	}
	// Settle: 30 seconds at 3 units/sec = 90 overage units. This creates the metering row.
	if err := env.client.Settle(ctx, &res2, 30); err != nil {
		t.Fatalf("settle2: %v", err)
	}

	// Now set overage cap = 100. Current overage = 90.
	// Next window would cost 3*60=180. 90+180=270 > 100 → should be blocked.
	if _, err := env.pg.ExecContext(ctx, `
		UPDATE subscriptions SET overage_cap_units = $1 WHERE subscription_id = $2
	`, 100, subID); err != nil {
		t.Fatalf("set overage cap: %v", err)
	}

	jobID3 := JobID(time.Now().UTC().UnixNano()%1_000_000_000 + 3)
	_, err = env.client.Reserve(ctx, ReserveRequest{
		JobID:      jobID3,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-live-cap",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  strconv.FormatInt(int64(jobID3), 10),
	})
	if !errors.Is(err, ErrOverageCeilingExceeded) {
		t.Fatalf("expected ErrOverageCeilingExceeded, got %v", err)
	}

	// Verify SumChargeUnits reads the overage metering rows.
	querier := NewClickHouseMeteringQuerier(chConn, "forge_metal")
	now := time.Now().UTC()
	overageSum, err := querier.SumChargeUnits(ctx, orgID, productID, PricingPhaseOverage, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("sum charge units: %v", err)
	}
	if overageSum != 90 {
		t.Fatalf("expected overage sum = 90, got %d", overageSum)
	}

	t.Logf("verified live overage cap enforcement: org_id=%d overage_sum=%d cap=100 → blocked",
		orgID, overageSum)
}

// TestMeteringQuerierSumDimensionAgainstLiveHost is a focused test for the
// ClickHouseMeteringQuerier.SumDimension method against real ClickHouse data.
func TestMeteringQuerierSumDimensionAgainstLiveHost(t *testing.T) {
	t.Parallel()

	chAddr := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_ADDR")
	chPassword := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	if chAddr == "" || chPassword == "" {
		t.Skip("set FORGE_METAL_BILLING_LIVE_CH_ADDR and FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	}

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddr},
		Auth: clickhouse.Auth{
			Database: "forge_metal",
			Username: "default",
			Password: chPassword,
		},
	})
	if err != nil {
		t.Fatalf("open clickhouse: %v", err)
	}
	t.Cleanup(func() { _ = chConn.Close() })

	env := newLivePhase1EnvWithMetering(t, NewClickHouseMeteringWriter(chConn, "forge_metal"), noopMeteringQuerier{})
	env.client.cfg.ReservationWindowSecs = 60
	env.client.cfg.PendingTimeoutSecs = 600

	orgID := OrgID(7_900_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-sum-dim")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 500)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Produce two metering rows.
	for i := int64(1); i <= 2; i++ {
		jobID := JobID(time.Now().UTC().UnixNano()%1_000_000_000 + i*100)
		res, err := env.client.Reserve(ctx, ReserveRequest{
			JobID:      jobID,
			OrgID:      orgID,
			ProductID:  productID,
			ActorID:    "user-live-sum",
			Allocation: map[string]float64{"unit": 1},
			SourceType: "job",
			SourceRef:  fmt.Sprintf("sum-dim-%d", i),
		})
		if err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
		if err := env.client.Settle(ctx, &res, 20); err != nil {
			t.Fatalf("settle %d: %v", i, err)
		}
	}

	querier := NewClickHouseMeteringQuerier(chConn, "forge_metal")
	since := time.Now().UTC().Add(-1 * time.Hour)

	unitSum, err := querier.SumDimension(ctx, orgID, productID, "unit", since)
	if err != nil {
		t.Fatalf("sum dimension: %v", err)
	}

	// Two metering rows with dimensions={"unit":1} → sum = 2.
	if unitSum < 2 {
		t.Fatalf("expected unit sum >= 2.0, got %.1f", unitSum)
	}

	chargeSum, err := querier.SumChargeUnits(ctx, orgID, productID, PricingPhaseIncluded, since)
	if err != nil {
		t.Fatalf("sum charge units: %v", err)
	}
	// Two windows of 20 seconds at 1 unit/sec = 40 total charge_units.
	if chargeSum != 40 {
		t.Fatalf("expected charge_units sum = 40, got %d", chargeSum)
	}

	t.Logf("verified live SumDimension=%.1f SumChargeUnits=%d for org=%d product=%s",
		unitSum, chargeSum, orgID, productID)
}
