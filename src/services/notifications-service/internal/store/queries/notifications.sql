-- name: Ping :one
SELECT 1::int AS one;

-- name: EnsureInboxState :exec
INSERT INTO notification_inbox_state (inbox_state_id, org_id, recipient_subject_id, next_sequence, read_up_to_sequence, created_at, updated_at)
VALUES (sqlc.arg(inbox_state_id), sqlc.arg(org_id), sqlc.arg(recipient_subject_id), 1, 0, sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (org_id, recipient_subject_id) DO NOTHING;

-- name: GetPreferences :one
SELECT version, enabled, web_enabled, email_enabled, push_enabled, sms_enabled, updated_at, updated_by
FROM notification_preferences
WHERE subject_id = $1;

-- name: GetSummaryState :one
SELECT state.next_sequence,
       state.read_up_to_sequence,
       GREATEST(state.next_sequence - 1, 0)::bigint AS latest_sequence,
       COUNT(n.notification_id)::bigint AS unread_count
FROM notification_inbox_state state
LEFT JOIN user_notifications n
  ON n.org_id = state.org_id
 AND n.recipient_subject_id = state.recipient_subject_id
 AND n.recipient_sequence > state.read_up_to_sequence
 AND n.read_at IS NULL
 AND n.dismissed_at IS NULL
WHERE state.org_id = $1 AND state.recipient_subject_id = $2
GROUP BY state.next_sequence, state.read_up_to_sequence;

-- name: ListNotifications :many
SELECT notification_id, org_id, recipient_subject_id, recipient_sequence, kind, priority, title, body,
       action_url, resource_kind, resource_id, created_at, expires_at, read_at, dismissed_at
FROM user_notifications
WHERE org_id = $1
  AND recipient_subject_id = $2
  AND dismissed_at IS NULL
  AND (sqlc.arg(cursor_enabled)::boolean = false OR recipient_sequence < sqlc.arg(cursor_sequence)::bigint)
ORDER BY recipient_sequence DESC
LIMIT sqlc.arg(limit_count)::int;

-- name: GetLatestNotification :one
SELECT notification_id, org_id, recipient_subject_id, recipient_sequence, kind, priority, title, body,
       action_url, resource_kind, resource_id, created_at, expires_at, read_at, dismissed_at
FROM user_notifications
WHERE org_id = $1 AND recipient_subject_id = $2
ORDER BY recipient_sequence DESC
LIMIT 1;

-- name: UpsertPreferences :execrows
INSERT INTO notification_preferences (
    subject_id, version, enabled, web_enabled, email_enabled, push_enabled,
    sms_enabled, updated_at, updated_by
) VALUES (
    sqlc.arg(subject_id), sqlc.arg(next_version), sqlc.arg(enabled), sqlc.arg(web_enabled),
    sqlc.arg(email_enabled), sqlc.arg(push_enabled), sqlc.arg(sms_enabled),
    sqlc.arg(updated_at), sqlc.arg(subject_id)
)
ON CONFLICT (subject_id) DO UPDATE
SET version = notification_preferences.version + 1,
    enabled = EXCLUDED.enabled,
    web_enabled = EXCLUDED.web_enabled,
    email_enabled = EXCLUDED.email_enabled,
    push_enabled = EXCLUDED.push_enabled,
    sms_enabled = EXCLUDED.sms_enabled,
    updated_at = EXCLUDED.updated_at,
    updated_by = EXCLUDED.updated_by
WHERE notification_preferences.version = sqlc.arg(expected_version);

-- name: InsertWorkflowRun :execrows
INSERT INTO notification_workflow_runs (
    workflow_run_id, workflow_key, org_id, triggered_by, idempotency_key,
    priority, title, body, action_url, resource_kind, resource_id,
    content_sha256, data, traceparent, recipient_count,
    web_notifications_accepted, email_deliveries_queued, suppressed_count, created_at
) VALUES (
    sqlc.arg(workflow_run_id), sqlc.arg(workflow_key), sqlc.arg(org_id),
    sqlc.arg(triggered_by), sqlc.arg(idempotency_key), sqlc.arg(priority),
    sqlc.arg(title), sqlc.arg(body), sqlc.arg(action_url), sqlc.arg(resource_kind),
    sqlc.arg(resource_id), sqlc.arg(content_sha256), sqlc.arg(data),
    sqlc.arg(traceparent), sqlc.arg(recipient_count), sqlc.arg(web_notifications_accepted),
    sqlc.arg(email_deliveries_queued), sqlc.arg(suppressed_count), sqlc.arg(created_at)
)
ON CONFLICT (workflow_key, org_id, idempotency_key) DO NOTHING;

-- name: GetWorkflowRun :one
SELECT workflow_run_id, workflow_key, org_id, triggered_by, idempotency_key,
       priority, title, body, action_url, resource_kind, resource_id,
       content_sha256, data, traceparent, recipient_count,
       web_notifications_accepted, email_deliveries_queued, suppressed_count, created_at
FROM notification_workflow_runs
WHERE workflow_key = $1 AND org_id = $2 AND idempotency_key = $3;

-- name: UpdateWorkflowRunCounts :exec
UPDATE notification_workflow_runs
SET web_notifications_accepted = sqlc.arg(web_notifications_accepted),
    email_deliveries_queued = sqlc.arg(email_deliveries_queued),
    suppressed_count = sqlc.arg(suppressed_count)
WHERE workflow_run_id = sqlc.arg(workflow_run_id);

-- name: AdvanceReadCursor :one
UPDATE notification_inbox_state
SET read_up_to_sequence = LEAST(sqlc.arg(read_up_to_sequence), next_sequence - 1),
    updated_at = sqlc.arg(updated_at)
WHERE org_id = sqlc.arg(org_id)
  AND recipient_subject_id = sqlc.arg(recipient_subject_id)
  AND read_up_to_sequence < LEAST(sqlc.arg(read_up_to_sequence), next_sequence - 1)
RETURNING read_up_to_sequence;

-- name: GetReadCursorProjectionSource :one
SELECT notification_id::text, event_source, event_id::text, kind, priority, content_sha256
FROM user_notifications
WHERE org_id = $1 AND recipient_subject_id = $2 AND recipient_sequence <= $3
ORDER BY recipient_sequence DESC
LIMIT 1;

-- name: MarkNotificationRead :one
UPDATE user_notifications AS n
SET read_at = sqlc.arg(read_at)
WHERE n.notification_id = sqlc.arg(notification_id)
  AND n.org_id = sqlc.arg(org_id)
  AND n.recipient_subject_id = sqlc.arg(recipient_subject_id)
  AND n.dismissed_at IS NULL
  AND n.read_at IS NULL
  AND n.recipient_sequence > (
      SELECT read_up_to_sequence
      FROM notification_inbox_state
      WHERE notification_inbox_state.org_id = sqlc.arg(org_id)
        AND notification_inbox_state.recipient_subject_id = sqlc.arg(recipient_subject_id)
  )
RETURNING n.org_id, n.recipient_subject_id, n.notification_id::text AS notification_id,
          n.recipient_sequence, n.event_source, n.event_id::text AS event_id,
          n.kind, n.priority, n.content_sha256;

-- name: DismissNotification :one
UPDATE user_notifications
SET dismissed_at = COALESCE(dismissed_at, sqlc.arg(dismissed_at))
WHERE notification_id = sqlc.arg(notification_id)
  AND org_id = sqlc.arg(org_id)
  AND recipient_subject_id = sqlc.arg(recipient_subject_id)
RETURNING org_id, recipient_subject_id, notification_id::text, recipient_sequence, event_source, event_id::text, kind, priority, content_sha256;

-- name: DismissAllNotifications :many
UPDATE user_notifications
SET dismissed_at = COALESCE(dismissed_at, sqlc.arg(dismissed_at))
WHERE org_id = sqlc.arg(org_id)
  AND recipient_subject_id = sqlc.arg(recipient_subject_id)
  AND dismissed_at IS NULL
RETURNING org_id, recipient_subject_id, notification_id::text, recipient_sequence, event_source, event_id::text, kind, priority, content_sha256;

-- name: InsertEvent :execrows
INSERT INTO notification_events (
    event_source, event_id, subject, org_id, actor_subject_id, recipient_subject_id,
    dedupe_key, kind, priority, title, body, action_url, resource_kind, resource_id,
    content_sha256, payload, traceparent, occurred_at, received_at
) VALUES (
    sqlc.arg(event_source), sqlc.arg(event_id), sqlc.arg(subject), sqlc.arg(org_id),
    sqlc.arg(actor_subject_id), sqlc.arg(recipient_subject_id), sqlc.arg(dedupe_key),
    sqlc.arg(kind), sqlc.arg(priority), sqlc.arg(title), sqlc.arg(body), sqlc.arg(action_url),
    sqlc.arg(resource_kind), sqlc.arg(resource_id), sqlc.arg(content_sha256), sqlc.arg(payload),
    sqlc.arg(traceparent), sqlc.arg(occurred_at), sqlc.arg(received_at)
)
ON CONFLICT DO NOTHING;

-- name: GetEventForUpdate :one
SELECT event_source, event_id, subject, org_id, actor_subject_id, recipient_subject_id,
       dedupe_key, kind, priority, title, body, action_url, resource_kind, resource_id,
       payload, traceparent, occurred_at, processed_at, suppressed_at
FROM notification_events
WHERE event_source = $1 AND event_id = $2
FOR UPDATE;

-- name: SuppressEvent :exec
UPDATE notification_events
SET suppressed_at = COALESCE(suppressed_at, sqlc.arg(suppressed_at)),
    suppression_reason = 'preferences_disabled'
WHERE event_source = sqlc.arg(event_source)
  AND event_id = sqlc.arg(event_id)
  AND processed_at IS NULL;

-- name: NextInboxSequence :one
UPDATE notification_inbox_state
SET next_sequence = next_sequence + 1,
    updated_at = sqlc.arg(updated_at)
WHERE org_id = sqlc.arg(org_id)
  AND recipient_subject_id = sqlc.arg(recipient_subject_id)
RETURNING (next_sequence - 1)::bigint AS recipient_sequence;

-- name: InsertUserNotification :exec
INSERT INTO user_notifications (
    notification_id, org_id, recipient_subject_id, recipient_sequence, dedupe_key,
    event_source, event_id, kind, priority, title, body, action_url, resource_kind,
    resource_id, content_sha256, created_at
) VALUES (
    sqlc.arg(notification_id), sqlc.arg(org_id), sqlc.arg(recipient_subject_id),
    sqlc.arg(recipient_sequence), sqlc.arg(dedupe_key), sqlc.arg(event_source),
    sqlc.arg(event_id), sqlc.arg(kind), sqlc.arg(priority), sqlc.arg(title), sqlc.arg(body),
    sqlc.arg(action_url), sqlc.arg(resource_kind), sqlc.arg(resource_id), sqlc.arg(content_sha256),
    sqlc.arg(created_at)
);

-- name: InsertDeliveryAttempt :execrows
INSERT INTO notification_delivery_attempts (
    delivery_attempt_id, workflow_run_id, org_id, recipient_subject_id,
    recipient_address, channel, workflow_key, idempotency_key, status,
    provider, priority, title, body, action_url, resource_kind, resource_id,
    content_sha256, traceparent, queued_at, next_attempt_at, updated_at
) VALUES (
    sqlc.arg(delivery_attempt_id), sqlc.arg(workflow_run_id), sqlc.arg(org_id),
    sqlc.arg(recipient_subject_id), sqlc.arg(recipient_address), 'email',
    sqlc.arg(workflow_key), sqlc.arg(idempotency_key), 'queued', 'email-service',
    sqlc.arg(priority), sqlc.arg(title), sqlc.arg(body), sqlc.arg(action_url),
    sqlc.arg(resource_kind), sqlc.arg(resource_id), sqlc.arg(content_sha256),
    sqlc.arg(traceparent), sqlc.arg(queued_at), sqlc.arg(next_attempt_at),
    sqlc.arg(updated_at)
)
ON CONFLICT (idempotency_key) DO NOTHING;

-- name: GetDeliveryAttemptForUpdate :one
SELECT delivery_attempt_id, workflow_run_id, org_id, recipient_subject_id,
       recipient_address, channel, workflow_key, idempotency_key, status,
       provider, provider_message_id, failure_reason, priority, title, body,
       action_url, resource_kind, resource_id, content_sha256, traceparent,
       queued_at, next_attempt_at, updated_at, sent_at, failed_at, attempt_count
FROM notification_delivery_attempts
WHERE delivery_attempt_id = $1
FOR UPDATE;

-- name: MarkDeliverySending :exec
UPDATE notification_delivery_attempts
SET status = 'sending',
    attempt_count = attempt_count + 1,
    updated_at = sqlc.arg(updated_at)
WHERE delivery_attempt_id = sqlc.arg(delivery_attempt_id)
  AND status IN ('queued', 'failed', 'sending');

-- name: MarkDeliverySent :exec
UPDATE notification_delivery_attempts
SET status = 'sent',
    provider_message_id = sqlc.arg(provider_message_id),
    failure_reason = '',
    sent_at = sqlc.arg(sent_at),
    updated_at = sqlc.arg(sent_at)
WHERE delivery_attempt_id = sqlc.arg(delivery_attempt_id);

-- name: MarkDeliveryFailed :exec
UPDATE notification_delivery_attempts
SET status = 'failed',
    failure_reason = sqlc.arg(failure_reason),
    failed_at = sqlc.arg(failed_at),
    updated_at = sqlc.arg(failed_at),
    next_attempt_at = sqlc.arg(next_attempt_at)
WHERE delivery_attempt_id = sqlc.arg(delivery_attempt_id);

-- name: MarkEventProcessed :exec
UPDATE notification_events
SET processed_at = COALESCE(processed_at, sqlc.arg(processed_at))
WHERE event_source = sqlc.arg(event_source)
  AND event_id = sqlc.arg(event_id);

-- name: ClaimProjectionQueue :many
SELECT ledger_event_id::text, event_type, org_id, recipient_subject_id,
       COALESCE(notification_id::text, '')::text AS notification_id_text, event_source, COALESCE(event_id::text, '')::text AS event_id_text,
       recipient_sequence, source_subject, kind, priority, content_sha256, status,
       reason, traceparent, occurred_at
FROM notification_projection_queue
WHERE projected_at IS NULL AND next_attempt_at <= now()
ORDER BY occurred_at, ledger_event_id
LIMIT sqlc.arg(limit_count)::int
FOR UPDATE SKIP LOCKED;

-- name: MarkProjectionProjected :exec
UPDATE notification_projection_queue
SET projected_at = COALESCE(projected_at, now()),
    attempts = attempts + 1,
    last_error = ''
WHERE ledger_event_id = $1;

-- name: TouchInboxState :execrows
UPDATE notification_inbox_state
SET updated_at = sqlc.arg(updated_at)
WHERE org_id = sqlc.arg(org_id)
  AND recipient_subject_id = sqlc.arg(recipient_subject_id);

-- name: NotificationExists :one
SELECT EXISTS (
    SELECT 1
    FROM user_notifications
    WHERE org_id = sqlc.arg(org_id)
      AND recipient_subject_id = sqlc.arg(recipient_subject_id)
      AND notification_id = sqlc.arg(notification_id)
) AS exists;

-- name: PruneInbox :many
DELETE FROM user_notifications
WHERE org_id = sqlc.arg(org_id)
  AND recipient_subject_id = sqlc.arg(recipient_subject_id)
  AND recipient_sequence <= sqlc.arg(cutoff_sequence)
RETURNING notification_id::text, recipient_sequence, event_source, event_id::text, kind, priority, content_sha256;

-- name: EnqueueProjection :exec
INSERT INTO notification_projection_queue (
    ledger_event_id, event_type, org_id, recipient_subject_id, notification_id,
    event_source, event_id, recipient_sequence, source_subject, kind, priority,
    content_sha256, status, reason, traceparent, occurred_at, next_attempt_at
) VALUES (
    sqlc.arg(ledger_event_id), sqlc.arg(event_type), sqlc.arg(org_id), sqlc.arg(recipient_subject_id),
    NULLIF(sqlc.arg(notification_id_text)::text, '')::uuid, sqlc.arg(event_source), NULLIF(sqlc.arg(event_id_text)::text, '')::uuid,
    sqlc.arg(recipient_sequence), sqlc.arg(source_subject), sqlc.arg(kind), sqlc.arg(priority),
    sqlc.arg(content_sha256), sqlc.arg(status), sqlc.arg(reason), sqlc.arg(traceparent),
    sqlc.arg(occurred_at), sqlc.arg(occurred_at)
)
ON CONFLICT (ledger_event_id) DO NOTHING;
