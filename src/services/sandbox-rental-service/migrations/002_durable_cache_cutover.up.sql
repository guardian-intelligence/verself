ALTER TABLE cache_declaration
    DROP CONSTRAINT IF EXISTS cache_declaration_source_kind_check;

ALTER TABLE cache_declaration
    ADD CONSTRAINT cache_declaration_source_kind_check
    CHECK (source_kind IN ('manifest', 'none'));

DROP TABLE IF EXISTS host_durable_journal;

CREATE INDEX IF NOT EXISTS idx_durable_generation_retention
    ON durable_generation (state, expires_at, committed_at);
