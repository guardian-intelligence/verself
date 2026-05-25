ALTER TABLE github_webhook_deliveries
    ADD COLUMN IF NOT EXISTS primary_problem_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_status INTEGER NOT NULL DEFAULT 0 CHECK (primary_problem_status BETWEEN 0 AND 599),
    ADD COLUMN IF NOT EXISTS primary_problem_title TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_detail TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS primary_problem_docs_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS problem_count INTEGER NOT NULL DEFAULT 0 CHECK (problem_count >= 0);

CREATE TABLE IF NOT EXISTS github_webhook_delivery_problems (
    delivery_id              TEXT        NOT NULL REFERENCES github_webhook_deliveries(delivery_id) ON DELETE CASCADE,
    problem_seq              INTEGER     NOT NULL CHECK (problem_seq > 0),
    phase                    TEXT        NOT NULL CHECK (phase <> ''),
    problem_type             TEXT        NOT NULL CHECK (problem_type <> ''),
    problem_code             TEXT        NOT NULL CHECK (problem_code <> ''),
    title                    TEXT        NOT NULL CHECK (title <> ''),
    detail                   TEXT        NOT NULL DEFAULT '',
    docs_url                 TEXT        NOT NULL DEFAULT '',
    status                   INTEGER     NOT NULL DEFAULT 0 CHECK (status BETWEEN 0 AND 599),
    retryable                BOOLEAN     NOT NULL DEFAULT false,
    pointer                  TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (delivery_id, problem_seq)
);

CREATE INDEX IF NOT EXISTS idx_github_webhook_delivery_problems_delivery
    ON github_webhook_delivery_problems (delivery_id, observed_at);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'github_webhook_deliveries'
          AND column_name = 'failure_reason'
    ) THEN
        INSERT INTO github_webhook_delivery_problems (
            delivery_id,
            problem_seq,
            phase,
            problem_type,
            problem_code,
            title,
            detail,
            docs_url,
            status,
            retryable,
            pointer,
            observed_at
        )
        SELECT
            delivery_id,
            1,
            'legacy',
            'urn:verself:problem:provider_webhook:processing_failed',
            'provider_webhook.processing_failed',
            'Webhook delivery processing failed',
            failure_reason,
            'https://verself.sh/docs/reference/github-integration/errors#provider-webhook-processing-failed',
            0,
            state = 'retryable',
            '',
            updated_at
        FROM github_webhook_deliveries
        WHERE failure_reason <> ''
        ON CONFLICT (delivery_id, problem_seq) DO NOTHING;

        UPDATE github_webhook_deliveries
        SET
            primary_problem_type = 'urn:verself:problem:provider_webhook:processing_failed',
            primary_problem_code = 'provider_webhook.processing_failed',
            primary_problem_status = 0,
            primary_problem_title = 'Webhook delivery processing failed',
            primary_problem_detail = failure_reason,
            primary_problem_docs_url = 'https://verself.sh/docs/reference/github-integration/errors#provider-webhook-processing-failed',
            problem_count = 1
        WHERE failure_reason <> ''
          AND problem_count = 0;

        ALTER TABLE github_webhook_deliveries DROP COLUMN failure_reason;
    END IF;
END $$;

UPDATE github_webhook_deliveries
SET state = 'accepted'
WHERE state = 'verified';

DROP INDEX IF EXISTS idx_github_webhook_deliveries_pending;

CREATE INDEX idx_github_webhook_deliveries_pending
    ON github_webhook_deliveries (next_attempt_at NULLS FIRST, received_at)
    WHERE state IN ('accepted', 'retryable');

ALTER TABLE IF EXISTS public.github_webhook_delivery_problems OWNER TO github_integration_service;
