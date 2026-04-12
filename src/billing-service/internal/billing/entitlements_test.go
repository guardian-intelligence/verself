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

func TestSubscriptionEntitlementPeriodScopesSourceReferenceByContract(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	policy := testEntitlementPolicy(SourceSubscription, "subscription:sandbox-pro:compute:v1", AnchorSubscriptionPeriod, 3_000)

	first, ok := subscriptionEntitlementPeriod(42, "contract_a", policy, periodStart, periodEnd, PaymentPaid, EntitlementActive)
	if !ok {
		t.Fatal("expected first subscription entitlement period")
	}
	second, ok := subscriptionEntitlementPeriod(42, "contract_b", policy, periodStart, periodEnd, PaymentPaid, EntitlementActive)
	if !ok {
		t.Fatal("expected second subscription entitlement period")
	}

	if first.SourceReferenceID == second.SourceReferenceID {
		t.Fatalf("source references must differ across contracts: %q", first.SourceReferenceID)
	}
	if first.PeriodID == second.PeriodID {
		t.Fatalf("period ids must differ across contracts: %q", first.PeriodID)
	}
	if !strings.Contains(first.SourceReferenceID, "subscription:contract_a:subscription:sandbox-pro:compute:v1:v1") {
		t.Fatalf("source reference %q does not include contract identity", first.SourceReferenceID)
	}
}

func TestSubscriptionEntitlementPeriodProratesPolicyActivationWithinBillingPeriod(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	policy := testEntitlementPolicy(SourceSubscription, "subscription:sandbox-pro:compute:v2", AnchorSubscriptionPeriod, 3_000)
	policy.ActiveFrom = time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)

	period, ok := subscriptionEntitlementPeriod(42, "contract_a", policy, periodStart, periodEnd, PaymentPaid, EntitlementActive)
	if !ok {
		t.Fatal("expected subscription entitlement period")
	}

	assertEqual(t, period.PeriodStart, policy.ActiveFrom, "period start")
	assertEqual(t, period.PeriodEnd, periodEnd, "period end")
	assertEqual(t, period.AmountUnits, uint64(1_500), "prorated amount")
}

func TestSubscriptionProviderEventDefaultsAndTerminalState(t *testing.T) {
	t.Parallel()

	state := stripeSubscriptionState{
		OrgIDText:            "42",
		ProductID:            "sandbox",
		PlanID:               "sandbox-pro",
		StripeSubscriptionID: "sub_test",
	}.withDefaults()
	event, err := state.providerEvent("customer.subscription.deleted", PaymentFailed, EntitlementClosed)
	if err != nil {
		t.Fatalf("providerEvent: %v", err)
	}

	assertEqual(t, event.Provider, "stripe", "provider")
	assertEqual(t, event.OrgID, OrgID(42), "org id")
	assertEqual(t, event.Cadence, "monthly", "cadence default")
	assertEqual(t, event.Status, "active", "status default")
	assertEqual(t, event.PaymentState, PaymentFailed, "payment state")
	assertEqual(t, event.EntitlementState, EntitlementClosed, "entitlement state")
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
