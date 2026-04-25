package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/envconfig"
)

type accountRow struct {
	AccountKey     string `json:"account_key,omitempty"`
	GrantID        string `json:"grant_id,omitempty"`
	OrgID          string `json:"org_id,omitempty"`
	ProductID      string `json:"product_id,omitempty"`
	Source         string `json:"source,omitempty"`
	AccountID      string `json:"account_id"`
	Available      uint64 `json:"available,omitempty"`
	Pending        uint64 `json:"pending,omitempty"`
	Spent          uint64 `json:"spent,omitempty"`
	DebitsPosted   uint64 `json:"debits_posted,omitempty"`
	DebitsPending  uint64 `json:"debits_pending,omitempty"`
	CreditsPosted  uint64 `json:"credits_posted,omitempty"`
	CreditsPending uint64 `json:"credits_pending,omitempty"`
}

type output struct {
	Accounts []accountRow `json:"accounts"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	defaults := envconfig.New()
	defaultTBAddress := defaults.String("BILLING_TB_ADDRESS", "127.0.0.1:3320")
	defaultClusterID := defaults.Uint64("BILLING_TB_CLUSTER_ID", 0)
	if err := defaults.Err(); err != nil {
		return err
	}

	var pgDSN, pgDSNFile, tbAddress, orgID, grantID string
	var clusterID uint64
	flag.StringVar(&pgDSN, "pg-dsn", "", "billing PostgreSQL DSN")
	flag.StringVar(&pgDSNFile, "pg-dsn-file", "", "file containing billing PostgreSQL DSN")
	flag.StringVar(&tbAddress, "tb-address", defaultTBAddress, "comma-separated TigerBeetle addresses")
	flag.Uint64Var(&clusterID, "tb-cluster-id", defaultClusterID, "TigerBeetle cluster ID")
	flag.StringVar(&orgID, "org-id", "", "optional org ID; defaults to operator accounts")
	flag.StringVar(&grantID, "grant-id", "", "optional credit grant ID")
	flag.Parse()

	dsn, err := resolvePGDSN(pgDSN, pgDSNFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pg, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()
	tb, err := ledger.NewClient(clusterID, strings.Split(tbAddress, ","))
	if err != nil {
		return err
	}
	defer tb.Close()

	rows, ids, err := loadAccounts(ctx, pg, orgID, grantID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(orgID) == "" && strings.TrimSpace(grantID) == "" {
		snapshots, err := tb.LookupAccountSnapshots(ctx, ids)
		if err != nil {
			return err
		}
		for i := range rows {
			id, err := ledger.ParseID(rows[i].AccountID)
			if err != nil {
				return err
			}
			snapshot, ok := snapshots[id]
			if !ok {
				return fmt.Errorf("%w: account %s", ledger.ErrAccountNotFound, rows[i].AccountID)
			}
			rows[i].DebitsPosted = snapshot.DebitsPosted
			rows[i].DebitsPending = snapshot.DebitsPending
			rows[i].CreditsPosted = snapshot.CreditsPosted
			rows[i].CreditsPending = snapshot.CreditsPending
		}
	} else {
		balances, err := tb.LookupBalances(ctx, ids)
		if err != nil {
			return err
		}
		for i := range rows {
			id, err := ledger.ParseID(rows[i].AccountID)
			if err != nil {
				return err
			}
			balance, ok := balances[id]
			if !ok {
				return fmt.Errorf("%w: account %s", ledger.ErrAccountNotFound, rows[i].AccountID)
			}
			rows[i].Available = balance.Available
			rows[i].Pending = balance.Pending
			rows[i].Spent = balance.Spent
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output{Accounts: rows})
}

func loadAccounts(ctx context.Context, pg *pgxpool.Pool, orgID, grantID string) ([]accountRow, []ledger.ID, error) {
	query := `
		SELECT account_key, '', '', '', '', encode(account_id, 'hex')
		FROM billing_ledger_accounts
		ORDER BY account_key
	`
	args := []any{}
	if strings.TrimSpace(grantID) != "" {
		query = `
			SELECT '', grant_id, org_id, COALESCE(scope_product_id,''), source, encode(account_id, 'hex')
			FROM credit_grants
			WHERE grant_id = $1
			ORDER BY starts_at, grant_id
		`
		args = append(args, grantID)
	} else if strings.TrimSpace(orgID) != "" {
		query = `
			SELECT '', grant_id, org_id, COALESCE(scope_product_id,''), source, encode(account_id, 'hex')
			FROM credit_grants
			WHERE org_id = $1 AND closed_at IS NULL
			ORDER BY starts_at, grant_id
		`
		args = append(args, orgID)
	}
	pgRows, err := pg.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query ledger accounts: %w", err)
	}
	defer pgRows.Close()
	rows := []accountRow{}
	ids := []ledger.ID{}
	for pgRows.Next() {
		var row accountRow
		if err := pgRows.Scan(&row.AccountKey, &row.GrantID, &row.OrgID, &row.ProductID, &row.Source, &row.AccountID); err != nil {
			return nil, nil, fmt.Errorf("scan ledger account: %w", err)
		}
		id, err := ledger.ParseID(row.AccountID)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, row)
		ids = append(ids, id)
	}
	return rows, ids, pgRows.Err()
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
