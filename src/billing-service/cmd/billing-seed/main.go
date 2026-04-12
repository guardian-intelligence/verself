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

const (
	seedStripeSecret = "seed-test-dummy"

	sandboxProductID       = "sandbox"
	sandboxComputeSKU      = "sandbox_compute_amd_epyc_4484px_vcpu_second"
	sandboxMemorySKU       = "sandbox_memory_standard_gib_second"
	sandboxBlockStorageSKU = "sandbox_block_storage_premium_nvme_gib_second"
)

type config struct {
	pgDSNFile           string
	pgDSN               string
	tbAddress           string
	tbClusterID         uint64
	orgID               uint64
	orgName             string
	orgTrustTier        string
	productID           string
	productDisplayName  string
	meterUnit           string
	billingModel        string
	planID              string
	planDisplayName     string
	includedBucketsJSON string
	quotasJSON          string
	targetPrepaidUnits  uint64
	prepaidSource       string
	expiresAfter        time.Duration
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
	CatalogUpserted     bool   `json:"catalog_upserted"`
	DefaultPlanUpserted bool   `json:"default_plan_upserted"`
}

type bucketSeed struct {
	BucketID    string
	DisplayName string
	SortOrder   int
}

type skuSeed struct {
	SKUID        string
	BucketID     string
	DisplayName  string
	QuantityUnit string
	UnitRate     uint64
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

	client, err := billing.NewClient(tbClient, pg, stripe.NewClient(seedStripeSecret), noopMeteringWriter{}, billingCfg)
	if err != nil {
		return fmt.Errorf("create billing client: %w", err)
	}

	if err := client.EnsureOrg(ctx, billing.OrgID(cfg.orgID), cfg.orgName, cfg.orgTrustTier); err != nil {
		return err
	}
	productUpserted, err := upsertProduct(ctx, pg, cfg)
	if err != nil {
		return err
	}
	catalogUpserted, err := upsertCatalog(ctx, pg, cfg)
	if err != nil {
		return err
	}
	defaultPlanUpserted, err := upsertDefaultPlan(ctx, pg, cfg)
	if err != nil {
		return err
	}

	before, err := currentPrepaidUnits(ctx, client, cfg.orgID, cfg.productID)
	if err != nil {
		return err
	}

	var deposited uint64
	if before < cfg.targetPrepaidUnits {
		deposited = cfg.targetPrepaidUnits - before
		expiresAt := time.Now().UTC().Add(cfg.expiresAfter)
		if _, err := client.DepositCredits(ctx, billing.CreditGrant{
			OrgID:             billing.OrgID(cfg.orgID),
			ScopeType:         billing.GrantScopeAccount,
			Amount:            deposited,
			Source:            cfg.prepaidSource,
			StripeReferenceID: fmt.Sprintf("seed:%d:%s", cfg.orgID, cfg.productID),
			ExpiresAt:         &expiresAt,
		}); err != nil {
			return fmt.Errorf("deposit prepaid credits: %w", err)
		}
	}

	after, err := currentPrepaidUnits(ctx, client, cfg.orgID, cfg.productID)
	if err != nil {
		return err
	}

	encoded, err := json.Marshal(seedResult{
		OrgID:               cfg.orgID,
		OrgName:             cfg.orgName,
		OrgTrustTier:        cfg.orgTrustTier,
		ProductID:           cfg.productID,
		PlanID:              cfg.planID,
		PrepaidBefore:       before,
		DepositedUnits:      deposited,
		PrepaidAfter:        after,
		TargetPrepaidUnits:  cfg.targetPrepaidUnits,
		ProductUpserted:     productUpserted,
		CatalogUpserted:     catalogUpserted,
		DefaultPlanUpserted: defaultPlanUpserted,
	})
	if err != nil {
		return fmt.Errorf("marshal seed result: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

func currentPrepaidUnits(ctx context.Context, client *billing.Client, orgID uint64, productID string) (uint64, error) {
	grants, err := client.ListGrantBalances(ctx, billing.OrgID(orgID), productID)
	if err != nil {
		return 0, err
	}
	var total uint64
	for _, grant := range grants {
		total += grant.Available
	}
	return total, nil
}

func parseFlags() (config, error) {
	cfg := config{
		productID:           sandboxProductID,
		productDisplayName:  "Sandbox",
		meterUnit:           "sku_second",
		billingModel:        "metered",
		orgTrustTier:        "new",
		planID:              "sandbox-default",
		planDisplayName:     "Sandbox PAYG",
		includedBucketsJSON: `{}`,
		quotasJSON:          `{}`,
		targetPrepaidUnits:  500_000_000,
		prepaidSource:       "purchase",
		expiresAfter:        365 * 24 * time.Hour,
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
	flag.StringVar(&cfg.includedBucketsJSON, "included-credit-buckets-json", cfg.includedBucketsJSON, "default plan included credit buckets JSON")
	flag.StringVar(&cfg.quotasJSON, "quotas-json", cfg.quotasJSON, "default plan quotas JSON")
	flag.Uint64Var(&cfg.targetPrepaidUnits, "target-prepaid-units", cfg.targetPrepaidUnits, "minimum prepaid units to ensure after seeding")
	flag.StringVar(&cfg.prepaidSource, "prepaid-source", cfg.prepaidSource, "credit grant source for seeded prepaid units")
	flag.DurationVar(&cfg.expiresAfter, "expires-after", cfg.expiresAfter, "duration until seeded credits expire")
	flag.Parse()

	switch {
	case cfg.pgDSN == "" && cfg.pgDSNFile == "":
		return config{}, fmt.Errorf("either --pg-dsn or --pg-dsn-file is required")
	case cfg.orgID == 0:
		return config{}, fmt.Errorf("--org-id is required")
	case cfg.orgName == "":
		cfg.orgName = fmt.Sprintf("org-%d", cfg.orgID)
	case cfg.productID != sandboxProductID:
		return config{}, fmt.Errorf("billing seed catalog currently owns product %q", sandboxProductID)
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
	reservePolicy := map[string]any{
		"shape":                   "time",
		"target_quantity":         300,
		"min_quantity":            30,
		"allow_partial_reserve":   true,
		"renew_slack_quantity":    60,
		"operator_grace_quantity": 0,
	}
	reservePolicyJSON, err := json.Marshal(reservePolicy)
	if err != nil {
		return false, err
	}
	result, err := pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model, reserve_policy)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (product_id) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    meter_unit = EXCLUDED.meter_unit,
		    billing_model = EXCLUDED.billing_model,
		    reserve_policy = EXCLUDED.reserve_policy,
		    updated_at = now()
	`, cfg.productID, cfg.productDisplayName, cfg.meterUnit, cfg.billingModel, string(reservePolicyJSON))
	if err != nil {
		return false, fmt.Errorf("upsert product %s: %w", cfg.productID, err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

func upsertCatalog(ctx context.Context, pg *sql.DB, cfg config) (bool, error) {
	var changed bool
	for _, bucket := range sandboxBuckets() {
		result, err := pg.ExecContext(ctx, `
			INSERT INTO credit_buckets (bucket_id, display_name, sort_order)
			VALUES ($1, $2, $3)
			ON CONFLICT (bucket_id) DO UPDATE
			SET display_name = EXCLUDED.display_name,
			    sort_order = EXCLUDED.sort_order,
			    updated_at = now()
		`, bucket.BucketID, bucket.DisplayName, bucket.SortOrder)
		if err != nil {
			return false, fmt.Errorf("upsert bucket %s: %w", bucket.BucketID, err)
		}
		rows, _ := result.RowsAffected()
		changed = changed || rows > 0
	}
	for _, sku := range sandboxSKUs() {
		result, err := pg.ExecContext(ctx, `
			INSERT INTO skus (sku_id, product_id, bucket_id, display_name, quantity_unit, active)
			VALUES ($1, $2, $3, $4, $5, true)
			ON CONFLICT (sku_id) DO UPDATE
			SET product_id = EXCLUDED.product_id,
			    bucket_id = EXCLUDED.bucket_id,
			    display_name = EXCLUDED.display_name,
			    quantity_unit = EXCLUDED.quantity_unit,
			    active = EXCLUDED.active,
			    updated_at = now()
		`, sku.SKUID, cfg.productID, sku.BucketID, sku.DisplayName, sku.QuantityUnit)
		if err != nil {
			return false, fmt.Errorf("upsert sku %s: %w", sku.SKUID, err)
		}
		rows, _ := result.RowsAffected()
		changed = changed || rows > 0
	}
	return changed, nil
}

func upsertDefaultPlan(ctx context.Context, pg *sql.DB, cfg config) (bool, error) {
	result, err := pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, billing_mode, included_credit_buckets, quotas, is_default, tier, active)
		VALUES ($1, $2, $3, 'prepaid', $4::jsonb, $5::jsonb, true, 'default', true)
		ON CONFLICT (plan_id) DO UPDATE
		SET product_id = EXCLUDED.product_id,
		    display_name = EXCLUDED.display_name,
		    billing_mode = EXCLUDED.billing_mode,
		    included_credit_buckets = EXCLUDED.included_credit_buckets,
		    quotas = EXCLUDED.quotas,
		    is_default = EXCLUDED.is_default,
		    tier = EXCLUDED.tier,
		    active = EXCLUDED.active,
		    updated_at = now()
	`, cfg.planID, cfg.productID, cfg.planDisplayName, cfg.includedBucketsJSON, cfg.quotasJSON)
	if err != nil {
		return false, fmt.Errorf("upsert default plan %s: %w", cfg.planID, err)
	}
	rows, _ := result.RowsAffected()
	changed := rows > 0
	for _, sku := range sandboxSKUs() {
		if sku.UnitRate > uint64(1<<63-1) {
			return false, fmt.Errorf("sku %s unit rate exceeds postgres bigint", sku.SKUID)
		}
		result, err := pg.ExecContext(ctx, `
			INSERT INTO plan_sku_rates (plan_id, sku_id, unit_rate, active)
			VALUES ($1, $2, $3, true)
			ON CONFLICT (plan_id, sku_id) DO UPDATE
			SET unit_rate = EXCLUDED.unit_rate,
			    active = EXCLUDED.active,
			    updated_at = now()
		`, cfg.planID, sku.SKUID, int64(sku.UnitRate))
		if err != nil {
			return false, fmt.Errorf("upsert plan sku rate %s/%s: %w", cfg.planID, sku.SKUID, err)
		}
		rows, _ := result.RowsAffected()
		changed = changed || rows > 0
	}
	return changed, nil
}

func sandboxBuckets() []bucketSeed {
	return []bucketSeed{
		{BucketID: "compute", DisplayName: "Compute", SortOrder: 10},
		{BucketID: "memory", DisplayName: "Memory", SortOrder: 20},
		{BucketID: "block_storage", DisplayName: "Block Storage", SortOrder: 30},
	}
}

func sandboxSKUs() []skuSeed {
	return []skuSeed{
		{
			SKUID:        sandboxComputeSKU,
			BucketID:     "compute",
			DisplayName:  "AMD EPYC 4484PX @ 5.66GHz",
			QuantityUnit: "vCPU-second",
			UnitRate:     325_000,
		},
		{
			SKUID:        sandboxMemorySKU,
			BucketID:     "memory",
			DisplayName:  "Standard Memory",
			QuantityUnit: "GiB-second",
			UnitRate:     40_000,
		},
		{
			SKUID:        sandboxBlockStorageSKU,
			BucketID:     "block_storage",
			DisplayName:  "Premium NVMe",
			QuantityUnit: "GiB-second",
			UnitRate:     10_000,
		},
	}
}
