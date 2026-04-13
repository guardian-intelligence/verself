package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

func (c *Client) activateCatalogContract(
	ctx context.Context,
	orgID OrgID,
	productID string,
	planID string,
	contractID string,
	phaseID string,
	cadence string,
	effectiveAt time.Time,
	paymentState EntitlementPaymentState,
) error {
	if cadence == "" {
		cadence = string(CadenceMonthly)
	}
	if cadence != string(CadenceMonthly) {
		return fmt.Errorf("%w: %q", ErrUnsupportedCadence, cadence)
	}
	if paymentState == "" {
		paymentState = PaymentPending
	}
	cadenceKind := "anniversary_monthly"
	existing, err := c.GetContract(ctx, orgID, contractID)
	newContract := false
	if errors.Is(err, ErrContractNotFound) {
		newContract = true
	} else if err != nil {
		return err
	}
	if !newContract {
		if existing.Status == "ended" || existing.Status == "voided" {
			return fmt.Errorf("%w: contract %s is %s", ErrUnsupportedContractChange, contractID, existing.Status)
		}
		if existing.PlanID != "" && existing.PlanID != planID {
			return fmt.Errorf("%w: contract %s current plan %s target plan %s", ErrUnsupportedContractChange, contractID, existing.PlanID, planID)
		}
	}

	var cycle BillingCycle
	if newContract {
		cycle, err = c.EnsureContractBillingCycle(ctx, orgID, productID, effectiveAt)
	} else {
		cycle, err = c.EnsureOpenBillingCycle(ctx, orgID, productID)
	}
	if err != nil {
		return err
	}
	policies, err := c.contractEntitlementPolicies(ctx, planID, cycle.StartsAt, cycle.EndsAt)
	if err != nil {
		return err
	}

	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin contract activation: %w", err)
	}
	defer tx.Rollback()

	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO contracts (
			contract_id, org_id, product_id, display_name, contract_kind, status,
			payment_state, entitlement_state, cadence_kind, billing_anchor_at,
			starts_at, overage_policy
		)
		VALUES ($1, $2, $3, $4, 'self_serve', 'active',
		        $5, 'active', $6, $7,
		        $7, 'bill_overages')
		ON CONFLICT (contract_id) DO UPDATE
		SET status = 'active',
		    payment_state = EXCLUDED.payment_state,
		    entitlement_state = 'active',
		    ends_at = NULL,
		    updated_at = now()
	`, contractID, orgIDText, productID, planID, string(paymentState), cadenceKind, effectiveAt.UTC()); err != nil {
		return fmt.Errorf("upsert contract %s: %w", contractID, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO contract_phases (
			phase_id, contract_id, org_id, product_id, plan_id, phase_kind, state,
			payment_state, entitlement_state, cadence_kind, effective_start,
			recurrence_anchor_at, overage_policy
		)
		VALUES ($1, $2, $3, $4, $5, 'catalog_plan', 'active',
		        $6, 'active', $7, $8,
		        $8, 'bill_overages')
		ON CONFLICT (phase_id) DO UPDATE
		SET state = 'active',
		    payment_state = EXCLUDED.payment_state,
		    entitlement_state = 'active',
		    effective_end = NULL,
		    updated_at = now()
	`, phaseID, contractID, orgIDText, productID, planID, string(paymentState), cadenceKind, effectiveAt.UTC()); err != nil {
		return fmt.Errorf("upsert contract phase %s: %w", phaseID, err)
	}

	for _, policy := range policies {
		lineID := contractEntitlementLineID(phaseID, policy.PolicyID)
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
			policy.AmountUnits, string(policy.Cadence), string(policy.ProrationMode)); err != nil {
			return fmt.Errorf("copy contract entitlement line %s: %w", policy.PolicyID, err)
		}
	}
	if newContract {
		if err := insertBillingEventTx(ctx, tx, contractCreatedEvent(orgID, productID, contractID, planID, effectiveAt)); err != nil {
			return err
		}
	}
	if err := insertBillingEventTx(ctx, tx, contractPhaseStartedEvent(orgID, productID, contractID, phaseID, planID, cycle, effectiveAt)); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit contract activation: %w", err)
	}

	for _, policy := range policies {
		if policy.ActiveFrom.Before(effectiveAt.UTC()) {
			policy.ActiveFrom = effectiveAt.UTC()
		}
		lineID := contractEntitlementLineID(phaseID, policy.PolicyID)
		period, ok := contractEntitlementPeriod(orgID, contractID, phaseID, lineID, policy, cycle.StartsAt, cycle.EndsAt, paymentState, EntitlementActive)
		if !ok {
			continue
		}
		period.CycleID = cycle.CycleID
		if err := c.ensureEntitlementPeriod(ctx, period); err != nil {
			return err
		}
		periodStart := period.PeriodStart
		periodEnd := period.PeriodEnd
		if _, err := c.IssueCreditGrant(ctx, CreditGrant{
			OrgID:               orgID,
			ScopeType:           period.ScopeType,
			ScopeProductID:      period.ScopeProductID,
			ScopeBucketID:       period.ScopeBucketID,
			ScopeSKUID:          period.ScopeSKUID,
			Amount:              period.AmountUnits,
			Source:              period.Source.String(),
			SourceReferenceID:   period.SourceReferenceID,
			EntitlementPeriodID: period.PeriodID,
			PolicyVersion:       period.PolicyVersion,
			StartsAt:            &periodStart,
			Period:              &GrantPeriod{Start: periodStart, End: periodEnd},
			ExpiresAt:           &periodEnd,
		}); err != nil {
			return fmt.Errorf("issue contract grant for policy %s: %w", policy.PolicyID, err)
		}
	}
	return nil
}

func contractEntitlementLineID(phaseID, policyID string) string {
	return deterministicTextID("contract-entitlement-line", phaseID, policyID)
}

func (c *Client) CancelContract(ctx context.Context, orgID OrgID, contractID string) (ContractRecord, error) {
	if err := ctx.Err(); err != nil {
		return ContractRecord{}, err
	}
	if contractID == "" {
		return ContractRecord{}, fmt.Errorf("%w: contract_id is required", ErrContractNotFound)
	}

	record, err := c.GetContract(ctx, orgID, contractID)
	if err != nil {
		return ContractRecord{}, err
	}
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, record.ProductID)
	if err != nil {
		return ContractRecord{}, err
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE contracts
		SET status = 'cancel_scheduled',
		    ends_at = $3,
		    updated_at = now()
		WHERE contract_id = $1
		  AND org_id = $2
		  AND status NOT IN ('ended', 'voided')
	`, contractID, strconv.FormatUint(uint64(orgID), 10), cycle.EndsAt)
	if err != nil {
		return ContractRecord{}, fmt.Errorf("schedule contract cancellation: %w", err)
	}
	_, err = c.pg.ExecContext(ctx, `
		UPDATE contract_phases
		SET effective_end = COALESCE(effective_end, $3),
		    updated_at = now()
		WHERE contract_id = $1
		  AND org_id = $2
		  AND state IN ('scheduled', 'pending_payment', 'active', 'grace')
	`, contractID, strconv.FormatUint(uint64(orgID), 10), cycle.EndsAt)
	if err != nil {
		return ContractRecord{}, fmt.Errorf("schedule contract phase cancellation: %w", err)
	}
	return c.GetContract(ctx, orgID, contractID)
}

func (c *Client) GetContract(ctx context.Context, orgID OrgID, contractID string) (ContractRecord, error) {
	var record ContractRecord
	var paymentState string
	var entitlementState string
	var endsAt sql.NullTime
	var phaseStart sql.NullTime
	var phaseEnd sql.NullTime
	now := c.clock().UTC()
	err := c.pg.QueryRowContext(ctx, `
		WITH current_phase AS (
			SELECT phase_id, plan_id, effective_start, effective_end
			FROM contract_phases
			WHERE contract_id = $1
			  AND effective_start <= $3
			  AND (effective_end IS NULL OR effective_end > $3)
			ORDER BY effective_start DESC, phase_id DESC
			LIMIT 1
		)
		SELECT c.contract_id, c.org_id, c.product_id, COALESCE(cp.plan_id, ''), COALESCE(cp.phase_id, ''),
		       c.cadence_kind, c.status, c.payment_state, c.entitlement_state,
		       c.starts_at, c.ends_at, cp.effective_start, cp.effective_end
		FROM contracts c
		LEFT JOIN current_phase cp ON true
		WHERE c.contract_id = $1
		  AND c.org_id = $2
	`, contractID, strconv.FormatUint(uint64(orgID), 10), now).Scan(
		&record.ContractID,
		&record.OrgID,
		&record.ProductID,
		&record.PlanID,
		&record.PhaseID,
		&record.CadenceKind,
		&record.Status,
		&paymentState,
		&entitlementState,
		&record.StartsAt,
		&endsAt,
		&phaseStart,
		&phaseEnd,
	)
	if err == sql.ErrNoRows {
		return ContractRecord{}, ErrContractNotFound
	}
	if err != nil {
		return ContractRecord{}, fmt.Errorf("load contract %s: %w", contractID, err)
	}
	record.PaymentState = EntitlementPaymentState(paymentState)
	record.EntitlementState = EntitlementState(entitlementState)
	record.StartsAt = record.StartsAt.UTC()
	if endsAt.Valid {
		value := endsAt.Time.UTC()
		record.EndsAt = &value
	}
	if phaseStart.Valid {
		value := phaseStart.Time.UTC()
		record.PhaseStart = &value
	}
	if phaseEnd.Valid {
		value := phaseEnd.Time.UTC()
		record.PhaseEnd = &value
	}
	return record, nil
}
