package billing

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/billing-service/internal/store"
)

func (c *Client) EnsureCurrentEntitlements(ctx context.Context, orgID OrgID, productID string) error {
	if _, err := c.ApplyDueBillingWork(ctx, orgID, productID); err != nil {
		return err
	}
	if err := c.ensureCurrentEntitlements(ctx, orgID, productID); err != nil {
		return err
	}
	_, err := c.PostPendingGrantDeposits(ctx, orgID, productID)
	return err
}

func (c *Client) ensureCurrentEntitlements(ctx context.Context, orgID OrgID, productID string) error {
	return c.WithTx(ctx, "billing.entitlements.ensure_current", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		now, err := c.BusinessNow(ctx, q, orgID, productID)
		if err != nil {
			return err
		}
		cycle, err := c.ensureOpenBillingCycleTx(ctx, tx, q, orgID, productID, now)
		if err != nil {
			return err
		}
		if err := c.materializeFreeTierTx(ctx, tx, q, orgID, productID, cycle, now); err != nil {
			return err
		}
		if err := c.materializeActiveContractTx(ctx, tx, q, orgID, productID, cycle, now); err != nil {
			return err
		}
		return nil
	})
}

func (c *Client) materializeFreeTierTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, cycle billingCycle, now time.Time) error {
	rows, err := q.ListActiveFreeTierPolicies(ctx, store.ListActiveFreeTierPoliciesParams{
		ProductID: productID,
		Now:       timestamptz(now),
	})
	if err != nil {
		return fmt.Errorf("query free-tier policies: %w", err)
	}
	inserts := []entitlementInsert{}
	for _, row := range rows {
		periodID := freeTierPeriodID(orgID, row.PolicyID, cycle.StartsAt, cycle.EndsAt)
		sourceRef := fmt.Sprintf("free_tier:%s:%s:%s", row.PolicyID, cycle.StartsAt.Format(time.RFC3339Nano), cycle.EndsAt.Format(time.RFC3339Nano))
		inserts = append(inserts, entitlementInsert{
			PeriodID: periodID, OrgID: orgID, ProductID: productID, CycleID: cycle.CycleID, Source: "free_tier", PolicyID: row.PolicyID,
			ScopeType: row.ScopeType, ScopeProductID: row.ScopeProductID, ScopeBucketID: row.ScopeBucketID, ScopeSKUID: row.ScopeSkuID,
			Amount: checkedUint64FromInt64(row.AmountUnits, "free tier amount units"), PeriodStart: cycle.StartsAt, PeriodEnd: cycle.EndsAt, PolicyVersion: row.PolicyVersion,
			EntitlementState: "active", PaymentState: "not_required", CalculationKind: "recurrence", SourceReferenceID: sourceRef,
			GrantID: grantID(orgID, "free_tier", row.ScopeType, row.ScopeProductID, row.ScopeBucketID, row.ScopeSkuID, sourceRef),
		})
	}
	for _, insert := range inserts {
		if err := c.insertEntitlementAndGrantTx(ctx, tx, q, insert); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) materializeActiveContractTx(ctx context.Context, tx pgx.Tx, q *store.Queries, orgID OrgID, productID string, cycle billingCycle, now time.Time) error {
	rows, err := q.ListActiveContractEntitlementLines(ctx, store.ListActiveContractEntitlementLinesParams{
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		Now:       timestamptz(now),
	})
	if err != nil {
		return fmt.Errorf("query active contract entitlement lines: %w", err)
	}
	inserts := []entitlementInsert{}
	for _, row := range rows {
		periodID := contractPeriodID(orgID, row.LineID, cycle.CycleID, cycle.StartsAt, cycle.EndsAt)
		sourceRef := fmt.Sprintf("contract:%s:%s:%s", row.LineID, cycle.StartsAt.Format(time.RFC3339Nano), cycle.EndsAt.Format(time.RFC3339Nano))
		inserts = append(inserts, entitlementInsert{
			PeriodID: periodID, OrgID: orgID, ProductID: productID, CycleID: cycle.CycleID, Source: "contract", PolicyID: row.PolicyID.String,
			ContractID: row.ContractID, PhaseID: row.PhaseID, LineID: row.LineID, ScopeType: row.ScopeType, ScopeProductID: row.ScopeProductID, ScopeBucketID: row.ScopeBucketID, ScopeSKUID: row.ScopeSkuID,
			PlanID: row.PlanID,
			Amount: checkedUint64FromInt64(row.AmountUnits, "contract amount units"), PeriodStart: cycle.StartsAt, PeriodEnd: cycle.EndsAt, PolicyVersion: row.PolicyVersion,
			EntitlementState: "active", PaymentState: "paid", CalculationKind: "recurrence", SourceReferenceID: sourceRef,
			GrantID: grantID(orgID, "contract", row.ScopeType, row.ScopeProductID, row.ScopeBucketID, row.ScopeSkuID, sourceRef),
		})
	}
	for _, insert := range inserts {
		if err := c.insertEntitlementAndGrantTx(ctx, tx, q, insert); err != nil {
			return err
		}
	}
	return nil
}

type entitlementInsert struct {
	PeriodID, ProductID, CycleID, Source, PolicyID, ContractID, PhaseID, LineID string
	ScopeType, ScopeProductID, ScopeBucketID, ScopeSKUID                        string
	PolicyVersion, EntitlementState, PaymentState, CalculationKind              string
	PlanID                                                                      string
	SourceReferenceID, GrantID                                                  string
	OrgID                                                                       OrgID
	Amount                                                                      uint64
	PeriodStart, PeriodEnd                                                      time.Time
}

func (c *Client) insertEntitlementAndGrantTx(ctx context.Context, tx pgx.Tx, q *store.Queries, in entitlementInsert) error {
	accountID := ledger.NewID()
	depositID := ledger.NewID()
	if err := q.InsertEntitlementPeriod(ctx, store.InsertEntitlementPeriodParams{
		PeriodID:          in.PeriodID,
		OrgID:             orgIDText(in.OrgID),
		ProductID:         in.ProductID,
		CycleID:           pgTextValue(in.CycleID),
		Source:            in.Source,
		PolicyID:          in.PolicyID,
		ContractID:        in.ContractID,
		PhaseID:           in.PhaseID,
		LineID:            in.LineID,
		ScopeType:         in.ScopeType,
		ScopeProductID:    in.ScopeProductID,
		ScopeBucketID:     in.ScopeBucketID,
		ScopeSkuID:        in.ScopeSKUID,
		AmountUnits:       checkedInt64FromUint64(in.Amount, "entitlement amount units"),
		PeriodStart:       timestamptz(in.PeriodStart),
		PeriodEnd:         timestamptz(in.PeriodEnd),
		PolicyVersion:     in.PolicyVersion,
		PaymentState:      in.PaymentState,
		EntitlementState:  in.EntitlementState,
		CalculationKind:   in.CalculationKind,
		SourceReferenceID: in.SourceReferenceID,
	}); err != nil {
		return fmt.Errorf("insert entitlement period %s: %w", in.PeriodID, err)
	}
	rowsAffected, err := q.InsertCreditGrantForEntitlement(ctx, store.InsertCreditGrantForEntitlementParams{
		GrantID:             in.GrantID,
		OrgID:               orgIDText(in.OrgID),
		ScopeType:           in.ScopeType,
		ScopeProductID:      in.ScopeProductID,
		ScopeBucketID:       in.ScopeBucketID,
		ScopeSkuID:          in.ScopeSKUID,
		Amount:              checkedInt64FromUint64(in.Amount, "credit grant amount"),
		Source:              in.Source,
		SourceReferenceID:   in.SourceReferenceID,
		EntitlementPeriodID: pgTextValue(in.PeriodID),
		PolicyVersion:       in.PolicyVersion,
		StartsAt:            timestamptz(in.PeriodStart),
		PeriodStart:         timestamptz(in.PeriodStart),
		PeriodEnd:           timestamptz(in.PeriodEnd),
		AccountID:           accountID.Bytes(),
		DepositTransferID:   depositID.Bytes(),
	})
	if err != nil {
		return fmt.Errorf("insert credit grant %s: %w", in.GrantID, err)
	}
	if rowsAffected == 0 {
		if err := c.reopenMaterializedEntitlementAndGrantTx(ctx, q, in); err != nil {
			return err
		}
		return nil
	}
	payload := map[string]any{
		"grant_id":              in.GrantID,
		"period_id":             in.PeriodID,
		"cycle_id":              in.CycleID,
		"source":                in.Source,
		"source_reference_id":   in.SourceReferenceID,
		"amount":                in.Amount,
		"scope_type":            in.ScopeType,
		"scope_product_id":      in.ScopeProductID,
		"scope_bucket_id":       in.ScopeBucketID,
		"scope_sku_id":          in.ScopeSKUID,
		"period_start":          in.PeriodStart.Format(time.RFC3339Nano),
		"period_end":            in.PeriodEnd.Format(time.RFC3339Nano),
		"pricing_contract_id":   in.ContractID,
		"pricing_phase_id":      in.PhaseID,
		"pricing_plan_id":       in.PlanID,
		"entitlement_policy_id": in.PolicyID,
	}
	return appendEvent(ctx, tx, q, eventFact{EventType: "grant_issued", AggregateType: "credit_grant", AggregateID: in.GrantID, OrgID: in.OrgID, ProductID: in.ProductID, OccurredAt: in.PeriodStart, Payload: payload})
}

func (c *Client) reopenMaterializedEntitlementAndGrantTx(ctx context.Context, q *store.Queries, in entitlementInsert) error {
	if err := q.ReopenMaterializedEntitlementPeriod(ctx, store.ReopenMaterializedEntitlementPeriodParams{
		CycleID:           pgTextValue(in.CycleID),
		AmountUnits:       checkedInt64FromUint64(in.Amount, "entitlement amount units"),
		PeriodStart:       timestamptz(in.PeriodStart),
		PeriodEnd:         timestamptz(in.PeriodEnd),
		PolicyVersion:     in.PolicyVersion,
		PaymentState:      in.PaymentState,
		EntitlementState:  in.EntitlementState,
		CalculationKind:   in.CalculationKind,
		OrgID:             orgIDText(in.OrgID),
		Source:            in.Source,
		SourceReferenceID: in.SourceReferenceID,
	}); err != nil {
		return fmt.Errorf("reopen entitlement period %s: %w", in.PeriodID, err)
	}
	if err := q.ReopenMaterializedCreditGrant(ctx, store.ReopenMaterializedCreditGrantParams{
		EntitlementPeriodID: pgTextValue(in.PeriodID),
		PolicyVersion:       in.PolicyVersion,
		PeriodStart:         timestamptz(in.PeriodStart),
		PeriodEnd:           timestamptz(in.PeriodEnd),
		OrgID:               orgIDText(in.OrgID),
		Source:              in.Source,
		ScopeType:           in.ScopeType,
		ScopeProductID:      pgtype.Text{String: in.ScopeProductID, Valid: true},
		ScopeBucketID:       pgtype.Text{String: in.ScopeBucketID, Valid: true},
		ScopeSkuID:          pgtype.Text{String: in.ScopeSKUID, Valid: true},
		SourceReferenceID:   in.SourceReferenceID,
	}); err != nil {
		return fmt.Errorf("reopen credit grant %s: %w", in.GrantID, err)
	}
	return nil
}

func (c *Client) DepositCredits(ctx context.Context, grant GrantBalance) (GrantBalance, error) {
	if grant.Source == "" {
		grant.Source = "purchase"
	}
	if grant.ScopeType == "" {
		grant.ScopeType = "account"
	}
	orgID := grant.OrgID
	if orgID == 0 {
		return GrantBalance{}, fmt.Errorf("org_id is required")
	}
	if firstUint64(grant.OriginalAmount, grant.Amount) == 0 {
		return GrantBalance{}, fmt.Errorf("amount is required")
	}
	if grant.SourceReferenceID == "" {
		grant.SourceReferenceID = textID("deposit_ref", orgIDText(orgID), grant.GrantID, time.Now().UTC().Format(time.RFC3339Nano))
	}
	if grant.GrantID == "" {
		grant.GrantID = grantID(orgID, grant.Source, grant.ScopeType, grant.ScopeProductID, grant.ScopeBucketID, grant.ScopeSKUID, grant.SourceReferenceID)
	}
	if grant.StartsAt.IsZero() {
		grant.StartsAt = time.Now().UTC()
	}
	accountID := ledger.NewID()
	depositID := ledger.NewID()
	err := c.WithTx(ctx, "billing.credits.deposit", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		if err := q.InsertManualCreditGrant(ctx, store.InsertManualCreditGrantParams{
			GrantID:             grant.GrantID,
			OrgID:               orgIDText(orgID),
			ScopeType:           grant.ScopeType,
			ScopeProductID:      grant.ScopeProductID,
			ScopeBucketID:       grant.ScopeBucketID,
			ScopeSkuID:          grant.ScopeSKUID,
			Amount:              checkedInt64FromUint64(firstUint64(grant.OriginalAmount, grant.Amount), "manual grant amount"),
			Source:              grant.Source,
			SourceReferenceID:   grant.SourceReferenceID,
			EntitlementPeriodID: grant.EntitlementPeriodID,
			PolicyVersion:       cleanNonEmpty(grant.PolicyVersion, "v1"),
			StartsAt:            timestamptz(grant.StartsAt),
			ExpiresAt:           timestamptzValue(nullableTime(grant.ExpiresAt)),
			AccountID:           accountID.Bytes(),
			DepositTransferID:   depositID.Bytes(),
		}); err != nil {
			return fmt.Errorf("insert deposit grant: %w", err)
		}
		return appendEvent(ctx, tx, q, eventFact{EventType: "grant_issued", AggregateType: "credit_grant", AggregateID: grant.GrantID, OrgID: orgID, ProductID: grant.ScopeProductID, OccurredAt: grant.StartsAt, Payload: map[string]any{"grant_id": grant.GrantID, "source": grant.Source, "amount": firstUint64(grant.OriginalAmount, grant.Amount)}})
	})
	if err != nil {
		return grant, err
	}
	return grant, c.postGrantDeposit(ctx, grant.GrantID)
}

func firstUint64(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func prorateUnits(full uint64, numerator, denominator int64) uint64 {
	if denominator <= 0 || numerator <= 0 || full == 0 {
		return 0
	}
	return uint64(math.Ceil(float64(full) * float64(numerator) / float64(denominator)))
}
