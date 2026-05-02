package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v85"
	"go.opentelemetry.io/otel"

	"github.com/verself/billing-service/internal/billing/ledger"
	"github.com/verself/billing-service/internal/store"
)

var tracer = otel.Tracer("billing-service/internal/billing")

type Client struct {
	pg      *pgxpool.Pool
	stripe  *stripe.Client
	ch      clickhouse.Conn
	cfg     Config
	logger  *slog.Logger
	queries *store.Queries
	runtime *Runtime
	ledger  *ledger.Client
}

func NewClient(pg *pgxpool.Pool, stripeClient *stripe.Client, ch clickhouse.Conn, cfg Config, logger *slog.Logger, ledgerClient *ledger.Client) (*Client, error) {
	if pg == nil {
		return nil, fmt.Errorf("%w: postgres pool is required", ErrInvalidConfig)
	}
	if cfg.PendingTimeout == 0 {
		cfg.PendingTimeout = time.Hour
	}
	if cfg.EventDeliveryProjectEvery == 0 {
		cfg.EventDeliveryProjectEvery = time.Second
	}
	if cfg.EntitlementReconcileEvery == 0 {
		cfg.EntitlementReconcileEvery = time.Hour
	}
	if cfg.LedgerDispatchEvery == 0 {
		cfg.LedgerDispatchEvery = time.Second
	}
	if cfg.LedgerReconcileEvery == 0 {
		cfg.LedgerReconcileEvery = time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{pg: pg, stripe: stripeClient, ch: ch, cfg: cfg, logger: logger, queries: store.New(pg), ledger: ledgerClient}, nil
}

func (c *Client) Pool() *pgxpool.Pool { return c.pg }

func (c *Client) SetRuntime(runtime *Runtime) {
	c.runtime = runtime
}

func (c *Client) WithTx(ctx context.Context, name string, fn func(context.Context, pgx.Tx, *store.Queries) error) error {
	ctx, span := tracer.Start(ctx, name)
	defer span.End()
	tx, err := c.pg.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := c.queries.WithTx(tx)
	if err := fn(ctx, tx, q); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}

func (c *Client) BusinessNow(ctx context.Context, q *store.Queries, orgID OrgID, productID string) (time.Time, error) {
	org := orgIDText(orgID)
	value, err := q.BusinessNow(ctx, store.BusinessNowParams{
		POrgProductScope: org + ":" + productID,
		POrgScope:        org,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("read business clock: %w", err)
	}
	if !value.Valid {
		return time.Time{}, fmt.Errorf("business clock returned null")
	}
	return value.Time.UTC(), nil
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func timestamptzValue(value any) pgtype.Timestamptz {
	t, ok := value.(time.Time)
	if !ok {
		return pgtype.Timestamptz{}
	}
	return timestamptz(t)
}

func pgTextValue(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func nullableTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC()
}
