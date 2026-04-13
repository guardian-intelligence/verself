package billing

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPrepareBillingEventCanonicalizesPayloadAndHashes(t *testing.T) {
	t.Parallel()

	first, err := prepareBillingEvent(billingEventFact{
		EventID:       "evt_test",
		EventType:     "grant_issued",
		AggregateType: "credit_grant",
		AggregateID:   "grant_01",
		OrgID:         "42",
		OccurredAt:    time.Date(2026, 4, 13, 12, 0, 0, 123, time.FixedZone("offset", -7*60*60)),
		Payload:       []byte(`{"b":2,"a":1}`),
	})
	if err != nil {
		t.Fatalf("prepare first event: %v", err)
	}
	second, err := prepareBillingEvent(billingEventFact{
		EventID:       "evt_test",
		EventType:     "grant_issued",
		AggregateType: "credit_grant",
		AggregateID:   "grant_01",
		OrgID:         "42",
		OccurredAt:    time.Date(2026, 4, 13, 19, 0, 0, 123, time.UTC),
		Payload:       []byte(`{"a":1,"b":2}`),
	})
	if err != nil {
		t.Fatalf("prepare second event: %v", err)
	}

	assertEqual(t, string(first.Payload), string(second.Payload), "canonical payload")
	assertEqual(t, first.PayloadHash, second.PayloadHash, "payload hash")
	assertEqual(t, first.EventVersion, billingEventCurrentVersion, "event version")
	assertEqual(t, first.OccurredAt.Location(), time.UTC, "occurred_at location")
}

func TestPrepareBillingEventRejectsIncompleteAndInvalidPayload(t *testing.T) {
	t.Parallel()

	_, err := prepareBillingEvent(billingEventFact{EventID: "evt_missing"})
	if err == nil || !strings.Contains(err.Error(), "billing event is incomplete") {
		t.Fatalf("expected incomplete event error, got %v", err)
	}

	_, err = prepareBillingEvent(billingEventFact{
		EventID:       "evt_bad_payload",
		EventType:     "grant_issued",
		AggregateType: "credit_grant",
		AggregateID:   "grant_01",
		OrgID:         "42",
		Payload:       []byte(`{"unterminated"`),
	})
	if err == nil || !strings.Contains(err.Error(), "payload") {
		t.Fatalf("expected payload error, got %v", err)
	}
}

func TestPopulateBillingEventProjectionDimensionsDerivesClickHouseAxes(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(map[string]string{
		"contract_id":         "contract_01",
		"phase_id":            "phase_01",
		"plan_id":             "plan_01",
		"cycle_id":            "cycle_01",
		"invoice_id":          "invoice_01",
		"provider_event_id":   "stripe_evt_01",
		"pricing_contract_id": "pricing_contract_01",
		"pricing_phase_id":    "pricing_phase_01",
		"pricing_plan_id":     "pricing_plan_01",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	event := BillingEvent{Payload: string(payload)}
	if err := populateBillingEventProjectionDimensions(&event); err != nil {
		t.Fatalf("populate dimensions: %v", err)
	}

	assertEqual(t, event.ContractID, "contract_01", "contract id")
	assertEqual(t, event.CycleID, "cycle_01", "cycle id")
	assertEqual(t, event.PricingContractID, "pricing_contract_01", "pricing contract id")
	assertEqual(t, event.PricingPhaseID, "pricing_phase_01", "pricing phase id")
	assertEqual(t, event.PricingPlanID, "pricing_plan_01", "pricing plan id")
	assertEqual(t, event.InvoiceID, "invoice_01", "invoice id")
	assertEqual(t, event.ProviderEventID, "stripe_evt_01", "provider event id")
}

func TestPopulateBillingEventProjectionDimensionsFallsBackToDomainAxes(t *testing.T) {
	t.Parallel()

	event := BillingEvent{Payload: `{"contract_id":"contract_01","phase_id":"phase_01","plan_id":"plan_01"}`}
	if err := populateBillingEventProjectionDimensions(&event); err != nil {
		t.Fatalf("populate dimensions: %v", err)
	}

	assertEqual(t, event.PricingContractID, "contract_01", "pricing contract fallback")
	assertEqual(t, event.PricingPhaseID, "phase_01", "pricing phase fallback")
	assertEqual(t, event.PricingPlanID, "plan_01", "pricing plan fallback")
}

func TestBillingEventDeliveryRetryDelayCaps(t *testing.T) {
	t.Parallel()

	assertEqual(t, billingEventDeliveryRetryDelay(1), 30*time.Second, "first retry delay")
	assertEqual(t, billingEventDeliveryRetryDelay(2), time.Minute, "second retry delay")
	assertEqual(t, billingEventDeliveryRetryDelay(100), 15*time.Minute, "capped retry delay")
}
