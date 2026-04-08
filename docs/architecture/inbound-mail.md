# Inbound Mail

Stalwart Mail Server (v0.15.5, Rust, AGPL-3.0) provides receive-only SMTP and JMAP on the single node. Outbound email stays with Resend. Stalwart serves two purposes: internal infrastructure (agent mailboxes for 2FA, invoices, testing) and future metered product (hosted email for operator's customers).

## Network topology

Stalwart has two network paths — SMTP direct, HTTP through Caddy:

```
                    Internet
                    │      │
                    │      │
            port 25 │      │ port 443
          (STARTTLS)│      │ (TLS)
                    │      │
                    ▼      ▼
              Stalwart    Caddy
              SMTP        (TLS + WAF)
                │          │
                │          │ mail.<domain>
                │          ▼
                │    Stalwart HTTP
                │    127.0.0.1:8090
                │    (JMAP + webmail + admin)
                │          │
                └────┬─────┘
                     │
                     ▼
                PostgreSQL
                db: stalwart
```

**SMTP (port 25):** Stalwart binds `0.0.0.0:25` directly on the public interface. Caddy is an HTTP reverse proxy — it cannot proxy raw SMTP. Stalwart handles its own STARTTLS using certs synced from Caddy's ACME storage.

**JMAP/Webmail (port 443):** The HTTP listener binds `127.0.0.1:8090` (loopback only, not internet-accessible). Caddy reverse proxies `mail.<domain>` to it, applying TLS termination, Coraza WAF, and access logging — same as every other HTTP service.

**Management API:** Same HTTP listener (`127.0.0.1:8090`), `/api/` path prefix. Only reachable from the box itself since the listener is loopback-bound.

## Security model

Four layers:

**1. nftables egress lockdown** (`stalwart.nft`): The `stalwart` system user can only make outbound connections to loopback (PostgreSQL on 5432, otelcol on 4317), DNS port 53 (DNSBL spam filter lookups), and port 465 (Resend SMTP relay for Sieve-initiated forwarding). All other egress is dropped.

**2. Receive-only SMTP** (`relay = false` in config): Stalwart rejects relay attempts from external SMTP clients — it only delivers to local mailboxes during inbound SMTP sessions. Server-side Sieve `redirect` rules can generate outbound mail that routes through the Resend relay (`queue.route.relay`), but this is not user-triggerable via SMTP — only admin-provisioned Sieve scripts can initiate outbound delivery.

**3. systemd hardening:** `ProtectSystem=strict`, `NoNewPrivileges=true`, `CapabilityBoundingSet=CAP_NET_BIND_SERVICE` (sole capability — for port 25), `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`, `RestrictNamespaces=true`, `LockPersonality=true`, `ReadWritePaths` limited to `/var/lib/stalwart`.

**4. HTTP loopback binding:** The JMAP API and Management API are not directly internet-accessible. External access routes through Caddy, which applies WAF rules and access logging. The admin endpoints require Basic Auth with the admin credential from credstore.

## Storage

PostgreSQL database `stalwart` (user `stalwart`). All four Stalwart store roles (data, blob, fts, lookup) and the internal directory map to this single database. Stalwart manages its own schema on first boot (~20s initialization) — no migration files in the repo.

## Mailbox scheme

- `ceo@<domain>` — operator, reserved
- `<name>.agent@<domain>` — agent mailboxes (`bernoulli.agent`, `dijkstra.agent`, `lamport.agent`)

Accounts are pre-created via Stalwart's Management REST API in `seed-demo.yml --tags stalwart`. No auto-provisioning on first OIDC or JMAP login.

## Authentication

Internal directory backed by PostgreSQL. Passwords must be bcrypt-hashed before passing to the Management API (Stalwart stores `secrets` verbatim and detects the hash algorithm by prefix). Accounts require `roles: ["user"]` for JMAP/IMAP access — without it, authentication succeeds but returns 403.

Basic Auth is used for both JMAP and the Management API. Stalwart does not support `grant_type=password` on its OAuth endpoint.

## Querying mail via JMAP

```bash
# List emails for an account
curl -s -u bernoulli.agent:<password> \
  https://mail.<domain>/jmap \
  -H 'Content-Type: application/json' \
  -d '{"using":["urn:ietf:params:jmap:core","urn:ietf:params:jmap:mail"],
       "methodCalls":[
         ["Email/query",{"limit":10,"sort":[{"property":"receivedAt","isAscending":false}]},"a"],
         ["Email/get",{"#ids":{"resultOf":"a","name":"Email/query","path":"/ids"},
                       "properties":["subject","receivedAt","from","bodyValues"],
                       "fetchAllBodyValues":true},"b"]]}'

# JMAP session discovery (shows capabilities, account IDs)
curl -s -u bernoulli.agent:<password> https://mail.<domain>/jmap/session
```

## Management API (admin only, loopback)

```bash
# List all principals
curl -s -u admin:<admin_password> http://127.0.0.1:8090/api/principal?limit=50

# Create an account (password must be pre-hashed)
HASH=$(python3 -c "import bcrypt; print(bcrypt.hashpw(b'MyPassword', bcrypt.gensalt()).decode())")
curl -s -u admin:<admin_password> http://127.0.0.1:8090/api/principal \
  -H 'Content-Type: application/json' \
  -d "{\"type\":\"individual\",\"name\":\"user\",\"secrets\":[\"$HASH\"],
       \"emails\":[\"user@<domain>\"],\"roles\":[\"user\"]}"

# Check if a principal exists (API returns 200 for everything; check .error field)
curl -s -u admin:<admin_password> http://127.0.0.1:8090/api/principal/name/user
# Found: {"data":{"id":3,...}}   Not found: {"error":"notFound","item":"name"}
```

## Telemetry

Native OTLP over gRPC to otelcol-contrib on `127.0.0.1:4317`. Traces, logs, and metrics are all pushed — no scraping required. Data lands in ClickHouse under `ServiceName = 'stalwart'`.

```bash
make traces SERVICE=stalwart           # recent traces + logs
make traces SERVICE=stalwart ERRORS=1  # errors only
```

Stalwart emits 500+ event types across SMTP, IMAP, JMAP, auth, delivery, spam, TLS, and queue categories. All flow through the existing otelcol pipeline into `default.otel_logs` and `default.otel_traces`.

## TLS cert lifecycle

1. **First deploy:** A self-signed EC placeholder cert is generated for `mail.<domain>`. Stalwart starts with STARTTLS using this cert. External senders will see an untrusted cert but most MTAs proceed anyway (opportunistic TLS).
2. **~2 minutes later:** Caddy obtains the real Let's Encrypt cert for `mail.<domain>` (it's in the Caddyfile as a reverse proxy target, which triggers ACME).
3. **Every 12 hours:** A systemd timer (`stalwart-cert-sync.timer`) runs `/usr/local/bin/stalwart-cert-sync`, which copies the ACME cert from Caddy's storage (`/caddy/certificates/`) to `/etc/stalwart/certs/` if newer, then sends SIGHUP to reload Stalwart's TLS context without dropping active connections.

## DNS records

Managed by Ansible:

| Record | Type | Value | Source |
|---|---|---|---|
| `mail.<domain>` | A | `<server_ip>` | `cloudflare_dns` role |
| `<domain>` | MX | `10 mail.<domain>` | `stalwart` role `dns.yml` |
| `<domain>` | TXT (SPF) | `v=spf1 mx include:<resend_domain> -all` | `stalwart` role `dns.yml` |

**Manual step:** Request a PTR record from Latitude.sh for `<server_ip>` → `mail.<domain>`. Required for forward-confirmed reverse DNS (FCrDNS). Without it, Google and Microsoft may soft-reject or spam-folder inbound delivery. This cannot be automated via Cloudflare — it's controlled by the IP address owner.

## Sieve filtering

Sieve (RFC 5228) scripts run server-side when a message arrives, before it lands in the recipient's mailbox. Stalwart supports per-account Sieve scripts managed via JMAP (`urn:ietf:params:jmap:sieve`) or ManageSieve (port 4190, internal only).

**Operator forwarding:** The `ceo@` account has a Sieve `redirect :copy` script that forwards all inbound mail to the operator's personal address. The forwarded copy is relayed through Resend SMTP (`queue.route.relay`). This is provisioned by `seed-demo.yml --tags stalwart` when `stalwart_operator_forward_to` is set in `group_vars/all/main.yml`. When empty (default), no forwarding is configured.

```sieve
require ["redirect", "copy"];
redirect :copy "operator@personal.com";
```

The `:copy` flag keeps a local copy in the `ceo@` mailbox. Without it, the original would be consumed by the redirect.

**Agent use case:** Sieve can auto-file 2FA codes (`if header :contains "Subject" "verification code" { fileinto "2FA"; }`) or discard noise before JMAP ever sees it. Agent Sieve scripts would be provisioned alongside accounts in the seed playbook.

## Configuration split

Stalwart v0.15+ enforces a split between **local settings** (TOML file, read at startup) and **database settings** (stored in PostgreSQL, pushed via the Settings API). Server/listener/certificate/store/directory/auth/tracer config lives in TOML. Session/queue/metrics config is pushed post-startup by `tasks/settings.yml` via `POST /api/settings`.

This matters for the outbound relay: `queue.route.relay.*` and `queue.strategy.route` are database settings — they cannot be defined in `stalwart.toml.j2`.

## Developer tooling

```bash
cd src/platform && ./scripts/mail.sh                         # List ceo@ inbox
cd src/platform && ./scripts/mail.sh -u bernoulli.agent      # List agent inbox
cd src/platform && ./scripts/mail.sh -u bernoulli.agent -c   # Extract latest 2FA code
cd src/platform && ./scripts/mail.sh -u bernoulli.agent -r <JMAP_ID>  # Read full email
```

## Relevant files

- `roles/stalwart/` — Ansible role (tasks, templates, defaults, handlers)
- `roles/stalwart/templates/stalwart.toml.j2` — local-only server config (TOML)
- `roles/stalwart/tasks/settings.yml` — database-scoped settings push (session, queue, metrics)
- `roles/stalwart/templates/stalwart.nft.j2` — egress firewall (per-user nftables)
- `roles/stalwart/templates/stalwart-cert-sync.sh.j2` — ACME cert sync script
- `roles/stalwart/tasks/dns.yml` — MX + SPF record creation
- `roles/stalwart/tasks/cert_sync.yml` — systemd timer + oneshot for cert sync
- `playbooks/seed-demo.yml` (tag: `stalwart`) — mailbox + Sieve provisioning
- `scripts/mail.sh` — JMAP client for inbox listing, email reading, 2FA code extraction

## Product evolution

Stalwart is deployed as internal infrastructure today but is structured to become a metered product (hosted email for the operator's customers). The metering surface:

| Dimension | Source |
|---|---|
| Storage (bytes) | Management API or PostgreSQL query |
| Inbound messages | ClickHouse counter on SMTP `message-ingest` events |
| Outbound messages | Resend webhook callbacks |
| Mailboxes provisioned | Stalwart principal count |

Billing integration follows the same Reserve/Settle/Void pattern as sandbox rentals. AGPL-3.0 requires source availability for network-service users — a non-issue since the entire stack is FOSS.

The built-in webmail is a temporary stopgap. The target UI is a clean-room TanStack Start + ElectricSQL implementation inspired by [Bulwark](https://github.com/bulwarkmail/webmail) (AGPL-3.0, purpose-built JMAP client for Stalwart). ElectricSQL's shape subscriptions replace Bulwark's manual optimistic updates, and the JMAP client interface (~1500 lines) is reimplemented against our stack.
