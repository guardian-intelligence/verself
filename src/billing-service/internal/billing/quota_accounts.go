package billing

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// ensureQuotaAccount creates a quota account in TigerBeetle if it doesn't exist,
// and credits it with the limit value. The account uses DebitsMustNotExceedCredits
// so that pending transfers fail atomically when the quota is exhausted.
func (c *Client) ensureQuotaAccount(ctx context.Context, orgID OrgID, productID, dimension, window string, limit uint64) error {
	acctID := QuotaAccountID(orgID, productID, dimension, window)
	sinkID := OperatorAccountID(AcctQuotaSink)

	// Create the quota account (idempotent — AccountExists is OK).
	if err := c.createAccounts([]types.Account{{
		ID:         acctID.raw,
		UserData64: uint64(orgID),
		Ledger:     1,
		Code:       AcctQuotaCode,
		Flags:      types.AccountFlags{DebitsMustNotExceedCredits: true}.ToUint16(),
	}}); err != nil {
		return fmt.Errorf("create quota account: %w", err)
	}

	// Check current credit balance. If already at the right level, skip.
	accounts, err := c.tb.LookupAccounts([]types.Uint128{acctID.raw})
	if err != nil {
		return fmt.Errorf("lookup quota account: %w", err)
	}
	if len(accounts) == 0 {
		return fmt.Errorf("quota account not found after creation")
	}

	currentCredits, err := uint128ToUint64(accounts[0].CreditsPosted)
	if err != nil {
		return fmt.Errorf("quota credits: %w", err)
	}

	if currentCredits == limit {
		return nil // already at correct level
	}

	// Credit the account to reach the target limit.
	// Use a deterministic transfer ID based on the account ID + limit to be idempotent.
	if currentCredits < limit {
		delta := limit - currentCredits
		xferID := quotaCreditTransferID(acctID, limit)
		results, err := c.tb.CreateTransfers([]types.Transfer{{
			ID:              xferID.raw,
			DebitAccountID:  sinkID.raw,
			CreditAccountID: acctID.raw,
			Amount:          types.ToUint128(delta),
			Ledger:          1,
			Code:            uint16(KindQuotaCheck),
		}})
		if err != nil {
			return fmt.Errorf("credit quota account: %w", err)
		}
		for _, r := range results {
			if r.Result != types.TransferOK && r.Result != types.TransferExists {
				return fmt.Errorf("credit quota account: %s", r.Result)
			}
		}
	}

	return nil
}

// attemptQuotaTransfers attempts pending transfers on quota accounts for all
// TB-enforceable limits. Returns violations for any that fail due to
// TransferExceedsCredits. If any violations are found, successful quota
// transfers are voided to avoid consuming quota slots for rejected jobs.
func (c *Client) attemptQuotaTransfers(ctx context.Context, orgID OrgID, productID string, limits []quotaLimit, usage map[string]float64, now time.Time) ([]QuotaViolation, error) {
	if len(limits) == 0 {
		return nil, nil
	}

	// Ensure all quota accounts exist.
	for _, lim := range limits {
		if err := c.ensureQuotaAccount(ctx, orgID, productID, lim.Dimension, lim.Window, lim.Limit); err != nil {
			return nil, fmt.Errorf("ensure quota account %s/%s: %w", lim.Dimension, lim.Window, err)
		}
	}

	sinkID := OperatorAccountID(AcctQuotaSink)
	nanoTS := now.UnixNano()

	// Build batch of pending transfers.
	transfers := make([]types.Transfer, len(limits))
	for i, lim := range limits {
		acctID := QuotaAccountID(orgID, productID, lim.Dimension, lim.Window)
		amount := uint64(math.Ceil(usage[lim.Dimension]))
		if amount == 0 {
			amount = 1
		}

		timeout := windowTimeoutSeconds(lim.Window, c.cfg.PendingTimeoutSecs)

		transfers[i] = types.Transfer{
			ID:              QuotaTransferID(orgID, lim.Dimension, nanoTS+int64(i)).raw,
			DebitAccountID:  acctID.raw,
			CreditAccountID: sinkID.raw,
			Amount:          types.ToUint128(amount),
			Timeout:         timeout,
			Ledger:          1,
			Code:            uint16(KindQuotaCheck),
			Flags:           types.TransferFlags{Pending: true}.ToUint16(),
		}
	}

	// Submit batch.
	results, err := c.tb.CreateTransfers(transfers)
	if err != nil {
		return nil, fmt.Errorf("create quota transfers: %w", err)
	}

	// Map failures to violations. Track which transfers succeeded.
	failedIndices := make(map[int]bool)
	var violations []QuotaViolation

	for _, r := range results {
		idx := int(r.Index)
		if r.Result == types.TransferExceedsCredits {
			failedIndices[idx] = true
			lim := limits[idx]
			violations = append(violations, QuotaViolation{
				Dimension: lim.Dimension,
				Window:    lim.Window,
				Limit:     lim.Limit,
				Current:   lim.Limit, // at capacity
			})
		} else if r.Result != types.TransferOK {
			return nil, fmt.Errorf("quota transfer %d: %s", idx, r.Result)
		}
	}

	// If any violations, void ALL successful quota transfers.
	if len(violations) > 0 {
		var voidTransfers []types.Transfer
		for i := range transfers {
			if !failedIndices[i] {
				voidTransfers = append(voidTransfers, types.Transfer{
					ID:        QuotaTransferID(orgID, limits[i].Dimension, nanoTS+int64(i)+int64(len(limits))).raw,
					PendingID: transfers[i].ID,
					Flags:     types.TransferFlags{VoidPendingTransfer: true}.ToUint16(),
				})
			}
		}
		if len(voidTransfers) > 0 {
			if _, err := c.tb.CreateTransfers(voidTransfers); err != nil {
				return nil, fmt.Errorf("void quota transfers after violation: %w", err)
			}
		}
	}

	return violations, nil
}

// windowTimeoutSeconds returns the TigerBeetle pending transfer timeout for
// a quota window type.
func windowTimeoutSeconds(window string, fallback uint32) uint32 {
	switch window {
	case "instant":
		return fallback // same as billing reservation timeout
	case "hour":
		return 3600
	case "4h":
		return 14400
	default:
		return fallback
	}
}

// ensureOverageCapAccount creates an overage cap account in TigerBeetle and
// credits it with the cap amount. This account tracks how much overage has been
// consumed in the current period. Settlement posts permanent debits; when the
// balance reaches zero, the next reservation's cap check fails atomically.
func (c *Client) ensureOverageCapAccount(ctx context.Context, orgID OrgID, productID string, capUnits uint64) error {
	acctID := OverageCapAccountID(orgID, productID)
	sinkID := OperatorAccountID(AcctQuotaSink)

	if err := c.createAccounts([]types.Account{{
		ID:         acctID.raw,
		UserData64: uint64(orgID),
		Ledger:     1,
		Code:       AcctOverageCapCode,
		Flags:      types.AccountFlags{DebitsMustNotExceedCredits: true}.ToUint16(),
	}}); err != nil {
		return fmt.Errorf("create overage cap account: %w", err)
	}

	// Check current credit balance and adjust to match cap.
	accounts, err := c.tb.LookupAccounts([]types.Uint128{acctID.raw})
	if err != nil || len(accounts) == 0 {
		return fmt.Errorf("lookup overage cap account: %w", err)
	}

	currentCredits, err := uint128ToUint64(accounts[0].CreditsPosted)
	if err != nil {
		return fmt.Errorf("overage cap credits: %w", err)
	}

	// Credits should equal the cap (set once per period). As Settle posts
	// real debits (KindOverageCapDebit), available decreases naturally.
	if currentCredits == capUnits {
		return nil
	}

	if currentCredits < capUnits {
		delta := capUnits - currentCredits
		xferID := quotaCreditTransferID(acctID, capUnits)
		results, err := c.tb.CreateTransfers([]types.Transfer{{
			ID:              xferID.raw,
			DebitAccountID:  sinkID.raw,
			CreditAccountID: acctID.raw,
			Amount:          types.ToUint128(delta),
			Ledger:          1,
			Code:            uint16(KindOverageCapCheck),
		}})
		if err != nil {
			return fmt.Errorf("credit overage cap: %w", err)
		}
		for _, r := range results {
			if r.Result != types.TransferOK && r.Result != types.TransferExists {
				return fmt.Errorf("credit overage cap: %s", r.Result)
			}
		}
	}

	return nil
}

// enforceOverageCapTB checks overage cap via TigerBeetle balance-conditional
// pending transfer. Returns ErrOverageCeilingExceeded if the cap would be
// breached. The pending transfer is stored in the Reservation so Settle can
// void it and post the real debit.
func (c *Client) enforceOverageCapTB(ctx context.Context, orgID OrgID, productID string, windowCost uint64, jobID JobID, windowSeq uint32) (*GrantLeg, error) {
	cap, _, err := c.loadOverageCap(ctx, orgID, productID)
	if err != nil {
		return nil, err
	}
	if cap == nil {
		return nil, nil // no cap configured
	}

	capUnits := uint64(*cap)
	if err := c.ensureOverageCapAccount(ctx, orgID, productID, capUnits); err != nil {
		return nil, err
	}

	acctID := OverageCapAccountID(orgID, productID)
	sinkID := OperatorAccountID(AcctQuotaSink)
	xferID := OverageCapTransferID(jobID, windowSeq, KindOverageCapCheck)

	results, err := c.tb.CreateTransfers([]types.Transfer{{
		ID:              xferID.raw,
		DebitAccountID:  acctID.raw,
		CreditAccountID: sinkID.raw,
		Amount:          types.ToUint128(windowCost),
		Timeout:         c.cfg.PendingTimeoutSecs,
		Ledger:          1,
		Code:            uint16(KindOverageCapCheck),
		Flags:           types.TransferFlags{Pending: true}.ToUint16(),
	}})
	if err != nil {
		return nil, fmt.Errorf("overage cap check: %w", err)
	}

	for _, r := range results {
		if r.Result == types.TransferExceedsCredits {
			orgIDStr := fmt.Sprintf("%d", orgID)
			_, _ = c.pg.ExecContext(ctx, `
				INSERT INTO billing_events (org_id, event_type, payload)
				VALUES ($1, 'overage_ceiling_hit', $2::jsonb)
			`, orgIDStr, mustJSON(map[string]interface{}{
				"product_id":  productID,
				"cap_units":   capUnits,
				"window_cost": windowCost,
			}))
			return nil, ErrOverageCeilingExceeded
		}
		if r.Result != types.TransferOK {
			return nil, fmt.Errorf("overage cap check: %s", r.Result)
		}
	}

	// Return the cap check transfer as a "leg" so Settle can void it.
	return &GrantLeg{
		TransferID: xferID,
		Amount:     windowCost,
	}, nil
}

// quotaCreditTransferID builds a deterministic transfer ID for crediting a
// quota account to a specific limit. Idempotent: same account + same limit
// = same transfer ID.
func quotaCreditTransferID(acctID AccountID, limit uint64) TransferID {
	b := acctID.raw.Bytes()
	var id [16]byte
	copy(id[0:8], b[0:8])
	binary.LittleEndian.PutUint64(id[8:16], limit)
	return TransferID{raw: types.BytesToUint128(id)}
}
