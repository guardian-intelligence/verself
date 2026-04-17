package ledger

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	DefaultLedger uint32 = 1
	BatchLimit           = 8189
)

const (
	AccountCodeStripeExternal            uint16 = 1
	AccountCodeStripeHolding             uint16 = 2
	AccountCodeContractAllowanceClearing uint16 = 3
	AccountCodeFreeTierExpense           uint16 = 4
	AccountCodePromoExpense              uint16 = 5
	AccountCodeRefundExpense             uint16 = 6
	AccountCodeWriteoffExpense           uint16 = 7
	AccountCodeExpiredCredits            uint16 = 8
	AccountCodeRevenue                   uint16 = 9
	AccountCodeReceivable                uint16 = 10
	AccountCodeRevokedContractAllowance  uint16 = 11
	AccountCodeCustomerGrant             uint16 = 100
)

const (
	TransferCodeGrantDeposit    uint16 = 1
	TransferCodeUsageSpend      uint16 = 2
	TransferCodeStripePaymentIn uint16 = 3
	TransferCodeGrantSweep      uint16 = 4
	TransferCodeCorrection      uint16 = 5
)

const (
	SourceFreeTier uint32 = 1
	SourceContract uint32 = 2
	SourcePurchase uint32 = 3
	SourcePromo    uint32 = 4
	SourceRefund   uint32 = 5
)

var (
	ErrUnavailable          = errors.New("ledger: unavailable")
	ErrBatchTooLarge        = errors.New("ledger: batch too large")
	ErrAccountConflict      = errors.New("ledger: account conflict")
	ErrTransferConflict     = errors.New("ledger: transfer conflict")
	ErrInsufficientBalance  = errors.New("ledger: insufficient balance")
	ErrPendingTransferGone  = errors.New("ledger: pending transfer expired or missing")
	ErrInvalidCommand       = errors.New("ledger: invalid command")
	ErrAccountNotFound      = errors.New("ledger: account not found")
	ErrBalanceOverflow      = errors.New("ledger: balance overflow")
	ErrUnsupportedGrantType = errors.New("ledger: unsupported grant source")
)

var tracer = otel.Tracer("billing-service/internal/billing/ledger")

type ID [16]byte

func NewID() ID {
	return IDFromUint128(tbtypes.ID())
}

func IDFromUint128(value tbtypes.Uint128) ID {
	return ID(value.Bytes())
}

func IDFromBytes(value []byte) (ID, error) {
	var out ID
	if len(value) != 16 {
		return out, fmt.Errorf("ledger id must be 16 bytes, got %d", len(value))
	}
	copy(out[:], value)
	return out, nil
}

func ParseID(value string) (ID, error) {
	var out ID
	value = strings.TrimSpace(value)
	if value == "" {
		return out, nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return out, fmt.Errorf("parse ledger id: %w", err)
	}
	return IDFromBytes(decoded)
}

func (id ID) IsZero() bool {
	return id == ID{}
}

func (id ID) String() string {
	if id.IsZero() {
		return ""
	}
	return hex.EncodeToString(id[:])
}

func (id ID) Bytes() []byte {
	out := make([]byte, 16)
	copy(out, id[:])
	return out
}

func (id ID) Uint128() tbtypes.Uint128 {
	return tbtypes.BytesToUint128([16]byte(id))
}

type Client struct {
	tb tb.Client
}

func NewClient(clusterID uint64, addresses []string) (*Client, error) {
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%w: tigerbeetle address is required", ErrUnavailable)
	}
	native, err := tb.NewClient(tbtypes.ToUint128(clusterID), addresses)
	if err != nil {
		return nil, fmt.Errorf("create tigerbeetle client: %w", err)
	}
	return &Client{tb: native}, nil
}

func Wrap(native tb.Client) *Client {
	if native == nil {
		return nil
	}
	return &Client{tb: native}
}

func (c *Client) Close() {
	if c != nil && c.tb != nil {
		c.tb.Close()
	}
}

type AccountDefinition struct {
	Key         string
	Kind        string
	Code        uint16
	Flags       uint16
	Description string
}

func OperatorAccountDefinitions() []AccountDefinition {
	history := tbtypes.AccountFlags{History: true}.ToUint16()
	creditPositive := tbtypes.AccountFlags{DebitsMustNotExceedCredits: true, History: true}.ToUint16()
	debitPositive := tbtypes.AccountFlags{CreditsMustNotExceedDebits: true, History: true}.ToUint16()
	return []AccountDefinition{
		{Key: "operator_stripe_external", Kind: "operator", Code: AccountCodeStripeExternal, Flags: history, Description: "Stripe and external payment rails"},
		{Key: "operator_stripe_holding", Kind: "operator", Code: AccountCodeStripeHolding, Flags: creditPositive, Description: "Cash-confirmed prepaid credit clearing"},
		{Key: "operator_contract_allowance_clearing", Kind: "operator", Code: AccountCodeContractAllowanceClearing, Flags: debitPositive, Description: "Recurring allowance clearing for active contracts"},
		{Key: "operator_free_tier_expense", Kind: "operator", Code: AccountCodeFreeTierExpense, Flags: debitPositive, Description: "Free-tier allowance funding"},
		{Key: "operator_promo_expense", Kind: "operator", Code: AccountCodePromoExpense, Flags: debitPositive, Description: "Promotional credit funding"},
		{Key: "operator_refund_expense", Kind: "operator", Code: AccountCodeRefundExpense, Flags: debitPositive, Description: "Refund credit funding"},
		{Key: "operator_writeoff_expense", Kind: "operator", Code: AccountCodeWriteoffExpense, Flags: debitPositive, Description: "No-consent usage absorbed by the platform"},
		{Key: "operator_expired_credits", Kind: "operator", Code: AccountCodeExpiredCredits, Flags: creditPositive, Description: "Unused grant balances swept at expiry"},
		{Key: "operator_revenue", Kind: "operator", Code: AccountCodeRevenue, Flags: creditPositive, Description: "Recognized usage revenue"},
		{Key: "operator_receivable", Kind: "operator", Code: AccountCodeReceivable, Flags: creditPositive, Description: "Consented usage awaiting invoice collection"},
		{Key: "operator_revoked_contract_allowance", Kind: "operator", Code: AccountCodeRevokedContractAllowance, Flags: creditPositive, Description: "Unused contract allowance revoked after terminal non-payment"},
	}
}

func GrantSourceCode(source string) (uint32, error) {
	switch source {
	case "free_tier":
		return SourceFreeTier, nil
	case "contract":
		return SourceContract, nil
	case "purchase":
		return SourcePurchase, nil
	case "promo":
		return SourcePromo, nil
	case "refund":
		return SourceRefund, nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedGrantType, source)
	}
}

func GrantFundingAccountKey(source string) (string, error) {
	switch source {
	case "free_tier":
		return "operator_free_tier_expense", nil
	case "contract":
		return "operator_contract_allowance_clearing", nil
	case "purchase":
		return "operator_stripe_holding", nil
	case "promo":
		return "operator_promo_expense", nil
	case "refund":
		return "operator_refund_expense", nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedGrantType, source)
	}
}

type AccountPayload struct {
	ID          string `json:"id"`
	UserData128 string `json:"user_data_128,omitempty"`
	UserData64  uint64 `json:"user_data_64,omitempty"`
	UserData32  uint32 `json:"user_data_32,omitempty"`
	Ledger      uint32 `json:"ledger"`
	Code        uint16 `json:"code"`
	Flags       uint16 `json:"flags"`
}

type TransferPayload struct {
	ID              string `json:"id"`
	DebitAccountID  string `json:"debit_account_id,omitempty"`
	CreditAccountID string `json:"credit_account_id,omitempty"`
	Amount          uint64 `json:"amount,omitempty"`
	PendingID       string `json:"pending_id,omitempty"`
	UserData128     string `json:"user_data_128,omitempty"`
	UserData64      uint64 `json:"user_data_64,omitempty"`
	UserData32      uint32 `json:"user_data_32,omitempty"`
	Timeout         uint32 `json:"timeout,omitempty"`
	Ledger          uint32 `json:"ledger,omitempty"`
	Code            uint16 `json:"code,omitempty"`
	Flags           uint16 `json:"flags,omitempty"`
}

type CommandPayload struct {
	Accounts  []AccountPayload  `json:"accounts,omitempty"`
	Transfers []TransferPayload `json:"transfers,omitempty"`
}

type Balance struct {
	AccountID ID
	Available uint64
	Pending   uint64
	Spent     uint64
}

type AccountSnapshot struct {
	AccountID      ID
	DebitsPosted   uint64
	DebitsPending  uint64
	CreditsPosted  uint64
	CreditsPending uint64
}

func CustomerGrantAccount(id ID, orgID uint64, source string) (AccountPayload, error) {
	sourceCode, err := GrantSourceCode(source)
	if err != nil {
		return AccountPayload{}, err
	}
	return AccountPayload{
		ID:         id.String(),
		UserData64: orgID,
		UserData32: sourceCode,
		Ledger:     DefaultLedger,
		Code:       AccountCodeCustomerGrant,
		Flags:      tbtypes.AccountFlags{DebitsMustNotExceedCredits: true, History: true}.ToUint16(),
	}, nil
}

func OperatorAccount(id ID, def AccountDefinition) AccountPayload {
	return AccountPayload{ID: id.String(), Ledger: DefaultLedger, Code: def.Code, Flags: def.Flags}
}

func LinkTransfers(transfers []TransferPayload) {
	for i := range transfers {
		flags := transferFlagsFromUint16(transfers[i].Flags)
		flags.Linked = i < len(transfers)-1
		transfers[i].Flags = flags.ToUint16()
	}
}

func UsageSpendTransfer(id ID, grantAccountID ID, revenueAccountID ID, amount uint64, windowCorrelation ID, businessTimeMS uint64) TransferPayload {
	return TransferPayload{
		ID:              id.String(),
		DebitAccountID:  grantAccountID.String(),
		CreditAccountID: revenueAccountID.String(),
		Amount:          amount,
		UserData128:     windowCorrelation.String(),
		UserData64:      businessTimeMS,
		Ledger:          DefaultLedger,
		Code:            TransferCodeUsageSpend,
	}
}

func GrantDepositTransfer(id ID, debitAccountID ID, creditAccountID ID, amount uint64, grantCorrelation ID, businessTimeMS uint64) TransferPayload {
	return TransferPayload{
		ID:              id.String(),
		DebitAccountID:  debitAccountID.String(),
		CreditAccountID: creditAccountID.String(),
		Amount:          amount,
		UserData128:     grantCorrelation.String(),
		UserData64:      businessTimeMS,
		Ledger:          DefaultLedger,
		Code:            TransferCodeGrantDeposit,
	}
}

func StripePaymentInTransfer(id ID, externalAccountID ID, holdingAccountID ID, amount uint64, grantCorrelation ID, businessTimeMS uint64) TransferPayload {
	return TransferPayload{
		ID:              id.String(),
		DebitAccountID:  externalAccountID.String(),
		CreditAccountID: holdingAccountID.String(),
		Amount:          amount,
		UserData128:     grantCorrelation.String(),
		UserData64:      businessTimeMS,
		Ledger:          DefaultLedger,
		Code:            TransferCodeStripePaymentIn,
	}
}

func (c *Client) Dispatch(ctx context.Context, operation string, payload CommandPayload) error {
	if c == nil || c.tb == nil {
		return ErrUnavailable
	}
	ctx, span := tracer.Start(ctx, "billing.ledger.dispatch")
	defer span.End()
	span.SetAttributes(attribute.String("ledger.operation", operation), attribute.Int("ledger.account_count", len(payload.Accounts)), attribute.Int("ledger.transfer_count", len(payload.Transfers)))
	if len(payload.Accounts) > BatchLimit || len(payload.Transfers) > BatchLimit {
		return ErrBatchTooLarge
	}
	if len(payload.Accounts) > 0 {
		accounts, err := payloadAccounts(payload.Accounts)
		if err != nil {
			return err
		}
		if err := c.createAccounts(ctx, operation, accounts); err != nil {
			return err
		}
	}
	if len(payload.Transfers) > 0 {
		transfers, err := payloadTransfers(payload.Transfers)
		if err != nil {
			return err
		}
		if err := c.createTransfers(ctx, operation, transfers); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) LookupBalances(ctx context.Context, ids []ID) (map[ID]Balance, error) {
	if c == nil || c.tb == nil {
		return nil, ErrUnavailable
	}
	ctx, span := tracer.Start(ctx, "billing.ledger.lookup_accounts")
	defer span.End()
	span.SetAttributes(attribute.Int("ledger.account_count", len(ids)))
	if len(ids) > BatchLimit {
		return nil, ErrBatchTooLarge
	}
	accountIDs := make([]tbtypes.Uint128, 0, len(ids))
	for _, id := range ids {
		if id.IsZero() {
			continue
		}
		accountIDs = append(accountIDs, id.Uint128())
	}
	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup accounts: %w", err)
	}
	out := make(map[ID]Balance, len(accounts))
	for _, account := range accounts {
		id := IDFromUint128(account.ID)
		available, err := availableFromAccount(account)
		if err != nil {
			return nil, fmt.Errorf("account %s available: %w", id.String(), err)
		}
		pending, err := uint128ToUint64(account.DebitsPending)
		if err != nil {
			return nil, fmt.Errorf("account %s pending: %w", id.String(), err)
		}
		spent, err := uint128ToUint64(account.DebitsPosted)
		if err != nil {
			return nil, fmt.Errorf("account %s spent: %w", id.String(), err)
		}
		out[id] = Balance{AccountID: id, Available: available, Pending: pending, Spent: spent}
	}
	return out, nil
}

func (c *Client) LookupAccountIDs(ctx context.Context, ids []ID) (map[ID]struct{}, error) {
	if c == nil || c.tb == nil {
		return nil, ErrUnavailable
	}
	ctx, span := tracer.Start(ctx, "billing.ledger.lookup_account_ids")
	defer span.End()
	span.SetAttributes(attribute.Int("ledger.account_count", len(ids)))
	if len(ids) > BatchLimit {
		return nil, ErrBatchTooLarge
	}
	accountIDs := make([]tbtypes.Uint128, 0, len(ids))
	for _, id := range ids {
		if id.IsZero() {
			continue
		}
		accountIDs = append(accountIDs, id.Uint128())
	}
	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup accounts: %w", err)
	}
	out := make(map[ID]struct{}, len(accounts))
	for _, account := range accounts {
		out[IDFromUint128(account.ID)] = struct{}{}
	}
	return out, nil
}

func (c *Client) LookupAccountSnapshots(ctx context.Context, ids []ID) (map[ID]AccountSnapshot, error) {
	if c == nil || c.tb == nil {
		return nil, ErrUnavailable
	}
	ctx, span := tracer.Start(ctx, "billing.ledger.lookup_account_snapshots")
	defer span.End()
	span.SetAttributes(attribute.Int("ledger.account_count", len(ids)))
	if len(ids) > BatchLimit {
		return nil, ErrBatchTooLarge
	}
	accountIDs := make([]tbtypes.Uint128, 0, len(ids))
	for _, id := range ids {
		if id.IsZero() {
			continue
		}
		accountIDs = append(accountIDs, id.Uint128())
	}
	accounts, err := c.tb.LookupAccounts(accountIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup accounts: %w", err)
	}
	out := make(map[ID]AccountSnapshot, len(accounts))
	for _, account := range accounts {
		id := IDFromUint128(account.ID)
		debitsPosted, err := uint128ToUint64(account.DebitsPosted)
		if err != nil {
			return nil, fmt.Errorf("account %s debits posted: %w", id.String(), err)
		}
		debitsPending, err := uint128ToUint64(account.DebitsPending)
		if err != nil {
			return nil, fmt.Errorf("account %s debits pending: %w", id.String(), err)
		}
		creditsPosted, err := uint128ToUint64(account.CreditsPosted)
		if err != nil {
			return nil, fmt.Errorf("account %s credits posted: %w", id.String(), err)
		}
		creditsPending, err := uint128ToUint64(account.CreditsPending)
		if err != nil {
			return nil, fmt.Errorf("account %s credits pending: %w", id.String(), err)
		}
		out[id] = AccountSnapshot{
			AccountID:      id,
			DebitsPosted:   debitsPosted,
			DebitsPending:  debitsPending,
			CreditsPosted:  creditsPosted,
			CreditsPending: creditsPending,
		}
	}
	return out, nil
}

func payloadAccounts(in []AccountPayload) ([]tbtypes.Account, error) {
	out := make([]tbtypes.Account, 0, len(in))
	for _, account := range in {
		id, err := ParseID(account.ID)
		if err != nil {
			return nil, err
		}
		userData128, err := ParseID(account.UserData128)
		if err != nil {
			return nil, err
		}
		if id.IsZero() || account.Ledger == 0 || account.Code == 0 {
			return nil, fmt.Errorf("%w: account missing id, ledger, or code", ErrInvalidCommand)
		}
		out = append(out, tbtypes.Account{
			ID:          id.Uint128(),
			UserData128: userData128.Uint128(),
			UserData64:  account.UserData64,
			UserData32:  account.UserData32,
			Ledger:      account.Ledger,
			Code:        account.Code,
			Flags:       account.Flags,
		})
	}
	return out, nil
}

func payloadTransfers(in []TransferPayload) ([]tbtypes.Transfer, error) {
	out := make([]tbtypes.Transfer, 0, len(in))
	for _, transfer := range in {
		id, err := ParseID(transfer.ID)
		if err != nil {
			return nil, err
		}
		debitID, err := ParseID(transfer.DebitAccountID)
		if err != nil {
			return nil, err
		}
		creditID, err := ParseID(transfer.CreditAccountID)
		if err != nil {
			return nil, err
		}
		pendingID, err := ParseID(transfer.PendingID)
		if err != nil {
			return nil, err
		}
		userData128, err := ParseID(transfer.UserData128)
		if err != nil {
			return nil, err
		}
		flags := transferFlagsFromUint16(transfer.Flags)
		if id.IsZero() {
			return nil, fmt.Errorf("%w: transfer id is required", ErrInvalidCommand)
		}
		if transfer.Amount == 0 && !flags.VoidPendingTransfer {
			return nil, fmt.Errorf("%w: transfer amount is required", ErrInvalidCommand)
		}
		if transfer.Ledger == 0 || transfer.Code == 0 {
			return nil, fmt.Errorf("%w: transfer ledger and code are required", ErrInvalidCommand)
		}
		if flags.Pending && !pendingID.IsZero() {
			return nil, fmt.Errorf("%w: pending transfer cannot reference pending_id", ErrInvalidCommand)
		}
		if (flags.PostPendingTransfer || flags.VoidPendingTransfer) && pendingID.IsZero() {
			return nil, fmt.Errorf("%w: post/void transfer requires pending_id", ErrInvalidCommand)
		}
		out = append(out, tbtypes.Transfer{
			ID:              id.Uint128(),
			DebitAccountID:  debitID.Uint128(),
			CreditAccountID: creditID.Uint128(),
			Amount:          tbtypes.ToUint128(transfer.Amount),
			PendingID:       pendingID.Uint128(),
			UserData128:     userData128.Uint128(),
			UserData64:      transfer.UserData64,
			UserData32:      transfer.UserData32,
			Timeout:         transfer.Timeout,
			Ledger:          transfer.Ledger,
			Code:            transfer.Code,
			Flags:           transfer.Flags,
		})
	}
	return out, nil
}

func transferFlagsFromUint16(raw uint16) tbtypes.TransferFlags {
	return tbtypes.Transfer{Flags: raw}.TransferFlags()
}

func (c *Client) createAccounts(ctx context.Context, operation string, accounts []tbtypes.Account) error {
	_, span := tracer.Start(ctx, "billing.ledger.create_accounts")
	defer span.End()
	span.SetAttributes(attribute.String("ledger.operation", operation), attribute.Int("ledger.batch_size", len(accounts)))
	results, err := c.tb.CreateAccounts(accounts)
	if err != nil {
		return fmt.Errorf("create accounts: %w", err)
	}
	for _, result := range results {
		switch result.Result {
		case tbtypes.AccountOK, tbtypes.AccountExists:
			continue
		case tbtypes.AccountExistsWithDifferentFlags,
			tbtypes.AccountExistsWithDifferentUserData128,
			tbtypes.AccountExistsWithDifferentUserData64,
			tbtypes.AccountExistsWithDifferentUserData32,
			tbtypes.AccountExistsWithDifferentLedger,
			tbtypes.AccountExistsWithDifferentCode:
			return fmt.Errorf("%w: account index %d result %s", ErrAccountConflict, result.Index, result.Result)
		default:
			return fmt.Errorf("create account index %d: %s", result.Index, result.Result)
		}
	}
	return nil
}

func (c *Client) createTransfers(ctx context.Context, operation string, transfers []tbtypes.Transfer) error {
	_, span := tracer.Start(ctx, "billing.ledger.create_transfers")
	defer span.End()
	span.SetAttributes(attribute.String("ledger.operation", operation), attribute.Int("ledger.batch_size", len(transfers)))
	results, err := c.tb.CreateTransfers(transfers)
	if err != nil {
		return fmt.Errorf("create transfers: %w", err)
	}
	for _, result := range results {
		switch result.Result {
		case tbtypes.TransferOK, tbtypes.TransferExists, tbtypes.TransferPendingTransferAlreadyPosted, tbtypes.TransferPendingTransferAlreadyVoided:
			continue
		case tbtypes.TransferExceedsCredits:
			return fmt.Errorf("%w: transfer index %d result %s", ErrInsufficientBalance, result.Index, result.Result)
		case tbtypes.TransferPendingTransferExpired, tbtypes.TransferPendingTransferNotFound, tbtypes.TransferPendingTransferNotPending:
			return fmt.Errorf("%w: transfer index %d result %s", ErrPendingTransferGone, result.Index, result.Result)
		case tbtypes.TransferExistsWithDifferentFlags,
			tbtypes.TransferExistsWithDifferentPendingID,
			tbtypes.TransferExistsWithDifferentTimeout,
			tbtypes.TransferExistsWithDifferentDebitAccountID,
			tbtypes.TransferExistsWithDifferentCreditAccountID,
			tbtypes.TransferExistsWithDifferentAmount,
			tbtypes.TransferExistsWithDifferentUserData128,
			tbtypes.TransferExistsWithDifferentUserData64,
			tbtypes.TransferExistsWithDifferentUserData32,
			tbtypes.TransferExistsWithDifferentLedger,
			tbtypes.TransferExistsWithDifferentCode:
			return fmt.Errorf("%w: transfer index %d result %s", ErrTransferConflict, result.Index, result.Result)
		default:
			return fmt.Errorf("create transfer index %d: %s", result.Index, result.Result)
		}
	}
	return nil
}

func availableFromAccount(account tbtypes.Account) (uint64, error) {
	creditsPosted, err := uint128ToUint64(account.CreditsPosted)
	if err != nil {
		return 0, err
	}
	debitsPosted, err := uint128ToUint64(account.DebitsPosted)
	if err != nil {
		return 0, err
	}
	debitsPending, err := uint128ToUint64(account.DebitsPending)
	if err != nil {
		return 0, err
	}
	if debitsPosted > math.MaxUint64-debitsPending {
		return 0, ErrBalanceOverflow
	}
	committed := debitsPosted + debitsPending
	if creditsPosted < committed {
		return 0, fmt.Errorf("%w: negative grant balance", ErrBalanceOverflow)
	}
	return creditsPosted - committed, nil
}

func uint128ToUint64(value tbtypes.Uint128) (uint64, error) {
	bytes := value.Bytes()
	for i := 8; i < 16; i++ {
		if bytes[i] != 0 {
			return 0, ErrBalanceOverflow
		}
	}
	return binary.LittleEndian.Uint64(bytes[0:8]), nil
}
