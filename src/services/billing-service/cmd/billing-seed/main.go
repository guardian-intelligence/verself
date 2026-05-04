package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/verself/billing-service/internal/billing"
	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/service-runtime/envconfig"
)

const (
	sandboxProductID                = "sandbox"
	sandboxComputeSKU               = "sandbox_compute_amd_epyc_4484px_vcpu_ms"
	sandboxMemorySKU                = "sandbox_memory_standard_gib_ms"
	sandboxExecutionRootStorageSKU  = "sandbox_execution_root_storage_premium_nvme_gib_ms"
	sandboxDurableVolumeLiveSKU     = "sandbox_durable_volume_live_storage_gib_ms"
	sandboxDurableVolumeRetainedSKU = "sandbox_durable_volume_retained_snapshot_gib_ms"
	secretsProductID                = "secrets"
	secretsKVOperationSKU           = "secrets_kv_operation"
	secretsCredentialOperationSKU   = "secrets_credential_operation"
	secretsTransitOperationSKU      = "secrets_transit_operation"
)

type config struct {
	pgDSNFile            string
	pgDSN                string
	orgID                uint64
	orgName              string
	orgTrustTier         string
	productID            string
	productDisplayName   string
	meterUnit            string
	billingModel         string
	planID               string
	planDisplayName      string
	freeTierBucketsJSON  string
	planEntitlementsJSON string
	targetPrepaidUnits   uint64
	prepaidSource        string
	expiresAfter         time.Duration
	skuScopedGrantsJSON  string
}

type seedResult struct {
	OrgID                uint64 `json:"org_id"`
	OrgName              string `json:"org_name"`
	OrgTrustTier         string `json:"org_trust_tier"`
	ProductID            string `json:"product_id"`
	PlanID               string `json:"plan_id"`
	PrepaidBefore        uint64 `json:"prepaid_before"`
	DepositedUnits       uint64 `json:"deposited_units"`
	PrepaidAfter         uint64 `json:"prepaid_after"`
	TargetPrepaidUnits   uint64 `json:"target_prepaid_units"`
	ProductUpserted      bool   `json:"product_upserted"`
	CatalogUpserted      bool   `json:"catalog_upserted"`
	DefaultPlanUpserted  bool   `json:"default_plan_upserted"`
	EntitlementsUpserted bool   `json:"entitlements_upserted"`
	SKUGrantsDeposited   int    `json:"sku_grants_deposited"`
}

type skuScopedGrantSpec struct {
	SKUID  string `json:"sku_id"`
	Units  uint64 `json:"units"`
	Source string `json:"source"`
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

type productSeed struct {
	Buckets       []bucketSeed
	SKUs          []skuSeed
	ReservePolicy string
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
	entitlementsUpserted, err := upsertEntitlementPolicies(ctx, pg, cfg)
	if err != nil {
		return err
	}
	if err := client.EnsureOrg(ctx, billing.OrgID(cfg.orgID), cfg.orgName, cfg.orgTrustTier); err != nil {
		return err
	}
	if err := client.EnsureCurrentEntitlements(ctx, billing.OrgID(cfg.orgID), cfg.productID); err != nil {
		return err
	}
	before, err := currentPrepaidUnits(ctx, client, cfg.orgID, cfg.productID)
	if err != nil {
		return err
	}
	deposited := uint64(0)
	if before < cfg.targetPrepaidUnits {
		deposited = cfg.targetPrepaidUnits - before
		expiresAt := time.Now().UTC().Add(cfg.expiresAfter)
		_, err := client.DepositCredits(ctx, billing.GrantBalance{OrgID: billing.OrgID(cfg.orgID), ScopeType: "account", Amount: deposited, Source: cfg.prepaidSource, SourceReferenceID: fmt.Sprintf("seed:%d:%s", cfg.orgID, cfg.productID), ExpiresAt: &expiresAt})
		if err != nil {
			return fmt.Errorf("deposit prepaid credits: %w", err)
		}
	}
	skuGrants, err := depositSKUScopedGrants(ctx, client, pg, cfg)
	if err != nil {
		return err
	}
	after, err := currentPrepaidUnits(ctx, client, cfg.orgID, cfg.productID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(seedResult{OrgID: cfg.orgID, OrgName: cfg.orgName, OrgTrustTier: cfg.orgTrustTier, ProductID: cfg.productID, PlanID: cfg.planID, PrepaidBefore: before, DepositedUnits: deposited, PrepaidAfter: after, TargetPrepaidUnits: cfg.targetPrepaidUnits, ProductUpserted: productUpserted, CatalogUpserted: catalogUpserted, DefaultPlanUpserted: defaultPlanUpserted, EntitlementsUpserted: entitlementsUpserted, SKUGrantsDeposited: skuGrants})
	if err != nil {
		return fmt.Errorf("marshal seed result: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
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

func parseFlags() (config, error) {
	cfg := config{productID: sandboxProductID, productDisplayName: "Sandbox", meterUnit: "sku_ms", billingModel: "metered", orgTrustTier: "new", planID: "sandbox-default", planDisplayName: "Sandbox PAYG", freeTierBucketsJSON: `{}`, planEntitlementsJSON: `{}`, targetPrepaidUnits: 500_000_000, prepaidSource: "purchase", expiresAfter: 365 * 24 * time.Hour}
	flag.StringVar(&cfg.pgDSNFile, "pg-dsn-file", "", "path to PostgreSQL DSN file")
	flag.StringVar(&cfg.pgDSN, "pg-dsn", "", "PostgreSQL DSN")
	flag.Uint64Var(&cfg.orgID, "org-id", 0, "org ID to seed")
	flag.StringVar(&cfg.orgName, "org-name", "", "org display name")
	flag.StringVar(&cfg.orgTrustTier, "org-trust-tier", cfg.orgTrustTier, "org trust tier")
	flag.StringVar(&cfg.productID, "product-id", cfg.productID, "product ID")
	flag.StringVar(&cfg.productDisplayName, "product-display-name", cfg.productDisplayName, "product display name")
	flag.StringVar(&cfg.meterUnit, "meter-unit", cfg.meterUnit, "meter unit")
	flag.StringVar(&cfg.billingModel, "billing-model", cfg.billingModel, "billing model")
	flag.StringVar(&cfg.planID, "plan-id", cfg.planID, "default plan ID")
	flag.StringVar(&cfg.planDisplayName, "plan-display-name", cfg.planDisplayName, "default plan display name")
	flag.StringVar(&cfg.freeTierBucketsJSON, "free-tier-buckets-json", cfg.freeTierBucketsJSON, "free-tier buckets JSON")
	flag.StringVar(&cfg.planEntitlementsJSON, "plan-entitlements-json", cfg.planEntitlementsJSON, "default-plan contract entitlements JSON")
	flag.Uint64Var(&cfg.targetPrepaidUnits, "target-prepaid-units", cfg.targetPrepaidUnits, "target prepaid units")
	flag.StringVar(&cfg.prepaidSource, "prepaid-source", cfg.prepaidSource, "credit source")
	flag.DurationVar(&cfg.expiresAfter, "expires-after", cfg.expiresAfter, "credit expiry duration")
	flag.StringVar(&cfg.skuScopedGrantsJSON, "sku-scoped-grants-json", "", "SKU-scoped grants JSON")
	flag.Parse()
	if cfg.pgDSN == "" && cfg.pgDSNFile == "" {
		return config{}, fmt.Errorf("either --pg-dsn or --pg-dsn-file is required")
	}
	if cfg.orgID == 0 {
		return config{}, fmt.Errorf("--org-id is required")
	}
	if cfg.orgName == "" {
		cfg.orgName = fmt.Sprintf("org-%d", cfg.orgID)
	}
	if _, err := productSeedFor(cfg.productID); err != nil {
		return config{}, err
	}
	return cfg, nil
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

func upsertProduct(ctx context.Context, pg *pgxpool.Pool, cfg config) (bool, error) {
	seed, err := productSeedFor(cfg.productID)
	if err != nil {
		return false, err
	}
	_, err = pg.Exec(ctx, `INSERT INTO products (product_id, display_name, meter_unit, billing_model, reserve_policy) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (product_id) DO UPDATE SET display_name = EXCLUDED.display_name, meter_unit = EXCLUDED.meter_unit, billing_model = EXCLUDED.billing_model, reserve_policy = EXCLUDED.reserve_policy`, cfg.productID, cfg.productDisplayName, cfg.meterUnit, cfg.billingModel, []byte(seed.ReservePolicy))
	return true, err
}

func upsertCatalog(ctx context.Context, pg *pgxpool.Pool, cfg config) (bool, error) {
	seed, err := productSeedFor(cfg.productID)
	if err != nil {
		return false, err
	}
	for _, bucket := range seed.Buckets {
		if _, err := pg.Exec(ctx, `INSERT INTO credit_buckets (bucket_id, display_name, sort_order) VALUES ($1,$2,$3) ON CONFLICT (bucket_id) DO UPDATE SET display_name = EXCLUDED.display_name, sort_order = EXCLUDED.sort_order`, bucket.BucketID, bucket.DisplayName, bucket.SortOrder); err != nil {
			return false, err
		}
	}
	for _, sku := range seed.SKUs {
		if _, err := pg.Exec(ctx, `INSERT INTO skus (sku_id, product_id, bucket_id, display_name, quantity_unit, active) VALUES ($1,$2,$3,$4,$5,true) ON CONFLICT (sku_id) DO UPDATE SET product_id = EXCLUDED.product_id, bucket_id = EXCLUDED.bucket_id, display_name = EXCLUDED.display_name, quantity_unit = EXCLUDED.quantity_unit, active = true`, sku.SKUID, cfg.productID, sku.BucketID, sku.DisplayName, sku.QuantityUnit); err != nil {
			return false, err
		}
	}
	return true, nil
}

func upsertDefaultPlan(ctx context.Context, pg *pgxpool.Pool, cfg config) (bool, error) {
	seed, err := productSeedFor(cfg.productID)
	if err != nil {
		return false, err
	}
	if _, err := pg.Exec(ctx, `INSERT INTO plans (plan_id, product_id, display_name, billing_mode, tier, is_default, active, monthly_amount_cents, currency) VALUES ($1,$2,$3,'prepaid','default',true,true,0,'usd') ON CONFLICT (plan_id) DO UPDATE SET product_id = EXCLUDED.product_id, display_name = EXCLUDED.display_name, billing_mode = EXCLUDED.billing_mode, tier = EXCLUDED.tier, is_default = true, active = true`, cfg.planID, cfg.productID, cfg.planDisplayName); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	for _, sku := range seed.SKUs {
		rateID := "rate:" + cfg.planID + ":" + sku.SKUID
		if _, err := pg.Exec(ctx, `INSERT INTO plan_sku_rates (rate_id, plan_id, sku_id, unit_rate, active, active_from) VALUES ($1,$2,$3,$4,true,$5) ON CONFLICT (rate_id) DO UPDATE SET unit_rate = EXCLUDED.unit_rate, active = true`, rateID, cfg.planID, sku.SKUID, int64FromUint64(sku.UnitRate, "sku unit rate"), now); err != nil {
			return false, err
		}
	}
	return true, nil
}

func upsertEntitlementPolicies(ctx context.Context, pg *pgxpool.Pool, cfg config) (bool, error) {
	free, err := parseUint64JSONMap(cfg.freeTierBucketsJSON)
	if err != nil {
		return false, err
	}
	plan, err := parseUint64JSONMap(cfg.planEntitlementsJSON)
	if err != nil {
		return false, err
	}
	for _, bucketID := range sortedMapKeys(free) {
		if err := upsertEntitlementPolicy(ctx, pg, fmt.Sprintf("free-tier:%s:%s:v1", cfg.productID, bucketID), "free_tier", cfg.productID, bucketID, free[bucketID]); err != nil {
			return false, err
		}
	}
	for _, bucketID := range sortedMapKeys(plan) {
		policyID := fmt.Sprintf("contract:%s:%s:%s:v1", cfg.planID, cfg.productID, bucketID)
		if err := upsertEntitlementPolicy(ctx, pg, policyID, "contract", cfg.productID, bucketID, plan[bucketID]); err != nil {
			return false, err
		}
		if _, err := pg.Exec(ctx, `INSERT INTO plan_entitlements (plan_id, policy_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, cfg.planID, policyID); err != nil {
			return false, err
		}
	}
	return true, nil
}

func upsertEntitlementPolicy(ctx context.Context, pg *pgxpool.Pool, policyID, source, productID, bucketID string, amount uint64) error {
	_, err := pg.Exec(ctx, `
		INSERT INTO entitlement_policies (policy_id, product_id, source, scope_type, scope_product_id, scope_bucket_id, amount_units, cadence, anchor_kind, proration_mode, active_from, policy_version)
		VALUES ($1,$2,$3,'bucket',$2,$4,$5,'monthly','billing_cycle','prorate_by_time_left',$6,'v1')
		ON CONFLICT (policy_id) DO UPDATE SET amount_units = EXCLUDED.amount_units, active_from = LEAST(entitlement_policies.active_from, EXCLUDED.active_from), active_until = NULL
	`, policyID, productID, source, bucketID, int64FromUint64(amount, "entitlement policy amount"), time.Now().UTC())
	return err
}

func currentPrepaidUnits(ctx context.Context, client *billing.Client, orgID uint64, productID string) (uint64, error) {
	grants, err := client.ListGrantBalances(ctx, billing.OrgID(orgID), productID)
	if err != nil {
		return 0, err
	}
	var total uint64
	for _, grant := range grants {
		if grant.Source != "free_tier" {
			total += grant.Available
		}
	}
	return total, nil
}

func int64FromUint64(value uint64, field string) int64 {
	const maxInt64AsUint64 = uint64(1<<63 - 1)
	if value > maxInt64AsUint64 {
		panic(fmt.Sprintf("%s exceeds int64 range: %d", field, value))
	}
	return int64(value) // #nosec G115 -- value is checked against MaxInt64 above.
}

func depositSKUScopedGrants(ctx context.Context, client *billing.Client, pg *pgxpool.Pool, cfg config) (int, error) {
	if strings.TrimSpace(cfg.skuScopedGrantsJSON) == "" {
		return 0, nil
	}
	var specs []skuScopedGrantSpec
	if err := json.Unmarshal([]byte(cfg.skuScopedGrantsJSON), &specs); err != nil {
		return 0, err
	}
	expiresAt := time.Now().UTC().Add(cfg.expiresAfter)
	count := 0
	for _, spec := range specs {
		if spec.Source == "" {
			spec.Source = "promo"
		}
		var productID, bucketID string
		if err := pg.QueryRow(ctx, `SELECT product_id, bucket_id FROM skus WHERE sku_id = $1`, spec.SKUID).Scan(&productID, &bucketID); err != nil {
			return count, err
		}
		_, err := client.DepositCredits(ctx, billing.GrantBalance{OrgID: billing.OrgID(cfg.orgID), ScopeType: "sku", ScopeProductID: productID, ScopeBucketID: bucketID, ScopeSKUID: spec.SKUID, Amount: spec.Units, Source: spec.Source, SourceReferenceID: fmt.Sprintf("seed-sku:%d:%s:%s", cfg.orgID, spec.SKUID, spec.Source), ExpiresAt: &expiresAt})
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func parseUint64JSONMap(raw string) (map[string]uint64, error) {
	out := map[string]uint64{}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func sortedMapKeys(in map[string]uint64) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func productSeedFor(productID string) (productSeed, error) {
	switch productID {
	case sandboxProductID:
		return productSeed{
			ReservePolicy: `{"shape":"time","target_quantity":300000}`,
			Buckets: []bucketSeed{
				{"compute", "Compute", 10},
				{"memory", "Memory", 20},
				{"execution_root_storage", "Execution Root Storage", 30},
				{"durable_volume_storage", "Durable Volume Storage", 40},
			},
			SKUs: []skuSeed{
				{sandboxComputeSKU, "compute", "AMD EPYC 4484PX", "vCPU-ms", 325},
				{sandboxMemorySKU, "memory", "DDR5-5200", "GiB-ms", 40},
				{sandboxExecutionRootStorageSKU, "execution_root_storage", "Premium NVMe root disk", "GiB-ms", 10},
				{sandboxDurableVolumeLiveSKU, "durable_volume_storage", "Durable volume live bytes", "GiB-ms", 10},
				{sandboxDurableVolumeRetainedSKU, "durable_volume_storage", "Durable volume retained snapshots", "GiB-ms", 5},
			},
		}, nil
	case secretsProductID:
		return productSeed{
			ReservePolicy: `{"shape":"count","target_quantity":1}`,
			Buckets: []bucketSeed{
				{"secrets_operations", "Secrets Operations", 50},
			},
			SKUs: []skuSeed{
				{secretsKVOperationSKU, "secrets_operations", "KV operation", "operation", 0},
				{secretsCredentialOperationSKU, "secrets_operations", "Credential operation", "operation", 0},
				{secretsTransitOperationSKU, "secrets_operations", "Transit operation", "operation", 0},
			},
		}, nil
	default:
		return productSeed{}, fmt.Errorf("billing seed does not know product %q", productID)
	}
}
