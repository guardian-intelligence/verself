package billing

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

type BillingCycle struct {
	CycleID            string
	OrgID              OrgID
	ProductID          string
	PredecessorCycleID string
	CadenceKind        string
	AnchorAt           time.Time
	CycleSeq           int64
	StartsAt           time.Time
	EndsAt             time.Time
	FinalizationDueAt  time.Time
}

func (c *Client) EnsureOpenBillingCycle(ctx context.Context, orgID OrgID, productID string) (BillingCycle, error) {
	if productID == "" {
		return BillingCycle{}, fmt.Errorf("product_id is required")
	}
	now := c.clock().UTC()
	anchorAt, err := c.billingCycleAnchorAt(ctx, orgID, productID, now)
	if err != nil {
		return BillingCycle{}, err
	}
	return c.ensureOpenBillingCycleForAnchor(ctx, orgID, productID, anchorAt, now)
}

func (c *Client) EnsureContractBillingCycle(ctx context.Context, orgID OrgID, productID string, anchorAt time.Time) (BillingCycle, error) {
	if productID == "" {
		return BillingCycle{}, fmt.Errorf("product_id is required")
	}
	anchorAt = anchorAt.UTC()
	return c.ensureOpenBillingCycleForAnchor(ctx, orgID, productID, anchorAt, anchorAt)
}

func (c *Client) ensureOpenBillingCycleForAnchor(ctx context.Context, orgID OrgID, productID string, anchorAt time.Time, now time.Time) (BillingCycle, error) {
	anchorAt = anchorAt.UTC()
	now = now.UTC()
	seq, startsAt, endsAt := anniversaryMonthlyCycle(anchorAt, now)
	cycleID := deterministicTextID(
		"billing-cycle",
		strconv.FormatUint(uint64(orgID), 10),
		productID,
		anchorAt.Format(time.RFC3339Nano),
		strconv.FormatInt(seq, 10),
	)
	out := BillingCycle{
		CycleID:           cycleID,
		OrgID:             orgID,
		ProductID:         productID,
		CadenceKind:       "anniversary_monthly",
		AnchorAt:          anchorAt,
		CycleSeq:          seq,
		StartsAt:          startsAt,
		EndsAt:            endsAt,
		FinalizationDueAt: endsAt,
	}

	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return BillingCycle{}, fmt.Errorf("begin ensure cycle: %w", err)
	}
	defer tx.Rollback()

	existing, found, err := loadOpenBillingCycleTx(ctx, tx, orgID, productID)
	if err != nil {
		return BillingCycle{}, err
	}
	if found && existing.AnchorAt.Equal(anchorAt) && !now.Before(existing.StartsAt) && now.Before(existing.EndsAt) {
		if err := tx.Commit(); err != nil {
			return BillingCycle{}, fmt.Errorf("commit existing cycle: %w", err)
		}
		return existing, nil
	}
	if found {
		if _, err := tx.ExecContext(ctx, `
			UPDATE billing_cycles
			SET status = 'closed_for_usage',
			    closed_for_usage_at = COALESCE(closed_for_usage_at, $2),
			    updated_at = now()
			WHERE cycle_id = $1 AND status = 'open'
		`, existing.CycleID, minTime(now, existing.EndsAt)); err != nil {
			return BillingCycle{}, fmt.Errorf("close stale billing cycle %s: %w", existing.CycleID, err)
		}
		out.PredecessorCycleID = existing.CycleID
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO billing_cycles (
			cycle_id, org_id, product_id, predecessor_cycle_id, cadence_kind, anchor_at, cycle_seq,
			starts_at, ends_at, status, finalization_due_at
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9, 'open', $10)
		ON CONFLICT (cycle_id) DO NOTHING
	`, out.CycleID, strconv.FormatUint(uint64(orgID), 10), productID, out.PredecessorCycleID, out.CadenceKind, out.AnchorAt, out.CycleSeq, out.StartsAt, out.EndsAt, out.FinalizationDueAt); err != nil {
		return BillingCycle{}, fmt.Errorf("insert billing cycle %s: %w", out.CycleID, err)
	}
	if err := tx.Commit(); err != nil {
		return BillingCycle{}, fmt.Errorf("commit ensure cycle: %w", err)
	}
	return out, nil
}

func (c *Client) billingCycleAnchorAt(ctx context.Context, orgID OrgID, productID string, now time.Time) (time.Time, error) {
	var anchor sql.NullTime
	err := c.pg.QueryRowContext(ctx, `
		SELECT c.billing_anchor_at
		FROM contracts c
		WHERE c.org_id = $1
		  AND c.product_id = $2
		  AND c.status IN ('active', 'cancel_scheduled', 'past_due', 'suspended')
		  AND c.starts_at <= $3
		  AND (c.ends_at IS NULL OR c.ends_at > $3)
		ORDER BY c.starts_at DESC, c.contract_id DESC
		LIMIT 1
	`, strconv.FormatUint(uint64(orgID), 10), productID, now.UTC()).Scan(&anchor)
	if err == nil && anchor.Valid {
		return anchor.Time.UTC(), nil
	}
	if err != nil && err != sql.ErrNoRows {
		return time.Time{}, fmt.Errorf("lookup billing cycle contract anchor: %w", err)
	}
	return c.orgCreatedAt(ctx, orgID)
}

func loadOpenBillingCycleTx(ctx context.Context, tx *sql.Tx, orgID OrgID, productID string) (BillingCycle, bool, error) {
	var out BillingCycle
	var orgIDText string
	var predecessor sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT cycle_id, org_id, product_id, predecessor_cycle_id, cadence_kind, anchor_at, cycle_seq,
		       starts_at, ends_at, finalization_due_at
		FROM billing_cycles
		WHERE org_id = $1
		  AND product_id = $2
		  AND status = 'open'
		FOR UPDATE
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(
		&out.CycleID,
		&orgIDText,
		&out.ProductID,
		&predecessor,
		&out.CadenceKind,
		&out.AnchorAt,
		&out.CycleSeq,
		&out.StartsAt,
		&out.EndsAt,
		&out.FinalizationDueAt,
	)
	if err == sql.ErrNoRows {
		return BillingCycle{}, false, nil
	}
	if err != nil {
		return BillingCycle{}, false, fmt.Errorf("load open billing cycle: %w", err)
	}
	parsed, err := strconv.ParseUint(orgIDText, 10, 64)
	if err != nil {
		return BillingCycle{}, false, fmt.Errorf("parse cycle org_id %q: %w", orgIDText, err)
	}
	out.OrgID = OrgID(parsed)
	if predecessor.Valid {
		out.PredecessorCycleID = predecessor.String
	}
	out.AnchorAt = out.AnchorAt.UTC()
	out.StartsAt = out.StartsAt.UTC()
	out.EndsAt = out.EndsAt.UTC()
	out.FinalizationDueAt = out.FinalizationDueAt.UTC()
	return out, true, nil
}

func anniversaryMonthlyCycle(anchor time.Time, now time.Time) (int64, time.Time, time.Time) {
	anchor = anchor.UTC()
	now = now.UTC()
	if now.Before(anchor) {
		return 0, anchor, addMonthsClampedUTC(anchor, 1)
	}
	months := int64((now.Year()-anchor.Year())*12 + int(now.Month()-anchor.Month()))
	if months < 0 {
		months = 0
	}
	start := addMonthsClampedUTC(anchor, int(months))
	for start.After(now) {
		months--
		start = addMonthsClampedUTC(anchor, int(months))
	}
	end := addMonthsClampedUTC(anchor, int(months)+1)
	for !now.Before(end) {
		months++
		start = end
		end = addMonthsClampedUTC(anchor, int(months)+1)
	}
	return months, start, end
}

func addMonthsClampedUTC(value time.Time, months int) time.Time {
	value = value.UTC()
	year, month, day := value.Date()
	hour, minute, second := value.Clock()
	targetFirst := time.Date(year, month+time.Month(months), 1, hour, minute, second, value.Nanosecond(), time.UTC)
	lastDay := daysInMonth(targetFirst.Year(), targetFirst.Month())
	if day > lastDay {
		day = lastDay
	}
	return time.Date(targetFirst.Year(), targetFirst.Month(), day, hour, minute, second, value.Nanosecond(), time.UTC)
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func minTime(a time.Time, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
