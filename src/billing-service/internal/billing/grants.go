package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/billing-service/internal/store"
)

type fundingLeg struct {
	GrantID           string `json:"grant_id,omitempty"`
	GrantAccountID    string `json:"grant_account_id,omitempty"`
	SettlementID      string `json:"settlement_transfer_id,omitempty"`
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
	rows, err := c.queries.ListGrantBalanceRows(ctx, store.ListGrantBalanceRowsParams{
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		Now:       timestamptz(now),
	})
	if err != nil {
		return nil, fmt.Errorf("query credit grants: %w", err)
	}
	out := []GrantBalance{}
	for _, row := range rows {
		accountID, err := ledger.IDFromBytes(row.AccountID)
		if err != nil {
			return nil, fmt.Errorf("parse grant account id %s: %w", row.GrantID, err)
		}
		g := GrantBalance{
			GrantID:             row.GrantID,
			OrgID:               orgID,
			ScopeType:           row.ScopeType,
			ScopeProductID:      row.ScopeProductID,
			ScopeBucketID:       row.ScopeBucketID,
			ScopeSKUID:          row.ScopeSkuID,
			Source:              row.Source,
			SourceReferenceID:   row.SourceReferenceID,
			EntitlementPeriodID: row.EntitlementPeriodID,
			PolicyVersion:       row.PolicyVersion,
			PlanID:              row.PlanID,
			PlanTier:            row.PlanTier,
			PlanDisplayName:     row.PlanDisplayName,
			StartsAt:            row.StartsAt.Time.UTC(),
			OriginalAmount:      uint64(row.Amount),
			Amount:              uint64(row.Amount),
			PeriodStart:         timePtr(row.PeriodStart),
			PeriodEnd:           timePtr(row.PeriodEnd),
			ExpiresAt:           timePtr(row.ExpiresAt),
			ledgerAccountID:     accountID,
		}
		out = append(out, g)
	}
	if err := c.hydrateGrantLedgerBalances(ctx, out); err != nil {
		return nil, err
	}
	authorized, err := c.grantAuthorizedUsage(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		amount := authorized[out[i].GrantID]
		if amount == 0 {
			continue
		}
		out[i].Pending += amount
		if amount >= out[i].Available {
			out[i].Available = 0
			continue
		}
		out[i].Available -= amount
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

func (c *Client) grantAuthorizedUsage(ctx context.Context, orgID OrgID) (map[string]uint64, error) {
	rows, err := c.queries.ListAuthorizedGrantUsage(ctx, store.ListAuthorizedGrantUsageParams{OrgID: orgIDText(orgID)})
	if err != nil {
		return nil, fmt.Errorf("query authorized grant usage: %w", err)
	}
	authorized := map[string]uint64{}
	for _, row := range rows {
		if row.GrantID.Valid && row.Amount > 0 {
			authorized[row.GrantID.String] = uint64(row.Amount)
		}
	}
	return authorized, nil
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
