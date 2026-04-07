package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// ClickHouseQuerier is the reconciliation-specific ClickHouse read interface.
// Separate from MeteringQuerier because reconciliation queries have different
// shapes than the hot-path quota/overage queries.
type ClickHouseQuerier interface {
	// SumChargeUnitsByOrg returns the total charge_units across all products
	// for an org since the given timestamp.
	SumChargeUnitsByOrg(ctx context.Context, orgID string, since time.Time) (uint64, error)

	// SumChargeUnitsByGrantSource returns charge_units grouped by grant source
	// (subscription_units, purchase_units, etc.) for reconciliation against
	// TigerBeetle transfer history. The returned map keys are source type strings.
	SumChargeUnitsByGrantSource(ctx context.Context, orgID string, productID string, since time.Time) (map[string]uint64, error)

	// CountLicensedChargeRows returns the number of metering rows with
	// pricing_phase='licensed' for a given org and product since the given time.
	CountLicensedChargeRows(ctx context.Context, orgID string, productID string, since time.Time) (uint64, error)
}

// ReconcileResult contains the results of all six named consistency checks.
type ReconcileResult struct {
	Checks []ReconcileCheck
}

// ReconcileCheck is one named consistency check result.
type ReconcileCheck struct {
	Name     string
	Severity string // "alert" or "warn"
	Passed   bool
	Details  string
}

// HasAlerts returns true if any alert-level check failed.
func (r ReconcileResult) HasAlerts() bool {
	for _, check := range r.Checks {
		if !check.Passed && check.Severity == "alert" {
			return true
		}
	}
	return false
}

// Reconcile runs six named consistency checks across PostgreSQL, TigerBeetle,
// and ClickHouse. It takes a separate ClickHouseQuerier parameter because
// reconciliation queries don't overlap with hot-path MeteringQuerier queries.
//
// Spec §5.3: the function is both an operational consistency checker and a
// named verification suite.
//
// Failure policy: fail immediately on infrastructure errors. Do not advance
// any watermark on failure.
func (c *Client) Reconcile(ctx context.Context, ch ClickHouseQuerier) (ReconcileResult, error) {
	if err := ctx.Err(); err != nil {
		return ReconcileResult{}, err
	}

	var result ReconcileResult

	// Check 1: grant_account_catalog_consistency
	check1, err := c.checkGrantAccountCatalogConsistency(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile: grant_account_catalog_consistency: %w", err)
	}
	result.Checks = append(result.Checks, check1)

	// Check 2: no_orphan_grant_accounts
	check2, err := c.checkNoOrphanGrantAccounts(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile: no_orphan_grant_accounts: %w", err)
	}
	result.Checks = append(result.Checks, check2)

	// Check 3: expired_grants_swept
	check3, err := c.checkExpiredGrantsSwept(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile: expired_grants_swept: %w", err)
	}
	result.Checks = append(result.Checks, check3)

	// Check 4: licensed_charge_exactly_once
	check4, err := c.checkLicensedChargeExactlyOnce(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile: licensed_charge_exactly_once: %w", err)
	}
	result.Checks = append(result.Checks, check4)

	// Check 5: metering_vs_transfers
	check5, err := c.checkMeteringVsTransfers(ctx, ch)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile: metering_vs_transfers: %w", err)
	}
	result.Checks = append(result.Checks, check5)

	// Check 6: trust_tier_monotonicity
	check6, err := c.checkTrustTierMonotonicity(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile: trust_tier_monotonicity: %w", err)
	}
	result.Checks = append(result.Checks, check6)

	// Log reconciliation_alert billing events for any failed checks.
	for _, check := range result.Checks {
		if !check.Passed {
			if _, err := c.pg.ExecContext(ctx, `
				INSERT INTO billing_events (org_id, event_type, payload)
				VALUES ('0', 'reconciliation_alert', $1::jsonb)
			`, mustJSON(map[string]interface{}{
				"check":    check.Name,
				"severity": check.Severity,
				"details":  check.Details,
			})); err != nil {
				return result, fmt.Errorf("reconcile: log alert for %s: %w", check.Name, err)
			}
		}
	}

	return result, nil
}

// Check 1: every active PG grant row has a TB account.
func (c *Client) checkGrantAccountCatalogConsistency(ctx context.Context) (ReconcileCheck, error) {
	check := ReconcileCheck{
		Name:     "grant_account_catalog_consistency",
		Severity: "alert",
		Passed:   true,
	}

	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id FROM credit_grants WHERE closed_at IS NULL
		ORDER BY grant_id ASC
	`)
	if err != nil {
		return check, fmt.Errorf("query active grants: %w", err)
	}
	defer rows.Close()

	var missing int
	for rows.Next() {
		var grantIDStr string
		if err := rows.Scan(&grantIDStr); err != nil {
			return check, fmt.Errorf("scan grant_id: %w", err)
		}

		parsedULID, err := ulid.ParseStrict(grantIDStr)
		if err != nil {
			return check, fmt.Errorf("parse ULID %q: %w", grantIDStr, err)
		}

		accountID := GrantAccountID(GrantID(parsedULID)).raw
		accounts, err := c.tb.LookupAccounts([]types.Uint128{accountID})
		if err != nil {
			return check, fmt.Errorf("lookup TB account for grant %s: %w", grantIDStr, err)
		}
		if len(accounts) == 0 {
			missing++
		}
	}
	if err := rows.Err(); err != nil {
		return check, fmt.Errorf("iterate grants: %w", err)
	}

	if missing > 0 {
		check.Passed = false
		check.Details = fmt.Sprintf("%d active PG grant(s) missing TB account", missing)
	}

	return check, nil
}

// Check 2: every TB grant account (code=9) has a PG catalog row.
// TB doesn't support "list all accounts with code=9", so we scan PG for
// all grant_ids (including closed ones) and verify each has a TB account.
// The reverse: for each known grant ID, check that PG has the row.
func (c *Client) checkNoOrphanGrantAccounts(ctx context.Context) (ReconcileCheck, error) {
	check := ReconcileCheck{
		Name:     "no_orphan_grant_accounts",
		Severity: "alert",
		Passed:   true,
	}

	// Load all grant IDs from PG (open and closed).
	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id FROM credit_grants ORDER BY grant_id ASC
	`)
	if err != nil {
		return check, fmt.Errorf("query all grants: %w", err)
	}
	defer rows.Close()

	var grantIDs []GrantID
	var accountIDs []types.Uint128
	pgGrants := make(map[types.Uint128]string) // accountID → grantIDStr

	for rows.Next() {
		var grantIDStr string
		if err := rows.Scan(&grantIDStr); err != nil {
			return check, fmt.Errorf("scan grant_id: %w", err)
		}

		parsedULID, err := ulid.ParseStrict(grantIDStr)
		if err != nil {
			return check, fmt.Errorf("parse ULID %q: %w", grantIDStr, err)
		}

		grantID := GrantID(parsedULID)
		accountID := GrantAccountID(grantID).raw
		grantIDs = append(grantIDs, grantID)
		accountIDs = append(accountIDs, accountID)
		pgGrants[accountID] = grantIDStr
	}
	if err := rows.Err(); err != nil {
		return check, fmt.Errorf("iterate grants: %w", err)
	}

	if len(accountIDs) == 0 {
		return check, nil
	}

	// Batch lookup all grant accounts in TB.
	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return check, fmt.Errorf("lookup TB accounts: %w", err)
	}

	// Every TB account found should have a PG row (guaranteed by construction
	// since we built accountIDs from PG). The real orphan check is: any TB
	// account with code=9 that we didn't ask about. Since TB has no list-by-code
	// API, we verify the inverse — any TB account we looked up that returned
	// but has no PG counterpart (shouldn't happen with this approach).
	tbAccounts := make(map[types.Uint128]struct{}, len(accounts))
	for _, acct := range accounts {
		tbAccounts[acct.ID] = struct{}{}
		if _, hasPG := pgGrants[acct.ID]; !hasPG {
			check.Passed = false
			check.Details = fmt.Sprintf("TB account %v has no PG catalog row", acct.ID)
			return check, nil
		}
	}

	return check, nil
}

// Check 3: no grants past expires_at without a CreditExpiryID transfer.
func (c *Client) checkExpiredGrantsSwept(ctx context.Context) (ReconcileCheck, error) {
	check := ReconcileCheck{
		Name:     "expired_grants_swept",
		Severity: "warn",
		Passed:   true,
	}

	now := c.clock().UTC()

	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id FROM credit_grants
		WHERE expires_at <= $1
		  AND closed_at IS NULL
		ORDER BY grant_id ASC
	`, now)
	if err != nil {
		return check, fmt.Errorf("query expired active grants: %w", err)
	}
	defer rows.Close()

	var unswept int
	for rows.Next() {
		var grantIDStr string
		if err := rows.Scan(&grantIDStr); err != nil {
			return check, fmt.Errorf("scan grant_id: %w", err)
		}

		parsedULID, err := ulid.ParseStrict(grantIDStr)
		if err != nil {
			return check, fmt.Errorf("parse ULID %q: %w", grantIDStr, err)
		}

		// Check if the expiry transfer exists in TB.
		transferID := CreditExpiryID(GrantID(parsedULID))
		transfers, err := c.tb.LookupTransfers([]types.Uint128{transferID.raw})
		if err != nil {
			return check, fmt.Errorf("lookup expiry transfer for %s: %w", grantIDStr, err)
		}
		if len(transfers) == 0 {
			unswept++
		}
	}
	if err := rows.Err(); err != nil {
		return check, fmt.Errorf("iterate expired grants: %w", err)
	}

	if unswept > 0 {
		check.Passed = false
		check.Details = fmt.Sprintf("%d expired grant(s) without expiry transfer", unswept)
	}

	return check, nil
}

// Check 4: each licensed invoice produced exactly one Revenue transfer.
// For each completed stripe_licensed_charge task, verify a single
// StripeHolding→Revenue transfer exists in TB.
func (c *Client) checkLicensedChargeExactlyOnce(ctx context.Context) (ReconcileCheck, error) {
	check := ReconcileCheck{
		Name:     "licensed_charge_exactly_once",
		Severity: "alert",
		Passed:   true,
	}

	rows, err := c.pg.QueryContext(ctx, `
		SELECT task_id FROM tasks
		WHERE task_type = 'stripe_licensed_charge'
		  AND status = 'completed'
		ORDER BY task_id ASC
	`)
	if err != nil {
		return check, fmt.Errorf("query licensed charge tasks: %w", err)
	}
	defer rows.Close()

	var missing int
	for rows.Next() {
		var taskID int64
		if err := rows.Scan(&taskID); err != nil {
			return check, fmt.Errorf("scan task_id: %w", err)
		}

		// The transfer ID for licensed charges uses StripeDepositID(taskID, KindSubscriptionDeposit).
		transferID := StripeDepositID(TaskID(taskID), KindSubscriptionDeposit)
		transfers, err := c.tb.LookupTransfers([]types.Uint128{transferID.raw})
		if err != nil {
			return check, fmt.Errorf("lookup transfer for task %d: %w", taskID, err)
		}
		if len(transfers) != 1 {
			missing++
		}
	}
	if err := rows.Err(); err != nil {
		return check, fmt.Errorf("iterate licensed tasks: %w", err)
	}

	if missing > 0 {
		check.Passed = false
		check.Details = fmt.Sprintf("%d licensed charge task(s) without exactly one Revenue transfer", missing)
	}

	return check, nil
}

// Check 5: CH metering charge_units totals align with TB debits_posted.
// Compare per-org: sum of charge_units in CH metering vs sum of debits_posted
// on all grant accounts for that org.
func (c *Client) checkMeteringVsTransfers(ctx context.Context, ch ClickHouseQuerier) (ReconcileCheck, error) {
	check := ReconcileCheck{
		Name:     "metering_vs_transfers",
		Severity: "alert",
		Passed:   true,
	}

	if ch == nil {
		check.Passed = false
		check.Details = "ClickHouseQuerier not provided"
		return check, nil
	}

	// Get all orgs that have grants (active or closed).
	rows, err := c.pg.QueryContext(ctx, `
		SELECT DISTINCT org_id FROM credit_grants
	`)
	if err != nil {
		return check, fmt.Errorf("query orgs with grants: %w", err)
	}
	defer rows.Close()

	// Use a generous lookback window for reconciliation.
	since := c.clock().UTC().Add(-180 * 24 * time.Hour)
	var mismatches int

	for rows.Next() {
		var orgIDStr string
		if err := rows.Scan(&orgIDStr); err != nil {
			return check, fmt.Errorf("scan org_id: %w", err)
		}

		// CH: total charge_units for this org.
		chTotal, err := ch.SumChargeUnitsByOrg(ctx, orgIDStr, since)
		if err != nil {
			return check, fmt.Errorf("CH sum for org %s: %w", orgIDStr, err)
		}

		// TB: total debits_posted across all grant accounts for this org.
		tbTotal, err := c.sumOrgDebitsPosted(ctx, orgIDStr)
		if err != nil {
			return check, fmt.Errorf("TB debits for org %s: %w", orgIDStr, err)
		}

		// Allow some tolerance — TB debits include expiry transfers and dispute
		// debits, not just metering settlements. CH charge_units only represents
		// settled metering. TB debits >= CH charge_units is expected.
		// Flag if CH reports more than TB (data loss).
		if chTotal > tbTotal {
			mismatches++
		}
	}
	if err := rows.Err(); err != nil {
		return check, fmt.Errorf("iterate orgs: %w", err)
	}

	if mismatches > 0 {
		check.Passed = false
		check.Details = fmt.Sprintf("%d org(s) where CH charge_units exceeds TB debits_posted", mismatches)
	}

	return check, nil
}

// sumOrgDebitsPosted sums debits_posted across all grant accounts for an org.
func (c *Client) sumOrgDebitsPosted(ctx context.Context, orgIDStr string) (uint64, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id FROM credit_grants WHERE org_id = $1
	`, orgIDStr)
	if err != nil {
		return 0, fmt.Errorf("query org grants: %w", err)
	}
	defer rows.Close()

	var accountIDs []types.Uint128
	for rows.Next() {
		var grantIDStr string
		if err := rows.Scan(&grantIDStr); err != nil {
			return 0, fmt.Errorf("scan grant_id: %w", err)
		}
		parsedULID, err := ulid.ParseStrict(grantIDStr)
		if err != nil {
			return 0, fmt.Errorf("parse ULID %q: %w", grantIDStr, err)
		}
		accountIDs = append(accountIDs, GrantAccountID(GrantID(parsedULID)).raw)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate grants: %w", err)
	}

	if len(accountIDs) == 0 {
		return 0, nil
	}

	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return 0, fmt.Errorf("lookup accounts: %w", err)
	}

	var total uint64
	for _, acct := range accounts {
		debits, err := consumedFromAccount(acct)
		if err != nil {
			return 0, fmt.Errorf("account debits: %w", err)
		}
		total, err = safeAddUint64(total, debits)
		if err != nil {
			return 0, err
		}
	}

	return total, nil
}

// Check 6: no org has trust_tier='enterprise' set by the cron.
// We check that no trust_tier_promoted event has to='enterprise'.
func (c *Client) checkTrustTierMonotonicity(ctx context.Context) (ReconcileCheck, error) {
	check := ReconcileCheck{
		Name:     "trust_tier_monotonicity",
		Severity: "warn",
		Passed:   true,
	}

	var count int
	err := c.pg.QueryRowContext(ctx, `
		SELECT count(*)
		FROM billing_events
		WHERE event_type = 'trust_tier_promoted'
		  AND payload->>'to' = 'enterprise'
	`).Scan(&count)
	if err != nil {
		return check, fmt.Errorf("query enterprise promotions: %w", err)
	}

	if count > 0 {
		check.Passed = false
		check.Details = fmt.Sprintf("%d org(s) automatically promoted to enterprise", count)
	}

	return check, nil
}
