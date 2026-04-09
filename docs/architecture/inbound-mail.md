# Inbound Mail

Stalwart Mail Server (v0.15.5, Rust, AGPL-3.0) provides receive-only SMTP and JMAP on the single node. Outbound email stays with Resend. `mailbox-service` is the repo-owned mailbox layer that sits beside Stalwart: it rewrites public JMAP session discovery, keeps a normalized mailbox projection in PostgreSQL, exposes authenticated mailbox mutations for future webmail, and currently hosts the tactical operator-forwarding sidecar.

Stalwart still serves two purposes: internal infrastructure (`agents@` for 2FA, invoices, testing) and future metered product (hosted email for operator's customers).

## Network topology

Inbound mail now has three distinct boundaries: Stalwart for SMTP/JMAP, Caddy for the public HTTP surface, and `mailbox-service` for repo-owned mailbox behavior:

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
                │          ├────────────── /jmap/session ─────► mailbox-service
                │          │                                    127.0.0.1:4246
                │          └────────────── everything else ───► Stalwart HTTP
                │                                               127.0.0.1:8090
                │
                ├────────────── PostgreSQL db: stalwart
                │
                └────────────── mailbox-service ───────────────► PostgreSQL db: mailbox_service
                                 (sync + write API + forwarder)  and Resend HTTPS
```

**SMTP (port 25):** Stalwart binds `0.0.0.0:25` directly on the public interface. Caddy is an HTTP reverse proxy — it cannot proxy raw SMTP. Stalwart handles its own STARTTLS using certs synced from Caddy's ACME storage.

**JMAP (port 443):** Stalwart's HTTP listener binds `127.0.0.1:8090` (loopback only, not internet-accessible). Caddy reverse proxies `mail.<domain>` to it, applying TLS termination, Coraza WAF, and access logging — same as every other HTTP service. The only exception is `GET /jmap/session`, which is proxied through `mailbox-service` so the public session document advertises the external `https://` / `wss://` origin rather than Stalwart's internal loopback listener.

**Management API:** Same HTTP listener (`127.0.0.1:8090`), `/api/` path prefix. It is intentionally blocked from the public edge: Caddy returns `404` for `/api/*`, and Stalwart's local `http.allowed-endpoint` rules also reject non-loopback `/api` requests. Operational access stays box-local.

**Mailbox service:** `mailbox-service` binds `127.0.0.1:4246` and is not internet-accessible directly. Caddy only exposes the repo-owned mailbox API under `/api/v1/mail/*` on the webmail surface and the narrow `/jmap/session` rewrite on `mail.<domain>`.

## Security model

Four layers:

**1. Per-process nftables egress lockdown:** Stalwart itself can only talk to loopback and DNS. It no longer owns any relay or Resend credentials, and its old outbound queue settings are deleted during deploy. `mailbox-service` has its own tighter box-local rules for JMAP/PostgreSQL/JWKS access plus HTTPS egress for the tactical operator-forwarding path.

**2. Receive-only SMTP:** Stalwart rejects relay attempts from external SMTP clients and only performs local delivery during inbound SMTP sessions. Outbound relay is not configured in Stalwart. The only current outbound path is the separate operator-forwarding sidecar inside `mailbox-service`, which forwards a copy of `ceo@` mail through Resend's HTTPS API.

**3. systemd hardening:** Stalwart runs with `ProtectSystem=strict`, `NoNewPrivileges=true`, `CapabilityBoundingSet=CAP_NET_BIND_SERVICE` (sole capability — for port 25), `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`, `RestrictNamespaces=true`, `LockPersonality=true`, `ReadWritePaths` limited to `/var/lib/stalwart`. `mailbox-service` runs as a separate unprivileged service with its own credstore and nftables profile.

**4. Public HTTP boundary at Caddy:** The JMAP API is not directly internet-accessible; external access routes through Caddy, which applies WAF rules and access logging. `/api/*` on `mail.<domain>` is hard-denied. The only repo-owned HTTP behavior on that host today is the JMAP session rewrite handled by `mailbox-service`.

## Storage

There are now two mail-adjacent PostgreSQL databases:

- `stalwart` (user `stalwart`) — Stalwart's own store. All four Stalwart store roles (data, blob, fts, lookup) and the internal directory map to this single database. Stalwart manages its own schema on first boot (~20s initialization) — no migration files in the repo.
- `mailbox_service` (user `mailbox_service`) — repo-owned normalized mailbox projection and auth binding state. The schema is managed in `src/mailbox-service/migrations/` and currently contains `mailbox_accounts`, `mailbox_bindings`, `sync_state`, `mailboxes`, `emails`, `email_mailboxes`, `email_bodies`, and `threads`.

## Mailbox-service boundary

`mailbox-service` is the boundary between Stalwart's JMAP protocol surface and the rest of the Forge Metal product:

- It rewrites `/jmap/session` so clients see the public `https://mail.<domain>` / `wss://mail.<domain>` origin instead of Stalwart's internal `127.0.0.1:8090` listener.
- It discovers Stalwart accounts through the local management API, maps account IDs to configured passwords, subscribes to JMAP EventSource, and applies `Mailbox/changes`, `Email/changes`, and `Thread/changes` into the `mailbox_service` PostgreSQL schema.
- It exposes the authenticated mailbox write API (`/api/v1/mail/*`) for read/unread, flag, move, trash, and body hydration.
- It also currently hosts the tactical `ceo@` operator-forwarding sidecar. That forwarding path is deliberately outside Stalwart because per-user Sieve forwarding was not reliable enough for the product path.

## Mailbox scheme

- `ceo@<domain>` — operator, reserved
- `agents@<domain>` — shared agent mailbox

Accounts are pre-created via Stalwart's Management REST API in `seed-system.yml --tags stalwart`. No auto-provisioning on first OIDC or JMAP login.

## Authentication

Internal directory backed by PostgreSQL. Passwords must be bcrypt-hashed before passing to the Management API (Stalwart stores `secrets` verbatim and detects the hash algorithm by prefix). Accounts require `roles: ["user"]` for JMAP/IMAP access — without it, authentication succeeds but returns 403.

Basic Auth is used for both JMAP and the Management API. Stalwart does not support `grant_type=password` on its OAuth endpoint.

## Operator read API

```bash
# List synced mailbox accounts over the local operator API
curl -s http://127.0.0.1:4246/internal/mailbox/v1/accounts

# List recent synced emails for one account
curl -s 'http://127.0.0.1:4246/internal/mailbox/v1/accounts/agents/emails?limit=10'

# Read a specific email body for one account
curl -s http://127.0.0.1:4246/internal/mailbox/v1/accounts/agents/emails/<EMAIL_ID>
```

These endpoints are loopback-only and are intended for operator tooling. They read from `mailbox-service`'s PostgreSQL projection and fetch/cache the body through the existing sync/JMAP path when needed.

## Querying mail via JMAP

JMAP remains the Stalwart protocol boundary, but repo-owned tooling should prefer the operator API above instead of issuing ad hoc JMAP requests over SSH.

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

**Operator forwarding:** The `ceo@` account is forwarded by `mailbox-service`, not by Stalwart Sieve. `mailbox-service` polls the `ceo@` mailbox over loopback JMAP, forwards a copy through the Resend HTTPS API, and keeps a local copy in `ceo@`. This is provisioned by `seed-system.yml --tags stalwart` when `stalwart_operator_forward_to` is set in `group_vars/all/main.yml`. When empty (default), operator forwarding is disabled but the mailbox still receives mail locally. This is a tactical operational path, not the future mailbox sync architecture.

**Agent use case:** Sieve can auto-file 2FA codes (`if header :contains "Subject" "verification code" { fileinto "2FA"; }`) or discard noise before JMAP ever sees it. Rules for the shared `agents@` account would be provisioned alongside the account in the seed playbook.

## Configuration split

Stalwart v0.15+ enforces a split between **local settings** (TOML file, read at startup) and **database settings** (stored in PostgreSQL, pushed via the Settings API). Server/listener/certificate/store/directory/auth/tracer config lives in TOML. Session/queue/metrics config is pushed post-startup by `tasks/settings.yml` via `POST /api/settings`.

This matters for any future Stalwart queue features: database-scoped keys such as `queue.*` cannot be defined in `stalwart.toml.j2`.

## Developer tooling

```bash
make mail                              # List recent agents@ email
make mail-read ID=eaaaaab              # Read one agents@ email
make mail-code                         # Extract latest agents@ verification code
make mail-mailboxes                    # Show agents@ mailbox ids/roles
make mail MAILBOX=ceo                  # Switch to ceo@
```

## Relevant files

- `src/mailbox-service/` — repo-owned mailbox sync, write API, JMAP session rewrite, operator forwarder
- `roles/mailbox_service/` — deploy + credstore + nftables + migrations for `mailbox-service`
- `roles/stalwart/` — Ansible role (tasks, templates, defaults, handlers)
- `roles/stalwart/templates/stalwart.toml.j2` — local-only server config (TOML)
- `roles/stalwart/tasks/settings.yml` — database-scoped settings push (session, queue, metrics)
- `roles/stalwart/templates/stalwart.nft.j2` — egress firewall (per-user nftables)
- `roles/stalwart/templates/stalwart-cert-sync.sh.j2` — ACME cert sync script
- `roles/stalwart/tasks/dns.yml` — MX + SPF record creation
- `roles/stalwart/tasks/cert_sync.yml` — systemd timer + oneshot for cert sync
- `playbooks/seed-system.yml` (tag: `stalwart`) — mailbox + Sieve provisioning
- `cmd/mailbox-openapi/` + `client/` — generated operator/mutation API client surface
- `cmd/mailbox-tool/` — typed operator CLI over the generated mailbox-service client
- `scripts/mail.sh` — thin wrapper around `cmd/mailbox-tool`

## Product evolution

Stalwart is deployed as internal infrastructure today but is structured to become a metered product (hosted email for the operator's customers). The metering surface:

| Dimension | Source |
|---|---|
| Storage (bytes) | Management API or PostgreSQL query |
| Inbound messages | ClickHouse counter on SMTP `message-ingest` events |
| Outbound messages | Resend webhook callbacks |
| Mailboxes provisioned | Stalwart principal count |

Billing integration follows the same Reserve/Settle/Void pattern as sandbox rentals. AGPL-3.0 requires source availability for network-service users — a non-issue since the entire stack is FOSS.

Stalwart's built-in webadmin is operational/admin-only and not the product webmail surface. The target UI is a clean-room TanStack Start + ElectricSQL implementation inspired by [Bulwark](https://github.com/bulwarkmail/webmail), but with a different boundary: JMAP stays server-side in `mailbox-service`. The browser reads mailbox state from the `mailbox_service` PostgreSQL projection via ElectricSQL and sends mutations back through `mailbox-service`'s authenticated HTTP API.
