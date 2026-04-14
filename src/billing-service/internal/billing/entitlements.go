package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/forge-metal/billing-service/internal/billing/ledger"
	"github.com/forge-metal/billing-service/internal/store"
)

type grantRow struct {
	GrantID             string
	ScopeType           string
	ScopeProductID      string
	ScopeBucketID       string
	ScopeSKUID          string
	Amount              uint64
	Source              string
	SourceReferenceID   string
	EntitlementPeriodID string
	PolicyVersion       string
	StartsAt            time.Time
	PeriodStart         *time.Time
	PeriodEnd           *time.Time
	ExpiresAt           *time.Time
	PlanID              string
	PlanDisplayName     string
}

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
	return c.WithTx(ctx, "billing.entitlements.ensure_current", func(ctx context.Context, tx pgx.Tx, _ *store.Queries) error {
		q := c.queries.WithTx(tx)
		now, err := c.BusinessNow(ctx, q, orgID, productID)
		if err != nil {
			return err
		}
		cycle, err := c.ensureOpenBillingCycleTx(ctx, tx, orgID, productID, now)
		if err != nil {
			return err
		}
		if err := c.materializeFreeTierTx(ctx, tx, orgID, productID, cycle, now); err != nil {
			return err
		}
		if err := c.materializeActiveContractTx(ctx, tx, orgID, productID, cycle, now); err != nil {
			return err
		}
		return nil
	})
}

func (c *Client) materializeFreeTierTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, cycle billingCycle, now time.Time) error {
	rows, err := tx.Query(ctx, `
		SELECT policy_id, scope_type, COALESCE(scope_product_id, ''), COALESCE(scope_bucket_id, ''), COALESCE(scope_sku_id, ''), amount_units, policy_version
		FROM entitlement_policies
		WHERE source = 'free_tier'
		  AND product_id = $1
		  AND active_from <= $2
		  AND (active_until IS NULL OR active_until > $2)
		ORDER BY policy_id
	`, productID, now)
	if err != nil {
		return fmt.Errorf("query free-tier policies: %w", err)
	}
	defer rows.Close()
	inserts := []entitlementInsert{}
	for rows.Next() {
		var policyID, scopeType, scopeProductID, scopeBucketID, scopeSKUID, policyVersion string
		var amount int64
		if err := rows.Scan(&policyID, &scopeType, &scopeProductID, &scopeBucketID, &scopeSKUID, &amount, &policyVersion); err != nil {
			return fmt.Errorf("scan free-tier policy: %w", err)
		}
		periodID := freeTierPeriodID(orgID, policyID, cycle.StartsAt, cycle.EndsAt)
		sourceRef := fmt.Sprintf("free_tier:%s:%s:%s", policyID, cycle.StartsAt.Format(time.RFC3339Nano), cycle.EndsAt.Format(time.RFC3339Nano))
		inserts = append(inserts, entitlementInsert{
			PeriodID: periodID, OrgID: orgID, ProductID: productID, CycleID: cycle.CycleID, Source: "free_tier", PolicyID: policyID,
			ScopeType: scopeType, ScopeProductID: scopeProductID, ScopeBucketID: scopeBucketID, ScopeSKUID: scopeSKUID,
			Amount: uint64(amount), PeriodStart: cycle.StartsAt, PeriodEnd: cycle.EndsAt, PolicyVersion: policyVersion,
			EntitlementState: "active", PaymentState: "not_required", CalculationKind: "recurrence", SourceReferenceID: sourceRef,
			GrantID: grantID(orgID, "free_tier", scopeType, scopeProductID, scopeBucketID, scopeSKUID, sourceRef),
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, insert := range inserts {
		if err := c.insertEntitlementAndGrantTx(ctx, tx, insert); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) materializeActiveContractTx(ctx context.Context, tx pgx.Tx, orgID OrgID, productID string, cycle billingCycle, now time.Time) error {
	rows, err := tx.Query(ctx, `
		SELECT l.line_id, l.phase_id, l.contract_id, l.policy_id, l.scope_type,
		       COALESCE(l.scope_product_id, ''), COALESCE(l.scope_bucket_id, ''), COALESCE(l.scope_sku_id, ''),
		       l.amount_units, l.policy_version, COALESCE(p.plan_id, '')
		FROM contract_entitlement_lines l
		JOIN contract_phases p ON p.phase_id = l.phase_id
		JOIN contracts c ON c.contract_id = l.contract_id
		WHERE l.org_id = $1
		  AND l.product_id = $2
		  AND p.state IN ('active', 'grace')
		  AND p.effective_start <= $3
		  AND (p.effective_end IS NULL OR p.effective_end > $3)
		  AND l.active_from <= $3
		  AND (l.active_until IS NULL OR l.active_until > $3)
		  AND c.state IN ('active', 'past_due', 'cancel_scheduled')
		ORDER BY l.line_id
	`, orgIDText(orgID), productID, now)
	if err != nil {
		return fmt.Errorf("query active contract entitlement lines: %w", err)
	}
	defer rows.Close()
	inserts := []entitlementInsert{}
	for rows.Next() {
		var lineID, phaseID, contractID, policyID, scopeType, scopeProductID, scopeBucketID, scopeSKUID, policyVersion, planID string
		var amount int64
		if err := rows.Scan(&lineID, &phaseID, &contractID, &policyID, &scopeType, &scopeProductID, &scopeBucketID, &scopeSKUID, &amount, &policyVersion, &planID); err != nil {
			return fmt.Errorf("scan contract entitlement line: %w", err)
		}
		periodID := contractPeriodID(orgID, lineID, cycle.CycleID, cycle.StartsAt, cycle.EndsAt)
		sourceRef := fmt.Sprintf("contract:%s:%s:%s", lineID, cycle.StartsAt.Format(time.RFC3339Nano), cycle.EndsAt.Format(time.RFC3339Nano))
		inserts = append(inserts, entitlementInsert{
			PeriodID: periodID, OrgID: orgID, ProductID: productID, CycleID: cycle.CycleID, Source: "contract", PolicyID: policyID,
			ContractID: contractID, PhaseID: phaseID, LineID: lineID, ScopeType: scopeType, ScopeProductID: scopeProductID, ScopeBucketID: scopeBucketID, ScopeSKUID: scopeSKUID,
			PlanID: planID,
			Amount: uint64(amount), PeriodStart: cycle.StartsAt, PeriodEnd: cycle.EndsAt, PolicyVersion: policyVersion,
			EntitlementState: "active", PaymentState: "paid", CalculationKind: "recurrence", SourceReferenceID: sourceRef,
			GrantID: grantID(orgID, "contract", scopeType, scopeProductID, scopeBucketID, scopeSKUID, sourceRef),
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, insert := range inserts {
		if err := c.insertEntitlementAndGrantTx(ctx, tx, insert); err != nil {
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

func (c *Client) insertEntitlementAndGrantTx(ctx context.Context, tx pgx.Tx, in entitlementInsert) error {
	accountID := ledger.NewID()
	depositID := ledger.NewID()
	_, err := tx.Exec(ctx, `
		INSERT INTO entitlement_periods (
			period_id, org_id, product_id, cycle_id, source, policy_id, contract_id, phase_id, line_id,
			scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount_units, period_start, period_end,
			policy_version, payment_state, entitlement_state, calculation_kind, source_reference_id, created_reason
		) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),$14,$15,$16,$17,$18,$19,$20,$21,'materialized')
		ON CONFLICT (org_id, source, source_reference_id) DO NOTHING
	`, in.PeriodID, orgIDText(in.OrgID), in.ProductID, in.CycleID, in.Source, in.PolicyID, in.ContractID, in.PhaseID, in.LineID, in.ScopeType, in.ScopeProductID, in.ScopeBucketID, in.ScopeSKUID, int64(in.Amount), in.PeriodStart, in.PeriodEnd, in.PolicyVersion, in.PaymentState, in.EntitlementState, in.CalculationKind, in.SourceReferenceID)
	if err != nil {
		return fmt.Errorf("insert entitlement period %s: %w", in.PeriodID, err)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO credit_grants (
			grant_id, org_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount, source,
			source_reference_id, entitlement_period_id, policy_version, starts_at, period_start, period_end, expires_at,
			account_id, deposit_transfer_id, ledger_posting_state
		) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13,$14,$14,$15,$16,'pending')
		ON CONFLICT (org_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, source_reference_id) DO NOTHING
	`, in.GrantID, orgIDText(in.OrgID), in.ScopeType, in.ScopeProductID, in.ScopeBucketID, in.ScopeSKUID, int64(in.Amount), in.Source, in.SourceReferenceID, in.PeriodID, in.PolicyVersion, in.PeriodStart, in.PeriodStart, in.PeriodEnd, accountID.Bytes(), depositID.Bytes())
	if err != nil {
		return fmt.Errorf("insert credit grant %s: %w", in.GrantID, err)
	}
	if tag.RowsAffected() == 0 {
		if err := c.reopenMaterializedEntitlementAndGrantTx(ctx, tx, in); err != nil {
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
	return appendEvent(ctx, tx, c.queries.WithTx(tx), eventFact{EventType: "grant_issued", AggregateType: "credit_grant", AggregateID: in.GrantID, OrgID: in.OrgID, ProductID: in.ProductID, OccurredAt: in.PeriodStart, Payload: payload})
}

func (c *Client) reopenMaterializedEntitlementAndGrantTx(ctx context.Context, tx pgx.Tx, in entitlementInsert) error {
	if _, err := tx.Exec(ctx, `
		UPDATE entitlement_periods
		SET cycle_id = $4,
		    amount_units = $5,
		    period_start = $6,
		    period_end = $7,
		    policy_version = $8,
		    payment_state = $9,
		    entitlement_state = $10,
		    calculation_kind = $11,
		    metadata = (metadata - 'voided_by') || jsonb_build_object('reopened_by', 'entitlement-materializer'),
		    updated_at = now()
		WHERE org_id = $1
		  AND source = $2
		  AND source_reference_id = $3
		  AND entitlement_state = 'voided'
	`, orgIDText(in.OrgID), in.Source, in.SourceReferenceID, in.CycleID, int64(in.Amount), in.PeriodStart, in.PeriodEnd, in.PolicyVersion, in.PaymentState, in.EntitlementState, in.CalculationKind); err != nil {
		return fmt.Errorf("reopen entitlement period %s: %w", in.PeriodID, err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE credit_grants
		SET entitlement_period_id = $8,
		    policy_version = $9,
		    starts_at = $10,
		    period_start = $10,
		    period_end = $11,
		    expires_at = $11,
		    closed_at = NULL,
		    closed_reason = '',
		    metadata = (metadata - 'closed_by') || jsonb_build_object('reopened_by', 'entitlement-materializer'),
		    updated_at = now()
		WHERE org_id = $1
		  AND source = $2
		  AND scope_type = $3
		  AND COALESCE(scope_product_id, '') = $4
		  AND COALESCE(scope_bucket_id, '') = $5
		  AND COALESCE(scope_sku_id, '') = $6
		  AND source_reference_id = $7
		  AND closed_at IS NOT NULL
	`, orgIDText(in.OrgID), in.Source, in.ScopeType, in.ScopeProductID, in.ScopeBucketID, in.ScopeSKUID, in.SourceReferenceID, in.PeriodID, in.PolicyVersion, in.PeriodStart, in.PeriodEnd); err != nil {
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
	err := c.WithTx(ctx, "billing.credits.deposit", func(ctx context.Context, tx pgx.Tx, _ *store.Queries) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO credit_grants (
				grant_id, org_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount, source,
				source_reference_id, entitlement_period_id, policy_version, starts_at, expires_at,
				account_id, deposit_transfer_id, ledger_posting_state
			)
			VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,$8,$9,NULLIF($10,''),$11,$12,$13,$14,$15,'pending')
			ON CONFLICT (org_id, source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, source_reference_id) DO NOTHING
		`, grant.GrantID, orgIDText(orgID), grant.ScopeType, grant.ScopeProductID, grant.ScopeBucketID, grant.ScopeSKUID, int64(firstUint64(grant.OriginalAmount, grant.Amount)), grant.Source, grant.SourceReferenceID, grant.EntitlementPeriodID, cleanNonEmpty(grant.PolicyVersion, "v1"), grant.StartsAt, nullableTime(grant.ExpiresAt), accountID.Bytes(), depositID.Bytes())
		if err != nil {
			return fmt.Errorf("insert deposit grant: %w", err)
		}
		return appendEvent(ctx, tx, c.queries.WithTx(tx), eventFact{EventType: "grant_issued", AggregateType: "credit_grant", AggregateID: grant.GrantID, OrgID: orgID, ProductID: grant.ScopeProductID, OccurredAt: grant.StartsAt, Payload: map[string]any{"grant_id": grant.GrantID, "source": grant.Source, "amount": firstUint64(grant.OriginalAmount, grant.Amount)}})
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

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func jsonMap(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
