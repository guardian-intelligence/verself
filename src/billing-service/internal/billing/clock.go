package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/verself/billing-service/internal/store"
)

func (c *Client) GetBusinessClock(ctx context.Context, orgID OrgID, productID string) (BusinessClockState, error) {
	if productID == "" {
		return BusinessClockState{}, fmt.Errorf("product_id is required")
	}
	scopeKind, scopeID := orgProductClockScope(orgID, productID)
	businessNow, err := c.BusinessNow(ctx, c.queries, orgID, productID)
	if err != nil {
		return BusinessClockState{}, err
	}
	state := BusinessClockState{OrgID: orgID, ProductID: productID, ScopeKind: scopeKind, ScopeID: scopeID, BusinessNow: businessNow}
	var generation int64
	err = c.pg.QueryRow(ctx, `
		SELECT business_now, generation
		FROM billing_clock_overrides
		WHERE scope_kind = $1 AND scope_id = $2
	`, scopeKind, scopeID).Scan(&state.BusinessNow, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return BusinessClockState{}, fmt.Errorf("load business clock override: %w", err)
	}
	state.BusinessNow = state.BusinessNow.UTC()
	state.HasOverride = true
	state.Generation = uint64(generation)
	return state, nil
}

func (c *Client) SetBusinessClock(ctx context.Context, orgID OrgID, productID string, businessNow time.Time, reason string) (BusinessClockState, error) {
	if businessNow.IsZero() {
		return BusinessClockState{}, fmt.Errorf("business_now is required")
	}
	if productID == "" {
		return BusinessClockState{}, fmt.Errorf("product_id is required")
	}
	scopeKind, scopeID := orgProductClockScope(orgID, productID)
	businessNow = businessNow.UTC()
	var generation int64
	if err := c.WithTx(ctx, "billing.clock.set", func(ctx context.Context, tx pgx.Tx, _ *store.Queries) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO billing_clock_overrides (scope_kind, scope_id, business_now, reason, updated_by)
			VALUES ($1, $2, $3, $4, 'billing-service')
			ON CONFLICT (scope_kind, scope_id) DO UPDATE
			SET business_now = EXCLUDED.business_now,
			    reason = EXCLUDED.reason,
			    updated_by = EXCLUDED.updated_by,
			    generation = billing_clock_overrides.generation + 1
			RETURNING generation
		`, scopeKind, scopeID, businessNow, reason).Scan(&generation)
		if err != nil {
			return fmt.Errorf("set business clock: %w", err)
		}
		return appendEvent(ctx, tx, c.queries.WithTx(tx), eventFact{EventType: "billing_clock_set", AggregateType: "billing_clock", AggregateID: scopeID, OrgID: orgID, ProductID: productID, OccurredAt: time.Now().UTC(), Payload: map[string]any{"scope_kind": scopeKind, "scope_id": scopeID, "business_now": businessNow.Format(time.RFC3339Nano), "generation": generation, "reason": reason}})
	}); err != nil {
		return BusinessClockState{}, err
	}
	return c.reconcileClockTarget(ctx, orgID, productID, DueWorkSummary{})
}

func (c *Client) AdvanceBusinessClock(ctx context.Context, orgID OrgID, productID string, delta time.Duration, reason string) (BusinessClockState, error) {
	if delta <= 0 {
		return BusinessClockState{}, fmt.Errorf("advance duration must be positive")
	}
	if productID == "" {
		return BusinessClockState{}, fmt.Errorf("product_id is required")
	}
	scopeKind, scopeID := orgProductClockScope(orgID, productID)
	var businessNow time.Time
	var generation int64
	if err := c.WithTx(ctx, "billing.clock.advance", func(ctx context.Context, tx pgx.Tx, _ *store.Queries) error {
		base, err := c.BusinessNow(ctx, c.queries.WithTx(tx), orgID, productID)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			SELECT business_now
			FROM billing_clock_overrides
			WHERE scope_kind = $1 AND scope_id = $2
			FOR UPDATE
		`, scopeKind, scopeID).Scan(&base)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lock business clock override: %w", err)
		}
		businessNow = base.UTC().Add(delta)
		err = tx.QueryRow(ctx, `
			INSERT INTO billing_clock_overrides (scope_kind, scope_id, business_now, reason, updated_by)
			VALUES ($1, $2, $3, $4, 'billing-service')
			ON CONFLICT (scope_kind, scope_id) DO UPDATE
			SET business_now = EXCLUDED.business_now,
			    reason = EXCLUDED.reason,
			    updated_by = EXCLUDED.updated_by,
			    generation = billing_clock_overrides.generation + 1
			RETURNING generation
		`, scopeKind, scopeID, businessNow, reason).Scan(&generation)
		if err != nil {
			return fmt.Errorf("advance business clock: %w", err)
		}
		return appendEvent(ctx, tx, c.queries.WithTx(tx), eventFact{EventType: "billing_clock_advanced", AggregateType: "billing_clock", AggregateID: scopeID, OrgID: orgID, ProductID: productID, OccurredAt: time.Now().UTC(), Payload: map[string]any{"scope_kind": scopeKind, "scope_id": scopeID, "business_now": businessNow.Format(time.RFC3339Nano), "advance_seconds": int64(delta / time.Second), "generation": generation, "reason": reason}})
	}); err != nil {
		return BusinessClockState{}, err
	}
	return c.reconcileClockTarget(ctx, orgID, productID, DueWorkSummary{})
}

func (c *Client) ClearBusinessClock(ctx context.Context, orgID OrgID, productID string, reason string) (BusinessClockState, error) {
	if productID == "" {
		return BusinessClockState{}, fmt.Errorf("product_id is required")
	}
	scopeKind, scopeID := orgProductClockScope(orgID, productID)
	if err := c.WithTx(ctx, "billing.clock.clear", func(ctx context.Context, tx pgx.Tx, _ *store.Queries) error {
		_, err := tx.Exec(ctx, `DELETE FROM billing_clock_overrides WHERE scope_kind = $1 AND scope_id = $2`, scopeKind, scopeID)
		if err != nil {
			return fmt.Errorf("clear business clock: %w", err)
		}
		return appendEvent(ctx, tx, c.queries.WithTx(tx), eventFact{EventType: "billing_clock_cleared", AggregateType: "billing_clock", AggregateID: scopeID, OrgID: orgID, ProductID: productID, OccurredAt: time.Now().UTC(), Payload: map[string]any{"scope_kind": scopeKind, "scope_id": scopeID, "reason": reason}})
	}); err != nil {
		return BusinessClockState{}, err
	}
	return c.reconcileClockTarget(ctx, orgID, productID, DueWorkSummary{})
}

func (c *Client) ResetBusinessClockToWallClock(ctx context.Context, orgID OrgID, productID string, reason string) (BusinessClockState, error) {
	if productID == "" {
		return BusinessClockState{}, fmt.Errorf("product_id is required")
	}
	scopeKind, scopeID := orgProductClockScope(orgID, productID)
	wallNow := time.Now().UTC()
	repair := BusinessClockRepairSummary{}
	if err := c.WithTx(ctx, "billing.clock.reset_to_wall_clock", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		if err := c.lockOrgProductTx(ctx, tx, orgID, productID); err != nil {
			return err
		}
		var previous time.Time
		err := tx.QueryRow(ctx, `
			SELECT business_now
			FROM billing_clock_overrides
			WHERE scope_kind = $1 AND scope_id = $2
			FOR UPDATE
		`, scopeKind, scopeID).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lock business clock override: %w", err)
		}
		if err == nil {
			previous = previous.UTC()
			repair.PreviousBusinessNow = &previous
		}
		if _, err := tx.Exec(ctx, `DELETE FROM billing_clock_overrides WHERE scope_kind = $1 AND scope_id = $2`, scopeKind, scopeID); err != nil {
			return fmt.Errorf("clear business clock override: %w", err)
		}

		paid, err := c.activePaidPhaseForWallClockResetTx(ctx, tx, orgID, productID, wallNow)
		if err != nil {
			return err
		}
		startsAt := monthStartUTC(wallNow)
		endsAt := nextMonth(wallNow)
		anchorAt := startsAt
		cycleSeq := int64(0)
		cadence := "calendar_monthly"
		if paid.ok {
			anchorAt = paid.anchorAt
			if anchorAt.IsZero() || anchorAt.After(wallNow) {
				anchorAt = wallNow
			}
			startsAt, endsAt, cycleSeq = cycleBoundsContaining("anniversary_monthly", anchorAt, wallNow)
			cadence = "anniversary_monthly"
			if err := c.shiftActiveContractToWallClockTx(ctx, tx, paid, wallNow); err != nil {
				return err
			}
		}

		voided, err := c.voidCyclesForWallClockResetTx(ctx, tx, orgID, productID, startsAt, endsAt, wallNow, reason)
		if err != nil {
			return err
		}
		repair.VoidedCycleIDs = voided
		closedGrantIDs, err := c.closeCurrentEntitlementGrantsForWallClockResetTx(ctx, tx, orgID, productID, startsAt, endsAt, wallNow)
		if err != nil {
			return err
		}
		repair.ClosedGrantIDs = closedGrantIDs
		if err := c.voidCurrentEntitlementPeriodsForWallClockResetTx(ctx, tx, orgID, productID, startsAt, endsAt, wallNow, reason); err != nil {
			return err
		}
		if err := c.reopenWallClockTargetEntitlementsTx(ctx, tx, orgID, productID, startsAt, endsAt, wallNow); err != nil {
			return err
		}
		cycle, err := c.insertWallClockResetCycleTx(ctx, tx, q, orgID, productID, anchorAt, cycleSeq, startsAt, endsAt, cadence, wallNow)
		if err != nil {
			return err
		}
		repair.CurrentCycleID = cycle.CycleID
		reassignedWindowIDs, err := c.reassignWallClockWindowsTx(ctx, tx, orgID, productID, startsAt, endsAt, cycle.CycleID, wallNow)
		if err != nil {
			return err
		}
		repair.ReassignedWindowIDs = reassignedWindowIDs
		payload := map[string]any{
			"scope_kind":                   scopeKind,
			"scope_id":                     scopeID,
			"reset_at":                     wallNow.Format(time.RFC3339Nano),
			"reason":                       reason,
			"voided_cycle_ids":             repair.VoidedCycleIDs,
			"voided_cycle_count":           len(repair.VoidedCycleIDs),
			"closed_entitlement_grant_ids": repair.ClosedGrantIDs,
			"closed_entitlement_grants":    len(repair.ClosedGrantIDs),
			"reassigned_window_ids":        repair.ReassignedWindowIDs,
			"reassigned_window_count":      len(repair.ReassignedWindowIDs),
			"current_cycle_id":             repair.CurrentCycleID,
			"preserved_paid_plan":          paid.ok,
			"preserved_purchase_balances":  true,
			"fixture_repair":               true,
			"closed_grant_ledger_sweep":    false,
		}
		if repair.PreviousBusinessNow != nil {
			payload["previous_business_now"] = repair.PreviousBusinessNow.Format(time.RFC3339Nano)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_clock_reset_to_wall_clock", AggregateType: "billing_clock", AggregateID: scopeID, OrgID: orgID, ProductID: productID, OccurredAt: wallNow, Payload: payload})
	}); err != nil {
		return BusinessClockState{}, err
	}
	state, err := c.reconcileClockTarget(ctx, orgID, productID, DueWorkSummary{})
	if err != nil {
		return BusinessClockState{}, err
	}
	state.Repair = repair
	return state, nil
}

type wallClockPaidPhase struct {
	ok         bool
	contractID string
	phaseID    string
	planID     string
	anchorAt   time.Time
}

func (c *Client) activePaidPhaseForWallClockResetTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, wallNow time.Time) (wallClockPaidPhase, error) {
	row := tx.QueryRow(ctx, `
		SELECT c.contract_id, p.phase_id, COALESCE(p.plan_id, ''), c.starts_at
		FROM contracts c
		JOIN contract_phases p ON p.contract_id = c.contract_id
		WHERE c.org_id = $1
		  AND c.product_id = $2
		  AND c.state IN ('active', 'past_due', 'cancel_scheduled')
		  AND p.state IN ('active', 'grace')
		ORDER BY CASE WHEN p.effective_start <= $3 AND (p.effective_end IS NULL OR p.effective_end > $3) THEN 0 ELSE 1 END,
		         p.effective_start DESC,
		         p.phase_id DESC
		LIMIT 1
		FOR UPDATE OF c, p
	`, orgIDText(orgID), productID, wallNow)
	var paid wallClockPaidPhase
	if err := row.Scan(&paid.contractID, &paid.phaseID, &paid.planID, &paid.anchorAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wallClockPaidPhase{}, nil
		}
		return wallClockPaidPhase{}, fmt.Errorf("load active paid phase for wall-clock reset: %w", err)
	}
	paid.ok = paid.planID != ""
	paid.anchorAt = paid.anchorAt.UTC()
	return paid, nil
}

func (c *Client) shiftActiveContractToWallClockTx(ctx context.Context, tx pgx.Tx, paid wallClockPaidPhase, wallNow time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE contracts
		SET starts_at = LEAST(starts_at, $2),
		    updated_at = now()
		WHERE contract_id = $1
	`, paid.contractID, wallNow); err != nil {
		return fmt.Errorf("shift active contract to wall clock: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE contract_phases
		SET effective_start = LEAST(effective_start, $2),
		    effective_end = CASE WHEN effective_end IS NOT NULL AND effective_end <= $2 THEN NULL ELSE effective_end END,
		    activated_at = COALESCE(activated_at, $2),
		    updated_at = now()
		WHERE phase_id = $1
	`, paid.phaseID, wallNow); err != nil {
		return fmt.Errorf("shift active contract phase to wall clock: %w", err)
	}
	return nil
}

func (c *Client) voidCyclesForWallClockResetTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, startsAt, endsAt, wallNow time.Time, reason string) ([]string, error) {
	rows, err := tx.Query(ctx, `
		UPDATE billing_cycles
		SET status = 'voided',
		    finalized_at = COALESCE(finalized_at, $5::timestamptz),
		    closed_reason = CASE WHEN closed_reason = '' THEN 'wall_clock_reset' ELSE closed_reason END,
		    metadata = metadata || jsonb_build_object('voided_by', 'billing-wall-clock', 'voided_at', $5::timestamptz::text, 'reason', $6::text),
		    updated_at = now()
		WHERE org_id = $1
		  AND product_id = $2
		  AND status <> 'voided'
		  AND (
		    status IN ('open', 'closing')
		    OR tstzrange(starts_at, ends_at, '[)') && tstzrange($3::timestamptz, $4::timestamptz, '[)')
		  )
		RETURNING cycle_id
	`, orgIDText(orgID), productID, startsAt, endsAt, wallNow, reason)
	if err != nil {
		return nil, fmt.Errorf("void cycles for wall-clock reset: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (c *Client) closeCurrentEntitlementGrantsForWallClockResetTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, startsAt, endsAt, wallNow time.Time) ([]string, error) {
	rows, err := tx.Query(ctx, `
		UPDATE credit_grants
		SET closed_at = $5::timestamptz,
		    closed_reason = 'wall_clock_reset',
		    metadata = metadata || jsonb_build_object('closed_by', 'billing-wall-clock', 'closed_at', $5::timestamptz::text),
		    updated_at = now()
		WHERE org_id = $1
		  AND closed_at IS NULL
		  AND source IN ('free_tier', 'contract')
		  AND (
		    scope_product_id = $2
		    OR entitlement_period_id IN (SELECT period_id FROM entitlement_periods WHERE org_id = $1 AND product_id = $2)
		  )
		  AND NOT (period_start = $3::timestamptz AND period_end = $4::timestamptz)
		RETURNING grant_id
	`, orgIDText(orgID), productID, startsAt, endsAt, wallNow)
	if err != nil {
		return nil, fmt.Errorf("close entitlement grants for wall-clock reset: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (c *Client) voidCurrentEntitlementPeriodsForWallClockResetTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, startsAt, endsAt, wallNow time.Time, reason string) error {
	_, err := tx.Exec(ctx, `
		UPDATE entitlement_periods
		SET entitlement_state = 'voided',
		    metadata = metadata || jsonb_build_object('voided_by', 'billing-wall-clock', 'voided_at', $5::timestamptz::text, 'reason', $6::text),
		    updated_at = now()
		WHERE org_id = $1
		  AND product_id = $2
		  AND source IN ('free_tier', 'contract')
		  AND entitlement_state IN ('scheduled', 'active', 'grace')
		  AND NOT (period_start = $3::timestamptz AND period_end = $4::timestamptz)
	`, orgIDText(orgID), productID, startsAt, endsAt, wallNow, reason)
	if err != nil {
		return fmt.Errorf("void entitlement periods for wall-clock reset: %w", err)
	}
	return nil
}

func (c *Client) reopenWallClockTargetEntitlementsTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, startsAt, endsAt, wallNow time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE entitlement_periods
		SET entitlement_state = 'active',
		    metadata = (metadata - 'voided_by' - 'voided_at' - 'reason') || jsonb_build_object('reopened_by', 'billing-wall-clock', 'reopened_at', $5::timestamptz::text),
		    updated_at = now()
		WHERE org_id = $1
		  AND product_id = $2
		  AND source IN ('free_tier', 'contract')
		  AND period_start = $3::timestamptz
		  AND period_end = $4::timestamptz
		  AND entitlement_state = 'voided'
		  AND metadata->>'voided_by' = 'billing-wall-clock'
	`, orgIDText(orgID), productID, startsAt, endsAt, wallNow); err != nil {
		return fmt.Errorf("reopen target entitlement periods for wall-clock reset: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE credit_grants
		SET closed_at = NULL,
		    closed_reason = '',
		    metadata = (metadata - 'closed_by' - 'closed_at') || jsonb_build_object('reopened_by', 'billing-wall-clock', 'reopened_at', $5::timestamptz::text),
		    updated_at = now()
		WHERE org_id = $1
		  AND source IN ('free_tier', 'contract')
		  AND period_start = $3::timestamptz
		  AND period_end = $4::timestamptz
		  AND closed_reason = 'wall_clock_reset'
		  AND (
		    scope_product_id = $2
		    OR entitlement_period_id IN (SELECT period_id FROM entitlement_periods WHERE org_id = $1 AND product_id = $2)
		  )
	`, orgIDText(orgID), productID, startsAt, endsAt, wallNow); err != nil {
		return fmt.Errorf("reopen target entitlement grants for wall-clock reset: %w", err)
	}
	return nil
}

func (c *Client) insertWallClockResetCycleTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, anchorAt time.Time, cycleSeq int64, startsAt, endsAt time.Time, cadence string, wallNow time.Time) (billingCycle, error) {
	cycle := billingCycle{CycleID: cycleID(orgID, productID, startsAt), Currency: "usd", AnchorAt: anchorAt.UTC(), CycleSeq: cycleSeq, CadenceKind: cadence, StartsAt: startsAt.UTC(), EndsAt: endsAt.UTC()}
	tag, err := tx.Exec(ctx, `
		INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at, closed_reason, metadata)
		VALUES ($1,$2,$3,'usd',$4,$5,$6,$7,$8,'open',$8,'',jsonb_build_object('opened_by', 'billing-wall-clock'))
		ON CONFLICT (cycle_id) DO UPDATE
		SET currency = EXCLUDED.currency,
		    anchor_at = EXCLUDED.anchor_at,
		    cycle_seq = EXCLUDED.cycle_seq,
		    cadence_kind = EXCLUDED.cadence_kind,
		    starts_at = EXCLUDED.starts_at,
		    ends_at = EXCLUDED.ends_at,
		    status = 'open',
		    finalization_due_at = EXCLUDED.finalization_due_at,
		    closed_reason = '',
		    active_finalization_id = NULL,
		    successor_cycle_id = NULL,
		    closed_by_event_id = NULL,
		    closed_for_usage_at = NULL,
		    finalized_at = NULL,
		    metadata = (billing_cycles.metadata - 'voided_by' - 'voided_at' - 'reason') || jsonb_build_object('opened_by', 'billing-wall-clock'),
		    updated_at = now()
		WHERE billing_cycles.status = 'voided'
	`, cycle.CycleID, orgIDText(orgID), productID, cycle.AnchorAt, cycle.CycleSeq, cadence, cycle.StartsAt, cycle.EndsAt)
	if err != nil {
		return billingCycle{}, fmt.Errorf("insert wall-clock reset billing cycle: %w", err)
	}
	if tag.RowsAffected() == 0 {
		existing, ok, err := c.openBillingCycleContainingTx(ctx, tx, orgID, productID, wallNow)
		if err != nil {
			return billingCycle{}, err
		}
		if ok {
			return existing, nil
		}
		return billingCycle{}, fmt.Errorf("insert wall-clock reset billing cycle %s: conflicting cycle", cycle.CycleID)
	}
	if err := appendEvent(ctx, tx, q, eventFact{
		EventType:     "billing_cycle_opened",
		AggregateType: "billing_cycle",
		AggregateID:   cycle.CycleID,
		OrgID:         orgID,
		ProductID:     productID,
		OccurredAt:    wallNow,
		Payload: map[string]any{
			"cycle_id":     cycle.CycleID,
			"starts_at":    cycle.StartsAt.Format(time.RFC3339Nano),
			"ends_at":      cycle.EndsAt.Format(time.RFC3339Nano),
			"anchor_at":    cycle.AnchorAt.Format(time.RFC3339Nano),
			"cycle_seq":    cycle.CycleSeq,
			"cadence_kind": cycle.CadenceKind,
			"reason":       "wall_clock_reset",
		},
	}); err != nil {
		return billingCycle{}, err
	}
	return cycle, nil
}

func (c *Client) reassignWallClockWindowsTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, startsAt, endsAt time.Time, cycleID string, wallNow time.Time) ([]string, error) {
	rows, err := tx.Query(ctx, `
		UPDATE billing_windows
		SET cycle_id = $5,
		    metadata = metadata || jsonb_build_object(
		      'cycle_reassigned_by', 'billing-wall-clock',
		      'cycle_reassigned_at', $6::timestamptz::text,
		      'previous_cycle_id', cycle_id
		    ),
		    updated_at = now()
		WHERE org_id = $1
		  AND product_id = $2
		  AND state IN ('reserved', 'active', 'settling', 'settled')
		  AND window_start >= $3::timestamptz
		  AND window_start < $4::timestamptz
		  AND cycle_id <> $5
		RETURNING window_id
	`, orgIDText(orgID), productID, startsAt, endsAt, cycleID, wallNow)
	if err != nil {
		return nil, fmt.Errorf("reassign wall-clock billing windows: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (c *Client) reconcileClockTarget(ctx context.Context, orgID OrgID, productID string, summary DueWorkSummary) (BusinessClockState, error) {
	due, err := c.ApplyDueBillingWork(ctx, orgID, productID)
	if err != nil {
		return BusinessClockState{}, err
	}
	summary.CyclesRolledOver += due.CyclesRolledOver
	summary.ContractChangesApplied += due.ContractChangesApplied
	if err := c.ensureCurrentEntitlements(ctx, orgID, productID); err != nil {
		return BusinessClockState{}, err
	}
	if _, err := c.PostPendingGrantDeposits(ctx, orgID, productID); err != nil {
		return BusinessClockState{}, err
	}
	summary.EntitlementsEnsured++
	state, err := c.GetBusinessClock(ctx, orgID, productID)
	if err != nil {
		return BusinessClockState{}, err
	}
	state.DueWork = summary
	return state, nil
}

func orgProductClockScope(orgID OrgID, productID string) (string, string) {
	return "org_product", orgIDText(orgID) + ":" + productID
}
