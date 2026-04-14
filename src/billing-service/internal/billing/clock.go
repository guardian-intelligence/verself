package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/forge-metal/billing-service/internal/store"
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
