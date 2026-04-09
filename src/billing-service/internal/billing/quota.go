package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const (
	concurrentDimension = "concurrent_vms"
	concurrentWindow    = "instant"
)

type trustTierPolicy struct {
	concurrentLimit *uint64
	spendCapUnits   *uint64
}

type planQuotaPolicy struct {
	ConcurrentVMs *uint64 `json:"concurrent_vms,omitempty"`
}

type spendCapPolicy struct {
	trustTierCapUnits *uint64
	planCapUnits      *uint64
	effectiveCapUnits *uint64
	periodStart       *time.Time
}

var trustTierPolicies = map[string]trustTierPolicy{
	"new": {
		concurrentLimit: uint64Ptr(2),
		spendCapUnits:   uint64Ptr(500),
	},
	"established": {
		concurrentLimit: uint64Ptr(20),
		spendCapUnits:   uint64Ptr(50000),
	},
	"enterprise": {
		concurrentLimit: nil,
		spendCapUnits:   nil,
	},
	"platform": {
		concurrentLimit: nil,
		spendCapUnits:   nil,
	},
}

// CheckQuotas is an advisory preflight read. Real enforcement happens in Reserve.
func (c *Client) CheckQuotas(ctx context.Context, orgID OrgID, productID string, concurrentCount uint64) (QuotaResult, error) {
	if err := ctx.Err(); err != nil {
		return QuotaResult{}, err
	}

	_, trustPolicy, err := c.loadTrustTierPolicy(ctx, orgID)
	if err != nil {
		return QuotaResult{}, err
	}
	activePlan, err := c.loadActiveSubscriptionPlan(ctx, orgID, productID)
	if err != nil {
		return QuotaResult{}, err
	}
	defaultPlan, err := c.loadDefaultPlan(ctx, orgID, productID)
	if err != nil {
		return QuotaResult{}, err
	}

	limit := effectiveConcurrentLimit(trustPolicy.concurrentLimit, activePlan, defaultPlan)
	if limit == nil || concurrentCount <= *limit {
		return QuotaResult{Allowed: true}, nil
	}

	return QuotaResult{
		Allowed: false,
		Violations: []QuotaViolation{{
			Dimension: concurrentDimension,
			Window:    concurrentWindow,
			Limit:     *limit,
			Current:   concurrentCount,
		}},
	}, nil
}

func (c *Client) loadTrustTierPolicy(ctx context.Context, orgID OrgID) (string, trustTierPolicy, error) {
	var trustTier string
	if err := c.pg.QueryRowContext(ctx, `
		SELECT trust_tier
		FROM orgs
		WHERE org_id = $1
	`, strconv.FormatUint(uint64(orgID), 10)).Scan(&trustTier); err != nil {
		return "", trustTierPolicy{}, fmt.Errorf("load trust tier: %w", err)
	}

	policy, ok := trustTierPolicies[trustTier]
	if !ok {
		return "", trustTierPolicy{}, fmt.Errorf("load trust tier: unsupported trust tier %q", trustTier)
	}
	return trustTier, policy, nil
}

func effectiveConcurrentLimit(trustTierLimit *uint64, activePlan *subscriptionPlan, defaultPlan *plan) *uint64 {
	return minOptionalUint64(trustTierLimit, planConcurrentLimit(activePlan, defaultPlan))
}

func planConcurrentLimit(activePlan *subscriptionPlan, defaultPlan *plan) *uint64 {
	switch {
	case activePlan != nil:
		return activePlan.concurrentLimit
	case defaultPlan != nil:
		return defaultPlan.concurrentLimit
	default:
		return nil
	}
}

func effectiveSpendCapPolicy(trustTierLimit *uint64, activePlan *subscriptionPlan) spendCapPolicy {
	if activePlan == nil {
		return spendCapPolicy{
			trustTierCapUnits: copyUint64Ptr(trustTierLimit),
		}
	}

	return spendCapPolicy{
		trustTierCapUnits: copyUint64Ptr(trustTierLimit),
		planCapUnits:      copyUint64Ptr(activePlan.spendCapUnits),
		effectiveCapUnits: minOptionalUint64(trustTierLimit, activePlan.spendCapUnits),
		periodStart:       copyTimePtr(activePlan.periodStart),
	}
}

func concurrentLimitSource(effective, trustTierLimit, planLimit *uint64) string {
	return limitSource(effective, trustTierLimit, planLimit)
}

func spendCapSource(effective, trustTierLimit, planLimit *uint64) string {
	return limitSource(effective, trustTierLimit, planLimit)
}

func limitSource(effective, trustTierLimit, planLimit *uint64) string {
	switch {
	case effective == nil:
		return ""
	case trustTierLimit != nil && (planLimit == nil || *trustTierLimit <= *planLimit):
		return "trust_tier"
	case planLimit != nil:
		return "plan"
	default:
		return "unknown"
	}
}

func decodePlanQuotaPolicy(raw sql.NullString) (*uint64, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw.String), &object); err != nil {
		return nil, err
	}
	if _, legacy := object["limits"]; legacy {
		return nil, fmt.Errorf("legacy quota policy schema no longer supported")
	}

	var policy planQuotaPolicy
	if err := json.Unmarshal([]byte(raw.String), &policy); err != nil {
		return nil, err
	}
	return copyUint64Ptr(policy.ConcurrentVMs), nil
}

func minOptionalUint64(left, right *uint64) *uint64 {
	switch {
	case left == nil && right == nil:
		return nil
	case left == nil:
		return copyUint64Ptr(right)
	case right == nil:
		return copyUint64Ptr(left)
	case *left <= *right:
		return copyUint64Ptr(left)
	default:
		return copyUint64Ptr(right)
	}
}

func copyUint64Ptr(v *uint64) *uint64 {
	if v == nil {
		return nil
	}
	return uint64Ptr(*v)
}

func copyTimePtr(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	copied := v.UTC()
	return &copied
}

func uint64Ptr(v uint64) *uint64 {
	return &v
}
