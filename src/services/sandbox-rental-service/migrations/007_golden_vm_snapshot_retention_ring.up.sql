CREATE INDEX IF NOT EXISTS idx_golden_vm_snapshot_org_ring_retention
    ON golden_vm_snapshot (org_id, state, last_used_at DESC, created_at DESC, golden_vm_snapshot_id DESC)
    WHERE state IN ('candidate', 'current', 'retained');
