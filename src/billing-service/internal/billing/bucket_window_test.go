package billing

import (
	"testing"
	"time"
)

func TestComputeRateBreakdownGroupsComponentsByBucket(t *testing.T) {
	t.Parallel()

	breakdown, err := computeRateBreakdown(
		"sandbox",
		map[string]float64{
			"premium_nvme_gib": 100,
			"vcpu":             2,
		},
		map[string]uint64{
			"premium_nvme_gib": 5,
			"vcpu":             10,
		},
		map[string]string{
			"premium_nvme_gib": "storage",
			"vcpu":             "compute",
		},
	)
	if err != nil {
		t.Fatalf("computeRateBreakdown: %v", err)
	}

	assertEqual(t, breakdown.CostPerUnit, uint64(520), "cost per unit")
	assertEqual(t, breakdown.ComponentCostPerUnit["premium_nvme_gib"], uint64(500), "premium NVMe component cost")
	assertEqual(t, breakdown.ComponentCostPerUnit["vcpu"], uint64(20), "vCPU component cost")
	assertEqual(t, breakdown.BucketCostPerUnit["storage"], uint64(500), "storage bucket cost")
	assertEqual(t, breakdown.BucketCostPerUnit["compute"], uint64(20), "compute bucket cost")
}

func TestPickBucketReservationShrinksToTightestFundedBucket(t *testing.T) {
	t.Parallel()

	quantity, totalChargeUnits, bucketChargeUnits, err := pickBucketReservationQuantity(
		ReservePolicy{
			TargetQuantity:      10,
			MinQuantity:         5,
			AllowPartialReserve: true,
		},
		map[string]uint64{
			"compute": 20,
			"storage": 500,
		},
		map[string]uint64{
			"compute": 10_000,
			"storage": 3_500,
		},
	)
	if err != nil {
		t.Fatalf("pickBucketReservationQuantity: %v", err)
	}

	assertEqual(t, quantity, uint32(7), "quantity")
	assertEqual(t, totalChargeUnits, uint64(3_640), "total charge units")
	assertEqual(t, bucketChargeUnits["compute"], uint64(140), "compute charge units")
	assertEqual(t, bucketChargeUnits["storage"], uint64(3_500), "storage charge units")
}

func TestPickBucketReservationFailsWhenAnyRequiredBucketIsShort(t *testing.T) {
	t.Parallel()

	_, _, _, err := pickBucketReservationQuantity(
		ReservePolicy{
			TargetQuantity:      10,
			MinQuantity:         8,
			AllowPartialReserve: true,
		},
		map[string]uint64{
			"compute": 20,
			"storage": 500,
		},
		map[string]uint64{
			"compute": 10_000,
			"storage": 3_500,
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
			{BucketID: "compute", Amount: 140},
			{BucketID: "storage", Amount: 3_500},
		},
		map[string]uint64{
			"compute": 100,
			"storage": 2_500,
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
			"premium_nvme_gib": 100,
			"vcpu":             2,
		},
		RateContext: windowRateContext{
			PlanID:      "sandbox-pro",
			CostPerUnit: 520,
			UnitRates: map[string]uint64{
				"premium_nvme_gib": 5,
				"vcpu":             10,
			},
			RateBuckets: map[string]string{
				"premium_nvme_gib": "storage",
				"vcpu":             "compute",
			},
			ComponentCostPerUnit: map[string]uint64{
				"premium_nvme_gib": 500,
				"vcpu":             20,
			},
			BucketCostPerUnit: map[string]uint64{
				"storage": 500,
				"compute": 20,
			},
		},
		FundingLegs: []WindowFundingLeg{
			{BucketID: "compute", Amount: 140, Source: SourceSubscription},
			{BucketID: "storage", Amount: 3_500, Source: SourceSubscription},
		},
		WindowStart: start,
		ActivatedAt: &start,
		ExpiresAt:   start.Add(time.Minute),
		SettledAt:   ptrTime(start.Add(5 * time.Second)),
	}

	row, err := buildMeteringRow(window)
	if err != nil {
		t.Fatalf("buildMeteringRow: %v", err)
	}

	assertEqual(t, row.ComponentQuantities["premium_nvme_gib"], float64(500), "premium NVMe component quantity")
	assertEqual(t, row.ComponentQuantities["vcpu"], float64(10), "vCPU component quantity")
	assertEqual(t, row.ComponentChargeUnits["premium_nvme_gib"], uint64(2_500), "premium NVMe component charge")
	assertEqual(t, row.ComponentChargeUnits["vcpu"], uint64(100), "vCPU component charge")
	assertEqual(t, row.BucketChargeUnits["storage"], uint64(2_500), "storage bucket charge")
	assertEqual(t, row.BucketChargeUnits["compute"], uint64(100), "compute bucket charge")
	assertEqual(t, row.BucketSubscriptionUnits["storage"], uint64(2_500), "storage subscription funding")
	assertEqual(t, row.BucketSubscriptionUnits["compute"], uint64(100), "compute subscription funding")
	assertEqual(t, row.SubscriptionUnits, uint64(2_600), "total subscription funding")
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
