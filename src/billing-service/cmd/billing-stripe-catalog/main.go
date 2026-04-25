package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v85"
)

const (
	defaultProductID          = "sandbox"
	defaultProductDisplayName = "Sandbox"
	defaultRateSourcePlanID   = "sandbox-default"
	billingEventsSink         = "clickhouse.billing_events"
)

type config struct {
	pgDSNFile           string
	pgDSN               string
	stripeSecretKeyFile string
	stripeSecretKey     string
	productID           string
	productDisplayName  string
	rateSourcePlanID    string
	tiersJSON           string
}

type tierConfig struct {
	PlanID          string            `json:"plan_id"`
	DisplayName     string            `json:"display_name"`
	Tier            string            `json:"tier"`
	Currency        string            `json:"currency"`
	Cadence         string            `json:"cadence"`
	UnitAmountCents int64             `json:"unit_amount_cents"`
	Entitlements    map[string]uint64 `json:"entitlements"`
}

type catalogResult struct {
	ReconciliationID string       `json:"reconciliation_id"`
	ProductID        string       `json:"product_id"`
	StripeProductID  string       `json:"stripe_product_id"`
	Plans            []planResult `json:"plans"`
}

type planResult struct {
	PlanID          string            `json:"plan_id"`
	StripePriceID   string            `json:"stripe_price_id"`
	LookupKey       string            `json:"lookup_key"`
	Cadence         string            `json:"cadence"`
	Currency        string            `json:"currency"`
	UnitAmountCents int64             `json:"unit_amount_cents"`
	Entitlements    map[string]uint64 `json:"entitlements"`
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
	tiers, err := parseTiers(cfg.tiersJSON)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	pgDSN, err := resolveSecret(cfg.pgDSN, cfg.pgDSNFile, "pg dsn")
	if err != nil {
		return err
	}
	stripeSecretKey, err := resolveSecret(cfg.stripeSecretKey, cfg.stripeSecretKeyFile, "stripe secret key")
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

	stripeClient := stripe.NewClient(stripeSecretKey)
	product, err := ensureStripeProduct(ctx, stripeClient, stripeProductID(cfg.productID), cfg)
	if err != nil {
		return err
	}
	reconciliationID, err := newReconciliationID()
	if err != nil {
		return err
	}

	result := catalogResult{
		ReconciliationID: reconciliationID,
		ProductID:        cfg.productID,
		StripeProductID:  product.ID,
		Plans:            make([]planResult, 0, len(tiers)),
	}
	for _, tier := range tiers {
		price, err := ensureStripePrice(ctx, stripeClient, product.ID, cfg.productID, tier)
		if err != nil {
			return err
		}
		if err := upsertPlan(ctx, pg, cfg, tier, price.ID); err != nil {
			return err
		}
		if err := upsertPlanRates(ctx, pg, cfg.rateSourcePlanID, tier.PlanID); err != nil {
			return err
		}
		if err := upsertPlanEntitlements(ctx, pg, cfg.productID, tier); err != nil {
			return err
		}
		if err := recordCatalogEvent(ctx, pg, reconciliationID, cfg, tier, product.ID, price.ID); err != nil {
			return err
		}
		if err := verifyPlan(ctx, pg, cfg.productID, tier, price.ID); err != nil {
			return err
		}
		result.Plans = append(result.Plans, planResult{
			PlanID:          tier.PlanID,
			StripePriceID:   price.ID,
			LookupKey:       priceLookupKey(cfg.productID, tier),
			Cadence:         tier.Cadence,
			Currency:        tier.Currency,
			UnitAmountCents: tier.UnitAmountCents,
			Entitlements:    tier.Entitlements,
		})
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal catalog result: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

func parseFlags() (config, error) {
	cfg := config{productID: defaultProductID, productDisplayName: defaultProductDisplayName, rateSourcePlanID: defaultRateSourcePlanID}
	flag.StringVar(&cfg.pgDSNFile, "pg-dsn-file", "", "path to PostgreSQL DSN file")
	flag.StringVar(&cfg.pgDSN, "pg-dsn", "", "PostgreSQL DSN")
	flag.StringVar(&cfg.stripeSecretKeyFile, "stripe-secret-key-file", "", "path to Stripe secret key file")
	flag.StringVar(&cfg.stripeSecretKey, "stripe-secret-key", "", "Stripe secret key")
	flag.StringVar(&cfg.productID, "product-id", cfg.productID, "Verself product ID")
	flag.StringVar(&cfg.productDisplayName, "product-display-name", cfg.productDisplayName, "Stripe product display name")
	flag.StringVar(&cfg.rateSourcePlanID, "rate-source-plan-id", cfg.rateSourcePlanID, "existing plan whose SKU rates should be copied")
	flag.StringVar(&cfg.tiersJSON, "tiers-json", "", "contract tier catalog JSON")
	flag.Parse()

	switch {
	case cfg.pgDSN == "" && cfg.pgDSNFile == "":
		return config{}, fmt.Errorf("either --pg-dsn or --pg-dsn-file is required")
	case cfg.stripeSecretKey == "" && cfg.stripeSecretKeyFile == "":
		return config{}, fmt.Errorf("either --stripe-secret-key or --stripe-secret-key-file is required")
	case cfg.tiersJSON == "":
		return config{}, fmt.Errorf("--tiers-json is required")
	case cfg.productID == "":
		return config{}, fmt.Errorf("--product-id is required")
	case cfg.productDisplayName == "":
		return config{}, fmt.Errorf("--product-display-name is required")
	case cfg.rateSourcePlanID == "":
		return config{}, fmt.Errorf("--rate-source-plan-id is required")
	}
	return cfg, nil
}

func parseTiers(raw string) ([]tierConfig, error) {
	var tiers []tierConfig
	if err := json.Unmarshal([]byte(raw), &tiers); err != nil {
		return nil, fmt.Errorf("parse tiers json: %w", err)
	}
	if len(tiers) == 0 {
		return nil, fmt.Errorf("at least one tier is required")
	}
	seen := map[string]struct{}{}
	for index := range tiers {
		tier := &tiers[index]
		tier.PlanID = strings.TrimSpace(tier.PlanID)
		tier.DisplayName = strings.TrimSpace(tier.DisplayName)
		tier.Tier = strings.TrimSpace(tier.Tier)
		tier.Currency = strings.ToLower(strings.TrimSpace(tier.Currency))
		tier.Cadence = strings.ToLower(strings.TrimSpace(tier.Cadence))
		if tier.Currency == "" {
			tier.Currency = "usd"
		}
		if tier.Cadence == "" {
			tier.Cadence = "monthly"
		}
		switch {
		case tier.PlanID == "":
			return nil, fmt.Errorf("tier %d: plan_id is required", index)
		case tier.DisplayName == "":
			return nil, fmt.Errorf("tier %s: display_name is required", tier.PlanID)
		case tier.Tier == "":
			tier.Tier = tier.PlanID
		case tier.Cadence != "monthly":
			return nil, fmt.Errorf("tier %s: unsupported cadence %q", tier.PlanID, tier.Cadence)
		case tier.UnitAmountCents <= 0:
			return nil, fmt.Errorf("tier %s: unit_amount_cents must be positive", tier.PlanID)
		case tier.UnitAmountCents > int64(1<<53-1):
			return nil, fmt.Errorf("tier %s: unit_amount_cents exceeds JS safe integer", tier.PlanID)
		}
		if _, ok := seen[tier.PlanID]; ok {
			return nil, fmt.Errorf("duplicate plan_id %s", tier.PlanID)
		}
		seen[tier.PlanID] = struct{}{}
		for bucketID := range tier.Entitlements {
			if strings.TrimSpace(bucketID) == "" {
				return nil, fmt.Errorf("tier %s: bucket id is required", tier.PlanID)
			}
		}
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].UnitAmountCents < tiers[j].UnitAmountCents })
	return tiers, nil
}

func resolveSecret(value string, filePath string, label string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), nil
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", label, err)
	}
	resolved := strings.TrimSpace(string(raw))
	if resolved == "" {
		return "", fmt.Errorf("%s file %s is empty", label, filePath)
	}
	return resolved, nil
}

func ensureStripeProduct(ctx context.Context, stripeClient *stripe.Client, stripeProductID string, cfg config) (*stripe.Product, error) {
	product, err := stripeClient.V1Products.Retrieve(ctx, stripeProductID, nil)
	if err == nil {
		updated, err := stripeClient.V1Products.Update(ctx, product.ID, &stripe.ProductUpdateParams{Active: stripe.Bool(true), Name: stripe.String(cfg.productDisplayName), Metadata: map[string]string{"verself_product_id": cfg.productID}})
		if err != nil {
			return nil, fmt.Errorf("update stripe product %s: %w", product.ID, err)
		}
		return updated, nil
	}
	if !isStripeMissing(err) {
		return nil, fmt.Errorf("retrieve stripe product %s: %w", stripeProductID, err)
	}
	created, err := stripeClient.V1Products.Create(ctx, &stripe.ProductCreateParams{ID: stripe.String(stripeProductID), Active: stripe.Bool(true), Name: stripe.String(cfg.productDisplayName), Metadata: map[string]string{"verself_product_id": cfg.productID}})
	if err != nil {
		return nil, fmt.Errorf("create stripe product %s: %w", stripeProductID, err)
	}
	return created, nil
}

func ensureStripePrice(ctx context.Context, stripeClient *stripe.Client, stripeProductID string, productID string, tier tierConfig) (*stripe.Price, error) {
	lookupKey := priceLookupKey(productID, tier)
	params := &stripe.PriceListParams{LookupKeys: []*string{stripe.String(lookupKey)}}
	params.Filters.AddFilter("limit", "", "10")
	var existing []*stripe.Price
	list := stripeClient.V1Prices.List(ctx, params)
	existing = append(existing, list.Data()...)
	if err := list.Err(); err != nil {
		return nil, fmt.Errorf("list stripe prices for lookup_key %s: %w", lookupKey, err)
	}
	for _, price := range existing {
		if stripePriceMatches(price, stripeProductID, tier) {
			return price, nil
		}
	}

	price, err := stripeClient.V1Prices.Create(ctx, &stripe.PriceCreateParams{
		Active:            stripe.Bool(true),
		Currency:          stripe.String(tier.Currency),
		LookupKey:         stripe.String(lookupKey),
		Metadata:          priceMetadata(productID, tier),
		Nickname:          stripe.String(tier.DisplayName + " monthly"),
		Product:           stripe.String(stripeProductID),
		TransferLookupKey: stripe.Bool(len(existing) > 0),
		Recurring: &stripe.PriceCreateRecurringParams{
			Interval:  stripe.String(string(stripe.PriceRecurringIntervalMonth)),
			UsageType: stripe.String(string(stripe.PriceRecurringUsageTypeLicensed)),
		},
		UnitAmount: stripe.Int64(tier.UnitAmountCents),
	})
	if err != nil {
		return nil, fmt.Errorf("create stripe price for plan %s: %w", tier.PlanID, err)
	}
	return price, nil
}

func stripePriceMatches(price *stripe.Price, stripeProductID string, tier tierConfig) bool {
	if price == nil || !price.Active || price.Product == nil || price.Recurring == nil {
		return false
	}
	return price.Product.ID == stripeProductID &&
		strings.EqualFold(string(price.Currency), tier.Currency) &&
		price.UnitAmount == tier.UnitAmountCents &&
		price.Recurring.Interval == stripe.PriceRecurringIntervalMonth &&
		price.Recurring.IntervalCount == 1
}

func upsertPlan(ctx context.Context, pg *pgxpool.Pool, cfg config, tier tierConfig, stripePriceID string) error {
	_, err := pg.Exec(ctx, `
		INSERT INTO plans (
			plan_id, product_id, display_name, billing_mode, is_default, tier, active,
			stripe_price_id_monthly, monthly_amount_cents, currency
		)
		VALUES ($1, $2, $3, 'prepaid', false, $4, true, $5, $6, $7)
		ON CONFLICT (plan_id) DO UPDATE
		SET product_id = EXCLUDED.product_id,
		    display_name = EXCLUDED.display_name,
		    billing_mode = EXCLUDED.billing_mode,
		    is_default = EXCLUDED.is_default,
		    tier = EXCLUDED.tier,
		    active = EXCLUDED.active,
		    stripe_price_id_monthly = EXCLUDED.stripe_price_id_monthly,
		    monthly_amount_cents = EXCLUDED.monthly_amount_cents,
		    currency = EXCLUDED.currency,
		    updated_at = now()
	`, tier.PlanID, cfg.productID, tier.DisplayName, tier.Tier, stripePriceID, tier.UnitAmountCents, tier.Currency)
	if err != nil {
		return fmt.Errorf("upsert plan %s: %w", tier.PlanID, err)
	}
	return nil
}

func upsertPlanRates(ctx context.Context, pg *pgxpool.Pool, sourcePlanID string, planID string) error {
	now := time.Now().UTC()
	result, err := pg.Exec(ctx, `
		INSERT INTO plan_sku_rates (rate_id, plan_id, sku_id, unit_rate, active, active_from, active_until)
		SELECT 'rate:' || $2 || ':' || sku_id, $2, sku_id, unit_rate, true, LEAST(active_from, $3), NULL
		FROM plan_sku_rates
		WHERE plan_id = $1
		  AND active
		  AND active_from <= $3
		  AND (active_until IS NULL OR active_until > $3)
		ON CONFLICT (rate_id) DO UPDATE
		SET unit_rate = EXCLUDED.unit_rate,
		    active = true,
		    active_until = NULL,
		    updated_at = now()
	`, sourcePlanID, planID, now)
	if err != nil {
		return fmt.Errorf("copy plan sku rates from %s to %s: %w", sourcePlanID, planID, err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("source plan %s has no active sku rates to copy", sourcePlanID)
	}
	return nil
}

func upsertPlanEntitlements(ctx context.Context, pg *pgxpool.Pool, productID string, tier tierConfig) error {
	now := time.Now().UTC()
	for _, bucketID := range sortedEntitlementBuckets(tier.Entitlements) {
		policyID := fmt.Sprintf("contract:%s:%s:%s:v1", tier.PlanID, productID, bucketID)
		_, err := pg.Exec(ctx, `
			INSERT INTO entitlement_policies (
				policy_id, source, product_id, scope_type, scope_product_id, scope_bucket_id,
				amount_units, cadence, anchor_kind, proration_mode, policy_version, active_from
			)
			VALUES ($1, 'contract', $2, 'bucket', $2, $3,
			        $4, 'monthly', 'billing_cycle', 'prorate_by_time_left', 'v1', $5)
			ON CONFLICT (policy_id) DO UPDATE
			SET source = EXCLUDED.source,
			    product_id = EXCLUDED.product_id,
			    scope_type = EXCLUDED.scope_type,
			    scope_product_id = EXCLUDED.scope_product_id,
			    scope_bucket_id = EXCLUDED.scope_bucket_id,
			    amount_units = EXCLUDED.amount_units,
			    cadence = EXCLUDED.cadence,
			    anchor_kind = EXCLUDED.anchor_kind,
			    proration_mode = EXCLUDED.proration_mode,
			    policy_version = EXCLUDED.policy_version,
			    active_from = LEAST(entitlement_policies.active_from, EXCLUDED.active_from),
			    active_until = NULL,
			    updated_at = now()
		`, policyID, productID, bucketID, int64(tier.Entitlements[bucketID]), now)
		if err != nil {
			return fmt.Errorf("upsert entitlement policy %s: %w", policyID, err)
		}
		_, err = pg.Exec(ctx, `
			INSERT INTO plan_entitlements (plan_id, policy_id)
			VALUES ($1, $2)
			ON CONFLICT (plan_id, policy_id) DO NOTHING
		`, tier.PlanID, policyID)
		if err != nil {
			return fmt.Errorf("upsert plan entitlement %s/%s: %w", tier.PlanID, policyID, err)
		}
	}
	return nil
}

func recordCatalogEvent(ctx context.Context, pg *pgxpool.Pool, reconciliationID string, cfg config, tier tierConfig, stripeProductID string, stripePriceID string) error {
	payload, err := json.Marshal(map[string]any{
		"reconciliation_id":       reconciliationID,
		"product_id":              cfg.productID,
		"plan_id":                 tier.PlanID,
		"tier":                    tier.Tier,
		"cadence":                 tier.Cadence,
		"currency":                tier.Currency,
		"unit_amount_cents":       tier.UnitAmountCents,
		"stripe_product_id":       stripeProductID,
		"stripe_price_id":         stripePriceID,
		"stripe_price_lookup_key": priceLookupKey(cfg.productID, tier),
		"entitlements":            tier.Entitlements,
	})
	if err != nil {
		return fmt.Errorf("marshal catalog event payload: %w", err)
	}
	eventID := deterministicID("billing-event", "contract_catalog_reconciled", reconciliationID, cfg.productID, tier.PlanID, stripePriceID)
	payloadHash := sha256.Sum256(payload)
	payloadHashHex := hex.EncodeToString(payloadHash[:])
	tx, err := pg.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin catalog event %s: %w", eventID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := tx.Exec(ctx, `
		INSERT INTO billing_events (
			event_id, event_type, event_version, aggregate_type, aggregate_id,
			org_id, product_id, occurred_at, payload, payload_hash
		)
		VALUES ($1, 'contract_catalog_reconciled', 1, 'plan', $2,
		        '', $3, now(), $4::jsonb, $5)
		ON CONFLICT (event_id) DO NOTHING
	`, eventID, tier.PlanID, cfg.productID, payload, payloadHashHex)
	if err != nil {
		return fmt.Errorf("insert catalog event %s: %w", eventID, err)
	}
	inserted := result.RowsAffected() > 0
	var existingHash string
	if err := tx.QueryRow(ctx, `SELECT payload_hash FROM billing_events WHERE event_id = $1`, eventID).Scan(&existingHash); err != nil {
		return fmt.Errorf("verify catalog event %s payload hash: %w", eventID, err)
	}
	if existingHash != payloadHashHex {
		return fmt.Errorf("catalog event %s payload hash mismatch: existing %s new %s", eventID, existingHash, payloadHashHex)
	}
	if inserted {
		if _, err := tx.Exec(ctx, `
			INSERT INTO billing_event_delivery_queue (event_id, sink, generation, state, next_attempt_at)
			VALUES ($1, $2, 1, 'pending', now())
			ON CONFLICT (event_id, sink) WHERE state <> 'dead_letter' DO NOTHING
		`, eventID, billingEventsSink); err != nil {
			return fmt.Errorf("insert catalog event delivery %s: %w", eventID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit catalog event %s: %w", eventID, err)
	}
	return nil
}

func newReconciliationID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate reconciliation id: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}

func verifyPlan(ctx context.Context, pg *pgxpool.Pool, productID string, tier tierConfig, stripePriceID string) error {
	var actualProductID string
	var actualPriceID string
	var actualAmount int64
	var actualCurrency string
	err := pg.QueryRow(ctx, `
		SELECT product_id, stripe_price_id_monthly, monthly_amount_cents, currency
		FROM plans
		WHERE plan_id = $1
	`, tier.PlanID).Scan(&actualProductID, &actualPriceID, &actualAmount, &actualCurrency)
	if err != nil {
		return fmt.Errorf("verify plan %s: %w", tier.PlanID, err)
	}
	if actualProductID != productID || actualPriceID != stripePriceID || actualAmount != tier.UnitAmountCents || actualCurrency != tier.Currency {
		return fmt.Errorf("plan %s did not stabilize: product=%s price=%s amount=%d currency=%s", tier.PlanID, actualProductID, actualPriceID, actualAmount, actualCurrency)
	}
	return nil
}

func isStripeMissing(err error) bool {
	var stripeErr *stripe.Error
	return errors.As(err, &stripeErr) && stripeErr.HTTPStatusCode == 404
}

func stripeProductID(productID string) string {
	return "verself_" + safeStripeID(productID)
}

func priceLookupKey(productID string, tier tierConfig) string {
	return strings.Join([]string{"verself", productID, tier.PlanID, tier.Cadence}, ":")
}

func priceMetadata(productID string, tier tierConfig) map[string]string {
	return map[string]string{
		"verself_product_id": productID,
		"verself_plan_id":    tier.PlanID,
		"verself_tier":       tier.Tier,
		"verself_cadence":    tier.Cadence,
	}
}

var unsafeStripeIDChar = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func safeStripeID(value string) string {
	value = unsafeStripeIDChar.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "unknown"
	}
	return value
}

func sortedEntitlementBuckets(entitlements map[string]uint64) []string {
	keys := make([]string, 0, len(entitlements))
	for key := range entitlements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func deterministicID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return "evt_" + hex.EncodeToString(h.Sum(nil))
}
