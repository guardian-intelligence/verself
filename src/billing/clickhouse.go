package billing

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// ClickHouseMeteringWriter implements MeteringWriter using a ClickHouse connection.
type ClickHouseMeteringWriter struct {
	conn     driver.Conn
	database string
}

// NewClickHouseMeteringWriter creates a metering writer backed by ClickHouse.
func NewClickHouseMeteringWriter(conn driver.Conn, database string) *ClickHouseMeteringWriter {
	return &ClickHouseMeteringWriter{conn: conn, database: database}
}

// InsertMeteringRow inserts a single metering row into forge_metal.metering.
func (w *ClickHouseMeteringWriter) InsertMeteringRow(ctx context.Context, row MeteringRow) error {
	batch, err := w.conn.PrepareBatch(ctx, "INSERT INTO "+w.database+".metering")
	if err != nil {
		return fmt.Errorf("prepare metering batch: %w", err)
	}

	if err := batch.AppendStruct(&row); err != nil {
		return fmt.Errorf("append metering row: %w", err)
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("send metering batch: %w", err)
	}
	return nil
}

// ClickHouseMeteringQuerier implements MeteringQuerier using a ClickHouse connection.
type ClickHouseMeteringQuerier struct {
	conn     driver.Conn
	database string
}

// NewClickHouseMeteringQuerier creates a metering querier backed by ClickHouse.
func NewClickHouseMeteringQuerier(conn driver.Conn, database string) *ClickHouseMeteringQuerier {
	return &ClickHouseMeteringQuerier{conn: conn, database: database}
}

// SumDimension returns the sum of a single dimension from the dimensions Map column.
// Uses arrayElement(dimensions, key) with a parameterized key to avoid string
// interpolation into the query — the dimension name goes through driver binding.
func (q *ClickHouseMeteringQuerier) SumDimension(ctx context.Context, orgID OrgID, productID string, dimension string, since time.Time) (float64, error) {
	var result float64
	err := q.conn.QueryRow(ctx, fmt.Sprintf(`
		SELECT sum(arrayElement(dimensions, $4))
		FROM %s.metering
		WHERE org_id = $1
		  AND product_id = $2
		  AND started_at >= $3
	`, q.database),
		strconv.FormatUint(uint64(orgID), 10), productID, since, dimension,
	).Scan(&result)
	if err != nil {
		return 0, fmt.Errorf("sum dimension %q: %w", dimension, err)
	}
	return result, nil
}

// SumChargeUnits returns the sum of charge_units filtered by pricing phase.
func (q *ClickHouseMeteringQuerier) SumChargeUnits(ctx context.Context, orgID OrgID, productID string, pricingPhase PricingPhase, since time.Time) (uint64, error) {
	var result uint64
	err := q.conn.QueryRow(ctx, fmt.Sprintf(`
		SELECT sum(charge_units)
		FROM %s.metering
		WHERE org_id = $1
		  AND product_id = $2
		  AND pricing_phase = $3
		  AND started_at >= $4
	`, q.database),
		strconv.FormatUint(uint64(orgID), 10), productID, string(pricingPhase), since,
	).Scan(&result)
	if err != nil {
		return 0, fmt.Errorf("sum charge_units for phase %q: %w", pricingPhase, err)
	}
	return result, nil
}

// ClickHouseReconcileQuerier implements ClickHouseQuerier for reconciliation.
// Separate from ClickHouseMeteringQuerier because reconciliation queries have
// different shapes than hot-path queries.
type ClickHouseReconcileQuerier struct {
	conn     driver.Conn
	database string
}

// NewClickHouseReconcileQuerier creates a reconciliation querier backed by ClickHouse.
func NewClickHouseReconcileQuerier(conn driver.Conn, database string) *ClickHouseReconcileQuerier {
	return &ClickHouseReconcileQuerier{conn: conn, database: database}
}

// SumChargeUnitsByOrg returns the total charge_units across all products for an org.
func (q *ClickHouseReconcileQuerier) SumChargeUnitsByOrg(ctx context.Context, orgID string, since time.Time) (uint64, error) {
	var result uint64
	err := q.conn.QueryRow(ctx, fmt.Sprintf(`
		SELECT sum(charge_units)
		FROM %s.metering
		WHERE org_id = $1
		  AND started_at >= $2
	`, q.database), orgID, since).Scan(&result)
	if err != nil {
		return 0, fmt.Errorf("sum charge_units for org %s: %w", orgID, err)
	}
	return result, nil
}

// SumChargeUnitsByGrantSource returns charge_units grouped by grant source type.
func (q *ClickHouseReconcileQuerier) SumChargeUnitsByGrantSource(ctx context.Context, orgID string, productID string, since time.Time) (map[string]uint64, error) {
	rows, err := q.conn.Query(ctx, fmt.Sprintf(`
		SELECT
			sum(free_tier_units) AS free_tier,
			sum(subscription_units) AS subscription,
			sum(purchase_units) AS purchase,
			sum(promo_units) AS promo,
			sum(refund_units) AS refund
		FROM %s.metering
		WHERE org_id = $1
		  AND product_id = $2
		  AND started_at >= $3
	`, q.database), orgID, productID, since)
	if err != nil {
		return nil, fmt.Errorf("sum by grant source: %w", err)
	}
	defer rows.Close()

	result := make(map[string]uint64)
	if rows.Next() {
		var freeTier, subscription, purchase, promo, refund uint64
		if err := rows.Scan(&freeTier, &subscription, &purchase, &promo, &refund); err != nil {
			return nil, fmt.Errorf("scan grant source sums: %w", err)
		}
		result["free_tier"] = freeTier
		result["subscription"] = subscription
		result["purchase"] = purchase
		result["promo"] = promo
		result["refund"] = refund
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grant source rows: %w", err)
	}

	return result, nil
}

// CountLicensedChargeRows returns the count of metering rows with pricing_phase='licensed'.
func (q *ClickHouseReconcileQuerier) CountLicensedChargeRows(ctx context.Context, orgID string, productID string, since time.Time) (uint64, error) {
	var result uint64
	err := q.conn.QueryRow(ctx, fmt.Sprintf(`
		SELECT count()
		FROM %s.metering
		WHERE org_id = $1
		  AND product_id = $2
		  AND pricing_phase = 'licensed'
		  AND started_at >= $3
	`, q.database), orgID, productID, since).Scan(&result)
	if err != nil {
		return 0, fmt.Errorf("count licensed rows: %w", err)
	}
	return result, nil
}
