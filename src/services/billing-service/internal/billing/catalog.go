package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/verself/billing-service/internal/store"
)

func (c *Client) EnsureOrg(ctx context.Context, orgID OrgID, displayName string, trustTier string) error {
	_, err := c.EnsureOrgRecord(ctx, orgID, displayName, trustTier)
	return err
}

func (c *Client) EnsureOrgRecord(ctx context.Context, orgID OrgID, displayName string, trustTier string) (OrgRecord, error) {
	if trustTier == "" {
		trustTier = "new"
	}
	if err := validateTrustTier(trustTier); err != nil {
		return OrgRecord{}, err
	}
	var record OrgRecord
	if err := c.WithTx(ctx, "billing.org.ensure", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		row, err := q.UpsertOrgReturning(ctx, store.UpsertOrgReturningParams{
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
		record = orgRecordFromFields(row.OrgID, row.DisplayName, row.State, row.TrustTier)
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "billing.organization.ensured",
			AggregateType: "organization",
			AggregateID:   row.OrgID,
			OrgID:         OrgID(row.OrgID),
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"display_name": row.DisplayName, "trust_tier": row.TrustTier},
		})
	}); err != nil {
		return OrgRecord{}, err
	}
	products, err := c.queries.ListProductIDs(ctx)
	if err != nil {
		return OrgRecord{}, fmt.Errorf("list products for org bootstrap: %w", err)
	}
	for _, productID := range products {
		if err := c.EnsureCurrentEntitlements(ctx, orgID, productID); err != nil {
			return OrgRecord{}, err
		}
		if _, err := c.EnsureOpenBillingCycle(ctx, orgID, productID); err != nil {
			return OrgRecord{}, err
		}
	}
	return record, nil
}

func (c *Client) SetOrganizationTrustTier(ctx context.Context, orgID OrgID, trustTier string) (OrgRecord, error) {
	if err := validateTrustTier(trustTier); err != nil {
		return OrgRecord{}, err
	}
	var record OrgRecord
	if err := c.WithTx(ctx, "billing.org.trust_tier.set", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		row, err := q.SetOrgTrustTier(ctx, store.SetOrgTrustTierParams{OrgID: orgIDText(orgID), TrustTier: trustTier})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrgNotFound
		}
		if err != nil {
			return fmt.Errorf("set org trust tier: %w", err)
		}
		record = orgRecordFromFields(row.OrgID, row.DisplayName, row.State, row.TrustTier)
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "billing.organization.trust_tier_set",
			AggregateType: "organization",
			AggregateID:   row.OrgID,
			OrgID:         OrgID(row.OrgID),
			OccurredAt:    time.Now().UTC(),
			Payload:       map[string]any{"trust_tier": row.TrustTier},
		})
	}); err != nil {
		return OrgRecord{}, err
	}
	return record, nil
}

func (c *Client) ApplyPlanPromotion(ctx context.Context, orgID OrgID, productID string, planID string, percentOff int, reason string) (PlanPromotionRecord, error) {
	productID = strings.TrimSpace(productID)
	planID = strings.TrimSpace(planID)
	reason = strings.TrimSpace(reason)
	if productID == "" || planID == "" || reason == "" {
		return PlanPromotionRecord{}, fmt.Errorf("product_id, plan_id, and reason are required")
	}
	if percentOff != 100 {
		return PlanPromotionRecord{}, fmt.Errorf("%w: only 100 percent off internal promotions are supported", ErrUnsupportedChange)
	}
	if _, err := c.queries.GetOrg(ctx, store.GetOrgParams{OrgID: orgIDText(orgID)}); errors.Is(err, pgx.ErrNoRows) {
		return PlanPromotionRecord{}, ErrOrgNotFound
	} else if err != nil {
		return PlanPromotionRecord{}, fmt.Errorf("load org: %w", err)
	}
	plan, err := c.loadPlan(ctx, planID)
	if err != nil {
		return PlanPromotionRecord{}, err
	}
	if plan.ProductID != productID {
		return PlanPromotionRecord{}, fmt.Errorf("%w: plan product mismatch", ErrUnsupportedChange)
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, productID)
	if err != nil {
		return PlanPromotionRecord{}, err
	}
	contractID := contractID(orgID, productID)
	phaseID := internalPromotionPhaseID(contractID, planID)
	activated, err := c.activateInternalPromotionalContract(ctx, orgID, productID, planID, contractID, phaseID, now, now)
	if err != nil {
		return PlanPromotionRecord{}, err
	}
	record := PlanPromotionRecord{OrgID: orgID, ProductID: productID, PlanID: planID, ContractID: contractID, PercentOff: percentOff, Reason: reason}
	if !activated {
		return record, nil
	}
	if err := c.WithTx(ctx, "billing.plan_promotion.event", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "billing.plan_promotion.applied",
			AggregateType: "contract",
			AggregateID:   contractID,
			OrgID:         orgID,
			ProductID:     productID,
			OccurredAt:    now,
			Payload:       map[string]any{"contract_id": contractID, "pricing_phase_id": phaseID, "pricing_plan_id": planID, "percent_off": percentOff, "reason": reason},
		})
	}); err != nil {
		return PlanPromotionRecord{}, err
	}
	return record, nil
}

func (c *Client) CancelPlanPromotion(ctx context.Context, orgID OrgID, productID string, reason string) (PlanPromotionCancellationRecord, error) {
	productID = strings.TrimSpace(productID)
	reason = strings.TrimSpace(reason)
	if productID == "" || reason == "" {
		return PlanPromotionCancellationRecord{}, fmt.Errorf("product_id and reason are required")
	}
	if _, err := c.queries.GetOrg(ctx, store.GetOrgParams{OrgID: orgIDText(orgID)}); errors.Is(err, pgx.ErrNoRows) {
		return PlanPromotionCancellationRecord{}, ErrOrgNotFound
	} else if err != nil {
		return PlanPromotionCancellationRecord{}, fmt.Errorf("load org: %w", err)
	}
	contractID := contractID(orgID, productID)
	kind, err := c.queries.GetContractKind(ctx, store.GetContractKindParams{ContractID: contractID, OrgID: orgIDText(orgID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanPromotionCancellationRecord{}, ErrContractNotFound
	}
	if err != nil {
		return PlanPromotionCancellationRecord{}, fmt.Errorf("load contract kind: %w", err)
	}
	if kind != "internal" {
		return PlanPromotionCancellationRecord{}, fmt.Errorf("%w: contract %s is %s", ErrUnsupportedChange, contractID, kind)
	}
	contract, err := c.CancelContract(ctx, orgID, contractID)
	if err != nil {
		return PlanPromotionCancellationRecord{}, err
	}
	record := PlanPromotionCancellationRecord{OrgID: orgID, ProductID: productID, ContractID: contractID, CancelAt: contract.PendingChangeEffectiveAt, Reason: reason}
	now, err := c.BusinessNow(ctx, c.queries, orgID, productID)
	if err != nil {
		return PlanPromotionCancellationRecord{}, err
	}
	if err := c.WithTx(ctx, "billing.plan_promotion.cancel_event", func(ctx context.Context, tx pgx.Tx, q *store.Queries) error {
		payload := map[string]any{"contract_id": contractID, "reason": reason}
		if record.CancelAt != nil {
			payload["cancel_at"] = record.CancelAt.Format(time.RFC3339Nano)
		}
		return appendEvent(ctx, tx, q, eventFact{
			EventType:     "billing.plan_promotion.canceled",
			AggregateType: "contract",
			AggregateID:   contractID,
			OrgID:         orgID,
			ProductID:     productID,
			OccurredAt:    now,
			Payload:       payload,
		})
	}); err != nil {
		return PlanPromotionCancellationRecord{}, err
	}
	return record, nil
}

func ensureDefaultOrgTx(ctx context.Context, q *store.Queries, orgID OrgID) error {
	if orgIDText(orgID) == "" {
		return fmt.Errorf("org_id is required")
	}
	if err := q.InsertDefaultOrg(ctx, store.InsertDefaultOrgParams{OrgID: orgIDText(orgID), DisplayName: "Org " + orgIDText(orgID)}); err != nil {
		return fmt.Errorf("bootstrap org: %w", err)
	}
	return nil
}

func validateTrustTier(value string) error {
	switch strings.TrimSpace(value) {
	case "new", "established", "enterprise", "platform":
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedTrustTier, value)
	}
}

func orgRecordFromFields(orgID string, displayName string, state string, trustTier string) OrgRecord {
	return OrgRecord{OrgID: OrgID(orgID), DisplayName: displayName, State: state, TrustTier: trustTier}
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
			MonthlyAmountCents: checkedUint64FromInt64(row.MonthlyAmountCents, "monthly amount cents"),
			AnnualAmountCents:  checkedUint64FromInt64(row.AnnualAmountCents, "annual amount cents"),
			Active:             row.Active,
			IsDefault:          row.IsDefault,
		})
	}
	return out, nil
}

func (c *Client) ListContracts(ctx context.Context, orgID OrgID) ([]ContractRecord, error) {
	productIDs, err := c.queries.ListContractProductIDs(ctx, store.ListContractProductIDsParams{OrgID: orgIDText(orgID)})
	if err != nil {
		return nil, fmt.Errorf("list contract products: %w", err)
	}
	for _, productID := range productIDs {
		if _, err := c.ApplyDueBillingWork(ctx, orgID, productID); err != nil {
			return nil, err
		}
	}
	rows, err := c.queries.ListContractsForOrg(ctx, store.ListContractsForOrgParams{OrgID: orgIDText(orgID)})
	if err != nil {
		return nil, fmt.Errorf("list contracts: %w", err)
	}
	out := []ContractRecord{}
	for _, row := range rows {
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
	return out, nil
}
