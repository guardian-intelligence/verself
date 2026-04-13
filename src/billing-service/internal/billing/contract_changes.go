package billing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const (
	ledgerUnitsPerCent uint64 = 100_000
)

type ContractChangeRequest struct {
	TargetPlanID string
	SuccessURL   string
	CancelURL    string
}

type ContractChangeResult struct {
	URL             string
	ChangeID        string
	InvoiceID       string
	Status          string
	PriceDeltaUnits uint64
}

type contractChangeQuote struct {
	OrgID                  OrgID
	ProductID              string
	ContractID             string
	FromPhaseID            string
	ToPhaseID              string
	FromPlanID             string
	TargetPlanID           string
	Currency               string
	ChangeID               string
	InvoiceID              string
	InvoiceNumber          string
	Cycle                  BillingCycle
	EffectiveAt            time.Time
	RequestedAt            time.Time
	ProrationNumerator     time.Duration
	ProrationDenominator   time.Duration
	ProrationNumeratorNS   int64
	ProrationDenominatorNS int64
	PriceDeltaCents        uint64
	PriceDeltaUnits        uint64
	EntitlementDeltas      []contractEntitlementDelta
	TargetPolicies         []EntitlementPolicy
}

type contractEntitlementDelta struct {
	Policy EntitlementPolicy
	LineID string
	Amount uint64
}

type entitlementScopeKey struct {
	Source         GrantSourceType
	ScopeType      GrantScopeType
	ScopeProductID string
	ScopeBucketID  string
	ScopeSKUID     string
}

func (c *Client) CreateContractChange(ctx context.Context, orgID OrgID, contractID string, req ContractChangeRequest) (ContractChangeResult, error) {
	if err := ctx.Err(); err != nil {
		return ContractChangeResult{}, err
	}
	if contractID == "" {
		return ContractChangeResult{}, fmt.Errorf("%w: contract_id is required", ErrContractNotFound)
	}
	if req.TargetPlanID == "" {
		return ContractChangeResult{}, fmt.Errorf("target_plan_id is required")
	}

	quote, err := c.prepareImmediateUpgradeQuote(ctx, orgID, contractID, req.TargetPlanID)
	if err != nil {
		return ContractChangeResult{}, err
	}
	if quote.PriceDeltaCents == 0 {
		return ContractChangeResult{}, fmt.Errorf("%w: immediate upgrade price delta must be positive", ErrUnsupportedContractChange)
	}

	existing, found, err := c.loadContractChangeState(ctx, quote.ChangeID, req.SuccessURL)
	if err != nil {
		return ContractChangeResult{}, err
	}
	if found {
		if existing.Status == "applied" || existing.Status == "paid" {
			return existing.Result, nil
		}
		if existing.ProviderInvoiceID != "" && existing.PaymentStatus == "paid" {
			if err := c.applyPaidContractChange(ctx, quote.ChangeID, quote.InvoiceID, existing.ProviderInvoiceID, "", c.clock().UTC()); err != nil {
				return ContractChangeResult{}, err
			}
			existing.Result.Status = "paid"
			existing.Result.URL = req.SuccessURL
			return existing.Result, nil
		}
		if existing.ProviderInvoiceID != "" {
			return existing.Result, nil
		}
		if existing.InvoiceNumber == "" {
			return ContractChangeResult{}, fmt.Errorf("contract change %s has no invoice number", quote.ChangeID)
		}
		quote.InvoiceNumber = existing.InvoiceNumber
	} else {
		tx, err := c.pg.BeginTx(ctx, nil)
		if err != nil {
			return ContractChangeResult{}, fmt.Errorf("begin contract change request: %w", err)
		}
		defer tx.Rollback()

		invoiceNumber, err := allocateInvoiceNumberTx(ctx, tx, invoiceIssuerForgeMetal, quote.RequestedAt)
		if err != nil {
			return ContractChangeResult{}, err
		}
		quote.InvoiceNumber = invoiceNumber
		if err := c.insertPendingContractChangeTx(ctx, tx, quote); err != nil {
			return ContractChangeResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return ContractChangeResult{}, fmt.Errorf("commit contract change request: %w", err)
		}
	}

	payment, err := c.collectContractChangeInvoice(ctx, quote)
	if err != nil {
		if markErr := c.failContractChangePayment(ctx, quote.ChangeID, quote.InvoiceID, err); markErr != nil {
			return ContractChangeResult{}, fmt.Errorf("collect contract change invoice: %w; mark failure: %v", err, markErr)
		}
		return ContractChangeResult{}, err
	}
	if payment.Status == "paid" {
		if err := c.applyPaidContractChange(ctx, quote.ChangeID, quote.InvoiceID, payment.ProviderInvoiceID, "", payment.PaidAt); err != nil {
			return ContractChangeResult{}, err
		}
		payment.URL = req.SuccessURL
	}
	if payment.URL == "" {
		payment.URL = req.SuccessURL
	}
	return ContractChangeResult{
		URL:             payment.URL,
		ChangeID:        quote.ChangeID,
		InvoiceID:       quote.InvoiceID,
		Status:          payment.Status,
		PriceDeltaUnits: quote.PriceDeltaUnits,
	}, nil
}

func (c *Client) prepareImmediateUpgradeQuote(ctx context.Context, orgID OrgID, contractID string, targetPlanID string) (contractChangeQuote, error) {
	existing, err := c.GetContract(ctx, orgID, contractID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	if existing.Status != "active" && existing.Status != "cancel_scheduled" {
		return contractChangeQuote{}, fmt.Errorf("%w: contract %s is %s", ErrUnsupportedContractChange, contractID, existing.Status)
	}
	if existing.PhaseID == "" || existing.PlanID == "" || existing.PhaseStart == nil {
		return contractChangeQuote{}, fmt.Errorf("%w: contract %s has no active catalog phase", ErrUnsupportedContractChange, contractID)
	}
	if existing.PlanID == targetPlanID {
		return contractChangeQuote{}, fmt.Errorf("%w: contract %s is already on plan %s", ErrUnsupportedContractChange, contractID, targetPlanID)
	}
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, existing.ProductID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	requestedAt := c.clock().UTC()
	effectiveAt := normalizePhaseChangeEffectiveAt(requestedAt, existing.PhaseStart)
	if !cycle.EndsAt.After(effectiveAt) {
		return contractChangeQuote{}, fmt.Errorf("%w: contract change %s is outside current billing cycle", ErrUnsupportedContractChange, contractID)
	}

	currentPlan, err := c.loadActivePlan(ctx, existing.PlanID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	targetPlan, err := c.loadActivePlan(ctx, targetPlanID)
	if err != nil {
		return contractChangeQuote{}, err
	}
	if currentPlan.ProductID != existing.ProductID || targetPlan.ProductID != existing.ProductID {
		return contractChangeQuote{}, fmt.Errorf("%w: plan product mismatch for %s -> %s", ErrUnsupportedContractChange, existing.PlanID, targetPlanID)
	}
	if targetPlan.MonthlyAmountCents <= currentPlan.MonthlyAmountCents {
		return contractChangeQuote{}, fmt.Errorf("%w: immediate catalog changes only support strict price upgrades", ErrUnsupportedContractChange)
	}
	if targetPlan.Currency == "" {
		targetPlan.Currency = "usd"
	}

	currentPolicies, err := c.contractEntitlementPolicies(ctx, existing.PlanID, cycle.StartsAt, cycle.EndsAt)
	if err != nil {
		return contractChangeQuote{}, err
	}
	targetPolicies, err := c.contractEntitlementPolicies(ctx, targetPlanID, cycle.StartsAt, cycle.EndsAt)
	if err != nil {
		return contractChangeQuote{}, err
	}
	deltas, err := entitlementPositiveDeltas(currentPolicies, targetPolicies)
	if err != nil {
		return contractChangeQuote{}, err
	}
	if len(deltas) == 0 {
		return contractChangeQuote{}, fmt.Errorf("%w: target plan has no positive entitlement deltas", ErrUnsupportedContractChange)
	}

	remaining := cycle.EndsAt.Sub(effectiveAt)
	fullPeriod := cycle.EndsAt.Sub(cycle.StartsAt)
	if remaining <= 0 || fullPeriod <= 0 {
		return contractChangeQuote{}, fmt.Errorf("%w: invalid proration window", ErrUnsupportedContractChange)
	}
	priceDeltaCents := prorateUint64ByDuration(targetPlan.MonthlyAmountCents-currentPlan.MonthlyAmountCents, remaining, fullPeriod)
	if priceDeltaCents == 0 {
		return contractChangeQuote{}, fmt.Errorf("%w: immediate upgrade would produce a zero-cent invoice", ErrUnsupportedContractChange)
	}
	priceDeltaUnits, err := safeMulUint64(priceDeltaCents, ledgerUnitsPerCent)
	if err != nil {
		return contractChangeQuote{}, fmt.Errorf("convert price delta to ledger units: %w", err)
	}

	toPhaseID := newSelfServeContractPhaseID(contractID, targetPlanID, requestedAt)
	changeID := deterministicTextID(
		"contract-change",
		contractID,
		existing.PhaseID,
		toPhaseID,
		targetPlanID,
		effectiveAt.Format(time.RFC3339Nano),
	)
	invoiceID := deterministicTextID("billing-invoice", "contract-change", changeID)
	for i := range deltas {
		deltas[i].LineID = contractEntitlementLineID(toPhaseID, deltas[i].Policy.PolicyID)
		deltas[i].Amount = prorateUint64ByDuration(deltas[i].Amount, remaining, fullPeriod)
	}

	return contractChangeQuote{
		OrgID:                  orgID,
		ProductID:              existing.ProductID,
		ContractID:             contractID,
		FromPhaseID:            existing.PhaseID,
		ToPhaseID:              toPhaseID,
		FromPlanID:             existing.PlanID,
		TargetPlanID:           targetPlanID,
		Currency:               targetPlan.Currency,
		ChangeID:               changeID,
		InvoiceID:              invoiceID,
		Cycle:                  cycle,
		EffectiveAt:            effectiveAt,
		RequestedAt:            requestedAt,
		ProrationNumerator:     remaining,
		ProrationDenominator:   fullPeriod,
		ProrationNumeratorNS:   int64(remaining),
		ProrationDenominatorNS: int64(fullPeriod),
		PriceDeltaCents:        priceDeltaCents,
		PriceDeltaUnits:        priceDeltaUnits,
		EntitlementDeltas:      deltas,
		TargetPolicies:         targetPolicies,
	}, nil
}

func (c *Client) insertPendingContractChangeTx(ctx context.Context, tx *sql.Tx, quote contractChangeQuote) error {
	orgIDText := strconv.FormatUint(uint64(quote.OrgID), 10)
	payload, err := contractChangePayload(quote)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO contracts (
			contract_id, org_id, product_id, display_name, contract_kind, status,
			payment_state, entitlement_state, cadence_kind, billing_anchor_at,
			starts_at, overage_policy
		)
		VALUES ($1, $2, $3, $4, 'self_serve', 'active',
		        'paid', 'active', 'anniversary_monthly', $5,
		        $5, 'bill_overages')
		ON CONFLICT (contract_id) DO UPDATE
		SET status = 'active',
		    display_name = EXCLUDED.display_name,
		    updated_at = now()
	`, quote.ContractID, orgIDText, quote.ProductID, quote.TargetPlanID, quote.Cycle.AnchorAt); err != nil {
		return fmt.Errorf("upsert contract for change %s: %w", quote.ChangeID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO contract_phases (
			phase_id, contract_id, org_id, product_id, plan_id, phase_kind, state,
			payment_state, entitlement_state, cadence_kind, effective_start,
			recurrence_anchor_at, overage_policy
		)
		VALUES ($1, $2, $3, $4, $5, 'catalog_plan', 'pending_payment',
		        'pending', 'scheduled', 'anniversary_monthly', $6,
		        $6, 'bill_overages')
		ON CONFLICT (phase_id) DO UPDATE
		SET state = CASE WHEN contract_phases.state = 'pending_payment' THEN EXCLUDED.state ELSE contract_phases.state END,
		    payment_state = CASE WHEN contract_phases.payment_state = 'pending' THEN EXCLUDED.payment_state ELSE contract_phases.payment_state END,
		    entitlement_state = CASE WHEN contract_phases.entitlement_state = 'scheduled' THEN EXCLUDED.entitlement_state ELSE contract_phases.entitlement_state END,
		    updated_at = now()
	`, quote.ToPhaseID, quote.ContractID, orgIDText, quote.ProductID, quote.TargetPlanID, quote.EffectiveAt); err != nil {
		return fmt.Errorf("insert target contract phase %s: %w", quote.ToPhaseID, err)
	}
	if err := insertContractEntitlementLinesTx(ctx, tx, quote.ContractID, quote.ToPhaseID, quote.TargetPolicies); err != nil {
		return err
	}
	priceDeltaUnits, err := uint64ToInt64(quote.PriceDeltaUnits)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO contract_changes (
			change_id, contract_id, org_id, product_id, change_type, state, timing,
			requested_plan_id, target_plan_id, from_phase_id, to_phase_id,
			provider, provider_request_id, idempotency_key, proration_basis_cycle_id,
			price_delta_units, entitlement_delta_mode, proration_numerator, proration_denominator,
			invoice_id, requested_effective_at, payload
		)
		VALUES ($1, $2, $3, $4, 'upgrade', 'awaiting_payment', 'immediate',
		        $5, $6, $7, $8,
		        'stripe', $9, $10, $11,
		        $12, 'positive_delta', $13, $14,
		        $15, $16, $17::jsonb)
		ON CONFLICT (change_id) DO UPDATE
		SET state = CASE WHEN contract_changes.state IN ('applied', 'applying') THEN contract_changes.state ELSE EXCLUDED.state END,
		    invoice_id = CASE WHEN contract_changes.invoice_id = '' THEN EXCLUDED.invoice_id ELSE contract_changes.invoice_id END,
		    provider_request_id = CASE WHEN contract_changes.provider_request_id = '' THEN EXCLUDED.provider_request_id ELSE contract_changes.provider_request_id END,
		    updated_at = now()
	`, quote.ChangeID, quote.ContractID, orgIDText, quote.ProductID,
		quote.FromPlanID, quote.TargetPlanID, quote.FromPhaseID, quote.ToPhaseID,
		quote.InvoiceID, contractChangeStripeIdempotencyKey(quote.ChangeID), quote.Cycle.CycleID,
		priceDeltaUnits, quote.ProrationNumeratorNS, quote.ProrationDenominatorNS,
		quote.InvoiceID, quote.EffectiveAt, string(payload)); err != nil {
		return fmt.Errorf("insert contract change %s: %w", quote.ChangeID, err)
	}
	if err := insertContractChangeInvoiceTx(ctx, tx, quote, payload); err != nil {
		return err
	}
	if err := insertBillingEventTx(ctx, tx, contractChangeRequestedEvent(quote)); err != nil {
		return err
	}
	return nil
}

func insertContractEntitlementLinesTx(ctx context.Context, tx *sql.Tx, contractID string, phaseID string, policies []EntitlementPolicy) error {
	for _, policy := range policies {
		lineID := contractEntitlementLineID(phaseID, policy.PolicyID)
		amountUnits, err := uint64ToInt64(policy.AmountUnits)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO contract_entitlement_lines (
				line_id, contract_id, phase_id, product_id, policy_id, policy_version,
				scope_type, scope_product_id, scope_bucket_id, scope_sku_id,
				amount_units, cadence, proration_mode
			)
			VALUES ($1, $2, $3, $4, $5, $6,
			        $7, $8, $9, $10,
			        $11, $12, $13)
			ON CONFLICT (line_id) DO UPDATE
			SET policy_version = EXCLUDED.policy_version,
			    amount_units = EXCLUDED.amount_units,
			    cadence = EXCLUDED.cadence,
			    proration_mode = EXCLUDED.proration_mode,
			    active = true,
			    updated_at = now()
		`, lineID, contractID, phaseID, policy.ProductID, policy.PolicyID, policy.PolicyVersion,
			policy.ScopeType.String(), policy.ScopeProductID, policy.ScopeBucketID, policy.ScopeSKUID,
			amountUnits, string(policy.Cadence), string(policy.ProrationMode)); err != nil {
			return fmt.Errorf("copy contract entitlement line %s: %w", policy.PolicyID, err)
		}
	}
	return nil
}

func insertContractChangeInvoiceTx(ctx context.Context, tx *sql.Tx, quote contractChangeQuote, payload []byte) error {
	totalDueUnits, err := uint64ToInt64(quote.PriceDeltaUnits)
	if err != nil {
		return err
	}
	rendered := renderContractChangeInvoiceHTML(quote)
	contentHash := sha256Hex([]byte(rendered))
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO billing_invoices (
			invoice_id, cycle_id, org_id, product_id, change_id, invoice_number, invoice_kind,
			status, payment_status, period_start, period_end, issued_at, due_at, currency,
			total_due_units, subtotal_units, invoice_snapshot_json, rendered_html, content_hash
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'contract_change',
		        'draft', 'pending', $7, $8, $9, $9, $10,
		        $11, $11, $12::jsonb, $13, $14)
		ON CONFLICT (invoice_id) DO UPDATE
		SET status = CASE WHEN billing_invoices.status = 'draft' THEN EXCLUDED.status ELSE billing_invoices.status END,
		    payment_status = CASE WHEN billing_invoices.payment_status = 'pending' THEN EXCLUDED.payment_status ELSE billing_invoices.payment_status END,
		    updated_at = now()
	`, quote.InvoiceID, quote.Cycle.CycleID, strconv.FormatUint(uint64(quote.OrgID), 10), quote.ProductID, quote.ChangeID, quote.InvoiceNumber,
		quote.EffectiveAt, quote.Cycle.EndsAt, quote.RequestedAt, quote.Currency,
		totalDueUnits, string(payload), rendered, contentHash); err != nil {
		return fmt.Errorf("insert contract change invoice %s: %w", quote.InvoiceID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invoice_line_items (
			line_item_id, invoice_id, line_type, product_id, description, quantity, quantity_unit,
			unit_rate, charge_units, metadata
		)
		VALUES ($1, $2, 'recurring_charge', $3, $4, 1, 'upgrade', $5, $5, $6::jsonb)
		ON CONFLICT (line_item_id) DO NOTHING
	`, deterministicTextID("invoice-line-item", quote.InvoiceID, "contract-change", quote.ChangeID), quote.InvoiceID, quote.ProductID,
		"Prorated upgrade from "+quote.FromPlanID+" to "+quote.TargetPlanID, totalDueUnits, string(payload)); err != nil {
		return fmt.Errorf("insert contract change invoice line %s: %w", quote.InvoiceID, err)
	}
	return nil
}

func (c *Client) applyPaidContractChange(ctx context.Context, changeID string, invoiceID string, providerInvoiceID string, providerEventID string, paidAt time.Time) error {
	if changeID == "" {
		return nil
	}
	paidAt = paidAt.UTC()
	change, alreadyApplied, err := c.loadContractChangeForApply(ctx, changeID)
	if err != nil {
		return err
	}
	if !alreadyApplied {
		tx, err := c.pg.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin apply contract change %s: %w", changeID, err)
		}
		defer tx.Rollback()
		if err := supersedeContractPhaseTx(ctx, tx, strconv.FormatUint(uint64(change.OrgID), 10), change.FromPhaseID, change.ToPhaseID, change.EffectiveAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE contract_phases
			SET state = 'active',
			    payment_state = 'paid',
			    entitlement_state = 'active',
			    updated_at = now()
			WHERE phase_id = $1
			  AND org_id = $2
		`, change.ToPhaseID, strconv.FormatUint(uint64(change.OrgID), 10)); err != nil {
			return fmt.Errorf("activate target phase %s: %w", change.ToPhaseID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE contracts
			SET display_name = $3,
			    payment_state = 'paid',
			    entitlement_state = 'active',
			    updated_at = now()
			WHERE contract_id = $1
			  AND org_id = $2
		`, change.ContractID, strconv.FormatUint(uint64(change.OrgID), 10), change.TargetPlanID); err != nil {
			return fmt.Errorf("activate contract %s target plan: %w", change.ContractID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE contract_changes
			SET state = 'applied',
			    actual_effective_at = $2,
			    provider_invoice_id = COALESCE(NULLIF($3, ''), provider_invoice_id),
			    invoice_id = COALESCE(NULLIF($4, ''), invoice_id),
			    state_version = state_version + 1,
			    updated_at = now()
			WHERE change_id = $1
				  AND state IN ('awaiting_payment', 'provider_pending', 'applying', 'requested', 'failed')
		`, changeID, paidAt, providerInvoiceID, firstNonEmpty(invoiceID, change.InvoiceID)); err != nil {
			return fmt.Errorf("mark contract change %s applied: %w", changeID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE billing_invoices
			SET payment_status = 'paid',
			    status = 'paid',
			    stripe_invoice_id = COALESCE(NULLIF($2, ''), stripe_invoice_id),
			    updated_at = now()
			WHERE invoice_id = $1
		`, firstNonEmpty(invoiceID, change.InvoiceID), providerInvoiceID); err != nil {
			return fmt.Errorf("mark contract change invoice %s paid: %w", firstNonEmpty(invoiceID, change.InvoiceID), err)
		}
		if err := insertContractChangeDeltaPeriodsTx(ctx, tx, change, providerInvoiceID, providerEventID); err != nil {
			return err
		}
		if err := insertBillingEventTx(ctx, tx, contractPhaseClosedEvent(change.OrgID, change.ProductID, change.ContractID, change.FromPhaseID, change.FromPlanID, change.ToPhaseID, "superseded", paidAt)); err != nil {
			return err
		}
		if err := insertBillingEventTx(ctx, tx, contractPhaseStartedEvent(change.OrgID, change.ProductID, change.ContractID, change.ToPhaseID, change.TargetPlanID, change.Cycle, change.EffectiveAt)); err != nil {
			return err
		}
		if err := insertBillingEventTx(ctx, tx, contractChangeAppliedEvent(change.OrgID, change.ProductID, change.ContractID, change.ChangeID, "upgrade", change.FromPlanID, change.TargetPlanID, change.FromPhaseID, change.ToPhaseID, paidAt)); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit apply contract change %s: %w", changeID, err)
		}
	}
	return c.materializeContractChangeDeltaGrants(ctx, changeID)
}

type contractChangeApplyRecord struct {
	ChangeID          string
	OrgID             OrgID
	ProductID         string
	ContractID        string
	FromPhaseID       string
	ToPhaseID         string
	FromPlanID        string
	TargetPlanID      string
	InvoiceID         string
	Cycle             BillingCycle
	EffectiveAt       time.Time
	State             string
	ProviderInvoiceID string
}

func (c *Client) loadContractChangeForApply(ctx context.Context, changeID string) (contractChangeApplyRecord, bool, error) {
	var rec contractChangeApplyRecord
	var orgIDText string
	var state string
	err := c.pg.QueryRowContext(ctx, `
		SELECT cc.change_id, cc.org_id, cc.product_id, cc.contract_id, cc.from_phase_id, cc.to_phase_id,
		       cc.requested_plan_id, cc.target_plan_id, cc.invoice_id, cc.state,
		       COALESCE(cc.actual_effective_at, cc.requested_effective_at), cc.provider_invoice_id,
		       bc.cycle_id, bc.org_id, bc.product_id, COALESCE(bc.predecessor_cycle_id, ''), bc.cadence_kind, bc.status,
		       bc.anchor_at, bc.cycle_seq, bc.starts_at, bc.ends_at, bc.finalization_due_at
		FROM contract_changes cc
		JOIN billing_cycles bc ON bc.cycle_id = cc.proration_basis_cycle_id
		WHERE cc.change_id = $1
	`, changeID).Scan(
		&rec.ChangeID,
		&orgIDText,
		&rec.ProductID,
		&rec.ContractID,
		&rec.FromPhaseID,
		&rec.ToPhaseID,
		&rec.FromPlanID,
		&rec.TargetPlanID,
		&rec.InvoiceID,
		&state,
		&rec.EffectiveAt,
		&rec.ProviderInvoiceID,
		&rec.Cycle.CycleID,
		&orgIDText,
		&rec.Cycle.ProductID,
		&rec.Cycle.PredecessorCycleID,
		&rec.Cycle.CadenceKind,
		&rec.Cycle.Status,
		&rec.Cycle.AnchorAt,
		&rec.Cycle.CycleSeq,
		&rec.Cycle.StartsAt,
		&rec.Cycle.EndsAt,
		&rec.Cycle.FinalizationDueAt,
	)
	if err == sql.ErrNoRows {
		return contractChangeApplyRecord{}, false, fmt.Errorf("%w: contract change %s", ErrUnsupportedContractChange, changeID)
	}
	if err != nil {
		return contractChangeApplyRecord{}, false, fmt.Errorf("load contract change %s: %w", changeID, err)
	}
	parsedOrgID, err := strconv.ParseUint(orgIDText, 10, 64)
	if err != nil {
		return contractChangeApplyRecord{}, false, fmt.Errorf("parse contract change org_id %q: %w", orgIDText, err)
	}
	rec.OrgID = OrgID(parsedOrgID)
	rec.Cycle.OrgID = rec.OrgID
	rec.State = state
	rec.EffectiveAt = rec.EffectiveAt.UTC()
	rec.Cycle.AnchorAt = rec.Cycle.AnchorAt.UTC()
	rec.Cycle.StartsAt = rec.Cycle.StartsAt.UTC()
	rec.Cycle.EndsAt = rec.Cycle.EndsAt.UTC()
	rec.Cycle.FinalizationDueAt = rec.Cycle.FinalizationDueAt.UTC()
	return rec, state == "applied", nil
}

func insertContractChangeDeltaPeriodsTx(ctx context.Context, tx *sql.Tx, change contractChangeApplyRecord, providerInvoiceID string, providerEventID string) error {
	policies, err := contractEntitlementLinePoliciesTx(ctx, tx, change.ToPhaseID)
	if err != nil {
		return err
	}
	currentPolicies, err := contractEntitlementLinePoliciesTx(ctx, tx, change.FromPhaseID)
	if err != nil {
		return err
	}
	deltas, err := entitlementPositiveDeltas(currentPolicies, policies)
	if err != nil {
		return err
	}
	remaining := change.Cycle.EndsAt.Sub(change.EffectiveAt)
	fullPeriod := change.Cycle.EndsAt.Sub(change.Cycle.StartsAt)
	for _, delta := range deltas {
		amount := prorateUint64ByDuration(delta.Amount, remaining, fullPeriod)
		if amount == 0 {
			continue
		}
		lineID := contractEntitlementLineID(change.ToPhaseID, delta.Policy.PolicyID)
		period := EntitlementPeriod{
			PeriodID:          contractEntitlementDeltaPeriodID(change.OrgID, change.ContractID, change.FromPhaseID, change.ToPhaseID, lineID, delta.Policy, change.ChangeID, change.EffectiveAt, change.Cycle.EndsAt),
			CycleID:           change.Cycle.CycleID,
			OrgID:             change.OrgID,
			ProductID:         delta.Policy.ProductID,
			Source:            delta.Policy.Source,
			PolicyID:          delta.Policy.PolicyID,
			ContractID:        change.ContractID,
			PhaseID:           change.ToPhaseID,
			LineID:            lineID,
			ChangeID:          change.ChangeID,
			CalculationKind:   "upgrade_delta",
			ProviderInvoiceID: firstNonEmpty(providerInvoiceID, change.ProviderInvoiceID),
			ProviderEventID:   providerEventID,
			ScopeType:         delta.Policy.ScopeType,
			ScopeProductID:    delta.Policy.ScopeProductID,
			ScopeBucketID:     delta.Policy.ScopeBucketID,
			ScopeSKUID:        delta.Policy.ScopeSKUID,
			AmountUnits:       amount,
			PeriodStart:       change.EffectiveAt,
			PeriodEnd:         change.Cycle.EndsAt,
			PolicyVersion:     delta.Policy.PolicyVersion,
			PaymentState:      PaymentPaid,
			EntitlementState:  EntitlementActive,
			SourceReferenceID: contractEntitlementDeltaSourceReference(change.ContractID, change.FromPhaseID, change.ToPhaseID, lineID, delta.Policy, change.ChangeID, change.EffectiveAt, change.Cycle.EndsAt),
			CreatedReason:     "contract_upgrade_delta",
		}
		if err := ensureEntitlementPeriodTx(ctx, tx, period); err != nil {
			return err
		}
	}
	return nil
}

func contractEntitlementLinePoliciesTx(ctx context.Context, tx *sql.Tx, phaseID string) ([]EntitlementPolicy, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT policy_id, product_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id,
		       amount_units, cadence, proration_mode, policy_version
		FROM contract_entitlement_lines
		WHERE phase_id = $1
		  AND active
		ORDER BY policy_id
	`, phaseID)
	if err != nil {
		return nil, fmt.Errorf("query contract entitlement lines for phase %s: %w", phaseID, err)
	}
	defer rows.Close()
	var out []EntitlementPolicy
	for rows.Next() {
		var policy EntitlementPolicy
		var scopeText string
		var amount int64
		var cadence string
		var prorationMode string
		if err := rows.Scan(&policy.PolicyID, &policy.ProductID, &scopeText, &policy.ScopeProductID, &policy.ScopeBucketID, &policy.ScopeSKUID, &amount, &cadence, &prorationMode, &policy.PolicyVersion); err != nil {
			return nil, fmt.Errorf("scan contract entitlement line: %w", err)
		}
		scope, err := ParseGrantScopeType(scopeText)
		if err != nil {
			return nil, err
		}
		if amount < 0 {
			return nil, fmt.Errorf("contract entitlement line %s has negative amount", policy.PolicyID)
		}
		policy.Source = SourceContract
		policy.ScopeType = scope
		policy.AmountUnits = uint64(amount)
		policy.Cadence = EntitlementCadence(cadence)
		policy.ProrationMode = EntitlementProrationMode(prorationMode)
		out = append(out, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contract entitlement lines: %w", err)
	}
	return out, nil
}

func ensureEntitlementPeriodTx(ctx context.Context, tx *sql.Tx, period EntitlementPeriod) error {
	if period.PeriodID == "" {
		return fmt.Errorf("entitlement period id is required")
	}
	if period.CalculationKind == "" {
		period.CalculationKind = "recurrence"
	}
	amountUnits, err := uint64ToInt64(period.AmountUnits)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO entitlement_periods (
			period_id, cycle_id, org_id, product_id, source, policy_id, contract_id, phase_id, line_id,
			change_id, calculation_kind, provider_invoice_id, provider_event_id,
			scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount_units,
			period_start, period_end, policy_version, payment_state, entitlement_state,
			source_reference_id, created_reason
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
		        $10, $11, $12, $13,
		        $14, $15, $16, $17, $18,
		        $19, $20, $21, $22, $23,
		        $24, $25)
		ON CONFLICT (period_id) DO UPDATE
		SET payment_state = EXCLUDED.payment_state,
		    entitlement_state = EXCLUDED.entitlement_state,
		    provider_invoice_id = CASE WHEN entitlement_periods.provider_invoice_id = '' THEN EXCLUDED.provider_invoice_id ELSE entitlement_periods.provider_invoice_id END,
		    provider_event_id = CASE WHEN entitlement_periods.provider_event_id = '' THEN EXCLUDED.provider_event_id ELSE entitlement_periods.provider_event_id END,
		    updated_at = now()
	`, period.PeriodID, period.CycleID, strconv.FormatUint(uint64(period.OrgID), 10), period.ProductID, period.Source.String(), period.PolicyID, period.ContractID, period.PhaseID, period.LineID,
		period.ChangeID, period.CalculationKind, period.ProviderInvoiceID, period.ProviderEventID,
		period.ScopeType.String(), period.ScopeProductID, period.ScopeBucketID, period.ScopeSKUID, amountUnits,
		period.PeriodStart, period.PeriodEnd, period.PolicyVersion, string(period.PaymentState), string(period.EntitlementState),
		period.SourceReferenceID, period.CreatedReason)
	if err != nil {
		return fmt.Errorf("ensure entitlement period %s: %w", period.PeriodID, err)
	}
	return nil
}

func (c *Client) materializeContractChangeDeltaGrants(ctx context.Context, changeID string) error {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT period_id, org_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id,
		       amount_units, source, source_reference_id, policy_version, period_start, period_end
		FROM entitlement_periods
		WHERE change_id = $1
		  AND calculation_kind = 'upgrade_delta'
		  AND entitlement_state = 'active'
		ORDER BY period_id
	`, changeID)
	if err != nil {
		return fmt.Errorf("query contract change delta periods %s: %w", changeID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var periodID string
		var orgIDText string
		var scopeText string
		var productID string
		var bucketID string
		var skuID string
		var amount int64
		var source string
		var sourceRef string
		var policyVersion string
		var periodStart time.Time
		var periodEnd time.Time
		if err := rows.Scan(&periodID, &orgIDText, &scopeText, &productID, &bucketID, &skuID, &amount, &source, &sourceRef, &policyVersion, &periodStart, &periodEnd); err != nil {
			return fmt.Errorf("scan contract change delta period: %w", err)
		}
		orgID, err := strconv.ParseUint(orgIDText, 10, 64)
		if err != nil {
			return fmt.Errorf("parse delta period org_id %q: %w", orgIDText, err)
		}
		scope, err := ParseGrantScopeType(scopeText)
		if err != nil {
			return err
		}
		if amount <= 0 {
			continue
		}
		period := GrantPeriod{Start: periodStart.UTC(), End: periodEnd.UTC()}
		_, err = c.IssueCreditGrant(ctx, CreditGrant{
			OrgID:               OrgID(orgID),
			ScopeType:           scope,
			ScopeProductID:      productID,
			ScopeBucketID:       bucketID,
			ScopeSKUID:          skuID,
			Amount:              uint64(amount),
			Source:              source,
			SourceReferenceID:   sourceRef,
			EntitlementPeriodID: periodID,
			PolicyVersion:       policyVersion,
			ChangeID:            changeID,
			CalculationKind:     "upgrade_delta",
			StartsAt:            &period.Start,
			Period:              &period,
			ExpiresAt:           &period.End,
		})
		if err != nil {
			return fmt.Errorf("issue upgrade delta grant for period %s: %w", periodID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate contract change delta periods: %w", err)
	}
	return nil
}

func entitlementPositiveDeltas(current []EntitlementPolicy, target []EntitlementPolicy) ([]contractEntitlementDelta, error) {
	currentByScope := make(map[entitlementScopeKey]EntitlementPolicy, len(current))
	for _, policy := range current {
		currentByScope[policy.ScopeKey()] = policy
	}
	targetByScope := make(map[entitlementScopeKey]EntitlementPolicy, len(target))
	for _, policy := range target {
		targetByScope[policy.ScopeKey()] = policy
	}
	for key, policy := range currentByScope {
		targetPolicy, ok := targetByScope[key]
		if !ok {
			return nil, fmt.Errorf("%w: target plan removes entitlement scope %s", ErrUnsupportedContractChange, key.String())
		}
		if targetPolicy.AmountUnits < policy.AmountUnits {
			return nil, fmt.Errorf("%w: target plan reduces entitlement scope %s", ErrUnsupportedContractChange, key.String())
		}
	}
	out := make([]contractEntitlementDelta, 0, len(targetByScope))
	for key, targetPolicy := range targetByScope {
		currentAmount := uint64(0)
		if currentPolicy, ok := currentByScope[key]; ok {
			currentAmount = currentPolicy.AmountUnits
		}
		if targetPolicy.AmountUnits > currentAmount {
			out = append(out, contractEntitlementDelta{Policy: targetPolicy, Amount: targetPolicy.AmountUnits - currentAmount})
		}
	}
	return out, nil
}

func (p EntitlementPolicy) ScopeKey() entitlementScopeKey {
	return entitlementScopeKey{
		Source:         p.Source,
		ScopeType:      p.ScopeType,
		ScopeProductID: p.ScopeProductID,
		ScopeBucketID:  p.ScopeBucketID,
		ScopeSKUID:     p.ScopeSKUID,
	}
}

func (k entitlementScopeKey) String() string {
	return k.Source.String() + ":" + k.ScopeType.String() + ":" + k.ScopeProductID + ":" + k.ScopeBucketID + ":" + k.ScopeSKUID
}

func (c *Client) loadActivePlan(ctx context.Context, planID string) (PlanRecord, error) {
	var record PlanRecord
	var monthlyAmount int64
	var annualAmount int64
	err := c.pg.QueryRowContext(ctx, `
		SELECT plan_id, product_id, display_name, billing_mode, tier, currency,
		       monthly_amount_cents, annual_amount_cents, active, is_default
		FROM plans
		WHERE plan_id = $1
		  AND active
	`, planID).Scan(
		&record.PlanID,
		&record.ProductID,
		&record.DisplayName,
		&record.BillingMode,
		&record.Tier,
		&record.Currency,
		&monthlyAmount,
		&annualAmount,
		&record.Active,
		&record.IsDefault,
	)
	if err != nil {
		return PlanRecord{}, fmt.Errorf("load active plan %s: %w", planID, err)
	}
	if monthlyAmount < 0 || annualAmount < 0 {
		return PlanRecord{}, fmt.Errorf("plan %s has negative amount", planID)
	}
	record.MonthlyAmountCents = uint64(monthlyAmount)
	record.AnnualAmountCents = uint64(annualAmount)
	return record, nil
}

type contractChangeExistingState struct {
	Result            ContractChangeResult
	InvoiceNumber     string
	ProviderInvoiceID string
	PaymentStatus     string
	Status            string
}

func (c *Client) loadContractChangeState(ctx context.Context, changeID string, successURL string) (contractChangeExistingState, bool, error) {
	var result ContractChangeResult
	var amount int64
	var url string
	var invoiceNumber string
	var providerInvoiceID string
	var paymentStatus string
	err := c.pg.QueryRowContext(ctx, `
		SELECT cc.change_id, cc.invoice_id, cc.state, cc.price_delta_units,
		       COALESCE(NULLIF(bi.stripe_hosted_invoice_url, ''), $2),
		       COALESCE(bi.invoice_number, ''),
		       COALESCE(NULLIF(cc.provider_invoice_id, ''), bi.stripe_invoice_id, ''),
		       COALESCE(bi.payment_status, '')
		FROM contract_changes cc
		LEFT JOIN billing_invoices bi ON bi.invoice_id = cc.invoice_id
		WHERE cc.change_id = $1
	`, changeID, successURL).Scan(&result.ChangeID, &result.InvoiceID, &result.Status, &amount, &url, &invoiceNumber, &providerInvoiceID, &paymentStatus)
	if err == sql.ErrNoRows {
		return contractChangeExistingState{}, false, nil
	}
	if err != nil {
		return contractChangeExistingState{}, false, fmt.Errorf("load contract change result %s: %w", changeID, err)
	}
	if amount < 0 {
		return contractChangeExistingState{}, false, fmt.Errorf("contract change %s has negative price delta", changeID)
	}
	result.URL = url
	result.PriceDeltaUnits = uint64(amount)
	return contractChangeExistingState{
		Result:            result,
		InvoiceNumber:     invoiceNumber,
		ProviderInvoiceID: providerInvoiceID,
		PaymentStatus:     paymentStatus,
		Status:            result.Status,
	}, true, nil
}

func (c *Client) failContractChangePayment(ctx context.Context, changeID string, invoiceID string, cause error) error {
	_, err := c.pg.ExecContext(ctx, `
		UPDATE contract_changes
		SET state = 'failed',
		    failure_reason = $2,
		    state_version = state_version + 1,
		    updated_at = now()
		WHERE change_id = $1
		  AND state <> 'applied'
	`, changeID, cause.Error())
	if err != nil {
		return fmt.Errorf("mark contract change %s failed: %w", changeID, err)
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE billing_invoices
		SET payment_status = 'failed',
		    status = 'payment_failed',
		    block_reason = $2,
		    updated_at = now()
		WHERE invoice_id = $1
		  AND status <> 'paid'
	`, invoiceID, cause.Error())
	if err != nil {
		return fmt.Errorf("mark contract change invoice %s failed: %w", invoiceID, err)
	}
	return nil
}

func contractEntitlementDeltaPeriodID(orgID OrgID, contractID string, fromPhaseID string, toPhaseID string, lineID string, policy EntitlementPolicy, changeID string, periodStart time.Time, periodEnd time.Time) string {
	return deterministicTextID(
		"contract-entitlement-period-upgrade-delta",
		strconv.FormatUint(uint64(orgID), 10),
		contractID,
		fromPhaseID,
		toPhaseID,
		lineID,
		policy.Source.String(),
		policy.PolicyID,
		policy.PolicyVersion,
		policy.ScopeKey().String(),
		changeID,
		periodStart.UTC().Format(time.RFC3339Nano),
		periodEnd.UTC().Format(time.RFC3339Nano),
	)
}

func contractEntitlementDeltaSourceReference(contractID string, fromPhaseID string, toPhaseID string, lineID string, policy EntitlementPolicy, changeID string, periodStart time.Time, periodEnd time.Time) string {
	return "contract_delta:" + contractID + ":" + fromPhaseID + ":" + toPhaseID + ":" + lineID + ":" + policy.PolicyID + ":" + policy.PolicyVersion + ":" + changeID + ":" +
		periodStart.UTC().Format(time.RFC3339Nano) + ":" + periodEnd.UTC().Format(time.RFC3339Nano)
}

func contractChangePayload(quote contractChangeQuote) ([]byte, error) {
	payload, err := json.Marshal(map[string]string{
		"change_id":              quote.ChangeID,
		"contract_id":            quote.ContractID,
		"org_id":                 strconv.FormatUint(uint64(quote.OrgID), 10),
		"product_id":             quote.ProductID,
		"change_type":            "upgrade",
		"from_plan_id":           quote.FromPlanID,
		"target_plan_id":         quote.TargetPlanID,
		"from_phase_id":          quote.FromPhaseID,
		"to_phase_id":            quote.ToPhaseID,
		"cycle_id":               quote.Cycle.CycleID,
		"invoice_id":             quote.InvoiceID,
		"invoice_number":         quote.InvoiceNumber,
		"price_delta_units":      strconv.FormatUint(quote.PriceDeltaUnits, 10),
		"price_delta_cents":      strconv.FormatUint(quote.PriceDeltaCents, 10),
		"proration_numerator":    strconv.FormatInt(quote.ProrationNumeratorNS, 10),
		"proration_denominator":  strconv.FormatInt(quote.ProrationDenominatorNS, 10),
		"entitlement_delta_mode": "positive_delta",
		"effective_at":           quote.EffectiveAt.Format(time.RFC3339Nano),
		"requested_at":           quote.RequestedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal contract change payload %s: %w", quote.ChangeID, err)
	}
	return payload, nil
}

func renderContractChangeInvoiceHTML(quote contractChangeQuote) string {
	return "<article><h1>Invoice " + quote.InvoiceNumber + "</h1><p>Prorated upgrade from " + quote.FromPlanID + " to " + quote.TargetPlanID + ": " + strconv.FormatUint(quote.PriceDeltaUnits, 10) + " ledger units.</p></article>"
}

func contractChangeStripeIdempotencyKey(changeID string) string {
	return "forge-metal:contract-change:" + changeID
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
