package billing

import (
	"context"
	"strings"
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
	entitlement := StorageEntitlement{
		OrgID:                    orgID,
		ProductID:                productID,
		PlanID:                   "free",
		PlanTier:                 "free",
		DurableStorageQuotaBytes: durableStorageFreeQuotaBytes,
	}
	contracts, err := c.ListContracts(ctx, orgID)
	if err != nil {
		return StorageEntitlement{}, err
	}
	for _, contract := range contracts {
		if contract.ProductID != productID || contract.PlanID == "" {
			continue
		}
		plan, err := c.loadPlan(ctx, contract.PlanID)
		if err != nil {
			return StorageEntitlement{}, err
		}
		entitlement.PlanID = plan.PlanID
		entitlement.PlanTier = firstNonEmptyStoragePlanValue(plan.Tier, plan.PlanID)
		entitlement.DurableStorageQuotaBytes = durableStorageQuotaBytesForPlan(plan.PlanID, plan.Tier)
		return entitlement, nil
	}
	return entitlement, nil
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
