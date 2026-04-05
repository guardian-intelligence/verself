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
func (q *ClickHouseMeteringQuerier) SumDimension(ctx context.Context, orgID OrgID, productID string, dimension string, since time.Time) (float64, error) {
	var result float64
	err := q.conn.QueryRow(ctx, fmt.Sprintf(`
		SELECT sum(dimensions[%s])
		FROM %s.metering
		WHERE org_id = $1
		  AND product_id = $2
		  AND started_at >= $3
	`, quoteCHString(dimension), q.database),
		strconv.FormatUint(uint64(orgID), 10), productID, since,
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

// quoteCHString returns a ClickHouse single-quoted string with basic escaping.
func quoteCHString(s string) string {
	// ClickHouse Map access uses bracket notation with single-quoted keys: dimensions['token']
	var buf []byte
	buf = append(buf, '\'')
	for _, c := range []byte(s) {
		if c == '\'' {
			buf = append(buf, '\\', '\'')
		} else if c == '\\' {
			buf = append(buf, '\\', '\\')
		} else {
			buf = append(buf, c)
		}
	}
	buf = append(buf, '\'')
	return string(buf)
}
