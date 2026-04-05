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

type quotaPolicySource string

const (
	quotaPolicySourceNone         quotaPolicySource = ""
	quotaPolicySourceSubscription quotaPolicySource = "subscription"
	quotaPolicySourceDefaultPlan  quotaPolicySource = "default_plan"
)

type loadedQuotaPolicy struct {
	source      quotaPolicySource
	limits      []quotaLimit
	periodStart *time.Time
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

	policy, err := c.loadQuotaPolicy(ctx, orgID, productID)
	if err != nil {
		return QuotaResult{}, err
	}
	if len(policy.limits) == 0 {
		return QuotaResult{Allowed: true}, nil
	}

	now := c.clock().UTC()
	var violations []QuotaViolation

	for _, lim := range policy.limits {
		var current float64

		switch lim.Window {
		case "instant":
			current = usage[lim.Dimension]
		default:
			since, err := windowSince(lim.Window, now, policy.periodStart)
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

// loadQuotaPolicy resolves the active quota policy for an org/product.
// It distinguishes "no applicable plan" from "plan exists but has no limits",
// and only populates periodStart for subscription-backed policies.
func (c *Client) loadQuotaPolicy(ctx context.Context, orgID OrgID, productID string) (loadedQuotaPolicy, error) {
	orgIDText := strconv.FormatUint(uint64(orgID), 10)

	policy, found, err := c.loadSubscriptionQuotaPolicy(ctx, orgIDText, productID)
	if err != nil {
		return loadedQuotaPolicy{}, err
	}
	if found {
		return policy, nil
	}

	policy, found, err = c.loadDefaultQuotaPolicy(ctx, productID)
	if err != nil {
		return loadedQuotaPolicy{}, err
	}
	if found {
		return policy, nil
	}

	return loadedQuotaPolicy{source: quotaPolicySourceNone}, nil
}

func (c *Client) loadSubscriptionQuotaPolicy(ctx context.Context, orgIDText, productID string) (loadedQuotaPolicy, bool, error) {
	var quotasJSON sql.NullString
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
		return loadedQuotaPolicy{}, false, nil
	}
	if err != nil {
		return loadedQuotaPolicy{}, false, fmt.Errorf("load subscription plan quotas: %w", err)
	}

	policy, err := decodeQuotaPolicy(quotasJSON)
	if err != nil {
		return loadedQuotaPolicy{}, false, fmt.Errorf("parse subscription plan quotas: %w", err)
	}
	policy.source = quotaPolicySourceSubscription

	if periodStart.Valid {
		t := periodStart.Time.UTC()
		policy.periodStart = &t
	}

	return policy, true, nil
}

func (c *Client) loadDefaultQuotaPolicy(ctx context.Context, productID string) (loadedQuotaPolicy, bool, error) {
	var quotasJSON sql.NullString

	err := c.pg.QueryRowContext(ctx, `
		SELECT p.quotas::text
		FROM plans p
		WHERE p.product_id = $1
		  AND p.is_default
		  AND p.active
		LIMIT 1
	`, productID).Scan(&quotasJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return loadedQuotaPolicy{}, false, nil
	}
	if err != nil {
		return loadedQuotaPolicy{}, false, fmt.Errorf("load default plan quotas: %w", err)
	}

	policy, err := decodeQuotaPolicy(quotasJSON)
	if err != nil {
		return loadedQuotaPolicy{}, false, fmt.Errorf("parse default plan quotas: %w", err)
	}
	policy.source = quotaPolicySourceDefaultPlan

	return policy, true, nil
}

func decodeQuotaPolicy(raw sql.NullString) (loadedQuotaPolicy, error) {
	if !raw.Valid || raw.String == "" {
		return loadedQuotaPolicy{}, nil
	}

	var policy quotaPolicy
	if err := json.Unmarshal([]byte(raw.String), &policy); err != nil {
		return loadedQuotaPolicy{}, err
	}

	return loadedQuotaPolicy{limits: policy.Limits}, nil
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
