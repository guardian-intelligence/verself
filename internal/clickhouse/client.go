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
	// TODO: implement columnar batch insert for performance
	return fmt.Errorf("not yet implemented")
}

// Close closes the ClickHouse connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
