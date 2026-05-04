-- name: Ping :one
SELECT 1::int AS one;

-- name: ResolveBinding :one
SELECT account_id
FROM mailbox_bindings
WHERE subject = sqlc.arg(subject);

-- name: LookupAccount :one
SELECT account_id, jmap_account_id, email_address, display_name, principal_type, synced_at
FROM mailbox_accounts
WHERE account_id = sqlc.arg(account_id);

-- name: ListAccounts :many
SELECT account_id, jmap_account_id, email_address, display_name, principal_type, synced_at
FROM mailbox_accounts
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
INSERT INTO mailbox_accounts (account_id, jmap_account_id, email_address, display_name, principal_type, synced_at)
VALUES (
    sqlc.arg(account_id),
    sqlc.arg(jmap_account_id),
    sqlc.arg(email_address),
    sqlc.arg(display_name),
    sqlc.arg(principal_type),
    sqlc.arg(synced_at)
)
ON CONFLICT (account_id) DO UPDATE
SET jmap_account_id = EXCLUDED.jmap_account_id,
    email_address = EXCLUDED.email_address,
    display_name = EXCLUDED.display_name,
    principal_type = EXCLUDED.principal_type,
    synced_at = EXCLUDED.synced_at;

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
