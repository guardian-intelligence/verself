package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/forge-metal/billing-service/internal/store"
)

type billingCycle struct {
	CycleID  string
	StartsAt time.Time
	EndsAt   time.Time
}

func (c *Client) EnsureOpenBillingCycle(ctx context.Context, orgID OrgID, productID string) (billingCycle, error) {
	var out billingCycle
	err := c.WithTx(ctx, "billing.cycle.ensure_open", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		now, err := c.BusinessNow(ctx, c.queries.WithTx(tx), orgID, productID)
		if err != nil {
			return err
		}
		start := monthStartUTC(now)
		end := nextMonth(now)
		id := cycleID(orgID, productID, start)
		var insertedID string
		err = tx.QueryRow(ctx, `
			INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at)
			VALUES ($1, $2, $3, 'usd', $4, 0, 'calendar_monthly', $4, $5, 'open', $5)
			ON CONFLICT (org_id, product_id, anchor_at, cycle_seq) DO NOTHING
			RETURNING cycle_id
		`, id, orgIDText(orgID), productID, start, end).Scan(&insertedID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("ensure open billing cycle: %w", err)
		}
		out = billingCycle{CycleID: id, StartsAt: start, EndsAt: end}
		if insertedID == "" {
			return nil
		}
		return appendEvent(ctx, tx, c.queries.WithTx(tx), eventFact{
			EventType:     "billing_cycle_opened",
			AggregateType: "billing_cycle",
			AggregateID:   id,
			OrgID:         orgID,
			ProductID:     productID,
			OccurredAt:    now,
			Payload: map[string]any{
				"cycle_id":  id,
				"starts_at": start.Format(time.RFC3339Nano),
				"ends_at":   end.Format(time.RFC3339Nano),
			},
		})
	})
	return out, err
}
