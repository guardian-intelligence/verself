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

	"github.com/verself/billing-service/internal/billing"
	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/envconfig"
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
	VoidedCycleCount       int    `json:"voided_cycle_count,omitempty"`
	ClosedGrantCount       int    `json:"closed_grant_count,omitempty"`
	ReassignedWindowCount  int    `json:"reassigned_window_count,omitempty"`
	CurrentCycleID         string `json:"current_cycle_id,omitempty"`
	PreviousBusinessNow    string `json:"previous_business_now,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var pgDSNFile, pgDSN, org, productID, setRaw, reason string
	var orgID uint64
	var advanceSeconds int64
	var clear, wallClock bool
	flag.StringVar(&pgDSNFile, "pg-dsn-file", "", "path to billing PG DSN")
	flag.StringVar(&pgDSN, "pg-dsn", "", "billing PG DSN")
	flag.Uint64Var(&orgID, "org-id", 0, "billing org id")
	flag.StringVar(&org, "org", "", "billing org id, display name, billing email, or platform shortcut")
	flag.StringVar(&productID, "product-id", "sandbox", "billing product id")
	flag.StringVar(&setRaw, "set", "", "set business time to RFC3339/RFC3339Nano")
	flag.Int64Var(&advanceSeconds, "advance-seconds", 0, "advance business time by seconds")
	flag.BoolVar(&clear, "clear", false, "clear org-product clock override")
	flag.BoolVar(&wallClock, "wall-clock", false, "clear override and repair current cycle around wall-clock time")
	flag.StringVar(&reason, "reason", "billing-clock", "operator reason")
	flag.Parse()
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
	if wallClock {
		selected++
	}
	if selected > 1 {
		return fmt.Errorf("choose only one of --set, --advance-seconds, --clear, or --wall-clock")
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
	resolvedOrgID, err := resolveOrgID(ctx, pg, orgID, org)
	if err != nil {
		return err
	}
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
		state, err = client.SetBusinessClock(ctx, billing.OrgID(resolvedOrgID), productID, businessNow, reason)
		if err != nil {
			return err
		}
	case advanceSeconds != 0:
		state, err = client.AdvanceBusinessClock(ctx, billing.OrgID(resolvedOrgID), productID, time.Duration(advanceSeconds)*time.Second, reason)
		if err != nil {
			return err
		}
	case clear:
		state, err = client.ClearBusinessClock(ctx, billing.OrgID(resolvedOrgID), productID, reason)
		if err != nil {
			return err
		}
	case wallClock:
		state, err = client.ResetBusinessClockToWallClock(ctx, billing.OrgID(resolvedOrgID), productID, reason)
		if err != nil {
			return err
		}
	default:
		state, err = client.GetBusinessClock(ctx, billing.OrgID(resolvedOrgID), productID)
		if err != nil {
			return err
		}
	}
	payload := output{
		OrgID:                  fmt.Sprintf("%d", state.OrgID),
		ProductID:              state.ProductID,
		ScopeKind:              state.ScopeKind,
		ScopeID:                state.ScopeID,
		BusinessNow:            state.BusinessNow.Format(time.RFC3339Nano),
		HasOverride:            state.HasOverride,
		Generation:             state.Generation,
		CyclesRolledOver:       state.DueWork.CyclesRolledOver,
		ContractChangesApplied: state.DueWork.ContractChangesApplied,
		EntitlementsEnsured:    state.DueWork.EntitlementsEnsured,
		VoidedCycleCount:       len(state.Repair.VoidedCycleIDs),
		ClosedGrantCount:       len(state.Repair.ClosedGrantIDs),
		ReassignedWindowCount:  len(state.Repair.ReassignedWindowIDs),
		CurrentCycleID:         state.Repair.CurrentCycleID,
	}
	if state.Repair.PreviousBusinessNow != nil {
		payload.PreviousBusinessNow = state.Repair.PreviousBusinessNow.Format(time.RFC3339Nano)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func resolveOrgID(ctx context.Context, pg *pgxpool.Pool, orgID uint64, org string) (uint64, error) {
	if orgID != 0 {
		return orgID, nil
	}
	org = strings.TrimSpace(org)
	if org == "" {
		return 0, fmt.Errorf("--org-id or --org is required")
	}
	if id, err := strconv.ParseUint(org, 10, 64); err == nil && id > 0 {
		return id, nil
	}
	predicate := `display_name = $1 OR metadata->>'org_key' = $1 OR billing_email = $1`
	args := []any{org}
	if org == "platform" {
		predicate = `trust_tier = 'platform'`
		args = nil
	}
	query := `SELECT org_id FROM orgs WHERE ` + predicate + ` ORDER BY created_at, org_id LIMIT 2`
	rows, err := pg.Query(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("resolve org %q: %w", org, err)
	}
	defer rows.Close()
	matches := []string{}
	for rows.Next() {
		var match string
		if err := rows.Scan(&match); err != nil {
			return 0, err
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(matches) == 0 {
		return 0, fmt.Errorf("org %q not found; pass --org-id", org)
	}
	if len(matches) > 1 {
		return 0, fmt.Errorf("org %q matched multiple billing orgs; pass --org-id", org)
	}
	parsed, err := strconv.ParseUint(matches[0], 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("resolved org id is not numeric: %q", matches[0])
	}
	return parsed, nil
}

func openLedgerClient() (*ledger.Client, error) {
	l := envconfig.New()
	address := l.String("BILLING_TB_ADDRESS", "127.0.0.1:3320")
	clusterID := l.Uint64("BILLING_TB_CLUSTER_ID", 0)
	if err := l.Err(); err != nil {
		return nil, err
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
