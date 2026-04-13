package billing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	BillingEventSinkClickHouse = "clickhouse_billing_events"

	billingEventCurrentVersion      = 1
	billingEventDeliveryMaxAttempts = 5
	billingEventDeliveryLease       = time.Minute
	billingEventDeliveryBaseBackoff = 30 * time.Second
	billingEventDeliveryMaxBackoff  = 15 * time.Minute
)

var billingEventTracer = otel.Tracer("billing-service/billing/events")

type billingEventFact struct {
	EventID          string
	EventType        string
	EventVersion     int
	AggregateType    string
	AggregateID      string
	OrgID            string
	ProductID        string
	OccurredAt       time.Time
	Payload          []byte
	PayloadHash      string
	CorrelationID    string
	CausationEventID string
}

type billingEventDelivery struct {
	EventID    string
	Sink       string
	Generation int
}

type billingEventDeliveryClaim struct {
	billingEventDelivery
	Attempts  int
	AttemptID string
}

type BillingEventWriter interface {
	InsertBillingEvents(ctx context.Context, events []BillingEvent) error
}

type noopBillingEventWriter struct{}

func (noopBillingEventWriter) InsertBillingEvents(context.Context, []BillingEvent) error { return nil }

func grantIssuedEvent(grantID GrantID, grant CreditGrant, startsAt time.Time) billingEventFact {
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
		"scope_sku_id":          grant.ScopeSKUID,
		"amount_units":          strconv.FormatUint(grant.Amount, 10),
		"starts_at":             startsAt.UTC().Format(time.RFC3339Nano),
		"period_start":          grantPeriodStartString(grant.Period),
		"period_end":            grantPeriodEndString(grant.Period),
		"expires_at":            timePtrString(grant.ExpiresAt),
	})
	return billingEventFact{
		EventID:       deterministicTextID("billing-event", "grant_issued", grantID.String()),
		EventType:     "grant_issued",
		EventVersion:  billingEventCurrentVersion,
		AggregateType: "credit_grant",
		AggregateID:   grantID.String(),
		OrgID:         strconv.FormatUint(uint64(grant.OrgID), 10),
		ProductID:     grant.ScopeProductID,
		OccurredAt:    startsAt.UTC(),
		Payload:       payload,
	}
}

func contractCreatedEvent(orgID OrgID, productID string, contractID string, planID string, occurredAt time.Time) billingEventFact {
	occurredAt = occurredAt.UTC()
	payload, _ := json.Marshal(map[string]string{
		"org_id":      strconv.FormatUint(uint64(orgID), 10),
		"product_id":  productID,
		"contract_id": contractID,
		"plan_id":     planID,
		"occurred_at": occurredAt.Format(time.RFC3339Nano),
	})
	return billingEventFact{
		EventID:       deterministicTextID("billing-event", "contract_created", contractID),
		EventType:     "contract_created",
		EventVersion:  billingEventCurrentVersion,
		AggregateType: "contract",
		AggregateID:   contractID,
		OrgID:         strconv.FormatUint(uint64(orgID), 10),
		ProductID:     productID,
		OccurredAt:    occurredAt,
		Payload:       payload,
	}
}

func contractPhaseStartedEvent(orgID OrgID, productID string, contractID string, phaseID string, planID string, cycle BillingCycle, occurredAt time.Time) billingEventFact {
	occurredAt = occurredAt.UTC()
	payload, _ := json.Marshal(map[string]string{
		"org_id":              strconv.FormatUint(uint64(orgID), 10),
		"product_id":          productID,
		"contract_id":         contractID,
		"phase_id":            phaseID,
		"plan_id":             planID,
		"cycle_id":            cycle.CycleID,
		"pricing_contract_id": contractID,
		"pricing_phase_id":    phaseID,
		"pricing_plan_id":     planID,
		"occurred_at":         occurredAt.Format(time.RFC3339Nano),
	})
	return billingEventFact{
		EventID:       deterministicTextID("billing-event", "contract_phase_started", contractID, phaseID, cycle.CycleID, occurredAt.Format(time.RFC3339Nano)),
		EventType:     "contract_phase_started",
		EventVersion:  billingEventCurrentVersion,
		AggregateType: "contract_phase",
		AggregateID:   phaseID,
		OrgID:         strconv.FormatUint(uint64(orgID), 10),
		ProductID:     productID,
		OccurredAt:    occurredAt,
		Payload:       payload,
	}
}

func insertBillingEventTx(ctx context.Context, tx *sql.Tx, event billingEventFact) error {
	prepared, err := prepareBillingEvent(event)
	if err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO billing_events (
			event_id, event_type, event_version, aggregate_type, aggregate_id,
			org_id, product_id, occurred_at, payload, payload_hash,
			correlation_id, causation_event_id
		)
		VALUES ($1, $2, $3, $4, $5,
		        $6, $7, $8, $9::jsonb, $10,
		        $11, $12)
		ON CONFLICT (event_id) DO NOTHING
	`, prepared.EventID, prepared.EventType, prepared.EventVersion, prepared.AggregateType, prepared.AggregateID,
		prepared.OrgID, prepared.ProductID, prepared.OccurredAt, string(prepared.Payload), prepared.PayloadHash,
		prepared.CorrelationID, prepared.CausationEventID)
	if err != nil {
		return fmt.Errorf("insert billing event %s: %w", prepared.EventID, err)
	}
	inserted, _ := result.RowsAffected()

	var existingHash string
	if err := tx.QueryRowContext(ctx, `
		SELECT payload_hash
		FROM billing_events
		WHERE event_id = $1
	`, prepared.EventID).Scan(&existingHash); err != nil {
		return fmt.Errorf("verify billing event %s payload hash: %w", prepared.EventID, err)
	}
	if existingHash != prepared.PayloadHash {
		return fmt.Errorf("billing event %s payload hash mismatch: existing %s new %s", prepared.EventID, existingHash, prepared.PayloadHash)
	}
	if inserted == 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO billing_event_delivery_queue (event_id, sink, generation, state, next_attempt_at)
		VALUES ($1, $2, 1, 'pending', $3)
		ON CONFLICT (event_id, sink) DO NOTHING
	`, prepared.EventID, BillingEventSinkClickHouse, prepared.OccurredAt); err != nil {
		return fmt.Errorf("insert billing event delivery %s/%s: %w", prepared.EventID, BillingEventSinkClickHouse, err)
	}
	return nil
}

func insertBillingEvent(ctx context.Context, pg *sql.DB, event billingEventFact) error {
	tx, err := pg.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin billing event insert: %w", err)
	}
	defer tx.Rollback()
	if err := insertBillingEventTx(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit billing event insert: %w", err)
	}
	return nil
}

func prepareBillingEvent(event billingEventFact) (billingEventFact, error) {
	if event.EventID == "" || event.EventType == "" || event.AggregateType == "" || event.AggregateID == "" || event.OrgID == "" || len(event.Payload) == 0 {
		return billingEventFact{}, fmt.Errorf("billing event is incomplete")
	}
	if event.EventVersion <= 0 {
		event.EventVersion = billingEventCurrentVersion
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	payload, hash, err := canonicalBillingEventPayload(event.Payload)
	if err != nil {
		return billingEventFact{}, fmt.Errorf("prepare billing event %s payload: %w", event.EventID, err)
	}
	event.Payload = payload
	event.PayloadHash = hash
	return event, nil
}

func canonicalBillingEventPayload(raw []byte) ([]byte, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, "", err
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

func (c *Client) ProjectPendingBillingEventDeliveries(ctx context.Context, limit int) (int, error) {
	ctx, span := billingEventTracer.Start(ctx, "billing.event_delivery.project_pending")
	defer span.End()

	if limit <= 0 {
		limit = 100
	}
	span.SetAttributes(attribute.Int("billing.project.limit", limit))

	rows, err := c.pg.QueryContext(ctx, `
		SELECT event_id, sink, generation
		FROM billing_event_delivery_queue
		WHERE (
			state IN ('pending', 'retryable_failed')
			AND next_attempt_at <= now()
		) OR (
			state = 'in_progress'
			AND lease_expires_at IS NOT NULL
			AND lease_expires_at <= now()
		)
		ORDER BY next_attempt_at, event_id, sink
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("query pending billing event deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]billingEventDelivery, 0, limit)
	for rows.Next() {
		var delivery billingEventDelivery
		if err := rows.Scan(&delivery.EventID, &delivery.Sink, &delivery.Generation); err != nil {
			return 0, fmt.Errorf("scan pending billing event delivery: %w", err)
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate pending billing event deliveries: %w", err)
	}

	projected := 0
	for _, delivery := range deliveries {
		didProject, err := c.ProjectBillingEventDelivery(ctx, delivery.EventID, delivery.Sink, delivery.Generation)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return projected, err
		}
		if didProject {
			projected++
		}
	}
	span.SetAttributes(attribute.Int("billing.projected_billing_event_delivery_count", projected))
	return projected, nil
}

func (c *Client) ProjectBillingEventDelivery(ctx context.Context, eventID string, sink string, generation int) (bool, error) {
	if sink == "" {
		sink = BillingEventSinkClickHouse
	}
	ctx, span := billingEventTracer.Start(ctx, "billing.event_delivery.project")
	defer span.End()
	span.SetAttributes(
		attribute.String("billing.event_id", eventID),
		attribute.String("billing.event_sink", sink),
		attribute.Int("billing.event_generation", generation),
	)

	claim, ok, err := c.claimBillingEventDelivery(ctx, eventID, sink, generation)
	if err != nil || !ok {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(attribute.Bool("billing.event_projected", false))
		return false, err
	}
	span.SetAttributes(
		attribute.String("billing.delivery_attempt_id", claim.AttemptID),
		attribute.Int("billing.delivery_attempts", claim.Attempts),
	)
	if claim.Sink != BillingEventSinkClickHouse {
		err := fmt.Errorf("unsupported billing event sink %q", claim.Sink)
		if nackErr := c.failBillingEventDelivery(ctx, claim, err); nackErr != nil {
			span.RecordError(nackErr)
			span.SetStatus(codes.Error, nackErr.Error())
			return true, fmt.Errorf("billing event delivery %s: %w; mark delivery failed: %v", claim.EventID, err, nackErr)
		}
		span.RecordError(err)
		span.SetAttributes(attribute.String("billing.delivery_state", "failed"))
		return true, nil
	}

	event, err := c.loadBillingEventProjection(ctx, claim.EventID)
	if err != nil {
		if nackErr := c.failBillingEventDelivery(ctx, claim, err); nackErr != nil {
			span.RecordError(nackErr)
			span.SetStatus(codes.Error, nackErr.Error())
			return true, fmt.Errorf("load billing event %s: %w; mark delivery failed: %v", claim.EventID, err, nackErr)
		}
		span.RecordError(err)
		span.SetAttributes(attribute.String("billing.delivery_state", "failed"))
		return true, nil
	}

	if err := c.events.InsertBillingEvents(ctx, []BillingEvent{event}); err != nil {
		if nackErr := c.failBillingEventDelivery(ctx, claim, err); nackErr != nil {
			span.RecordError(nackErr)
			span.SetStatus(codes.Error, nackErr.Error())
			return true, fmt.Errorf("project billing event %s: %w; mark delivery failed: %v", claim.EventID, err, nackErr)
		}
		span.RecordError(err)
		span.SetAttributes(attribute.String("billing.delivery_state", "failed"))
		return true, nil
	}
	if err := c.ackBillingEventDelivery(ctx, claim); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return true, err
	}
	span.SetAttributes(
		attribute.Bool("billing.event_projected", true),
		attribute.String("billing.delivery_state", "delivered"),
	)
	return true, nil
}

func (c *Client) claimBillingEventDelivery(ctx context.Context, eventID string, sink string, generation int) (billingEventDeliveryClaim, bool, error) {
	if eventID == "" || sink == "" || generation <= 0 {
		return billingEventDeliveryClaim{}, false, fmt.Errorf("billing event delivery identity is incomplete")
	}
	now := c.clock().UTC()
	attemptID := deterministicTextID("billing-event-delivery-attempt", eventID, sink, strconv.Itoa(generation), now.Format(time.RFC3339Nano))
	leaseExpiresAt := now.Add(billingEventDeliveryLease)

	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return billingEventDeliveryClaim{}, false, fmt.Errorf("begin billing event delivery claim: %w", err)
	}
	defer tx.Rollback()

	claim := billingEventDeliveryClaim{billingEventDelivery: billingEventDelivery{EventID: eventID, Sink: sink, Generation: generation}, AttemptID: attemptID}
	err = tx.QueryRowContext(ctx, `
		UPDATE billing_event_delivery_queue
		SET state = 'in_progress',
		    attempts = attempts + 1,
		    last_attempt_at = $4,
		    lease_expires_at = $5,
		    leased_by = 'billing-service',
		    last_attempt_id = $6,
		    delivery_error = '',
		    updated_at = $4
		WHERE event_id = $1
		  AND sink = $2
		  AND generation = $3
		  AND (
		    (state IN ('pending', 'retryable_failed') AND next_attempt_at <= $4)
		    OR (state = 'in_progress' AND lease_expires_at IS NOT NULL AND lease_expires_at <= $4)
		  )
		RETURNING attempts
	`, eventID, sink, generation, now, leaseExpiresAt, attemptID).Scan(&claim.Attempts)
	if err == sql.ErrNoRows {
		if commitErr := tx.Commit(); commitErr != nil {
			return billingEventDeliveryClaim{}, false, fmt.Errorf("commit skipped billing event delivery claim: %w", commitErr)
		}
		return billingEventDeliveryClaim{}, false, nil
	}
	if err != nil {
		return billingEventDeliveryClaim{}, false, fmt.Errorf("claim billing event delivery %s/%s/%d: %w", eventID, sink, generation, err)
	}
	if err := tx.Commit(); err != nil {
		return billingEventDeliveryClaim{}, false, fmt.Errorf("commit billing event delivery claim: %w", err)
	}
	return claim, true, nil
}

func (c *Client) loadBillingEventProjection(ctx context.Context, eventID string) (BillingEvent, error) {
	var event BillingEvent
	var payload string
	var eventVersion int
	err := c.pg.QueryRowContext(ctx, `
		SELECT event_id, event_type, event_version, aggregate_type, aggregate_id,
		       org_id, product_id, occurred_at, payload::text, payload_hash,
		       correlation_id, causation_event_id
		FROM billing_events
		WHERE event_id = $1
	`, eventID).Scan(
		&event.EventID,
		&event.EventType,
		&eventVersion,
		&event.AggregateType,
		&event.AggregateID,
		&event.OrgID,
		&event.ProductID,
		&event.OccurredAt,
		&payload,
		&event.PayloadHash,
		&event.CorrelationID,
		&event.CausationEventID,
	)
	if err != nil {
		return BillingEvent{}, err
	}
	event.EventVersion = uint16(eventVersion)
	event.OccurredAt = event.OccurredAt.UTC()
	event.Payload = payload
	event.RecordedAt = c.clock().UTC()
	if err := populateBillingEventProjectionDimensions(&event); err != nil {
		return BillingEvent{}, err
	}
	return event, nil
}

func populateBillingEventProjectionDimensions(event *BillingEvent) error {
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		return fmt.Errorf("decode billing event payload for projection: %w", err)
	}
	event.ContractID = payloadString(payload, "contract_id")
	event.CycleID = payloadString(payload, "cycle_id")
	event.PricingContractID = firstNonEmpty(payloadString(payload, "pricing_contract_id"), event.ContractID)
	event.PricingPhaseID = firstNonEmpty(payloadString(payload, "pricing_phase_id"), payloadString(payload, "phase_id"))
	event.PricingPlanID = firstNonEmpty(payloadString(payload, "pricing_plan_id"), payloadString(payload, "plan_id"))
	event.InvoiceID = payloadString(payload, "invoice_id")
	event.ProviderEventID = payloadString(payload, "provider_event_id")
	return nil
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (c *Client) ackBillingEventDelivery(ctx context.Context, claim billingEventDeliveryClaim) error {
	_, err := c.pg.ExecContext(ctx, `
		DELETE FROM billing_event_delivery_queue
		WHERE event_id = $1
		  AND sink = $2
		  AND generation = $3
		  AND last_attempt_id = $4
	`, claim.EventID, claim.Sink, claim.Generation, claim.AttemptID)
	if err != nil {
		return fmt.Errorf("ack billing event delivery %s/%s/%d: %w", claim.EventID, claim.Sink, claim.Generation, err)
	}
	return nil
}

func (c *Client) failBillingEventDelivery(ctx context.Context, claim billingEventDeliveryClaim, cause error) error {
	now := c.clock().UTC()
	state := "retryable_failed"
	deadLetteredAt := any(nil)
	deadLetterReason := ""
	nextAttemptAt := any(now.Add(billingEventDeliveryRetryDelay(claim.Attempts)))
	if claim.Attempts >= billingEventDeliveryMaxAttempts {
		state = "dead_letter"
		deadLetteredAt = now
		deadLetterReason = cause.Error()
		nextAttemptAt = now
	}
	_, err := c.pg.ExecContext(ctx, `
		UPDATE billing_event_delivery_queue
		SET state = $5,
		    next_attempt_at = $6,
		    lease_expires_at = NULL,
		    leased_by = '',
		    delivery_error = $7,
		    dead_lettered_at = $8,
		    dead_letter_reason = $9,
		    updated_at = $10
		WHERE event_id = $1
		  AND sink = $2
		  AND generation = $3
		  AND last_attempt_id = $4
	`, claim.EventID, claim.Sink, claim.Generation, claim.AttemptID,
		state, nextAttemptAt, cause.Error(), deadLetteredAt, deadLetterReason, now)
	if err != nil {
		return fmt.Errorf("mark billing event delivery %s/%s/%d failed: %w", claim.EventID, claim.Sink, claim.Generation, err)
	}
	return nil
}

func billingEventDeliveryRetryDelay(attempts int) time.Duration {
	if attempts <= 0 {
		attempts = 1
	}
	delay := billingEventDeliveryBaseBackoff
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= billingEventDeliveryMaxBackoff {
			return billingEventDeliveryMaxBackoff
		}
	}
	return delay
}

func timePtrString(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

// grantPeriodStartString and grantPeriodEndString format the period boundaries
// for billing event payloads. They keep the wire format identical to the prior
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
