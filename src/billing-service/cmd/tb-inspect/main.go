package main

import (
	"fmt"
	"os"

	"github.com/oklog/ulid/v2"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"

	"github.com/forge-metal/billing"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: tb-inspect <grant-ulid>\n")
		os.Exit(1)
	}

	parsedULID, err := ulid.ParseStrict(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse ULID: %v\n", err)
		os.Exit(1)
	}

	addr := os.Getenv("TB_ADDRESS")
	if addr == "" {
		addr = "127.0.0.1:3320"
	}

	client, err := tb.NewClient(types.ToUint128(0), []string{addr})
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect TB: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	grantID := billing.GrantID(parsedULID)
	accountID := billing.GrantAccountID(grantID)

	// Lookup account
	accounts, err := client.LookupAccounts([]types.Uint128{accountID.Raw()})
	if err != nil {
		fmt.Fprintf(os.Stderr, "lookup account: %v\n", err)
		os.Exit(1)
	}

	if len(accounts) == 0 {
		fmt.Println("Account: NOT FOUND")
	} else {
		a := accounts[0]
		fmt.Printf("Account:\n")
		fmt.Printf("  ID:              %v\n", a.ID)
		fmt.Printf("  Code:            %d\n", a.Code)
		fmt.Printf("  Ledger:          %d\n", a.Ledger)
		fmt.Printf("  UserData64:      %d (orgID)\n", a.UserData64)
		fmt.Printf("  UserData32:      %d (sourceType)\n", a.UserData32)
		fmt.Printf("  CreditsPosted:   %v\n", a.CreditsPosted)
		fmt.Printf("  CreditsPending:  %v\n", a.CreditsPending)
		fmt.Printf("  DebitsPosted:    %v\n", a.DebitsPosted)
		fmt.Printf("  DebitsPending:   %v\n", a.DebitsPending)
		fmt.Printf("  Flags:           0x%04x\n", a.Flags)
	}

	// Lookup transfers for this account
	filter := types.AccountFilter{
		AccountID:    accountID.Raw(),
		TimestampMin: 0,
		TimestampMax: 0, // 0 = no upper bound
		Limit:        100,
		Flags: types.AccountFilterFlags{
			Credits:  true,
			Debits:   true,
			Reversed: false,
		}.ToUint32(),
	}
	transfers, err := client.GetAccountTransfers(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get transfers: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nTransfers (%d):\n", len(transfers))
	for i, t := range transfers {
		flagStr := fmt.Sprintf("0x%04x", t.Flags)
		if t.Flags&(1<<0) != 0 {
			flagStr += " LINKED"
		}
		if t.Flags&(1<<1) != 0 {
			flagStr += " PENDING"
		}
		if t.Flags&(1<<2) != 0 {
			flagStr += " POST"
		}
		if t.Flags&(1<<3) != 0 {
			flagStr += " VOID"
		}
		if t.Flags&(1<<4) != 0 {
			flagStr += " BALANCING_DEBIT"
		}
		if t.Flags&(1<<5) != 0 {
			flagStr += " BALANCING_CREDIT"
		}
		fmt.Printf("  [%d] ID=%v\n", i, t.ID)
		fmt.Printf("      Code=%d Flags=%s\n", t.Code, flagStr)
		fmt.Printf("      Amount=%v\n", t.Amount)
		fmt.Printf("      Debit=%v Credit=%v\n", t.DebitAccountID, t.CreditAccountID)
		if t.PendingID != (types.Uint128{}) {
			fmt.Printf("      PendingID=%v\n", t.PendingID)
		}
		if t.Timeout > 0 {
			fmt.Printf("      Timeout=%d\n", t.Timeout)
		}
	}

	// Also show StripeHolding and Revenue operator accounts
	stripeHolding := billing.OperatorAccountID(billing.AcctStripeHolding)
	revenue := billing.OperatorAccountID(billing.AcctRevenue)
	opAccts, err := client.LookupAccounts([]types.Uint128{stripeHolding.Raw(), revenue.Raw()})
	if err != nil {
		fmt.Fprintf(os.Stderr, "lookup operator accounts: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nOperator Accounts:\n")
	for _, a := range opAccts {
		name := "unknown"
		switch a.Code {
		case uint16(billing.AcctStripeHolding):
			name = "StripeHolding"
		case uint16(billing.AcctRevenue):
			name = "Revenue"
		}
		fmt.Printf("  %s (code=%d):\n", name, a.Code)
		fmt.Printf("    CreditsPosted=%v CreditsPending=%v\n", a.CreditsPosted, a.CreditsPending)
		fmt.Printf("    DebitsPosted=%v  DebitsPending=%v\n", a.DebitsPosted, a.DebitsPending)
	}
}
