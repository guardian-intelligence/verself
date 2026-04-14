package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/forge-metal/billing-service/internal/store"
)

const clickHouseBillingEventsSink = "clickhouse.billing_events"

type eventFact struct {
	EventType     string
	AggregateType string
	AggregateID   string
	OrgID         OrgID
	ProductID     string
	OccurredAt    time.Time
	Payload       map[string]any
	CorrelationID string
	CausationID   string
}

func appendEvent(ctx context.Context, tx pgx.Tx, q *store.Queries, event eventFact) error {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	payloadBytes, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal billing event payload: %w", err)
	}
	sum := sha256.Sum256(payloadBytes)
	payloadHash := hex.EncodeToString(sum[:])
	eid := eventID(event.EventType, event.AggregateID, event.OccurredAt, payloadHash)
	if err := q.InsertBillingEvent(ctx, store.InsertBillingEventParams{
		EventID:       eid,
		EventType:     event.EventType,
		EventVersion:  1,
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		OrgID:         orgIDText(event.OrgID),
		ProductID:     event.ProductID,
		OccurredAt:    timestamptz(event.OccurredAt),
		Payload:       payloadBytes,
		PayloadHash:   payloadHash,
		CorrelationID: event.CorrelationID,
		Column12:      event.CausationID,
	}); err != nil {
		return fmt.Errorf("insert billing event %s: %w", event.EventType, err)
	}
	if err := q.InsertBillingEventDelivery(ctx, store.InsertBillingEventDeliveryParams{EventID: eid, Sink: clickHouseBillingEventsSink}); err != nil {
		return fmt.Errorf("enqueue billing event delivery %s: %w", event.EventType, err)
	}
	_ = tx
	return nil
}

type billingEventProjectionRow struct {
	EventID           string    `ch:"event_id"`
	EventType         string    `ch:"event_type"`
	EventVersion      uint16    `ch:"event_version"`
	AggregateType     string    `ch:"aggregate_type"`
	AggregateID       string    `ch:"aggregate_id"`
	ContractID        string    `ch:"contract_id"`
	CycleID           string    `ch:"cycle_id"`
	PricingContractID string    `ch:"pricing_contract_id"`
	PricingPhaseID    string    `ch:"pricing_phase_id"`
	PricingPlanID     string    `ch:"pricing_plan_id"`
	InvoiceID         string    `ch:"invoice_id"`
	ProviderEventID   string    `ch:"provider_event_id"`
	OrgID             string    `ch:"org_id"`
	ProductID         string    `ch:"product_id"`
	OccurredAt        time.Time `ch:"occurred_at"`
	Payload           string    `ch:"payload"`
	PayloadHash       string    `ch:"payload_hash"`
	CorrelationID     string    `ch:"correlation_id"`
	CausationEventID  string    `ch:"causation_event_id"`
	RecordedAt        time.Time `ch:"recorded_at"`
}

func (c *Client) ProjectPendingBillingEventDeliveries(ctx context.Context, limit int) (int, error) {
	if c.ch == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 100
	}
	attemptID := textID("delivery_attempt", time.Now().UTC().Format(time.RFC3339Nano))
	claimed, err := c.queries.ClaimBillingEventDeliveries(ctx, store.ClaimBillingEventDeliveriesParams{
		LeaseDuration: pgtype.Interval{Microseconds: int64((30 * time.Second) / time.Microsecond), Valid: true},
		LeasedBy:      "billing-service",
		AttemptID:     attemptID,
		PSink:         clickHouseBillingEventsSink,
		LimitCount:    int32(limit),
	})
	if err != nil {
		return 0, fmt.Errorf("claim billing event deliveries: %w", err)
	}
	projected := 0
	for _, delivery := range claimed {
		if err := c.projectBillingEventDelivery(ctx, delivery.EventID, delivery.Sink, delivery.Generation); err != nil {
			_ = c.queries.MarkBillingEventDeliveryFailed(ctx, store.MarkBillingEventDeliveryFailedParams{
				RetryAfter:    pgtype.Interval{Microseconds: int64((30 * time.Second) / time.Microsecond), Valid: true},
				DeliveryError: err.Error(),
				EventID:       delivery.EventID,
				PSink:         delivery.Sink,
				Generation:    delivery.Generation,
			})
			return projected, err
		}
		projected++
	}
	return projected, nil
}

func (c *Client) projectBillingEventDelivery(ctx context.Context, eventID string, sink string, generation int32) error {
	event, err := c.queries.GetBillingEventForProjection(ctx, store.GetBillingEventForProjectionParams{EventID: eventID})
	if err != nil {
		return fmt.Errorf("load billing event %s: %w", eventID, err)
	}
	payload := map[string]any{}
	_ = json.Unmarshal(event.Payload, &payload)
	row := billingEventProjectionRow{
		EventID:           event.EventID,
		EventType:         event.EventType,
		EventVersion:      uint16(event.EventVersion),
		AggregateType:     event.AggregateType,
		AggregateID:       event.AggregateID,
		ContractID:        stringPayload(payload, "contract_id"),
		CycleID:           stringPayload(payload, "cycle_id"),
		PricingContractID: stringPayload(payload, "pricing_contract_id"),
		PricingPhaseID:    stringPayload(payload, "pricing_phase_id"),
		PricingPlanID:     stringPayload(payload, "pricing_plan_id"),
		InvoiceID:         stringPayload(payload, "invoice_id"),
		ProviderEventID:   stringPayload(payload, "provider_event_id"),
		OrgID:             event.OrgID,
		ProductID:         event.ProductID,
		OccurredAt:        event.OccurredAt.Time.UTC(),
		Payload:           string(event.Payload),
		PayloadHash:       event.PayloadHash,
		CorrelationID:     event.CorrelationID,
		CausationEventID:  event.CausationEventID,
		RecordedAt:        event.CreatedAt.Time.UTC(),
	}
	batch, err := c.ch.PrepareBatch(ctx, "INSERT INTO forge_metal.billing_events")
	if err != nil {
		return fmt.Errorf("prepare billing event ClickHouse batch: %w", err)
	}
	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("append billing event ClickHouse row: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send billing event ClickHouse row: %w", err)
	}
	if err := c.queries.MarkBillingEventDeliverySucceeded(ctx, store.MarkBillingEventDeliverySucceededParams{EventID: eventID, Sink: sink, Generation: generation}); err != nil {
		return fmt.Errorf("mark billing event delivery succeeded: %w", err)
	}
	return nil
}

func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}
