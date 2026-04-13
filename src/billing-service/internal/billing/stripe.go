package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	stripeProviderEventMaxAttempts = 5
	stripeProviderEventBaseBackoff = 30 * time.Second
	stripeProviderEventMaxBackoff  = 15 * time.Minute
)

var stripeProviderEventTracer = otel.Tracer("billing-service/billing/provider-events")

type stripeProviderEventClaim struct {
	EventID   string
	EventType string
	Payload   []byte
	Attempts  int
}

func (c *Client) CreateCheckoutSession(ctx context.Context, orgID OrgID, productID string, params CheckoutParams) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var displayName string
	if err := c.pg.QueryRowContext(ctx, `
		SELECT display_name FROM products WHERE product_id = $1
	`, productID).Scan(&displayName); err != nil {
		return "", fmt.Errorf("read product for checkout: %w", err)
	}

	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	var stripeCustomerID sql.NullString
	_ = c.pg.QueryRowContext(ctx, `
		SELECT stripe_customer_id FROM orgs WHERE org_id = $1
	`, orgIDText).Scan(&stripeCustomerID)

	sessionParams := &stripe.CheckoutSessionCreateParams{
		Mode:             stripe.String(string(stripe.CheckoutSessionModePayment)),
		CustomerCreation: stripe.String("always"),
		SuccessURL:       stripe.String(params.SuccessURL),
		CancelURL:        stripe.String(params.CancelURL),
		PaymentMethodOptions: &stripe.CheckoutSessionCreatePaymentMethodOptionsParams{
			Card: &stripe.CheckoutSessionCreatePaymentMethodOptionsCardParams{
				RequestThreeDSecure: stripe.String(
					string(stripe.CheckoutSessionPaymentMethodOptionsCardRequestThreeDSecureAny),
				),
			},
		},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String("usd"),
				UnitAmount: stripe.Int64(params.AmountCents),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: stripe.String(displayName + " Credits"),
				},
			},
		}},
		PaymentIntentData: &stripe.CheckoutSessionCreatePaymentIntentDataParams{
			Metadata: map[string]string{
				"org_id":       orgIDText,
				"product_id":   productID,
				"ledger_units": strconv.FormatInt(params.AmountCents*100_000, 10),
			},
		},
	}
	if stripeCustomerID.Valid && stripeCustomerID.String != "" {
		sessionParams.Customer = stripe.String(stripeCustomerID.String)
		sessionParams.CustomerCreation = nil
	}
	sessionParams.AddMetadata("org_id", orgIDText)
	sessionParams.AddMetadata("product_id", productID)
	sessionParams.AddMetadata("ledger_units", strconv.FormatInt(params.AmountCents*100_000, 10))

	session, err := c.stripe.V1CheckoutSessions.Create(ctx, sessionParams)
	if err != nil {
		return "", fmt.Errorf("create checkout session: %w", err)
	}
	return session.URL, nil
}

func (c *Client) CreateContract(ctx context.Context, orgID OrgID, planID string, cadence BillingCadence, successURL, cancelURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if planID == "" {
		return "", fmt.Errorf("plan_id is required")
	}
	if cadence == "" {
		cadence = CadenceMonthly
	}
	if cadence != CadenceMonthly {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedCadence, cadence)
	}

	var productID string
	var displayName string
	var currency string
	if err := c.pg.QueryRowContext(ctx, `
		SELECT p.product_id, p.display_name, p.currency
		FROM plans p
		WHERE p.plan_id = $1
		  AND p.active
	`, planID).Scan(&productID, &displayName, &currency); err != nil {
		return "", fmt.Errorf("read plan for contract: %w", err)
	}
	if currency == "" {
		currency = "usd"
	}

	contractID := deterministicTextID("self-serve-contract", strconv.FormatUint(uint64(orgID), 10), productID)
	phaseID := deterministicTextID("self-serve-contract-phase", contractID, planID)

	customerID, err := c.ensureStripeCustomer(ctx, orgID)
	if err != nil {
		return "", err
	}

	metadata := map[string]string{
		"org_id":      strconv.FormatUint(uint64(orgID), 10),
		"product_id":  productID,
		"plan_id":     planID,
		"contract_id": contractID,
		"phase_id":    phaseID,
		"cadence":     string(cadence),
	}
	sessionParams := &stripe.CheckoutSessionCreateParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSetup)),
		Customer:   stripe.String(customerID),
		Currency:   stripe.String(currency),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		SetupIntentData: &stripe.CheckoutSessionCreateSetupIntentDataParams{
			Description: stripe.String(displayName + " payment method"),
			Metadata:    metadata,
		},
		Metadata: metadata,
	}

	session, err := c.stripe.V1CheckoutSessions.Create(ctx, sessionParams)
	if err != nil {
		return "", fmt.Errorf("create contract payment-method checkout session: %w", err)
	}
	return session.URL, nil
}

func (c *Client) ensureStripeCustomer(ctx context.Context, orgID OrgID) (string, error) {
	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	var stripeCustomerID sql.NullString
	var billingEmail sql.NullString
	if err := c.pg.QueryRowContext(ctx, `
		SELECT stripe_customer_id, billing_email FROM orgs WHERE org_id = $1
	`, orgIDText).Scan(&stripeCustomerID, &billingEmail); err != nil {
		return "", fmt.Errorf("lookup org stripe customer: %w", err)
	}
	if stripeCustomerID.Valid && stripeCustomerID.String != "" {
		return stripeCustomerID.String, nil
	}

	params := &stripe.CustomerCreateParams{
		Metadata: map[string]string{"org_id": orgIDText},
	}
	params.SetIdempotencyKey("forge-metal:stripe-customer:" + orgIDText)
	if billingEmail.Valid && billingEmail.String != "" {
		params.Email = stripe.String(billingEmail.String)
	}
	customer, err := c.stripe.V1Customers.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}
	if _, err := c.pg.ExecContext(ctx, `
		UPDATE orgs
		SET stripe_customer_id = $2,
		    updated_at = now()
		WHERE org_id = $1
	`, orgIDText, customer.ID); err != nil {
		return "", fmt.Errorf("persist stripe customer: %w", err)
	}
	return customer.ID, nil
}

func (c *Client) CreatePortalSession(ctx context.Context, orgID OrgID, returnURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	var stripeCustomerID sql.NullString
	_ = c.pg.QueryRowContext(ctx, `
		SELECT stripe_customer_id FROM orgs WHERE org_id = $1
	`, orgIDText).Scan(&stripeCustomerID)
	if !stripeCustomerID.Valid || stripeCustomerID.String == "" {
		return "", ErrNoStripeCustomer
	}

	session, err := c.stripe.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(stripeCustomerID.String),
		ReturnURL: stripe.String(returnURL),
	})
	if err != nil {
		return "", fmt.Errorf("create portal session: %w", err)
	}
	return session.URL, nil
}

func (c *Client) HandleStripeWebhook(ctx context.Context, payload []byte, signatureHeader string, webhookSecret string) error {
	ctx, span := stripeProviderEventTracer.Start(ctx, "billing.stripe.webhook.receive")
	defer span.End()

	event, err := webhook.ConstructEvent(payload, signatureHeader, webhookSecret)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("construct stripe webhook event: %w", err)
	}
	span.SetAttributes(
		attribute.String("stripe.event_id", event.ID),
		attribute.String("stripe.event_type", string(event.Type)),
	)

	_, err = c.recordStripeProviderEvent(ctx, event, payload)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (c *Client) ApplyPendingProviderEvents(ctx context.Context, limit int) (int, error) {
	ctx, span := stripeProviderEventTracer.Start(ctx, "billing.provider_event.apply_pending")
	defer span.End()

	if limit <= 0 {
		limit = 100
	}
	span.SetAttributes(attribute.Int("billing.provider_event.limit", limit))

	rows, err := c.pg.QueryContext(ctx, `
		SELECT event_id
		FROM billing_provider_events
		WHERE provider = 'stripe'
		  AND state IN ('received', 'queued', 'failed')
		  AND COALESCE(next_attempt_at, received_at) <= now()
		ORDER BY COALESCE(next_attempt_at, received_at), event_id
		LIMIT $1
	`, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return 0, fmt.Errorf("query pending stripe provider events: %w", err)
	}
	defer rows.Close()

	var eventIDs []string
	for rows.Next() {
		var eventID string
		if err := rows.Scan(&eventID); err != nil {
			return 0, fmt.Errorf("scan pending stripe provider event: %w", err)
		}
		eventIDs = append(eventIDs, eventID)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate pending stripe provider events: %w", err)
	}

	applied := 0
	for _, eventID := range eventIDs {
		didApply, err := c.ApplyProviderEvent(ctx, eventID)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return applied, err
		}
		if didApply {
			applied++
		}
	}
	span.SetAttributes(attribute.Int("billing.provider_event.applied_count", applied))
	return applied, nil
}

func (c *Client) ApplyProviderEvent(ctx context.Context, eventID string) (bool, error) {
	ctx, span := stripeProviderEventTracer.Start(ctx, "billing.provider_event.apply")
	defer span.End()
	span.SetAttributes(attribute.String("billing.provider_event.event_id", eventID))

	claim, ok, err := c.claimStripeProviderEvent(ctx, eventID)
	if err != nil || !ok {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(attribute.Bool("billing.provider_event.applied", false))
		return false, err
	}
	span.SetAttributes(
		attribute.String("billing.provider_event.event_type", claim.EventType),
		attribute.Int("billing.provider_event.attempts", claim.Attempts),
	)

	var event stripe.Event
	if err := json.Unmarshal(claim.Payload, &event); err != nil {
		if markErr := c.failStripeProviderEvent(ctx, claim, fmt.Errorf("decode stripe provider event: %w", err)); markErr != nil {
			span.RecordError(markErr)
			span.SetStatus(codes.Error, markErr.Error())
			return true, markErr
		}
		span.RecordError(err)
		return true, nil
	}

	var handleErr error
	finalState := "applied"
	switch event.Type {
	case "checkout.session.completed":
		handleErr = c.handleCheckoutSessionCompleted(ctx, claim.EventID, event)
	case "setup_intent.succeeded":
		handleErr = c.handleSetupIntentSucceeded(ctx, claim.EventID, event)
	case "payment_intent.succeeded":
		handleErr = c.handlePaymentIntentSucceeded(ctx, event)
	case "invoice.paid":
		handleErr = c.handleInvoicePaid(ctx, event)
	case "invoice.payment_failed":
		handleErr = c.handleInvoicePaymentFailed(ctx, event)
	default:
		finalState = "ignored"
	}
	if handleErr != nil {
		if markErr := c.failStripeProviderEvent(ctx, claim, handleErr); markErr != nil {
			span.RecordError(markErr)
			span.SetStatus(codes.Error, markErr.Error())
			return true, fmt.Errorf("apply stripe provider event %s: %w; mark failure: %v", claim.EventID, handleErr, markErr)
		}
		span.RecordError(handleErr)
		span.SetAttributes(attribute.String("billing.provider_event.state", "failed"))
		return true, nil
	}
	if err := c.markStripeProviderEventFinal(ctx, claim.EventID, finalState); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return true, err
	}
	span.SetAttributes(
		attribute.Bool("billing.provider_event.applied", finalState == "applied"),
		attribute.String("billing.provider_event.state", finalState),
	)
	return true, nil
}

func (c *Client) recordStripeProviderEvent(ctx context.Context, event stripe.Event, payload []byte) (string, error) {
	if event.ID == "" {
		return "", fmt.Errorf("stripe event id is required")
	}
	eventID := deterministicTextID("stripe-provider-event", event.ID)
	annotation := stripeRawProviderEventAnnotation(event)
	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin stripe provider event record: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO billing_provider_events (
			event_id, provider, provider_event_id, event_type, state, livemode,
			provider_created_at, next_attempt_at, org_id, product_id, contract_id,
			invoice_id, provider_customer_id, provider_invoice_id, provider_payment_intent_id,
			provider_object_type, provider_object_id, payload
		)
		VALUES ($1, 'stripe', $2, $3, 'queued', $4, $5, $6,
		        $7, $8, $9,
		        $10, $11, $12, $13,
		        $14, $15, $16::jsonb)
		ON CONFLICT (provider, provider_event_id) DO UPDATE
		SET event_type = EXCLUDED.event_type,
		    livemode = EXCLUDED.livemode,
		    provider_created_at = EXCLUDED.provider_created_at,
		    payload = EXCLUDED.payload,
		    state = CASE
		        WHEN billing_provider_events.state IN ('applying', 'applied', 'ignored', 'dead_letter')
		            THEN billing_provider_events.state
		        ELSE 'queued'
		    END,
		    next_attempt_at = CASE
		        WHEN billing_provider_events.state IN ('applying', 'applied', 'ignored', 'dead_letter')
		            THEN billing_provider_events.next_attempt_at
		        ELSE EXCLUDED.next_attempt_at
		    END,
		    org_id = CASE WHEN billing_provider_events.org_id = '' THEN EXCLUDED.org_id ELSE billing_provider_events.org_id END,
		    product_id = CASE WHEN billing_provider_events.product_id = '' THEN EXCLUDED.product_id ELSE billing_provider_events.product_id END,
		    contract_id = CASE WHEN billing_provider_events.contract_id = '' THEN EXCLUDED.contract_id ELSE billing_provider_events.contract_id END,
		    invoice_id = CASE WHEN billing_provider_events.invoice_id = '' THEN EXCLUDED.invoice_id ELSE billing_provider_events.invoice_id END,
		    provider_customer_id = CASE WHEN billing_provider_events.provider_customer_id = '' THEN EXCLUDED.provider_customer_id ELSE billing_provider_events.provider_customer_id END,
		    provider_invoice_id = CASE WHEN billing_provider_events.provider_invoice_id = '' THEN EXCLUDED.provider_invoice_id ELSE billing_provider_events.provider_invoice_id END,
		    provider_payment_intent_id = CASE WHEN billing_provider_events.provider_payment_intent_id = '' THEN EXCLUDED.provider_payment_intent_id ELSE billing_provider_events.provider_payment_intent_id END,
		    provider_object_type = CASE WHEN billing_provider_events.provider_object_type = '' THEN EXCLUDED.provider_object_type ELSE billing_provider_events.provider_object_type END,
		    provider_object_id = CASE WHEN billing_provider_events.provider_object_id = '' THEN EXCLUDED.provider_object_id ELSE billing_provider_events.provider_object_id END,
		    updated_at = now()
	`, eventID, event.ID, string(event.Type), event.Livemode, time.Unix(event.Created, 0).UTC(), c.clock().UTC(),
		annotation.OrgID, annotation.ProductID, annotation.ContractID,
		annotation.InvoiceID, annotation.ProviderCustomer, annotation.ProviderInvoiceID, annotation.ProviderPaymentIntentID,
		annotation.ObjectType, annotation.ObjectID, string(payload))
	if err != nil {
		return "", fmt.Errorf("record stripe provider event %s: %w", event.ID, err)
	}
	if annotation.OrgID != "" {
		if err := insertBillingEventTx(ctx, tx, providerEventReceivedEvent(eventID, event, annotation)); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit stripe provider event record: %w", err)
	}
	return eventID, nil
}

func stripeRawProviderEventAnnotation(event stripe.Event) providerEventAnnotation {
	object := map[string]any{}
	if event.Data != nil && len(event.Data.Raw) > 0 {
		_ = json.Unmarshal(event.Data.Raw, &object)
	}
	metadata := stringMapFromAny(object["metadata"])
	annotation := providerEventAnnotation{
		OrgID:            metadata["org_id"],
		ProductID:        metadata["product_id"],
		ContractID:       metadata["contract_id"],
		InvoiceID:        metadata["invoice_id"],
		ProviderCustomer: stringValue(object["customer"]),
		ObjectType:       providerObjectTypeFromStripeEventType(string(event.Type)),
		ObjectID:         stringValue(object["id"]),
		Normalized: map[string]string{
			"stripe_event_id":   event.ID,
			"stripe_event_type": string(event.Type),
			"object_id":         stringValue(object["id"]),
		},
	}
	switch string(event.Type) {
	case "invoice.finalized", "invoice.paid", "invoice.payment_failed":
		annotation.ProviderInvoiceID = stringValue(object["id"])
		annotation.ProviderPaymentIntentID = stringValue(object["payment_intent"])
	case "payment_intent.succeeded", "payment_intent.payment_failed":
		annotation.ProviderPaymentIntentID = stringValue(object["id"])
	case "checkout.session.completed":
		annotation.ProviderPaymentIntentID = stringValue(object["payment_intent"])
	}
	return annotation
}

func providerObjectTypeFromStripeEventType(eventType string) string {
	if eventType == "" {
		return ""
	}
	parts := strings.Split(eventType, ".")
	if len(parts) <= 1 {
		return eventType
	}
	return strings.Join(parts[:len(parts)-1], ".")
}

func stringMapFromAny(value any) map[string]string {
	out := map[string]string{}
	switch typed := value.(type) {
	case map[string]any:
		for key, raw := range typed {
			if str := stringValue(raw); str != "" {
				out[key] = str
			}
		}
	case map[string]string:
		for key, str := range typed {
			if str != "" {
				out[key] = str
			}
		}
	}
	return out
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case map[string]any:
		return stringValue(typed["id"])
	default:
		return ""
	}
}

func providerEventReceivedEvent(eventID string, event stripe.Event, annotation providerEventAnnotation) billingEventFact {
	occurredAt := time.Now().UTC()
	if event.Created > 0 {
		occurredAt = time.Unix(event.Created, 0).UTC()
	}
	payload, _ := json.Marshal(map[string]string{
		"provider":                   "stripe",
		"event_id":                   eventID,
		"provider_event_id":          event.ID,
		"provider_event_type":        string(event.Type),
		"org_id":                     annotation.OrgID,
		"product_id":                 annotation.ProductID,
		"contract_id":                annotation.ContractID,
		"invoice_id":                 annotation.InvoiceID,
		"provider_customer_id":       annotation.ProviderCustomer,
		"provider_invoice_id":        annotation.ProviderInvoiceID,
		"provider_payment_intent_id": annotation.ProviderPaymentIntentID,
		"provider_object_type":       annotation.ObjectType,
		"provider_object_id":         annotation.ObjectID,
		"occurred_at":                occurredAt.Format(time.RFC3339Nano),
	})
	return billingEventFact{
		EventID:       deterministicTextID("billing-event", "provider_event_received", eventID),
		EventType:     "provider_event_received",
		EventVersion:  billingEventCurrentVersion,
		AggregateType: "provider_event",
		AggregateID:   eventID,
		OrgID:         annotation.OrgID,
		ProductID:     annotation.ProductID,
		OccurredAt:    occurredAt,
		Payload:       payload,
	}
}

func providerEventAppliedEvent(eventID string, providerEventID string, providerEventType string, state string, annotation providerEventAnnotation, occurredAt time.Time) billingEventFact {
	occurredAt = occurredAt.UTC()
	eventType := "provider_event_applied"
	payload, _ := json.Marshal(map[string]string{
		"provider":                   "stripe",
		"event_id":                   eventID,
		"provider_event_id":          providerEventID,
		"provider_event_type":        providerEventType,
		"provider_event_state":       state,
		"org_id":                     annotation.OrgID,
		"product_id":                 annotation.ProductID,
		"contract_id":                annotation.ContractID,
		"invoice_id":                 annotation.InvoiceID,
		"provider_customer_id":       annotation.ProviderCustomer,
		"provider_invoice_id":        annotation.ProviderInvoiceID,
		"provider_payment_intent_id": annotation.ProviderPaymentIntentID,
		"provider_object_type":       annotation.ObjectType,
		"provider_object_id":         annotation.ObjectID,
		"occurred_at":                occurredAt.Format(time.RFC3339Nano),
	})
	return billingEventFact{
		EventID:       deterministicTextID("billing-event", eventType, eventID, state),
		EventType:     eventType,
		EventVersion:  billingEventCurrentVersion,
		AggregateType: "provider_event",
		AggregateID:   eventID,
		OrgID:         annotation.OrgID,
		ProductID:     annotation.ProductID,
		OccurredAt:    occurredAt,
		Payload:       payload,
	}
}

func (c *Client) claimStripeProviderEvent(ctx context.Context, eventID string) (stripeProviderEventClaim, bool, error) {
	if strings.TrimSpace(eventID) == "" {
		return stripeProviderEventClaim{}, false, fmt.Errorf("provider event id is required")
	}
	now := c.clock().UTC()
	var claim stripeProviderEventClaim
	var payload string
	err := c.pg.QueryRowContext(ctx, `
		UPDATE billing_provider_events
		SET state = 'applying',
		    attempts = attempts + 1,
		    next_attempt_at = NULL,
		    last_error = '',
		    updated_at = $2
		WHERE event_id = $1
		  AND provider = 'stripe'
		  AND state IN ('received', 'queued', 'failed')
		  AND COALESCE(next_attempt_at, received_at) <= $2
		RETURNING event_id, event_type, payload::text, attempts
	`, eventID, now).Scan(&claim.EventID, &claim.EventType, &payload, &claim.Attempts)
	if err == sql.ErrNoRows {
		return stripeProviderEventClaim{}, false, nil
	}
	if err != nil {
		return stripeProviderEventClaim{}, false, fmt.Errorf("claim stripe provider event %s: %w", eventID, err)
	}
	claim.Payload = []byte(payload)
	return claim, true, nil
}

func (c *Client) markStripeProviderEventFinal(ctx context.Context, eventID string, state string) error {
	if eventID == "" {
		return nil
	}
	if state != "applied" && state != "ignored" {
		return fmt.Errorf("invalid final stripe provider event state %q", state)
	}
	now := c.clock().UTC()
	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin stripe provider event final mark: %w", err)
	}
	defer tx.Rollback()

	var annotation providerEventAnnotation
	var providerEventID string
	var eventType string
	err = tx.QueryRowContext(ctx, `
		UPDATE billing_provider_events
		SET state = $2,
		    applied_at = $3,
		    last_error = '',
		    next_attempt_at = NULL,
		    updated_at = $3
		WHERE event_id = $1
		  AND state = 'applying'
		RETURNING provider_event_id, event_type, org_id, product_id, contract_id, invoice_id,
		          provider_customer_id, provider_invoice_id, provider_payment_intent_id,
		          provider_object_type, provider_object_id
	`, eventID, state, now).Scan(
		&providerEventID,
		&eventType,
		&annotation.OrgID,
		&annotation.ProductID,
		&annotation.ContractID,
		&annotation.InvoiceID,
		&annotation.ProviderCustomer,
		&annotation.ProviderInvoiceID,
		&annotation.ProviderPaymentIntentID,
		&annotation.ObjectType,
		&annotation.ObjectID,
	)
	if err == sql.ErrNoRows {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit skipped stripe provider event final mark: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("mark stripe provider event %s final: %w", eventID, err)
	}
	if annotation.OrgID != "" {
		if err := insertBillingEventTx(ctx, tx, providerEventAppliedEvent(eventID, providerEventID, eventType, state, annotation, now)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit stripe provider event final mark: %w", err)
	}
	return nil
}

func (c *Client) failStripeProviderEvent(ctx context.Context, claim stripeProviderEventClaim, cause error) error {
	if claim.EventID == "" {
		return nil
	}
	now := c.clock().UTC()
	state := "failed"
	var nextAttemptAt any = now.Add(stripeProviderEventRetryDelay(claim.Attempts))
	if claim.Attempts >= stripeProviderEventMaxAttempts {
		state = "dead_letter"
		nextAttemptAt = nil
	}
	_, err := c.pg.ExecContext(ctx, `
		UPDATE billing_provider_events
		SET state = $2,
		    next_attempt_at = $3,
		    last_error = $4,
		    updated_at = $5
		WHERE event_id = $1
		  AND state = 'applying'
	`, claim.EventID, state, nextAttemptAt, cause.Error(), now)
	if err != nil {
		return fmt.Errorf("mark stripe provider event %s failed: %w", claim.EventID, err)
	}
	return nil
}

func stripeProviderEventRetryDelay(attempts int) time.Duration {
	if attempts <= 0 {
		attempts = 1
	}
	delay := stripeProviderEventBaseBackoff
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= stripeProviderEventMaxBackoff {
			return stripeProviderEventMaxBackoff
		}
	}
	return delay
}

func (c *Client) handleCheckoutSessionCompleted(ctx context.Context, providerEventID string, event stripe.Event) error {
	session, err := decodeStripeEventObject[stripe.CheckoutSession](event, "checkout.session.completed")
	if err != nil {
		return err
	}
	if err := c.annotateCheckoutSessionProviderEvent(ctx, providerEventID, session); err != nil {
		return err
	}

	customerID := stripeCustomerID(session.Customer)
	orgIDText := session.Metadata["org_id"]
	if customerID != "" && orgIDText != "" {
		_, err := c.pg.ExecContext(ctx, `
			UPDATE orgs
			SET stripe_customer_id = $1,
			    updated_at = now()
			WHERE org_id = $2
		`, customerID, orgIDText)
		if err != nil {
			return fmt.Errorf("checkout.session.completed: update customer: %w", err)
		}
	}
	if session.SetupIntent != nil && session.SetupIntent.ID != "" {
		intent, err := c.retrieveCheckoutSetupIntent(ctx, session)
		if err != nil {
			return err
		}
		return c.upsertPaymentMethodFromSetupIntent(ctx, intent)
	}
	return nil
}

func (c *Client) handleSetupIntentSucceeded(ctx context.Context, providerEventID string, event stripe.Event) error {
	intent, err := decodeStripeEventObject[stripe.SetupIntent](event, "setup_intent.succeeded")
	if err != nil {
		return err
	}
	if err := c.annotateSetupIntentProviderEvent(ctx, providerEventID, intent); err != nil {
		return err
	}
	return c.upsertPaymentMethodFromSetupIntent(ctx, intent)
}

func (c *Client) retrieveCheckoutSetupIntent(ctx context.Context, session stripe.CheckoutSession) (stripe.SetupIntent, error) {
	params := &stripe.SetupIntentRetrieveParams{}
	params.AddExpand("payment_method")
	intent, err := c.stripe.V1SetupIntents.Retrieve(ctx, session.SetupIntent.ID, params)
	if err != nil {
		return stripe.SetupIntent{}, fmt.Errorf("checkout.session.completed: retrieve setup intent: %w", err)
	}
	if intent == nil {
		return stripe.SetupIntent{}, fmt.Errorf("checkout.session.completed: missing setup intent %s", session.SetupIntent.ID)
	}
	if intent.Metadata == nil {
		intent.Metadata = map[string]string{}
	}
	for key, value := range session.Metadata {
		if intent.Metadata[key] == "" {
			intent.Metadata[key] = value
		}
	}
	return *intent, nil
}

func (c *Client) annotateCheckoutSessionProviderEvent(ctx context.Context, eventID string, session stripe.CheckoutSession) error {
	setupIntentID := ""
	if session.SetupIntent != nil {
		setupIntentID = session.SetupIntent.ID
	}
	normalized := map[string]string{
		"checkout_session_id": session.ID,
		"mode":                string(session.Mode),
		"payment_status":      string(session.PaymentStatus),
		"setup_intent_id":     setupIntentID,
	}
	return c.annotateStripeProviderEvent(ctx, eventID, providerEventAnnotation{
		OrgID:            session.Metadata["org_id"],
		ProductID:        session.Metadata["product_id"],
		ContractID:       session.Metadata["contract_id"],
		ProviderCustomer: stripeCustomerID(session.Customer),
		ObjectType:       "checkout.session",
		ObjectID:         session.ID,
		Normalized:       normalized,
	})
}

func (c *Client) annotateSetupIntentProviderEvent(ctx context.Context, eventID string, intent stripe.SetupIntent) error {
	paymentMethodID := ""
	if intent.PaymentMethod != nil {
		paymentMethodID = intent.PaymentMethod.ID
	}
	normalized := map[string]string{
		"setup_intent_id":   intent.ID,
		"payment_method_id": paymentMethodID,
		"status":            string(intent.Status),
	}
	return c.annotateStripeProviderEvent(ctx, eventID, providerEventAnnotation{
		OrgID:            intent.Metadata["org_id"],
		ProductID:        intent.Metadata["product_id"],
		ContractID:       intent.Metadata["contract_id"],
		ProviderCustomer: stripeCustomerID(intent.Customer),
		ObjectType:       "setup_intent",
		ObjectID:         intent.ID,
		Normalized:       normalized,
	})
}

type providerEventAnnotation struct {
	OrgID                   string
	ProductID               string
	ContractID              string
	InvoiceID               string
	ProviderCustomer        string
	ProviderInvoiceID       string
	ProviderPaymentIntentID string
	ObjectType              string
	ObjectID                string
	Normalized              map[string]string
}

func (c *Client) annotateStripeProviderEvent(ctx context.Context, eventID string, annotation providerEventAnnotation) error {
	if eventID == "" {
		return nil
	}
	normalized, err := json.Marshal(annotation.Normalized)
	if err != nil {
		return fmt.Errorf("marshal stripe provider event annotation: %w", err)
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_provider_events
		SET org_id = $2,
		    product_id = $3,
		    contract_id = $4,
		    invoice_id = $5,
		    provider_customer_id = $6,
		    provider_invoice_id = $7,
		    provider_payment_intent_id = $8,
		    provider_object_type = $9,
		    provider_object_id = $10,
		    normalized_payload = $11::jsonb,
		    updated_at = now()
		WHERE event_id = $1
	`, eventID, annotation.OrgID, annotation.ProductID, annotation.ContractID, annotation.InvoiceID,
		annotation.ProviderCustomer, annotation.ProviderInvoiceID, annotation.ProviderPaymentIntentID,
		annotation.ObjectType, annotation.ObjectID, string(normalized))
	if err != nil {
		return fmt.Errorf("annotate stripe provider event %s: %w", eventID, err)
	}
	return nil
}

func (c *Client) upsertPaymentMethodFromSetupIntent(ctx context.Context, intent stripe.SetupIntent) error {
	orgIDText := intent.Metadata["org_id"]
	if orgIDText == "" || intent.PaymentMethod == nil || intent.PaymentMethod.ID == "" {
		return nil
	}
	customerID := stripeCustomerID(intent.Customer)
	paymentMethodID := deterministicTextID("stripe-payment-method", intent.PaymentMethod.ID)
	brand := ""
	last4 := ""
	var expMonth any = nil
	var expYear any = nil
	if intent.PaymentMethod.Card != nil {
		brand = string(intent.PaymentMethod.Card.Brand)
		last4 = intent.PaymentMethod.Card.Last4
		if intent.PaymentMethod.Card.ExpMonth != 0 {
			expMonth = intent.PaymentMethod.Card.ExpMonth
		}
		if intent.PaymentMethod.Card.ExpYear != 0 {
			expYear = intent.PaymentMethod.Card.ExpYear
		}
	}
	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin stripe payment method upsert: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE payment_methods
		SET is_default = false,
		    updated_at = now()
		WHERE org_id = $1
		  AND is_default
	`, orgIDText); err != nil {
		return fmt.Errorf("clear prior default stripe payment method: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO payment_methods (
			payment_method_id, org_id, provider, provider_payment_method_id,
			provider_customer_id, status, is_default, card_brand, card_last4,
			card_exp_month, card_exp_year
		)
		VALUES ($1, $2, 'stripe', $3, $4, 'active', true, $5, $6, $7, $8)
		ON CONFLICT (provider, provider_payment_method_id) WHERE provider_payment_method_id <> '' DO UPDATE
		SET status = 'active',
		    is_default = true,
		    provider_customer_id = EXCLUDED.provider_customer_id,
		    card_brand = EXCLUDED.card_brand,
		    card_last4 = EXCLUDED.card_last4,
		    card_exp_month = EXCLUDED.card_exp_month,
		    card_exp_year = EXCLUDED.card_exp_year,
		    updated_at = now()
	`, paymentMethodID, orgIDText, intent.PaymentMethod.ID, customerID, brand, last4, expMonth, expYear)
	if err != nil {
		return fmt.Errorf("upsert stripe payment method: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit stripe payment method upsert: %w", err)
	}

	contractID := intent.Metadata["contract_id"]
	phaseID := intent.Metadata["phase_id"]
	planID := intent.Metadata["plan_id"]
	productID := intent.Metadata["product_id"]
	cadence := intent.Metadata["cadence"]
	if contractID == "" || phaseID == "" || planID == "" || productID == "" {
		return nil
	}
	rawOrgID, err := strconv.ParseUint(orgIDText, 10, 64)
	if err != nil {
		return fmt.Errorf("setup_intent.succeeded: parse org id: %w", err)
	}
	effectiveAt := setupIntentEffectiveAt(intent, c.clock().UTC())
	if err := c.activateCatalogContract(ctx, OrgID(rawOrgID), productID, planID, contractID, phaseID, cadence, effectiveAt, PaymentPaid); err != nil {
		return fmt.Errorf("activate contract from setup intent: %w", err)
	}
	return nil
}

func setupIntentEffectiveAt(intent stripe.SetupIntent, fallback time.Time) time.Time {
	if intent.Created > 0 {
		return time.Unix(intent.Created, 0).UTC()
	}
	return fallback.UTC()
}

func (c *Client) handlePaymentIntentSucceeded(ctx context.Context, event stripe.Event) error {
	intent, err := decodeStripeEventObject[stripe.PaymentIntent](event, "payment_intent.succeeded")
	if err != nil {
		return err
	}

	paymentIntentID := intent.ID
	orgIDText := intent.Metadata["org_id"]
	productID := intent.Metadata["product_id"]
	ledgerUnitsText := intent.Metadata["ledger_units"]
	if paymentIntentID == "" || orgIDText == "" || productID == "" || ledgerUnitsText == "" {
		return nil
	}

	orgID, err := strconv.ParseUint(orgIDText, 10, 64)
	if err != nil {
		return fmt.Errorf("payment_intent.succeeded: parse org id: %w", err)
	}
	ledgerUnits, err := strconv.ParseUint(ledgerUnitsText, 10, 64)
	if err != nil {
		return fmt.Errorf("payment_intent.succeeded: parse ledger units: %w", err)
	}
	// Prepaid account credit is modeled as a debit-card balance, not a coupon:
	// it must not appear to expire in the customer-facing UI.
	expiresAt := c.clock().UTC().AddDate(100, 0, 0)
	_, err = c.DepositCredits(ctx, CreditGrant{
		OrgID:             OrgID(orgID),
		ScopeType:         GrantScopeAccount,
		Amount:            ledgerUnits,
		Source:            "purchase",
		SourceReferenceID: "stripe:payment_intent:" + paymentIntentID,
		ExpiresAt:         &expiresAt,
	})
	if err != nil {
		return fmt.Errorf("payment_intent.succeeded: deposit credits: %w", err)
	}
	return nil
}

func (c *Client) handleInvoicePaid(ctx context.Context, event stripe.Event) error {
	invoice, err := decodeStripeEventObject[stripe.Invoice](event, "invoice.paid")
	if err != nil {
		return err
	}
	if invoice.ID == "" {
		return nil
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_invoices
		SET payment_status = 'paid',
		    status = CASE WHEN status = 'issued' THEN 'paid' ELSE status END,
		    updated_at = now()
		WHERE stripe_invoice_id = $1
	`, invoice.ID)
	if err != nil {
		return fmt.Errorf("invoice.paid: update billing invoice: %w", err)
	}
	return nil
}

func (c *Client) handleInvoicePaymentFailed(ctx context.Context, event stripe.Event) error {
	invoice, err := decodeStripeEventObject[stripe.Invoice](event, "invoice.payment_failed")
	if err != nil {
		return err
	}
	if invoice.ID == "" {
		return nil
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_invoices
		SET payment_status = 'failed',
		    status = CASE WHEN status = 'issued' THEN 'payment_failed' ELSE status END,
		    updated_at = now()
		WHERE stripe_invoice_id = $1
	`, invoice.ID)
	if err != nil {
		return fmt.Errorf("invoice.payment_failed: update billing invoice: %w", err)
	}
	return nil
}

func (c *Client) contractEntitlementPolicies(ctx context.Context, planID string, periodStart time.Time, periodEnd time.Time) ([]EntitlementPolicy, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT p.policy_id, p.source, p.product_id, p.scope_type, p.scope_product_id, p.scope_bucket_id, p.scope_sku_id,
		       p.amount_units, p.cadence, p.anchor_kind, p.proration_mode, p.policy_version, p.active_from, p.active_until
		FROM plan_entitlements pe
		JOIN entitlement_policies p ON p.policy_id = pe.policy_id
		WHERE pe.plan_id = $1
		  AND p.source = 'contract'
		  AND p.active_from < $2
		  AND (p.active_until IS NULL OR p.active_until > $3)
		ORDER BY p.policy_id
	`, planID, periodEnd.UTC(), periodStart.UTC())
	if err != nil {
		return nil, fmt.Errorf("lookup plan entitlement policies: %w", err)
	}
	defer rows.Close()

	var out []EntitlementPolicy
	for rows.Next() {
		var policy EntitlementPolicy
		var sourceText, scopeText string
		var amount int64
		var activeUntil sql.NullTime
		if err := rows.Scan(
			&policy.PolicyID,
			&sourceText,
			&policy.ProductID,
			&scopeText,
			&policy.ScopeProductID,
			&policy.ScopeBucketID,
			&policy.ScopeSKUID,
			&amount,
			&policy.Cadence,
			&policy.AnchorKind,
			&policy.ProrationMode,
			&policy.PolicyVersion,
			&policy.ActiveFrom,
			&activeUntil,
		); err != nil {
			return nil, fmt.Errorf("scan plan entitlement policy: %w", err)
		}
		source, err := ParseGrantSourceType(sourceText)
		if err != nil {
			return nil, err
		}
		scope, err := ParseGrantScopeType(scopeText)
		if err != nil {
			return nil, err
		}
		if amount < 0 {
			return nil, fmt.Errorf("policy %s has negative amount", policy.PolicyID)
		}
		policy.Source = source
		policy.ScopeType = scope
		policy.AmountUnits = uint64(amount)
		policy.ActiveFrom = policy.ActiveFrom.UTC()
		if activeUntil.Valid {
			value := activeUntil.Time.UTC()
			policy.ActiveUntil = &value
		}
		if err := validateGrantScope(policy.ScopeType, policy.ScopeProductID, policy.ScopeBucketID, policy.ScopeSKUID); err != nil {
			return nil, fmt.Errorf("policy %s: %w", policy.PolicyID, err)
		}
		out = append(out, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan entitlement policies: %w", err)
	}
	return out, nil
}

func decodeStripeEventObject[T any](event stripe.Event, eventType string) (T, error) {
	var out T
	if event.Data == nil || len(event.Data.Raw) == 0 {
		return out, fmt.Errorf("%s: missing Stripe event object", eventType)
	}
	if err := json.Unmarshal(event.Data.Raw, &out); err != nil {
		return out, fmt.Errorf("%s: decode Stripe event object: %w", eventType, err)
	}
	return out, nil
}

func stripeCustomerID(customer *stripe.Customer) string {
	if customer == nil {
		return ""
	}
	return customer.ID
}
