DROP INDEX IF EXISTS idx_cache_declaration_hash;

CREATE UNIQUE INDEX idx_cache_declaration_hash
    ON cache_declaration (
        repository_id,
        declaration_hash,
        source_kind,
        source_path,
        workflow_identity,
        job_identity,
        step_identity
    );
