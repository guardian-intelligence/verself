package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forge-metal/billing-service/internal/billing"
	"github.com/forge-metal/billing-service/internal/billing/ledger"
)

type output struct {
	OrgID                  string `json:"org_id"`
	ProductID              string `json:"product_id"`
	ScopeKind              string `json:"scope_kind"`
	ScopeID                string `json:"scope_id"`
	BusinessNow            string `json:"business_now"`
	HasOverride            bool   `json:"has_override"`
	Generation             uint64 `json:"generation"`
	CyclesRolledOver       uint64 `json:"cycles_rolled_over"`
	ContractChangesApplied uint64 `json:"contract_changes_applied"`
	EntitlementsEnsured    uint64 `json:"entitlements_ensured"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var pgDSNFile, pgDSN, productID, setRaw, reason string
	var orgID uint64
	var advanceSeconds int64
	var clear bool
	flag.StringVar(&pgDSNFile, "pg-dsn-file", "", "path to billing PG DSN")
	flag.StringVar(&pgDSN, "pg-dsn", "", "billing PG DSN")
	flag.Uint64Var(&orgID, "org-id", 0, "billing org id")
	flag.StringVar(&productID, "product-id", "sandbox", "billing product id")
	flag.StringVar(&setRaw, "set", "", "set business time to RFC3339/RFC3339Nano")
	flag.Int64Var(&advanceSeconds, "advance-seconds", 0, "advance business time by seconds")
	flag.BoolVar(&clear, "clear", false, "clear org-product clock override")
	flag.StringVar(&reason, "reason", "billing-clock", "operator reason")
	flag.Parse()
	if orgID == 0 {
		return fmt.Errorf("--org-id is required")
	}
	selected := 0
	if setRaw != "" {
		selected++
	}
	if advanceSeconds != 0 {
		selected++
	}
	if clear {
		selected++
	}
	if selected > 1 {
		return fmt.Errorf("choose only one of --set, --advance-seconds, or --clear")
	}
	dsn, err := resolvePGDSN(pgDSN, pgDSNFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pg, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()
	ledgerClient, err := openLedgerClient()
	if err != nil {
		return err
	}
	defer ledgerClient.Close()
	client, err := billing.NewClient(pg, nil, nil, billing.Config{UseStripe: false}, slog.Default(), ledgerClient)
	if err != nil {
		return err
	}
	if err := client.EnsureLedgerBootstrapped(ctx); err != nil {
		return err
	}
	var state billing.BusinessClockState
	switch {
	case setRaw != "":
		businessNow, err := parseTime(setRaw)
		if err != nil {
			return err
		}
		state, err = client.SetBusinessClock(ctx, billing.OrgID(orgID), productID, businessNow, reason)
		if err != nil {
			return err
		}
	case advanceSeconds != 0:
		state, err = client.AdvanceBusinessClock(ctx, billing.OrgID(orgID), productID, time.Duration(advanceSeconds)*time.Second, reason)
		if err != nil {
			return err
		}
	case clear:
		state, err = client.ClearBusinessClock(ctx, billing.OrgID(orgID), productID, reason)
		if err != nil {
			return err
		}
	default:
		state, err = client.GetBusinessClock(ctx, billing.OrgID(orgID), productID)
		if err != nil {
			return err
		}
	}
	payload := output{OrgID: fmt.Sprintf("%d", state.OrgID), ProductID: state.ProductID, ScopeKind: state.ScopeKind, ScopeID: state.ScopeID, BusinessNow: state.BusinessNow.Format(time.RFC3339Nano), HasOverride: state.HasOverride, Generation: state.Generation, CyclesRolledOver: state.DueWork.CyclesRolledOver, ContractChangesApplied: state.DueWork.ContractChangesApplied, EntitlementsEnsured: state.DueWork.EntitlementsEnsured}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func openLedgerClient() (*ledger.Client, error) {
	address := strings.TrimSpace(os.Getenv("BILLING_TB_ADDRESS"))
	if address == "" {
		address = "127.0.0.1:3320"
	}
	clusterID := uint64(0)
	if raw := strings.TrimSpace(os.Getenv("BILLING_TB_CLUSTER_ID")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse BILLING_TB_CLUSTER_ID: %w", err)
		}
		clusterID = parsed
	}
	return ledger.NewClient(clusterID, strings.Split(address, ","))
}

func resolvePGDSN(value, file string) (string, error) {
	if value != "" {
		return value, nil
	}
	if file == "" {
		return "", fmt.Errorf("--pg-dsn or --pg-dsn-file is required")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read pg dsn file: %w", err)
	}
	dsn := strings.TrimSpace(string(data))
	if dsn == "" {
		return "", fmt.Errorf("pg dsn file is empty")
	}
	return dsn, nil
}

func parseTime(raw string) (time.Time, error) {
	value, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse --set as RFC3339Nano: %w", err)
	}
	return value.UTC(), nil
}
