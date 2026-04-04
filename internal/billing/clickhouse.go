package billing

import (
	"context"
	"fmt"
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

	if err := batch.Append(
		row.OrgID,
		row.ActorID,
		row.ProductID,
		row.SourceType,
		row.SourceRef,
		row.WindowSeq,
		row.StartedAt,
		row.EndedAt,
		row.BilledSeconds,
		row.PricingPhase,
		row.Dimensions,
		row.ChargeUnits,
		row.FreeTierUnits,
		row.SubscriptionUnits,
		row.PurchaseUnits,
		row.PromoUnits,
		row.RefundUnits,
		row.ExitReason,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("append metering row: %w", err)
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("send metering batch: %w", err)
	}
	return nil
}
