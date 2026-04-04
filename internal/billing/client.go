package billing

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// Client is the billing package entrypoint.
type Client struct {
	tb     tb.Client
	pg     *sql.DB
	stripe *stripe.Client
	cfg    Config
	clock  func() time.Time
}

// NewClient constructs a billing Client.
func NewClient(tbClient tb.Client, pg *sql.DB, sc *stripe.Client, cfg Config) (*Client, error) {
	if tbClient == nil {
		return nil, fmt.Errorf("%w: tigerbeetle client is required", ErrInvalidConfig)
	}
	if pg == nil {
		return nil, fmt.Errorf("%w: postgres handle is required", ErrInvalidConfig)
	}
	if sc == nil {
		return nil, fmt.Errorf("%w: stripe client is required", ErrInvalidConfig)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client := &Client{
		tb:     tbClient,
		pg:     pg,
		stripe: sc,
		cfg:    cfg,
		clock:  time.Now,
	}

	if err := client.createAccounts(operatorAccounts()); err != nil {
		return nil, fmt.Errorf("ensure operator accounts: %w", err)
	}

	return client, nil
}

// EnsureOrg provisions an org in PostgreSQL.
func (c *Client) EnsureOrg(ctx context.Context, orgID OrgID, displayName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO orgs (org_id, display_name)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, orgIDText, displayName); err != nil {
		return fmt.Errorf("insert org row: %w", err)
	}

	return nil
}

// GetOrgBalance sums active grant-account balances by source category.
func (c *Client) GetOrgBalance(ctx context.Context, orgID OrgID) (Balance, error) {
	if err := ctx.Err(); err != nil {
		return Balance{}, err
	}

	rows, err := c.pg.QueryContext(ctx, `
		SELECT grant_id, source
		FROM credit_grants
		WHERE org_id = $1
		  AND closed_at IS NULL
		ORDER BY grant_id ASC
	`, strconv.FormatUint(uint64(orgID), 10))
	if err != nil {
		return Balance{}, fmt.Errorf("query active grants: %w", err)
	}
	defer rows.Close()

	accountIDs := make([]types.Uint128, 0, 8)
	sourceByAccountID := make(map[types.Uint128]GrantSourceType)
	for rows.Next() {
		var grantID int64
		var source string
		if err := rows.Scan(&grantID, &source); err != nil {
			return Balance{}, fmt.Errorf("scan active grant: %w", err)
		}

		sourceType, err := ParseGrantSourceType(source)
		if err != nil {
			return Balance{}, fmt.Errorf("grant %d: %w", grantID, err)
		}

		accountID := GrantAccountID(GrantID(grantID)).raw
		accountIDs = append(accountIDs, accountID)
		sourceByAccountID[accountID] = sourceType
	}
	if err := rows.Err(); err != nil {
		return Balance{}, fmt.Errorf("iterate active grants: %w", err)
	}
	if len(accountIDs) == 0 {
		return Balance{}, nil
	}

	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return Balance{}, fmt.Errorf("lookup grant accounts: %w", err)
	}

	var balance Balance
	seen := make(map[types.Uint128]struct{}, len(accounts))
	for _, account := range accounts {
		sourceType, ok := sourceByAccountID[account.ID]
		if !ok {
			return Balance{}, fmt.Errorf("lookup grant accounts: unexpected account %v", account.ID)
		}

		available, err := availableFromAccount(account)
		if err != nil {
			return Balance{}, fmt.Errorf("grant account %v available: %w", account.ID, err)
		}
		pending, err := pendingFromAccount(account)
		if err != nil {
			return Balance{}, fmt.Errorf("grant account %v pending: %w", account.ID, err)
		}

		if sourceType.IsFreeTier() {
			balance.FreeTierAvailable, err = safeAddUint64(balance.FreeTierAvailable, available)
			if err != nil {
				return Balance{}, fmt.Errorf("sum free tier available: %w", err)
			}
			balance.FreeTierPending, err = safeAddUint64(balance.FreeTierPending, pending)
			if err != nil {
				return Balance{}, fmt.Errorf("sum free tier pending: %w", err)
			}
		} else {
			balance.CreditAvailable, err = safeAddUint64(balance.CreditAvailable, available)
			if err != nil {
				return Balance{}, fmt.Errorf("sum credit available: %w", err)
			}
			balance.CreditPending, err = safeAddUint64(balance.CreditPending, pending)
			if err != nil {
				return Balance{}, fmt.Errorf("sum credit pending: %w", err)
			}
		}

		seen[account.ID] = struct{}{}
	}

	if len(seen) != len(accountIDs) {
		return Balance{}, fmt.Errorf("lookup grant accounts: expected %d accounts, got %d", len(accountIDs), len(seen))
	}

	balance.TotalAvailable, err = safeAddUint64(balance.FreeTierAvailable, balance.CreditAvailable)
	if err != nil {
		return Balance{}, fmt.Errorf("total available: %w", err)
	}

	return balance, nil
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
			ID:     OperatorAccountID(AcctFreeTierPool).raw,
			Ledger: 1,
			Code:   uint16(AcctFreeTierPool),
			Flags:  types.AccountFlags{DebitsMustNotExceedCredits: true, History: true}.ToUint16(),
		},
		{
			ID:     OperatorAccountID(AcctStripeHolding).raw,
			Ledger: 1,
			Code:   uint16(AcctStripeHolding),
			Flags:  types.AccountFlags{History: true}.ToUint16(),
		},
		{
			ID:     OperatorAccountID(AcctPromoPool).raw,
			Ledger: 1,
			Code:   uint16(AcctPromoPool),
			Flags:  types.AccountFlags{DebitsMustNotExceedCredits: true, History: true}.ToUint16(),
		},
		{
			ID:     OperatorAccountID(AcctFreeTierExpense).raw,
			Ledger: 1,
			Code:   uint16(AcctFreeTierExpense),
			Flags:  types.AccountFlags{History: true}.ToUint16(),
		},
		{
			ID:     OperatorAccountID(AcctExpiredCredits).raw,
			Ledger: 1,
			Code:   uint16(AcctExpiredCredits),
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
		Code:       uint16(AcctGrant),
		Flags:      types.AccountFlags{DebitsMustNotExceedCredits: true, History: true}.ToUint16(),
	}
}

func (c *Client) createGrantAccount(grantID GrantID, orgID OrgID, sourceType GrantSourceType) error {
	if err := c.createAccounts([]types.Account{grantAccount(grantID, orgID, sourceType)}); err != nil {
		return fmt.Errorf("create grant account: %w", err)
	}
	return nil
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
