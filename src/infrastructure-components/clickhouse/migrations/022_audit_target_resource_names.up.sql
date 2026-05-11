ALTER TABLE verself.audit_events
    ADD COLUMN IF NOT EXISTS target_resource_name String AFTER target_id;
