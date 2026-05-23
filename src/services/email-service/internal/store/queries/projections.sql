-- name: Ping :one
SELECT 1::int AS one;

-- name: ResolveBinding :one
SELECT account_id
FROM (
    SELECT identity_id AS account_id
    FROM email_identity_memberships
    WHERE subject_id = sqlc.arg(subject)
    ORDER BY
        CASE role
            WHEN 'owner' THEN 0
            WHEN 'writer' THEN 1
            ELSE 2
        END,
        identity_id
    LIMIT 1
) resolved;

-- name: LookupAccount :one
SELECT account_id, jmap_account_id, email_address, display_name, principal_type, synced_at
FROM (
    SELECT
        identity_id AS account_id,
        jmap_account_id,
        primary_address AS email_address,
        display_name,
        stalwart_principal_type AS principal_type,
        synced_at
    FROM email_identities
    WHERE identity_id = sqlc.arg(account_id)
) account_projection
WHERE account_id = sqlc.arg(account_id);

-- name: ListAccounts :many
SELECT account_id, jmap_account_id, email_address, display_name, principal_type, synced_at
FROM (
    SELECT
        identity_id AS account_id,
        jmap_account_id,
        primary_address AS email_address,
        display_name,
        stalwart_principal_type AS principal_type,
        synced_at
    FROM email_identities
) account_projection
ORDER BY account_id;

-- name: ListSyncStates :many
SELECT entity_type, jmap_state
FROM sync_state
WHERE account_id = sqlc.arg(account_id);

-- name: GetMailboxIDByRole :one
SELECT id
FROM mailboxes
WHERE account_id = sqlc.arg(account_id) AND role = sqlc.arg(role);

-- name: ListMailboxes :many
SELECT account_id, id, name, parent_id, role, sort_order, total_emails, unread_emails, total_threads, unread_threads, synced_at
FROM mailboxes
WHERE account_id = sqlc.arg(account_id)
ORDER BY sort_order, name, id;

-- name: GetEmail :one
SELECT account_id, id, blob_id, thread_id, subject, from_name, from_email, to_list, cc_list, reply_to_list,
       preview, keywords, has_attachment, size, received_at, sent_at, is_seen, is_flagged, is_answered,
       is_draft, synced_at
FROM emails
WHERE account_id = sqlc.arg(account_id) AND id = sqlc.arg(email_id);

-- name: ListEmailsByAccount :many
SELECT account_id, id, blob_id, thread_id, subject, from_name, from_email, to_list, cc_list, reply_to_list,
       preview, keywords, has_attachment, size, received_at, sent_at, is_seen, is_flagged, is_answered,
       is_draft, synced_at
FROM emails
WHERE account_id = sqlc.arg(account_id)
ORDER BY received_at DESC, id DESC
LIMIT sqlc.arg(limit_count);

-- name: ListEmailsByMailbox :many
SELECT e.account_id, e.id, e.blob_id, e.thread_id, e.subject, e.from_name, e.from_email, e.to_list,
       e.cc_list, e.reply_to_list, e.preview, e.keywords, e.has_attachment, e.size, e.received_at,
       e.sent_at, e.is_seen, e.is_flagged, e.is_answered, e.is_draft, e.synced_at
FROM emails e
INNER JOIN email_mailboxes em
        ON em.account_id = e.account_id
       AND em.email_id = e.id
WHERE e.account_id = sqlc.arg(account_id) AND em.mailbox_id = sqlc.arg(mailbox_id)
ORDER BY e.received_at DESC, e.id DESC
LIMIT sqlc.arg(limit_count);

-- name: GetEmailSnapshot :one
SELECT account_id, id, thread_id, keywords
FROM emails
WHERE account_id = sqlc.arg(account_id) AND id = sqlc.arg(email_id);

-- name: ListThreadIDsForEmails :many
SELECT DISTINCT thread_id
FROM emails
WHERE account_id = sqlc.arg(account_id) AND id = ANY(sqlc.arg(email_ids)::text[]);

-- name: GetEmailBody :one
SELECT account_id, email_id, text_body, html_body, fetched_at
FROM email_bodies
WHERE account_id = sqlc.arg(account_id) AND email_id = sqlc.arg(email_id);

-- name: ListEmailMailboxIDs :many
SELECT mailbox_id
FROM email_mailboxes
WHERE account_id = sqlc.arg(account_id) AND email_id = sqlc.arg(email_id)
ORDER BY mailbox_id;

-- name: ListMailboxIDsForEmails :many
SELECT email_id, mailbox_id
FROM email_mailboxes
WHERE account_id = sqlc.arg(account_id) AND email_id = ANY(sqlc.arg(email_ids)::text[])
ORDER BY email_id, mailbox_id;

-- name: PatchEmailKeywords :execrows
UPDATE emails
SET keywords = sqlc.arg(keywords)::jsonb,
    is_seen = sqlc.arg(is_seen),
    is_flagged = sqlc.arg(is_flagged),
    is_answered = sqlc.arg(is_answered),
    is_draft = sqlc.arg(is_draft),
    synced_at = sqlc.arg(synced_at)
WHERE account_id = sqlc.arg(account_id) AND id = sqlc.arg(email_id);

-- name: TouchEmailSyncedAt :execrows
UPDATE emails
SET synced_at = sqlc.arg(synced_at)
WHERE account_id = sqlc.arg(account_id) AND id = sqlc.arg(email_id);

-- name: DeleteEmailMailboxesForAccount :exec
DELETE FROM email_mailboxes
WHERE account_id = sqlc.arg(account_id);

-- name: DeleteEmailBodiesForAccount :exec
DELETE FROM email_bodies
WHERE account_id = sqlc.arg(account_id);

-- name: DeleteEmailsForAccount :exec
DELETE FROM emails
WHERE account_id = sqlc.arg(account_id);

-- name: DeleteThreadsForAccount :exec
DELETE FROM threads
WHERE account_id = sqlc.arg(account_id);

-- name: DeleteMailboxesForAccount :exec
DELETE FROM mailboxes
WHERE account_id = sqlc.arg(account_id);

-- name: DeleteEmailMailboxesForEmails :exec
DELETE FROM email_mailboxes
WHERE account_id = sqlc.arg(account_id) AND email_id = ANY(sqlc.arg(email_ids)::text[]);

-- name: DeleteEmailBodiesForEmails :exec
DELETE FROM email_bodies
WHERE account_id = sqlc.arg(account_id) AND email_id = ANY(sqlc.arg(email_ids)::text[]);

-- name: DeleteEmailsByIDs :exec
DELETE FROM emails
WHERE account_id = sqlc.arg(account_id) AND id = ANY(sqlc.arg(email_ids)::text[]);

-- name: DeleteThreadsByIDs :exec
DELETE FROM threads
WHERE account_id = sqlc.arg(account_id) AND id = ANY(sqlc.arg(thread_ids)::text[]);

-- name: DeleteEmailMailboxesForEmail :exec
DELETE FROM email_mailboxes
WHERE account_id = sqlc.arg(account_id) AND email_id = sqlc.arg(email_id);

-- name: UpsertAccount :exec
INSERT INTO email_identities (
    identity_id, jmap_account_id, primary_address, display_name,
    stalwart_principal_type, identity_type, status, synced_at, updated_at
)
VALUES (
    sqlc.arg(account_id),
    sqlc.arg(jmap_account_id),
    sqlc.arg(email_address),
    sqlc.arg(display_name),
    sqlc.arg(principal_type),
    sqlc.arg(principal_type),
    'active',
    sqlc.arg(synced_at),
    sqlc.arg(synced_at)
)
ON CONFLICT (identity_id) DO UPDATE
SET jmap_account_id = EXCLUDED.jmap_account_id,
    primary_address = EXCLUDED.primary_address,
    display_name = EXCLUDED.display_name,
    stalwart_principal_type = EXCLUDED.stalwart_principal_type,
    identity_type = EXCLUDED.identity_type,
    synced_at = EXCLUDED.synced_at,
    updated_at = EXCLUDED.updated_at;

-- name: UpsertProvisionedIdentity :exec
INSERT INTO email_identities (
    identity_id, org_id, identity_type, primary_address, display_name,
    zitadel_user_id, status, updated_at
)
VALUES (
    sqlc.arg(identity_id),
    sqlc.arg(org_id),
    sqlc.arg(identity_type),
    sqlc.arg(primary_address),
    sqlc.arg(display_name),
    sqlc.arg(zitadel_user_id),
    sqlc.arg(status),
    sqlc.arg(updated_at)
)
ON CONFLICT (identity_id) DO UPDATE
SET org_id = EXCLUDED.org_id,
    identity_type = EXCLUDED.identity_type,
    primary_address = EXCLUDED.primary_address,
    display_name = EXCLUDED.display_name,
    zitadel_user_id = EXCLUDED.zitadel_user_id,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at;

-- name: UpsertAddress :exec
INSERT INTO email_addresses (
    address, org_id, identity_id, local_part, domain, address_type,
    inbound_enabled, outbound_enabled, status, updated_at
)
VALUES (
    sqlc.arg(address),
    sqlc.arg(org_id),
    sqlc.arg(identity_id),
    sqlc.arg(local_part),
    sqlc.arg(domain),
    sqlc.arg(address_type),
    sqlc.arg(inbound_enabled),
    sqlc.arg(outbound_enabled),
    sqlc.arg(status),
    sqlc.arg(updated_at)
)
ON CONFLICT (address) DO UPDATE
SET org_id = EXCLUDED.org_id,
    identity_id = EXCLUDED.identity_id,
    local_part = EXCLUDED.local_part,
    domain = EXCLUDED.domain,
    address_type = EXCLUDED.address_type,
    inbound_enabled = EXCLUDED.inbound_enabled,
    outbound_enabled = EXCLUDED.outbound_enabled,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at;

-- name: UpsertEmailIdentityMembership :exec
INSERT INTO email_identity_memberships (identity_id, subject_id, role, granted_by)
VALUES (sqlc.arg(identity_id), sqlc.arg(subject_id), sqlc.arg(role), sqlc.arg(granted_by))
ON CONFLICT (identity_id, subject_id) DO UPDATE
SET role = EXCLUDED.role,
    granted_by = EXCLUDED.granted_by;

-- name: UpsertSendAsGrant :exec
INSERT INTO send_as_grants (address, subject_id, granted_by, revoked_at)
VALUES (sqlc.arg(address), sqlc.arg(subject_id), sqlc.arg(granted_by), NULL)
ON CONFLICT (address, subject_id) DO UPDATE
SET granted_by = EXCLUDED.granted_by,
    revoked_at = NULL;

-- name: HasSendAsGrant :one
SELECT EXISTS (
    SELECT 1
    FROM send_as_grants
    WHERE address = sqlc.arg(address)
      AND subject_id = sqlc.arg(subject_id)
      AND revoked_at IS NULL
)::bool;

-- name: GetOutboundAddress :one
SELECT address, org_id, identity_id, outbound_enabled, status
FROM email_addresses
WHERE org_id = sqlc.arg(org_id)
  AND address = sqlc.arg(address);

-- name: UpsertForwardingDestination :exec
INSERT INTO forwarding_destinations (destination_id, org_id, destination_email, status, verified_at)
VALUES (sqlc.arg(destination_id), sqlc.arg(org_id), sqlc.arg(destination_email), sqlc.arg(status), sqlc.arg(verified_at))
ON CONFLICT (org_id, destination_email) DO UPDATE
SET status = EXCLUDED.status,
    verified_at = EXCLUDED.verified_at;

-- name: UpsertForwardingRule :exec
INSERT INTO forwarding_rules (rule_id, source_address, destination_id, keep_local_copy, enabled)
VALUES (sqlc.arg(rule_id), sqlc.arg(source_address), sqlc.arg(destination_id), sqlc.arg(keep_local_copy), sqlc.arg(enabled))
ON CONFLICT (source_address, destination_id) DO UPDATE
SET keep_local_copy = EXCLUDED.keep_local_copy,
    enabled = EXCLUDED.enabled;

-- name: UpsertProviderBinding :exec
INSERT INTO provider_bindings (
    provider, domain, from_address, active, requests_per_second_limit,
    daily_message_limit, metadata, updated_at
)
VALUES (
    sqlc.arg(provider),
    sqlc.arg(domain),
    sqlc.arg(from_address),
    sqlc.arg(active),
    sqlc.arg(requests_per_second_limit),
    sqlc.arg(daily_message_limit),
    sqlc.arg(metadata)::jsonb,
    sqlc.arg(updated_at)
)
ON CONFLICT (provider, domain, from_address) DO UPDATE
SET active = EXCLUDED.active,
    requests_per_second_limit = EXCLUDED.requests_per_second_limit,
    daily_message_limit = EXCLUDED.daily_message_limit,
    metadata = EXCLUDED.metadata,
    updated_at = EXCLUDED.updated_at;

-- name: CreateOutboundMessage :one
INSERT INTO outbound_messages (
    message_id, org_id, from_address, to_address, subject, text_body, html_body,
    idempotency_key, payload_fingerprint, workflow_key, workflow_run_id, provider, status, created_by
)
VALUES (
    sqlc.arg(message_id),
    sqlc.arg(org_id),
    sqlc.arg(from_address),
    sqlc.arg(to_address),
    sqlc.arg(subject),
    sqlc.arg(text_body),
    sqlc.arg(html_body),
    sqlc.arg(idempotency_key),
    sqlc.arg(payload_fingerprint),
    sqlc.arg(workflow_key),
    sqlc.arg(workflow_run_id),
    sqlc.arg(provider),
    'queued',
    sqlc.arg(created_by)
)
ON CONFLICT (org_id, idempotency_key) DO UPDATE
SET updated_at = outbound_messages.updated_at
RETURNING message_id, org_id, from_address, to_address, subject, text_body, html_body,
          idempotency_key, payload_fingerprint, workflow_key, workflow_run_id, provider, provider_message_id,
          status, created_by, created_at, updated_at, sent_at;

-- name: InsertDeliveryAttempt :exec
INSERT INTO email_delivery_attempts (
    attempt_id, message_id, provider, idempotency_key, status, requested_at
)
VALUES (
    sqlc.arg(attempt_id),
    sqlc.arg(message_id),
    sqlc.arg(provider),
    sqlc.arg(idempotency_key),
    'sending',
    sqlc.arg(requested_at)
);

-- name: CompleteDeliveryAttempt :exec
UPDATE email_delivery_attempts
SET status = sqlc.arg(status),
    provider_message_id = sqlc.arg(provider_message_id),
    error_message = sqlc.arg(error_message),
    completed_at = sqlc.arg(completed_at)
WHERE attempt_id = sqlc.arg(attempt_id);

-- name: CompleteOutboundMessage :exec
UPDATE outbound_messages
SET status = sqlc.arg(status),
    provider_message_id = sqlc.arg(provider_message_id),
    updated_at = sqlc.arg(completed_at),
    sent_at = CASE
        WHEN sqlc.arg(status)::text = 'sent' THEN sqlc.arg(completed_at)
        ELSE sent_at
    END
WHERE message_id = sqlc.arg(message_id);

-- name: UpsertMailbox :exec
INSERT INTO mailboxes (
    account_id, id, name, parent_id, role, sort_order,
    total_emails, unread_emails, total_threads, unread_threads, synced_at
)
VALUES (
    sqlc.arg(account_id),
    sqlc.arg(mailbox_id),
    sqlc.arg(name),
    sqlc.arg(parent_id),
    sqlc.arg(role),
    sqlc.arg(sort_order),
    sqlc.arg(total_emails),
    sqlc.arg(unread_emails),
    sqlc.arg(total_threads),
    sqlc.arg(unread_threads),
    sqlc.arg(synced_at)
)
ON CONFLICT (account_id, id) DO UPDATE
SET name = EXCLUDED.name,
    parent_id = EXCLUDED.parent_id,
    role = EXCLUDED.role,
    sort_order = EXCLUDED.sort_order,
    total_emails = EXCLUDED.total_emails,
    unread_emails = EXCLUDED.unread_emails,
    total_threads = EXCLUDED.total_threads,
    unread_threads = EXCLUDED.unread_threads,
    synced_at = EXCLUDED.synced_at;

-- name: UpsertEmail :exec
INSERT INTO emails (
    account_id, id, blob_id, thread_id, subject, from_name, from_email,
    to_list, cc_list, reply_to_list, preview, keywords, has_attachment,
    size, received_at, sent_at, is_seen, is_flagged, is_answered, is_draft, synced_at
)
VALUES (
    sqlc.arg(account_id),
    sqlc.arg(email_id),
    sqlc.arg(blob_id),
    sqlc.arg(thread_id),
    sqlc.arg(subject),
    sqlc.arg(from_name),
    sqlc.arg(from_email),
    sqlc.arg(to_list)::jsonb,
    sqlc.arg(cc_list)::jsonb,
    sqlc.arg(reply_to_list)::jsonb,
    sqlc.arg(preview),
    sqlc.arg(keywords)::jsonb,
    sqlc.arg(has_attachment),
    sqlc.arg(size),
    sqlc.arg(received_at),
    sqlc.arg(sent_at),
    sqlc.arg(is_seen),
    sqlc.arg(is_flagged),
    sqlc.arg(is_answered),
    sqlc.arg(is_draft),
    sqlc.arg(synced_at)
)
ON CONFLICT (account_id, id) DO UPDATE
SET blob_id = EXCLUDED.blob_id,
    thread_id = EXCLUDED.thread_id,
    subject = EXCLUDED.subject,
    from_name = EXCLUDED.from_name,
    from_email = EXCLUDED.from_email,
    to_list = EXCLUDED.to_list,
    cc_list = EXCLUDED.cc_list,
    reply_to_list = EXCLUDED.reply_to_list,
    preview = EXCLUDED.preview,
    keywords = EXCLUDED.keywords,
    has_attachment = EXCLUDED.has_attachment,
    size = EXCLUDED.size,
    received_at = EXCLUDED.received_at,
    sent_at = EXCLUDED.sent_at,
    is_seen = EXCLUDED.is_seen,
    is_flagged = EXCLUDED.is_flagged,
    is_answered = EXCLUDED.is_answered,
    is_draft = EXCLUDED.is_draft,
    synced_at = EXCLUDED.synced_at;

-- name: UpsertEmailMailbox :exec
INSERT INTO email_mailboxes (account_id, email_id, mailbox_id)
VALUES (sqlc.arg(account_id), sqlc.arg(email_id), sqlc.arg(mailbox_id))
ON CONFLICT (account_id, email_id, mailbox_id) DO NOTHING;

-- name: UpsertThread :exec
INSERT INTO threads (account_id, id, email_ids, synced_at)
VALUES (sqlc.arg(account_id), sqlc.arg(thread_id), sqlc.arg(email_ids)::jsonb, sqlc.arg(synced_at))
ON CONFLICT (account_id, id) DO UPDATE
SET email_ids = EXCLUDED.email_ids,
    synced_at = EXCLUDED.synced_at;

-- name: TouchSyncState :exec
INSERT INTO sync_state (account_id, entity_type, jmap_state, updated_at)
VALUES (sqlc.arg(account_id), sqlc.arg(entity_type), sqlc.arg(jmap_state), sqlc.arg(updated_at))
ON CONFLICT (account_id, entity_type) DO UPDATE
SET jmap_state = EXCLUDED.jmap_state,
    updated_at = EXCLUDED.updated_at;

-- name: UpsertEmailBody :exec
INSERT INTO email_bodies (account_id, email_id, text_body, html_body, fetched_at)
VALUES (sqlc.arg(account_id), sqlc.arg(email_id), sqlc.arg(text_body), sqlc.arg(html_body), sqlc.arg(fetched_at))
ON CONFLICT (account_id, email_id) DO UPDATE
SET text_body = EXCLUDED.text_body,
    html_body = EXCLUDED.html_body,
    fetched_at = EXCLUDED.fetched_at;
