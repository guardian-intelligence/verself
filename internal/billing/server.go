package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// SubscriptionRow is a single subscription for API serialization.
type SubscriptionRow struct {
	SubscriptionID       int64      `json:"subscription_id"`
	PlanID               string     `json:"plan_id"`
	ProductID            string     `json:"product_id"`
	Cadence              string     `json:"cadence"`
	Status               string     `json:"status"`
	StripeSubscriptionID *string    `json:"stripe_subscription_id,omitempty"`
	CurrentPeriodStart   *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd     *time.Time `json:"current_period_end,omitempty"`
	OverageCapUnits      *int64     `json:"overage_cap_units,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

// GrantRow is a single credit grant for API serialization.
type GrantRow struct {
	GrantID   string     `json:"grant_id"`
	ProductID string     `json:"product_id"`
	Amount    int64      `json:"amount"`
	Source    string     `json:"source"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// UsageEvent is a single billing event for API serialization.
type UsageEvent struct {
	EventID   int64           `json:"event_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// ListSubscriptions returns all subscriptions for an org.
func (c *Client) ListSubscriptions(ctx context.Context, orgID OrgID) ([]SubscriptionRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	rows, err := c.pg.QueryContext(ctx, `
		SELECT subscription_id, plan_id, product_id, cadence, status,
		       stripe_subscription_id, current_period_start, current_period_end,
		       overage_cap_units, created_at
		FROM subscriptions
		WHERE org_id = $1
		ORDER BY created_at DESC
	`, orgIDStr)
	if err != nil {
		return nil, fmt.Errorf("query subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []SubscriptionRow
	for rows.Next() {
		var sub SubscriptionRow
		if err := rows.Scan(
			&sub.SubscriptionID, &sub.PlanID, &sub.ProductID, &sub.Cadence, &sub.Status,
			&sub.StripeSubscriptionID, &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd,
			&sub.OverageCapUnits, &sub.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}
	if subs == nil {
		subs = []SubscriptionRow{}
	}
	return subs, nil
}

// ListGrants returns credit grants for an org, with optional product and active filters.
func (c *Client) ListGrants(ctx context.Context, orgID OrgID, productID string, activeOnly bool) ([]GrantRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	query := `
		SELECT grant_id, product_id, amount, source, expires_at, closed_at, created_at
		FROM credit_grants
		WHERE org_id = $1
	`
	args := []any{orgIDStr}
	argIdx := 2

	if productID != "" {
		query += fmt.Sprintf(" AND product_id = $%d", argIdx)
		args = append(args, productID)
		argIdx++
	}
	if activeOnly {
		query += " AND closed_at IS NULL"
	}
	query += " ORDER BY created_at DESC"

	rows, err := c.pg.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query grants: %w", err)
	}
	defer rows.Close()

	var grants []GrantRow
	for rows.Next() {
		var g GrantRow
		if err := rows.Scan(&g.GrantID, &g.ProductID, &g.Amount, &g.Source, &g.ExpiresAt, &g.ClosedAt, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grants: %w", err)
	}
	if grants == nil {
		grants = []GrantRow{}
	}
	return grants, nil
}

// ListUsageEvents returns billing events for an org, with optional filters.
func (c *Client) ListUsageEvents(ctx context.Context, orgID OrgID, productID string, since *time.Time, limit int) ([]UsageEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	query := `
		SELECT event_id, event_type, payload, created_at
		FROM billing_events
		WHERE org_id = $1
	`
	args := []any{orgIDStr}
	argIdx := 2

	if productID != "" {
		query += fmt.Sprintf(" AND payload->>'product_id' = $%d", argIdx)
		args = append(args, productID)
		argIdx++
	}
	if since != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *since)
		argIdx++
	}
	query += " ORDER BY created_at DESC"

	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := c.pg.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query usage events: %w", err)
	}
	defer rows.Close()

	var events []UsageEvent
	for rows.Next() {
		var e UsageEvent
		if err := rows.Scan(&e.EventID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	if events == nil {
		events = []UsageEvent{}
	}
	return events, nil
}
