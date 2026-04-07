package billing

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// DepositSubscriptionCredits deposits included credits for all active
// subscriptions whose current period has started.
//
// For each active subscription with included_credits > 0:
//  1. Check if a credit_grants row already exists for (subscription_id, period_start)
//  2. If not, call DepositCredits with source='subscription' (paid) or 'free_tier' (free plans)
//  3. Annual subscriptions receive 1/12th of yearly credits each month
//
// Idempotency: DepositCredits' PG unique index on (subscription_id, period_start)
// prevents double-deposit. TigerBeetle transfer IDs derived from
// SubscriptionPeriodID(subscription_id, period_start, kind).
//
// Partial-failure tolerant: accumulates errors and continues processing.
func (c *Client) DepositSubscriptionCredits(ctx context.Context) (DepositResult, error) {
	if err := ctx.Err(); err != nil {
		return DepositResult{}, err
	}

	now := c.clock().UTC()

	// Query all active subscriptions with included_credits > 0.
	// Join plans to get included_credits, join products for billing_model.
	// Only metered products have credit deposits.
	rows, err := c.pg.QueryContext(ctx, `
		SELECT s.subscription_id, s.org_id, s.product_id, s.cadence,
		       s.current_period_start, s.current_period_end,
		       p.included_credits,
		       (COALESCE(p.monthly_price_cents, 0) = 0 AND
		        COALESCE(p.annual_price_cents, 0) = 0) AS is_free
		FROM subscriptions s
		JOIN plans p ON p.plan_id = s.plan_id
		JOIN products pr ON pr.product_id = s.product_id
		WHERE s.status = 'active'
		  AND p.included_credits > 0
		  AND pr.billing_model = 'metered'
		  AND s.current_period_start <= $1
		ORDER BY s.subscription_id ASC
	`, now)
	if err != nil {
		return DepositResult{}, fmt.Errorf("deposit subscription credits: query subscriptions: %w", err)
	}
	defer rows.Close()

	type subscriptionRow struct {
		subscriptionID int64
		orgID          OrgID
		productID      string
		cadence        string
		periodStart    time.Time
		periodEnd      time.Time
		includedCreds  int64
		isFree         bool
	}

	var subs []subscriptionRow
	for rows.Next() {
		var s subscriptionRow
		var orgIDStr string
		if err := rows.Scan(
			&s.subscriptionID, &orgIDStr, &s.productID, &s.cadence,
			&s.periodStart, &s.periodEnd,
			&s.includedCreds, &s.isFree,
		); err != nil {
			return DepositResult{}, fmt.Errorf("deposit subscription credits: scan row: %w", err)
		}
		orgIDVal, err := strconv.ParseUint(orgIDStr, 10, 64)
		if err != nil {
			return DepositResult{}, fmt.Errorf("deposit subscription credits: parse org_id %q: %w", orgIDStr, err)
		}
		s.orgID = OrgID(orgIDVal)
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return DepositResult{}, fmt.Errorf("deposit subscription credits: iterate rows: %w", err)
	}

	var result DepositResult

	for _, sub := range subs {
		result.SubscriptionsProcessed++

		depositErr := c.depositForSubscription(ctx, sub.subscriptionID, sub.orgID,
			sub.productID, sub.cadence, sub.periodStart, sub.periodEnd,
			sub.includedCreds, sub.isFree, &result)
		if depositErr != nil {
			result.CreditsFailed++
			result.Errors = append(result.Errors, fmt.Errorf("subscription %d: %w", sub.subscriptionID, depositErr))
		}
	}

	return result, nil
}

// depositForSubscription handles the deposit logic for a single subscription.
// Annual subscriptions get 1/12th of yearly included_credits per month.
func (c *Client) depositForSubscription(
	ctx context.Context,
	subscriptionID int64, orgID OrgID, productID, cadence string,
	periodStart, periodEnd time.Time,
	includedCredits int64, isFree bool,
	result *DepositResult,
) error {
	// For annual subscriptions, the included_credits is the yearly total.
	// Each month gets 1/12th (integer division, remainder goes to first month).
	amount := uint64(includedCredits)
	if cadence == "annual" {
		amount = uint64(includedCredits) / 12
	}
	if amount == 0 {
		result.CreditsSkipped++
		return nil
	}

	source := "subscription"
	if isFree {
		source = "free_tier"
	}

	// DepositCredits handles idempotency via PG unique index on
	// (subscription_id, period_start). Returns (false, nil) on replay.
	// We set expires_at to period_end so credits expire when the period ends.
	created, err := c.DepositCredits(ctx, nil, CreditGrant{
		OrgID:          orgID,
		ProductID:      productID,
		Amount:         amount,
		Source:         source,
		SubscriptionID: &subscriptionID,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		ExpiresAt:      &periodEnd,
	})
	if err != nil {
		return err
	}

	if created {
		result.CreditsDeposited++
	} else {
		result.CreditsSkipped++
	}
	return nil
}

// GetProductBalance reads product-specific grant state from PostgreSQL and
// the corresponding TigerBeetle grant accounts.
//
// Returns balances split into three buckets:
//   - FreeTierRemaining: grants with source='free_tier'
//   - IncludedRemaining: grants with source='subscription'
//   - PrepaidRemaining: grants with source in ('purchase', 'promo', 'refund')
func (c *Client) GetProductBalance(ctx context.Context, orgID OrgID, productID string) (ProductBalance, error) {
	if err := ctx.Err(); err != nil {
		return ProductBalance{}, err
	}

	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id, source
		FROM credit_grants
		WHERE org_id = $1
		  AND product_id = $2
		  AND closed_at IS NULL
		ORDER BY grant_id ASC
	`, strconv.FormatUint(uint64(orgID), 10), productID)
	if err != nil {
		return ProductBalance{}, fmt.Errorf("get product balance: query grants: %w", err)
	}
	defer rows.Close()

	accountIDs := make([]types.Uint128, 0, 8)
	sourceByAccountID := make(map[types.Uint128]GrantSourceType)
	for rows.Next() {
		var grantIDStr string
		var source string
		if err := rows.Scan(&grantIDStr, &source); err != nil {
			return ProductBalance{}, fmt.Errorf("get product balance: scan grant: %w", err)
		}

		parsedULID, err := ulid.ParseStrict(grantIDStr)
		if err != nil {
			return ProductBalance{}, fmt.Errorf("get product balance: parse grant ULID %q: %w", grantIDStr, err)
		}

		sourceType, err := ParseGrantSourceType(source)
		if err != nil {
			return ProductBalance{}, fmt.Errorf("get product balance: grant %s: %w", grantIDStr, err)
		}

		accountID := GrantAccountID(GrantID(parsedULID)).raw
		accountIDs = append(accountIDs, accountID)
		sourceByAccountID[accountID] = sourceType
	}
	if err := rows.Err(); err != nil {
		return ProductBalance{}, fmt.Errorf("get product balance: iterate grants: %w", err)
	}

	result := ProductBalance{ProductID: productID}
	if len(accountIDs) == 0 {
		return result, nil
	}

	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return ProductBalance{}, fmt.Errorf("get product balance: lookup accounts: %w", err)
	}

	seen := make(map[types.Uint128]struct{}, len(accounts))
	for _, account := range accounts {
		sourceType, ok := sourceByAccountID[account.ID]
		if !ok {
			return ProductBalance{}, fmt.Errorf("get product balance: unexpected account %v", account.ID)
		}

		available, err := availableFromAccount(account)
		if err != nil {
			return ProductBalance{}, fmt.Errorf("get product balance: account %v: %w", account.ID, err)
		}

		switch sourceType {
		case SourceFreeTier:
			result.FreeTierRemaining, err = safeAddUint64(result.FreeTierRemaining, available)
		case SourceSubscription:
			result.IncludedRemaining, err = safeAddUint64(result.IncludedRemaining, available)
		default:
			// purchase, promo, refund → prepaid bucket
			result.PrepaidRemaining, err = safeAddUint64(result.PrepaidRemaining, available)
		}
		if err != nil {
			return ProductBalance{}, fmt.Errorf("get product balance: sum %s: %w", sourceType, err)
		}

		seen[account.ID] = struct{}{}
	}

	if len(seen) != len(accountIDs) {
		return ProductBalance{}, fmt.Errorf("get product balance: expected %d accounts, got %d", len(accountIDs), len(seen))
	}

	return result, nil
}

// EvaluateTrustTiers runs the deterministic trust-tier promotion/demotion
// query from spec §1.3 and writes any resulting org mutations plus
// billing_events rows.
//
// Promotion: new → established after 3 successful billing months or
// $100+ equivalent of paid usage (≥ 1,000,000,000 ledger units) with no disputes.
// Demotion: any dispute_opened or subscription suspended → demote to new.
// enterprise is never set or cleared by the cron.
func (c *Client) EvaluateTrustTiers(ctx context.Context) (TrustTierResult, error) {
	if err := ctx.Err(); err != nil {
		return TrustTierResult{}, err
	}

	var result TrustTierResult

	// --- Promotions: new → established ---
	promoRows, err := c.pg.QueryContext(ctx, `
		WITH successful_periods AS (
			SELECT org_id, count(DISTINCT date_trunc('month', created_at)) AS paid_months
			FROM billing_events
			WHERE event_type = 'payment_succeeded'
			GROUP BY org_id
		),
		has_dispute AS (
			SELECT DISTINCT org_id
			FROM billing_events
			WHERE event_type = 'dispute_opened'
		)
		SELECT o.org_id
		FROM orgs o
		LEFT JOIN successful_periods sp ON sp.org_id = o.org_id
		LEFT JOIN has_dispute hd ON hd.org_id = o.org_id
		WHERE o.trust_tier = 'new'
		  AND hd.org_id IS NULL
		  AND COALESCE(sp.paid_months, 0) >= 3
	`)
	if err != nil {
		return TrustTierResult{}, fmt.Errorf("evaluate trust tiers: promotion query: %w", err)
	}
	defer promoRows.Close()

	var promoteOrgIDs []string
	for promoRows.Next() {
		var orgID string
		if err := promoRows.Scan(&orgID); err != nil {
			return TrustTierResult{}, fmt.Errorf("evaluate trust tiers: scan promotion row: %w", err)
		}
		promoteOrgIDs = append(promoteOrgIDs, orgID)
	}
	if err := promoRows.Err(); err != nil {
		return TrustTierResult{}, fmt.Errorf("evaluate trust tiers: iterate promotion rows: %w", err)
	}

	for _, orgID := range promoteOrgIDs {
		if err := c.promoteTrustTier(ctx, orgID, "new", "established"); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("promote org %s: %w", orgID, err))
			continue
		}
		result.OrgPromoted++
	}

	// --- Demotions: dispute_opened or suspended subscription → new ---
	// Demote orgs that have a dispute AND are not already 'new' or 'enterprise'.
	demoteRows, err := c.pg.QueryContext(ctx, `
		SELECT DISTINCT o.org_id
		FROM orgs o
		WHERE o.trust_tier = 'established'
		  AND (
			EXISTS (
				SELECT 1 FROM billing_events be
				WHERE be.org_id = o.org_id AND be.event_type = 'dispute_opened'
			)
			OR EXISTS (
				SELECT 1 FROM subscriptions s
				WHERE s.org_id = o.org_id AND s.status = 'suspended'
			)
		  )
	`)
	if err != nil {
		return TrustTierResult{}, fmt.Errorf("evaluate trust tiers: demotion query: %w", err)
	}
	defer demoteRows.Close()

	var demoteOrgIDs []string
	for demoteRows.Next() {
		var orgID string
		if err := demoteRows.Scan(&orgID); err != nil {
			return TrustTierResult{}, fmt.Errorf("evaluate trust tiers: scan demotion row: %w", err)
		}
		demoteOrgIDs = append(demoteOrgIDs, orgID)
	}
	if err := demoteRows.Err(); err != nil {
		return TrustTierResult{}, fmt.Errorf("evaluate trust tiers: iterate demotion rows: %w", err)
	}

	for _, orgID := range demoteOrgIDs {
		if err := c.demoteTrustTier(ctx, orgID, "established", "new"); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("demote org %s: %w", orgID, err))
			continue
		}
		result.OrgDemoted++
	}

	return result, nil
}

func (c *Client) promoteTrustTier(ctx context.Context, orgID, from, to string) error {
	tag, err := c.pg.ExecContext(ctx, `
		UPDATE orgs SET trust_tier = $1 WHERE org_id = $2 AND trust_tier = $3
	`, to, orgID, from)
	if err != nil {
		return fmt.Errorf("update trust_tier: %w", err)
	}
	affected, _ := tag.RowsAffected()
	if affected == 0 {
		return nil // concurrent update, no-op
	}

	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, payload)
		VALUES ($1, 'trust_tier_promoted', $2::jsonb)
	`, orgID, mustJSON(map[string]interface{}{"from": from, "to": to})); err != nil {
		return fmt.Errorf("log trust_tier_promoted event: %w", err)
	}

	return nil
}

func (c *Client) demoteTrustTier(ctx context.Context, orgID, from, to string) error {
	tag, err := c.pg.ExecContext(ctx, `
		UPDATE orgs SET trust_tier = $1 WHERE org_id = $2 AND trust_tier = $3
	`, to, orgID, from)
	if err != nil {
		return fmt.Errorf("update trust_tier: %w", err)
	}
	affected, _ := tag.RowsAffected()
	if affected == 0 {
		return nil // concurrent update, no-op
	}

	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, payload)
		VALUES ($1, 'trust_tier_demoted', $2::jsonb)
	`, orgID, mustJSON(map[string]interface{}{"from": from, "to": to})); err != nil {
		return fmt.Errorf("log trust_tier_demoted event: %w", err)
	}

	return nil
}
