package billingruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
	tb "github.com/tigerbeetle/tigerbeetle-go"

	"github.com/forge-metal/billing"
)

const (
	defaultWebhookBodyLimit = 1 << 18
)

// App owns billing-service runtime concerns that do not belong in the billing domain package.
type App struct {
	PG                  *sql.DB
	TB                  tb.Client
	CH                  driver.Conn
	Billing             *billing.Client
	ReconcileQuerier    billing.ClickHouseQuerier
	StripeWebhookSecret string
	Logger              *slog.Logger

	clock              func() time.Time
	workerPollInterval time.Duration
	lastWorkerActivity atomic.Int64
}

func New(
	pg *sql.DB,
	tbClient tb.Client,
	chConn driver.Conn,
	billingClient *billing.Client,
	reconcileQuerier billing.ClickHouseQuerier,
	stripeWebhookSecret string,
	logger *slog.Logger,
) *App {
	app := &App{
		PG:                  pg,
		TB:                  tbClient,
		CH:                  chConn,
		Billing:             billingClient,
		ReconcileQuerier:    reconcileQuerier,
		StripeWebhookSecret: stripeWebhookSecret,
		Logger:              logger,
		clock:               time.Now,
	}
	app.markWorkerActivity()
	return app
}

func (a *App) markWorkerActivity() {
	if a == nil || a.clock == nil {
		return
	}
	a.lastWorkerActivity.Store(a.clock().UnixNano())
}

// Ready verifies that the service's runtime dependencies are healthy.
func (a *App) Ready(ctx context.Context) error {
	if a == nil {
		return fmt.Errorf("billing runtime not configured")
	}
	if a.PG == nil || a.TB == nil || a.CH == nil || a.Billing == nil {
		return fmt.Errorf("billing runtime dependencies are incomplete")
	}
	if err := a.PG.PingContext(ctx); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	if err := a.TB.Nop(); err != nil {
		return fmt.Errorf("tigerbeetle: %w", err)
	}
	if err := a.CH.Ping(ctx); err != nil {
		return fmt.Errorf("clickhouse: %w", err)
	}
	if a.workerPollInterval > 0 {
		last := time.Unix(0, a.lastWorkerActivity.Load())
		if last.IsZero() || a.clock().Sub(last) > 5*a.workerPollInterval {
			return fmt.Errorf("worker inactive since %s", last.UTC().Format(time.RFC3339))
		}
	}
	return nil
}

// RunWorker claims and executes billing tasks until ctx is cancelled.
func (a *App) RunWorker(ctx context.Context, pollInterval time.Duration) error {
	a.workerPollInterval = pollInterval
	a.markWorkerActivity()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		task, ok, err := a.claimTask(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}
		if !ok {
			a.markWorkerActivity()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pollInterval):
				continue
			}
		}

		a.markWorkerActivity()
		dispatchErr := a.dispatchTask(ctx, task)
		if dispatchErr != nil {
			if err := a.failTask(ctx, task, dispatchErr); err != nil {
				a.Logger.ErrorContext(ctx, "billing: fail task transition", "task_id", task.TaskID, "error", err)
			}
			continue
		}
		if err := a.completeTask(ctx, task); err != nil {
			a.Logger.ErrorContext(ctx, "billing: complete task transition", "task_id", task.TaskID, "error", err)
		}
	}
}

// WebhookHandler validates Stripe signatures and enqueues raw webhook tasks.
func (a *App) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, defaultWebhookBodyLimit))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		event, err := verifyWebhook(body, r.Header.Get("Stripe-Signature"), a.StripeWebhookSecret)
		if err != nil {
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		if err := a.enqueueRawTask(ctx, "stripe_webhook_event", "stripe_event:"+event.ID, body); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		a.Logger.InfoContext(r.Context(), "billing: queued stripe webhook", "stripe_event_id", event.ID, "type", event.Type)
		w.WriteHeader(http.StatusOK)
	})
}

func verifyWebhook(payload []byte, signature string, secret string) (stripe.Event, error) {
	event, err := webhook.ConstructEvent(payload, signature, secret)
	if err != nil {
		return stripe.Event{}, fmt.Errorf("verify stripe webhook: %w", err)
	}
	return event, nil
}

func (a *App) dispatchTask(ctx context.Context, task claimedTask) error {
	switch task.TaskType {
	case "stripe_webhook_event":
		return a.dispatchWebhookEvent(ctx, task)
	case "stripe_purchase_deposit":
		return a.dispatchPurchaseDeposit(ctx, task)
	case "stripe_subscription_credit_deposit":
		return a.dispatchSubscriptionCreditDeposit(ctx, task)
	case "stripe_licensed_charge":
		return a.dispatchLicensedCharge(ctx, task)
	case "stripe_dispute_debit":
		return a.dispatchDisputeDebit(ctx, task)
	case "trust_tier_evaluate":
		return a.dispatchTrustTierEvaluate(ctx, task)
	default:
		return fmt.Errorf("unknown task_type %q", task.TaskType)
	}
}

func (a *App) dispatchWebhookEvent(ctx context.Context, task claimedTask) error {
	var event stripe.Event
	if err := json.Unmarshal(task.Payload, &event); err != nil {
		return fmt.Errorf("parse stripe webhook event payload: %w", err)
	}
	if err := a.handleWebhookEvent(ctx, event); err != nil {
		return err
	}
	a.Logger.InfoContext(ctx, "billing: processed stripe webhook", "stripe_event_id", event.ID, "type", event.Type, "task_id", task.TaskID)
	return nil
}

func (a *App) dispatchPurchaseDeposit(ctx context.Context, task claimedTask) error {
	var payload struct {
		OrgID             string `json:"org_id"`
		ProductID         string `json:"product_id"`
		StripePIID        string `json:"stripe_payment_intent_id"`
		AmountLedgerUnits int64  `json:"amount_ledger_units"`
		ExpiresAt         string `json:"expires_at"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return fmt.Errorf("parse purchase deposit payload: %w", err)
	}

	orgID, err := strconv.ParseUint(payload.OrgID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse org_id %q: %w", payload.OrgID, err)
	}
	expiresAt, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse expires_at: %w", err)
	}

	taskID := billing.TaskID(task.TaskID)
	_, err = a.Billing.DepositCredits(ctx, &taskID, billing.CreditGrant{
		OrgID:             billing.OrgID(orgID),
		ProductID:         payload.ProductID,
		Amount:            uint64(payload.AmountLedgerUnits),
		Source:            "purchase",
		StripeReferenceID: payload.StripePIID,
		ExpiresAt:         &expiresAt,
	})
	return err
}

func (a *App) dispatchSubscriptionCreditDeposit(ctx context.Context, task claimedTask) error {
	var payload struct {
		OrgID             string `json:"org_id"`
		ProductID         string `json:"product_id"`
		SubscriptionID    int64  `json:"subscription_id"`
		StripeInvoiceID   string `json:"stripe_invoice_id"`
		AmountLedgerUnits int64  `json:"amount_ledger_units"`
		PeriodStart       string `json:"period_start"`
		PeriodEnd         string `json:"period_end"`
		ExpiresAt         string `json:"expires_at"`
		Source            string `json:"source"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return fmt.Errorf("parse subscription credit deposit payload: %w", err)
	}

	orgID, err := strconv.ParseUint(payload.OrgID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse org_id %q: %w", payload.OrgID, err)
	}
	periodStart, err := time.Parse(time.RFC3339, payload.PeriodStart)
	if err != nil {
		return fmt.Errorf("parse period_start: %w", err)
	}
	periodEnd, err := time.Parse(time.RFC3339, payload.PeriodEnd)
	if err != nil {
		return fmt.Errorf("parse period_end: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse expires_at: %w", err)
	}
	source := payload.Source
	if source == "" {
		source = "subscription"
	}

	taskID := billing.TaskID(task.TaskID)
	_, err = a.Billing.DepositCredits(ctx, &taskID, billing.CreditGrant{
		OrgID:             billing.OrgID(orgID),
		ProductID:         payload.ProductID,
		Amount:            uint64(payload.AmountLedgerUnits),
		Source:            source,
		StripeReferenceID: payload.StripeInvoiceID,
		SubscriptionID:    &payload.SubscriptionID,
		PeriodStart:       &periodStart,
		PeriodEnd:         &periodEnd,
		ExpiresAt:         &expiresAt,
	})
	return err
}

func (a *App) dispatchLicensedCharge(ctx context.Context, task claimedTask) error {
	var payload struct {
		OrgID             string `json:"org_id"`
		ProductID         string `json:"product_id"`
		SubscriptionID    int64  `json:"subscription_id"`
		StripeInvoiceID   string `json:"stripe_invoice_id"`
		AmountLedgerUnits int64  `json:"amount_ledger_units"`
		PeriodStart       string `json:"period_start"`
		PeriodEnd         string `json:"period_end"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return fmt.Errorf("parse licensed charge payload: %w", err)
	}

	orgID, err := strconv.ParseUint(payload.OrgID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse org_id %q: %w", payload.OrgID, err)
	}
	periodStart, err := time.Parse(time.RFC3339, payload.PeriodStart)
	if err != nil {
		return fmt.Errorf("parse period_start: %w", err)
	}
	periodEnd, err := time.Parse(time.RFC3339, payload.PeriodEnd)
	if err != nil {
		return fmt.Errorf("parse period_end: %w", err)
	}

	return a.Billing.RecordLicensedCharge(ctx, billing.TaskID(task.TaskID), billing.LicensedCharge{
		OrgID:           billing.OrgID(orgID),
		ProductID:       payload.ProductID,
		SubscriptionID:  payload.SubscriptionID,
		StripeInvoiceID: payload.StripeInvoiceID,
		Amount:          uint64(payload.AmountLedgerUnits),
		PeriodStart:     periodStart,
		PeriodEnd:       periodEnd,
	})
}

func (a *App) dispatchDisputeDebit(ctx context.Context, task claimedTask) error {
	var payload struct {
		OrgID             string `json:"org_id"`
		DisputeID         string `json:"stripe_dispute_id"`
		PaymentIntentID   string `json:"stripe_payment_intent_id"`
		AmountLedgerUnits int64  `json:"amount_ledger_units"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return fmt.Errorf("parse dispute debit payload: %w", err)
	}

	orgID, err := strconv.ParseUint(payload.OrgID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse org_id %q: %w", payload.OrgID, err)
	}
	if payload.AmountLedgerUnits <= 0 {
		return fmt.Errorf("dispute debit: amount must be positive, got %d", payload.AmountLedgerUnits)
	}

	return a.Billing.HandleDispute(ctx, billing.OrgID(orgID), billing.TaskID(task.TaskID), payload.PaymentIntentID, uint64(payload.AmountLedgerUnits))
}

func (a *App) dispatchTrustTierEvaluate(ctx context.Context, _ claimedTask) error {
	result, err := a.Billing.EvaluateTrustTiers(ctx)
	if err != nil {
		return fmt.Errorf("evaluate trust tiers: %w", err)
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("evaluate trust tiers: %d partial errors, first: %w", len(result.Errors), result.Errors[0])
	}
	return nil
}

func (a *App) handleWebhookEvent(ctx context.Context, event stripe.Event) error {
	switch event.Type {
	case "checkout.session.completed":
		return a.handleCheckoutSessionCompleted(ctx, event)
	case "payment_intent.succeeded":
		return a.handlePaymentIntentSucceeded(ctx, event)
	case "invoice.paid":
		return a.handleInvoicePaid(ctx, event)
	case "invoice.payment_failed":
		return a.handleInvoicePaymentFailed(ctx, event)
	case "charge.dispute.created":
		return a.handleDisputeCreated(ctx, event)
	case "customer.subscription.deleted":
		return a.handleSubscriptionDeleted(ctx, event)
	default:
		return nil
	}
}

func (a *App) handleCheckoutSessionCompleted(ctx context.Context, event stripe.Event) error {
	customerID := event.GetObjectValue("customer")
	orgIDStr := event.GetObjectValue("metadata", "org_id")
	if customerID == "" || orgIDStr == "" {
		return nil
	}

	result, err := a.PG.ExecContext(ctx, `
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
		_, _ = a.PG.ExecContext(ctx, `
			INSERT INTO billing_events (org_id, event_type, stripe_event_id, payload)
			VALUES ($1, 'subscription_created', $2, $3::jsonb)
		`,
			orgIDStr,
			event.ID,
			marshalJSONText(map[string]any{
				"stripe_customer_id": customerID,
				"session_mode":       event.GetObjectValue("mode"),
			}),
		)
	}

	return nil
}

func (a *App) handlePaymentIntentSucceeded(ctx context.Context, event stripe.Event) error {
	piID := event.GetObjectValue("id")
	if piID == "" {
		return fmt.Errorf("payment_intent.succeeded: missing payment_intent id")
	}

	orgIDStr := event.GetObjectValue("metadata", "org_id")
	productID := event.GetObjectValue("metadata", "product_id")
	if orgIDStr == "" || productID == "" {
		return nil
	}

	amountCentsStr := event.GetObjectValue("amount")
	amountCents, err := strconv.ParseInt(amountCentsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("payment_intent.succeeded: parse amount: %w", err)
	}
	amountLedgerUnits := amountCents * 100_000
	expiresAt := a.clock().UTC().AddDate(1, 0, 0)

	payload := map[string]any{
		"org_id":                   orgIDStr,
		"product_id":               productID,
		"stripe_payment_intent_id": piID,
		"amount_ledger_units":      amountLedgerUnits,
		"expires_at":               expiresAt.Format(time.RFC3339),
	}
	return a.enqueueTask(ctx, "stripe_purchase_deposit", piID, payload)
}

func (a *App) handleInvoicePaid(ctx context.Context, event stripe.Event) error {
	invoiceID := event.GetObjectValue("id")
	stripeSubID := event.GetObjectValue("subscription")
	if invoiceID == "" || stripeSubID == "" {
		return nil
	}

	var (
		subscriptionID int64
		orgIDStr       string
		productID      string
		billingModel   string
		includedCreds  int64
		periodStart    time.Time
		periodEnd      time.Time
	)
	err := a.PG.QueryRowContext(ctx, `
		SELECT s.subscription_id, s.org_id, s.product_id, pr.billing_model, p.included_credits
		FROM subscriptions s
		JOIN products pr ON pr.product_id = s.product_id
		JOIN plans p ON p.plan_id = s.plan_id
		WHERE s.stripe_subscription_id = $1
	`, stripeSubID).Scan(&subscriptionID, &orgIDStr, &productID, &billingModel, &includedCreds)
	if err != nil {
		return fmt.Errorf("invoice.paid: lookup subscription: %w", err)
	}

	periodStartStr := event.GetObjectValue("period_start")
	periodEndStr := event.GetObjectValue("period_end")
	if periodStartStr != "" {
		if ts, parseErr := strconv.ParseInt(periodStartStr, 10, 64); parseErr == nil {
			periodStart = time.Unix(ts, 0).UTC()
		}
	}
	if periodEndStr != "" {
		if ts, parseErr := strconv.ParseInt(periodEndStr, 10, 64); parseErr == nil {
			periodEnd = time.Unix(ts, 0).UTC()
		}
	}

	if !periodStart.IsZero() && !periodEnd.IsZero() {
		_, _ = a.PG.ExecContext(ctx, `
			UPDATE subscriptions
			SET current_period_start = $1, current_period_end = $2
			WHERE subscription_id = $3
		`, periodStart, periodEnd, subscriptionID)
	}

	_, _ = a.PG.ExecContext(ctx, `
		UPDATE subscriptions
		SET status = 'active',
		    status_changed_at = now(),
		    past_due_since = NULL
		WHERE subscription_id = $1
		  AND status = 'past_due'
	`, subscriptionID)

	amountStr := event.GetObjectValue("amount_paid")
	_, _ = a.PG.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, subscription_id, stripe_event_id, payload)
		VALUES ($1, 'payment_succeeded', $2, $3, $4::jsonb)
		ON CONFLICT (stripe_event_id) WHERE stripe_event_id IS NOT NULL DO NOTHING
	`,
		orgIDStr,
		subscriptionID,
		event.ID,
		marshalJSONText(map[string]any{
			"stripe_invoice_id": invoiceID,
			"amount_paid":       amountStr,
		}),
	)

	switch billingModel {
	case "metered":
		if includedCreds <= 0 {
			return nil
		}
		expiresAt := periodEnd.AddDate(0, 0, 30)
		payload := map[string]any{
			"org_id":              orgIDStr,
			"product_id":          productID,
			"subscription_id":     subscriptionID,
			"stripe_invoice_id":   invoiceID,
			"amount_ledger_units": includedCreds,
			"period_start":        periodStart.Format(time.RFC3339),
			"period_end":          periodEnd.Format(time.RFC3339),
			"expires_at":          expiresAt.Format(time.RFC3339),
			"source":              "subscription",
		}
		return a.enqueueTask(ctx, "stripe_subscription_credit_deposit", invoiceID, payload)
	case "licensed":
		amountPaid, _ := strconv.ParseInt(amountStr, 10, 64)
		payload := map[string]any{
			"org_id":              orgIDStr,
			"product_id":          productID,
			"subscription_id":     subscriptionID,
			"stripe_invoice_id":   invoiceID,
			"amount_ledger_units": amountPaid * 100_000,
			"period_start":        periodStart.Format(time.RFC3339),
			"period_end":          periodEnd.Format(time.RFC3339),
		}
		return a.enqueueTask(ctx, "stripe_licensed_charge", invoiceID, payload)
	default:
		return nil
	}
}

func (a *App) handleInvoicePaymentFailed(ctx context.Context, event stripe.Event) error {
	stripeSubID := event.GetObjectValue("subscription")
	if stripeSubID == "" {
		return nil
	}

	orgIDStr := ""
	_ = a.PG.QueryRowContext(ctx, `
		SELECT org_id FROM subscriptions WHERE stripe_subscription_id = $1
	`, stripeSubID).Scan(&orgIDStr)

	_, err := a.PG.ExecContext(ctx, `
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

	if orgIDStr != "" {
		attemptCount := event.GetObjectValue("attempt_count")
		_, _ = a.PG.ExecContext(ctx, `
			INSERT INTO billing_events (org_id, event_type, stripe_event_id, payload)
			VALUES ($1, 'payment_failed', $2, $3::jsonb)
			ON CONFLICT (stripe_event_id) WHERE stripe_event_id IS NOT NULL DO NOTHING
		`,
			orgIDStr,
			event.ID,
			marshalJSONText(map[string]any{
				"stripe_subscription_id": stripeSubID,
				"attempt_count":          attemptCount,
			}),
		)
	}

	return nil
}

func (a *App) handleDisputeCreated(ctx context.Context, event stripe.Event) error {
	disputeID := event.GetObjectValue("id")
	if disputeID == "" {
		return fmt.Errorf("charge.dispute.created: missing dispute id")
	}

	amountStr := event.GetObjectValue("amount")
	amount, err := strconv.ParseInt(amountStr, 10, 64)
	if err != nil {
		return fmt.Errorf("charge.dispute.created: parse amount: %w", err)
	}

	piID := event.GetObjectValue("payment_intent")
	orgIDStr := event.GetObjectValue("metadata", "org_id")
	if orgIDStr == "" {
		customerID := event.GetObjectValue("customer")
		if customerID != "" {
			_ = a.PG.QueryRowContext(ctx, `
				SELECT org_id FROM orgs WHERE stripe_customer_id = $1
			`, customerID).Scan(&orgIDStr)
		}
	}
	if orgIDStr == "" {
		return fmt.Errorf("charge.dispute.created: cannot resolve org_id")
	}

	payload := map[string]any{
		"org_id":                   orgIDStr,
		"stripe_dispute_id":        disputeID,
		"stripe_payment_intent_id": piID,
		"amount_ledger_units":      amount * 100_000,
	}

	_, _ = a.PG.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, stripe_event_id, payload)
		VALUES ($1, 'dispute_opened', $2, $3::jsonb)
		ON CONFLICT (stripe_event_id) WHERE stripe_event_id IS NOT NULL DO NOTHING
	`,
		orgIDStr,
		event.ID,
		marshalJSONText(payload),
	)

	return a.enqueueTask(ctx, "stripe_dispute_debit", disputeID, payload)
}

func (a *App) handleSubscriptionDeleted(ctx context.Context, event stripe.Event) error {
	stripeSubID := event.GetObjectValue("id")
	if stripeSubID == "" {
		return nil
	}

	orgIDStr := ""
	_ = a.PG.QueryRowContext(ctx, `
		SELECT org_id FROM subscriptions WHERE stripe_subscription_id = $1
	`, stripeSubID).Scan(&orgIDStr)

	_, err := a.PG.ExecContext(ctx, `
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
		_, _ = a.PG.ExecContext(ctx, `
			INSERT INTO billing_events (org_id, event_type, stripe_event_id, payload)
			VALUES ($1, 'subscription_cancelled', $2, $3::jsonb)
			ON CONFLICT (stripe_event_id) WHERE stripe_event_id IS NOT NULL DO NOTHING
		`,
			orgIDStr,
			event.ID,
			marshalJSONText(map[string]any{
				"stripe_subscription_id": stripeSubID,
				"reason":                 "stripe_terminal_action",
			}),
		)
	}

	return nil
}

func (a *App) enqueueTask(ctx context.Context, taskType, idempotencyKey string, payload map[string]any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("enqueue task: marshal payload: %w", err)
	}
	return a.enqueueRawTask(ctx, taskType, idempotencyKey, payloadJSON)
}

func (a *App) enqueueRawTask(ctx context.Context, taskType, idempotencyKey string, payloadJSON []byte) error {
	_, err := a.PG.ExecContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key)
		VALUES ($1, $2::jsonb, $3)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, taskType, string(payloadJSON), idempotencyKey)
	if err != nil {
		return fmt.Errorf("enqueue task %s: %w", taskType, err)
	}
	return nil
}

func (a *App) claimTask(ctx context.Context) (claimedTask, bool, error) {
	var task claimedTask
	err := a.PG.QueryRowContext(ctx, `
		UPDATE tasks
		SET status = 'claimed', claimed_at = now(), attempts = attempts + 1
		WHERE task_id = (
			SELECT task_id
			FROM tasks
			WHERE status IN ('pending', 'retrying')
			  AND (next_retry_at IS NULL OR next_retry_at <= now())
			ORDER BY scheduled_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING task_id, task_type, payload, attempts, max_attempts, idempotency_key
	`).Scan(&task.TaskID, &task.TaskType, &task.Payload, &task.Attempts, &task.MaxAttempts, &task.IdempotencyKey)
	if err == sql.ErrNoRows {
		return claimedTask{}, false, nil
	}
	if err != nil {
		return claimedTask{}, false, fmt.Errorf("claim task: %w", err)
	}
	return task, true, nil
}

func (a *App) completeTask(ctx context.Context, task claimedTask) error {
	_, err := a.PG.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'completed', completed_at = now()
		WHERE task_id = $1
	`, task.TaskID)
	if err != nil {
		return fmt.Errorf("complete task %d: %w", task.TaskID, err)
	}
	return nil
}

func (a *App) failTask(ctx context.Context, task claimedTask, taskErr error) error {
	errMsg := taskErr.Error()
	if task.Attempts >= task.MaxAttempts {
		_, err := a.PG.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'dead', last_error = $1, dead_at = now()
			WHERE task_id = $2
		`, errMsg, task.TaskID)
		if err != nil {
			return fmt.Errorf("mark task %d dead: %w", task.TaskID, err)
		}
		return nil
	}

	backoffSecs := 5.0 * math.Pow(2, float64(task.Attempts-1))
	_, err := a.PG.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'retrying',
		    last_error = $1,
		    next_retry_at = now() + make_interval(secs => $2)
		WHERE task_id = $3
	`, errMsg, backoffSecs, task.TaskID)
	if err != nil {
		return fmt.Errorf("retry task %d: %w", task.TaskID, err)
	}
	return nil
}

type claimedTask struct {
	TaskID         int64
	TaskType       string
	Payload        json.RawMessage
	Attempts       int
	MaxAttempts    int
	IdempotencyKey sql.NullString
}

func marshalJSONText(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("billing-service: marshal JSON: %v", err))
	}
	return string(data)
}
