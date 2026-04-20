package billing

import (
	"reflect"
	"testing"
	"time"
)

func TestReserveWindowQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		shape     string
		requested uint32
		want      uint32
	}{
		{
			name:  "zero time uses default five minute reservation",
			shape: ReservationShapeTime,
			want:  defaultWindowMillis,
		},
		{
			name:      "custom time duration is honored",
			shape:     ReservationShapeTime,
			requested: 60_000,
			want:      60_000,
		},
		{
			name:      "count quantity is honored",
			shape:     ReservationShapeCount,
			requested: 1,
			want:      1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := reserveWindowQuantity(tt.shape, tt.requested)
			if err != nil {
				t.Fatalf("reserveWindowQuantity() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("reserveWindowQuantity() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReserveWindowTimingUsesChosenQuantity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	expiresAt, renewBy := reserveWindowTiming(ReservationShapeTime, now, 60_000)

	if got := expiresAt.Sub(now); got != time.Minute {
		t.Fatalf("expiresAt - now = %s, want 1m", got)
	}
	if got := renewBy.Sub(now); got != 40*time.Second {
		t.Fatalf("renewBy - now = %s, want 40s", got)
	}
}

func TestReserveWindowTimingSupportsThirtySecondWindows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	expiresAt, renewBy := reserveWindowTiming(ReservationShapeTime, now, 30_000)

	if got := expiresAt.Sub(now); got != 30*time.Second {
		t.Fatalf("expiresAt - now = %s, want 30s", got)
	}
	if got := renewBy.Sub(now); got != 20*time.Second {
		t.Fatalf("renewBy - now = %s, want 20s", got)
	}
}

func TestReserveWindowQuantityRejectsInvalidQuantities(t *testing.T) {
	t.Parallel()

	if _, err := reserveWindowQuantity(ReservationShapeTime, 0); err != nil {
		t.Fatalf("zero default window returned error: %v", err)
	}
	if _, err := reserveWindowQuantity(ReservationShapeTime, 30_000); err != nil {
		t.Fatalf("30s custom window returned error: %v", err)
	}
	if _, err := reserveWindowQuantity(ReservationShapeTime, 29_999); err == nil {
		t.Fatalf("29.999s custom window returned nil error")
	}
	if _, err := reserveWindowQuantity(ReservationShapeCount, 0); err == nil {
		t.Fatalf("zero count quantity returned nil error")
	}
}

func TestBillingWindowIDIsOrgScoped(t *testing.T) {
	t.Parallel()

	left := billingWindowID(42, "sandbox", "volume_meter_tick", "tick-1", 1)
	right := billingWindowID(43, "sandbox", "volume_meter_tick", "tick-1", 1)

	if left == right {
		t.Fatalf("billingWindowID collided across orgs: %s", left)
	}
}

func TestReserveSourceFingerprintUsesResolvedQuantityAndAllocation(t *testing.T) {
	t.Parallel()

	base := ReserveRequest{
		OrgID:            42,
		ProductID:        "sandbox",
		ActorID:          "actor",
		ConcurrentCount:  1,
		SourceType:       "volume_meter_tick",
		SourceRef:        "tick-1",
		WindowSeq:        1,
		ReservationShape: ReservationShapeTime,
		Allocation:       map[string]float64{"durable_volume_live_storage_gib_ms": 0.000001},
		BillingJobID:     100,
	}

	quantity, err := reserveWindowQuantity(base.ReservationShape, base.ReservedQuantity)
	if err != nil {
		t.Fatalf("reserveWindowQuantity() error = %v", err)
	}
	implicitDefault := reserveSourceFingerprint(base, base.ReservationShape, quantity)
	explicitDefault := base
	explicitDefault.ReservedQuantity = defaultWindowMillis
	explicitQuantity, err := reserveWindowQuantity(explicitDefault.ReservationShape, explicitDefault.ReservedQuantity)
	if err != nil {
		t.Fatalf("reserveWindowQuantity() explicit error = %v", err)
	}
	if got := reserveSourceFingerprint(explicitDefault, explicitDefault.ReservationShape, explicitQuantity); got != implicitDefault {
		t.Fatalf("fingerprint changed between implicit and explicit default quantity: %s != %s", got, implicitDefault)
	}

	changed := base
	changed.Allocation = map[string]float64{"durable_volume_live_storage_gib_ms": 0.000002}
	if got := reserveSourceFingerprint(changed, changed.ReservationShape, quantity); got == implicitDefault {
		t.Fatalf("fingerprint did not change after allocation changed: %s", got)
	}

	changedShape := base
	changedShape.ReservationShape = ReservationShapeCount
	if got := reserveSourceFingerprint(changedShape, ReservationShapeCount, 1); got == implicitDefault {
		t.Fatalf("fingerprint did not change after shape changed: %s", got)
	}
}

func TestComputeWindowChargesRoundsAfterQuantity(t *testing.T) {
	t.Parallel()

	componentCharges, bucketCharges, _, err := computeWindowCharges(
		map[string]float64{"durable_volume_live_storage_gib_ms": 0.000001},
		map[string]uint64{"durable_volume_live_storage_gib_ms": 1},
		map[string]string{"durable_volume_live_storage_gib_ms": "durable_storage"},
		60_000,
	)
	if err != nil {
		t.Fatalf("computeWindowCharges() error = %v", err)
	}
	if got := componentCharges["durable_volume_live_storage_gib_ms"]; got != 1 {
		t.Fatalf("component charge = %d, want 1", got)
	}
	if got := bucketCharges["durable_storage"]; got != 1 {
		t.Fatalf("bucket charge = %d, want 1", got)
	}
}

func TestReservationExposesChosenQuantity(t *testing.T) {
	t.Parallel()

	windowStart := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	expiresAt, renewBy := reserveWindowTiming(ReservationShapeTime, windowStart, 60_000)
	reservation := persistedWindow{
		WindowID:            "win_test",
		OrgID:               42,
		ProductID:           "sandbox",
		PricingPlanID:       "sandbox-default",
		ActorID:             "actor",
		SourceType:          "test",
		SourceRef:           "custom-window",
		WindowSeq:           1,
		ReservationShape:    "time",
		ReservedQuantity:    60_000,
		ReservedChargeUnits: 123,
		PricingPhase:        pricingPhaseIncluded,
		Allocation:          map[string]float64{"sku": 2},
		RateContext:         pricingContext{SKURates: map[string]uint64{"sku": 10}, CostPerUnit: 20},
		WindowStart:         windowStart,
		ExpiresAt:           expiresAt,
		RenewBy:             &renewBy,
	}.reservation()

	if reservation.ReservedQuantity != 60_000 {
		t.Fatalf("reservation ReservedQuantity = %d, want 60000", reservation.ReservedQuantity)
	}
	if got := reservation.ExpiresAt.Sub(reservation.WindowStart); got != time.Minute {
		t.Fatalf("reservation expiry duration = %s, want 1m", got)
	}
}

func TestMeteringRowForWindowUsesSettledQuantity(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	recordedAt := startedAt.Add(2 * time.Minute)
	row, err := meteringRowForWindow(persistedWindow{
		WindowID:          "win_metering",
		CycleID:           "cycle",
		OrgID:             42,
		ActorID:           "actor",
		ProductID:         "sandbox",
		SourceType:        "test",
		SourceRef:         "metering-custom-window",
		WindowSeq:         1,
		ReservationShape:  "time",
		ReservedQuantity:  60_000,
		ActualQuantity:    60_000,
		BillableQuantity:  60_000,
		PricingPhase:      pricingPhaseIncluded,
		Allocation:        map[string]float64{"sandbox_durable_volume_live_storage_gib_ms": 2.5},
		RateContext:       pricingContext{SKURates: map[string]uint64{"sandbox_durable_volume_live_storage_gib_ms": 10}, SKUBuckets: map[string]string{"sandbox_durable_volume_live_storage_gib_ms": "durable_volume_storage"}, CostPerUnit: 25},
		WindowStart:       startedAt,
		BilledChargeUnits: 1_500_000,
		UsageSummary:      map[string]any{"samples": []any{map[string]any{"observed_ms": float64(60_000)}}},
		FundingLegs:       []fundingLeg{{Source: "free_tier", Amount: 1_500_000, ComponentSKUID: "sandbox_durable_volume_live_storage_gib_ms"}},
	}, recordedAt)
	if err != nil {
		t.Fatalf("meteringRowForWindow() error = %v", err)
	}

	if row.ReservedQuantity != 60_000 {
		t.Fatalf("row ReservedQuantity = %d, want 60000", row.ReservedQuantity)
	}
	if row.ActualQuantity != 60_000 {
		t.Fatalf("row ActualQuantity = %d, want 60000", row.ActualQuantity)
	}
	if got := row.EndedAt.Sub(row.StartedAt); got != time.Minute {
		t.Fatalf("row duration = %s, want 1m", got)
	}
	if got := row.ComponentQuantities["sandbox_durable_volume_live_storage_gib_ms"]; got != 150_000 {
		t.Fatalf("component quantity = %f, want 150000", got)
	}
}

func TestMeteringRowForWindowRoundsComponentChargesAfterQuantity(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	row, err := meteringRowForWindow(persistedWindow{
		WindowID:         "win_metering_fractional",
		CycleID:          "cycle",
		OrgID:            42,
		ActorID:          "actor",
		ProductID:        "sandbox",
		SourceType:       "volume_meter_tick",
		SourceRef:        "tiny-volume",
		WindowSeq:        1,
		ReservationShape: "time",
		ReservedQuantity: 60_000,
		ActualQuantity:   60_000,
		BillableQuantity: 60_000,
		PricingPhase:     pricingPhaseIncluded,
		Allocation:       map[string]float64{"durable_volume_live_storage_gib_ms": 0.000001},
		RateContext: pricingContext{
			SKURates:    map[string]uint64{"durable_volume_live_storage_gib_ms": 1},
			SKUBuckets:  map[string]string{"durable_volume_live_storage_gib_ms": "durable_storage"},
			CostPerUnit: 1,
		},
		WindowStart:       startedAt,
		BilledChargeUnits: 1,
		FundingLegs:       []fundingLeg{{Source: "free_tier", Amount: 1, ComponentSKUID: "durable_volume_live_storage_gib_ms"}},
	}, startedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("meteringRowForWindow() error = %v", err)
	}

	if got := row.ComponentChargeUnits["durable_volume_live_storage_gib_ms"]; got != 1 {
		t.Fatalf("component charge = %d, want 1", got)
	}
	if got := row.BucketChargeUnits["durable_storage"]; got != 1 {
		t.Fatalf("bucket charge = %d, want 1", got)
	}
}

func TestFundingLegsDoNotPersistReservationTransferIDs(t *testing.T) {
	t.Parallel()

	legType := reflect.TypeOf(fundingLeg{})
	for _, name := range []string{"ReservationID", "VoidID"} {
		if _, ok := legType.FieldByName(name); ok {
			t.Fatalf("fundingLeg still exposes %s; authorization windows must not persist TigerBeetle reservation/void transfer IDs", name)
		}
	}
}
