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

**1. nftables egress lockdown** (`stalwart.nft`): The `stalwart` system user can only make outbound connections to loopback (PostgreSQL on 5432, otelcol on 4317) and DNS port 53 (for DNSBL spam filter lookups). All other egress is dropped. Even a compromised Stalwart process cannot relay mail, exfiltrate data, or reach arbitrary endpoints.

**2. Receive-only SMTP** (`relay = false` in config): Stalwart delivers to local mailboxes only. Attempts to use it as an open relay are rejected at the SMTP session level. Combined with the nftables egress drop, outbound mail is impossible through two independent mechanisms.

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

## Relevant files

- `roles/stalwart/` — Ansible role (tasks, templates, defaults, handlers)
- `roles/stalwart/templates/stalwart.toml.j2` — server config
- `roles/stalwart/templates/stalwart.nft.j2` — egress firewall (per-user nftables)
- `roles/stalwart/templates/stalwart-cert-sync.sh.j2` — ACME cert sync script
- `roles/stalwart/tasks/dns.yml` — MX + SPF record creation
- `roles/stalwart/tasks/cert_sync.yml` — systemd timer + oneshot for cert sync
- `playbooks/seed-demo.yml` (tag: `stalwart`) — mailbox provisioning

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
