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
	row, err := c.queries.GetBusinessClockOverride(ctx, store.GetBusinessClockOverrideParams{ScopeKind: scopeKind, ScopeID: scopeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return BusinessClockState{}, fmt.Errorf("load business clock override: %w", err)
	}
	state.BusinessNow = row.BusinessNow.Time.UTC()
	state.HasOverride = true
	state.Generation = uint64(row.Generation)
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
	if err := c.WithTx(ctx, "billing.clock.set", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		var err error
		generation, err = q.UpsertBusinessClockOverride(ctx, store.UpsertBusinessClockOverrideParams{
			ScopeKind:   scopeKind,
			ScopeID:     scopeID,
			BusinessNow: timestamptz(businessNow),
			Reason:      reason,
		})
		if err != nil {
			return fmt.Errorf("set business clock: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_clock_set", AggregateType: "billing_clock", AggregateID: scopeID, OrgID: orgID, ProductID: productID, OccurredAt: time.Now().UTC(), Payload: map[string]any{"scope_kind": scopeKind, "scope_id": scopeID, "business_now": businessNow.Format(time.RFC3339Nano), "generation": generation, "reason": reason}})
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
	if err := c.WithTx(ctx, "billing.clock.advance", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		base, err := c.BusinessNow(ctx, q, orgID, productID)
		if err != nil {
			return err
		}
		locked, err := q.LockBusinessClockOverride(ctx, store.LockBusinessClockOverrideParams{ScopeKind: scopeKind, ScopeID: scopeID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lock business clock override: %w", err)
		}
		if err == nil {
			base = locked.Time
		}
		businessNow = base.UTC().Add(delta)
		generation, err = q.UpsertBusinessClockOverride(ctx, store.UpsertBusinessClockOverrideParams{
			ScopeKind:   scopeKind,
			ScopeID:     scopeID,
			BusinessNow: timestamptz(businessNow),
			Reason:      reason,
		})
		if err != nil {
			return fmt.Errorf("advance business clock: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_clock_advanced", AggregateType: "billing_clock", AggregateID: scopeID, OrgID: orgID, ProductID: productID, OccurredAt: time.Now().UTC(), Payload: map[string]any{"scope_kind": scopeKind, "scope_id": scopeID, "business_now": businessNow.Format(time.RFC3339Nano), "advance_seconds": int64(delta / time.Second), "generation": generation, "reason": reason}})
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
	if err := c.WithTx(ctx, "billing.clock.clear", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		if err := q.DeleteBusinessClockOverride(ctx, store.DeleteBusinessClockOverrideParams{ScopeKind: scopeKind, ScopeID: scopeID}); err != nil {
			return fmt.Errorf("clear business clock: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "billing_clock_cleared", AggregateType: "billing_clock", AggregateID: scopeID, OrgID: orgID, ProductID: productID, OccurredAt: time.Now().UTC(), Payload: map[string]any{"scope_kind": scopeKind, "scope_id": scopeID, "reason": reason}})
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
		locked, err := q.LockBusinessClockOverride(ctx, store.LockBusinessClockOverrideParams{ScopeKind: scopeKind, ScopeID: scopeID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lock business clock override: %w", err)
		}
		if err == nil {
			previous = locked.Time.UTC()
			repair.PreviousBusinessNow = &previous
		}
		if err := q.DeleteBusinessClockOverride(ctx, store.DeleteBusinessClockOverrideParams{ScopeKind: scopeKind, ScopeID: scopeID}); err != nil {
			return fmt.Errorf("clear business clock override: %w", err)
		}

		paid, err := c.activePaidPhaseForWallClockResetTx(ctx, q, orgID, productID, wallNow)
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
			if err := c.shiftActiveContractToWallClockTx(ctx, q, paid, wallNow); err != nil {
				return err
			}
		}

		voided, err := c.voidCyclesForWallClockResetTx(ctx, q, orgID, productID, startsAt, endsAt, wallNow, reason)
		if err != nil {
			return err
		}
		repair.VoidedCycleIDs = voided
		closedGrantIDs, err := c.closeCurrentEntitlementGrantsForWallClockResetTx(ctx, q, orgID, productID, startsAt, endsAt, wallNow)
		if err != nil {
			return err
		}
		repair.ClosedGrantIDs = closedGrantIDs
		if err := c.voidCurrentEntitlementPeriodsForWallClockResetTx(ctx, q, orgID, productID, startsAt, endsAt, wallNow, reason); err != nil {
			return err
		}
		if err := c.reopenWallClockTargetEntitlementsTx(ctx, q, orgID, productID, startsAt, endsAt, wallNow); err != nil {
			return err
		}
		cycle, err := c.insertWallClockResetCycleTx(ctx, tx, q, orgID, productID, anchorAt, cycleSeq, startsAt, endsAt, cadence, wallNow)
		if err != nil {
			return err
		}
		repair.CurrentCycleID = cycle.CycleID
		reassignedWindowIDs, err := c.reassignWallClockWindowsTx(ctx, q, orgID, productID, startsAt, endsAt, cycle.CycleID, wallNow)
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

func (c *Client) activePaidPhaseForWallClockResetTx(ctx context.Context, q *store.Queries, orgID OrgID, productID string, wallNow time.Time) (wallClockPaidPhase, error) {
	row, err := q.GetActivePaidPhaseForWallClockReset(ctx, store.GetActivePaidPhaseForWallClockResetParams{
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		WallNow:   timestamptz(wallNow),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wallClockPaidPhase{}, nil
		}
		return wallClockPaidPhase{}, fmt.Errorf("load active paid phase for wall-clock reset: %w", err)
	}
	paid := wallClockPaidPhase{
		contractID: row.ContractID,
		phaseID:    row.PhaseID,
		planID:     row.PlanID,
		anchorAt:   row.StartsAt.Time,
	}
	paid.ok = paid.planID != ""
	paid.anchorAt = paid.anchorAt.UTC()
	return paid, nil
}

func (c *Client) shiftActiveContractToWallClockTx(ctx context.Context, q *store.Queries, paid wallClockPaidPhase, wallNow time.Time) error {
	if err := q.ShiftActiveContractToWallClock(ctx, store.ShiftActiveContractToWallClockParams{
		WallNow:    timestamptz(wallNow),
		ContractID: paid.contractID,
	}); err != nil {
		return fmt.Errorf("shift active contract to wall clock: %w", err)
	}
	if err := q.ShiftActiveContractPhaseToWallClock(ctx, store.ShiftActiveContractPhaseToWallClockParams{
		WallNow: timestamptz(wallNow),
		PhaseID: paid.phaseID,
	}); err != nil {
		return fmt.Errorf("shift active contract phase to wall clock: %w", err)
	}
	return nil
}

func (c *Client) voidCyclesForWallClockResetTx(ctx context.Context, q *store.Queries, orgID OrgID, productID string, startsAt, endsAt, wallNow time.Time, reason string) ([]string, error) {
	ids, err := q.VoidCyclesForWallClockReset(ctx, store.VoidCyclesForWallClockResetParams{
		WallNow:   timestamptz(wallNow),
		Reason:    reason,
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		StartsAt:  timestamptz(startsAt),
		EndsAt:    timestamptz(endsAt),
	})
	if err != nil {
		return nil, fmt.Errorf("void cycles for wall-clock reset: %w", err)
	}
	return ids, nil
}

func (c *Client) closeCurrentEntitlementGrantsForWallClockResetTx(ctx context.Context, q *store.Queries, orgID OrgID, productID string, startsAt, endsAt, wallNow time.Time) ([]string, error) {
	ids, err := q.CloseCurrentEntitlementGrantsForWallClockReset(ctx, store.CloseCurrentEntitlementGrantsForWallClockResetParams{
		WallNow:   timestamptz(wallNow),
		OrgID:     orgIDText(orgID),
		ProductID: pgTextValue(productID),
		StartsAt:  timestamptz(startsAt),
		EndsAt:    timestamptz(endsAt),
	})
	if err != nil {
		return nil, fmt.Errorf("close entitlement grants for wall-clock reset: %w", err)
	}
	return ids, nil
}

func (c *Client) voidCurrentEntitlementPeriodsForWallClockResetTx(ctx context.Context, q *store.Queries, orgID OrgID, productID string, startsAt, endsAt, wallNow time.Time, reason string) error {
	if err := q.VoidCurrentEntitlementPeriodsForWallClockReset(ctx, store.VoidCurrentEntitlementPeriodsForWallClockResetParams{
		WallNow:   timestamptz(wallNow),
		Reason:    reason,
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		StartsAt:  timestamptz(startsAt),
		EndsAt:    timestamptz(endsAt),
	}); err != nil {
		return fmt.Errorf("void entitlement periods for wall-clock reset: %w", err)
	}
	return nil
}

func (c *Client) reopenWallClockTargetEntitlementsTx(ctx context.Context, q *store.Queries, orgID OrgID, productID string, startsAt, endsAt, wallNow time.Time) error {
	if err := q.ReopenWallClockTargetEntitlementPeriods(ctx, store.ReopenWallClockTargetEntitlementPeriodsParams{
		WallNow:   timestamptz(wallNow),
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		StartsAt:  timestamptz(startsAt),
		EndsAt:    timestamptz(endsAt),
	}); err != nil {
		return fmt.Errorf("reopen target entitlement periods for wall-clock reset: %w", err)
	}
	if err := q.ReopenWallClockTargetCreditGrants(ctx, store.ReopenWallClockTargetCreditGrantsParams{
		WallNow:   timestamptz(wallNow),
		OrgID:     orgIDText(orgID),
		StartsAt:  timestamptz(startsAt),
		EndsAt:    timestamptz(endsAt),
		ProductID: pgTextValue(productID),
	}); err != nil {
		return fmt.Errorf("reopen target entitlement grants for wall-clock reset: %w", err)
	}
	return nil
}

func (c *Client) insertWallClockResetCycleTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, anchorAt time.Time, cycleSeq int64, startsAt, endsAt time.Time, cadence string, wallNow time.Time) (billingCycle, error) {
	cycle := billingCycle{CycleID: cycleID(orgID, productID, startsAt), Currency: "usd", AnchorAt: anchorAt.UTC(), CycleSeq: cycleSeq, CadenceKind: cadence, StartsAt: startsAt.UTC(), EndsAt: endsAt.UTC()}
	rowsAffected, err := q.UpsertWallClockResetCycle(ctx, store.UpsertWallClockResetCycleParams{
		CycleID:     cycle.CycleID,
		OrgID:       orgIDText(orgID),
		ProductID:   productID,
		AnchorAt:    timestamptz(cycle.AnchorAt),
		CycleSeq:    cycle.CycleSeq,
		CadenceKind: cadence,
		StartsAt:    timestamptz(cycle.StartsAt),
		EndsAt:      timestamptz(cycle.EndsAt),
	})
	if err != nil {
		return billingCycle{}, fmt.Errorf("insert wall-clock reset billing cycle: %w", err)
	}
	if rowsAffected == 0 {
		existing, ok, err := c.openBillingCycleContainingTx(ctx, q, orgID, productID, wallNow)
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

func (c *Client) reassignWallClockWindowsTx(ctx context.Context, q *store.Queries, orgID OrgID, productID string, startsAt, endsAt time.Time, cycleID string, wallNow time.Time) ([]string, error) {
	ids, err := q.ReassignWallClockWindows(ctx, store.ReassignWallClockWindowsParams{
		CycleID:   cycleID,
		WallNow:   timestamptz(wallNow),
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		StartsAt:  timestamptz(startsAt),
		EndsAt:    timestamptz(endsAt),
	})
	if err != nil {
		return nil, fmt.Errorf("reassign wall-clock billing windows: %w", err)
	}
	return ids, nil
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
