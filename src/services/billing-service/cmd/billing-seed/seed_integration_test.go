package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/verself/billing-service/migrations"
	"github.com/verself/service-runtime/pgtest"
)

func TestBillingSeedCatalogIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db := pgtest.Acquire(ctx, t, pgtest.Template{
		Service:     "billing-service",
		Fingerprint: migrations.Fingerprint(),
		Migrate: func(ctx context.Context, dsn string) error {
			return migrations.UpDSN(ctx, "billing-service", dsn)
		},
	})
	pg, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatalf("open billing postgres: %v", err)
	}
	defer pg.Close()

	sandbox := config{
		productID:            sandboxProductID,
		productDisplayName:   "Sandbox",
		meterUnit:            "sku_ms",
		billingModel:         "metered",
		planID:               "sandbox-default",
		planDisplayName:      "Sandbox PAYG",
		freeTierBucketsJSON:  `{"compute":1000,"memory":2000}`,
		planEntitlementsJSON: `{"execution_root_storage":3000}`,
		targetPrepaidUnits:   defaultTargetPrepaidUnits,
		prepaidSource:        "purchase",
		expiresAfter:         365 * 24 * time.Hour,
		orgID:                "org_ci",
		orgName:              "CI Org",
		orgTrustTier:         "new",
	}
	secrets := sandbox
	secrets.productID = secretsProductID
	secrets.productDisplayName = "Secrets"
	secrets.meterUnit = "operation"
	secrets.billingModel = "metered"
	secrets.planID = "secrets-default"
	secrets.planDisplayName = "Secrets PAYG"
	secrets.freeTierBucketsJSON = `{"secrets_operations":50}`
	secrets.planEntitlementsJSON = `{}`

	for _, cfg := range []config{sandbox, secrets, sandbox, secrets} {
		if _, err := upsertProduct(ctx, pg, cfg); err != nil {
			t.Fatalf("upsert product %s: %v", cfg.productID, err)
		}
		if _, err := upsertCatalog(ctx, pg, cfg); err != nil {
			t.Fatalf("upsert catalog %s: %v", cfg.productID, err)
		}
		if _, err := upsertDefaultPlan(ctx, pg, cfg); err != nil {
			t.Fatalf("upsert default plan %s: %v", cfg.productID, err)
		}
		if _, err := upsertEntitlementPolicies(ctx, pg, cfg); err != nil {
			t.Fatalf("upsert entitlement policies %s: %v", cfg.productID, err)
		}
	}

	assertScalar(t, ctx, pg, `SELECT reserve_policy->>'shape' FROM products WHERE product_id = 'sandbox'`, "time")
	assertScalar(t, ctx, pg, `SELECT reserve_policy->>'shape' FROM products WHERE product_id = 'secrets'`, "count")
	assertScalar(t, ctx, pg, `SELECT count(*)::text FROM products WHERE active`, "2")
	assertScalar(t, ctx, pg, `SELECT count(*)::text FROM credit_buckets`, "4")
	assertScalar(t, ctx, pg, `SELECT count(*)::text FROM skus`, "6")
	assertScalar(t, ctx, pg, `SELECT count(*)::text FROM plans WHERE active AND is_default`, "2")
	assertScalar(t, ctx, pg, `SELECT count(*)::text FROM plan_sku_rates WHERE active`, "6")
	assertScalar(t, ctx, pg, `SELECT amount_units::text FROM entitlement_policies WHERE policy_id = 'free-tier:sandbox:compute:v1'`, "1000")
	assertScalar(t, ctx, pg, `SELECT amount_units::text FROM entitlement_policies WHERE policy_id = 'contract:sandbox-default:sandbox:execution_root_storage:v1'`, "3000")
	assertScalar(t, ctx, pg, `SELECT count(*)::text FROM plan_entitlements WHERE plan_id = 'sandbox-default'`, "1")
}

func assertScalar(t *testing.T, ctx context.Context, pg *pgxpool.Pool, query string, want string) {
	t.Helper()
	var got string
	if err := pg.QueryRow(ctx, query).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %q, want %q", query, got, want)
	}
}
