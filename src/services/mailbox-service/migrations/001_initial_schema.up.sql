CREATE TABLE mailbox_accounts (
    account_id TEXT PRIMARY KEY,
    jmap_account_id TEXT NOT NULL DEFAULT '',
    email_address TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    principal_type TEXT NOT NULL DEFAULT '',
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE mailbox_bindings (
    subject TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    email_address TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE sync_state (
    account_id TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    jmap_state TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, entity_type)
);

CREATE TABLE mailboxes (
    account_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    parent_id TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    total_emails INT NOT NULL DEFAULT 0,
    unread_emails INT NOT NULL DEFAULT 0,
    total_threads INT NOT NULL DEFAULT 0,
    unread_threads INT NOT NULL DEFAULT 0,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, id)
);

CREATE TABLE emails (
    account_id TEXT NOT NULL,
    id TEXT NOT NULL,
    blob_id TEXT NOT NULL DEFAULT '',
    thread_id TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    from_name TEXT NOT NULL DEFAULT '',
    from_email TEXT NOT NULL DEFAULT '',
    to_list JSONB NOT NULL DEFAULT '[]'::jsonb,
    cc_list JSONB NOT NULL DEFAULT '[]'::jsonb,
    reply_to_list JSONB NOT NULL DEFAULT '[]'::jsonb,
    preview TEXT NOT NULL DEFAULT '',
    keywords JSONB NOT NULL DEFAULT '{}'::jsonb,
    has_attachment BOOLEAN NOT NULL DEFAULT FALSE,
    size INT NOT NULL DEFAULT 0,
    received_at TIMESTAMPTZ NOT NULL DEFAULT to_timestamp(0),
    sent_at TIMESTAMPTZ NOT NULL DEFAULT to_timestamp(0),
    is_seen BOOLEAN NOT NULL DEFAULT FALSE,
    is_flagged BOOLEAN NOT NULL DEFAULT FALSE,
    is_answered BOOLEAN NOT NULL DEFAULT FALSE,
    is_draft BOOLEAN NOT NULL DEFAULT FALSE,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, id)
);

CREATE TABLE email_mailboxes (
    account_id TEXT NOT NULL,
    email_id TEXT NOT NULL,
    mailbox_id TEXT NOT NULL,
    PRIMARY KEY (account_id, email_id, mailbox_id)
);

CREATE TABLE email_bodies (
    account_id TEXT NOT NULL,
    email_id TEXT NOT NULL,
    text_body TEXT NOT NULL DEFAULT '',
    html_body TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, email_id)
);

CREATE TABLE threads (
    account_id TEXT NOT NULL,
    id TEXT NOT NULL,
    email_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, id)
);

CREATE INDEX mailboxes_account_role_idx
    ON mailboxes (account_id, role);

CREATE INDEX emails_account_received_idx
    ON emails (account_id, received_at DESC, id);

CREATE INDEX emails_account_thread_idx
    ON emails (account_id, thread_id);

CREATE INDEX email_mailboxes_account_mailbox_idx
    ON email_mailboxes (account_id, mailbox_id, email_id);

CREATE INDEX mailbox_bindings_account_idx
    ON mailbox_bindings (account_id);

-- Electric will consume these tables later. When that wiring lands, keep
-- REPLICA IDENTITY FULL on the synced tables and add them to the service-owned
-- publication instead of creating frontend-specific copies.
