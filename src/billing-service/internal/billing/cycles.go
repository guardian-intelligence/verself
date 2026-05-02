package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/verself/billing-service/internal/store"
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
		cycle, err := c.ensureOpenBillingCycleTx(ctx, tx, q, orgID, productID, now)
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
			cycle, ok, err := c.nextDueOpenCycleTx(ctx, q, orgID, productID, now)
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
	rows, err := c.queries.ListPendingDueBillingWorkTargets(ctx, store.ListPendingDueBillingWorkTargetsParams{LimitCount: checkedInt32FromInt(limit, "pending billing work limit")})
	if err != nil {
		return 0, fmt.Errorf("query pending due billing work: %w", err)
	}
	type target struct {
		orgIDText string
		productID string
	}
	targets := []target{}
	for _, row := range rows {
		targets = append(targets, target{orgIDText: row.OrgID, productID: row.ProductID})
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
		if _, err := c.PostPendingGrantDeposits(ctx, OrgID(parsed), target.productID); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func (c *Client) ensureOpenBillingCycleTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, now time.Time) (billingCycle, error) {
	if cycle, ok, err := c.openBillingCycleContainingTx(ctx, q, orgID, productID, now); err != nil || ok {
		return cycle, err
	}
	if cycle, ok, err := c.anyOpenBillingCycleTx(ctx, q, orgID, productID); err != nil {
		return billingCycle{}, err
	} else if ok {
		return billingCycle{}, fmt.Errorf("open billing cycle %s covers %s..%s, not business time %s; run due work or reset state", cycle.CycleID, cycle.StartsAt.Format(time.RFC3339Nano), cycle.EndsAt.Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	}
	start := monthStartUTC(now)
	end := nextMonth(now)
	id := cycleID(orgID, productID, start)
	cycle := billingCycle{CycleID: id, Currency: "usd", AnchorAt: start, CycleSeq: 0, CadenceKind: "calendar_monthly", StartsAt: start, EndsAt: end}
	rowsAffected, err := q.UpsertOpenBillingCycle(ctx, store.UpsertOpenBillingCycleParams{
		CycleID:     cycle.CycleID,
		OrgID:       orgIDText(orgID),
		ProductID:   productID,
		Currency:    cycle.Currency,
		AnchorAt:    timestamptz(cycle.AnchorAt),
		CycleSeq:    cycle.CycleSeq,
		CadenceKind: cycle.CadenceKind,
		StartsAt:    timestamptz(cycle.StartsAt),
		EndsAt:      timestamptz(cycle.EndsAt),
	})
	if err != nil {
		return billingCycle{}, fmt.Errorf("ensure open billing cycle: %w", err)
	}
	if rowsAffected == 0 {
		if existing, ok, err := c.openBillingCycleContainingTx(ctx, q, orgID, productID, now); err != nil || ok {
			return existing, err
		}
		return billingCycle{}, fmt.Errorf("ensure open billing cycle %s: conflicting non-open cycle", cycle.CycleID)
	}
	return cycle, appendEvent(ctx, tx, q, eventFact{
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

func (c *Client) nextDueOpenCycleTx(ctx context.Context, q *store.Queries, orgID OrgID, productID string, now time.Time) (billingCycle, bool, error) {
	row, err := q.GetNextDueOpenCycleForUpdate(ctx, store.GetNextDueOpenCycleForUpdateParams{
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		Now:       timestamptz(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return billingCycle{}, false, nil
	}
	if err != nil {
		return billingCycle{}, false, fmt.Errorf("load due open billing cycle: %w", err)
	}
	return billingCycleFromStore(row.CycleID, row.Currency, row.PredecessorCycleID, row.AnchorAt, row.CycleSeq, row.CadenceKind, row.StartsAt, row.EndsAt), true, nil
}

func (c *Client) openBillingCycleContainingTx(ctx context.Context, q *store.Queries, orgID OrgID, productID string, now time.Time) (billingCycle, bool, error) {
	row, err := q.GetOpenBillingCycleContaining(ctx, store.GetOpenBillingCycleContainingParams{
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		Now:       timestamptz(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return billingCycle{}, false, nil
	}
	if err != nil {
		return billingCycle{}, false, fmt.Errorf("load current open billing cycle: %w", err)
	}
	return billingCycleFromStore(row.CycleID, row.Currency, row.PredecessorCycleID, row.AnchorAt, row.CycleSeq, row.CadenceKind, row.StartsAt, row.EndsAt), true, nil
}

func (c *Client) anyOpenBillingCycleTx(ctx context.Context, q *store.Queries, orgID OrgID, productID string) (billingCycle, bool, error) {
	row, err := q.GetAnyOpenBillingCycle(ctx, store.GetAnyOpenBillingCycleParams{
		OrgID:     orgIDText(orgID),
		ProductID: productID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return billingCycle{}, false, nil
	}
	if err != nil {
		return billingCycle{}, false, fmt.Errorf("load open billing cycle: %w", err)
	}
	return billingCycleFromStore(row.CycleID, row.Currency, row.PredecessorCycleID, row.AnchorAt, row.CycleSeq, row.CadenceKind, row.StartsAt, row.EndsAt), true, nil
}

func billingCycleFromStore(cycleID, currency, predecessorCycleID string, anchorAt pgtype.Timestamptz, cycleSeq int64, cadenceKind string, startsAt, endsAt pgtype.Timestamptz) billingCycle {
	return billingCycle{
		CycleID:            cycleID,
		Currency:           currency,
		PredecessorCycleID: predecessorCycleID,
		AnchorAt:           anchorAt.Time.UTC(),
		CycleSeq:           cycleSeq,
		CadenceKind:        cadenceKind,
		StartsAt:           startsAt.Time.UTC(),
		EndsAt:             endsAt.Time.UTC(),
	}
}

func (c *Client) closeCycleForUsageTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, cycle billingCycle, now time.Time) error {
	rowsAffected, err := q.CloseCycleForUsage(ctx, store.CloseCycleForUsageParams{
		ClosedAt: timestamptz(now),
		CycleID:  cycle.CycleID,
	})
	if err != nil {
		return fmt.Errorf("close billing cycle for usage: %w", err)
	}
	if rowsAffected == 0 {
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
	rowsAffected, err := q.UpsertSuccessorBillingCycle(ctx, store.UpsertSuccessorBillingCycleParams{
		CycleID:            successor.CycleID,
		OrgID:              orgIDText(orgID),
		ProductID:          productID,
		Currency:           successor.Currency,
		PredecessorCycleID: pgTextValue(successor.PredecessorCycleID),
		AnchorAt:           timestamptz(successor.AnchorAt),
		CycleSeq:           successor.CycleSeq,
		CadenceKind:        successor.CadenceKind,
		StartsAt:           timestamptz(successor.StartsAt),
		EndsAt:             timestamptz(successor.EndsAt),
	})
	if err != nil {
		return fmt.Errorf("open successor billing cycle: %w", err)
	}
	if rowsAffected == 0 {
		return nil
	}
	if err := q.LinkSuccessorBillingCycle(ctx, store.LinkSuccessorBillingCycleParams{CycleID: predecessor.CycleID, SuccessorCycleID: pgTextValue(successor.CycleID)}); err != nil {
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
	if cycle, ok, err := c.openBillingCycleContainingTx(ctx, q, orgID, productID, effectiveAt); err != nil {
		return billingCycle{}, "", err
	} else if ok && !cycle.StartsAt.Before(effectiveAt) {
		return cycle, "", nil
	} else if ok {
		originalEnd := cycle.EndsAt
		if err := q.CloseFreeCycleAtActivation(ctx, store.CloseFreeCycleAtActivationParams{CycleID: cycle.CycleID, EffectiveAt: timestamptz(effectiveAt)}); err != nil {
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
	rowsAffected, err := q.UpsertActivationBillingCycle(ctx, store.UpsertActivationBillingCycleParams{
		CycleID:            cycle.CycleID,
		OrgID:              orgIDText(orgID),
		ProductID:          productID,
		Currency:           cycle.Currency,
		PredecessorCycleID: predecessorCycleID,
		AnchorAt:           timestamptz(cycle.AnchorAt),
		CadenceKind:        cycle.CadenceKind,
		StartsAt:           timestamptz(cycle.StartsAt),
		EndsAt:             timestamptz(cycle.EndsAt),
	})
	if err != nil {
		return billingCycle{}, fmt.Errorf("insert activation billing cycle: %w", err)
	}
	if rowsAffected == 0 {
		existing, ok, err := c.openBillingCycleContainingTx(ctx, q, orgID, productID, startsAt)
		if err != nil {
			return billingCycle{}, err
		}
		if ok && existing.CycleID == cycle.CycleID {
			return existing, nil
		}
		return billingCycle{}, fmt.Errorf("insert activation billing cycle %s: conflicting non-open cycle", cycle.CycleID)
	}
	if predecessorCycleID != "" {
		if err := q.LinkSuccessorBillingCycle(ctx, store.LinkSuccessorBillingCycleParams{CycleID: predecessorCycleID, SuccessorCycleID: pgTextValue(cycle.CycleID)}); err != nil {
			return billingCycle{}, fmt.Errorf("link activation successor cycle: %w", err)
		}
	}
	if rowsAffected > 0 {
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

func cycleBoundsContaining(cadenceKind string, anchorAt time.Time, t time.Time) (time.Time, time.Time, int64) {
	anchorAt = anchorAt.UTC()
	t = t.UTC()
	if cadenceKind != "anniversary_monthly" {
		start := monthStartUTC(t)
		return start, nextMonth(start), 0
	}
	if anchorAt.IsZero() || anchorAt.After(t) {
		return t, cycleEndAfter(cadenceKind, t), 0
	}
	start := anchorAt
	seq := int64(0)
	for {
		end := cycleEndAfter(cadenceKind, start)
		if !end.After(start) {
			return start, t, seq
		}
		if (start.Equal(t) || start.Before(t)) && end.After(t) {
			return start, end, seq
		}
		start = end
		seq++
		if seq > 2400 {
			return start, cycleEndAfter(cadenceKind, start), seq
		}
	}
}
