package billing

import (
	"encoding/binary"
	"fmt"

	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// availableFromAccount returns the maximum new debit TigerBeetle will accept.
// All forge-metal accounts set DebitsMustNotExceedCredits, so TigerBeetle enforces:
//
//	debits_pending + debits_posted + transfer.amount <= credits_posted
//
// Available = credits_posted - debits_posted - debits_pending.
// credits_pending is excluded: pending credits are not yet posted and cannot
// back new debits.
func availableFromAccount(account types.Account) (uint64, error) {
	creditsPosted, err := uint128ToUint64(account.CreditsPosted)
	if err != nil {
		return 0, fmt.Errorf("credits_posted: %w", err)
	}
	debitsPosted, err := uint128ToUint64(account.DebitsPosted)
	if err != nil {
		return 0, fmt.Errorf("debits_posted: %w", err)
	}
	debitsPending, err := uint128ToUint64(account.DebitsPending)
	if err != nil {
		return 0, fmt.Errorf("debits_pending: %w", err)
	}

	committed, err := safeAddUint64(debitsPosted, debitsPending)
	if err != nil {
		return 0, err
	}
	if creditsPosted < committed {
		return 0, fmt.Errorf("account has negative available balance")
	}

	return creditsPosted - committed, nil
}

// pendingFromAccount returns the total amount reserved by pending debit transfers.
func pendingFromAccount(account types.Account) (uint64, error) {
	return uint128ToUint64(account.DebitsPending)
}

// consumedFromAccount returns the posted debit total for a grant account.
func consumedFromAccount(account types.Account) (uint64, error) {
	return uint128ToUint64(account.DebitsPosted)
}

func safeAddUint64(a, b uint64) (uint64, error) {
	sum := a + b
	if sum < a {
		return 0, fmt.Errorf("uint64 overflow")
	}
	return sum, nil
}

func uint128ToUint64(v types.Uint128) (uint64, error) {
	b := v.Bytes()
	for i := 8; i < 16; i++ {
		if b[i] != 0 {
			return 0, fmt.Errorf("uint128 overflow")
		}
	}
	return binary.LittleEndian.Uint64(b[0:8]), nil
}
