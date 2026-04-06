package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"

	"github.com/forge-metal/billing"
)

const seedStripeSecret = "seed-test-dummy"

type config struct {
	pgDSNFile          string
	pgDSN              string
	tbAddress          string
	tbClusterID        uint64
	orgID              uint64
	orgName            string
	productID          string
	productDisplayName string
	meterUnit          string
	billingModel       string
	planID             string
	planDisplayName    string
	unitRatesJSON      string
	overageRatesJSON   string
	quotasJSON         string
	targetPrepaidUnits uint64
	expiresAfter       time.Duration
}

type seedResult struct {
	OrgID               uint64 `json:"org_id"`
	OrgName             string `json:"org_name"`
	ProductID           string `json:"product_id"`
	PlanID              string `json:"plan_id"`
	PrepaidBefore       uint64 `json:"prepaid_before"`
	DepositedUnits      uint64 `json:"deposited_units"`
	PrepaidAfter        uint64 `json:"prepaid_after"`
	TargetPrepaidUnits  uint64 `json:"target_prepaid_units"`
	ProductUpserted     bool   `json:"product_upserted"`
	DefaultPlanUpserted bool   `json:"default_plan_upserted"`
}

type noopMeteringWriter struct{}

func (noopMeteringWriter) InsertMeteringRow(context.Context, billing.MeteringRow) error { return nil }

type noopMeteringQuerier struct{}

func (noopMeteringQuerier) SumDimension(context.Context, billing.OrgID, string, string, time.Time) (float64, error) {
	return 0, nil
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

	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()
	if err := pg.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	tbAddresses := strings.Split(cfg.tbAddress, ",")
	tbClient, err := tb.NewClient(tbtypes.ToUint128(cfg.tbClusterID), tbAddresses)
	if err != nil {
		return fmt.Errorf("create tigerbeetle client: %w", err)
	}
	defer tbClient.Close()

	billingCfg := billing.DefaultConfig()
	billingCfg.StripeSecretKey = seedStripeSecret
	billingCfg.TigerBeetleAddresses = tbAddresses
	billingCfg.TigerBeetleClusterID = cfg.tbClusterID

	client, err := billing.NewClient(
		tbClient,
		pg,
		stripe.NewClient(seedStripeSecret),
		noopMeteringWriter{},
		noopMeteringQuerier{},
		billingCfg,
	)
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}

	unitRates, err := decodeUint64Map("unit_rates_json", cfg.unitRatesJSON)
	if err != nil {
		return err
	}
	overageRates, err := decodeUint64Map("overage_unit_rates_json", cfg.overageRatesJSON)
	if err != nil {
		return err
	}
	quotasJSON, err := normalizeJSON("quotas_json", cfg.quotasJSON)
	if err != nil {
		return err
	}

	if err := client.EnsureOrg(ctx, billing.OrgID(cfg.orgID), cfg.orgName); err != nil {
		return fmt.Errorf("ensure org: %w", err)
	}

	productUpserted, err := upsertProduct(ctx, pg, cfg)
	if err != nil {
		return err
	}
	defaultPlanUpserted, err := upsertDefaultPlan(ctx, pg, cfg, unitRates, overageRates, quotasJSON)
	if err != nil {
		return err
	}

	before, err := client.GetProductBalance(ctx, billing.OrgID(cfg.orgID), cfg.productID)
	if err != nil {
		return fmt.Errorf("get product balance before seed: %w", err)
	}

	var deposited uint64
	if before.PrepaidRemaining < cfg.targetPrepaidUnits {
		deposited = cfg.targetPrepaidUnits - before.PrepaidRemaining
		taskID := billing.TaskID(time.Now().UTC().UnixNano())
		expiresAt := time.Now().UTC().Add(cfg.expiresAfter)
		if err := client.DepositCredits(ctx, &taskID, billing.CreditGrant{
			OrgID:             billing.OrgID(cfg.orgID),
			ProductID:         cfg.productID,
			Amount:            deposited,
			Source:            "purchase",
			StripeReferenceID: fmt.Sprintf("seed:%d:%s:%d", cfg.orgID, cfg.productID, taskID),
			ExpiresAt:         &expiresAt,
		}); err != nil {
			return fmt.Errorf("deposit prepaid credits: %w", err)
		}
	}

	after, err := client.GetProductBalance(ctx, billing.OrgID(cfg.orgID), cfg.productID)
	if err != nil {
		return fmt.Errorf("get product balance after seed: %w", err)
	}

	result := seedResult{
		OrgID:               cfg.orgID,
		OrgName:             cfg.orgName,
		ProductID:           cfg.productID,
		PlanID:              cfg.planID,
		PrepaidBefore:       before.PrepaidRemaining,
		DepositedUnits:      deposited,
		PrepaidAfter:        after.PrepaidRemaining,
		TargetPrepaidUnits:  cfg.targetPrepaidUnits,
		ProductUpserted:     productUpserted,
		DefaultPlanUpserted: defaultPlanUpserted,
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal seed result: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

func parseFlags() (config, error) {
	cfg := config{
		productID:          "sandbox",
		productDisplayName: "Sandbox",
		meterUnit:          "vcpu_second",
		billingModel:       "metered",
		planID:             "sandbox-default",
		planDisplayName:    "Sandbox PAYG",
		unitRatesJSON:      `{"vcpu":325,"gib":40}`,
		overageRatesJSON:   `{}`,
		quotasJSON:         `{}`,
		targetPrepaidUnits: 5_000_000,
		expiresAfter:       365 * 24 * time.Hour,
	}

	flag.StringVar(&cfg.pgDSNFile, "pg-dsn-file", "", "path to PostgreSQL DSN file")
	flag.StringVar(&cfg.pgDSN, "pg-dsn", "", "PostgreSQL DSN")
	flag.StringVar(&cfg.tbAddress, "tb-address", "127.0.0.1:3320", "comma-separated TigerBeetle addresses")
	flag.Uint64Var(&cfg.tbClusterID, "tb-cluster-id", 0, "TigerBeetle cluster ID")
	flag.Uint64Var(&cfg.orgID, "org-id", 0, "org ID to seed")
	flag.StringVar(&cfg.orgName, "org-name", "", "org display name")
	flag.StringVar(&cfg.productID, "product-id", cfg.productID, "product ID")
	flag.StringVar(&cfg.productDisplayName, "product-display-name", cfg.productDisplayName, "product display name")
	flag.StringVar(&cfg.meterUnit, "meter-unit", cfg.meterUnit, "product meter unit")
	flag.StringVar(&cfg.billingModel, "billing-model", cfg.billingModel, "product billing model")
	flag.StringVar(&cfg.planID, "plan-id", cfg.planID, "default plan ID")
	flag.StringVar(&cfg.planDisplayName, "plan-display-name", cfg.planDisplayName, "default plan display name")
	flag.StringVar(&cfg.unitRatesJSON, "unit-rates-json", cfg.unitRatesJSON, "default plan unit rates JSON")
	flag.StringVar(&cfg.overageRatesJSON, "overage-unit-rates-json", cfg.overageRatesJSON, "default plan overage unit rates JSON")
	flag.StringVar(&cfg.quotasJSON, "quotas-json", cfg.quotasJSON, "default plan quotas JSON")
	flag.Uint64Var(&cfg.targetPrepaidUnits, "target-prepaid-units", cfg.targetPrepaidUnits, "minimum prepaid units to ensure after seeding")
	flag.DurationVar(&cfg.expiresAfter, "expires-after", cfg.expiresAfter, "duration until seeded credits expire")
	flag.Parse()

	switch {
	case cfg.pgDSN == "" && cfg.pgDSNFile == "":
		return config{}, fmt.Errorf("either --pg-dsn or --pg-dsn-file is required")
	case cfg.orgID == 0:
		return config{}, fmt.Errorf("--org-id is required")
	case cfg.orgName == "":
		cfg.orgName = fmt.Sprintf("org-%d", cfg.orgID)
	}

	return cfg, nil
}

func resolvePGDSN(cfg config) (string, error) {
	if cfg.pgDSN != "" {
		return cfg.pgDSN, nil
	}
	raw, err := os.ReadFile(cfg.pgDSNFile)
	if err != nil {
		return "", fmt.Errorf("read pg dsn file: %w", err)
	}
	dsn := strings.TrimSpace(string(raw))
	if dsn == "" {
		return "", fmt.Errorf("pg dsn file %s is empty", cfg.pgDSNFile)
	}
	return dsn, nil
}

func upsertProduct(ctx context.Context, pg *sql.DB, cfg config) (bool, error) {
	result, err := pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (product_id) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    meter_unit = EXCLUDED.meter_unit,
		    billing_model = EXCLUDED.billing_model
	`, cfg.productID, cfg.productDisplayName, cfg.meterUnit, cfg.billingModel)
	if err != nil {
		return false, fmt.Errorf("upsert product %s: %w", cfg.productID, err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

func upsertDefaultPlan(ctx context.Context, pg *sql.DB, cfg config, unitRates map[string]uint64, overageRates map[string]uint64, quotasJSON string) (bool, error) {
	unitRatesRaw, err := json.Marshal(unitRates)
	if err != nil {
		return false, fmt.Errorf("marshal unit rates: %w", err)
	}
	overageRatesRaw, err := json.Marshal(overageRates)
	if err != nil {
		return false, fmt.Errorf("marshal overage unit rates: %w", err)
	}

	result, err := pg.ExecContext(ctx, `
		INSERT INTO plans (
			plan_id,
			product_id,
			display_name,
			included_credits,
			unit_rates,
			overage_unit_rates,
			quotas,
			is_default,
			active
		)
		VALUES ($1, $2, $3, 0, $4::jsonb, $5::jsonb, $6::jsonb, true, true)
		ON CONFLICT (plan_id) DO UPDATE
		SET product_id = EXCLUDED.product_id,
		    display_name = EXCLUDED.display_name,
		    included_credits = EXCLUDED.included_credits,
		    unit_rates = EXCLUDED.unit_rates,
		    overage_unit_rates = EXCLUDED.overage_unit_rates,
		    quotas = EXCLUDED.quotas,
		    is_default = EXCLUDED.is_default,
		    active = EXCLUDED.active
	`, cfg.planID, cfg.productID, cfg.planDisplayName, string(unitRatesRaw), string(overageRatesRaw), quotasJSON)
	if err != nil {
		return false, fmt.Errorf("upsert default plan %s: %w", cfg.planID, err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

func decodeUint64Map(name string, raw string) (map[string]uint64, error) {
	var decoded map[string]uint64
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if decoded == nil {
		return map[string]uint64{}, nil
	}
	return decoded, nil
}

func normalizeJSON(name string, raw string) (string, error) {
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return string(encoded), nil
}
