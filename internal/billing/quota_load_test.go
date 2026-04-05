package billing

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestLoadQuotaPolicySubscriptionEmptyPolicyIsDistinctFromMissingPolicy(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_800_000_000_000_000_101)
	productID, planID := uniqueCatalogIDs("quota-load-empty-subscription")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	policy, err := env.client.loadQuotaPolicy(ctx, orgID, productID)
	if err != nil {
		t.Fatalf("load quota policy: %v", err)
	}
	if policy.source != quotaPolicySourceSubscription {
		t.Fatalf("expected subscription policy source, got %q", policy.source)
	}
	if policy.periodStart == nil {
		t.Fatal("expected subscription quota policy to include periodStart")
	}
	if len(policy.limits) != 0 {
		t.Fatalf("expected zero limits for empty subscription policy, got %+v", policy.limits)
	}
}

func TestLoadQuotaPolicyUsesDefaultPlanWithoutSubscription(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_800_000_000_000_000_102)
	productID, planID := uniqueCatalogIDs("quota-load-default")

	insertQuotaPlanWithoutSubscription(t, env.client, env.pg, orgID, productID, planID, true)
	setQuotasOnPlan(t, env.pg, planID, quotaPolicy{Limits: []quotaLimit{
		{Dimension: "token", Window: "hour", Limit: 42},
	}})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	policy, err := env.client.loadQuotaPolicy(ctx, orgID, productID)
	if err != nil {
		t.Fatalf("load quota policy: %v", err)
	}
	if policy.source != quotaPolicySourceDefaultPlan {
		t.Fatalf("expected default plan policy source, got %q", policy.source)
	}
	if policy.periodStart != nil {
		t.Fatalf("expected default plan quota policy to omit periodStart, got %v", *policy.periodStart)
	}
	if len(policy.limits) != 1 {
		t.Fatalf("expected 1 default-plan limit, got %+v", policy.limits)
	}
}

func TestLoadQuotaPolicyReturnsNoneWhenNoSubscriptionOrDefaultPlanExists(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(8_800_000_000_000_000_103)
	productID, _ := uniqueCatalogIDs("quota-load-missing")

	ensureMeteredProduct(t, env.pg, productID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := env.client.EnsureOrg(ctx, orgID, "quota-load-missing"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	policy, err := env.client.loadQuotaPolicy(ctx, orgID, productID)
	if err != nil {
		t.Fatalf("load quota policy: %v", err)
	}
	if policy.source != quotaPolicySourceNone {
		t.Fatalf("expected no quota policy source, got %q", policy.source)
	}
	if policy.periodStart != nil {
		t.Fatalf("expected no periodStart for missing policy, got %v", *policy.periodStart)
	}
	if len(policy.limits) != 0 {
		t.Fatalf("expected zero limits for missing policy, got %+v", policy.limits)
	}
}

func TestCheckQuotasMonthWindowOnDefaultPlanWithoutSubscriptionFails(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnvWithMetering(t, noopMeteringWriter{}, &stubMeteringQuerier{})

	orgID := OrgID(8_800_000_000_000_000_104)
	productID, planID := uniqueCatalogIDs("quota-default-month")

	insertQuotaPlanWithoutSubscription(t, env.client, env.pg, orgID, productID, planID, true)
	setQuotasOnPlan(t, env.pg, planID, quotaPolicy{Limits: []quotaLimit{
		{Dimension: "token", Window: "month", Limit: 100},
	}})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := env.client.CheckQuotas(ctx, orgID, productID, nil)
	if err == nil {
		t.Fatal("expected month-window default plan quota check to fail, got nil")
	}
	if !strings.Contains(err.Error(), "month window requires a subscription") {
		t.Fatalf("expected month-window subscription anchor error, got %v", err)
	}
}

func insertQuotaPlanWithoutSubscription(t fatalHelper, client *Client, db *sql.DB, orgID OrgID, productID, planID string, isDefault bool) {
	t.Helper()

	ensureMeteredProduct(t, db, productID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.EnsureOrg(ctx, orgID, "quota-plan-without-subscription"); err != nil {
		t.Fatalf("ensure org %d: %v", orgID, err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO plans (
			plan_id,
			product_id,
			display_name,
			included_credits,
			unit_rates,
			overage_unit_rates,
			is_default,
			active
		)
		VALUES ($1, $2, $3, 0, '{}'::jsonb, '{}'::jsonb, $4, true)
		ON CONFLICT (plan_id) DO UPDATE
		SET is_default = EXCLUDED.is_default,
		    active = EXCLUDED.active
	`, planID, productID, "Quota Test Plan", isDefault); err != nil {
		t.Fatalf("insert plan %s: %v", planID, err)
	}
}
