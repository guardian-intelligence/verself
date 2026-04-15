package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/forge-metal/billing-service/internal/billing/ledger"
)

type fundingLeg struct {
	GrantID           string `json:"grant_id,omitempty"`
	GrantAccountID    string `json:"grant_account_id,omitempty"`
	ReservationID     string `json:"reservation_transfer_id,omitempty"`
	SettlementID      string `json:"settlement_transfer_id,omitempty"`
	VoidID            string `json:"void_transfer_id,omitempty"`
	Amount            uint64 `json:"amount"`
	Source            string `json:"source"`
	ScopeType         string `json:"scope_type,omitempty"`
	ScopeProductID    string `json:"scope_product_id,omitempty"`
	ScopeBucketID     string `json:"scope_bucket_id,omitempty"`
	ScopeSKUID        string `json:"scope_sku_id,omitempty"`
	PlanID            string `json:"plan_id,omitempty"`
	ComponentSKUID    string `json:"component_sku_id,omitempty"`
	ComponentBucketID string `json:"component_bucket_id,omitempty"`
}

func (c *Client) ListGrantBalances(ctx context.Context, orgID OrgID, productID string) ([]GrantBalance, error) {
	now, err := c.BusinessNow(ctx, c.queries, orgID, productID)
	if err != nil {
		return nil, err
	}
	if productID != "" {
		if err := c.EnsureCurrentEntitlements(ctx, orgID, productID); err != nil {
			return nil, err
		}
	} else if _, err := c.PostPendingGrantDeposits(ctx, orgID, ""); err != nil {
		return nil, err
	}
	rows, err := c.pg.Query(ctx, `
		SELECT g.grant_id, g.scope_type, COALESCE(g.scope_product_id,''), COALESCE(g.scope_bucket_id,''), COALESCE(g.scope_sku_id,''),
		       g.amount, g.source, g.source_reference_id, COALESCE(g.entitlement_period_id,''), g.policy_version,
		       COALESCE(cp.plan_id,''), COALESCE(pl.tier,''), COALESCE(pl.display_name,''), g.starts_at, g.period_start, g.period_end, g.expires_at, g.account_id
		FROM credit_grants g
		LEFT JOIN entitlement_periods p ON p.period_id = g.entitlement_period_id
		LEFT JOIN contract_phases cp ON cp.phase_id = p.phase_id
		LEFT JOIN plans pl ON pl.plan_id = cp.plan_id
		WHERE g.org_id = $1
		  AND g.closed_at IS NULL
		  AND ($2 = '' OR COALESCE(g.scope_product_id, $2) = $2 OR g.scope_type = 'account')
		  AND (g.expires_at IS NULL OR g.expires_at > $3)
		ORDER BY CASE g.source WHEN 'free_tier' THEN 1 WHEN 'contract' THEN 2 WHEN 'promo' THEN 3 WHEN 'refund' THEN 4 WHEN 'purchase' THEN 5 ELSE 6 END, g.starts_at, g.grant_id
	`, orgIDText(orgID), productID, now)
	if err != nil {
		return nil, fmt.Errorf("query credit grants: %w", err)
	}
	defer rows.Close()
	out := []GrantBalance{}
	for rows.Next() {
		var g GrantBalance
		var amount int64
		var accountRaw []byte
		var periodStart, periodEnd, expiresAt pgtype.Timestamptz
		if err := rows.Scan(&g.GrantID, &g.ScopeType, &g.ScopeProductID, &g.ScopeBucketID, &g.ScopeSKUID, &amount, &g.Source, &g.SourceReferenceID, &g.EntitlementPeriodID, &g.PolicyVersion, &g.PlanID, &g.PlanTier, &g.PlanDisplayName, &g.StartsAt, &periodStart, &periodEnd, &expiresAt, &accountRaw); err != nil {
			return nil, fmt.Errorf("scan credit grant: %w", err)
		}
		accountID, err := ledger.IDFromBytes(accountRaw)
		if err != nil {
			return nil, fmt.Errorf("parse grant account id %s: %w", g.GrantID, err)
		}
		g.OrgID = orgID
		g.OriginalAmount = uint64(amount)
		g.Amount = uint64(amount)
		g.PeriodStart = timePtr(periodStart)
		g.PeriodEnd = timePtr(periodEnd)
		g.ExpiresAt = timePtr(expiresAt)
		g.ledgerAccountID = accountID
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := c.hydrateGrantLedgerBalances(ctx, out); err != nil {
		return nil, err
	}
	return grantsByFundingPriority(out), nil
}

func (c *Client) hydrateGrantLedgerBalances(ctx context.Context, grants []GrantBalance) error {
	if len(grants) == 0 {
		return nil
	}
	ledgerClient, err := c.requireLedger()
	if err != nil {
		return err
	}
	ids := make([]ledger.ID, 0, len(grants))
	for _, grant := range grants {
		if grant.ledgerAccountID.IsZero() {
			return fmt.Errorf("grant %s missing ledger account id", grant.GrantID)
		}
		ids = append(ids, grant.ledgerAccountID)
	}
	balances, err := ledgerClient.LookupBalances(ctx, ids)
	if err != nil {
		return err
	}
	for i := range grants {
		balance, ok := balances[grants[i].ledgerAccountID]
		if !ok {
			return fmt.Errorf("%w: grant %s account %s", ledger.ErrAccountNotFound, grants[i].GrantID, grants[i].ledgerAccountID.String())
		}
		grants[i].Available = balance.Available
		grants[i].Pending = balance.Pending
		grants[i].Spent = balance.Spent
	}
	return nil
}

func (c *Client) grantUsage(ctx context.Context, orgID OrgID) (map[string]uint64, map[string]uint64, error) {
	rows, err := c.pg.Query(ctx, `
		SELECT w.state, leg->>'grant_id', SUM((leg->>'amount')::bigint)
		FROM billing_windows w
		CROSS JOIN LATERAL jsonb_array_elements(w.funding_legs) leg
		WHERE w.org_id = $1
		  AND w.state IN ('reserved', 'active', 'settled')
		  AND COALESCE(leg->>'grant_id','') <> ''
		GROUP BY w.state, leg->>'grant_id'
	`, orgIDText(orgID))
	if err != nil {
		return nil, nil, fmt.Errorf("query grant usage: %w", err)
	}
	defer rows.Close()
	spent := map[string]uint64{}
	pending := map[string]uint64{}
	for rows.Next() {
		var state, grantID string
		var amount int64
		if err := rows.Scan(&state, &grantID, &amount); err != nil {
			return nil, nil, fmt.Errorf("scan grant usage: %w", err)
		}
		if state == "settled" {
			spent[grantID] += uint64(amount)
		} else {
			pending[grantID] += uint64(amount)
		}
	}
	return spent, pending, rows.Err()
}

func grantsByFundingPriority(grants []GrantBalance) []GrantBalance {
	out := append([]GrantBalance(nil), grants...)
	sort.SliceStable(out, func(i, j int) bool {
		if sourcePriority(out[i].Source) != sourcePriority(out[j].Source) {
			return sourcePriority(out[i].Source) < sourcePriority(out[j].Source)
		}
		if scopePriority(out[i].ScopeType) != scopePriority(out[j].ScopeType) {
			return scopePriority(out[i].ScopeType) < scopePriority(out[j].ScopeType)
		}
		if planPriority(out[i].PlanTier, out[i].PlanID) != planPriority(out[j].PlanTier, out[j].PlanID) {
			return planPriority(out[i].PlanTier, out[i].PlanID) < planPriority(out[j].PlanTier, out[j].PlanID)
		}
		if !out[i].StartsAt.Equal(out[j].StartsAt) {
			return out[i].StartsAt.Before(out[j].StartsAt)
		}
		return out[i].GrantID < out[j].GrantID
	})
	return out
}

func scopePriority(scope string) int {
	switch scope {
	case "sku":
		return 1
	case "bucket":
		return 2
	case "product":
		return 3
	case "account":
		return 4
	default:
		return 99
	}
}

func sourcePriority(source string) int {
	switch source {
	case "free_tier":
		return 1
	case "contract":
		return 2
	case "promo":
		return 3
	case "refund":
		return 4
	case "purchase":
		return 5
	default:
		return 99
	}
}

func planPriority(tier, planID string) int {
	switch tier {
	case "":
		return 0
	case "default":
		return 10
	case "hobby":
		return 20
	case "pro":
		return 30
	default:
		if planID == "" {
			return 90
		}
		return 50
	}
}

func grantCoversSKU(grant GrantBalance, productID, bucketID, skuID string) bool {
	switch grant.ScopeType {
	case "account":
		return true
	case "product":
		return grant.ScopeProductID == productID
	case "bucket":
		return grant.ScopeProductID == productID && grant.ScopeBucketID == bucketID
	case "sku":
		return grant.ScopeProductID == productID && grant.ScopeBucketID == bucketID && grant.ScopeSKUID == skuID
	default:
		return false
	}
}

func fundingLegsJSON(legs []fundingLeg) ([]byte, error) {
	if legs == nil {
		legs = []fundingLeg{}
	}
	return json.Marshal(legs)
}
