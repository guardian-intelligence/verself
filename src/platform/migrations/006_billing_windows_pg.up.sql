-- Live bridge to PostgreSQL billing_windows table.
-- Uses the PostgreSQL table engine for operational diagnostics; no data is copied.
-- Credentials come from the billing_pg named collection in clickhouse-config.xml.

DROP TABLE IF EXISTS forge_metal.billing_events_pg;

CREATE TABLE IF NOT EXISTS forge_metal.billing_windows_pg (
    window_id               String,
    org_id                  String,
    actor_id                String,
    product_id              String,
    plan_id                 String,
    source_type             String,
    source_ref              String,
    window_seq              Int64,
    state                   String,
    reservation_shape       String,
    reserved_quantity       Int64,
    actual_quantity         Int64,
    billable_quantity       Int64,
    writeoff_quantity       Int64,
    reserved_charge_units   Int64,
    billed_charge_units     Int64,
    writeoff_charge_units   Int64,
    pricing_phase           String,
    allocation              String,
    rate_context            String,
    usage_summary           String,
    funding_legs            String,
    window_start            DateTime64(6, 'UTC'),
    expires_at              DateTime64(6, 'UTC'),
    renew_by                Nullable(DateTime64(6, 'UTC')),
    settled_at              Nullable(DateTime64(6, 'UTC')),
    metering_projected_at   Nullable(DateTime64(6, 'UTC')),
    last_projection_error   String,
    created_at              DateTime64(6, 'UTC')
)
ENGINE = PostgreSQL(billing_pg, table = 'billing_windows');
