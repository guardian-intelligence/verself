-- Live bridge to PostgreSQL billing_events table.
-- Uses the PostgreSQL table engine for real-time reads; no data is copied.
-- Credentials come from the billing_pg named collection in clickhouse-config.xml.

CREATE TABLE IF NOT EXISTS forge_metal.billing_events_pg (
    event_id        Int64,
    org_id          String,
    event_type      String,
    subscription_id Nullable(Int64),
    grant_id        Nullable(String),
    task_id         Nullable(Int64),
    payload         String,
    stripe_event_id Nullable(String),
    created_at      DateTime64(6, 'UTC')
)
ENGINE = PostgreSQL(billing_pg, table = 'billing_events');
