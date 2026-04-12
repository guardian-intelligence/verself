package billing

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"strconv"
	"time"
)

const (
	entitlementReasonCurrent = "current_period_reconcile"
	entitlementReasonNext    = "next_period_reconcile"
)

func (c *Client) EnsureCurrentEntitlements(ctx context.Context, orgID OrgID, productID string) error {
	return c.ensureCalendarFreeTierEntitlements(ctx, orgID, productID, c.clock().UTC(), 0, entitlementReasonCurrent)
}

func (c *Client) EnsureNextEntitlements(ctx context.Context, orgID OrgID, productID string) error {
	return c.ensureCalendarFreeTierEntitlements(ctx, orgID, productID, c.clock().UTC(), 1, entitlementReasonNext)
}

func (c *Client) ReconcileEntitlements(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.pg.QueryContext(ctx, `
		SELECT org_id
		FROM orgs
		ORDER BY org_id
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("query entitlement orgs: %w", err)
	}
	defer rows.Close()

	reconciled := 0
	for rows.Next() {
		var orgIDText string
		if err := rows.Scan(&orgIDText); err != nil {
			return reconciled, fmt.Errorf("scan entitlement org: %w", err)
		}
		rawOrgID, err := strconv.ParseUint(orgIDText, 10, 64)
		if err != nil {
			return reconciled, fmt.Errorf("parse entitlement org id %q: %w", orgIDText, err)
		}
		orgID := OrgID(rawOrgID)
		if err := c.EnsureCurrentEntitlements(ctx, orgID, ""); err != nil {
			return reconciled, err
		}
		if err := c.EnsureNextEntitlements(ctx, orgID, ""); err != nil {
			return reconciled, err
		}
		reconciled++
	}
	if err := rows.Err(); err != nil {
		return reconciled, fmt.Errorf("iterate entitlement orgs: %w", err)
	}
	return reconciled, nil
}

func (c *Client) ensureCalendarFreeTierEntitlements(
	ctx context.Context,
	orgID OrgID,
	productID string,
	now time.Time,
	monthOffset int,
	reason string,
) error {
	orgCreatedAt, err := c.orgCreatedAt(ctx, orgID)
	if err != nil {
		return err
	}
	anchorStart, anchorEnd := calendarMonthWindow(now.UTC().AddDate(0, monthOffset, 0))
	policies, err := c.activeFreeTierPolicies(ctx, productID, anchorStart, anchorEnd)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		period, ok, err := entitlementPeriodForPolicy(orgID, policy, orgCreatedAt, anchorStart, anchorEnd, reason)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := c.ensureEntitlementPeriod(ctx, period); err != nil {
			return err
		}
		periodStart := period.PeriodStart
		periodEnd := period.PeriodEnd
		if _, err := c.IssueCreditGrant(ctx, CreditGrant{
			OrgID:               orgID,
			ScopeType:           period.ScopeType,
			ScopeProductID:      period.ScopeProductID,
			ScopeBucketID:       period.ScopeBucketID,
			ScopeSKUID:          period.ScopeSKUID,
			Amount:              period.AmountUnits,
			Source:              period.Source.String(),
			SourceReferenceID:   period.SourceReferenceID,
			EntitlementPeriodID: period.PeriodID,
			PolicyVersion:       period.PolicyVersion,
			StartsAt:            &periodStart,
			PeriodStart:         &periodStart,
			PeriodEnd:           &periodEnd,
			ExpiresAt:           &periodEnd,
		}); err != nil {
			return fmt.Errorf("issue free-tier grant for policy %s: %w", policy.PolicyID, err)
		}
	}
	return nil
}

func (c *Client) orgCreatedAt(ctx context.Context, orgID OrgID) (time.Time, error) {
	var createdAt time.Time
	if err := c.pg.QueryRowContext(ctx, `
		SELECT created_at FROM orgs WHERE org_id = $1
	`, strconv.FormatUint(uint64(orgID), 10)).Scan(&createdAt); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, fmt.Errorf("org %d is not known to billing", orgID)
		}
		return time.Time{}, fmt.Errorf("lookup org created_at: %w", err)
	}
	return createdAt.UTC(), nil
}

func (c *Client) activeFreeTierPolicies(ctx context.Context, productID string, periodStart, periodEnd time.Time) ([]EntitlementPolicy, error) {
	query := `
		SELECT policy_id, source, product_id, scope_type, scope_product_id, scope_bucket_id, scope_sku_id,
		       amount_units, cadence, anchor_kind, proration_mode, policy_version, active_from, active_until
		FROM entitlement_policies
		WHERE source = 'free_tier'
		  AND anchor_kind = 'calendar_month'
		  AND active_from < $1
		  AND (active_until IS NULL OR active_until > $2)
	`
	args := []any{periodEnd, periodStart}
	if productID != "" {
		query += " AND (product_id = '' OR product_id = $3)"
		args = append(args, productID)
	}
	query += " ORDER BY policy_id"

	rows, err := c.pg.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query active free-tier policies: %w", err)
	}
	defer rows.Close()

	var out []EntitlementPolicy
	for rows.Next() {
		var policy EntitlementPolicy
		var sourceText, scopeText string
		var amount int64
		var activeUntil sql.NullTime
		if err := rows.Scan(
			&policy.PolicyID,
			&sourceText,
			&policy.ProductID,
			&scopeText,
			&policy.ScopeProductID,
			&policy.ScopeBucketID,
			&policy.ScopeSKUID,
			&amount,
			&policy.Cadence,
			&policy.AnchorKind,
			&policy.ProrationMode,
			&policy.PolicyVersion,
			&policy.ActiveFrom,
			&activeUntil,
		); err != nil {
			return nil, fmt.Errorf("scan free-tier policy: %w", err)
		}
		source, err := ParseGrantSourceType(sourceText)
		if err != nil {
			return nil, err
		}
		scope, err := ParseGrantScopeType(scopeText)
		if err != nil {
			return nil, err
		}
		if amount < 0 {
			return nil, fmt.Errorf("policy %s has negative amount", policy.PolicyID)
		}
		policy.Source = source
		policy.ScopeType = scope
		policy.AmountUnits = uint64(amount)
		policy.ActiveFrom = policy.ActiveFrom.UTC()
		if activeUntil.Valid {
			value := activeUntil.Time.UTC()
			policy.ActiveUntil = &value
		}
		if err := validateGrantScope(policy.ScopeType, policy.ScopeProductID, policy.ScopeBucketID, policy.ScopeSKUID); err != nil {
			return nil, fmt.Errorf("policy %s: %w", policy.PolicyID, err)
		}
		out = append(out, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate free-tier policies: %w", err)
	}
	return out, nil
}

func entitlementPeriodForPolicy(
	orgID OrgID,
	policy EntitlementPolicy,
	orgCreatedAt time.Time,
	anchorStart time.Time,
	anchorEnd time.Time,
	reason string,
) (EntitlementPeriod, bool, error) {
	if policy.AmountUnits == 0 {
		return EntitlementPeriod{}, false, nil
	}
	periodStart := maxTime(anchorStart, orgCreatedAt.UTC(), policy.ActiveFrom.UTC())
	periodEnd := anchorEnd
	if policy.ActiveUntil != nil && policy.ActiveUntil.Before(periodEnd) {
		periodEnd = policy.ActiveUntil.UTC()
	}
	if !periodEnd.After(periodStart) {
		return EntitlementPeriod{}, false, nil
	}

	amount := policy.AmountUnits
	if policy.ProrationMode == ProrationByTimeLeft && periodStart.After(anchorStart) {
		amount = prorateUint64ByDuration(policy.AmountUnits, periodEnd.Sub(periodStart), anchorEnd.Sub(anchorStart))
	}
	if amount == 0 {
		return EntitlementPeriod{}, false, nil
	}

	sourceRef := entitlementSourceReference(policy, periodStart, periodEnd)
	return EntitlementPeriod{
		PeriodID:          entitlementPeriodID(orgID, policy, periodStart, periodEnd),
		OrgID:             orgID,
		ProductID:         policy.ProductID,
		Source:            policy.Source,
		PolicyID:          policy.PolicyID,
		ScopeType:         policy.ScopeType,
		ScopeProductID:    policy.ScopeProductID,
		ScopeBucketID:     policy.ScopeBucketID,
		ScopeSKUID:        policy.ScopeSKUID,
		AmountUnits:       amount,
		PeriodStart:       periodStart,
		PeriodEnd:         periodEnd,
		PolicyVersion:     policy.PolicyVersion,
		PaymentState:      PaymentNotRequired,
		EntitlementState:  EntitlementActive,
		SourceReferenceID: sourceRef,
		CreatedReason:     reason,
	}, true, nil
}

func subscriptionEntitlementPeriod(
	orgID OrgID,
	contractID string,
	policy EntitlementPolicy,
	periodStart time.Time,
	periodEnd time.Time,
	paymentState EntitlementPaymentState,
	entitlementState EntitlementState,
) (EntitlementPeriod, bool) {
	anchorStart := periodStart.UTC()
	anchorEnd := periodEnd.UTC()
	effectiveStart := maxTime(anchorStart, policy.ActiveFrom.UTC())
	effectiveEnd := anchorEnd
	if policy.ActiveUntil != nil && policy.ActiveUntil.Before(effectiveEnd) {
		effectiveEnd = policy.ActiveUntil.UTC()
	}
	if policy.AmountUnits == 0 || contractID == "" || !effectiveEnd.After(effectiveStart) {
		return EntitlementPeriod{}, false
	}
	amount := policy.AmountUnits
	if policy.ProrationMode == ProrationByTimeLeft {
		fullPeriodEnd := periodEndForCadence(anchorStart, policy.Cadence)
		fullPeriod := anchorEnd.Sub(anchorStart)
		if fullPeriodEnd.After(anchorStart) && anchorEnd.Before(fullPeriodEnd) {
			fullPeriod = fullPeriodEnd.Sub(anchorStart)
		}
		if effectiveStart.After(anchorStart) || effectiveEnd.Before(anchorEnd) {
			amount = prorateUint64ByDuration(policy.AmountUnits, effectiveEnd.Sub(effectiveStart), fullPeriod)
		}
	}
	if amount == 0 {
		return EntitlementPeriod{}, false
	}
	sourceRef := subscriptionEntitlementSourceReference(contractID, policy, effectiveStart, effectiveEnd)
	return EntitlementPeriod{
		PeriodID:          subscriptionEntitlementPeriodID(orgID, contractID, policy, effectiveStart, effectiveEnd),
		OrgID:             orgID,
		ProductID:         policy.ProductID,
		Source:            policy.Source,
		PolicyID:          policy.PolicyID,
		ContractID:        contractID,
		ScopeType:         policy.ScopeType,
		ScopeProductID:    policy.ScopeProductID,
		ScopeBucketID:     policy.ScopeBucketID,
		ScopeSKUID:        policy.ScopeSKUID,
		AmountUnits:       amount,
		PeriodStart:       effectiveStart,
		PeriodEnd:         effectiveEnd,
		PolicyVersion:     policy.PolicyVersion,
		PaymentState:      paymentState,
		EntitlementState:  entitlementState,
		SourceReferenceID: sourceRef,
		CreatedReason:     "subscription_period_reconcile",
	}, true
}

func (c *Client) ensureEntitlementPeriod(ctx context.Context, period EntitlementPeriod) error {
	if period.PeriodID == "" {
		return fmt.Errorf("entitlement period id is required")
	}
	_, err := c.pg.ExecContext(ctx, `
		INSERT INTO entitlement_periods (
			period_id, org_id, product_id, source, policy_id, contract_id,
			scope_type, scope_product_id, scope_bucket_id, scope_sku_id, amount_units,
			period_start, period_end, policy_version, payment_state, entitlement_state,
			source_reference_id, created_reason
		)
		VALUES ($1, $2, $3, $4, $5, $6,
		        $7, $8, $9, $10, $11,
		        $12, $13, $14, $15, $16,
		        $17, $18)
		ON CONFLICT (period_id) DO UPDATE
		SET payment_state = EXCLUDED.payment_state,
		    entitlement_state = EXCLUDED.entitlement_state,
		    updated_at = now()
	`, period.PeriodID, strconv.FormatUint(uint64(period.OrgID), 10), period.ProductID, period.Source.String(), period.PolicyID, period.ContractID,
		period.ScopeType.String(), period.ScopeProductID, period.ScopeBucketID, period.ScopeSKUID, period.AmountUnits,
		period.PeriodStart, period.PeriodEnd, period.PolicyVersion, string(period.PaymentState), string(period.EntitlementState),
		period.SourceReferenceID, period.CreatedReason)
	if err != nil {
		return fmt.Errorf("ensure entitlement period %s: %w", period.PeriodID, err)
	}
	return nil
}

func entitlementPeriodID(orgID OrgID, policy EntitlementPolicy, periodStart time.Time, periodEnd time.Time) string {
	return deterministicTextID(
		"entitlement-period",
		strconv.FormatUint(uint64(orgID), 10),
		policy.Source.String(),
		policy.PolicyID,
		policy.PolicyVersion,
		policy.ScopeType.String(),
		policy.ScopeProductID,
		policy.ScopeBucketID,
		policy.ScopeSKUID,
		periodStart.UTC().Format(time.RFC3339Nano),
		periodEnd.UTC().Format(time.RFC3339Nano),
	)
}

func subscriptionEntitlementPeriodID(orgID OrgID, contractID string, policy EntitlementPolicy, periodStart time.Time, periodEnd time.Time) string {
	return deterministicTextID(
		"subscription-entitlement-period",
		strconv.FormatUint(uint64(orgID), 10),
		contractID,
		policy.Source.String(),
		policy.PolicyID,
		policy.PolicyVersion,
		policy.ScopeType.String(),
		policy.ScopeProductID,
		policy.ScopeBucketID,
		policy.ScopeSKUID,
		periodStart.UTC().Format(time.RFC3339Nano),
		periodEnd.UTC().Format(time.RFC3339Nano),
	)
}

func entitlementSourceReference(policy EntitlementPolicy, periodStart time.Time, periodEnd time.Time) string {
	return policy.Source.String() + ":" + policy.PolicyID + ":" + policy.PolicyVersion + ":" +
		periodStart.UTC().Format(time.RFC3339Nano) + ":" + periodEnd.UTC().Format(time.RFC3339Nano)
}

func subscriptionEntitlementSourceReference(contractID string, policy EntitlementPolicy, periodStart time.Time, periodEnd time.Time) string {
	return policy.Source.String() + ":" + contractID + ":" + policy.PolicyID + ":" + policy.PolicyVersion + ":" +
		periodStart.UTC().Format(time.RFC3339Nano) + ":" + periodEnd.UTC().Format(time.RFC3339Nano)
}

func calendarMonthWindow(value time.Time) (time.Time, time.Time) {
	utc := value.UTC()
	start := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func periodEndForCadence(start time.Time, cadence EntitlementCadence) time.Time {
	switch cadence {
	case EntitlementCadenceAnnual:
		return start.UTC().AddDate(1, 0, 0)
	default:
		return start.UTC().AddDate(0, 1, 0)
	}
}

func stripeContractID(stripeSubscriptionID string) string {
	if stripeSubscriptionID == "" {
		return ""
	}
	return deterministicTextID("stripe-subscription-contract", stripeSubscriptionID)
}

func graceUntil(periodEnd *time.Time, grace time.Duration) *time.Time {
	if periodEnd == nil || grace <= 0 {
		return nil
	}
	value := periodEnd.UTC().Add(grace)
	return &value
}

func maxTime(values ...time.Time) time.Time {
	var out time.Time
	for _, value := range values {
		if out.IsZero() || value.After(out) {
			out = value
		}
	}
	return out
}

func prorateUint64ByDuration(amount uint64, numerator time.Duration, denominator time.Duration) uint64 {
	if amount == 0 || numerator <= 0 || denominator <= 0 {
		return 0
	}
	if numerator >= denominator {
		return amount
	}

	num := new(big.Int).SetUint64(amount)
	num.Mul(num, big.NewInt(int64(numerator)))
	den := big.NewInt(int64(denominator))
	quotient, remainder := new(big.Int).QuoRem(num, den, new(big.Int))
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsUint64() {
		return amount
	}
	return quotient.Uint64()
}
