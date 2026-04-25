package billing

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/verself/billing-service/internal/store"
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
	productRows, err := c.pg.Query(ctx, `
		SELECT DISTINCT product_id
		FROM contracts
		WHERE org_id = $1
		ORDER BY product_id
	`, orgIDText(orgID))
	if err != nil {
		return nil, fmt.Errorf("list contract products: %w", err)
	}
	productIDs := []string{}
	for productRows.Next() {
		var productID string
		if err := productRows.Scan(&productID); err != nil {
			productRows.Close()
			return nil, fmt.Errorf("scan contract product: %w", err)
		}
		productIDs = append(productIDs, productID)
	}
	if err := productRows.Err(); err != nil {
		productRows.Close()
		return nil, fmt.Errorf("scan contract products: %w", err)
	}
	productRows.Close()
	for _, productID := range productIDs {
		if _, err := c.ApplyDueBillingWork(ctx, orgID, productID); err != nil {
			return nil, err
		}
	}
	rows, err := c.pg.Query(ctx, `
		WITH contract_now AS (
			SELECT c.contract_id,
			       COALESCE(product_clock.business_now, org_clock.business_now, global_clock.business_now, transaction_timestamp()) AS effective_at
			FROM contracts c
			LEFT JOIN billing_clock_overrides product_clock
			  ON product_clock.scope_kind = 'org_product'
			 AND product_clock.scope_id = c.org_id || ':' || c.product_id
			LEFT JOIN billing_clock_overrides org_clock
			  ON org_clock.scope_kind = 'org'
			 AND org_clock.scope_id = c.org_id
			LEFT JOIN billing_clock_overrides global_clock
			  ON global_clock.scope_kind = 'global'
			 AND global_clock.scope_id = ''
			WHERE c.org_id = $1
		),
		current_phase AS (
			SELECT DISTINCT ON (p.contract_id)
			       p.contract_id, p.phase_id, COALESCE(p.plan_id, '') AS plan_id, p.effective_start, p.effective_end
			FROM contract_phases p
			JOIN contract_now cn ON cn.contract_id = p.contract_id
			WHERE p.org_id = $1
			  AND p.state IN ('active', 'grace', 'pending_payment', 'scheduled')
			  AND p.effective_start <= cn.effective_at
			  AND (p.effective_end IS NULL OR p.effective_end > cn.effective_at)
			ORDER BY p.contract_id, p.effective_start DESC, p.phase_id DESC
		)
		SELECT c.contract_id, c.product_id, COALESCE(cp.plan_id, '') AS plan_id, COALESCE(cp.phase_id, '') AS phase_id,
		       c.state, c.payment_state, c.entitlement_state,
		       COALESCE(pending_change.change_id, '') AS pending_change_id,
		       COALESCE(pending_change.change_type, '') AS pending_change_type,
		       COALESCE(pending_change.target_plan_id, '') AS pending_change_target_plan_id,
		       pending_change.requested_effective_at AS pending_change_effective_at,
		       c.starts_at, c.ends_at, cp.effective_start AS phase_start, cp.effective_end AS phase_end
		FROM contracts c
		LEFT JOIN current_phase cp ON cp.contract_id = c.contract_id
		LEFT JOIN LATERAL (
			SELECT cc.change_id, cc.change_type, COALESCE(cc.target_plan_id, '') AS target_plan_id, cc.requested_effective_at
			FROM contract_changes cc
			WHERE cc.contract_id = c.contract_id
			  AND cc.org_id = c.org_id
			  AND cc.product_id = c.product_id
			  AND cc.state = 'scheduled'
			  AND cc.timing = 'period_end'
			  AND cc.change_type IN ('downgrade', 'cancel')
			ORDER BY cc.requested_effective_at, cc.change_id
			LIMIT 1
		) pending_change ON true
		WHERE c.org_id = $1
		ORDER BY c.starts_at DESC, c.contract_id DESC
	`, orgIDText(orgID))
	if err != nil {
		return nil, fmt.Errorf("list contracts: %w", err)
	}
	defer rows.Close()
	out := []ContractRecord{}
	for rows.Next() {
		var row struct {
			ContractID                string
			ProductID                 string
			PlanID                    string
			PhaseID                   string
			State                     string
			PaymentState              string
			EntitlementState          string
			PendingChangeID           string
			PendingChangeType         string
			PendingChangeTargetPlanID string
			PendingChangeEffectiveAt  pgtype.Timestamptz
			StartsAt                  pgtype.Timestamptz
			EndsAt                    pgtype.Timestamptz
			PhaseStart                pgtype.Timestamptz
			PhaseEnd                  pgtype.Timestamptz
		}
		if err := rows.Scan(&row.ContractID, &row.ProductID, &row.PlanID, &row.PhaseID, &row.State, &row.PaymentState, &row.EntitlementState, &row.PendingChangeID, &row.PendingChangeType, &row.PendingChangeTargetPlanID, &row.PendingChangeEffectiveAt, &row.StartsAt, &row.EndsAt, &row.PhaseStart, &row.PhaseEnd); err != nil {
			return nil, fmt.Errorf("scan contract: %w", err)
		}
		record := ContractRecord{
			ContractID:                row.ContractID,
			ProductID:                 row.ProductID,
			PlanID:                    row.PlanID,
			PhaseID:                   row.PhaseID,
			CadenceKind:               "anniversary_monthly",
			Status:                    row.State,
			PaymentState:              row.PaymentState,
			EntitlementState:          row.EntitlementState,
			PendingChangeID:           row.PendingChangeID,
			PendingChangeType:         row.PendingChangeType,
			PendingChangeTargetPlanID: row.PendingChangeTargetPlanID,
		}
		if row.PendingChangeEffectiveAt.Valid {
			v := row.PendingChangeEffectiveAt.Time.UTC()
			record.PendingChangeEffectiveAt = &v
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
	return out, rows.Err()
}
