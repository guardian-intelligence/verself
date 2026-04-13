package billing

import (
	"strings"
	"testing"
	"time"
)

func TestFreeTierEntitlementPeriodProratesFromOrgCreation(t *testing.T) {
	t.Parallel()

	anchorStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	anchorEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	orgCreatedAt := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	policy := testEntitlementPolicy(SourceFreeTier, "free-tier:sandbox:compute:v1", AnchorCalendarMonth, 3_000)

	period, ok, err := entitlementPeriodForPolicy(42, policy, orgCreatedAt, anchorStart, anchorEnd, entitlementReasonCurrent)
	if err != nil {
		t.Fatalf("entitlementPeriodForPolicy: %v", err)
	}
	if !ok {
		t.Fatal("expected entitlement period")
	}

	assertEqual(t, period.PeriodStart, orgCreatedAt, "period start")
	assertEqual(t, period.PeriodEnd, anchorEnd, "period end")
	assertEqual(t, period.AmountUnits, uint64(1_500), "prorated amount")
	assertEqual(t, period.PaymentState, PaymentNotRequired, "payment state")
	assertEqual(t, period.EntitlementState, EntitlementActive, "entitlement state")
	if !strings.Contains(period.SourceReferenceID, "free_tier:free-tier:sandbox:compute:v1:v1") {
		t.Fatalf("source reference %q does not include policy identity", period.SourceReferenceID)
	}
}

func TestContractEntitlementPeriodScopesSourceReferenceByContract(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	policy := testEntitlementPolicy(SourceContract, "contract:sandbox-pro:compute:v1", AnchorContractPhase, 3_000)

	first, ok := contractEntitlementPeriod(42, "contract_a", "phase_a", "line_a", policy, periodStart, periodEnd, PaymentPaid, EntitlementActive)
	if !ok {
		t.Fatal("expected first contract entitlement period")
	}
	second, ok := contractEntitlementPeriod(42, "contract_b", "phase_b", "line_b", policy, periodStart, periodEnd, PaymentPaid, EntitlementActive)
	if !ok {
		t.Fatal("expected second contract entitlement period")
	}

	if first.SourceReferenceID == second.SourceReferenceID {
		t.Fatalf("source references must differ across contracts: %q", first.SourceReferenceID)
	}
	if first.PeriodID == second.PeriodID {
		t.Fatalf("period ids must differ across contracts: %q", first.PeriodID)
	}
	third, ok := contractEntitlementPeriod(42, "contract_a", "phase_b", "line_b", policy, periodStart, periodEnd, PaymentPaid, EntitlementActive)
	if !ok {
		t.Fatal("expected third contract entitlement period")
	}
	if first.PeriodID == third.PeriodID {
		t.Fatalf("period ids must differ across phases and lines: %q", first.PeriodID)
	}
	if !strings.Contains(first.SourceReferenceID, "contract:contract_a:phase_a:line_a:contract:sandbox-pro:compute:v1:v1") {
		t.Fatalf("source reference %q does not include contract identity", first.SourceReferenceID)
	}
}

func TestContractEntitlementPeriodProratesPolicyActivationWithinBillingPeriod(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	policy := testEntitlementPolicy(SourceContract, "contract:sandbox-pro:compute:v2", AnchorContractPhase, 3_000)
	policy.ActiveFrom = time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)

	period, ok := contractEntitlementPeriod(42, "contract_a", "phase_a", "line_a", policy, periodStart, periodEnd, PaymentPaid, EntitlementActive)
	if !ok {
		t.Fatal("expected contract entitlement period")
	}

	assertEqual(t, period.PeriodStart, policy.ActiveFrom, "period start")
	assertEqual(t, period.PeriodEnd, periodEnd, "period end")
	assertEqual(t, period.AmountUnits, uint64(1_500), "prorated amount")
}

func testEntitlementPolicy(source GrantSourceType, policyID string, anchorKind EntitlementAnchorKind, amount uint64) EntitlementPolicy {
	return EntitlementPolicy{
		PolicyID:       policyID,
		Source:         source,
		ProductID:      "sandbox",
		ScopeType:      GrantScopeBucket,
		ScopeProductID: "sandbox",
		ScopeBucketID:  "compute",
		AmountUnits:    amount,
		Cadence:        EntitlementCadenceMonthly,
		AnchorKind:     anchorKind,
		ProrationMode:  ProrationByTimeLeft,
		PolicyVersion:  "v1",
		ActiveFrom:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}
