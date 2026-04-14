package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/forge-metal/billing-service/internal/store"
)

type billingCycle struct {
	CycleID            string
	Currency           string
	PredecessorCycleID string
	AnchorAt           time.Time
	CycleSeq           int64
	CadenceKind        string
	StartsAt           time.Time
	EndsAt             time.Time
}

func (c *Client) EnsureOpenBillingCycle(ctx context.Context, orgID OrgID, productID string) (billingCycle, error) {
	if _, err := c.ApplyDueBillingWork(ctx, orgID, productID); err != nil {
		return billingCycle{}, err
	}
	var out billingCycle
	err := c.WithTx(ctx, "billing.cycle.ensure_open", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		now, err := c.BusinessNow(ctx, q, orgID, productID)
		if err != nil {
			return err
		}
		cycle, err := c.ensureOpenBillingCycleTx(ctx, tx, orgID, productID, now)
		if err != nil {
			return err
		}
		out = cycle
		return nil
	})
	return out, err
}

func (c *Client) ApplyDueBillingWork(ctx context.Context, orgID OrgID, productID string) (DueWorkSummary, error) {
	summary := DueWorkSummary{}
	for {
		rolled := false
		var appliedChanges uint64
		var finalizationID string
		err := c.WithTx(ctx, "billing.due_work.apply", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
			now, err := c.BusinessNow(ctx, q, orgID, productID)
			if err != nil {
				return err
			}
			cycle, ok, err := c.nextDueOpenCycleTx(ctx, tx, orgID, productID, now)
			if err != nil || !ok {
				return err
			}
			if err := c.closeCycleForUsageTx(ctx, tx, q, orgID, productID, cycle, now); err != nil {
				return err
			}
			appliedChanges, err = c.applyDueContractChangesTx(ctx, tx, q, orgID, productID, cycle, now)
			if err != nil {
				return err
			}
			finalizationID, err = c.ensureCycleFinalizationTx(ctx, tx, q, orgID, productID, cycle, now)
			if err != nil {
				return err
			}
			if err := c.openSuccessorCycleTx(ctx, tx, q, orgID, productID, cycle, now); err != nil {
				return err
			}
			rolled = true
			return nil
		})
		if err != nil {
			return summary, err
		}
		if !rolled {
			return summary, nil
		}
		if finalizationID != "" && c.runtime == nil {
			if _, err := c.FinalizeBillingFinalization(ctx, finalizationID); err != nil {
				return summary, err
			}
		}
		summary.CyclesRolledOver++
		summary.ContractChangesApplied += appliedChanges
	}
}

func (c *Client) ApplyPendingDueBillingWork(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.pg.Query(ctx, `
		SELECT DISTINCT org_id, product_id
		FROM billing_cycles
		WHERE status IN ('open', 'closing')
		  AND ends_at <= transaction_timestamp()
		ORDER BY org_id, product_id
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("query pending due billing work: %w", err)
	}
	defer rows.Close()
	type target struct {
		orgIDText string
		productID string
	}
	targets := []target{}
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.orgIDText, &t.productID); err != nil {
			return 0, fmt.Errorf("scan pending due billing work: %w", err)
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("scan pending due billing work: %w", err)
	}
	applied := 0
	for _, target := range targets {
		parsed, err := strconv.ParseUint(target.orgIDText, 10, 64)
		if err != nil {
			return applied, fmt.Errorf("parse pending due org_id %q: %w", target.orgIDText, err)
		}
		if _, err := c.ApplyDueBillingWork(ctx, OrgID(parsed), target.productID); err != nil {
			return applied, err
		}
		if err := c.ensureCurrentEntitlements(ctx, OrgID(parsed), target.productID); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func (c *Client) ensureOpenBillingCycleTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, now time.Time) (billingCycle, error) {
	if cycle, ok, err := c.openBillingCycleContainingTx(ctx, tx, orgID, productID, now); err != nil || ok {
		return cycle, err
	}
	if cycle, ok, err := c.anyOpenBillingCycleTx(ctx, tx, orgID, productID); err != nil {
		return billingCycle{}, err
	} else if ok {
		return billingCycle{}, fmt.Errorf("open billing cycle %s covers %s..%s, not business time %s; run due work or reset state", cycle.CycleID, cycle.StartsAt.Format(time.RFC3339Nano), cycle.EndsAt.Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	}
	start := monthStartUTC(now)
	end := nextMonth(now)
	id := cycleID(orgID, productID, start)
	cycle := billingCycle{CycleID: id, Currency: "usd", AnchorAt: start, CycleSeq: 0, CadenceKind: "calendar_monthly", StartsAt: start, EndsAt: end}
	tag, err := tx.Exec(ctx, `
		INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'open', $9)
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
		    metadata = billing_cycles.metadata - 'voided_by'
		WHERE billing_cycles.status = 'voided'
	`, cycle.CycleID, orgIDText(orgID), productID, cycle.Currency, cycle.AnchorAt, cycle.CycleSeq, cycle.CadenceKind, cycle.StartsAt, cycle.EndsAt)
	if err != nil {
		return billingCycle{}, fmt.Errorf("ensure open billing cycle: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if existing, ok, err := c.openBillingCycleContainingTx(ctx, tx, orgID, productID, now); err != nil || ok {
			return existing, err
		}
		return billingCycle{}, fmt.Errorf("ensure open billing cycle %s: conflicting non-open cycle", cycle.CycleID)
	}
	return cycle, appendEvent(ctx, tx, c.queries.WithTx(tx), eventFact{
		EventType:     "billing_cycle_opened",
		AggregateType: "billing_cycle",
		AggregateID:   cycle.CycleID,
		OrgID:         orgID,
		ProductID:     productID,
		OccurredAt:    now,
		Payload: map[string]any{
			"cycle_id":     cycle.CycleID,
			"cadence_kind": cycle.CadenceKind,
			"starts_at":    cycle.StartsAt.Format(time.RFC3339Nano),
			"ends_at":      cycle.EndsAt.Format(time.RFC3339Nano),
		},
	})
}

func (c *Client) nextDueOpenCycleTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, now time.Time) (billingCycle, bool, error) {
	cycle, err := c.scanBillingCycle(tx.QueryRow(ctx, `
		SELECT cycle_id, currency, COALESCE(predecessor_cycle_id, ''), anchor_at, cycle_seq, cadence_kind, starts_at, ends_at
		FROM billing_cycles
		WHERE org_id = $1
		  AND product_id = $2
		  AND status IN ('open', 'closing')
		  AND ends_at <= $3
		ORDER BY ends_at, cycle_id
		LIMIT 1
		FOR UPDATE
	`, orgIDText(orgID), productID, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return billingCycle{}, false, nil
	}
	if err != nil {
		return billingCycle{}, false, fmt.Errorf("load due open billing cycle: %w", err)
	}
	return cycle, true, nil
}

func (c *Client) openBillingCycleContainingTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, now time.Time) (billingCycle, bool, error) {
	cycle, err := c.scanBillingCycle(tx.QueryRow(ctx, `
		SELECT cycle_id, currency, COALESCE(predecessor_cycle_id, ''), anchor_at, cycle_seq, cadence_kind, starts_at, ends_at
		FROM billing_cycles
		WHERE org_id = $1
		  AND product_id = $2
		  AND status IN ('open', 'closing')
		  AND starts_at <= $3
		  AND ends_at > $3
		ORDER BY starts_at DESC, cycle_id DESC
		LIMIT 1
	`, orgIDText(orgID), productID, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return billingCycle{}, false, nil
	}
	if err != nil {
		return billingCycle{}, false, fmt.Errorf("load current open billing cycle: %w", err)
	}
	return cycle, true, nil
}

func (c *Client) anyOpenBillingCycleTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string) (billingCycle, bool, error) {
	cycle, err := c.scanBillingCycle(tx.QueryRow(ctx, `
		SELECT cycle_id, currency, COALESCE(predecessor_cycle_id, ''), anchor_at, cycle_seq, cadence_kind, starts_at, ends_at
		FROM billing_cycles
		WHERE org_id = $1
		  AND product_id = $2
		  AND status IN ('open', 'closing')
		ORDER BY starts_at DESC, cycle_id DESC
		LIMIT 1
	`, orgIDText(orgID), productID))
	if errors.Is(err, pgx.ErrNoRows) {
		return billingCycle{}, false, nil
	}
	if err != nil {
		return billingCycle{}, false, fmt.Errorf("load open billing cycle: %w", err)
	}
	return cycle, true, nil
}

func (c *Client) scanBillingCycle(row pgx.Row) (billingCycle, error) {
	var cycle billingCycle
	if err := row.Scan(&cycle.CycleID, &cycle.Currency, &cycle.PredecessorCycleID, &cycle.AnchorAt, &cycle.CycleSeq, &cycle.CadenceKind, &cycle.StartsAt, &cycle.EndsAt); err != nil {
		return billingCycle{}, err
	}
	cycle.AnchorAt = cycle.AnchorAt.UTC()
	cycle.StartsAt = cycle.StartsAt.UTC()
	cycle.EndsAt = cycle.EndsAt.UTC()
	return cycle, nil
}

func (c *Client) closeCycleForUsageTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, cycle billingCycle, now time.Time) error {
	tag, err := tx.Exec(ctx, `
		UPDATE billing_cycles
		SET status = 'closed_for_usage', closed_for_usage_at = $2
		WHERE cycle_id = $1
		  AND status IN ('open', 'closing')
	`, cycle.CycleID, now)
	if err != nil {
		return fmt.Errorf("close billing cycle for usage: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return appendEvent(ctx, tx, q, eventFact{
		EventType:     "billing_cycle_closed_for_usage",
		AggregateType: "billing_cycle",
		AggregateID:   cycle.CycleID,
		OrgID:         orgID,
		ProductID:     productID,
		OccurredAt:    cycle.EndsAt,
		Payload: map[string]any{
			"cycle_id":  cycle.CycleID,
			"starts_at": cycle.StartsAt.Format(time.RFC3339Nano),
			"ends_at":   cycle.EndsAt.Format(time.RFC3339Nano),
			"closed_at": now.UTC().Format(time.RFC3339Nano),
			"status":    "closed_for_usage",
			"cycle_seq": cycle.CycleSeq,
			"anchor_at": cycle.AnchorAt.Format(time.RFC3339Nano),
			"currency":  cycle.Currency,
			"cadence":   cycle.CadenceKind,
		},
	})
}

func (c *Client) openSuccessorCycleTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, predecessor billingCycle, now time.Time) error {
	start := predecessor.EndsAt
	end := cycleEndAfter(predecessor.CadenceKind, start)
	anchor := predecessor.AnchorAt
	if anchor.IsZero() {
		anchor = predecessor.StartsAt
	}
	successor := billingCycle{CycleID: cycleID(orgID, productID, start), Currency: cleanNonEmpty(predecessor.Currency, "usd"), PredecessorCycleID: predecessor.CycleID, AnchorAt: anchor, CycleSeq: predecessor.CycleSeq + 1, CadenceKind: cleanNonEmpty(predecessor.CadenceKind, "calendar_monthly"), StartsAt: start, EndsAt: end}
	tag, err := tx.Exec(ctx, `
		INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, predecessor_cycle_id, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'open', $10)
		ON CONFLICT (cycle_id) DO UPDATE
		SET currency = EXCLUDED.currency,
		    predecessor_cycle_id = EXCLUDED.predecessor_cycle_id,
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
		    metadata = billing_cycles.metadata - 'voided_by'
		WHERE billing_cycles.status = 'voided'
	`, successor.CycleID, orgIDText(orgID), productID, successor.Currency, successor.PredecessorCycleID, successor.AnchorAt, successor.CycleSeq, successor.CadenceKind, successor.StartsAt, successor.EndsAt)
	if err != nil {
		return fmt.Errorf("open successor billing cycle: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE billing_cycles SET successor_cycle_id = $2 WHERE cycle_id = $1 AND successor_cycle_id IS NULL`, predecessor.CycleID, successor.CycleID); err != nil {
		return fmt.Errorf("link successor billing cycle: %w", err)
	}
	return appendEvent(ctx, tx, q, eventFact{
		EventType:     "billing_cycle_opened",
		AggregateType: "billing_cycle",
		AggregateID:   successor.CycleID,
		OrgID:         orgID,
		ProductID:     productID,
		OccurredAt:    now,
		Payload: map[string]any{
			"cycle_id":             successor.CycleID,
			"predecessor_cycle_id": successor.PredecessorCycleID,
			"starts_at":            successor.StartsAt.Format(time.RFC3339Nano),
			"ends_at":              successor.EndsAt.Format(time.RFC3339Nano),
			"anchor_at":            successor.AnchorAt.Format(time.RFC3339Nano),
			"cycle_seq":            successor.CycleSeq,
			"cadence_kind":         successor.CadenceKind,
		},
	})
}

func (c *Client) splitCycleForContractActivationTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, effectiveAt time.Time) (billingCycle, string, error) {
	if cycle, ok, err := c.openBillingCycleContainingTx(ctx, tx, orgID, productID, effectiveAt); err != nil {
		return billingCycle{}, "", err
	} else if ok && !cycle.StartsAt.Before(effectiveAt) {
		return cycle, "", nil
	} else if ok {
		originalEnd := cycle.EndsAt
		_, err := tx.Exec(ctx, `
			UPDATE billing_cycles
			SET status = 'closed_for_usage',
			    ends_at = $2,
			    finalization_due_at = $2,
			    closed_for_usage_at = $2,
			    closed_reason = 'free_to_paid_activation'
			WHERE cycle_id = $1
			  AND status IN ('open', 'closing')
		`, cycle.CycleID, effectiveAt)
		if err != nil {
			return billingCycle{}, "", fmt.Errorf("close free cycle at activation: %w", err)
		}
		cycle.EndsAt = effectiveAt
		finalizationID, err := c.ensureCycleFinalizationTx(ctx, tx, q, orgID, productID, cycle, effectiveAt)
		if err != nil {
			return billingCycle{}, "", err
		}
		successor, err := c.insertActivationCycleTx(ctx, tx, q, orgID, productID, cycle.CycleID, effectiveAt, effectiveAt, originalEnd)
		if err != nil {
			return billingCycle{}, "", err
		}
		return successor, finalizationID, nil
	}
	cycle, err := c.insertActivationCycleTx(ctx, tx, q, orgID, productID, "", effectiveAt, effectiveAt, time.Time{})
	return cycle, "", err
}

func (c *Client) insertActivationCycleTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, predecessorCycleID string, anchor time.Time, startsAt time.Time, _ time.Time) (billingCycle, error) {
	cadence := "anniversary_monthly"
	endsAt := cycleEndAfter(cadence, startsAt)
	cycle := billingCycle{CycleID: cycleID(orgID, productID, startsAt), Currency: "usd", PredecessorCycleID: predecessorCycleID, AnchorAt: anchor, CycleSeq: 0, CadenceKind: cadence, StartsAt: startsAt, EndsAt: endsAt}
	tag, err := tx.Exec(ctx, `
		INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, predecessor_cycle_id, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,0,$7,$8,$9,'open',$9)
		ON CONFLICT (cycle_id) DO UPDATE
		SET currency = EXCLUDED.currency,
		    predecessor_cycle_id = EXCLUDED.predecessor_cycle_id,
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
		    metadata = billing_cycles.metadata - 'voided_by'
		WHERE billing_cycles.status = 'voided'
	`, cycle.CycleID, orgIDText(orgID), productID, cycle.Currency, predecessorCycleID, cycle.AnchorAt, cycle.CadenceKind, cycle.StartsAt, cycle.EndsAt)
	if err != nil {
		return billingCycle{}, fmt.Errorf("insert activation billing cycle: %w", err)
	}
	if tag.RowsAffected() == 0 {
		existing, ok, err := c.openBillingCycleContainingTx(ctx, tx, orgID, productID, startsAt)
		if err != nil {
			return billingCycle{}, err
		}
		if ok && existing.CycleID == cycle.CycleID {
			return existing, nil
		}
		return billingCycle{}, fmt.Errorf("insert activation billing cycle %s: conflicting non-open cycle", cycle.CycleID)
	}
	if predecessorCycleID != "" {
		if _, err := tx.Exec(ctx, `UPDATE billing_cycles SET successor_cycle_id = $2 WHERE cycle_id = $1 AND successor_cycle_id IS NULL`, predecessorCycleID, cycle.CycleID); err != nil {
			return billingCycle{}, fmt.Errorf("link activation successor cycle: %w", err)
		}
	}
	if tag.RowsAffected() > 0 {
		if err := appendEvent(ctx, tx, q, eventFact{
			EventType:     "billing_cycle_opened",
			AggregateType: "billing_cycle",
			AggregateID:   cycle.CycleID,
			OrgID:         orgID,
			ProductID:     productID,
			OccurredAt:    startsAt,
			Payload: map[string]any{
				"cycle_id":             cycle.CycleID,
				"predecessor_cycle_id": predecessorCycleID,
				"starts_at":            cycle.StartsAt.Format(time.RFC3339Nano),
				"ends_at":              cycle.EndsAt.Format(time.RFC3339Nano),
				"anchor_at":            cycle.AnchorAt.Format(time.RFC3339Nano),
				"cycle_seq":            cycle.CycleSeq,
				"cadence_kind":         cycle.CadenceKind,
				"reason":               "free_to_paid_activation",
			},
		}); err != nil {
			return billingCycle{}, err
		}
	}
	return cycle, nil
}

func cycleEndAfter(cadenceKind string, start time.Time) time.Time {
	switch cadenceKind {
	case "annual":
		return start.UTC().AddDate(1, 0, 0)
	case "anniversary_monthly":
		return start.UTC().AddDate(0, 1, 0)
	default:
		return nextMonth(start)
	}
}
