package billing

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/forge-metal/billing-service/internal/store"
)

func (c *Client) EnsureOrg(ctx context.Context, orgID OrgID, displayName string, trustTier string) error {
	if trustTier == "" {
		trustTier = "new"
	}
	err := c.queries.UpsertOrg(ctx, store.UpsertOrgParams{
		OrgID:            orgIDText(orgID),
		DisplayName:      cleanNonEmpty(displayName, "Org "+orgIDText(orgID)),
		BillingEmail:     "",
		TrustTier:        trustTier,
		OveragePolicy:    "block",
		OverageConsentAt: pgtype.Timestamptz{},
	})
	if err != nil {
		return fmt.Errorf("upsert org: %w", err)
	}
	products, err := c.queries.ListProductIDs(ctx)
	if err != nil {
		return fmt.Errorf("list products for org bootstrap: %w", err)
	}
	for _, productID := range products {
		if err := c.EnsureCurrentEntitlements(ctx, orgID, productID); err != nil {
			return err
		}
		if _, err := c.EnsureOpenBillingCycle(ctx, orgID, productID); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ListPlans(ctx context.Context, productID string) ([]PlanRecord, error) {
	rows, err := c.queries.ListActivePlans(ctx, store.ListActivePlansParams{ProductID: productID})
	if err != nil {
		return nil, fmt.Errorf("list active plans: %w", err)
	}
	out := make([]PlanRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, PlanRecord{
			PlanID:             row.PlanID,
			ProductID:          row.ProductID,
			DisplayName:        row.DisplayName,
			BillingMode:        row.BillingMode,
			Tier:               row.Tier,
			Currency:           row.Currency,
			MonthlyAmountCents: uint64(row.MonthlyAmountCents),
			AnnualAmountCents:  uint64(row.AnnualAmountCents),
			Active:             row.Active,
			IsDefault:          row.IsDefault,
		})
	}
	return out, nil
}

func (c *Client) ListContracts(ctx context.Context, orgID OrgID) ([]ContractRecord, error) {
	now, err := c.BusinessNow(ctx, c.queries, orgID, "")
	if err != nil {
		return nil, err
	}
	rows, err := c.queries.ListContractsForOrg(ctx, store.ListContractsForOrgParams{OrgID: orgIDText(orgID), EffectiveStart: timestamptz(now)})
	if err != nil {
		return nil, fmt.Errorf("list contracts: %w", err)
	}
	out := make([]ContractRecord, 0, len(rows))
	for _, row := range rows {
		record := ContractRecord{
			ContractID:       row.ContractID,
			ProductID:        row.ProductID,
			PlanID:           row.PlanID,
			PhaseID:          row.PhaseID,
			CadenceKind:      "anniversary_monthly",
			Status:           row.State,
			PaymentState:     row.PaymentState,
			EntitlementState: row.EntitlementState,
		}
		if row.StartsAt.Valid {
			record.StartsAt = row.StartsAt.Time.UTC()
		}
		if row.EndsAt.Valid {
			v := row.EndsAt.Time.UTC()
			record.EndsAt = &v
		}
		if row.PhaseStart.Valid {
			v := row.PhaseStart.Time.UTC()
			record.PhaseStart = &v
		}
		if row.PhaseEnd.Valid {
			v := row.PhaseEnd.Time.UTC()
			record.PhaseEnd = &v
		}
		out = append(out, record)
	}
	return out, nil
}
