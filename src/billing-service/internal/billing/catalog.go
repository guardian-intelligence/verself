package billing

import (
	"context"
	"database/sql"
	"fmt"
)

type SKUConfig struct {
	SKUID             string
	DisplayName       string
	BucketID          string
	BucketDisplayName string
	QuantityUnit      string
	UnitRate          uint64
}

type BucketConfig struct {
	BucketID    string
	DisplayName string
}

type skuRateContext struct {
	DisplayName       string `json:"display_name"`
	BucketID          string `json:"bucket_id"`
	BucketDisplayName string `json:"bucket_display_name"`
	QuantityUnit      string `json:"quantity_unit"`
	UnitRate          uint64 `json:"unit_rate"`
}

func (c *Client) loadPlanSKUConfig(ctx context.Context, planID string) (map[string]SKUConfig, map[string]BucketConfig, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT s.sku_id, s.display_name, s.bucket_id, b.display_name, s.quantity_unit, r.unit_rate
		FROM plan_sku_rates r
		JOIN skus s ON s.sku_id = r.sku_id
		JOIN credit_buckets b ON b.bucket_id = s.bucket_id
		WHERE r.plan_id = $1
		  AND r.active
		  AND s.active
		ORDER BY b.sort_order, s.sku_id
	`, planID)
	if err != nil {
		return nil, nil, fmt.Errorf("load plan sku rates: %w", err)
	}
	defer rows.Close()

	skus := map[string]SKUConfig{}
	buckets := map[string]BucketConfig{}
	for rows.Next() {
		var sku SKUConfig
		var unitRate int64
		if err := rows.Scan(&sku.SKUID, &sku.DisplayName, &sku.BucketID, &sku.BucketDisplayName, &sku.QuantityUnit, &unitRate); err != nil {
			return nil, nil, fmt.Errorf("scan plan sku rate: %w", err)
		}
		if sku.SKUID == "" || sku.BucketID == "" || sku.DisplayName == "" || sku.BucketDisplayName == "" || sku.QuantityUnit == "" {
			return nil, nil, fmt.Errorf("plan %s has incomplete sku catalog row", planID)
		}
		if unitRate < 0 {
			return nil, nil, fmt.Errorf("plan %s sku %s has negative SKU rate", planID, sku.SKUID)
		}
		sku.UnitRate = uint64(unitRate)
		skus[sku.SKUID] = sku
		buckets[sku.BucketID] = BucketConfig{BucketID: sku.BucketID, DisplayName: sku.BucketDisplayName}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate plan sku rates: %w", err)
	}
	if len(skus) == 0 {
		return nil, nil, sql.ErrNoRows
	}
	return skus, buckets, nil
}

func skuRatesFromSKUConfig(skus map[string]SKUConfig) map[string]uint64 {
	out := make(map[string]uint64, len(skus))
	for skuID, sku := range skus {
		out[skuID] = sku.UnitRate
	}
	return out
}

func skuBucketsFromSKUConfig(skus map[string]SKUConfig) map[string]string {
	out := make(map[string]string, len(skus))
	for skuID, sku := range skus {
		out[skuID] = sku.BucketID
	}
	return out
}

func skuRateContextFromConfig(skus map[string]SKUConfig) map[string]skuRateContext {
	out := make(map[string]skuRateContext, len(skus))
	for skuID, sku := range skus {
		out[skuID] = skuRateContext{
			DisplayName:       sku.DisplayName,
			BucketID:          sku.BucketID,
			BucketDisplayName: sku.BucketDisplayName,
			QuantityUnit:      sku.QuantityUnit,
			UnitRate:          sku.UnitRate,
		}
	}
	return out
}

func bucketDisplayNamesFromConfig(buckets map[string]BucketConfig) map[string]string {
	out := make(map[string]string, len(buckets))
	for bucketID, bucket := range buckets {
		out[bucketID] = bucket.DisplayName
	}
	return out
}
