package billing

import (
	"reflect"
	"testing"
	"time"
)

func TestReserveWindowQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  ReserveRequest
		want uint32
	}{
		{
			name: "zero uses default five minute reservation",
			req:  ReserveRequest{},
			want: defaultWindowMillis,
		},
		{
			name: "custom window duration is honored",
			req:  ReserveRequest{WindowMillis: 60_000},
			want: 60_000,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := reserveWindowQuantity(tt.req); got != tt.want {
				t.Fatalf("reserveWindowQuantity() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReserveWindowTimingUsesChosenQuantity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	expiresAt, renewBy := reserveWindowTiming(now, 60_000)

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
	expiresAt, renewBy := reserveWindowTiming(now, 30_000)

	if got := expiresAt.Sub(now); got != 30*time.Second {
		t.Fatalf("expiresAt - now = %s, want 30s", got)
	}
	if got := renewBy.Sub(now); got != 20*time.Second {
		t.Fatalf("renewBy - now = %s, want 20s", got)
	}
}

func TestValidateReserveWindowMillisRejectsSubThirtySecondCustomWindow(t *testing.T) {
	t.Parallel()

	if err := validateReserveWindowMillis(0); err != nil {
		t.Fatalf("zero default window returned error: %v", err)
	}
	if err := validateReserveWindowMillis(30_000); err != nil {
		t.Fatalf("30s custom window returned error: %v", err)
	}
	if err := validateReserveWindowMillis(29_999); err == nil {
		t.Fatalf("29.999s custom window returned nil error")
	}
}

func TestReservationExposesChosenQuantity(t *testing.T) {
	t.Parallel()

	windowStart := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	expiresAt, renewBy := reserveWindowTiming(windowStart, 60_000)
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
	row := meteringRowForWindow(persistedWindow{
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
		Allocation:        map[string]float64{"sandbox_volume_live_storage_gib_ms": 2.5},
		RateContext:       pricingContext{SKURates: map[string]uint64{"sandbox_volume_live_storage_gib_ms": 10}, SKUBuckets: map[string]string{"sandbox_volume_live_storage_gib_ms": "block_storage"}, CostPerUnit: 25},
		WindowStart:       startedAt,
		BilledChargeUnits: 1_500_000,
		UsageSummary:      map[string]any{"samples": []any{map[string]any{"observed_ms": float64(60_000)}}},
		FundingLegs:       []fundingLeg{{Source: "free_tier", Amount: 1_500_000, ComponentSKUID: "sandbox_volume_live_storage_gib_ms"}},
	}, recordedAt)

	if row.ReservedQuantity != 60_000 {
		t.Fatalf("row ReservedQuantity = %d, want 60000", row.ReservedQuantity)
	}
	if row.ActualQuantity != 60_000 {
		t.Fatalf("row ActualQuantity = %d, want 60000", row.ActualQuantity)
	}
	if got := row.EndedAt.Sub(row.StartedAt); got != time.Minute {
		t.Fatalf("row duration = %s, want 1m", got)
	}
	if got := row.ComponentQuantities["sandbox_volume_live_storage_gib_ms"]; got != 150_000 {
		t.Fatalf("component quantity = %f, want 150000", got)
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
