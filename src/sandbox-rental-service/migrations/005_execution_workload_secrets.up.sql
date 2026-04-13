-- Encrypted per-execution workload credentials. River jobs carry execution IDs;
-- provider JIT tokens and workflow secrets stay in service-owned PostgreSQL.

CREATE TABLE execution_workload_secrets (
    execution_id UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    secret_key   TEXT        NOT NULL,
    ciphertext   TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (execution_id, secret_key)
);
