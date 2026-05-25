package billing

import (
	"context"
	"strings"

	"github.com/verself/billing-service/internal/store"
)

const (
	SandboxProductID = "sandbox"

	durableStorageFreeQuotaBytes       = uint64(20 << 30)
	durableStorageProQuotaBytes        = uint64(200 << 30)
	durableStorageEnterpriseQuotaBytes = uint64(500 << 30)
)

type StorageEntitlement struct {
	OrgID                    OrgID
	ProductID                string
	PlanID                   string
	PlanTier                 string
	DurableStorageQuotaBytes uint64
}

func (c *Client) GetStorageEntitlement(ctx context.Context, orgID OrgID, productID string) (StorageEntitlement, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		productID = SandboxProductID
	}
	if _, err := c.ApplyDueBillingWork(ctx, orgID, productID); err != nil {
		return StorageEntitlement{}, err
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, productID)
	if err != nil {
		return StorageEntitlement{}, err
	}
	pricing, err := c.queries.LoadPricingContext(ctx, store.LoadPricingContextParams{
		OrgID:     orgIDText(orgID),
		ProductID: productID,
		Now:       timestamptz(now),
	})
	if err != nil {
		return StorageEntitlement{}, err
	}
	plan, err := c.loadPlan(ctx, pricing.PlanID)
	if err != nil {
		return StorageEntitlement{}, err
	}
	return StorageEntitlement{
		OrgID:                    orgID,
		ProductID:                productID,
		PlanID:                   plan.PlanID,
		PlanTier:                 firstNonEmptyStoragePlanValue(plan.Tier, plan.PlanID),
		DurableStorageQuotaBytes: durableStorageQuotaBytesForPlan(plan.PlanID, plan.Tier),
	}, nil
}

func durableStorageQuotaBytesForPlan(planID, tier string) uint64 {
	key := strings.ToLower(strings.TrimSpace(firstNonEmptyStoragePlanValue(tier, planID)))
	switch key {
	case "free", "sandbox-free", "starter":
		return durableStorageFreeQuotaBytes
	case "pro", "sandbox-pro", "sandbox-default", "default", "payg", "metered":
		return durableStorageProQuotaBytes
	case "enterprise", "sandbox-enterprise":
		return durableStorageEnterpriseQuotaBytes
	}
	if strings.Contains(key, "enterprise") {
		return durableStorageEnterpriseQuotaBytes
	}
	if strings.Contains(key, "pro") || strings.Contains(key, "default") || strings.Contains(key, "payg") || strings.Contains(key, "metered") {
		return durableStorageProQuotaBytes
	}
	return durableStorageFreeQuotaBytes
}

func firstNonEmptyStoragePlanValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
