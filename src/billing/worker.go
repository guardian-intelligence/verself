package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/stripe/stripe-go/v85"
)

// RunWorker runs the task worker loop. It claims one task at a time,
// dispatches by task_type, and transitions to completed/retrying/dead.
// Blocks until ctx is cancelled. Poll interval between empty claims.
func (c *Client) RunWorker(ctx context.Context, pollInterval time.Duration) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		task, ok, err := c.claimTask(ctx)
		if err != nil {
			// PG error — back off briefly and retry.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}
		if !ok {
			// No claimable tasks — wait for poll interval.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pollInterval):
				continue
			}
		}

		dispatchErr := c.dispatchTask(ctx, task)
		if dispatchErr != nil {
			c.failTask(ctx, task, dispatchErr)
		} else {
			c.completeTask(ctx, task)
		}
	}
}

// claimTask atomically claims the next claimable task using the spec §1.7
// worker claim query with FOR UPDATE SKIP LOCKED.
func (c *Client) claimTask(ctx context.Context) (claimedTask, bool, error) {
	var task claimedTask
	err := c.pg.QueryRowContext(ctx, `
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
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return claimedTask{}, false, nil
		}
		return claimedTask{}, false, fmt.Errorf("claim task: %w", err)
	}
	return task, true, nil
}

// dispatchTask routes a claimed task to the appropriate billing API call.
// Spec §8.2 dispatch table.
func (c *Client) dispatchTask(ctx context.Context, task claimedTask) error {
	switch task.TaskType {
	case "stripe_webhook_event":
		return c.dispatchWebhookEvent(ctx, task)
	case "stripe_purchase_deposit":
		return c.dispatchPurchaseDeposit(ctx, task)
	case "stripe_subscription_credit_deposit":
		return c.dispatchSubscriptionCreditDeposit(ctx, task)
	case "stripe_licensed_charge":
		return c.dispatchLicensedCharge(ctx, task)
	case "stripe_dispute_debit":
		return c.dispatchDisputeDebit(ctx, task)
	case "trust_tier_evaluate":
		return c.dispatchTrustTierEvaluate(ctx, task)
	default:
		return fmt.Errorf("unknown task_type %q", task.TaskType)
	}
}

func (c *Client) dispatchWebhookEvent(ctx context.Context, task claimedTask) error {
	var event stripe.Event
	if err := json.Unmarshal(task.Payload, &event); err != nil {
		return fmt.Errorf("parse stripe webhook event payload: %w", err)
	}
	if err := c.handleWebhookEvent(ctx, event); err != nil {
		return err
	}
	log.Printf("billing: processed stripe webhook event id=%s type=%s task_id=%d", event.ID, event.Type, task.TaskID)
	return nil
}

// dispatchPurchaseDeposit calls DepositCredits for a one-time purchase.
func (c *Client) dispatchPurchaseDeposit(ctx context.Context, task claimedTask) error {
	var p struct {
		OrgID             string `json:"org_id"`
		ProductID         string `json:"product_id"`
		StripePIID        string `json:"stripe_payment_intent_id"`
		AmountLedgerUnits int64  `json:"amount_ledger_units"`
		ExpiresAt         string `json:"expires_at"`
	}
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		return fmt.Errorf("parse purchase deposit payload: %w", err)
	}

	orgIDVal, err := strconv.ParseUint(p.OrgID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse org_id %q: %w", p.OrgID, err)
	}

	expiresAt, err := time.Parse(time.RFC3339, p.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse expires_at: %w", err)
	}

	taskID := TaskID(task.TaskID)
	return c.DepositCredits(ctx, &taskID, CreditGrant{
		OrgID:             OrgID(orgIDVal),
		ProductID:         p.ProductID,
		Amount:            uint64(p.AmountLedgerUnits),
		Source:            "purchase",
		StripeReferenceID: p.StripePIID,
		ExpiresAt:         &expiresAt,
	})
}

// dispatchSubscriptionCreditDeposit calls DepositCredits for a subscription
// period credit grant.
func (c *Client) dispatchSubscriptionCreditDeposit(ctx context.Context, task claimedTask) error {
	var p struct {
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
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		return fmt.Errorf("parse subscription credit deposit payload: %w", err)
	}

	orgIDVal, err := strconv.ParseUint(p.OrgID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse org_id %q: %w", p.OrgID, err)
	}

	periodStart, err := time.Parse(time.RFC3339, p.PeriodStart)
	if err != nil {
		return fmt.Errorf("parse period_start: %w", err)
	}
	periodEnd, err := time.Parse(time.RFC3339, p.PeriodEnd)
	if err != nil {
		return fmt.Errorf("parse period_end: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339, p.ExpiresAt)
	if err != nil {
		return fmt.Errorf("parse expires_at: %w", err)
	}

	source := p.Source
	if source == "" {
		source = "subscription"
	}

	taskID := TaskID(task.TaskID)
	return c.DepositCredits(ctx, &taskID, CreditGrant{
		OrgID:             OrgID(orgIDVal),
		ProductID:         p.ProductID,
		Amount:            uint64(p.AmountLedgerUnits),
		Source:            source,
		StripeReferenceID: p.StripeInvoiceID,
		SubscriptionID:    &p.SubscriptionID,
		PeriodStart:       &periodStart,
		PeriodEnd:         &periodEnd,
		ExpiresAt:         &expiresAt,
	})
}

// dispatchLicensedCharge calls RecordLicensedCharge for a licensed invoice.
func (c *Client) dispatchLicensedCharge(ctx context.Context, task claimedTask) error {
	var p struct {
		OrgID             string `json:"org_id"`
		ProductID         string `json:"product_id"`
		SubscriptionID    int64  `json:"subscription_id"`
		StripeInvoiceID   string `json:"stripe_invoice_id"`
		AmountLedgerUnits int64  `json:"amount_ledger_units"`
		PeriodStart       string `json:"period_start"`
		PeriodEnd         string `json:"period_end"`
	}
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		return fmt.Errorf("parse licensed charge payload: %w", err)
	}

	orgIDVal, err := strconv.ParseUint(p.OrgID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse org_id %q: %w", p.OrgID, err)
	}

	periodStart, err := time.Parse(time.RFC3339, p.PeriodStart)
	if err != nil {
		return fmt.Errorf("parse period_start: %w", err)
	}
	periodEnd, err := time.Parse(time.RFC3339, p.PeriodEnd)
	if err != nil {
		return fmt.Errorf("parse period_end: %w", err)
	}

	return c.RecordLicensedCharge(ctx, TaskID(task.TaskID), LicensedCharge{
		OrgID:           OrgID(orgIDVal),
		ProductID:       p.ProductID,
		SubscriptionID:  p.SubscriptionID,
		StripeInvoiceID: p.StripeInvoiceID,
		Amount:          uint64(p.AmountLedgerUnits),
		PeriodStart:     periodStart,
		PeriodEnd:       periodEnd,
	})
}

// dispatchDisputeDebit calls HandleDispute for a chargeback debit.
func (c *Client) dispatchDisputeDebit(ctx context.Context, task claimedTask) error {
	var p struct {
		OrgID             string `json:"org_id"`
		DisputeID         string `json:"stripe_dispute_id"`
		PaymentIntentID   string `json:"stripe_payment_intent_id"`
		AmountLedgerUnits int64  `json:"amount_ledger_units"`
	}
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		return fmt.Errorf("parse dispute debit payload: %w", err)
	}

	orgIDVal, err := strconv.ParseUint(p.OrgID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse org_id %q: %w", p.OrgID, err)
	}

	if p.AmountLedgerUnits <= 0 {
		return fmt.Errorf("dispute debit: amount must be positive, got %d", p.AmountLedgerUnits)
	}

	return c.HandleDispute(ctx, OrgID(orgIDVal), TaskID(task.TaskID), p.PaymentIntentID, uint64(p.AmountLedgerUnits))
}

// dispatchTrustTierEvaluate runs the trust-tier evaluation cron.
func (c *Client) dispatchTrustTierEvaluate(ctx context.Context, task claimedTask) error {
	result, err := c.EvaluateTrustTiers(ctx)
	if err != nil {
		return fmt.Errorf("evaluate trust tiers: %w", err)
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("evaluate trust tiers: %d partial errors, first: %w", len(result.Errors), result.Errors[0])
	}

	return nil
}

// completeTask marks a task as completed.
func (c *Client) completeTask(ctx context.Context, task claimedTask) {
	_, _ = c.pg.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'completed', completed_at = now()
		WHERE task_id = $1
	`, task.TaskID)
}

// failTask transitions a task to retrying (with exponential backoff) or dead.
// Backoff: 5s × 2^(attempts-1) → 5s, 10s, 20s, 40s, 80s.
func (c *Client) failTask(ctx context.Context, task claimedTask, taskErr error) {
	errMsg := taskErr.Error()

	if task.Attempts >= task.MaxAttempts {
		_, _ = c.pg.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'dead', last_error = $1, dead_at = now()
			WHERE task_id = $2
		`, errMsg, task.TaskID)
		return
	}

	backoffSecs := 5.0 * math.Pow(2, float64(task.Attempts-1))
	_, _ = c.pg.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'retrying',
		    last_error = $1,
		    next_retry_at = now() + make_interval(secs => $2)
		WHERE task_id = $3
	`, errMsg, backoffSecs, task.TaskID)
}
