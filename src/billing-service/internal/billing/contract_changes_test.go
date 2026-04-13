package billing

import (
	"testing"
	"time"
)

func TestImmediateUpgradeQuoteMathMatchesArchitectureExample(t *testing.T) {
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	effectiveAt := periodStart.Add(periodEnd.Sub(periodStart) / 4)
	remaining := periodEnd.Sub(effectiveAt)
	fullPeriod := periodEnd.Sub(periodStart)

	priceDeltaCents := prorateUint64ByDuration(20_00-5_00, remaining, fullPeriod)
	if priceDeltaCents != 11_25 {
		t.Fatalf("price delta cents = %d, want 1125", priceDeltaCents)
	}
	priceDeltaUnits, err := safeMulUint64(priceDeltaCents, ledgerUnitsPerCent)
	if err != nil {
		t.Fatalf("price delta units: %v", err)
	}
	if priceDeltaUnits != 112_500_000 {
		t.Fatalf("price delta units = %d, want 112500000", priceDeltaUnits)
	}

	current := []EntitlementPolicy{{
		PolicyID:       "hobby-compute",
		Source:         SourceContract,
		ProductID:      "sandbox",
		ScopeType:      GrantScopeBucket,
		ScopeProductID: "sandbox",
		ScopeBucketID:  "compute",
		AmountUnits:    30_000_000,
	}}
	target := []EntitlementPolicy{{
		PolicyID:       "pro-compute",
		Source:         SourceContract,
		ProductID:      "sandbox",
		ScopeType:      GrantScopeBucket,
		ScopeProductID: "sandbox",
		ScopeBucketID:  "compute",
		AmountUnits:    120_000_000,
	}}
	deltas, err := entitlementPositiveDeltas(current, target)
	if err != nil {
		t.Fatalf("entitlement deltas: %v", err)
	}
	if len(deltas) != 1 {
		t.Fatalf("delta count = %d, want 1", len(deltas))
	}
	amount := prorateUint64ByDuration(deltas[0].Amount, remaining, fullPeriod)
	if amount != 67_500_000 {
		t.Fatalf("entitlement delta amount = %d, want 67500000", amount)
	}
}

func TestEntitlementPositiveDeltasRejectPlanArbitrage(t *testing.T) {
	current := []EntitlementPolicy{{
		PolicyID:       "hobby-compute",
		Source:         SourceContract,
		ProductID:      "sandbox",
		ScopeType:      GrantScopeBucket,
		ScopeProductID: "sandbox",
		ScopeBucketID:  "compute",
		AmountUnits:    30_000_000,
	}}
	target := []EntitlementPolicy{{
		PolicyID:       "lower-compute",
		Source:         SourceContract,
		ProductID:      "sandbox",
		ScopeType:      GrantScopeBucket,
		ScopeProductID: "sandbox",
		ScopeBucketID:  "compute",
		AmountUnits:    29_999_999,
	}}
	if _, err := entitlementPositiveDeltas(current, target); err == nil {
		t.Fatal("expected reduced entitlement scope to be rejected")
	}
}
