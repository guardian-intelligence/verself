-- Mail observability views: normalize inbound delivery attempts from Stalwart
-- traces and mailbox-service sync/forwarder lifecycle logs into one ClickHouse
-- surface that is easy to query during live debugging.

DROP VIEW IF EXISTS default.mail_events;

CREATE VIEW default.mail_events AS
SELECT
    Timestamp,
    toDateTime(Timestamp) AS TimestampTime,
    'log' AS SourceKind,
    ServiceName AS SourceService,
    multiIf(
        Body = 'mailbox-service: email changes applied', 'mailbox_sync_email_changes',
        Body = 'mailbox-service: forwarded email', 'operator_forwarded_email',
        Body = 'mailbox-service: operator forwarder skipped self-generated message', 'operator_forwarder_skip',
        Body = 'mailbox-service: sync worker bootstrap completed', 'mailbox_sync_bootstrap_completed',
        Body = 'mailbox-service: sync worker eventsource connected', 'mailbox_sync_eventsource_connected',
        'mailbox_service_log'
    ) AS EventType,
    multiIf(
        Body = 'mailbox-service: forwarded email', 'outbound',
        Body = 'mailbox-service: operator forwarder skipped self-generated message', 'outbound',
        'inbound'
    ) AS Direction,
    LogAttributes['mailbox_account'] AS MailboxAccount,
    LogAttributes['email_id'] AS EmailID,
    '' AS QueueID,
    '' AS QueueName,
    LogAttributes['resend_id'] AS ExternalID,
    '' AS Sender,
    LogAttributes['subject'] AS Subject,
    '' AS RecipientSummary,
    LogAttributes['state'] AS SyncState,
    toUInt32OrZero(LogAttributes['upserted_emails']) AS UpsertedEmails,
    toUInt32OrZero(LogAttributes['destroyed_emails']) AS DestroyedEmails,
    toUInt32OrZero(LogAttributes['upserted_threads']) AS UpsertedThreads,
    toUInt32OrZero(LogAttributes['emails']) AS BootstrapEmails,
    toUInt32OrZero(LogAttributes['mailboxes']) AS BootstrapMailboxes,
    toUInt32OrZero(LogAttributes['threads']) AS BootstrapThreads,
    toUInt64(0) AS MessageSizeBytes,
    toUInt16(0) AS RecipientCount,
    TraceId,
    SpanId,
    Body AS Message,
    LogAttributes AS RawAttributes
FROM default.otel_logs
WHERE ServiceName = 'mailbox-service'
  AND Body IN (
    'mailbox-service: email changes applied',
    'mailbox-service: forwarded email',
    'mailbox-service: operator forwarder skipped self-generated message',
    'mailbox-service: sync worker bootstrap completed',
    'mailbox-service: sync worker eventsource connected'
  )

UNION ALL

SELECT
    Timestamp,
    toDateTime(Timestamp) AS TimestampTime,
    'trace' AS SourceKind,
    ServiceName AS SourceService,
    'stalwart_delivery_attempt' AS EventType,
    'inbound' AS Direction,
    '' AS MailboxAccount,
    '' AS EmailID,
    SpanAttributes['queueId'] AS QueueID,
    SpanAttributes['queueName'] AS QueueName,
    '' AS ExternalID,
    SpanAttributes['from'] AS Sender,
    '' AS Subject,
    SpanAttributes['to'] AS RecipientSummary,
    '' AS SyncState,
    toUInt32(0) AS UpsertedEmails,
    toUInt32(0) AS DestroyedEmails,
    toUInt32(0) AS UpsertedThreads,
    toUInt32(0) AS BootstrapEmails,
    toUInt32(0) AS BootstrapMailboxes,
    toUInt32(0) AS BootstrapThreads,
    toUInt64OrZero(SpanAttributes['size']) AS MessageSizeBytes,
    toUInt16OrZero(SpanAttributes['total']) AS RecipientCount,
    TraceId,
    SpanId,
    SpanName AS Message,
    SpanAttributes AS RawAttributes
FROM default.otel_traces
WHERE ServiceName = 'stalwart'
  AND SpanName = 'delivery.attempt-start';

DROP VIEW IF EXISTS default.mail_metrics_latest;

CREATE VIEW default.mail_metrics_latest AS
SELECT
    ServiceName,
    multiIf(
        MetricName LIKE 'message-ingest.%', 'ingest',
        MetricName LIKE 'delivery.%', 'delivery',
        MetricName LIKE 'queue.%', 'queue',
        MetricName LIKE 'smtp.%', 'smtp',
        'other'
    ) AS MetricGroup,
    MetricName,
    argMax(Value, TimeUnix) AS CurrentValue,
    max(TimeUnix) AS SampledAt
FROM default.otel_metrics_sum
WHERE ServiceName = 'stalwart'
  AND (
    MetricName LIKE 'message-ingest.%'
    OR MetricName LIKE 'delivery.%'
    OR MetricName LIKE 'queue.%'
    OR MetricName LIKE 'smtp.%'
  )
GROUP BY ServiceName, MetricGroup, MetricName;
