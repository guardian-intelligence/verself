-- Lift the per-runner-class Checkpoint slot count out of the vm-orchestrator
-- constant. Each row in runner_classes carries the number of reserved
-- Checkpoint drive slots a guest VM gets at boot. vm-orchestrator still
-- enforces an upper bound (currently 16) so a misconfigured row cannot
-- exhaust host disk.
ALTER TABLE runner_classes
    ADD COLUMN checkpoint_slot_count INTEGER NOT NULL DEFAULT 5
        CHECK (checkpoint_slot_count BETWEEN 0 AND 16);
