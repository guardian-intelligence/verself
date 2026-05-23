DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'notification_preferences'
          AND column_name = 'org_id'
    ) THEN
        DELETE FROM notification_preferences p
        USING (
            SELECT ctid,
                   row_number() OVER (PARTITION BY subject_id ORDER BY updated_at DESC, org_id) AS row_number
            FROM notification_preferences
        ) ranked
        WHERE p.ctid = ranked.ctid
          AND ranked.row_number > 1;

        ALTER TABLE notification_preferences DROP CONSTRAINT IF EXISTS notification_preferences_pkey;
        ALTER TABLE notification_preferences DROP COLUMN IF EXISTS org_id;
        ALTER TABLE notification_preferences ADD PRIMARY KEY (subject_id);
    END IF;
END $$;

ALTER TABLE notification_preferences ADD COLUMN IF NOT EXISTS web_enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE notification_preferences ADD COLUMN IF NOT EXISTS email_enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE notification_preferences ADD COLUMN IF NOT EXISTS push_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE notification_preferences ADD COLUMN IF NOT EXISTS sms_enabled BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS notification_workflow_runs (
    workflow_run_id UUID PRIMARY KEY,
    workflow_key TEXT NOT NULL,
    org_id TEXT NOT NULL,
    triggered_by TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    priority TEXT NOT NULL CHECK (priority IN ('low', 'normal', 'high')),
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    action_url TEXT NOT NULL DEFAULT '',
    resource_kind TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    content_sha256 TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    traceparent TEXT NOT NULL DEFAULT '',
    recipient_count INTEGER NOT NULL DEFAULT 0,
    web_notifications_accepted INTEGER NOT NULL DEFAULT 0,
    email_deliveries_queued INTEGER NOT NULL DEFAULT 0,
    suppressed_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (workflow_key, org_id, idempotency_key),
    CHECK (length(btrim(workflow_key)) > 0),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(triggered_by)) > 0),
    CHECK (length(btrim(idempotency_key)) > 0),
    CHECK (length(btrim(title)) > 0),
    CHECK (length(btrim(body)) > 0),
    CHECK (length(content_sha256) = 64),
    CHECK (recipient_count >= 0),
    CHECK (web_notifications_accepted >= 0),
    CHECK (email_deliveries_queued >= 0),
    CHECK (suppressed_count >= 0)
);

CREATE INDEX IF NOT EXISTS notification_workflow_runs_org_idx ON notification_workflow_runs (org_id, created_at DESC);

CREATE TABLE IF NOT EXISTS notification_delivery_attempts (
    delivery_attempt_id UUID PRIMARY KEY,
    workflow_run_id UUID NOT NULL REFERENCES notification_workflow_runs (workflow_run_id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    recipient_subject_id TEXT NOT NULL DEFAULT '',
    recipient_address TEXT NOT NULL,
    channel TEXT NOT NULL CHECK (channel IN ('email')),
    workflow_key TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'sending', 'sent', 'failed', 'suppressed')),
    provider TEXT NOT NULL DEFAULT 'email-service',
    provider_message_id TEXT NOT NULL DEFAULT '',
    failure_reason TEXT NOT NULL DEFAULT '',
    priority TEXT NOT NULL CHECK (priority IN ('low', 'normal', 'high')),
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    action_url TEXT NOT NULL DEFAULT '',
    resource_kind TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    content_sha256 TEXT NOT NULL,
    traceparent TEXT NOT NULL DEFAULT '',
    queued_at TIMESTAMPTZ NOT NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    sent_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    UNIQUE (idempotency_key),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(recipient_address)) > 0),
    CHECK (length(btrim(workflow_key)) > 0),
    CHECK (length(btrim(idempotency_key)) > 0),
    CHECK (length(btrim(title)) > 0),
    CHECK (length(btrim(body)) > 0),
    CHECK (length(content_sha256) = 64),
    CHECK (attempt_count >= 0)
);

CREATE INDEX IF NOT EXISTS notification_delivery_attempts_pending_idx ON notification_delivery_attempts (next_attempt_at, queued_at)
    WHERE status IN ('queued', 'failed');
CREATE INDEX IF NOT EXISTS notification_delivery_attempts_workflow_idx ON notification_delivery_attempts (workflow_run_id);
