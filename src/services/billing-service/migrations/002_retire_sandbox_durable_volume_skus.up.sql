UPDATE plan_sku_rates
SET active = false,
    active_until = COALESCE(active_until, now()),
    updated_at = now()
WHERE sku_id IN (
    'sandbox_durable_volume_live_storage_gib_ms',
    'sandbox_durable_volume_retained_snapshot_gib_ms'
)
  AND active = true;

UPDATE skus
SET active = false,
    updated_at = now()
WHERE sku_id IN (
    'sandbox_durable_volume_live_storage_gib_ms',
    'sandbox_durable_volume_retained_snapshot_gib_ms'
)
  AND active = true;
