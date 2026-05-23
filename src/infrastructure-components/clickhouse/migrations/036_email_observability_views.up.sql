DROP VIEW IF EXISTS default.mail_events;
DROP VIEW IF EXISTS default.email_events;
DROP VIEW IF EXISTS default.mail_metrics_latest;
DROP VIEW IF EXISTS default.email_metrics_latest;

CREATE VIEW default.email_events AS
SELECT
    Timestamp,
    toDateTime(Timestamp) AS TimestampTime,
    'log' AS SourceKind,
    ServiceName AS SourceService,
    multiIf(
        Body = 'email-service: outbound accepted', 'outbound_accepted',
        Body = 'email-service: provider accepted', 'provider_accepted',
        Body = 'email-service: provider send failed', 'provider_send_failed',
        Body = 'email-service: send_as denied', 'send_as_denied',
        Body = 'email-service: email changes applied', 'email_sync_email_changes',
        Body = 'email-service: sync worker bootstrap completed', 'email_sync_bootstrap_completed',
        Body = 'email-service: sync worker eventsource connected', 'email_sync_eventsource_connected',
        'email_service_log'
    ) AS EventType,
    multiIf(
        Body IN (
            'email-service: outbound accepted',
            'email-service: provider accepted',
            'email-service: provider send failed',
            'email-service: send_as denied'
        ), 'outbound',
        'inbound'
    ) AS Direction,
    LogAttributes['org_id'] AS OrgID,
    LogAttributes['mailbox_account'] AS AccountID,
    LogAttributes['message_id'] AS MessageID,
    LogAttributes['email_id'] AS EmailID,
    '' AS QueueID,
    '' AS QueueName,
    LogAttributes['provider'] AS Provider,
    LogAttributes['provider_message_id'] AS ProviderMessageID,
    LogAttributes['from_address'] AS FromAddress,
    LogAttributes['to_address'] AS RecipientSummary,
    LogAttributes['subject'] AS Subject,
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
WHERE ServiceName = 'email-service'
  AND Body IN (
    'email-service: outbound accepted',
    'email-service: provider accepted',
    'email-service: provider send failed',
    'email-service: send_as denied',
    'email-service: email changes applied',
    'email-service: sync worker bootstrap completed',
    'email-service: sync worker eventsource connected'
  )

UNION ALL

SELECT
    Timestamp,
    toDateTime(Timestamp) AS TimestampTime,
    'trace' AS SourceKind,
    ServiceName AS SourceService,
    'stalwart_delivery_attempt' AS EventType,
    'inbound' AS Direction,
    '' AS OrgID,
    '' AS AccountID,
    '' AS MessageID,
    '' AS EmailID,
    SpanAttributes['queueId'] AS QueueID,
    SpanAttributes['queueName'] AS QueueName,
    'stalwart' AS Provider,
    '' AS ProviderMessageID,
    SpanAttributes['from'] AS FromAddress,
    SpanAttributes['to'] AS RecipientSummary,
    '' AS Subject,
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

CREATE VIEW default.email_metrics_latest AS
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
