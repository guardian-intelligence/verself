package billing

import (
	"testing"
	"time"
)

func TestBuildStatementSeparatesUsageFundingAndReservations(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	grantID := stripeGrantID(42, GrantScopeAccount, "", "", "pi_test")
	statement, err := buildStatement(
		42,
		"sandbox",
		statementPeriod{Start: periodStart, End: periodEnd, Source: "subscription"},
		[]GrantBalance{
			{
				GrantID:        stripeGrantID(42, GrantScopeBucket, "sandbox", "storage", "in_test"),
				ScopeType:      GrantScopeBucket,
				ScopeProductID: "sandbox",
				ScopeBucketID:  "storage",
				Source:         SourceSubscription,
				Available:      6_000,
				Pending:        300,
			},
			{
				GrantID:   grantID,
				ScopeType: GrantScopeAccount,
				Source:    SourcePurchase,
				Available: 50_000,
				Pending:   2_000,
			},
		},
		[]persistedWindow{
			statementTestWindow("settled-storage", "settled", 100, []WindowFundingLeg{
				{
					GrantID:             stripeGrantID(42, GrantScopeBucket, "sandbox", "storage", "in_test"),
					ChargeProductID:     "sandbox",
					ChargeBucketID:      "storage",
					Amount:              2_500,
					Source:              SourceSubscription,
					GrantScopeType:      GrantScopeBucket,
					GrantScopeProductID: "sandbox",
					GrantScopeBucketID:  "storage",
				},
				{
					GrantID:            grantID,
					ChargeProductID:    "sandbox",
					ChargeBucketID:     "storage",
					Amount:             1_500,
					Source:             SourcePurchase,
					GrantScopeType:     GrantScopeAccount,
					GrantScopeBucketID: "",
				},
			}),
			statementTestWindow("reserved-storage", "reserved", 20, []WindowFundingLeg{
				{
					GrantID:         grantID,
					ChargeProductID: "sandbox",
					ChargeBucketID:  "storage",
					Amount:          800,
					Source:          SourcePurchase,
					GrantScopeType:  GrantScopeAccount,
				},
			}),
		},
		time.Date(2026, 4, 10, 12, 1, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("buildStatement: %v", err)
	}

	assertEqual(t, statement.OrgID, OrgID(42), "org id")
	assertEqual(t, statement.ProductID, "sandbox", "product id")
	assertEqual(t, statement.PeriodSource, "subscription", "period source")
	assertEqual(t, len(statement.LineItems), 1, "line count")
	line := statement.LineItems[0]
	assertEqual(t, line.ProductID, "sandbox", "line product")
	assertEqual(t, line.BucketID, "storage", "line bucket")
	assertEqual(t, line.ComponentID, "premium_nvme_gib", "line component")
	assertEqual(t, line.Quantity, float64(100), "line quantity")
	assertEqual(t, line.UnitRate, uint64(40), "line unit rate")
	assertEqual(t, line.ChargeUnits, uint64(4_000), "line charge")
	assertEqual(t, line.PricingPhase, string(PricingPhaseIncluded), "line pricing phase")

	assertEqual(t, len(statement.BucketSummaries), 1, "bucket count")
	bucket := statement.BucketSummaries[0]
	assertEqual(t, bucket.ChargeUnits, uint64(4_000), "bucket charge")
	assertEqual(t, bucket.SubscriptionUnits, uint64(2_500), "bucket subscription")
	assertEqual(t, bucket.PurchaseUnits, uint64(1_500), "bucket purchase")
	assertEqual(t, bucket.ReservedUnits, uint64(800), "bucket reserved")
	assertEqual(t, statement.Totals.ChargeUnits, uint64(4_000), "total charge")
	assertEqual(t, statement.Totals.SubscriptionUnits, uint64(2_500), "total subscription")
	assertEqual(t, statement.Totals.PurchaseUnits, uint64(1_500), "total purchase")
	assertEqual(t, statement.Totals.ReservedUnits, uint64(800), "total reserved")
	assertEqual(t, statement.Totals.TotalDueUnits, uint64(0), "total due")

	assertEqual(t, len(statement.GrantSummaries), 2, "grant summary count")
	accountGrant := findStatementGrantSummary(t, statement, GrantScopeAccount, "", "", SourcePurchase)
	assertEqual(t, accountGrant.Available, uint64(50_000), "account available")
	assertEqual(t, accountGrant.Pending, uint64(2_000), "account pending")
	bucketGrant := findStatementGrantSummary(t, statement, GrantScopeBucket, "sandbox", "storage", SourceSubscription)
	assertEqual(t, bucketGrant.Available, uint64(6_000), "bucket available")
	assertEqual(t, bucketGrant.Pending, uint64(300), "bucket pending")
}

func statementTestWindow(windowID string, state string, quantity uint32, legs []WindowFundingLeg) persistedWindow {
	return persistedWindow{
		WindowID:            windowID,
		OrgID:               42,
		ActorID:             "actor",
		ProductID:           "sandbox",
		PlanID:              "sandbox-pro",
		SourceType:          "execution",
		SourceRef:           "attempt",
		WindowSeq:           1,
		State:               state,
		ReservationShape:    ReservationShapeTime,
		ReservedQuantity:    quantity,
		ActualQuantity:      quantity,
		BillableQuantity:    quantity,
		ReservedChargeUnits: uint64(quantity) * 40,
		BilledChargeUnits:   uint64(quantity) * 40,
		PricingPhase:        PricingPhaseIncluded,
		Allocation:          map[string]float64{"premium_nvme_gib": 1},
		RateContext: windowRateContext{
			PlanID:               "sandbox-pro",
			UnitRates:            map[string]uint64{"premium_nvme_gib": 40},
			RateBuckets:          map[string]string{"premium_nvme_gib": "storage"},
			ComponentCostPerUnit: map[string]uint64{"premium_nvme_gib": 40},
			BucketCostPerUnit:    map[string]uint64{"storage": 40},
			CostPerUnit:          40,
		},
		FundingLegs: legs,
		WindowStart: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
		ExpiresAt:   time.Date(2026, 4, 10, 12, 5, 0, 0, time.UTC),
	}
}

func findStatementGrantSummary(t *testing.T, statement Statement, scope GrantScopeType, productID string, bucketID string, source GrantSourceType) StatementGrantSummary {
	t.Helper()
	for _, grant := range statement.GrantSummaries {
		if grant.ScopeType == scope && grant.ScopeProductID == productID && grant.ScopeBucketID == bucketID && grant.Source == source {
			return grant
		}
	}
	t.Fatalf("grant summary %s/%s/%s/%s not found", scope, productID, bucketID, source)
	return StatementGrantSummary{}
}
