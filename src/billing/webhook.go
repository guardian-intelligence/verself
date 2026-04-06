package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/stripe/stripe-go/v85"
)

// WebhookHandler returns an http.Handler that receives Stripe webhook events,
// verifies signatures, and persists task rows for async processing.
//
// The handler itself does minimal work: it parses the event, performs any
// synchronous side effects (customer correlation, dunning updates, subscription
// cancellation), enqueues a task row for async work, and returns 200 to Stripe.
//
// Returns 400 on bad signature, 500 on persistence failure (so Stripe retries).
func (c *Client) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16)) // 64KB max
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		event, err := VerifyWebhook(body, r.Header.Get("Stripe-Signature"), c.cfg.StripeWebhookSecret)
		if err != nil {
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		if err := c.handleWebhookEvent(ctx, event); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})
}

func (c *Client) handleWebhookEvent(ctx context.Context, event stripe.Event) error {
	switch event.Type {
	case "checkout.session.completed":
		return c.handleCheckoutSessionCompleted(ctx, event)
	case "payment_intent.succeeded":
		return c.handlePaymentIntentSucceeded(ctx, event)
	case "invoice.paid":
		return c.handleInvoicePaid(ctx, event)
	case "invoice.payment_failed":
		return c.handleInvoicePaymentFailed(ctx, event)
	case "charge.dispute.created":
		return c.handleDisputeCreated(ctx, event)
	case "customer.subscription.deleted":
		return c.handleSubscriptionDeleted(ctx, event)
	default:
		return nil // ignore unknown event types
	}
}

// handleCheckoutSessionCompleted upserts orgs.stripe_customer_id for customer
// correlation. No TigerBeetle mutation. Spec §4.6.
func (c *Client) handleCheckoutSessionCompleted(ctx context.Context, event stripe.Event) error {
	customerID := event.GetObjectValue("customer")
	orgIDStr := event.GetObjectValue("metadata", "org_id")
	if customerID == "" || orgIDStr == "" {
		return nil // no correlation possible
	}

	result, err := c.pg.ExecContext(ctx, `
		UPDATE orgs
		SET stripe_customer_id = $1
		WHERE org_id = $2
		  AND (stripe_customer_id IS NULL OR stripe_customer_id != $1)
	`, customerID, orgIDStr)
	if err != nil {
		return fmt.Errorf("checkout.session.completed: upsert customer: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		// First correlation — log billing event.
		_, _ = c.pg.ExecContext(ctx, `
			INSERT INTO billing_events (org_id, event_type, stripe_event_id, payload)
			VALUES ($1, 'subscription_created', $2, $3::jsonb)
		`,
			orgIDStr,
			event.ID,
			mustJSON(map[string]interface{}{
				"stripe_customer_id": customerID,
				"session_mode":       event.GetObjectValue("mode"),
			}),
		)
	}

	return nil
}

// handlePaymentIntentSucceeded enqueues a stripe_purchase_deposit task.
// Spec §4.6: idempotency_key = payment_intent.id.
func (c *Client) handlePaymentIntentSucceeded(ctx context.Context, event stripe.Event) error {
	piID := event.GetObjectValue("id")
	if piID == "" {
		return fmt.Errorf("payment_intent.succeeded: missing payment_intent id")
	}

	// The metadata comes from the Checkout Session, which Stripe copies to the
	// PaymentIntent. Read org_id and product_id from the event object.
	orgIDStr := event.GetObjectValue("metadata", "org_id")
	productID := event.GetObjectValue("metadata", "product_id")
	if orgIDStr == "" || productID == "" {
		return nil // not our Checkout session
	}

	// Amount is in cents; convert to ledger units (10^7 per USD, so cents × 10^5).
	amountCentsStr := event.GetObjectValue("amount")
	amountCents, err := strconv.ParseInt(amountCentsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("payment_intent.succeeded: parse amount: %w", err)
	}
	amountLedgerUnits := amountCents * 100_000 // cents to ledger units

	// Default expiration: 12 months for purchases.
	expiresAt := c.clock().UTC().AddDate(1, 0, 0)

	payload := map[string]interface{}{
		"org_id":                   orgIDStr,
		"product_id":               productID,
		"stripe_payment_intent_id": piID,
		"amount_ledger_units":      amountLedgerUnits,
		"expires_at":               expiresAt.Format(time.RFC3339),
	}

	return c.enqueueTask(ctx, "stripe_purchase_deposit", piID, payload)
}

// handleInvoicePaid processes a recurring invoice success.
// Branches by products.billing_model per spec §4.6.
func (c *Client) handleInvoicePaid(ctx context.Context, event stripe.Event) error {
	invoiceID := event.GetObjectValue("id")
	stripeSubID := event.GetObjectValue("subscription")
	if invoiceID == "" || stripeSubID == "" {
		return nil // not a subscription invoice
	}

	// Look up the local subscription and product billing model.
	var (
		subscriptionID int64
		orgIDStr       string
		productID      string
		planID         string
		billingModel   string
		includedCreds  int64
		periodStart    time.Time
		periodEnd      time.Time
	)
	err := c.pg.QueryRowContext(ctx, `
		SELECT s.subscription_id, s.org_id, s.product_id, s.plan_id,
		       pr.billing_model, p.included_credits
		FROM subscriptions s
		JOIN products pr ON pr.product_id = s.product_id
		JOIN plans p ON p.plan_id = s.plan_id
		WHERE s.stripe_subscription_id = $1
	`, stripeSubID).Scan(&subscriptionID, &orgIDStr, &productID, &planID, &billingModel, &includedCreds)
	if err != nil {
		return fmt.Errorf("invoice.paid: lookup subscription: %w", err)
	}

	// Parse period from Stripe invoice.
	periodStartStr := event.GetObjectValue("period_start")
	periodEndStr := event.GetObjectValue("period_end")
	if periodStartStr != "" {
		if ts, err := strconv.ParseInt(periodStartStr, 10, 64); err == nil {
			periodStart = time.Unix(ts, 0).UTC()
		}
	}
	if periodEndStr != "" {
		if ts, err := strconv.ParseInt(periodEndStr, 10, 64); err == nil {
			periodEnd = time.Unix(ts, 0).UTC()
		}
	}

	// Update subscription period.
	if !periodStart.IsZero() && !periodEnd.IsZero() {
		_, _ = c.pg.ExecContext(ctx, `
			UPDATE subscriptions
			SET current_period_start = $1, current_period_end = $2
			WHERE subscription_id = $3
		`, periodStart, periodEnd, subscriptionID)
	}

	// Revert past_due → active on successful payment.
	_, _ = c.pg.ExecContext(ctx, `
		UPDATE subscriptions
		SET status = 'active',
		    status_changed_at = now(),
		    past_due_since = NULL
		WHERE subscription_id = $1
		  AND status = 'past_due'
	`, subscriptionID)

	// Log payment_succeeded billing event.
	amountStr := event.GetObjectValue("amount_paid")
	_, _ = c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, subscription_id, stripe_event_id, payload)
		VALUES ($1, 'payment_succeeded', $2, $3, $4::jsonb)
		ON CONFLICT (stripe_event_id) WHERE stripe_event_id IS NOT NULL DO NOTHING
	`,
		orgIDStr,
		subscriptionID,
		event.ID,
		mustJSON(map[string]interface{}{
			"stripe_invoice_id": invoiceID,
			"amount_paid":       amountStr,
		}),
	)

	// Branch by billing model.
	switch billingModel {
	case "metered":
		if includedCreds <= 0 {
			return nil // no credits to deposit
		}
		expiresAt := periodEnd.AddDate(0, 0, 30) // period_end + 30 days grace
		payload := map[string]interface{}{
			"org_id":               orgIDStr,
			"product_id":           productID,
			"subscription_id":      subscriptionID,
			"stripe_invoice_id":    invoiceID,
			"amount_ledger_units":  includedCreds,
			"period_start":         periodStart.Format(time.RFC3339),
			"period_end":           periodEnd.Format(time.RFC3339),
			"expires_at":           expiresAt.Format(time.RFC3339),
			"source":               "subscription",
		}
		return c.enqueueTask(ctx, "stripe_subscription_credit_deposit", invoiceID, payload)

	case "licensed":
		amountPaid, _ := strconv.ParseInt(amountStr, 10, 64)
		amountLedgerUnits := amountPaid * 100_000
		payload := map[string]interface{}{
			"org_id":              orgIDStr,
			"product_id":          productID,
			"subscription_id":     subscriptionID,
			"stripe_invoice_id":   invoiceID,
			"amount_ledger_units": amountLedgerUnits,
			"period_start":        periodStart.Format(time.RFC3339),
			"period_end":          periodEnd.Format(time.RFC3339),
		}
		return c.enqueueTask(ctx, "stripe_licensed_charge", invoiceID, payload)

	default:
		return nil // one_time or unknown — no task needed
	}
}

// handleInvoicePaymentFailed updates subscription status to past_due.
// No TigerBeetle mutation. Spec §4.6.
func (c *Client) handleInvoicePaymentFailed(ctx context.Context, event stripe.Event) error {
	stripeSubID := event.GetObjectValue("subscription")
	if stripeSubID == "" {
		return nil
	}

	orgIDStr := ""
	_ = c.pg.QueryRowContext(ctx, `
		SELECT org_id FROM subscriptions WHERE stripe_subscription_id = $1
	`, stripeSubID).Scan(&orgIDStr)

	_, err := c.pg.ExecContext(ctx, `
		UPDATE subscriptions
		SET status = 'past_due',
		    past_due_since = COALESCE(past_due_since, now()),
		    status_changed_at = now()
		WHERE stripe_subscription_id = $1
		  AND status IN ('active', 'trialing')
	`, stripeSubID)
	if err != nil {
		return fmt.Errorf("invoice.payment_failed: update subscription: %w", err)
	}

	// Log payment_failed billing event.
	if orgIDStr != "" {
		attemptCount := event.GetObjectValue("attempt_count")
		_, _ = c.pg.ExecContext(ctx, `
			INSERT INTO billing_events (org_id, event_type, stripe_event_id, payload)
			VALUES ($1, 'payment_failed', $2, $3::jsonb)
			ON CONFLICT (stripe_event_id) WHERE stripe_event_id IS NOT NULL DO NOTHING
		`,
			orgIDStr,
			event.ID,
			mustJSON(map[string]interface{}{
				"stripe_subscription_id": stripeSubID,
				"attempt_count":          attemptCount,
			}),
		)
	}

	return nil
}

// handleDisputeCreated enqueues a stripe_dispute_debit task.
// Spec §4.6: idempotency_key = dispute.id.
func (c *Client) handleDisputeCreated(ctx context.Context, event stripe.Event) error {
	disputeID := event.GetObjectValue("id")
	if disputeID == "" {
		return fmt.Errorf("charge.dispute.created: missing dispute id")
	}

	amountStr := event.GetObjectValue("amount")
	amount, err := strconv.ParseInt(amountStr, 10, 64)
	if err != nil {
		return fmt.Errorf("charge.dispute.created: parse amount: %w", err)
	}
	amountLedgerUnits := amount * 100_000

	piID := event.GetObjectValue("payment_intent")

	// Resolve org_id from the payment intent's charges → customer → orgs.
	// For now, try metadata or charge's customer → orgs lookup.
	orgIDStr := event.GetObjectValue("metadata", "org_id")
	if orgIDStr == "" {
		customerID := event.GetObjectValue("customer")
		if customerID != "" {
			_ = c.pg.QueryRowContext(ctx, `
				SELECT org_id FROM orgs WHERE stripe_customer_id = $1
			`, customerID).Scan(&orgIDStr)
		}
	}
	if orgIDStr == "" {
		return fmt.Errorf("charge.dispute.created: cannot resolve org_id")
	}

	payload := map[string]interface{}{
		"org_id":                   orgIDStr,
		"stripe_dispute_id":        disputeID,
		"stripe_payment_intent_id": piID,
		"amount_ledger_units":      amountLedgerUnits,
	}

	// Log dispute_opened billing event.
	_, _ = c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, stripe_event_id, payload)
		VALUES ($1, 'dispute_opened', $2, $3::jsonb)
		ON CONFLICT (stripe_event_id) WHERE stripe_event_id IS NOT NULL DO NOTHING
	`,
		orgIDStr,
		event.ID,
		mustJSON(payload),
	)

	return c.enqueueTask(ctx, "stripe_dispute_debit", disputeID, payload)
}

// handleSubscriptionDeleted marks the subscription as cancelled.
// No TigerBeetle mutation. Spec §4.6.
func (c *Client) handleSubscriptionDeleted(ctx context.Context, event stripe.Event) error {
	stripeSubID := event.GetObjectValue("id")
	if stripeSubID == "" {
		return nil
	}

	orgIDStr := ""
	_ = c.pg.QueryRowContext(ctx, `
		SELECT org_id FROM subscriptions WHERE stripe_subscription_id = $1
	`, stripeSubID).Scan(&orgIDStr)

	_, err := c.pg.ExecContext(ctx, `
		UPDATE subscriptions
		SET status = 'cancelled',
		    cancelled_at = now(),
		    status_changed_at = now()
		WHERE stripe_subscription_id = $1
	`, stripeSubID)
	if err != nil {
		return fmt.Errorf("customer.subscription.deleted: update subscription: %w", err)
	}

	if orgIDStr != "" {
		_, _ = c.pg.ExecContext(ctx, `
			INSERT INTO billing_events (org_id, event_type, stripe_event_id, payload)
			VALUES ($1, 'subscription_cancelled', $2, $3::jsonb)
			ON CONFLICT (stripe_event_id) WHERE stripe_event_id IS NOT NULL DO NOTHING
		`,
			orgIDStr,
			event.ID,
			mustJSON(map[string]interface{}{
				"stripe_subscription_id": stripeSubID,
				"reason":                 "stripe_terminal_action",
			}),
		)
	}

	return nil
}

// enqueueTask inserts a task row with idempotency_key. ON CONFLICT handles
// duplicate Stripe deliveries — the same event ID maps to the same task.
func (c *Client) enqueueTask(ctx context.Context, taskType, idempotencyKey string, payload map[string]interface{}) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("enqueue task: marshal payload: %w", err)
	}

	_, err = c.pg.ExecContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key)
		VALUES ($1, $2::jsonb, $3)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, taskType, string(payloadJSON), idempotencyKey)
	if err != nil {
		return fmt.Errorf("enqueue task %s: %w", taskType, err)
	}

	return nil
}

// claimedTask represents a task row claimed by the worker.
type claimedTask struct {
	TaskID         int64
	TaskType       string
	Payload        json.RawMessage
	Attempts       int
	MaxAttempts    int
	IdempotencyKey sql.NullString
}
