package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/forge-metal/billing-service/internal/billing"
)

const (
	defaultProductID            = "sandbox"
	ledgerUnitsPerCent   uint64 = 100_000
	clickHouseEventsSink        = "clickhouse.billing_events"
)

type config struct {
	pgDSNFile      string
	pgDSN          string
	email          string
	org            string
	orgID          string
	orgName        string
	productID      string
	state          string
	planID         string
	balanceUnits   uint64
	balanceCents   uint64
	balanceSet     bool
	businessNowRaw string
	overagePolicy  string
	trustTier      string
	updatedBy      string
}

type result struct {
	OrgID          string            `json:"org_id"`
	OrgName        string            `json:"org_name"`
	Email          string            `json:"email"`
	ProductID      string            `json:"product_id"`
	State          string            `json:"state"`
	PlanID         string            `json:"plan_id"`
	BusinessNow    string            `json:"business_now"`
	BalanceUnits   *uint64           `json:"balance_units,omitempty"`
	TotalsBySource map[string]uint64 `json:"totals_by_source"`
	Contracts      int               `json:"contracts"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := parseFlags()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pgDSN, err := resolvePGDSN(cfg)
	if err != nil {
		return err
	}
	pg, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()
	if err := pg.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	client, err := billing.NewClient(pg, nil, nil, billing.Config{UseStripe: false}, slog.Default())
	if err != nil {
		return err
	}

	orgID, orgName, err := resolveOrg(ctx, pg, cfg)
	if err != nil {
		return err
	}
	now, err := resolveBusinessNow(cfg)
	if err != nil {
		return err
	}
	targetPlanID, err := resolveTargetPlan(ctx, pg, cfg)
	if err != nil {
		return err
	}
	freeState := targetPlanID == ""
	overagePolicy := cfg.overagePolicy
	if overagePolicy == "" {
		if freeState {
			overagePolicy = "block"
		} else {
			overagePolicy = "bill_published_rate"
		}
	}
	balanceUnits := cfg.balanceUnits
	if cfg.balanceCents > 0 {
		balanceUnits = cfg.balanceCents * ledgerUnitsPerCent
	}

	if err := prepareState(ctx, pg, cfg, orgID, orgName, targetPlanID, overagePolicy, now); err != nil {
		return err
	}
	if !freeState {
		if _, err := client.CreateContract(ctx, billing.OrgID(orgID), targetPlanID, billing.CadenceMonthly, "http://127.0.0.1/billing/state/success", "http://127.0.0.1/billing/state/cancel"); err != nil {
			return fmt.Errorf("activate contract: %w", err)
		}
	}
	if err := client.EnsureCurrentEntitlements(ctx, billing.OrgID(orgID), cfg.productID); err != nil {
		return fmt.Errorf("ensure current entitlements: %w", err)
	}
	if cfg.balanceSet {
		if err := closePurchaseGrants(ctx, pg, orgID, cfg.productID, now); err != nil {
			return err
		}
		if balanceUnits > 0 {
			expiresAt := now.AddDate(1, 0, 0)
			sourceRef := textID("set_user_state_purchase", strconv.FormatUint(orgID, 10), cfg.productID, time.Now().UTC().Format(time.RFC3339Nano))
			if _, err := client.DepositCredits(ctx, billing.GrantBalance{OrgID: billing.OrgID(orgID), ScopeType: "account", Source: "purchase", SourceReferenceID: sourceRef, Amount: balanceUnits, StartsAt: now, ExpiresAt: &expiresAt}); err != nil {
				return fmt.Errorf("deposit balance grant: %w", err)
			}
		}
	}
	if err := appendStateEvent(ctx, pg, cfg, orgID, targetPlanID, balanceUnits, now); err != nil {
		return err
	}

	grants, err := client.ListGrantBalances(ctx, billing.OrgID(orgID), cfg.productID)
	if err != nil {
		return fmt.Errorf("list grant balances: %w", err)
	}
	contracts, err := client.ListContracts(ctx, billing.OrgID(orgID))
	if err != nil {
		return fmt.Errorf("list contracts: %w", err)
	}
	totals := map[string]uint64{}
	for _, grant := range grants {
		totals[grant.Source] += grant.Available
	}
	out := result{OrgID: orgIDText(orgID), OrgName: orgName, Email: cfg.email, ProductID: cfg.productID, State: cfg.state, PlanID: targetPlanID, BusinessNow: now.Format(time.RFC3339Nano), TotalsBySource: totals, Contracts: len(contracts)}
	if cfg.balanceSet {
		out.BalanceUnits = &balanceUnits
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func parseFlags() (config, error) {
	cfg := config{productID: defaultProductID, updatedBy: "set-user-state"}
	flag.StringVar(&cfg.pgDSNFile, "pg-dsn-file", "", "path to PostgreSQL DSN file")
	flag.StringVar(&cfg.pgDSN, "pg-dsn", "", "PostgreSQL DSN")
	flag.StringVar(&cfg.email, "email", "", "user email to store as billing_email")
	flag.StringVar(&cfg.org, "org", "", "org id, billing org display name, or platform/acme shortcut")
	flag.StringVar(&cfg.orgID, "org-id", "", "numeric org id")
	flag.StringVar(&cfg.orgName, "org-name", "", "org display name when inserting a new org by id")
	flag.StringVar(&cfg.productID, "product-id", cfg.productID, "product id")
	flag.StringVar(&cfg.state, "state", "", "named state: free, hobby, pro, or a plan tier")
	flag.StringVar(&cfg.planID, "plan-id", "", "exact target plan id; free/none clears paid contracts")
	flag.Uint64Var(&cfg.balanceUnits, "balance-units", 0, "exact purchase balance in ledger units")
	flag.Uint64Var(&cfg.balanceCents, "balance-cents", 0, "exact purchase balance in cents")
	flag.StringVar(&cfg.businessNowRaw, "business-now", "", "business clock override timestamp")
	flag.StringVar(&cfg.overagePolicy, "overage-policy", "", "org overage policy override")
	flag.StringVar(&cfg.trustTier, "trust-tier", "", "org trust tier override")
	flag.StringVar(&cfg.updatedBy, "updated-by", cfg.updatedBy, "billing clock updated_by value")
	flag.Parse()

	if cfg.pgDSN == "" && cfg.pgDSNFile == "" {
		return config{}, fmt.Errorf("either --pg-dsn or --pg-dsn-file is required")
	}
	if strings.TrimSpace(cfg.email) == "" {
		return config{}, fmt.Errorf("--email is required")
	}
	if strings.TrimSpace(cfg.orgID) == "" && strings.TrimSpace(cfg.org) == "" {
		return config{}, fmt.Errorf("--org or --org-id is required")
	}
	if cfg.productID == "" {
		return config{}, fmt.Errorf("--product-id is required")
	}
	if cfg.balanceUnits > 0 && cfg.balanceCents > 0 {
		return config{}, fmt.Errorf("set only one of --balance-units or --balance-cents")
	}
	cfg.balanceSet = flagWasSet("balance-units") || flagWasSet("balance-cents")
	cfg.email = strings.TrimSpace(cfg.email)
	cfg.org = strings.TrimSpace(cfg.org)
	cfg.orgID = strings.TrimSpace(cfg.orgID)
	cfg.orgName = strings.TrimSpace(cfg.orgName)
	cfg.state = strings.ToLower(strings.TrimSpace(cfg.state))
	cfg.planID = strings.TrimSpace(cfg.planID)
	cfg.overagePolicy = strings.TrimSpace(cfg.overagePolicy)
	cfg.trustTier = strings.TrimSpace(cfg.trustTier)
	cfg.updatedBy = strings.TrimSpace(cfg.updatedBy)
	return cfg, nil
}

func flagWasSet(name string) bool {
	seen := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

func resolvePGDSN(cfg config) (string, error) {
	if strings.TrimSpace(cfg.pgDSN) != "" {
		return strings.TrimSpace(cfg.pgDSN), nil
	}
	raw, err := os.ReadFile(cfg.pgDSNFile)
	if err != nil {
		return "", fmt.Errorf("read pg dsn file: %w", err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return "", fmt.Errorf("pg dsn file is empty")
	}
	return strings.TrimSpace(string(raw)), nil
}

func resolveBusinessNow(cfg config) (time.Time, error) {
	if cfg.businessNowRaw == "" {
		return time.Now().UTC(), nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		t, err := time.Parse(layout, cfg.businessNowRaw)
		if err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse --business-now: expected RFC3339/RFC3339Nano")
}

func resolveOrg(ctx context.Context, pg *pgxpool.Pool, cfg config) (uint64, string, error) {
	if cfg.orgID != "" {
		id, err := strconv.ParseUint(cfg.orgID, 10, 64)
		if err != nil || id == 0 {
			return 0, "", fmt.Errorf("--org-id must be an unsigned integer")
		}
		name := cfg.orgName
		if name == "" {
			_ = pg.QueryRow(ctx, `SELECT display_name FROM orgs WHERE org_id = $1`, cfg.orgID).Scan(&name)
		}
		if name == "" {
			name = "Org " + cfg.orgID
		}
		return id, name, nil
	}
	if id, err := strconv.ParseUint(cfg.org, 10, 64); err == nil && id > 0 {
		name := cfg.orgName
		if name == "" {
			_ = pg.QueryRow(ctx, `SELECT display_name FROM orgs WHERE org_id = $1`, cfg.org).Scan(&name)
		}
		if name == "" {
			name = "Org " + cfg.org
		}
		return id, name, nil
	}
	if cfg.org == "platform" {
		return resolveSingleOrg(ctx, pg, cfg, `trust_tier = 'platform'`)
	}
	return resolveSingleOrg(ctx, pg, cfg, `display_name = $1 OR metadata->>'org_key' = $1 OR billing_email = $1`)
}

func resolveSingleOrg(ctx context.Context, pg *pgxpool.Pool, cfg config, predicate string) (uint64, string, error) {
	query := `SELECT org_id, display_name FROM orgs WHERE ` + predicate + ` ORDER BY created_at, org_id LIMIT 2`
	var rows pgx.Rows
	var err error
	if strings.Contains(predicate, "$1") {
		rows, err = pg.Query(ctx, query, cfg.org)
	} else {
		rows, err = pg.Query(ctx, query)
	}
	if err != nil {
		return 0, "", fmt.Errorf("resolve org %q: %w", cfg.org, err)
	}
	defer rows.Close()
	type match struct {
		id   string
		name string
	}
	matches := []match{}
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.id, &m.name); err != nil {
			return 0, "", err
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	if len(matches) == 0 {
		return 0, "", fmt.Errorf("org %q not found in billing orgs; pass --org-id for a new org", cfg.org)
	}
	if len(matches) > 1 {
		return 0, "", fmt.Errorf("org %q matched multiple billing orgs; pass --org-id", cfg.org)
	}
	id, err := strconv.ParseUint(matches[0].id, 10, 64)
	if err != nil || id == 0 {
		return 0, "", fmt.Errorf("resolved org id is not numeric: %q", matches[0].id)
	}
	return id, matches[0].name, nil
}

func resolveTargetPlan(ctx context.Context, pg *pgxpool.Pool, cfg config) (string, error) {
	switch strings.ToLower(cfg.planID) {
	case "free", "none":
		return "", nil
	case "":
	default:
		var isDefault bool
		err := pg.QueryRow(ctx, `SELECT is_default FROM plans WHERE plan_id = $1 AND product_id = $2 AND active`, cfg.planID, cfg.productID).Scan(&isDefault)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("plan %q not found for product %q", cfg.planID, cfg.productID)
		}
		if err != nil {
			return "", fmt.Errorf("load plan %q: %w", cfg.planID, err)
		}
		if isDefault {
			return "", nil
		}
		return cfg.planID, nil
	}
	switch cfg.state {
	case "", "free", "none":
		return "", nil
	default:
		var planID string
		err := pg.QueryRow(ctx, `
			SELECT plan_id
			FROM plans
			WHERE product_id = $1
			  AND active
			  AND NOT is_default
			  AND (tier = $2 OR plan_id = $2 OR lower(display_name) = $2)
			ORDER BY monthly_amount_cents, plan_id
			LIMIT 1
		`, cfg.productID, cfg.state).Scan(&planID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("state %q did not resolve to an active non-default plan for product %q", cfg.state, cfg.productID)
		}
		if err != nil {
			return "", fmt.Errorf("resolve plan for state %q: %w", cfg.state, err)
		}
		return planID, nil
	}
}

func prepareState(ctx context.Context, pg *pgxpool.Pool, cfg config, orgID uint64, orgName, planID, overagePolicy string, now time.Time) error {
	cycleStart := monthStartUTC(now)
	cycleEnd := nextMonth(now)
	cycleID := textID("cycle", strconv.FormatUint(orgID, 10), cfg.productID, cycleStart.Format(time.RFC3339Nano))
	return withTx(ctx, pg, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO orgs (org_id, display_name, billing_email, trust_tier, overage_policy, overage_consent_at)
			VALUES ($1,$2,$3,COALESCE(NULLIF($4,''),'new'),$5,CASE WHEN $5 = 'bill_published_rate' THEN $6::timestamptz ELSE NULL END)
			ON CONFLICT (org_id) DO UPDATE
			SET display_name = EXCLUDED.display_name,
			    billing_email = EXCLUDED.billing_email,
			    trust_tier = CASE WHEN NULLIF($4,'') IS NULL THEN orgs.trust_tier ELSE EXCLUDED.trust_tier END,
			    overage_policy = EXCLUDED.overage_policy,
			    overage_consent_at = EXCLUDED.overage_consent_at
		`, orgIDText(orgID), cleanNonEmpty(orgName, "Org "+orgIDText(orgID)), cfg.email, cfg.trustTier, overagePolicy, now)
		if err != nil {
			return fmt.Errorf("upsert org: %w", err)
		}
		if cfg.businessNowRaw != "" {
			_, err = tx.Exec(ctx, `
				INSERT INTO billing_clock_overrides (scope_kind, scope_id, business_now, reason, updated_by)
				VALUES ('org_product', $1, $2, 'set-user-state', $3)
				ON CONFLICT (scope_kind, scope_id) DO UPDATE
				SET business_now = EXCLUDED.business_now,
				    reason = EXCLUDED.reason,
				    updated_by = EXCLUDED.updated_by,
				    generation = billing_clock_overrides.generation + 1
			`, orgIDText(orgID)+":"+cfg.productID, now, cleanNonEmpty(cfg.updatedBy, "set-user-state"))
			if err != nil {
				return fmt.Errorf("set business clock override: %w", err)
			}
		} else {
			// A stale override would make the contract activation below reopen a future cycle.
			_, err = tx.Exec(ctx, `DELETE FROM billing_clock_overrides WHERE scope_kind = 'org_product' AND scope_id = $1`, orgIDText(orgID)+":"+cfg.productID)
			if err != nil {
				return fmt.Errorf("clear business clock override: %w", err)
			}
		}
		_, err = tx.Exec(ctx, `
			UPDATE billing_cycles
			SET status = 'voided', finalized_at = COALESCE(finalized_at, $5), metadata = metadata || '{"voided_by":"set-user-state"}'::jsonb
			WHERE org_id = $1
			  AND product_id = $2
			  AND cycle_id <> $3
			  AND status <> 'voided'
			  AND (
			    status = 'open'
			    OR tstzrange(starts_at, ends_at, '[)') && tstzrange($4::timestamptz, $5::timestamptz, '[)')
			  )
		`, orgIDText(orgID), cfg.productID, cycleID, cycleStart, cycleEnd)
		if err != nil {
			return fmt.Errorf("void overlapping cycles: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO billing_cycles (cycle_id, org_id, product_id, currency, anchor_at, cycle_seq, cadence_kind, starts_at, ends_at, status, finalization_due_at)
			VALUES ($1,$2,$3,'usd',$4,0,'calendar_monthly',$4,$5,'open',$5)
			ON CONFLICT (org_id, product_id, anchor_at, cycle_seq) DO UPDATE
			SET cycle_id = EXCLUDED.cycle_id,
			    starts_at = EXCLUDED.starts_at,
			    ends_at = EXCLUDED.ends_at,
			    status = 'open',
			    finalization_due_at = EXCLUDED.finalization_due_at,
			    blocked_reason = '',
			    closed_for_usage_at = NULL,
			    finalized_at = NULL,
			    invoice_id = NULL
		`, cycleID, orgIDText(orgID), cfg.productID, cycleStart, cycleEnd)
		if err != nil {
			return fmt.Errorf("ensure open cycle: %w", err)
		}
		_, err = tx.Exec(ctx, `
			DELETE FROM credit_grants
			WHERE org_id = $1
			  AND source IN ('free_tier', 'contract')
			  AND (
			    scope_product_id = $2
			    OR entitlement_period_id IN (SELECT period_id FROM entitlement_periods WHERE org_id = $1 AND product_id = $2)
			  )
		`, orgIDText(orgID), cfg.productID)
		if err != nil {
			return fmt.Errorf("clear old entitlement grants: %w", err)
		}
		_, err = tx.Exec(ctx, `DELETE FROM entitlement_periods WHERE org_id = $1 AND product_id = $2 AND source IN ('free_tier', 'contract')`, orgIDText(orgID), cfg.productID)
		if err != nil {
			return fmt.Errorf("clear old entitlement periods: %w", err)
		}
		_, err = tx.Exec(ctx, `DELETE FROM contracts WHERE org_id = $1 AND product_id = $2`, orgIDText(orgID), cfg.productID)
		if err != nil {
			return fmt.Errorf("clear contracts: %w", err)
		}
		return nil
	})
}

func closePurchaseGrants(ctx context.Context, pg *pgxpool.Pool, orgID uint64, productID string, now time.Time) error {
	_, err := pg.Exec(ctx, `
		UPDATE credit_grants
		SET closed_at = $3, closed_reason = 'set-user-state'
		WHERE org_id = $1
		  AND source = 'purchase'
		  AND scope_type = 'account'
		  AND closed_at IS NULL
		  AND (
		    scope_product_id IS NULL
		    OR scope_product_id = ''
		    OR scope_product_id = $2
		  )
	`, orgIDText(orgID), productID, now)
	if err != nil {
		return fmt.Errorf("close existing purchase grants: %w", err)
	}
	return nil
}

func appendStateEvent(ctx context.Context, pg *pgxpool.Pool, cfg config, orgID uint64, planID string, balanceUnits uint64, now time.Time) error {
	payload := map[string]any{
		"email":        cfg.email,
		"org_id":       orgIDText(orgID),
		"product_id":   cfg.productID,
		"state":        cfg.state,
		"plan_id":      planID,
		"balance_set":  cfg.balanceSet,
		"updated_by":   cleanNonEmpty(cfg.updatedBy, "set-user-state"),
		"business_now": now.Format(time.RFC3339Nano),
	}
	if cfg.balanceSet {
		payload["balance_units"] = balanceUnits
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(payloadBytes)
	payloadHash := hex.EncodeToString(sum[:])
	eventID := textID("evt", "fixture_user_state_set", orgIDText(orgID), now.Format(time.RFC3339Nano), payloadHash)
	return withTx(ctx, pg, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO billing_events (event_id, event_type, event_version, aggregate_type, aggregate_id, org_id, product_id, occurred_at, payload, payload_hash, correlation_id)
			VALUES ($1,'fixture_user_state_set',1,'org',$2,$2,$3,$4,$5,$6,'set-user-state')
			ON CONFLICT (event_id) DO NOTHING
		`, eventID, orgIDText(orgID), cfg.productID, now, payloadBytes, payloadHash)
		if err != nil {
			return fmt.Errorf("insert fixture event: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO billing_event_delivery_queue (event_id, sink, generation, state, next_attempt_at)
			VALUES ($1,$2,1,'pending',now())
			ON CONFLICT (event_id, sink) WHERE state <> 'dead_letter' DO NOTHING
		`, eventID, clickHouseEventsSink)
		if err != nil {
			return fmt.Errorf("enqueue fixture event delivery: %w", err)
		}
		return nil
	})
}

func withTx(ctx context.Context, pg *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pg.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func textID(kind string, parts ...string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(kind))
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	sum := h.Sum(nil)
	return kind + "_" + hex.EncodeToString(sum[:16])
}

func monthStartUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func nextMonth(t time.Time) time.Time {
	return monthStartUTC(t).AddDate(0, 1, 0)
}

func orgIDText(orgID uint64) string {
	return strconv.FormatUint(orgID, 10)
}

func cleanNonEmpty(parts ...string) string {
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			return strings.TrimSpace(part)
		}
	}
	return ""
}
