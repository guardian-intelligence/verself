package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// quotaPolicy is the parsed form of plans.quotas JSONB.
type quotaPolicy struct {
	Limits []quotaLimit `json:"limits"`
}

type quotaLimit struct {
	Dimension string `json:"dimension"`
	Window    string `json:"window"`
	Limit     uint64 `json:"limit"`
}

// CheckQuotas verifies that an org's usage is within plan-defined quota limits.
// Rolling windows are evaluated via ClickHouse. Instant windows are compared
// in-process from the caller-supplied usage map. Fails closed if the metering
// querier is not configured (§7.4).
func (c *Client) CheckQuotas(ctx context.Context, orgID OrgID, productID string, usage map[string]float64) (QuotaResult, error) {
	if err := ctx.Err(); err != nil {
		return QuotaResult{}, err
	}
	if c.querier == nil {
		return QuotaResult{}, ErrQuotaCheckUnavailable
	}

	limits, periodStart, err := c.loadQuotaPolicy(ctx, orgID, productID)
	if err != nil {
		return QuotaResult{}, err
	}
	if len(limits) == 0 {
		return QuotaResult{Allowed: true}, nil
	}

	now := c.clock().UTC()
	var violations []QuotaViolation

	for _, lim := range limits {
		var current float64

		switch lim.Window {
		case "instant":
			current = usage[lim.Dimension]
		default:
			since, err := windowSince(lim.Window, now, periodStart)
			if err != nil {
				return QuotaResult{}, fmt.Errorf("quota %s/%s: %w", lim.Dimension, lim.Window, err)
			}
			current, err = c.querier.SumDimension(ctx, orgID, productID, lim.Dimension, since)
			if err != nil {
				return QuotaResult{}, fmt.Errorf("quota %s/%s: %w", lim.Dimension, lim.Window, err)
			}
		}

		if uint64(current) >= lim.Limit {
			violations = append(violations, QuotaViolation{
				Dimension: lim.Dimension,
				Window:    lim.Window,
				Limit:     lim.Limit,
				Current:   uint64(current),
			})
		}
	}

	return QuotaResult{
		Allowed:    len(violations) == 0,
		Violations: violations,
	}, nil
}

// loadQuotaPolicy reads the quota limits from the active subscription's plan
// or the default plan. Returns the parsed limits and the subscription's
// current_period_start (needed for "month" window alignment). If there is no
// subscription, periodStart is nil and "month" windows will error.
func (c *Client) loadQuotaPolicy(ctx context.Context, orgID OrgID, productID string) ([]quotaLimit, *time.Time, error) {
	orgIDText := strconv.FormatUint(uint64(orgID), 10)

	// Try active subscription first.
	var quotasJSON []byte
	var periodStart sql.NullTime

	err := c.pg.QueryRowContext(ctx, `
		SELECT p.quotas::text, s.current_period_start
		FROM subscriptions s
		JOIN plans p ON p.plan_id = s.plan_id
		WHERE s.org_id = $1
		  AND s.product_id = $2
		  AND s.status IN ('active', 'past_due', 'trialing')
		ORDER BY s.subscription_id DESC
		LIMIT 1
	`, orgIDText, productID).Scan(&quotasJSON, &periodStart)

	if errors.Is(err, sql.ErrNoRows) {
		// Fall back to default plan quotas.
		err = c.pg.QueryRowContext(ctx, `
			SELECT p.quotas::text
			FROM plans p
			WHERE p.product_id = $1
			  AND p.is_default
			  AND p.active
			LIMIT 1
		`, productID).Scan(&quotasJSON)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("load default plan quotas: %w", err)
		}
	} else if err != nil {
		return nil, nil, fmt.Errorf("load subscription plan quotas: %w", err)
	}

	if len(quotasJSON) == 0 || string(quotasJSON) == "{}" {
		return nil, nil, nil
	}

	var policy quotaPolicy
	if err := json.Unmarshal(quotasJSON, &policy); err != nil {
		return nil, nil, fmt.Errorf("parse quotas JSON: %w", err)
	}

	var ps *time.Time
	if periodStart.Valid {
		t := periodStart.Time.UTC()
		ps = &t
	}

	return policy.Limits, ps, nil
}

// windowSince computes the lower bound for a rolling window query.
func windowSince(window string, now time.Time, periodStart *time.Time) (time.Time, error) {
	switch window {
	case "month":
		if periodStart == nil {
			return time.Time{}, fmt.Errorf("month window requires a subscription with current_period_start")
		}
		return *periodStart, nil
	case "week":
		return now.Add(-7 * 24 * time.Hour), nil
	case "4h":
		return now.Add(-4 * time.Hour), nil
	case "hour":
		return now.Add(-1 * time.Hour), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported quota window %q", window)
	}
}
