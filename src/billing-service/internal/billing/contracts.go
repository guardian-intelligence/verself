package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stripe/stripe-go/v85"

	"github.com/forge-metal/billing-service/internal/store"
)

type planEntitlementPolicy struct {
	PolicyID             string
	ProductID            string
	ScopeType            string
	ScopeProductID       string
	ScopeBucketID        string
	ScopeSKUID           string
	AmountUnits          uint64
	Cadence              string
	ProrationMode        string
	PolicyVersion        string
	RecurrenceAnchorKind string
}

type contractChangeQuote struct {
	ChangeID        string                     `json:"change_id"`
	ContractID      string                     `json:"contract_id"`
	OrgID           OrgID                      `json:"org_id"`
	ProductID       string                     `json:"product_id"`
	FromPlanID      string                     `json:"from_plan_id"`
	TargetPlanID    string                     `json:"target_plan_id"`
	FromPhaseID     string                     `json:"from_phase_id"`
	ToPhaseID       string                     `json:"to_phase_id"`
	CycleID         string                     `json:"cycle_id"`
	CycleStart      time.Time                  `json:"cycle_start"`
	CycleEnd        time.Time                  `json:"cycle_end"`
	EffectiveAt     time.Time                  `json:"effective_at"`
	RequestedAt     time.Time                  `json:"requested_at"`
	PriceDeltaCents uint64                     `json:"price_delta_cents"`
	PriceDeltaUnits uint64                     `json:"price_delta_units"`
	Deltas          []contractEntitlementDelta `json:"deltas"`
	TargetPolicies  []planEntitlementPolicy    `json:"target_policies"`
}

type contractEntitlementDelta struct {
	Policy planEntitlementPolicy `json:"policy"`
	Amount uint64                `json:"amount"`
}

func (c *Client) CreateContract(ctx context.Context, orgID OrgID, planID string, cadence BillingCadence, successURL, cancelURL string) (string, error) {
	if planID == "" {
		return "", fmt.Errorf("plan_id is required")
	}
	if cadence == "" {
		cadence = CadenceMonthly
	}
	if cadence != CadenceMonthly {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedCadence, cadence)
	}
	plan, err := c.loadPlan(ctx, planID)
	if err != nil {
		return "", err
	}
	contractID := contractID(orgID, plan.ProductID)
	if existing, err := c.GetContract(ctx, orgID, contractID); err == nil {
		if existing.PlanID == planID && (existing.Status == "active" || existing.Status == "cancel_scheduled") {
			return successURL, nil
		}
		result, err := c.CreateContractChange(ctx, orgID, contractID, ContractChangeRequest{TargetPlanID: planID, SuccessURL: successURL, CancelURL: cancelURL})
		if err != nil {
			return "", err
		}
		return result.URL, nil
	} else if !errors.Is(err, ErrContractNotFound) {
		return "", err
	}
	if !c.cfg.UseStripe || c.stripe == nil {
		now, err := c.BusinessNow(ctx, c.queries, orgID, plan.ProductID)
		if err != nil {
			return "", err
		}
		if err := c.activateCatalogContract(ctx, orgID, plan.ProductID, planID, contractID, phaseID(contractID, planID, now), now, now); err != nil {
			return "", err
		}
		return successURL, nil
	}
	customerID, err := c.ensureStripeCustomer(ctx, orgID)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	phaseID := phaseID(contractID, planID, now)
	metadata := map[string]string{"org_id": orgIDText(orgID), "product_id": plan.ProductID, "plan_id": planID, "contract_id": contractID, "phase_id": phaseID, "cadence": string(cadence)}
	params := &stripe.CheckoutSessionCreateParams{Mode: stripe.String(string(stripe.CheckoutSessionModeSetup)), Customer: stripe.String(customerID), Currency: stripe.String(plan.Currency), SuccessURL: stripe.String(successURL), CancelURL: stripe.String(cancelURL), SetupIntentData: &stripe.CheckoutSessionCreateSetupIntentDataParams{Description: stripe.String(plan.DisplayName + " payment method"), Metadata: metadata}, Metadata: metadata}
	// Checkout setup sessions carry per-attempt metadata and return URLs; a plan-level key
	// replays stale parameters across browser e2e runs and Stripe rejects the request.
	params.SetIdempotencyKey(textID("stripe_setup_checkout", contractID, planID, phaseID, successURL, cancelURL))
	session, err := c.stripe.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("create stripe setup checkout: %w", err)
	}
	return session.URL, nil
}

func (c *Client) GetContract(ctx context.Context, orgID OrgID, contractID string) (ContractRecord, error) {
	var record ContractRecord
	now, err := c.BusinessNow(ctx, c.queries, orgID, "")
	if err != nil {
		return ContractRecord{}, err
	}
	var startsAt time.Time
	var endsAt, phaseStart, phaseEnd *time.Time
	err = c.pg.QueryRow(ctx, `
		WITH current_phase AS (
			SELECT phase_id, COALESCE(plan_id, '') AS plan_id, effective_start, effective_end
			FROM contract_phases
			WHERE contract_id = $1
			  AND state IN ('active','grace','pending_payment','scheduled')
			  AND effective_start <= $3
			  AND (effective_end IS NULL OR effective_end > $3)
			ORDER BY CASE state WHEN 'active' THEN 1 WHEN 'grace' THEN 2 ELSE 3 END, effective_start DESC, phase_id DESC
			LIMIT 1
		)
		SELECT c.contract_id, c.product_id, COALESCE(cp.plan_id,''), COALESCE(cp.phase_id,''), c.state, c.payment_state, c.entitlement_state, c.starts_at, c.ends_at, cp.effective_start, cp.effective_end
		FROM contracts c
		LEFT JOIN current_phase cp ON true
		WHERE c.contract_id = $1 AND c.org_id = $2
	`, contractID, orgIDText(orgID), now).Scan(&record.ContractID, &record.ProductID, &record.PlanID, &record.PhaseID, &record.Status, &record.PaymentState, &record.EntitlementState, &startsAt, &endsAt, &phaseStart, &phaseEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContractRecord{}, ErrContractNotFound
	}
	if err != nil {
		return ContractRecord{}, fmt.Errorf("load contract %s: %w", contractID, err)
	}
	record.CadenceKind = "anniversary_monthly"
	record.StartsAt = startsAt.UTC()
	record.EndsAt = normalizeTimePtr(endsAt)
	record.PhaseStart = normalizeTimePtr(phaseStart)
	record.PhaseEnd = normalizeTimePtr(phaseEnd)
	return record, nil
}

func (c *Client) CancelContract(ctx context.Context, orgID OrgID, contractID string) (ContractRecord, error) {
	record, err := c.GetContract(ctx, orgID, contractID)
	if err != nil {
		return ContractRecord{}, err
	}
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, record.ProductID)
	if err != nil {
		return ContractRecord{}, err
	}
	now := time.Now().UTC()
	err = c.WithTx(ctx, "billing.contract.cancel", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		_, err := tx.Exec(ctx, `UPDATE contracts SET state = 'cancel_scheduled', cancel_at = $3, ends_at = $3 WHERE contract_id = $1 AND org_id = $2 AND state IN ('active','past_due')`, contractID, orgIDText(orgID), cycle.EndsAt)
		if err != nil {
			return fmt.Errorf("schedule contract cancellation: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE contract_phases SET effective_end = COALESCE(effective_end, $3) WHERE contract_id = $1 AND org_id = $2 AND state IN ('active','grace','scheduled')`, contractID, orgIDText(orgID), cycle.EndsAt)
		if err != nil {
			return fmt.Errorf("schedule phase cancellation: %w", err)
		}
		changeID := textID("contract_change", contractID, "cancel", cycle.EndsAt.Format(time.RFC3339Nano))
		_, err = tx.Exec(ctx, `
			INSERT INTO contract_changes (change_id, contract_id, org_id, product_id, change_type, timing, requested_effective_at, target_plan_id, state, idempotency_key, requested_at)
			VALUES ($1,$2,$3,$4,'cancel','period_end',$5,NULL,'scheduled',$1,$6)
			ON CONFLICT (contract_id, idempotency_key) DO NOTHING
		`, changeID, contractID, orgIDText(orgID), record.ProductID, cycle.EndsAt, now)
		if err != nil {
			return fmt.Errorf("insert cancellation change: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "contract_cancel_scheduled", AggregateType: "contract", AggregateID: contractID, OrgID: orgID, ProductID: record.ProductID, OccurredAt: now, Payload: map[string]any{"contract_id": contractID, "cycle_id": cycle.CycleID, "cancel_at": cycle.EndsAt.Format(time.RFC3339Nano)}})
	})
	if err != nil {
		return ContractRecord{}, err
	}
	return c.GetContract(ctx, orgID, contractID)
}

func (c *Client) CreateContractChange(ctx context.Context, orgID OrgID, contractID string, req ContractChangeRequest) (ContractChangeResult, error) {
	if req.TargetPlanID == "" {
		return ContractChangeResult{}, fmt.Errorf("target_plan_id is required")
	}
	existing, err := c.GetContract(ctx, orgID, contractID)
	if err != nil {
		return ContractChangeResult{}, err
	}
	target, err := c.loadPlan(ctx, req.TargetPlanID)
	if err != nil {
		return ContractChangeResult{}, err
	}
	current, err := c.loadPlan(ctx, existing.PlanID)
	if err != nil {
		return ContractChangeResult{}, err
	}
	if target.ProductID != existing.ProductID || current.ProductID != existing.ProductID {
		return ContractChangeResult{}, fmt.Errorf("%w: plan product mismatch", ErrUnsupportedChange)
	}
	if target.PlanID == current.PlanID {
		return ContractChangeResult{URL: req.SuccessURL, Status: "unchanged"}, nil
	}
	if target.MonthlyAmountCents <= current.MonthlyAmountCents {
		change, err := c.scheduleDowngrade(ctx, orgID, existing, target.PlanID)
		if err != nil {
			return ContractChangeResult{}, err
		}
		return ContractChangeResult{URL: req.SuccessURL, ChangeID: change, Status: "scheduled"}, nil
	}
	quote, err := c.prepareUpgradeQuote(ctx, orgID, existing, current, target)
	if err != nil {
		return ContractChangeResult{}, err
	}
	if err := c.insertPendingUpgrade(ctx, quote); err != nil {
		return ContractChangeResult{}, err
	}
	status := "paid"
	url := req.SuccessURL
	providerInvoiceID := ""
	if c.cfg.UseStripe && c.stripe != nil {
		stripeURL, stripeInvoiceID, paid, err := c.collectUpgradeInvoice(ctx, quote)
		if err != nil {
			return ContractChangeResult{}, err
		}
		providerInvoiceID = stripeInvoiceID
		if stripeURL != "" {
			url = stripeURL
		}
		if !paid {
			status = "pending"
		}
	}
	if status == "paid" {
		if err := c.applyPaidUpgrade(ctx, quote, providerInvoiceID); err != nil {
			return ContractChangeResult{}, err
		}
		url = req.SuccessURL
	}
	return ContractChangeResult{URL: url, ChangeID: quote.ChangeID, InvoiceID: invoiceID(quote.ChangeID), Status: status, PriceDeltaUnits: quote.PriceDeltaUnits}, nil
}

func (c *Client) scheduleDowngrade(ctx context.Context, orgID OrgID, existing ContractRecord, targetPlanID string) (string, error) {
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, existing.ProductID)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	changeID := changeID(existing.ContractID, targetPlanID, cycle.EndsAt)
	toPhaseID := phaseID(existing.ContractID, targetPlanID, cycle.EndsAt)
	err = c.WithTx(ctx, "billing.contract_change.schedule_downgrade", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO contract_changes (change_id, contract_id, org_id, product_id, change_type, timing, requested_effective_at, from_phase_id, to_phase_id, target_plan_id, state, idempotency_key, requested_at)
			VALUES ($1,$2,$3,$4,'downgrade','period_end',$5,NULLIF($6,''),$7,$8,'scheduled',$1,$9)
			ON CONFLICT (contract_id, idempotency_key) DO NOTHING
		`, changeID, existing.ContractID, orgIDText(orgID), existing.ProductID, cycle.EndsAt, existing.PhaseID, toPhaseID, targetPlanID, now)
		if err != nil {
			return fmt.Errorf("insert scheduled downgrade: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "contract_change_scheduled", AggregateType: "contract_change", AggregateID: changeID, OrgID: orgID, ProductID: existing.ProductID, OccurredAt: now, Payload: map[string]any{"contract_id": existing.ContractID, "change_id": changeID, "cycle_id": cycle.CycleID, "from_phase_id": existing.PhaseID, "to_phase_id": toPhaseID, "target_plan_id": targetPlanID}})
	})
	return changeID, err
}

func (c *Client) prepareUpgradeQuote(ctx context.Context, orgID OrgID, existing ContractRecord, current PlanRecord, target PlanRecord) (contractChangeQuote, error) {
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, existing.ProductID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, existing.ProductID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	if !cycle.EndsAt.After(now) {
		return contractChangeQuote{}, fmt.Errorf("%w: upgrade outside open cycle", ErrUnsupportedChange)
	}
	currentPolicies, err := c.planEntitlementPolicies(ctx, current.PlanID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	targetPolicies, err := c.planEntitlementPolicies(ctx, target.PlanID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	deltas := entitlementDeltas(currentPolicies, targetPolicies, now, cycle)
	remaining := cycle.EndsAt.Sub(now)
	period := cycle.EndsAt.Sub(cycle.StartsAt)
	priceDeltaCents := prorateCents(target.MonthlyAmountCents-current.MonthlyAmountCents, remaining, period)
	units, err := moneyUnitsFromCents(int64(priceDeltaCents))
	if err != nil {
		return contractChangeQuote{}, err
	}
	toPhaseID := phaseID(existing.ContractID, target.PlanID, now)
	changeID := changeID(existing.ContractID, target.PlanID, now)
	return contractChangeQuote{ChangeID: changeID, ContractID: existing.ContractID, OrgID: orgID, ProductID: existing.ProductID, FromPlanID: current.PlanID, TargetPlanID: target.PlanID, FromPhaseID: existing.PhaseID, ToPhaseID: toPhaseID, CycleID: cycle.CycleID, CycleStart: cycle.StartsAt, CycleEnd: cycle.EndsAt, EffectiveAt: now, RequestedAt: now, PriceDeltaCents: priceDeltaCents, PriceDeltaUnits: units, Deltas: deltas, TargetPolicies: targetPolicies}, nil
}

func (c *Client) insertPendingUpgrade(ctx context.Context, quote contractChangeQuote) error {
	payload, err := json.Marshal(quote)
	if err != nil {
		return err
	}
	return c.WithTx(ctx, "billing.contract_change.request_upgrade", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		number, err := allocateInvoiceNumberTx(ctx, tx, quote.RequestedAt)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO contract_changes (change_id, contract_id, org_id, product_id, change_type, timing, requested_effective_at, from_phase_id, to_phase_id, target_plan_id, state, provider, idempotency_key, requested_at, proration_basis_cycle_id, price_delta_units, entitlement_delta_mode, proration_numerator, proration_denominator, payload)
			VALUES ($1,$2,$3,$4,'upgrade','immediate',$5,NULLIF($6,''),$7,$8,'awaiting_payment','stripe',$1,$9,$10,$11,'positive_delta',$12,$13,$14)
			ON CONFLICT (contract_id, idempotency_key) DO NOTHING
		`, quote.ChangeID, quote.ContractID, orgIDText(quote.OrgID), quote.ProductID, quote.EffectiveAt, quote.FromPhaseID, quote.ToPhaseID, quote.TargetPlanID, quote.RequestedAt, quote.CycleID, int64(quote.PriceDeltaUnits), int64(quote.CycleEnd.Sub(quote.EffectiveAt)), int64(quote.CycleEnd.Sub(quote.CycleStart)), payload)
		if err != nil {
			return fmt.Errorf("insert upgrade change: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO billing_invoices (invoice_id, invoice_number, org_id, product_id, cycle_id, change_id, invoice_kind, status, payment_status, period_start, period_end, issued_at, currency, subtotal_units, total_due_units, invoice_snapshot_json, content_hash)
			VALUES ($1,$2,$3,$4,$5,$6,'contract_change','issued','pending',$7,$8,$9,'usd',$10,$10,$11,$12)
			ON CONFLICT (invoice_id) DO NOTHING
		`, invoiceID(quote.ChangeID), number, orgIDText(quote.OrgID), quote.ProductID, quote.CycleID, quote.ChangeID, quote.EffectiveAt, quote.CycleEnd, quote.RequestedAt, int64(quote.PriceDeltaUnits), payload, textID("invoice_snapshot", string(payload)))
		if err != nil {
			return fmt.Errorf("insert upgrade invoice: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "contract_change_requested", AggregateType: "contract_change", AggregateID: quote.ChangeID, OrgID: quote.OrgID, ProductID: quote.ProductID, OccurredAt: quote.RequestedAt, Payload: map[string]any{"contract_id": quote.ContractID, "change_id": quote.ChangeID, "cycle_id": quote.CycleID, "from_phase_id": quote.FromPhaseID, "to_phase_id": quote.ToPhaseID, "target_plan_id": quote.TargetPlanID, "invoice_id": invoiceID(quote.ChangeID), "price_delta_units": quote.PriceDeltaUnits}})
	})
}

func (c *Client) applyPaidUpgrade(ctx context.Context, quote contractChangeQuote, providerInvoiceID string) error {
	return c.WithTx(ctx, "billing.contract_change.apply_upgrade", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		_, err := tx.Exec(ctx, `UPDATE contract_phases SET state = 'superseded', entitlement_state = 'closed', effective_end = $3, closed_at = $3 WHERE phase_id = $1 AND contract_id = $2 AND state IN ('active','grace')`, quote.FromPhaseID, quote.ContractID, quote.EffectiveAt)
		if err != nil {
			return fmt.Errorf("close old phase: %w", err)
		}
		if err := c.insertContractPhaseTx(ctx, tx, quote.OrgID, quote.ProductID, quote.ContractID, quote.ToPhaseID, quote.TargetPlanID, "active", "paid", quote.EffectiveAt); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE contract_phases SET superseded_by_phase_id = $3 WHERE phase_id = $1 AND contract_id = $2 AND state = 'superseded'`, quote.FromPhaseID, quote.ContractID, quote.ToPhaseID)
		if err != nil {
			return fmt.Errorf("link superseded phase: %w", err)
		}
		if err := c.copyPlanEntitlementLinesTx(ctx, tx, quote.OrgID, quote.ProductID, quote.ContractID, quote.ToPhaseID, quote.TargetPolicies, quote.CycleEnd); err != nil {
			return err
		}
		for _, delta := range quote.Deltas {
			if err := c.insertUpgradeDeltaGrantTx(ctx, tx, quote, delta); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE contracts SET state = 'active', payment_state = 'paid', entitlement_state = 'active', display_name = $3, ends_at = NULL, cancel_at = NULL WHERE contract_id = $1 AND org_id = $2`, quote.ContractID, orgIDText(quote.OrgID), quote.TargetPlanID)
		if err != nil {
			return fmt.Errorf("update upgraded contract: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE contract_changes SET state = 'applied', actual_effective_at = $2, provider_invoice_id = NULLIF($3,''), updated_at = now() WHERE change_id = $1`, quote.ChangeID, quote.EffectiveAt, providerInvoiceID)
		if err != nil {
			return fmt.Errorf("mark upgrade applied: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE billing_invoices SET status = 'paid', payment_status = 'paid', stripe_invoice_id = NULLIF($2,''), issued_at = COALESCE(issued_at, $3) WHERE invoice_id = $1`, invoiceID(quote.ChangeID), providerInvoiceID, quote.EffectiveAt)
		if err != nil {
			return fmt.Errorf("mark upgrade invoice paid: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "contract_change_applied", AggregateType: "contract_change", AggregateID: quote.ChangeID, OrgID: quote.OrgID, ProductID: quote.ProductID, OccurredAt: quote.EffectiveAt, Payload: map[string]any{"contract_id": quote.ContractID, "change_id": quote.ChangeID, "cycle_id": quote.CycleID, "from_phase_id": quote.FromPhaseID, "to_phase_id": quote.ToPhaseID, "pricing_plan_id": quote.TargetPlanID, "invoice_id": invoiceID(quote.ChangeID), "provider_invoice_id": providerInvoiceID}})
	})
}

func (c *Client) activateCatalogContract(ctx context.Context, orgID OrgID, productID, planID, contractID, phaseID string, effectiveAt time.Time, lineActiveFrom time.Time) error {
	policies, err := c.planEntitlementPolicies(ctx, planID)
	if err != nil {
		return err
	}
	return c.WithTx(ctx, "billing.contract.activate", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		var alreadyActive bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM contract_phases WHERE phase_id = $1 AND contract_id = $2 AND state = 'active' AND payment_state = 'paid')`, phaseID, contractID).Scan(&alreadyActive); err != nil {
			return fmt.Errorf("check active contract phase: %w", err)
		}
		if err := c.insertContractTx(ctx, tx, orgID, productID, contractID, planID, effectiveAt); err != nil {
			return err
		}
		if err := c.insertContractPhaseTx(ctx, tx, orgID, productID, contractID, phaseID, planID, "active", "paid", effectiveAt); err != nil {
			return err
		}
		if err := c.copyPlanEntitlementLinesTx(ctx, tx, orgID, productID, contractID, phaseID, policies, lineActiveFrom); err != nil {
			return err
		}
		if alreadyActive {
			return nil
		}
		if err := appendEvent(ctx, tx, q, eventFact{EventType: "contract_activated", AggregateType: "contract", AggregateID: contractID, OrgID: orgID, ProductID: productID, OccurredAt: effectiveAt, Payload: map[string]any{"contract_id": contractID, "pricing_phase_id": phaseID, "pricing_plan_id": planID}}); err != nil {
			return err
		}
		return nil
	})
}

func (c *Client) insertContractTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID, contractID, planID string, startsAt time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO contracts (contract_id, org_id, product_id, display_name, contract_kind, state, payment_state, entitlement_state, overage_policy, starts_at)
		VALUES ($1,$2,$3,$4,'self_serve','active','paid','active','bill_published_rate',$5)
		ON CONFLICT (contract_id) DO UPDATE
		SET state = 'active', payment_state = 'paid', entitlement_state = 'active', display_name = EXCLUDED.display_name, ends_at = NULL, cancel_at = NULL
	`, contractID, orgIDText(orgID), productID, planID, startsAt)
	if err != nil {
		return fmt.Errorf("upsert contract: %w", err)
	}
	return nil
}

func (c *Client) insertContractPhaseTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID, contractID, phaseID, planID, state, paymentState string, effectiveAt time.Time) error {
	plan, err := c.loadPlan(ctx, planID)
	if err != nil {
		return err
	}
	units, err := moneyUnitsFromCents(int64(plan.MonthlyAmountCents))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO contract_phases (phase_id, contract_id, org_id, product_id, plan_id, phase_kind, state, payment_state, entitlement_state, currency, recurring_amount_units, recurring_interval, effective_start, activated_at, created_reason)
		VALUES ($1,$2,$3,$4,$5,'catalog_plan',$6,$7,'active',$8,$9,'month',$10,$10,'catalog_contract')
		ON CONFLICT (phase_id) DO UPDATE
		SET state = EXCLUDED.state, payment_state = EXCLUDED.payment_state, entitlement_state = 'active', effective_end = NULL, activated_at = COALESCE(contract_phases.activated_at, EXCLUDED.activated_at)
	`, phaseID, contractID, orgIDText(orgID), productID, planID, state, paymentState, plan.Currency, int64(units), effectiveAt)
	if err != nil {
		return fmt.Errorf("insert contract phase: %w", err)
	}
	return nil
}

func (c *Client) copyPlanEntitlementLinesTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID, contractID, phaseID string, policies []planEntitlementPolicy, activeFrom time.Time) error {
	for _, policy := range policies {
		lineID := textID("contract_line", phaseID, policy.PolicyID)
		_, err := tx.Exec(ctx, `
			INSERT INTO contract_entitlement_lines (line_id, phase_id, contract_id, org_id, product_id, policy_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount_units, recurrence_interval, recurrence_anchor_kind, recurrence_anchor_day, proration_mode, policy_version, active_from, next_materialize_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),$11,'month',$12,NULL,$13,$14,$15,$15)
			ON CONFLICT (line_id) DO UPDATE
			SET amount_units = EXCLUDED.amount_units, active_from = EXCLUDED.active_from, next_materialize_at = EXCLUDED.next_materialize_at, policy_version = EXCLUDED.policy_version
		`, lineID, phaseID, contractID, orgIDText(orgID), productID, policy.PolicyID, policy.ScopeType, policy.ScopeProductID, policy.ScopeBucketID, policy.ScopeSKUID, int64(policy.AmountUnits), cleanNonEmpty(policy.RecurrenceAnchorKind, "billing_cycle"), cleanNonEmpty(policy.ProrationMode, "none"), policy.PolicyVersion, activeFrom)
		if err != nil {
			return fmt.Errorf("copy entitlement line %s: %w", policy.PolicyID, err)
		}
	}
	return nil
}

func (c *Client) insertUpgradeDeltaGrantTx(ctx context.Context, tx pgx.Tx, quote contractChangeQuote, delta contractEntitlementDelta) error {
	if delta.Amount == 0 {
		return nil
	}
	policy := delta.Policy
	lineID := textID("contract_line", quote.ToPhaseID, policy.PolicyID)
	sourceRef := "upgrade_delta:" + quote.ChangeID + ":" + policy.PolicyID
	periodID := textID("period", orgIDText(quote.OrgID), sourceRef)
	grantID := grantID(quote.OrgID, "contract", policy.ScopeType, policy.ScopeProductID, policy.ScopeBucketID, policy.ScopeSKUID, sourceRef)
	_, err := tx.Exec(ctx, `
		INSERT INTO entitlement_periods (period_id, org_id, product_id, cycle_id, source, policy_id, contract_id, phase_id, line_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount_units, period_start, period_end, policy_version, payment_state, entitlement_state, calculation_kind, source_reference_id, created_reason, change_id)
		VALUES ($1,$2,$3,$4,'contract',$5,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),$13,$14,$15,$16,'paid','active','upgrade_delta',$17,'upgrade_delta',$18)
		ON CONFLICT (org_id, source, source_reference_id) DO NOTHING
	`, periodID, orgIDText(quote.OrgID), quote.ProductID, quote.CycleID, policy.PolicyID, quote.ContractID, quote.ToPhaseID, lineID, policy.ScopeType, policy.ScopeProductID, policy.ScopeBucketID, policy.ScopeSKUID, int64(delta.Amount), quote.EffectiveAt, quote.CycleEnd, policy.PolicyVersion, sourceRef, quote.ChangeID)
	if err != nil {
		return fmt.Errorf("insert upgrade delta period: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO credit_grants (grant_id, org_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount, source, source_reference_id, entitlement_period_id, policy_version, starts_at, period_start, period_end, expires_at, ledger_state)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,'contract',$8,$9,$10,$11,$11,$12,$12,'posted')
		ON CONFLICT (org_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, source_reference_id) DO NOTHING
	`, grantID, orgIDText(quote.OrgID), policy.ScopeType, policy.ScopeProductID, policy.ScopeBucketID, policy.ScopeSKUID, int64(delta.Amount), sourceRef, periodID, policy.PolicyVersion, quote.EffectiveAt, quote.CycleEnd)
	if err != nil {
		return fmt.Errorf("insert upgrade delta grant: %w", err)
	}
	return appendEvent(ctx, tx, c.queries.WithTx(tx), eventFact{EventType: "credit_grant_issued", AggregateType: "credit_grant", AggregateID: grantID, OrgID: quote.OrgID, ProductID: quote.ProductID, OccurredAt: quote.EffectiveAt, Payload: map[string]any{"grant_id": grantID, "contract_id": quote.ContractID, "phase_id": quote.ToPhaseID, "change_id": quote.ChangeID, "cycle_id": quote.CycleID, "source": "contract", "amount": delta.Amount}})
}

func (c *Client) planEntitlementPolicies(ctx context.Context, planID string) ([]planEntitlementPolicy, error) {
	rows, err := c.pg.Query(ctx, `
		SELECT e.policy_id, e.product_id, e.scope_type, COALESCE(e.scope_product_id,''), COALESCE(e.scope_bucket_id,''), COALESCE(e.scope_sku_id,''), e.amount_units, e.cadence, e.anchor_kind, e.proration_mode, e.policy_version
		FROM plan_entitlements pe
		JOIN entitlement_policies e ON e.policy_id = pe.policy_id
		WHERE pe.plan_id = $1
		ORDER BY pe.sort_order, e.policy_id
	`, planID)
	if err != nil {
		return nil, fmt.Errorf("query plan entitlement policies: %w", err)
	}
	defer rows.Close()
	out := []planEntitlementPolicy{}
	for rows.Next() {
		var policy planEntitlementPolicy
		var amount int64
		if err := rows.Scan(&policy.PolicyID, &policy.ProductID, &policy.ScopeType, &policy.ScopeProductID, &policy.ScopeBucketID, &policy.ScopeSKUID, &amount, &policy.Cadence, &policy.RecurrenceAnchorKind, &policy.ProrationMode, &policy.PolicyVersion); err != nil {
			return nil, fmt.Errorf("scan plan entitlement policy: %w", err)
		}
		policy.AmountUnits = uint64(amount)
		if policy.RecurrenceAnchorKind == "calendar_month" {
			policy.RecurrenceAnchorKind = "billing_cycle"
		}
		out = append(out, policy)
	}
	return out, rows.Err()
}

func (c *Client) loadPlan(ctx context.Context, planID string) (PlanRecord, error) {
	var out PlanRecord
	var monthly, annual int64
	err := c.pg.QueryRow(ctx, `SELECT plan_id, product_id, display_name, billing_mode, tier, currency, monthly_amount_cents, annual_amount_cents, active, is_default FROM plans WHERE plan_id = $1 AND active`, planID).Scan(&out.PlanID, &out.ProductID, &out.DisplayName, &out.BillingMode, &out.Tier, &out.Currency, &monthly, &annual, &out.Active, &out.IsDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanRecord{}, fmt.Errorf("plan %s not found", planID)
	}
	if err != nil {
		return PlanRecord{}, fmt.Errorf("load plan: %w", err)
	}
	out.MonthlyAmountCents = uint64(monthly)
	out.AnnualAmountCents = uint64(annual)
	return out, nil
}

func entitlementDeltas(current, target []planEntitlementPolicy, now time.Time, cycle billingCycle) []contractEntitlementDelta {
	currentByScope := map[string]uint64{}
	for _, policy := range current {
		currentByScope[entitlementPolicyScopeKey(policy)] = policy.AmountUnits
	}
	out := []contractEntitlementDelta{}
	for _, policy := range target {
		delta := uint64(0)
		if policy.AmountUnits > currentByScope[entitlementPolicyScopeKey(policy)] {
			delta = policy.AmountUnits - currentByScope[entitlementPolicyScopeKey(policy)]
		}
		if delta == 0 {
			continue
		}
		out = append(out, contractEntitlementDelta{Policy: policy, Amount: prorateUnits(delta, int64(cycle.EndsAt.Sub(now)), int64(cycle.EndsAt.Sub(cycle.StartsAt)))})
	}
	return out
}

func entitlementPolicyScopeKey(policy planEntitlementPolicy) string {
	return policy.ScopeType + ":" + policy.ScopeProductID + ":" + policy.ScopeBucketID + ":" + policy.ScopeSKUID
}

func prorateCents(cents uint64, remaining time.Duration, period time.Duration) uint64 {
	if cents == 0 || remaining <= 0 || period <= 0 {
		return 0
	}
	return uint64(math.Ceil(float64(cents) * float64(remaining) / float64(period)))
}

func allocateInvoiceNumberTx(ctx context.Context, tx pgx.Tx, issuedAt time.Time) (string, error) {
	year := issuedAt.UTC().Year()
	_, err := tx.Exec(ctx, `INSERT INTO invoice_number_allocators (issuer_id, invoice_year, prefix, next_number) VALUES ('forge-metal', $1, 'FM', 1) ON CONFLICT (issuer_id, invoice_year) DO NOTHING`, year)
	if err != nil {
		return "", fmt.Errorf("ensure invoice allocator: %w", err)
	}
	var next int64
	err = tx.QueryRow(ctx, `SELECT next_number FROM invoice_number_allocators WHERE issuer_id = 'forge-metal' AND invoice_year = $1 FOR UPDATE`, year).Scan(&next)
	if err != nil {
		return "", fmt.Errorf("lock invoice allocator: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE invoice_number_allocators SET next_number = next_number + 1 WHERE issuer_id = 'forge-metal' AND invoice_year = $1`, year)
	if err != nil {
		return "", fmt.Errorf("advance invoice allocator: %w", err)
	}
	return fmt.Sprintf("FM-%d-%06d", year, next), nil
}

func normalizeTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := value.UTC()
	return &v
}
