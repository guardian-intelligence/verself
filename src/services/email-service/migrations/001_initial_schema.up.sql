CREATE TABLE email_identities (
    identity_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL DEFAULT '',
    identity_type TEXT NOT NULL DEFAULT '',
    primary_address TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    zitadel_user_id TEXT NOT NULL DEFAULT '',
    jmap_account_id TEXT NOT NULL DEFAULT '',
    stalwart_principal_type TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    synced_at TIMESTAMPTZ NOT NULL DEFAULT to_timestamp(0),
    UNIQUE (org_id, primary_address)
);

CREATE TABLE email_addresses (
    address TEXT PRIMARY KEY,
    org_id TEXT NOT NULL DEFAULT '',
    identity_id TEXT NOT NULL REFERENCES email_identities(identity_id) ON DELETE CASCADE,
    local_part TEXT NOT NULL DEFAULT '',
    domain TEXT NOT NULL DEFAULT '',
    address_type TEXT NOT NULL DEFAULT '',
    inbound_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    outbound_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, identity_id),
    UNIQUE (domain, local_part)
);

CREATE TABLE email_identity_memberships (
    identity_id TEXT NOT NULL REFERENCES email_identities(identity_id) ON DELETE CASCADE,
    subject_id TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'reader',
    granted_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (identity_id, subject_id)
);

CREATE TABLE send_as_grants (
    address TEXT NOT NULL REFERENCES email_addresses(address) ON DELETE CASCADE,
    subject_id TEXT NOT NULL,
    granted_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    PRIMARY KEY (address, subject_id)
);

CREATE TABLE forwarding_destinations (
    destination_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL DEFAULT '',
    destination_email TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'verified',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    verified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, destination_email)
);

CREATE TABLE forwarding_rules (
    rule_id TEXT PRIMARY KEY,
    source_address TEXT NOT NULL REFERENCES email_addresses(address) ON DELETE CASCADE,
    destination_id TEXT NOT NULL REFERENCES forwarding_destinations(destination_id) ON DELETE CASCADE,
    keep_local_copy BOOLEAN NOT NULL DEFAULT TRUE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_address, destination_id)
);

CREATE TABLE provider_bindings (
    provider TEXT NOT NULL,
    domain TEXT NOT NULL DEFAULT '',
    from_address TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    requests_per_second_limit INT NOT NULL DEFAULT 1,
    daily_message_limit INT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, domain, from_address)
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

CREATE TABLE outbound_messages (
    message_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL DEFAULT '',
    from_address TEXT NOT NULL DEFAULT '',
    to_address TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    text_body TEXT NOT NULL DEFAULT '',
    html_body TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    payload_fingerprint TEXT NOT NULL DEFAULT '',
    workflow_key TEXT NOT NULL DEFAULT '',
    workflow_run_id TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    provider_message_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'queued',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    UNIQUE (org_id, idempotency_key)
);

CREATE TABLE email_delivery_attempts (
    attempt_id TEXT PRIMARY KEY,
    message_id TEXT NOT NULL REFERENCES outbound_messages(message_id) ON DELETE CASCADE,
    provider TEXT NOT NULL DEFAULT '',
    provider_message_id TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'sending',
    error_message TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE TABLE audience_contacts (
    contact_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL DEFAULT '',
    email_address TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '',
    last_name TEXT NOT NULL DEFAULT '',
    unsubscribed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, email_address)
);

CREATE TABLE suppression_entries (
    org_id TEXT NOT NULL DEFAULT '',
    email_address TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    provider_event_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, email_address, reason)
);

CREATE TABLE campaigns (
    campaign_id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL DEFAULT '',
    from_address TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE campaign_recipients (
    campaign_id TEXT NOT NULL REFERENCES campaigns(campaign_id) ON DELETE CASCADE,
    contact_id TEXT NOT NULL REFERENCES audience_contacts(contact_id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'planned',
    message_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (campaign_id, contact_id)
);

CREATE INDEX email_identities_org_idx
    ON email_identities (org_id, identity_type, primary_address);

CREATE INDEX email_addresses_identity_idx
    ON email_addresses (identity_id);

CREATE INDEX email_identity_memberships_subject_idx
    ON email_identity_memberships (subject_id, identity_id);

CREATE INDEX send_as_grants_subject_idx
    ON send_as_grants (subject_id, address)
    WHERE revoked_at IS NULL;

CREATE INDEX forwarding_rules_source_idx
    ON forwarding_rules (source_address)
    WHERE enabled;

CREATE INDEX mailboxes_account_role_idx
    ON mailboxes (account_id, role);

CREATE INDEX emails_account_received_idx
    ON emails (account_id, received_at DESC, id);

CREATE INDEX emails_account_thread_idx
    ON emails (account_id, thread_id);

CREATE INDEX email_mailboxes_account_mailbox_idx
    ON email_mailboxes (account_id, mailbox_id, email_id);

CREATE INDEX outbound_messages_org_created_idx
    ON outbound_messages (org_id, created_at DESC, message_id);

CREATE INDEX email_delivery_attempts_message_idx
    ON email_delivery_attempts (message_id, requested_at DESC);

CREATE INDEX audience_contacts_org_email_idx
    ON audience_contacts (org_id, email_address);
