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

	"github.com/forge-metal/billing-service/internal/billing"
)

const seedStripeSecret = "seed-test-dummy"

type config struct {
	pgDSNFile          string
	pgDSN              string
	tbAddress          string
	tbClusterID        uint64
	orgID              uint64
	orgName            string
	orgTrustTier       string
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
	prepaidSource      string
	promotionName      string
	promotionPercent   int
	expiresAfter       time.Duration
}

type seedResult struct {
	OrgID               uint64 `json:"org_id"`
	OrgName             string `json:"org_name"`
	OrgTrustTier        string `json:"org_trust_tier"`
	ProductID           string `json:"product_id"`
	PlanID              string `json:"plan_id"`
	PrepaidBefore       uint64 `json:"prepaid_before"`
	DepositedUnits      uint64 `json:"deposited_units"`
	PrepaidAfter        uint64 `json:"prepaid_after"`
	TargetPrepaidUnits  uint64 `json:"target_prepaid_units"`
	ProductUpserted     bool   `json:"product_upserted"`
	DefaultPlanUpserted bool   `json:"default_plan_upserted"`
	PromotionUpserted   bool   `json:"promotion_upserted"`
}

type noopMeteringWriter struct{}

func (noopMeteringWriter) InsertMeteringRow(context.Context, billing.MeteringRow) error { return nil }

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

	if err := upsertOrg(ctx, pg, cfg); err != nil {
		return err
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
		if _, err := client.DepositCredits(ctx, &taskID, billing.CreditGrant{
			OrgID:             billing.OrgID(cfg.orgID),
			ProductID:         cfg.productID,
			Amount:            deposited,
			Source:            cfg.prepaidSource,
			StripeReferenceID: fmt.Sprintf("seed:%d:%s:%d", cfg.orgID, cfg.productID, taskID),
			ExpiresAt:         &expiresAt,
		}); err != nil {
			return fmt.Errorf("deposit prepaid credits: %w", err)
		}
	}

	promotionUpserted, err := upsertPromotion(ctx, pg, cfg)
	if err != nil {
		return err
	}

	after, err := client.GetProductBalance(ctx, billing.OrgID(cfg.orgID), cfg.productID)
	if err != nil {
		return fmt.Errorf("get product balance after seed: %w", err)
	}

	result := seedResult{
		OrgID:               cfg.orgID,
		OrgName:             cfg.orgName,
		OrgTrustTier:        cfg.orgTrustTier,
		ProductID:           cfg.productID,
		PlanID:              cfg.planID,
		PrepaidBefore:       before.PrepaidRemaining,
		DepositedUnits:      deposited,
		PrepaidAfter:        after.PrepaidRemaining,
		TargetPrepaidUnits:  cfg.targetPrepaidUnits,
		ProductUpserted:     productUpserted,
		DefaultPlanUpserted: defaultPlanUpserted,
		PromotionUpserted:   promotionUpserted,
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
		orgTrustTier:       "new",
		planID:             "sandbox-default",
		planDisplayName:    "Sandbox PAYG",
		unitRatesJSON:      `{"vcpu":325,"gib":40}`,
		overageRatesJSON:   `{}`,
		quotasJSON:         `{}`,
		targetPrepaidUnits: 5_000_000,
		prepaidSource:      "purchase",
		expiresAfter:       365 * 24 * time.Hour,
	}

	flag.StringVar(&cfg.pgDSNFile, "pg-dsn-file", "", "path to PostgreSQL DSN file")
	flag.StringVar(&cfg.pgDSN, "pg-dsn", "", "PostgreSQL DSN")
	flag.StringVar(&cfg.tbAddress, "tb-address", "127.0.0.1:3320", "comma-separated TigerBeetle addresses")
	flag.Uint64Var(&cfg.tbClusterID, "tb-cluster-id", 0, "TigerBeetle cluster ID")
	flag.Uint64Var(&cfg.orgID, "org-id", 0, "org ID to seed")
	flag.StringVar(&cfg.orgName, "org-name", "", "org display name")
	flag.StringVar(&cfg.orgTrustTier, "org-trust-tier", cfg.orgTrustTier, "org trust tier")
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
	flag.StringVar(&cfg.prepaidSource, "prepaid-source", cfg.prepaidSource, "credit grant source for seeded prepaid units")
	flag.StringVar(&cfg.promotionName, "promotion-name", "", "org-scoped promotion name to upsert")
	flag.IntVar(&cfg.promotionPercent, "promotion-percent", 0, "org-scoped promotion percent off to upsert")
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
	cfg.orgTrustTier = strings.TrimSpace(cfg.orgTrustTier)
	if cfg.orgTrustTier == "" {
		return config{}, fmt.Errorf("--org-trust-tier is required")
	}
	switch cfg.orgTrustTier {
	case "new", "established", "enterprise", "platform":
	default:
		return config{}, fmt.Errorf("--org-trust-tier must be one of new, established, enterprise, platform")
	}
	cfg.prepaidSource = strings.TrimSpace(cfg.prepaidSource)
	if cfg.prepaidSource == "" {
		return config{}, fmt.Errorf("--prepaid-source is required")
	}
	if cfg.promotionPercent < 0 || cfg.promotionPercent > 100 {
		return config{}, fmt.Errorf("--promotion-percent must be between 0 and 100")
	}
	cfg.promotionName = strings.TrimSpace(cfg.promotionName)
	if cfg.promotionPercent > 0 && cfg.promotionName == "" {
		return config{}, fmt.Errorf("--promotion-name is required when --promotion-percent is set")
	}

	return cfg, nil
}

func upsertOrg(ctx context.Context, pg *sql.DB, cfg config) error {
	if _, err := pg.ExecContext(ctx, `
		INSERT INTO orgs (org_id, display_name, trust_tier)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    trust_tier = EXCLUDED.trust_tier
	`, fmt.Sprintf("%d", cfg.orgID), cfg.orgName, cfg.orgTrustTier); err != nil {
		return fmt.Errorf("upsert org %d: %w", cfg.orgID, err)
	}
	return nil
}

func upsertPromotion(ctx context.Context, pg *sql.DB, cfg config) (bool, error) {
	if cfg.promotionPercent == 0 {
		return false, nil
	}
	result, err := pg.ExecContext(ctx, `
		INSERT INTO promotions (org_id, promotion_name, percent_off, effective_at, ending_at)
		VALUES ($1, $2, $3, now(), NULL)
		ON CONFLICT (org_id, promotion_name) DO UPDATE
		SET percent_off = EXCLUDED.percent_off,
		    effective_at = EXCLUDED.effective_at,
		    ending_at = EXCLUDED.ending_at
	`, fmt.Sprintf("%d", cfg.orgID), cfg.promotionName, cfg.promotionPercent)
	if err != nil {
		return false, fmt.Errorf("upsert promotion %s: %w", cfg.promotionName, err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
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
