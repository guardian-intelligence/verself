package billing

import (
	"testing"
	"time"
)

const (
	testComputeSKU      = "sandbox_compute_amd_epyc_4484px_vcpu_second"
	testBlockStorageSKU = "sandbox_block_storage_premium_nvme_gib_second"
)

func TestComputeRateBreakdownGroupsComponentsByBucket(t *testing.T) {
	t.Parallel()

	skuRates := map[string]uint64{
		testBlockStorageSKU: 5,
		testComputeSKU:      10,
	}
	breakdown, err := computeRateBreakdown(
		map[string]float64{
			testBlockStorageSKU: 100,
			testComputeSKU:      2,
		},
		skuRates,
		map[string]string{
			testBlockStorageSKU: "block_storage",
			testComputeSKU:      "compute",
		},
		testSKURateContext(skuRates),
		testBucketDisplayNames(),
	)
	if err != nil {
		t.Fatalf("computeRateBreakdown: %v", err)
	}

	assertEqual(t, breakdown.CostPerUnit, uint64(520), "cost per unit")
	assertEqual(t, breakdown.ComponentCostPerUnit[testBlockStorageSKU], uint64(500), "premium NVMe component cost")
	assertEqual(t, breakdown.ComponentCostPerUnit[testComputeSKU], uint64(20), "vCPU component cost")
	assertEqual(t, breakdown.BucketCostPerUnit["block_storage"], uint64(500), "storage bucket cost")
	assertEqual(t, breakdown.BucketCostPerUnit["compute"], uint64(20), "compute bucket cost")
}

func TestPickBucketReservationShrinksToTightestFundedBucket(t *testing.T) {
	t.Parallel()

	quantity, totalChargeUnits, bucketChargeUnits, err := pickBucketReservationQuantity(
		"sandbox",
		ReservePolicy{
			TargetQuantity:      10,
			MinQuantity:         5,
			AllowPartialReserve: true,
		},
		map[string]uint64{
			"compute":       20,
			"block_storage": 500,
		},
		[]scopedGrantBalance{
			testScopedGrant("bucket-compute", SourceSubscription, GrantScopeBucket, "sandbox", "compute", 10_000),
			testScopedGrant("bucket-storage", SourceSubscription, GrantScopeBucket, "sandbox", "block_storage", 3_500),
		},
	)
	if err != nil {
		t.Fatalf("pickBucketReservationQuantity: %v", err)
	}

	assertEqual(t, quantity, uint32(7), "quantity")
	assertEqual(t, totalChargeUnits, uint64(3_640), "total charge units")
	assertEqual(t, bucketChargeUnits["compute"], uint64(140), "compute charge units")
	assertEqual(t, bucketChargeUnits["block_storage"], uint64(3_500), "storage charge units")
}

func TestPickBucketReservationFailsWhenAnyRequiredBucketIsShort(t *testing.T) {
	t.Parallel()

	_, _, _, err := pickBucketReservationQuantity(
		"sandbox",
		ReservePolicy{
			TargetQuantity:      10,
			MinQuantity:         8,
			AllowPartialReserve: true,
		},
		map[string]uint64{
			"compute":       20,
			"block_storage": 500,
		},
		[]scopedGrantBalance{
			testScopedGrant("bucket-compute", SourceSubscription, GrantScopeBucket, "sandbox", "compute", 10_000),
			testScopedGrant("bucket-storage", SourceSubscription, GrantScopeBucket, "sandbox", "block_storage", 3_500),
		},
	)
	if err != ErrInsufficientBalance {
		t.Fatalf("pickBucketReservationQuantity error = %v, want %v", err, ErrInsufficientBalance)
	}
}

func TestSettlementFundingPostsEachBucketIndependently(t *testing.T) {
	t.Parallel()

	actions, err := settleFundingLegsByBucket(
		[]WindowFundingLeg{
			{ChargeBucketID: "compute", Amount: 140},
			{ChargeBucketID: "block_storage", Amount: 3_500},
		},
		map[string]uint64{
			"compute":       100,
			"block_storage": 2_500,
		},
	)
	if err != nil {
		t.Fatalf("settleFundingLegsByBucket: %v", err)
	}

	assertEqual(t, len(actions), 2, "action count")
	assertEqual(t, actions[0].PostAmount, uint64(100), "compute post amount")
	assertEqual(t, actions[0].Void, false, "compute void")
	assertEqual(t, actions[1].PostAmount, uint64(2_500), "storage post amount")
	assertEqual(t, actions[1].Void, false, "storage void")
}

func TestBuildMeteringRowProjectsComponentAndBucketEvidence(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	window := persistedWindow{
		WindowID:            "window_01",
		OrgID:               42,
		ActorID:             "actor",
		ProductID:           "sandbox",
		PlanID:              "sandbox-pro",
		SourceType:          "job",
		SourceRef:           "job_01",
		State:               "settled",
		ReservationShape:    ReservationShapeTime,
		ReservedQuantity:    7,
		ActualQuantity:      5,
		BillableQuantity:    5,
		ReservedChargeUnits: 3_640,
		BilledChargeUnits:   2_600,
		PricingPhase:        PricingPhaseIncluded,
		Allocation: map[string]float64{
			testBlockStorageSKU: 100,
			testComputeSKU:      2,
		},
		RateContext: windowRateContext{
			PlanID:      "sandbox-pro",
			CostPerUnit: 520,
			SKURates: map[string]uint64{
				testBlockStorageSKU: 5,
				testComputeSKU:      10,
			},
			SKUBuckets: map[string]string{
				testBlockStorageSKU: "block_storage",
				testComputeSKU:      "compute",
			},
			SKUDetails:         testSKURateContext(map[string]uint64{testBlockStorageSKU: 5, testComputeSKU: 10}),
			BucketDisplayNames: testBucketDisplayNames(),
			ComponentCostPerUnit: map[string]uint64{
				testBlockStorageSKU: 500,
				testComputeSKU:      20,
			},
			BucketCostPerUnit: map[string]uint64{
				"block_storage": 500,
				"compute":       20,
			},
		},
		FundingLegs: []WindowFundingLeg{
			{ChargeProductID: "sandbox", ChargeBucketID: "compute", Amount: 140, Source: SourceSubscription, GrantScopeType: GrantScopeBucket, GrantScopeProductID: "sandbox", GrantScopeBucketID: "compute"},
			{ChargeProductID: "sandbox", ChargeBucketID: "block_storage", Amount: 3_500, Source: SourceSubscription, GrantScopeType: GrantScopeBucket, GrantScopeProductID: "sandbox", GrantScopeBucketID: "block_storage"},
		},
		UsageSummary: map[string]any{"rootfs_provisioned_bytes": uint64(1_073_741_824)},
		WindowStart:  start,
		ActivatedAt:  &start,
		ExpiresAt:    start.Add(time.Minute),
		SettledAt:    ptrTime(start.Add(5 * time.Second)),
	}

	row, err := buildMeteringRow(window)
	if err != nil {
		t.Fatalf("buildMeteringRow: %v", err)
	}

	assertEqual(t, row.ComponentQuantities[testBlockStorageSKU], float64(500), "premium NVMe component quantity")
	assertEqual(t, row.ComponentQuantities[testComputeSKU], float64(10), "vCPU component quantity")
	assertEqual(t, row.ComponentChargeUnits[testBlockStorageSKU], uint64(2_500), "premium NVMe component charge")
	assertEqual(t, row.ComponentChargeUnits[testComputeSKU], uint64(100), "vCPU component charge")
	assertEqual(t, row.BucketChargeUnits["block_storage"], uint64(2_500), "storage bucket charge")
	assertEqual(t, row.BucketChargeUnits["compute"], uint64(100), "compute bucket charge")
	assertEqual(t, row.BucketSubscriptionUnits["block_storage"], uint64(2_500), "storage subscription funding")
	assertEqual(t, row.BucketSubscriptionUnits["compute"], uint64(100), "compute subscription funding")
	assertEqual(t, row.SubscriptionUnits, uint64(2_600), "total subscription funding")
	assertEqual(t, row.UsageEvidence["rootfs_provisioned_bytes"], uint64(1_073_741_824), "rootfs evidence")
}

func TestBuildMeteringRowProjectsAccountCreditToChargeBucket(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	window := persistedWindow{
		WindowID:            "window_account_credit",
		OrgID:               42,
		ActorID:             "actor",
		ProductID:           "sandbox",
		PlanID:              "sandbox-pro",
		SourceType:          "job",
		SourceRef:           "job_02",
		State:               "settled",
		ReservationShape:    ReservationShapeTime,
		ReservedQuantity:    1,
		ActualQuantity:      1,
		BillableQuantity:    1,
		ReservedChargeUnits: 100,
		BilledChargeUnits:   100,
		PricingPhase:        PricingPhaseIncluded,
		Allocation: map[string]float64{
			testBlockStorageSKU: 100,
		},
		RateContext: windowRateContext{
			PlanID:      "sandbox-pro",
			CostPerUnit: 100,
			SKURates: map[string]uint64{
				testBlockStorageSKU: 1,
			},
			SKUBuckets: map[string]string{
				testBlockStorageSKU: "block_storage",
			},
			SKUDetails:         testSKURateContext(map[string]uint64{testBlockStorageSKU: 1}),
			BucketDisplayNames: testBucketDisplayNames(),
			ComponentCostPerUnit: map[string]uint64{
				testBlockStorageSKU: 100,
			},
			BucketCostPerUnit: map[string]uint64{
				"block_storage": 100,
			},
		},
		FundingLegs: []WindowFundingLeg{
			{
				ChargeProductID:     "sandbox",
				ChargeBucketID:      "block_storage",
				Amount:              100,
				Source:              SourcePurchase,
				GrantScopeType:      GrantScopeAccount,
				GrantScopeProductID: "",
				GrantScopeBucketID:  "",
			},
		},
		WindowStart: start,
		ActivatedAt: &start,
		ExpiresAt:   start.Add(time.Minute),
		SettledAt:   ptrTime(start.Add(time.Second)),
	}

	row, err := buildMeteringRow(window)
	if err != nil {
		t.Fatalf("buildMeteringRow: %v", err)
	}

	assertEqual(t, row.ComponentChargeUnits[testBlockStorageSKU], uint64(100), "premium NVMe component charge")
	assertEqual(t, row.BucketChargeUnits["block_storage"], uint64(100), "storage bucket charge")
	assertEqual(t, row.BucketPurchaseUnits["block_storage"], uint64(100), "storage purchase funding")
	assertEqual(t, row.PurchaseUnits, uint64(100), "total purchase funding")
}

func assertEqual[T comparable](t *testing.T, got T, want T, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func testSKURateContext(skuRates map[string]uint64) map[string]skuRateContext {
	out := map[string]skuRateContext{}
	if rate, ok := skuRates[testComputeSKU]; ok {
		out[testComputeSKU] = skuRateContext{
			DisplayName:       "AMD EPYC 4484PX @ 5.66GHz",
			BucketID:          "compute",
			BucketDisplayName: "Compute",
			QuantityUnit:      "vCPU-second",
			UnitRate:          rate,
		}
	}
	if rate, ok := skuRates[testBlockStorageSKU]; ok {
		out[testBlockStorageSKU] = skuRateContext{
			DisplayName:       "Premium NVMe",
			BucketID:          "block_storage",
			BucketDisplayName: "Block Storage",
			QuantityUnit:      "GiB-second",
			UnitRate:          rate,
		}
	}
	return out
}

func testBucketDisplayNames() map[string]string {
	return map[string]string{
		"block_storage": "Block Storage",
		"compute":       "Compute",
	}
}
