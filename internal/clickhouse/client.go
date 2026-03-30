package clickhouse

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/forge-metal/forge-metal/internal/config"
)

// Client wraps a ClickHouse connection for writing wide events.
type Client struct {
	conn driver.Conn
	cfg  config.ClickHouseConfig
}

// New creates a new ClickHouse client from config.
func New(cfg config.ClickHouseConfig) (*Client, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to clickhouse: %w", err)
	}
	return &Client{conn: conn, cfg: cfg}, nil
}

// InsertEvent writes a single wide event to the ci_events table.
func (c *Client) InsertEvent(ctx context.Context, event *CIEvent) error {
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO "+c.cfg.Database+".ci_events")
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}
	if err := batch.AppendStruct(event); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return batch.Send()
}

// Ping checks connectivity to the ClickHouse server.
func (c *Client) Ping(ctx context.Context) error {
	return c.conn.Ping(ctx)
}

// QueryRows executes a query and returns the result rows.
func (c *Client) QueryRows(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	return c.conn.Query(ctx, query, args...)
}

// Close closes the ClickHouse connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
