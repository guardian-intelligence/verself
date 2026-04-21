-- Historical bridge to PostgreSQL billing_windows.
-- The bridge is removed in favor of ClickHouse-native billing projections.

DROP TABLE IF EXISTS forge_metal.billing_windows_pg;
