package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	opch "github.com/verself/operator-runtime/clickhouse"
	oppg "github.com/verself/operator-runtime/postgres"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	billingProductDefault = "sandbox"
	billingClockTarget    = "//src/services/billing-service/cmd/billing-clock:billing-clock"
	billingClockBin       = "bazel-bin/src/services/billing-service/cmd/billing-clock/billing-clock_/billing-clock"
	billingSeedTarget     = "//src/services/billing-service/cmd/billing-seed:billing-seed"
	billingSeedBin        = "bazel-bin/src/services/billing-service/cmd/billing-seed/billing-seed_/billing-seed"
)

var billingTokenRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type billingOptions struct {
	operatorRuntimeOptions
	secretsFile string
	pgUser      string
	remotePort  int
}

type billingInspectOptions struct {
	*billingOptions
	org       string
	orgID     string
	productID string
	format    string
	limit     uint
	eventType string
	minutes   uint
}

func cmdBilling(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("billing: missing subcommand (try `seed`, `clock`, `state`, `documents`, `finalizations`, or `events`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "seed":
		return cmdBillingSeed(rest)
	case "clock":
		return cmdBillingClock(rest)
	case "state":
		return cmdBillingInspect(rest, "state")
	case "documents":
		return cmdBillingInspect(rest, "documents")
	case "finalizations":
		return cmdBillingInspect(rest, "finalizations")
	case "events":
		return cmdBillingInspect(rest, "events")
	default:
		return fmt.Errorf("billing: unknown subcommand: %s", sub)
	}
}

func cmdBillingSeed(args []string) error {
	fs := flagSet("billing seed")
	opts := addBillingFlags(fs)
	orgID := fs.String("org-id", "", "Numeric org ID")
	orgName := fs.String("org-name", "", "Org display name")
	orgTrustTier := fs.String("org-trust-tier", "", "Org trust tier")
	productID := fs.String("product-id", billingProductDefault, "Billing product ID")
	targetPrepaidUnits := fs.String("target-prepaid-units", "", "Target prepaid units")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *orgID == "" {
		return errors.New("billing seed: --org-id is required")
	}
	if _, err := strconv.ParseUint(*orgID, 10, 64); err != nil {
		return fmt.Errorf("billing seed: --org-id must be an unsigned integer: %w", err)
	}
	if *productID == "" {
		return errors.New("billing seed: --product-id is required")
	}
	if *targetPrepaidUnits != "" {
		if _, err := strconv.ParseUint(*targetPrepaidUnits, 10, 64); err != nil {
			return fmt.Errorf("billing seed: --target-prepaid-units must be an unsigned integer: %w", err)
		}
	}
	return runOperatorRuntime("billing.seed", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		remoteArgs := []string{
			"--pg-dsn", "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable",
			"--org-id", *orgID,
			"--product-id", *productID,
		}
		addStringFlag := func(name, value string) {
			if value != "" {
				remoteArgs = append(remoteArgs, name, value)
			}
		}
		addStringFlag("--org-name", *orgName)
		addStringFlag("--org-trust-tier", *orgTrustTier)
		addStringFlag("--target-prepaid-units", *targetPrepaidUnits)
		return runRemoteBazelExecutable(rt, billingSeedTarget, billingSeedBin, "verself-billing-seed", "billing", remoteArgs)
	})
}

func cmdBillingClock(args []string) error {
	fs := flagSet("billing clock")
	opts := addBillingFlags(fs)
	org := fs.String("org", "", "Org slug")
	orgID := fs.String("org-id", "", "Numeric org ID")
	productID := fs.String("product-id", billingProductDefault, "Billing product ID")
	setAt := fs.String("set", "", "Set business-time to RFC3339 timestamp")
	advanceSeconds := fs.String("advance-seconds", "", "Advance business-time by N seconds")
	clear := fs.Bool("clear", false, "Clear business-time override")
	wallClock := fs.Bool("wall-clock", false, "Reset to wall-clock and repair current cycle")
	reason := fs.String("reason", "billing-clock", "Audit reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *org == "" && *orgID == "" {
		return errors.New("billing clock: --org or --org-id is required")
	}
	selected := 0
	for _, enabled := range []bool{*setAt != "", *advanceSeconds != "", *clear, *wallClock} {
		if enabled {
			selected++
		}
	}
	if selected > 1 {
		return errors.New("billing clock: choose only one of --set, --advance-seconds, --clear, or --wall-clock")
	}
	if *productID == "" {
		return errors.New("billing clock: --product-id is required")
	}
	if *advanceSeconds != "" {
		if _, err := strconv.ParseInt(*advanceSeconds, 10, 64); err != nil {
			return fmt.Errorf("billing clock: --advance-seconds must be an integer: %w", err)
		}
	}
	return runOperatorRuntime("billing.clock", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		localPath, err := buildBazelBinary(rt.Ctx, rt.RepoRoot, billingClockTarget, billingClockBin)
		if err != nil {
			return err
		}
		remotePath, err := rt.SSH.UploadExecutable(rt.Ctx, localPath, "verself-billing-clock")
		if err != nil {
			return err
		}
		defer func() { _ = rt.SSH.RemoveRemotePath(contextWithoutCancel(rt.Ctx), remotePath) }()
		remoteArgs := []string{
			remotePath,
			"--pg-dsn", "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable",
			"--product-id", *productID,
			"--reason", *reason,
		}
		if *orgID != "" {
			remoteArgs = append(remoteArgs, "--org-id", *orgID)
		} else {
			remoteArgs = append(remoteArgs, "--org", *org)
		}
		if *setAt != "" {
			remoteArgs = append(remoteArgs, "--set", *setAt)
		}
		if *advanceSeconds != "" {
			remoteArgs = append(remoteArgs, "--advance-seconds", *advanceSeconds)
		}
		if *clear {
			remoteArgs = append(remoteArgs, "--clear")
		}
		if *wallClock {
			remoteArgs = append(remoteArgs, "--wall-clock")
		}
		return rt.SSH.RunArgv(rt.Ctx, "billing", remoteArgs, nil, os.Stdout, os.Stderr)
	})
}

func cmdBillingInspect(args []string, kind string) error {
	fs := flagSet("billing " + kind)
	opts := addBillingInspectFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if kind != "events" && opts.org == "" && opts.orgID == "" {
		return fmt.Errorf("billing %s: --org or --org-id is required", kind)
	}
	if opts.productID == "" {
		return fmt.Errorf("billing %s: --product-id is required", kind)
	}
	if opts.limit == 0 || opts.limit > 1000 {
		return fmt.Errorf("billing %s: --limit must be between 1 and 1000", kind)
	}
	if opts.minutes == 0 || opts.minutes > 24*60*31 {
		return fmt.Errorf("billing %s: --minutes must be between 1 and 44640", kind)
	}
	switch normalizeOutputFormat(opts.format) {
	case "table", "json", "csv", "tsv":
	default:
		return fmt.Errorf("billing %s: --format must be table, json, csv, or tsv", kind)
	}
	return runOperatorRuntime("billing."+kind, opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, chClient *opch.Client) error {
		conn, err := openBillingPG(rt, opts.billingOptions)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(rt.Ctx) }()
		resolvedOrgID, err := resolveBillingOrgID(rt, conn, opts.orgID, opts.org, kind == "events")
		if err != nil {
			return err
		}
		if kind == "events" {
			return printBillingEvents(rt, chClient, opts, resolvedOrgID)
		}
		return printBillingPG(rt, conn, opts, resolvedOrgID, kind)
	})
}

func addBillingFlags(fs *flag.FlagSet) *billingOptions {
	opts := &billingOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.StringVar(&opts.secretsFile, "secrets-file", os.Getenv("SOPS_SECRETS_FILE"), "SOPS secrets file")
	fs.StringVar(&opts.pgUser, "pg-user", envOr("PG_USER", oppg.DefaultUser), "PostgreSQL user")
	fs.IntVar(&opts.remotePort, "remote-port", envIntOr("PG_PORT", oppg.DefaultPort), "Remote PostgreSQL port")
	return opts
}

func addBillingInspectFlags(fs *flag.FlagSet) *billingInspectOptions {
	base := addBillingFlags(fs)
	opts := &billingInspectOptions{billingOptions: base, productID: billingProductDefault, format: "table", limit: 100, minutes: 60}
	fs.StringVar(&opts.org, "org", "", "Org slug")
	fs.StringVar(&opts.orgID, "org-id", "", "Numeric org ID")
	fs.StringVar(&opts.productID, "product-id", opts.productID, "Billing product ID")
	fs.StringVar(&opts.format, "format", opts.format, "Output format: table|json|csv|tsv")
	fs.UintVar(&opts.limit, "limit", opts.limit, "Maximum rows")
	fs.StringVar(&opts.eventType, "event-type", "", "Billing event type filter")
	fs.UintVar(&opts.minutes, "minutes", opts.minutes, "Events lookback window in minutes")
	return opts
}

func openBillingPG(rt *opruntime.Runtime, opts *billingOptions) (*pgx.Conn, error) {
	if opts.remotePort <= 0 || opts.remotePort > 65535 {
		return nil, fmt.Errorf("billing pg: --remote-port must be between 1 and 65535 (got %d)", opts.remotePort)
	}
	passwordPath := opts.secretsFile
	if passwordPath == "" {
		passwordPath = opruntime.HostConfigurationSecretsPath(rt.RepoRoot, rt.Site)
	}
	return oppg.OpenOverSSH(rt.Ctx, rt, oppg.Config{
		Database:     "billing",
		User:         opts.pgUser,
		RemotePort:   opts.remotePort,
		PasswordPath: passwordPath,
	})
}

func resolveBillingOrgID(rt *opruntime.Runtime, conn *pgx.Conn, orgID, org string, allowEmpty bool) (string, error) {
	if orgID != "" {
		if _, err := strconv.ParseUint(orgID, 10, 64); err != nil {
			return "", fmt.Errorf("--org-id must be an unsigned integer: %w", err)
		}
		return orgID, nil
	}
	org = strings.TrimSpace(org)
	if org == "" {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("--org or --org-id is required")
	}
	if _, err := strconv.ParseUint(org, 10, 64); err == nil {
		return org, nil
	}
	predicate := `display_name = $1 OR metadata->>'org_key' = $1 OR billing_email = $1`
	args := []any{org}
	if org == "platform" {
		predicate = `trust_tier = 'platform'`
		args = nil
	}
	rows, err := conn.Query(rt.Ctx, `SELECT org_id FROM orgs WHERE `+predicate+` ORDER BY created_at, org_id LIMIT 2`, args...)
	if err != nil {
		return "", fmt.Errorf("resolve org %q: %w", org, err)
	}
	defer rows.Close()
	matches := []string{}
	for rows.Next() {
		var match string
		if err := rows.Scan(&match); err != nil {
			return "", err
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("org %q not found", org)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("org %q matched multiple billing orgs; pass --org-id", org)
	}
	return matches[0], nil
}

func printBillingPG(rt *opruntime.Runtime, conn *pgx.Conn, opts *billingInspectOptions, orgID, kind string) error {
	productID := opts.productID
	queries := billingPGQueries(kind)
	if kind == "state" {
		_, _ = fmt.Fprintf(os.Stdout, "== billing state ==\norg_id=%s\nproduct_id=%s\n\n", orgID, productID)
	}
	for i, item := range queries {
		if normalizeOutputFormat(opts.format) == "table" {
			if i > 0 || kind == "state" {
				_, _ = fmt.Fprintln(os.Stdout)
			}
			_, _ = fmt.Fprintf(os.Stdout, "== %s ==\n", item.title)
		}
		table, _, err := oppg.QueryTableArgs(rt.Ctx, conn, item.sql, item.args(orgID, productID, opts.limit)...)
		if err != nil {
			return fmt.Errorf("%s: %w", item.title, err)
		}
		if err := printTableFormat(os.Stdout, table, opts.format); err != nil {
			return err
		}
	}
	return nil
}

type billingSQL struct {
	title string
	sql   string
	args  func(orgID, productID string, limit uint) []any
}

func billingPGQueries(kind string) []billingSQL {
	switch kind {
	case "documents":
		return []billingSQL{{title: "documents", args: billingArgsOrgProductLimit, sql: `
SELECT document_id, COALESCE(document_number,'') AS document_number, document_kind, COALESCE(finalization_id,'') AS finalization_id, COALESCE(cycle_id,'') AS cycle_id, status, payment_status, period_start, period_end, issued_at, total_due_units, COALESCE(stripe_hosted_invoice_url,'') AS stripe_hosted_invoice_url, COALESCE(stripe_invoice_pdf_url,'') AS stripe_invoice_pdf_url
FROM billing_documents
WHERE org_id = $1
  AND ($2 = '' OR product_id = $2)
ORDER BY period_start DESC, issued_at DESC NULLS LAST, document_id DESC
LIMIT $3`}}
	case "finalizations":
		return []billingSQL{{title: "finalizations", args: billingArgsOrgProductLimit, sql: `
SELECT finalization_id, subject_type, subject_id, COALESCE(cycle_id,'') AS cycle_id, COALESCE(document_id,'') AS document_id, document_kind, state, customer_visible, has_usage, has_financial_activity, started_at, completed_at, last_error
FROM billing_finalizations
WHERE org_id = $1
  AND ($2 = '' OR product_id = $2)
ORDER BY started_at DESC, finalization_id DESC
LIMIT $3`}}
	default:
		return []billingSQL{
			{title: "org", args: billingArgsOrg, sql: `
SELECT org_id, display_name, billing_email, state, trust_tier, overage_policy, overage_consent_at, created_at, updated_at
FROM orgs
WHERE org_id = $1`},
			{title: "clock overrides", args: billingArgsOrgProduct, sql: `
SELECT scope_kind, scope_id, business_now, reason, generation, updated_at
FROM billing_clock_overrides
WHERE scope_id IN ($1 || ':' || $2, $1)
ORDER BY scope_kind, scope_id`},
			{title: "cycles", args: billingArgsOrgProductLimit, sql: `
SELECT cycle_id, status, cadence_kind, starts_at, ends_at, finalization_due_at, active_finalization_id, metadata
FROM billing_cycles
WHERE org_id = $1
  AND product_id = $2
ORDER BY starts_at DESC, cycle_id DESC
LIMIT $3`},
			{title: "contracts", args: billingArgsOrgProductLimit, sql: `
SELECT c.contract_id, c.state, c.payment_state, c.entitlement_state, c.starts_at, c.cancel_at, p.phase_id, p.plan_id, p.state AS phase_state, p.effective_start, p.effective_end
FROM contracts c
LEFT JOIN contract_phases p ON p.contract_id = c.contract_id AND p.state IN ('active','grace','scheduled')
WHERE c.org_id = $1
  AND c.product_id = $2
ORDER BY c.created_at DESC, p.effective_start DESC NULLS LAST
LIMIT $3`},
			{title: "credit grants", args: billingArgsOrgProduct, sql: `
SELECT source, scope_type, COALESCE(scope_product_id,'') AS scope_product_id, COALESCE(scope_bucket_id,'') AS scope_bucket_id, COALESCE(scope_sku_id,'') AS scope_sku_id, ledger_posting_state, count(*) AS grants, sum(amount) AS total_units
FROM credit_grants
WHERE org_id = $1
  AND closed_at IS NULL
  AND ($2 = '' OR COALESCE(scope_product_id, $2) = $2 OR scope_type = 'account')
GROUP BY source, scope_type, COALESCE(scope_product_id,''), COALESCE(scope_bucket_id,''), COALESCE(scope_sku_id,''), ledger_posting_state
ORDER BY source, scope_type, scope_product_id, scope_bucket_id, scope_sku_id, ledger_posting_state`},
		}
	}
}

func billingArgsOrg(orgID, _ string, _ uint) []any {
	return []any{orgID}
}

func billingArgsOrgProduct(orgID, productID string, _ uint) []any {
	return []any{orgID, productID}
}

func billingArgsOrgProductLimit(orgID, productID string, limit uint) []any {
	return []any{orgID, productID, limit}
}

func printBillingEvents(rt *opruntime.Runtime, chClient *opch.Client, opts *billingInspectOptions, orgID string) error {
	if opts.eventType != "" && !billingTokenRE.MatchString(opts.eventType) {
		return errors.New("billing events: --event-type must contain only letters, numbers, dot, underscore, or dash")
	}
	table, err := chClient.QueryTableParams(rt.Ctx, `
SELECT event_id, event_type, aggregate_type, aggregate_id, org_id, product_id, occurred_at, recorded_at, payload
FROM verself.billing_events
WHERE recorded_at > now() - toIntervalMinute({minutes:UInt32})
  AND ({org_id:String} = '' OR org_id = {org_id:String})
  AND ({product_id:String} = '' OR product_id = {product_id:String})
  AND ({event_type:String} = '' OR event_type = {event_type:String})
ORDER BY recorded_at DESC, event_id DESC
LIMIT {row_limit:UInt32}
`, map[string]string{
		"org_id":     orgID,
		"product_id": opts.productID,
		"event_type": opts.eventType,
		"minutes":    strconv.FormatUint(uint64(opts.minutes), 10),
		"row_limit":  strconv.FormatUint(uint64(opts.limit), 10),
	})
	if err != nil {
		return err
	}
	return printTableFormat(os.Stdout, table, opts.format)
}
