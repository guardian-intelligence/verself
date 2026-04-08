package billing

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// ensureSpendCapAccount creates the period-scoped spend-cap account and seeds
// it with the configured cap. Credits are pinned to capUnits; posted debits
// accumulate naturally over the billing period.
func (c *Client) ensureSpendCapAccount(ctx context.Context, orgID OrgID, productID string, periodStart time.Time, capUnits uint64) error {
	acctID := SpendCapAccountID(orgID, productID, periodStart)
	sinkID := OperatorAccountID(AcctQuotaSink)

	if err := c.createAccounts([]types.Account{{
		ID:         acctID.raw,
		UserData64: uint64(orgID),
		Ledger:     1,
		Code:       AcctSpendCapCode,
		Flags:      types.AccountFlags{DebitsMustNotExceedCredits: true}.ToUint16(),
	}}); err != nil {
		return fmt.Errorf("create spend cap account: %w", err)
	}

	accounts, err := c.tb.LookupAccounts([]types.Uint128{acctID.raw})
	if err != nil {
		return fmt.Errorf("lookup spend cap account: %w", err)
	}
	if len(accounts) != 1 {
		return fmt.Errorf("lookup spend cap account: expected 1 account, got %d", len(accounts))
	}

	currentCredits, err := uint128ToUint64(accounts[0].CreditsPosted)
	if err != nil {
		return fmt.Errorf("spend cap credits: %w", err)
	}
	if currentCredits >= capUnits {
		return nil
	}

	results, err := c.tb.CreateTransfers([]types.Transfer{{
		ID:              spendCapCreditTransferID(acctID, capUnits).raw,
		DebitAccountID:  sinkID.raw,
		CreditAccountID: acctID.raw,
		Amount:          types.ToUint128(capUnits - currentCredits),
		Ledger:          1,
		Code:            uint16(KindSpendCapCheck),
	}})
	if err != nil {
		return fmt.Errorf("credit spend cap: %w", err)
	}
	for _, result := range results {
		if result.Result != types.TransferOK && result.Result != types.TransferExists {
			return fmt.Errorf("credit spend cap: %s", result.Result)
		}
	}
	return nil
}

func (c *Client) reserveGrantWaterfall(
	ctx context.Context,
	jobID JobID,
	orgID OrgID,
	productID string,
	windowSeq uint32,
	phase PricingPhase,
	windowCost uint64,
	grants []grantBalance,
	spendCap spendCapPolicy,
	trustTier string,
) ([]GrantLeg, error) {
	if windowCost == 0 {
		return nil, nil
	}

	sort.Slice(grants, func(i, j int) bool {
		left := grants[i]
		right := grants[j]
		switch {
		case left.expiresAt == nil && right.expiresAt != nil:
			return false
		case left.expiresAt != nil && right.expiresAt == nil:
			return true
		case left.expiresAt != nil && right.expiresAt != nil && !left.expiresAt.Equal(*right.expiresAt):
			return left.expiresAt.Before(*right.expiresAt)
		default:
			return bytes.Compare(left.grantID[:], right.grantID[:]) < 0
		}
	})

	transfers := make([]types.Transfer, 0, len(grants)+2)
	if spendCap.effectiveCapUnits != nil {
		if spendCap.periodStart == nil {
			return nil, fmt.Errorf("spend cap period start is required")
		}
		if err := c.ensureSpendCapAccount(ctx, orgID, productID, *spendCap.periodStart, *spendCap.effectiveCapUnits); err != nil {
			return nil, err
		}

		capAcctID := SpendCapAccountID(orgID, productID, *spendCap.periodStart)
		sinkID := OperatorAccountID(AcctQuotaSink)
		probeID := SpendCapTransferID(jobID, windowSeq, KindSpendCapCheck)

		transfers = append(transfers,
			types.Transfer{
				ID:              probeID.raw,
				DebitAccountID:  capAcctID.raw,
				CreditAccountID: sinkID.raw,
				Amount:          types.ToUint128(windowCost),
				Timeout:         c.cfg.PendingTimeoutSecs,
				Ledger:          1,
				Code:            uint16(KindSpendCapCheck),
				Flags:           types.TransferFlags{Pending: true}.ToUint16(),
			},
			types.Transfer{
				ID:        SpendCapTransferID(jobID, windowSeq, KindVoid).raw,
				PendingID: probeID.raw,
				Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
			},
		)
	}

	phaseSinkAccount := phaseSinkAccountID(phase)
	remainder := windowCost
	grantLegs := make([]GrantLeg, 0, len(grants))

	for i, grant := range grants {
		if remainder == 0 {
			break
		}
		if i > math.MaxUint8 {
			return nil, fmt.Errorf("grant leg index %d exceeds max supported tigerbeetle grant_idx", i)
		}

		amount := minUint64(grant.available, remainder)
		if amount == 0 {
			continue
		}

		transferID := VMTransferID(jobID, windowSeq, uint8(i), KindReservation)
		transfers = append(transfers, types.Transfer{
			ID:              transferID.raw,
			DebitAccountID:  GrantAccountID(grant.grantID).raw,
			CreditAccountID: phaseSinkAccount.raw,
			Amount:          types.ToUint128(amount),
			Ledger:          1,
			Code:            uint16(KindReservation),
			Flags:           types.TransferFlags{Pending: true}.ToUint16(),
			UserData64:      uint64(orgID),
			UserData32:      windowSeq,
			Timeout:         c.cfg.PendingTimeoutSecs,
		})
		grantLegs = append(grantLegs, GrantLeg{
			GrantID:    grant.grantID,
			TransferID: transferID,
			Amount:     amount,
			Source:     grant.source,
		})
		remainder -= amount
	}

	if remainder > 0 {
		return nil, ErrInsufficientBalance
	}
	for i := 0; i < len(transfers)-1; i++ {
		transfers[i].Flags |= types.TransferFlags{Linked: true}.ToUint16()
	}

	results, err := c.tb.CreateTransfers(transfers)
	if err != nil {
		return nil, fmt.Errorf("reserve linked batch: %w", err)
	}
	if err := c.translateReserveBatchResults(ctx, orgID, productID, trustTier, spendCap, windowCost, results); err != nil {
		return nil, err
	}

	return grantLegs, nil
}

func (c *Client) translateReserveBatchResults(
	ctx context.Context,
	orgID OrgID,
	productID string,
	trustTier string,
	spendCap spendCapPolicy,
	windowCost uint64,
	results []types.TransferEventResult,
) error {
	spendCapFailed := false
	grantBalanceFailed := false
	spendCapApplied := spendCap.effectiveCapUnits != nil

	for _, result := range results {
		if result.Result != types.TransferExceedsCredits {
			continue
		}
		if spendCapApplied && result.Index == 0 {
			spendCapFailed = true
			continue
		}
		grantBalanceFailed = true
	}

	if spendCapFailed {
		c.recordSpendCapExceededEvent(ctx, orgID, productID, trustTier, spendCap, windowCost)
		return ErrSpendCapExceeded
	}
	if grantBalanceFailed {
		return ErrInsufficientBalance
	}

	for _, result := range results {
		switch result.Result {
		case types.TransferOK, types.TransferExists:
			continue
		case types.TransferLinkedEventFailed:
			return fmt.Errorf("reserve linked batch: transfer %d: %s", result.Index, result.Result)
		case types.TransferExistsWithDifferentFlags,
			types.TransferExistsWithDifferentPendingID,
			types.TransferExistsWithDifferentTimeout,
			types.TransferExistsWithDifferentDebitAccountID,
			types.TransferExistsWithDifferentCreditAccountID,
			types.TransferExistsWithDifferentAmount,
			types.TransferExistsWithDifferentUserData128,
			types.TransferExistsWithDifferentUserData64,
			types.TransferExistsWithDifferentUserData32,
			types.TransferExistsWithDifferentLedger,
			types.TransferExistsWithDifferentCode:
			return fmt.Errorf("reserve linked batch: transfer %d: %s", result.Index, result.Result)
		default:
			return fmt.Errorf("reserve linked batch: transfer %d: %s", result.Index, result.Result)
		}
	}

	return nil
}

func (c *Client) recordSpendCapExceededEvent(
	ctx context.Context,
	orgID OrgID,
	productID string,
	trustTier string,
	spendCap spendCapPolicy,
	windowCost uint64,
) {
	_, _ = c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, payload)
		VALUES ($1, 'spend_cap_exceeded', $2::jsonb)
	`,
		strconv.FormatUint(uint64(orgID), 10),
		mustJSON(map[string]any{
			"product_id":           productID,
			"trust_tier":           trustTier,
			"limit_source":         spendCapSource(spendCap.effectiveCapUnits, spendCap.trustTierCapUnits, spendCap.planCapUnits),
			"trust_tier_cap_units": spendCap.trustTierCapUnits,
			"plan_cap_units":       spendCap.planCapUnits,
			"effective_cap_units":  spendCap.effectiveCapUnits,
			"window_cost":          windowCost,
			"period_start":         spendCap.periodStart,
		}),
	)
}

func spendCapCreditTransferID(acctID AccountID, capUnits uint64) TransferID {
	b := acctID.raw.Bytes()
	var id [16]byte
	copy(id[0:8], b[0:8])
	binary.LittleEndian.PutUint64(id[8:16], capUnits)
	return TransferID{raw: types.BytesToUint128(id)}
}
