-- ═══════════════════════════════════════════════════════════════════════════
-- verself: host SSH authentication events
--
-- One row per sshd journald line on the bare-metal node. Source pipeline:
-- otelcol journald/sshd receiver → default.otel_logs (Body holds the parsed
-- MESSAGE field) → host_auth_events_mv extracts source IP, principal, cert
-- serial, and outcome via re2 patterns over Body.
--
-- The MV is the boundary between unstructured sshd log lines and the
-- queryable security surface. sshd's log format is stable (the strings
-- "Accepted publickey", "Invalid user", "from <IP> port <N>", and the cert
-- "ID ... (serial N)" suffix all date back to OpenSSH ~6.x), so regex
-- extraction is the simplest correct option. If sshd ever switches to
-- structured logging, the MV becomes a renaming exercise.
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS verself.host_auth_events
(
    recorded_at         DateTime64(9)               CODEC(Delta(8), ZSTD(3)),
    event_date          Date                        DEFAULT toDate(recorded_at),

    outcome             LowCardinality(String)      CODEC(ZSTD(3)),
    auth_method         LowCardinality(String)      CODEC(ZSTD(3)),
    user                LowCardinality(String)      CODEC(ZSTD(3)),

    source_ip           String                      CODEC(ZSTD(3)),
    source_port         UInt16                      CODEC(T64, ZSTD(3)),

    key_type            LowCardinality(String)      CODEC(ZSTD(3)),
    key_fingerprint     String                      CODEC(ZSTD(3)),

    cert_serial         String                      CODEC(ZSTD(3)),
    cert_id             String                      CODEC(ZSTD(3)),
    ca_fingerprint      String                      CODEC(ZSTD(3)),

    body                String                      CODEC(ZSTD(3)),

    INDEX idx_source_ip       source_ip       TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_cert_serial     cert_serial     TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_user            user            TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toDate(event_date)
PRIMARY KEY (event_date, outcome, auth_method)
ORDER BY (event_date, outcome, auth_method, recorded_at)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- MV runs on every insert into default.otel_logs; the WHERE clause drops
-- non-sshd rows before the regex extractors fire. Body is the raw sshd
-- MESSAGE, e.g.
--   Accepted publickey for ubuntu from 1.2.3.4 port 51234 ssh2: ED25519 SHA256:abc CA ED25519 SHA256:def ID "operator" (serial 7)
--   Invalid user solana from 195.178.110.218 port 35708
--   Connection closed by authenticating user root 79.76.58.113 port 43406 [preauth]
CREATE MATERIALIZED VIEW IF NOT EXISTS verself.host_auth_events_mv
TO verself.host_auth_events
AS SELECT
    Timestamp                                                              AS recorded_at,
    toDate(Timestamp)                                                      AS event_date,
    multiIf(
        match(Body, '^Accepted '),                                          'accepted',
        -- "invalid user" appears in both "^Invalid user X..." and
        -- "^Failed password for invalid user X..."; classify both as
        -- invalid_user so the password-grinder noise doesn't get split
        -- across two outcomes.
        match(Body, 'invalid user '),                                       'invalid_user',
        match(Body, '^Invalid user '),                                      'invalid_user',
        match(Body, '^Failed password '),                                   'failed_password',
        match(Body, '^Connection closed by authenticating user '),          'preauth_close',
        match(Body, '^Disconnected from authenticating user '),             'preauth_close',
        match(Body, '^Connection closed by '),                              'preauth_close',
        match(Body, '^Authentication refused'),                             'auth_refused',
        'other'
    )                                                                      AS outcome,
    multiIf(
        match(Body, ' CA \\S+ SHA256:'),                                    'publickey-cert',
        match(Body, '^Accepted publickey '),                                'publickey',
        match(Body, '^Accepted password '),                                 'password',
        match(Body, '^Failed password '),                                   'password',
        match(Body, '\\[preauth\\]'),                                       'preauth',
        'other'
    )                                                                      AS auth_method,
    -- The username is whatever immediately follows the recognised verb. The
    -- verbs are stable across OpenSSH 8.x and 9.x; if a future upgrade adds
    -- a new shape, it lands as user='' in 'other' rows and stays queryable
    -- via Body.
    extract(
        Body,
        '(?:Accepted (?:publickey|password) for|Invalid user|Failed password for invalid user|Failed password for|Connection closed by invalid user|Disconnected from invalid user|Connection closed by authenticating user|Disconnected from authenticating user) (\\S+)'
    )                                                                      AS user,
    -- Two source_ip shapes: "from <ip> port <n>" (most lines) and
    -- "user <name> <ip> port <n>" (the preauth-close family). Try the
    -- former first; fall back to the latter.
    multiIf(
        match(Body, 'from \\S+ port \\d+'), extract(Body, 'from (\\S+) port \\d+'),
        match(Body, 'user \\S+ \\S+ port \\d+'), extract(Body, 'user \\S+ (\\S+) port \\d+'),
        ''
    )                                                                      AS source_ip,
    toUInt16OrZero(extract(Body, ' port (\\d+)'))                           AS source_port,
    extract(Body, 'ssh2: (\\w+) SHA256:')                                   AS key_type,
    extract(Body, 'ssh2: \\w+ SHA256:(\\S+)')                               AS key_fingerprint,
    extract(Body, '\\(serial (\\d+)\\)')                                    AS cert_serial,
    extract(Body, 'ID "([^"]+)"')                                           AS cert_id,
    extract(Body, ' CA \\w+ SHA256:(\\S+)')                                 AS ca_fingerprint,
    Body                                                                    AS body
FROM default.otel_logs
WHERE ServiceName = 'sshd';
