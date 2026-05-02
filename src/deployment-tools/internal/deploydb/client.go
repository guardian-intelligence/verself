package deploydb

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/verself/deployment-tools/internal/deploydb"

// Client is the deploy controller's typed ClickHouse connection. It
// owns the native ClickHouse driver plus the SSH local forward the
// driver dials through.
type Client struct {
	conn    chdriver.Conn
	forward io.Closer
	tracer  trace.Tracer
}

// Config is the explicit operator-side ClickHouse contract used by
// runtime bootstrap. TLS endpoint details are read from the rendered
// clickhouse-client XML file so the deploy controller and substrate
// role share one source of truth.
type Config struct {
	Database           string
	Username           string
	OperatorConfigPath string
}

func openNative(ctx context.Context, forwardAddr string, tlsConfig *tls.Config, cfg Config) (chdriver.Conn, error) {
	return clickhouse.Open(&clickhouse.Options{
		Addr: []string{forwardAddr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
		},
		TLS:             tlsConfig,
		DialTimeout:     time.Second,
		ReadTimeout:     5 * time.Second,
		MaxOpenConns:    2,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Hour,
	})
}

// Close closes the native driver and its role-specific SSH forward.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	var closeErr error
	if c.conn != nil {
		closeErr = errors.Join(closeErr, c.conn.Close())
	}
	if c.forward != nil {
		closeErr = errors.Join(closeErr, c.forward.Close())
	}
	return closeErr
}

func insertStructs[T any](ctx context.Context, c *Client, table string, rows []T) error {
	if c == nil {
		return errors.New("deploydb: client is nil")
	}
	if len(rows) == 0 {
		return nil
	}
	ctx, span := c.tracer.Start(ctx, "verself_deploy.clickhouse.batch_insert",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("db.sql.table", table),
			attribute.Int("db.row_count", len(rows)),
			attribute.String("db.protocol", "native"),
		),
	)
	defer span.End()

	batch, err := c.conn.PrepareBatch(ctx, fmt.Sprintf("INSERT INTO %s", table))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("deploydb: prepare insert %s: %w", table, err)
	}
	for i := range rows {
		if err := batch.AppendStruct(&rows[i]); err != nil {
			_ = batch.Abort()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("deploydb: append row to %s: %w", table, err)
		}
	}
	if err := batch.Send(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("deploydb: send insert %s: %w", table, err)
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}
