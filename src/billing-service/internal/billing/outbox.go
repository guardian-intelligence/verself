package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type billingOutboxEvent struct {
	EventID           string
	EventType         string
	AggregateType     string
	AggregateID       string
	ContractID        string
	CycleID           string
	PricingContractID string
	PricingPhaseID    string
	PricingPlanID     string
	InvoiceID         string
	ProviderEventID   string
	OrgID             string
	ProductID         string
	OccurredAt        time.Time
	Payload           []byte
}

type BillingEventWriter interface {
	InsertBillingEvents(ctx context.Context, events []BillingEvent) error
}

type noopBillingEventWriter struct{}

func (noopBillingEventWriter) InsertBillingEvents(context.Context, []BillingEvent) error { return nil }

func grantIssuedEvent(grantID GrantID, grant CreditGrant, startsAt time.Time) billingOutboxEvent {
	productID := grant.ScopeProductID
	payload, _ := json.Marshal(map[string]string{
		"grant_id":              grantID.String(),
		"org_id":                strconv.FormatUint(uint64(grant.OrgID), 10),
		"source":                grant.Source,
		"source_reference_id":   grant.SourceReferenceID,
		"entitlement_period_id": grant.EntitlementPeriodID,
		"policy_version":        grant.PolicyVersion,
		"scope_type":            grant.ScopeType.String(),
		"scope_product_id":      grant.ScopeProductID,
		"scope_bucket_id":       grant.ScopeBucketID,
		"amount_units":          strconv.FormatUint(grant.Amount, 10),
		"starts_at":             startsAt.UTC().Format(time.RFC3339Nano),
		"period_start":          grantPeriodStartString(grant.Period),
		"period_end":            grantPeriodEndString(grant.Period),
		"expires_at":            timePtrString(grant.ExpiresAt),
	})
	eventID := deterministicTextID("billing-outbox-event", "grant_issued", grantID.String())
	return billingOutboxEvent{
		EventID:       eventID,
		EventType:     "grant_issued",
		AggregateType: "credit_grant",
		AggregateID:   grantID.String(),
		OrgID:         strconv.FormatUint(uint64(grant.OrgID), 10),
		ProductID:     productID,
		OccurredAt:    startsAt.UTC(),
		Payload:       payload,
	}
}

func contractActivatedEvent(orgID OrgID, productID string, contractID string, phaseID string, planID string, cycle BillingCycle, occurredAt time.Time) billingOutboxEvent {
	occurredAt = occurredAt.UTC()
	payload, _ := json.Marshal(map[string]string{
		"org_id":      strconv.FormatUint(uint64(orgID), 10),
		"product_id":  productID,
		"contract_id": contractID,
		"phase_id":    phaseID,
		"plan_id":     planID,
		"cycle_id":    cycle.CycleID,
		"occurred_at": occurredAt.Format(time.RFC3339Nano),
	})
	return billingOutboxEvent{
		EventID:           deterministicTextID("billing-outbox-event", "contract_activated", contractID, phaseID, cycle.CycleID, occurredAt.Format(time.RFC3339Nano)),
		EventType:         "contract_activated",
		AggregateType:     "contract",
		AggregateID:       contractID,
		ContractID:        contractID,
		CycleID:           cycle.CycleID,
		PricingContractID: contractID,
		PricingPhaseID:    phaseID,
		PricingPlanID:     planID,
		OrgID:             strconv.FormatUint(uint64(orgID), 10),
		ProductID:         productID,
		OccurredAt:        occurredAt,
		Payload:           payload,
	}
}

func insertOutboxEventTx(ctx context.Context, tx *sql.Tx, event billingOutboxEvent) error {
	if event.EventID == "" || event.EventType == "" || event.AggregateID == "" || len(event.Payload) == 0 {
		return fmt.Errorf("billing outbox event is incomplete")
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO billing_outbox_events (
			event_id, event_type, aggregate_type, aggregate_id,
			contract_id, cycle_id, pricing_contract_id, pricing_phase_id, pricing_plan_id, invoice_id, provider_event_id,
			org_id, product_id, occurred_at, payload
		)
		VALUES ($1, $2, $3, $4,
		        $5, $6, $7, $8, $9, $10, $11,
		        $12, $13, $14, $15::jsonb)
		ON CONFLICT (event_id) DO NOTHING
	`, event.EventID, event.EventType, event.AggregateType, event.AggregateID,
		event.ContractID, event.CycleID, event.PricingContractID, event.PricingPhaseID, event.PricingPlanID, event.InvoiceID, event.ProviderEventID,
		event.OrgID, event.ProductID, event.OccurredAt, string(event.Payload))
	if err != nil {
		return fmt.Errorf("insert billing outbox event %s: %w", event.EventID, err)
	}
	return nil
}

func insertOutboxEvent(ctx context.Context, pg *sql.DB, event billingOutboxEvent) error {
	if event.EventID == "" || event.EventType == "" || event.AggregateID == "" || len(event.Payload) == 0 {
		return fmt.Errorf("billing outbox event is incomplete")
	}
	_, err := pg.ExecContext(ctx, `
		INSERT INTO billing_outbox_events (
			event_id, event_type, aggregate_type, aggregate_id,
			contract_id, cycle_id, pricing_contract_id, pricing_phase_id, pricing_plan_id, invoice_id, provider_event_id,
			org_id, product_id, occurred_at, payload
		)
		VALUES ($1, $2, $3, $4,
		        $5, $6, $7, $8, $9, $10, $11,
		        $12, $13, $14, $15::jsonb)
		ON CONFLICT (event_id) DO NOTHING
	`, event.EventID, event.EventType, event.AggregateType, event.AggregateID,
		event.ContractID, event.CycleID, event.PricingContractID, event.PricingPhaseID, event.PricingPlanID, event.InvoiceID, event.ProviderEventID,
		event.OrgID, event.ProductID, event.OccurredAt, string(event.Payload))
	if err != nil {
		return fmt.Errorf("insert billing outbox event %s: %w", event.EventID, err)
	}
	return nil
}

func (c *Client) ProjectPendingOutboxEvents(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin outbox projection transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT event_id, event_type, aggregate_type, aggregate_id,
		       contract_id, cycle_id, pricing_contract_id, pricing_phase_id, pricing_plan_id, invoice_id, provider_event_id,
		       org_id, product_id, occurred_at, payload::text
		FROM billing_outbox_events
		WHERE state IN ('pending', 'failed')
		  AND (next_attempt_at IS NULL OR next_attempt_at <= now())
		ORDER BY occurred_at, event_id
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("claim billing outbox events: %w", err)
	}
	defer rows.Close()

	events := make([]BillingEvent, 0, limit)
	for rows.Next() {
		var event BillingEvent
		if err := rows.Scan(
			&event.EventID,
			&event.EventType,
			&event.AggregateType,
			&event.AggregateID,
			&event.ContractID,
			&event.CycleID,
			&event.PricingContractID,
			&event.PricingPhaseID,
			&event.PricingPlanID,
			&event.InvoiceID,
			&event.ProviderEventID,
			&event.OrgID,
			&event.ProductID,
			&event.OccurredAt,
			&event.Payload,
		); err != nil {
			return 0, fmt.Errorf("scan billing outbox event: %w", err)
		}
		event.OccurredAt = event.OccurredAt.UTC()
		event.RecordedAt = c.clock().UTC()
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate billing outbox events: %w", err)
	}
	if len(events) == 0 {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("commit empty outbox projection transaction: %w", err)
		}
		return 0, nil
	}

	if err := c.events.InsertBillingEvents(ctx, events); err != nil {
		for _, event := range events {
			_, _ = tx.ExecContext(ctx, `
				UPDATE billing_outbox_events
				SET state = 'failed',
				    attempts = attempts + 1,
				    next_attempt_at = now() + interval '30 seconds',
				    delivery_error = $2
				WHERE event_id = $1
			`, event.EventID, err.Error())
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return 0, fmt.Errorf("commit outbox projection failure marker: %w", commitErr)
		}
		return 0, fmt.Errorf("project billing outbox events: %w", err)
	}

	deliveredAt := c.clock().UTC()
	for _, event := range events {
		if _, err := tx.ExecContext(ctx, `
			UPDATE billing_outbox_events
			SET state = 'delivered',
			    delivered_at = $2,
			    next_attempt_at = NULL,
			    delivery_error = ''
			WHERE event_id = $1
		`, event.EventID, deliveredAt); err != nil {
			return 0, fmt.Errorf("mark billing outbox event delivered %s: %w", event.EventID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit outbox projection: %w", err)
	}
	return len(events), nil
}

func timePtrString(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

// grantPeriodStartString and grantPeriodEndString format the period boundaries
// for outbox payloads. They keep the wire format identical to the prior
// per-field serialization (two top-level period_start / period_end keys) so
// downstream consumers don't need to learn a new shape.
func grantPeriodStartString(p *GrantPeriod) string {
	if p == nil {
		return ""
	}
	return p.Start.UTC().Format(time.RFC3339Nano)
}

func grantPeriodEndString(p *GrantPeriod) string {
	if p == nil {
		return ""
	}
	return p.End.UTC().Format(time.RFC3339Nano)
}
