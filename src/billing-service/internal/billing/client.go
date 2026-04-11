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

	client := &Client{
		tb:       tbClient,
		pg:       pg,
		stripe:   sc,
		metering: metering,
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
	return nil
}

func (c *Client) ListSubscriptions(ctx context.Context, orgID OrgID) ([]SubscriptionRecord, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT subscription_id, org_id, product_id, plan_id, cadence, status, current_period_start, current_period_end
		FROM subscriptions
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
		if err := rows.Scan(
			&record.SubscriptionID,
			&record.OrgID,
			&record.ProductID,
			&record.PlanID,
			&record.Cadence,
			&record.Status,
			&start,
			&end,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
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
	return c.listGrantBalances(ctx, orgID, productID, "")
}

func (c *Client) listGrantBalancesByBucket(ctx context.Context, orgID OrgID, productID, bucketID string) ([]GrantBalance, error) {
	if bucketID == "" {
		return nil, fmt.Errorf("bucket_id is required")
	}
	return c.listGrantBalances(ctx, orgID, productID, bucketID)
}

func (c *Client) listGrantBalances(ctx context.Context, orgID OrgID, productID string, bucketID string) ([]GrantBalance, error) {
	now := c.clock().UTC()
	query := `
		SELECT grant_id, product_id, bucket_id, source, expires_at
		FROM credit_grants
		WHERE org_id = $1
		  AND closed_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $2)
	`
	args := []any{strconv.FormatUint(uint64(orgID), 10), now}
	if productID != "" {
		query += " AND product_id = $3"
		args = append(args, productID)
	}
	if bucketID != "" {
		query += fmt.Sprintf(" AND bucket_id = $%d", len(args)+1)
		args = append(args, bucketID)
	}
	query += " ORDER BY expires_at ASC NULLS LAST, grant_id ASC"

	rows, err := c.pg.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query grants: %w", err)
	}
	defer rows.Close()

	type rowGrant struct {
		GrantID   GrantID
		ProductID string
		BucketID  string
		Source    GrantSourceType
		ExpiresAt *time.Time
	}
	var grantRows []rowGrant
	accountIDs := make([]types.Uint128, 0, 8)
	for rows.Next() {
		var grantIDText string
		var grantProductID string
		var bucketID string
		var sourceText string
		var expiresAt sql.NullTime
		if err := rows.Scan(&grantIDText, &grantProductID, &bucketID, &sourceText, &expiresAt); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		grantID, err := ParseGrantID(grantIDText)
		if err != nil {
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
		if bucketID == "" {
			bucketID = grantProductID
		}
		grantRows = append(grantRows, rowGrant{GrantID: grantID, ProductID: grantProductID, BucketID: bucketID, Source: source, ExpiresAt: expiresAtPtr})
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
			GrantID:   rowGrant.GrantID,
			ProductID: rowGrant.ProductID,
			BucketID:  rowGrant.BucketID,
			Source:    rowGrant.Source,
			ExpiresAt: rowGrant.ExpiresAt,
			Available: available,
			Pending:   pending,
		})
	}
	return out, nil
}

func (c *Client) DepositCredits(ctx context.Context, grant CreditGrant) (GrantBalance, error) {
	if err := ctx.Err(); err != nil {
		return GrantBalance{}, err
	}
	sourceType, err := ParseGrantSourceType(grant.Source)
	if err != nil {
		return GrantBalance{}, err
	}
	if grant.ProductID == "" {
		return GrantBalance{}, fmt.Errorf("grant product_id is required")
	}
	if grant.BucketID == "" {
		grant.BucketID = grant.ProductID
	}
	if grant.Amount == 0 {
		return GrantBalance{}, fmt.Errorf("grant amount must be greater than zero")
	}

	grantID := NewGrantID()
	if grant.StripeReferenceID != "" {
		grantID = stripeGrantID(grant.OrgID, grant.ProductID, grant.BucketID, grant.StripeReferenceID)
		existing, err := c.lookupGrantByStripeRef(ctx, grant.OrgID, grant.ProductID, grant.BucketID, grant.StripeReferenceID)
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

	_, err = c.pg.ExecContext(ctx, `
		INSERT INTO credit_grants (grant_id, org_id, product_id, bucket_id, amount, source, stripe_reference_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, grantID.String(), strconv.FormatUint(uint64(grant.OrgID), 10), grant.ProductID, grant.BucketID, grant.Amount, grant.Source, grant.StripeReferenceID, grant.ExpiresAt)
	if err != nil {
		if grant.StripeReferenceID != "" && isUniqueViolation(err) {
			existing, lookupErr := c.lookupGrantByStripeRef(ctx, grant.OrgID, grant.ProductID, grant.BucketID, grant.StripeReferenceID)
			if lookupErr != nil {
				return GrantBalance{}, fmt.Errorf("lookup grant after stripe reference conflict: %w", lookupErr)
			}
			if existing != nil {
				return *existing, nil
			}
		}
		return GrantBalance{}, fmt.Errorf("insert grant row: %w", err)
	}

	return GrantBalance{
		GrantID:   grantID,
		ProductID: grant.ProductID,
		BucketID:  grant.BucketID,
		Source:    sourceType,
		ExpiresAt: grant.ExpiresAt,
		Available: grant.Amount,
	}, nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && string(pqErr.Code) == "23505"
}

func (c *Client) lookupGrantByStripeRef(ctx context.Context, orgID OrgID, productID, bucketID, stripeRef string) (*GrantBalance, error) {
	var grantIDText string
	var grantProductID string
	var grantBucketID string
	var sourceText string
	var expiresAt sql.NullTime
	err := c.pg.QueryRowContext(ctx, `
		SELECT grant_id, product_id, bucket_id, source, expires_at
		FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND bucket_id = $3 AND stripe_reference_id = $4
	`, strconv.FormatUint(uint64(orgID), 10), productID, bucketID, stripeRef).Scan(&grantIDText, &grantProductID, &grantBucketID, &sourceText, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup grant by stripe ref: %w", err)
	}

	grantID, err := ParseGrantID(grantIDText)
	if err != nil {
		return nil, err
	}
	source, err := ParseGrantSourceType(sourceText)
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
	return &GrantBalance{
		GrantID:   grantID,
		ProductID: grantProductID,
		BucketID:  grantBucketID,
		Source:    source,
		ExpiresAt: expiresAtPtr,
		Available: available,
		Pending:   pending,
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
