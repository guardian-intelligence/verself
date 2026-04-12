package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/lib/pq"
	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

type Client struct {
	tb       tb.Client
	pg       *sql.DB
	stripe   *stripe.Client
	metering MeteringWriter
	events   BillingEventWriter
	cfg      Config
	clock    func() time.Time
}

func NewClient(tbClient tb.Client, pg *sql.DB, sc *stripe.Client, metering MeteringWriter, cfg Config) (*Client, error) {
	if tbClient == nil {
		return nil, fmt.Errorf("%w: tigerbeetle client is required", ErrInvalidConfig)
	}
	if pg == nil {
		return nil, fmt.Errorf("%w: postgres handle is required", ErrInvalidConfig)
	}
	if sc == nil {
		return nil, fmt.Errorf("%w: stripe client is required", ErrInvalidConfig)
	}
	if metering == nil {
		return nil, fmt.Errorf("%w: metering writer is required", ErrInvalidConfig)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	eventWriter, ok := metering.(BillingEventWriter)
	if !ok {
		eventWriter = noopBillingEventWriter{}
	}

	client := &Client{
		tb:       tbClient,
		pg:       pg,
		stripe:   sc,
		metering: metering,
		events:   eventWriter,
		cfg:      cfg,
		clock:    time.Now,
	}
	if err := client.createAccounts(operatorAccounts()); err != nil {
		return nil, fmt.Errorf("ensure operator accounts: %w", err)
	}
	return client, nil
}

func (c *Client) EnsureOrg(ctx context.Context, orgID OrgID, displayName string, trustTier string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if trustTier == "" {
		trustTier = "new"
	}
	_, err := c.pg.ExecContext(ctx, `
		INSERT INTO orgs (org_id, display_name, trust_tier)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    trust_tier = EXCLUDED.trust_tier,
		    updated_at = now()
	`, strconv.FormatUint(uint64(orgID), 10), displayName, trustTier)
	if err != nil {
		return fmt.Errorf("upsert org: %w", err)
	}
	return c.EnsureCurrentEntitlements(ctx, orgID, "")
}

func (c *Client) ListSubscriptions(ctx context.Context, orgID OrgID) ([]SubscriptionRecord, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT subscription_id, contract_id, org_id, product_id, plan_id, cadence, status,
		       payment_state, entitlement_state, current_period_start, current_period_end
		FROM subscription_contracts
		WHERE org_id = $1
		ORDER BY subscription_id DESC
	`, strconv.FormatUint(uint64(orgID), 10))
	if err != nil {
		return nil, fmt.Errorf("query subscriptions: %w", err)
	}
	defer rows.Close()

	var out []SubscriptionRecord
	for rows.Next() {
		var record SubscriptionRecord
		var start sql.NullTime
		var end sql.NullTime
		var paymentState string
		var entitlementState string
		if err := rows.Scan(
			&record.SubscriptionID,
			&record.ContractID,
			&record.OrgID,
			&record.ProductID,
			&record.PlanID,
			&record.Cadence,
			&record.Status,
			&paymentState,
			&entitlementState,
			&start,
			&end,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		record.PaymentState = EntitlementPaymentState(paymentState)
		record.EntitlementState = EntitlementState(entitlementState)
		if start.Valid {
			value := start.Time.UTC()
			record.CurrentPeriodStart = &value
		}
		if end.Valid {
			value := end.Time.UTC()
			record.CurrentPeriodEnd = &value
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}
	return out, nil
}

func (c *Client) GetOrgBalance(ctx context.Context, orgID OrgID) (Balance, error) {
	if err := ctx.Err(); err != nil {
		return Balance{}, err
	}

	grants, err := c.ListGrantBalances(ctx, orgID, "")
	if err != nil {
		return Balance{}, err
	}

	var balance Balance
	for _, grant := range grants {
		if grant.Source.IsFreeTier() {
			balance.FreeTierAvailable, err = safeAddUint64(balance.FreeTierAvailable, grant.Available)
			if err != nil {
				return Balance{}, fmt.Errorf("sum free tier available: %w", err)
			}
			balance.FreeTierPending, err = safeAddUint64(balance.FreeTierPending, grant.Pending)
			if err != nil {
				return Balance{}, fmt.Errorf("sum free tier pending: %w", err)
			}
			continue
		}
		balance.CreditAvailable, err = safeAddUint64(balance.CreditAvailable, grant.Available)
		if err != nil {
			return Balance{}, fmt.Errorf("sum credit available: %w", err)
		}
		balance.CreditPending, err = safeAddUint64(balance.CreditPending, grant.Pending)
		if err != nil {
			return Balance{}, fmt.Errorf("sum credit pending: %w", err)
		}
	}
	balance.TotalAvailable, err = safeAddUint64(balance.FreeTierAvailable, balance.CreditAvailable)
	if err != nil {
		return Balance{}, fmt.Errorf("total available: %w", err)
	}
	return balance, nil
}

func (c *Client) ListGrantBalances(ctx context.Context, orgID OrgID, productID string) ([]GrantBalance, error) {
	return c.listGrantBalances(ctx, orgID, productID)
}

func (c *Client) listScopedGrantBalancesForFunding(ctx context.Context, orgID OrgID, productID string) ([]scopedGrantBalance, error) {
	grants, err := c.listGrantBalances(ctx, orgID, productID)
	if err != nil {
		return nil, err
	}
	out := make([]scopedGrantBalance, 0, len(grants))
	for _, grant := range grants {
		out = append(out, scopedGrantBalance{
			GrantID:        grant.GrantID,
			Source:         grant.Source,
			ScopeType:      grant.ScopeType,
			ScopeProductID: grant.ScopeProductID,
			ScopeBucketID:  grant.ScopeBucketID,
			AvailableUnits: grant.Available,
		})
	}
	return out, nil
}

func (c *Client) listGrantBalances(ctx context.Context, orgID OrgID, productID string) ([]GrantBalance, error) {
	now := c.clock().UTC()
	query := `
		SELECT grant_id, scope_type, scope_product_id, scope_bucket_id, source,
		       source_reference_id, entitlement_period_id, policy_version,
		       starts_at, period_start, period_end, expires_at
		FROM credit_grants
		WHERE org_id = $1
		  AND closed_at IS NULL
		  AND starts_at <= $2
		  AND (expires_at IS NULL OR expires_at > $2)
	`
	args := []any{strconv.FormatUint(uint64(orgID), 10), now}
	if productID != "" {
		query += " AND (scope_type = 'account' OR scope_product_id = $3)"
		args = append(args, productID)
	}
	query += " ORDER BY expires_at ASC NULLS LAST, grant_id ASC"

	rows, err := c.pg.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query grants: %w", err)
	}
	defer rows.Close()

	type rowGrant struct {
		GrantID        GrantID
		ScopeType      GrantScopeType
		ScopeProductID string
		ScopeBucketID  string
		Source         GrantSourceType
		SourceRef      string
		PeriodID       string
		PolicyVersion  string
		StartsAt       time.Time
		PeriodStart    *time.Time
		PeriodEnd      *time.Time
		ExpiresAt      *time.Time
	}
	var grantRows []rowGrant
	accountIDs := make([]types.Uint128, 0, 8)
	for rows.Next() {
		var grantIDText string
		var scopeTypeText string
		var scopeProductID string
		var scopeBucketID string
		var sourceText string
		var sourceRef string
		var periodID string
		var policyVersion string
		var startsAt time.Time
		var periodStart sql.NullTime
		var periodEnd sql.NullTime
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&grantIDText,
			&scopeTypeText,
			&scopeProductID,
			&scopeBucketID,
			&sourceText,
			&sourceRef,
			&periodID,
			&policyVersion,
			&startsAt,
			&periodStart,
			&periodEnd,
			&expiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		grantID, err := ParseGrantID(grantIDText)
		if err != nil {
			return nil, err
		}
		scopeType, err := ParseGrantScopeType(scopeTypeText)
		if err != nil {
			return nil, err
		}
		if err := validateGrantScope(scopeType, scopeProductID, scopeBucketID); err != nil {
			return nil, err
		}
		source, err := ParseGrantSourceType(sourceText)
		if err != nil {
			return nil, err
		}
		var expiresAtPtr *time.Time
		if expiresAt.Valid {
			value := expiresAt.Time.UTC()
			expiresAtPtr = &value
		}
		var periodStartPtr *time.Time
		if periodStart.Valid {
			value := periodStart.Time.UTC()
			periodStartPtr = &value
		}
		var periodEndPtr *time.Time
		if periodEnd.Valid {
			value := periodEnd.Time.UTC()
			periodEndPtr = &value
		}
		grantRows = append(grantRows, rowGrant{
			GrantID:        grantID,
			ScopeType:      scopeType,
			ScopeProductID: scopeProductID,
			ScopeBucketID:  scopeBucketID,
			Source:         source,
			SourceRef:      sourceRef,
			PeriodID:       periodID,
			PolicyVersion:  policyVersion,
			StartsAt:       startsAt.UTC(),
			PeriodStart:    periodStartPtr,
			PeriodEnd:      periodEndPtr,
			ExpiresAt:      expiresAtPtr,
		})
		accountIDs = append(accountIDs, GrantAccountID(grantID).raw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grants: %w", err)
	}
	if len(accountIDs) == 0 {
		return nil, nil
	}

	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup grant accounts: %w", err)
	}
	accountByID := make(map[types.Uint128]types.Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}

	out := make([]GrantBalance, 0, len(grantRows))
	for _, rowGrant := range grantRows {
		account, ok := accountByID[GrantAccountID(rowGrant.GrantID).raw]
		if !ok {
			return nil, fmt.Errorf("grant account missing for %s", rowGrant.GrantID.String())
		}
		available, err := availableFromAccount(account)
		if err != nil {
			return nil, fmt.Errorf("available balance %s: %w", rowGrant.GrantID.String(), err)
		}
		pending, err := pendingFromAccount(account)
		if err != nil {
			return nil, fmt.Errorf("pending balance %s: %w", rowGrant.GrantID.String(), err)
		}
		out = append(out, GrantBalance{
			GrantID:             rowGrant.GrantID,
			ScopeType:           rowGrant.ScopeType,
			ScopeProductID:      rowGrant.ScopeProductID,
			ScopeBucketID:       rowGrant.ScopeBucketID,
			Source:              rowGrant.Source,
			SourceReferenceID:   rowGrant.SourceRef,
			EntitlementPeriodID: rowGrant.PeriodID,
			PolicyVersion:       rowGrant.PolicyVersion,
			StartsAt:            rowGrant.StartsAt,
			PeriodStart:         rowGrant.PeriodStart,
			PeriodEnd:           rowGrant.PeriodEnd,
			ExpiresAt:           rowGrant.ExpiresAt,
			Available:           available,
			Pending:             pending,
		})
	}
	return out, nil
}

func (c *Client) DepositCredits(ctx context.Context, grant CreditGrant) (GrantBalance, error) {
	return c.IssueCreditGrant(ctx, grant)
}

func (c *Client) IssueCreditGrant(ctx context.Context, grant CreditGrant) (GrantBalance, error) {
	if err := ctx.Err(); err != nil {
		return GrantBalance{}, err
	}
	sourceType, err := ParseGrantSourceType(grant.Source)
	if err != nil {
		return GrantBalance{}, err
	}
	if err := validateGrantScope(grant.ScopeType, grant.ScopeProductID, grant.ScopeBucketID); err != nil {
		return GrantBalance{}, err
	}
	if grant.Amount == 0 {
		return GrantBalance{}, fmt.Errorf("grant amount must be greater than zero")
	}
	startsAt := c.clock().UTC()
	if grant.StartsAt != nil {
		startsAt = grant.StartsAt.UTC()
	}

	grantID := NewGrantID()
	if grant.SourceReferenceID != "" {
		grantID = sourceReferenceGrantID(grant.OrgID, sourceType, grant.ScopeType, grant.ScopeProductID, grant.ScopeBucketID, grant.SourceReferenceID)
		existing, err := c.lookupGrantBySourceRef(ctx, grant.OrgID, sourceType, grant.ScopeType, grant.ScopeProductID, grant.ScopeBucketID, grant.SourceReferenceID)
		if err != nil {
			return GrantBalance{}, err
		}
		if existing != nil {
			return *existing, nil
		}
	}

	if err := c.createGrantAccount(grantID, grant.OrgID, sourceType); err != nil {
		return GrantBalance{}, err
	}

	if err := c.createTransfers([]types.Transfer{{
		ID:              WindowTransferID("deposit:"+grantID.String(), 0, KindStripeDeposit).raw,
		DebitAccountID:  OperatorAccountID(AcctStripeHolding).raw,
		CreditAccountID: GrantAccountID(grantID).raw,
		Amount:          types.ToUint128(grant.Amount),
		Ledger:          1,
		Code:            uint16(KindStripeDeposit),
	}}); err != nil {
		return GrantBalance{}, fmt.Errorf("deposit grant transfer: %w", err)
	}

	tx, err := c.pg.BeginTx(ctx, nil)
	if err != nil {
		return GrantBalance{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO credit_grants (
			grant_id, org_id, scope_type, scope_product_id, scope_bucket_id, amount, source,
			source_reference_id, entitlement_period_id, policy_version,
			starts_at, period_start, period_end, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7,
		        $8, $9, $10,
		        $11, $12, $13, $14)
		ON CONFLICT (grant_id) DO NOTHING
	`, grantID.String(), strconv.FormatUint(uint64(grant.OrgID), 10), grant.ScopeType.String(), grant.ScopeProductID, grant.ScopeBucketID, grant.Amount, grant.Source,
		grant.SourceReferenceID, grant.EntitlementPeriodID, grant.PolicyVersion,
		startsAt, grant.PeriodStart, grant.PeriodEnd, grant.ExpiresAt)
	if err != nil {
		if grant.SourceReferenceID != "" && isUniqueViolation(err) {
			return c.lookupGrantAfterSourceRefConflict(ctx, grant, sourceType)
		}
		return GrantBalance{}, fmt.Errorf("insert grant row: %w", err)
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		if grant.SourceReferenceID != "" {
			return c.lookupGrantAfterSourceRefConflict(ctx, grant, sourceType)
		}
		return GrantBalance{}, fmt.Errorf("grant %s already exists", grantID.String())
	}

	if err := insertOutboxEventTx(ctx, tx, grantIssuedEvent(grantID, grant, startsAt)); err != nil {
		return GrantBalance{}, err
	}
	if err := tx.Commit(); err != nil {
		return GrantBalance{}, fmt.Errorf("commit grant row: %w", err)
	}

	return GrantBalance{
		GrantID:             grantID,
		ScopeType:           grant.ScopeType,
		ScopeProductID:      grant.ScopeProductID,
		ScopeBucketID:       grant.ScopeBucketID,
		Source:              sourceType,
		SourceReferenceID:   grant.SourceReferenceID,
		EntitlementPeriodID: grant.EntitlementPeriodID,
		PolicyVersion:       grant.PolicyVersion,
		StartsAt:            startsAt,
		PeriodStart:         grant.PeriodStart,
		PeriodEnd:           grant.PeriodEnd,
		ExpiresAt:           grant.ExpiresAt,
		Available:           grant.Amount,
	}, nil
}

func (c *Client) lookupGrantAfterSourceRefConflict(ctx context.Context, grant CreditGrant, sourceType GrantSourceType) (GrantBalance, error) {
	existing, lookupErr := c.lookupGrantBySourceRef(ctx, grant.OrgID, sourceType, grant.ScopeType, grant.ScopeProductID, grant.ScopeBucketID, grant.SourceReferenceID)
	if lookupErr != nil {
		return GrantBalance{}, fmt.Errorf("lookup grant after source reference conflict: %w", lookupErr)
	}
	if existing != nil {
		return *existing, nil
	}
	return GrantBalance{}, fmt.Errorf("source reference conflict without matching grant")
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && string(pqErr.Code) == "23505"
}

func (c *Client) lookupGrantBySourceRef(ctx context.Context, orgID OrgID, source GrantSourceType, scopeType GrantScopeType, scopeProductID, scopeBucketID, sourceRef string) (*GrantBalance, error) {
	var grantIDText string
	var scopeTypeText string
	var grantScopeProductID string
	var grantScopeBucketID string
	var sourceText string
	var foundSourceRef string
	var entitlementPeriodID string
	var policyVersion string
	var startsAt time.Time
	var periodStart sql.NullTime
	var periodEnd sql.NullTime
	var expiresAt sql.NullTime
	err := c.pg.QueryRowContext(ctx, `
		SELECT grant_id, scope_type, scope_product_id, scope_bucket_id, source,
		       source_reference_id, entitlement_period_id, policy_version,
		       starts_at, period_start, period_end, expires_at
		FROM credit_grants
		WHERE org_id = $1
		  AND source = $2
		  AND scope_type = $3
		  AND scope_product_id = $4
		  AND scope_bucket_id = $5
		  AND source_reference_id = $6
	`, strconv.FormatUint(uint64(orgID), 10), source.String(), scopeType.String(), scopeProductID, scopeBucketID, sourceRef).Scan(
		&grantIDText,
		&scopeTypeText,
		&grantScopeProductID,
		&grantScopeBucketID,
		&sourceText,
		&foundSourceRef,
		&entitlementPeriodID,
		&policyVersion,
		&startsAt,
		&periodStart,
		&periodEnd,
		&expiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup grant by source ref: %w", err)
	}

	grantID, err := ParseGrantID(grantIDText)
	if err != nil {
		return nil, err
	}
	parsedScopeType, err := ParseGrantScopeType(scopeTypeText)
	if err != nil {
		return nil, err
	}
	if err := validateGrantScope(parsedScopeType, grantScopeProductID, grantScopeBucketID); err != nil {
		return nil, err
	}
	parsedSource, err := ParseGrantSourceType(sourceText)
	if err != nil {
		return nil, err
	}
	accounts, err := c.tb.LookupAccounts([]types.Uint128{GrantAccountID(grantID).raw})
	if err != nil {
		return nil, fmt.Errorf("lookup existing grant account: %w", err)
	}
	if len(accounts) != 1 {
		return nil, fmt.Errorf("expected one account for grant %s, got %d", grantIDText, len(accounts))
	}
	available, err := availableFromAccount(accounts[0])
	if err != nil {
		return nil, err
	}
	pending, err := pendingFromAccount(accounts[0])
	if err != nil {
		return nil, err
	}
	var expiresAtPtr *time.Time
	if expiresAt.Valid {
		value := expiresAt.Time.UTC()
		expiresAtPtr = &value
	}
	var periodStartPtr *time.Time
	if periodStart.Valid {
		value := periodStart.Time.UTC()
		periodStartPtr = &value
	}
	var periodEndPtr *time.Time
	if periodEnd.Valid {
		value := periodEnd.Time.UTC()
		periodEndPtr = &value
	}
	return &GrantBalance{
		GrantID:             grantID,
		ScopeType:           parsedScopeType,
		ScopeProductID:      grantScopeProductID,
		ScopeBucketID:       grantScopeBucketID,
		Source:              parsedSource,
		SourceReferenceID:   foundSourceRef,
		EntitlementPeriodID: entitlementPeriodID,
		PolicyVersion:       policyVersion,
		StartsAt:            startsAt.UTC(),
		PeriodStart:         periodStartPtr,
		PeriodEnd:           periodEndPtr,
		ExpiresAt:           expiresAtPtr,
		Available:           available,
		Pending:             pending,
	}, nil
}

func operatorAccounts() []types.Account {
	return []types.Account{
		{
			ID:     OperatorAccountID(AcctRevenue).raw,
			Ledger: 1,
			Code:   uint16(AcctRevenue),
			Flags:  types.AccountFlags{DebitsMustNotExceedCredits: true, History: true}.ToUint16(),
		},
		{
			ID:     OperatorAccountID(AcctStripeHolding).raw,
			Ledger: 1,
			Code:   uint16(AcctStripeHolding),
			Flags:  types.AccountFlags{History: true}.ToUint16(),
		},
	}
}

func grantAccount(grantID GrantID, orgID OrgID, sourceType GrantSourceType) types.Account {
	return types.Account{
		ID:         GrantAccountID(grantID).raw,
		UserData64: uint64(orgID),
		UserData32: uint32(sourceType),
		Ledger:     1,
		Code:       AcctGrantCode,
		Flags:      types.AccountFlags{DebitsMustNotExceedCredits: true, History: true}.ToUint16(),
	}
}

func (c *Client) createGrantAccount(grantID GrantID, orgID OrgID, sourceType GrantSourceType) error {
	return c.createAccounts([]types.Account{grantAccount(grantID, orgID, sourceType)})
}

func (c *Client) createAccounts(accounts []types.Account) error {
	results, err := c.tb.CreateAccounts(accounts)
	if err != nil {
		return fmt.Errorf("create accounts: %w", err)
	}
	for _, result := range results {
		switch result.Result {
		case types.AccountOK, types.AccountExists:
			continue
		case types.AccountExistsWithDifferentFlags,
			types.AccountExistsWithDifferentUserData128,
			types.AccountExistsWithDifferentUserData64,
			types.AccountExistsWithDifferentUserData32,
			types.AccountExistsWithDifferentLedger,
			types.AccountExistsWithDifferentCode:
			return fmt.Errorf("account %d: %w: %s", result.Index, ErrAccountConflict, result.Result)
		default:
			return fmt.Errorf("account %d: creation failed: %s", result.Index, result.Result)
		}
	}
	return nil
}

func (c *Client) createTransfers(transfers []types.Transfer) error {
	if len(transfers) == 0 {
		return nil
	}
	results, err := c.tb.CreateTransfers(transfers)
	if err != nil {
		return fmt.Errorf("create transfers: %w", err)
	}
	for _, result := range results {
		switch result.Result {
		case types.TransferOK, types.TransferExists, types.TransferPendingTransferAlreadyPosted:
			continue
		case types.TransferExceedsCredits:
			return ErrInsufficientBalance
		case types.TransferPendingTransferExpired:
			return ErrPendingTransferExpired
		default:
			return fmt.Errorf("transfer %d: %s", result.Index, result.Result)
		}
	}
	return nil
}

func decodeUint64Map(raw []byte) (map[string]uint64, error) {
	if len(raw) == 0 {
		return map[string]uint64{}, nil
	}
	var out map[string]uint64
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode uint64 map: %w", err)
	}
	if out == nil {
		return map[string]uint64{}, nil
	}
	return out, nil
}

func decodeStringMap(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode string map: %w", err)
	}
	if out == nil {
		return map[string]string{}, nil
	}
	return out, nil
}

func cloneFloat64Map(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneUint64Map(in map[string]uint64) map[string]uint64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]uint64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyJSONMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
