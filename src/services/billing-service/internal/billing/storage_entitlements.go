package billing

import (
	"context"
	"strings"

	"github.com/verself/billing-service/internal/store"
)

const (
	SandboxProductID = "sandbox"

	durableStorageFreeQuotaBytes       = uint64(10 << 30)
	durableStorageDefaultQuotaBytes    = uint64(256 << 30)
	durableStorageTeamQuotaBytes       = uint64(512 << 30)
	durableStorageBusinessQuotaBytes   = uint64(1 << 40)
	durableStorageEnterpriseQuotaBytes = uint64(4 << 40)
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
	case "sandbox-default", "default", "payg", "metered":
		return durableStorageDefaultQuotaBytes
	case "team":
		return durableStorageTeamQuotaBytes
	case "pro", "business":
		return durableStorageBusinessQuotaBytes
	case "enterprise":
		return durableStorageEnterpriseQuotaBytes
	}
	if strings.Contains(key, "enterprise") {
		return durableStorageEnterpriseQuotaBytes
	}
	if strings.Contains(key, "business") || strings.Contains(key, "pro") {
		return durableStorageBusinessQuotaBytes
	}
	if strings.Contains(key, "team") {
		return durableStorageTeamQuotaBytes
	}
	if strings.Contains(key, "default") || strings.Contains(key, "payg") {
		return durableStorageDefaultQuotaBytes
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
