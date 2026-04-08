# forge-metal

Free Open-Source Software for a turnkey "software company in a box": fully self-hosted bare-metal platform with Forgejo, Fast CI via Firecracker + deep ZFS optimizations, ClickStack observability (logs + traces + metrics), TigerBeetle for financial OTLP, Stripe integration, Zitadel for enterprise-grade auth, PostgreSQL for general purpose RDBMs. This is not a PaaS -- the user owns what they deploy.

Bootstrapping UX: single command to go from their laptop -> bare metal instance -> all services + 2 deployed frontend apps reading/writing off the same DB (frontends not yet implemented).

## Direction

* vm-orchestrator (Go daemon) is the single privileged host process that manages Firecracker VMs: ZFS clones, TAP networking, jailer lifecycle, and guest telemetry aggregation. Exposes a gRPC API over a Unix socket for K8s services. vm-guest-telemetry (Zig) is the minimal guest agent streaming 60Hz health samples over vsock. Same infrastructure powers CI and customer sandbox workloads.
* Stalwart Mail Server for self-hosted email (receive-only, JMAP API for agent access). mail.<domain> frontend: clean-room TanStack + ElectricSQL implementation inspired by Bulwark (AGPL-3.0, Next.js 16, purpose-built JMAP client for Stalwart, ~243 stars), not a fork. Bulwark is the only production-quality JMAP-native webmail; we rewrite in our stack (TanStack Start + ElectricSQL) to avoid Next.js dependency and get real-time sync via Electric's Postgres-backed CRDT layer.
* Avoidd CLIs. Things talk to each other over HTTP. We're moving to k3s soon.

## Deployment Topology

This is a deploy-together system. Single-node is the default deployment. Everything runs on one box with no replication. Adding two more nodes (3 total) enables TigerBeetle consensus replication, ClickHouse ReplicatedMergeTree, Postgres streaming replication, and cross-node health monitoring with external paging. The single-node path is what we're currently working on and we will provide in the future a path to seamlessly upgrade to a three node topology with Netbird as the overlay.

Hard product design requirement: everything must be self-hosted.

Exceptions:

Optional - Backblaze B2, Cloudflare R2, AWS S3 for backups (will be done through `zfs send`, not LINSTOR + DRBD) [Backups not yet implemented]
Required - Domain Registar (Cloudflare only for now)
Required - Compute Provider (Latitude.sh only for now)
Required - Email Delivery (Resend only for now)

## Service Architecture

```
                              Internet (port 25)
                                      │
                              ┌───────▼───────┐
                              │  Stalwart     │
                              │  (SMTP+JMAP)  │───── OTLP ──┐
                              └───────────────┘              │
                                                             │
                                    ┌─────────────────────────────────────────────────────────┐
                                    │                     Caddy (TLS + WAF)                    │
                                    │   allowlist routing, Coraza WAF, Stripe IP allowlist     │
                                    └──┬──────────┬──────────┬──────────┬──────────┬──────────┘
                                       │          │          │          │          │
                              ┌────────▼──┐ ┌─────▼────┐ ┌──▼───┐ ┌───▼────┐ ┌───▼──────────┐
                              │rent-a-    │ │billing-  │ │Zitadel│ │Forgejo │ │  HyperDX     │
                              │sandbox    │ │service   │ │(OIDC) │ │(git+CI)│ │  (obs UI)    │
                              │(webapp)   │ │(Go/Huma) │ │       │ │        │ │              │
                              └─────┬─────┘ └──┬───┬───┘ └──┬───┘ └───┬────┘ └──────────────┘
                                    │          │   │        │         │
                              ┌─────▼──────────▼┐  │   OIDC JWKS      │
                              │sandbox-rental-  │  │   (cached)       │
                              │service (Go/Huma)│  │        │         │
                              └──┬────┬────┬────┘  │        │         │
                                 │    │    │       │        │         │
                    ┌────────────▼┐   │  ┌─▼───────▼───┐    │    ┌────▼─────────────┐
                    │vm-          │   │  │auth-        │    │    │forge-metal CLI   │
                    │orchestrator │   │  │middleware   │    │    │(CI warm/exec)    │
                    │(Go daemon)  │   │  │(Go library) │    │    │imports           │
                    └──┬──────────┘   │  └─────────────┘    │    │vm-orchestrator   │
                       │              │                     │    └──────────────────┘
              ┌────────▼──────┐       │
              │  Firecracker  │       │
              │  VMs (jailer) │       │                    Data Stores
              │  ┌──────────┐ │       │    ┌─────────────────────────────────────────┐
              │  │vm-guest- │ │       │    │                                         │
              │  │telemetry │ │       │    │  PostgreSQL ◄── billing schemas         │
              │  │(Zig agent│ │       │    │               ◄── sandbox job_logs      │
              │  │ 60Hz)    │ │       │    │               ◄── Zitadel event store   │
              │  └──────────┘ │       │    │               ◄── Forgejo metadata      │
              └───────────────┘       │    │               ◄── Stalwart mail store   │
                                      │    │                                         │
                                      │    │  TigerBeetle ◄── billing ledger         │
                                      │    │                   (Reserve/Settle/Void) │
                                      │    │                                         │
                                      │    │  ClickHouse  ◄── OTel logs/traces       │
                                      ├───►│               ◄── billing metering      │
                                      │    │               ◄── CI wide events        │
                                      │    │               ◄── sandbox job logs      │
                                      │    │               ◄── deploy events         │
                                      │    │                                         │
                                      │    │  MongoDB     ◄── HyperDX app state      │
                                      │    └─────────────────────────────────────────┘
                                      │
                              ┌───────▼───────┐
                              │  Stripe       │
                              │  (webhooks)   │
                              └───────────────┘
```

See src/platform/ansible/group_vars/all/services.yml for port assignments.

Secrets are SOPS-encrypted in `group_vars/all/secrets.sops.yml`, written by each service's Ansible role to `/etc/credstore/{service}/` (root-owned, service-group-readable), and loaded at runtime via systemd `LoadCredential=` into `$CREDENTIALS_DIRECTORY`.

Go services are written with the Huma v2 framework (https://pkg.go.dev/github.com/danielgtaylor/huma/v2) to support automatic generation of clients via OpenAPI v3.1. Do not write custom clients for go services.

### Auth model

Zitadel is the sole IdP. All Go services import `src/auth-middleware/` which validates JWTs against Zitadel's JWKS endpoint (cached, local crypto after first fetch). Identity (subject, org ID, roles, email) is extracted from token claims and attached to request context. Frontends use OIDC code flow + PKCE directly with Zitadel. No auth proxy, no BFF session layer. Social login (Google/GitHub/Microsoft/Apple), MFA, and passkeys are Zitadel-side configuration — the middleware sees the same JWT regardless.

**Single-node JWKS fetch path:** On a single bare-metal node, Go services fetch JWKS directly from Zitadel's loopback address (`http://127.0.0.1:8085/oauth/v2/keys`) using `oidc.ProviderConfig` with a split issuer/JWKS URL. The `IssuerURL` (`https://auth.<domain>`) validates the JWT `iss` claim; the `JWKSURL` controls where keys are fetched from. A Host-header-overriding HTTP transport sends `Host: auth.<domain>` on JWKS requests so Zitadel's instance router accepts them. This avoids routing JWKS fetches through Caddy (TLS termination, WAF, DNS resolution) and eliminates the need for port-443 and DNS egress rules in per-service nftables. The existing `oifname "lo" tcp dport 8085 accept` rule is sufficient only for the current single-node topology. On a 3-node topology, the JWKS URL and the per-service nftables egress rules both need to become topology-aware; the current loopback-only rule is not sufficient once Zitadel is remote.

### Dual-write pattern

Services that produce data for both real-time UX and long-term analytics use **application-level dual write**: the service writes to PostgreSQL (for live sync via ElectricSQL → TanStack DB in the browser) and to ClickHouse (for dashboards, metering, and historical queries) in the same request path. Consistency between the two stores is verified by periodic reconciliation (same pattern as billing's 6-check `Reconcile()`).

ClickHouse's `MaterializedPostgreSQL` engine was evaluated as a CDC alternative but rejected — it is experimental and carries replication-slot coupling risks on a single node. The 3-node evolution of the system should introduce NATS JetStream or Kafka + Debezium for proper CDC, replacing application-level dual write with WAL-based streaming.

### Entitlement

Billing is the entitlement layer. Services call the billing-service HTTP API to reserve credits before performing work, renew reservations during long-running operations (300s windows), and settle actual usage on completion. The billing library uses two-phase TigerBeetle transfers (pending → post/void) for crash-safe fund reservation. Metering rows in ClickHouse capture per-window cost breakdowns by grant source. Concurrent admission control is policy from PostgreSQL: `orgs.trust_tier` supplies fraud caps, `plans.quotas` supplies downward-only plan caps, and `CheckQuotas` is advisory only. Financial enforcement is TigerBeetle-backed spend caps: each billing period gets a fresh spend-cap account, `Reserve` runs a linked spend-cap probe+void with the grant reservation batch, and `Settle` posts the real spend-cap debit.

### Inbound mail

Stalwart Mail Server (v0.15.5, Rust, AGPL-3.0) provides receive-only SMTP and JMAP on the single node. Outbound email stays with Resend. Stalwart binds directly to port 25 (not proxied through Caddy) and handles its own STARTTLS with certs synced from Caddy's ACME storage. The JMAP API and built-in webmail are served via Caddy at `mail.<domain>`.

**Network path:** Internet → port 25 (Stalwart SMTP, STARTTLS) → local delivery → PostgreSQL. JMAP reads go through Caddy: `https://mail.<domain>/jmap` → `127.0.0.1:8090`.

**Storage:** PostgreSQL database `stalwart` (user `stalwart`). All four store roles (data, blob, fts, lookup) and the internal directory map to this single database. Stalwart manages its own schema — no migration files in the repo.

**Mailbox scheme:**
- `ceo@<domain>` — operator, reserved
- `<name>.agent@<domain>` — agent mailboxes (`bernoulli.agent`, `dijkstra.agent`, `lamport.agent`)

Accounts are pre-created via Stalwart's Management REST API in `seed-demo.yml --tags stalwart`. No auto-provisioning on first login.

**Authentication:** Internal directory backed by PostgreSQL. Passwords must be bcrypt-hashed before passing to the API (Stalwart stores `secrets` verbatim, verifies by prefix detection). Accounts require `roles: ["user"]` for JMAP access. Basic Auth is used for JMAP/API — Stalwart does not support `grant_type=password` on its OAuth endpoint.

**Querying mail via JMAP:**

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

**Management API (admin only, loopback):**

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

**Telemetry:** Native OTLP over gRPC to otelcol-contrib on `127.0.0.1:4317`. Traces and logs land in ClickHouse under `ServiceName = 'stalwart'`. Query with `make traces SERVICE=stalwart`.

**TLS cert lifecycle:** Self-signed placeholder generated on first deploy. A systemd timer (`stalwart-cert-sync.timer`) runs every 12 hours and copies the real ACME cert from Caddy's storage (`/caddy/certificates/`) to `/etc/stalwart/certs/`, then reloads Stalwart. First real cert appears ~2 minutes after Caddy starts (post-DNS propagation).

**Relevant files:**
- `roles/stalwart/` — Ansible role (tasks, templates, defaults, handlers)
- `roles/stalwart/templates/stalwart.toml.j2` — server config
- `roles/stalwart/templates/stalwart.nft.j2` — egress firewall
- `roles/stalwart/templates/stalwart-cert-sync.sh.j2` — cert sync script
- `roles/stalwart/tasks/dns.yml` — MX + SPF record creation
- `playbooks/seed-demo.yml` (tag: `stalwart`) — mailbox provisioning

**DNS records** (managed by Ansible):
- `A mail.<domain> → <server_ip>` (cloudflare_dns role)
- `MX <domain> → mail.<domain>` priority 10 (stalwart role dns.yml)
- `TXT <domain>` — SPF record including the mail server (stalwart role dns.yml)
- **Manual:** PTR record for `<server_ip> → mail.<domain>` (request from Latitude.sh)

## Supply Chain Management

* Git repos (including this one) are hosted on the deployed Forgejo instance at git.<domain_name>.com
* We self-host NPM via verdaccio

## Context

Key focus areas for this project

* Secure by default, above and beyond most SaaS provided options. Security must be regularly audited and verified (still working on this)
* Cheap -- the operator, when starting and operating their business. They only pay for compute and object storage which are commodity priced, not for DataDog's operating margin.
* [aspirational, not yet fully implemented] Solves genuinely difficult problems faced by businesses - Lowering a price for a product should be easy and fast: when the oeprator of the company reduces the price of a metered product, customer billing pages should update, marketing pages' pricing sections should update, emails should go out to customers, end-of-month invoices should reflect usage at both old and new prices, metering should update at a specified effective_at field, customer support agents (not yet implemented) should be able to answer questions and query safe tables to pull information about recent price changes and the customer's spend history that may have impacted them. All of this should happen seamlessly via a combination of maintaining a robust system of record and deterministic workflows.
* Observable - o11y 2.0. Logs, traces, and metrics are one thing: the Wide Event. ClickHouse can handle millions of writes per second, leverage that by instrumenting as much as possible. It's easier to reduce instrumentation that's unnecessary than it is to backfill gaps.

arch at a high level:

- We support only Ubuntu 24.04 on the bare metal box.
- vm-orchestrator is the privileged Go host daemon managing Firecracker VM lifecycle (ZFS, TAP, jailer) and aggregating guest telemetry. vm-guest-telemetry is the Zig guest agent streaming 60Hz health frames over vsock port 10790.
- Our current working bare metal box is available at `ssh ubuntu@64.34.84.75`
- Auth: Zitadel
- Payments: Stripe + TigerBeetle + PostgreSQL
- otelcol-config.yaml.j2 contains a lot of our custom otel collection config.

* You can run `make clickhouse-schemas` to read all of our ClickHouse tables, which contains a lot of useful ground truth.

* Less important but useful if editing instructions: .claude/CLAUDE.md is symlinked from AGENTS.md

## CI Architecture & Quickstart

See README.md for more -- the repo started as a CI orchestrator but has since evolved.

### 5. Query ClickHouse

Use the Makefile wrappers instead of typing the SSH and password prefix by hand. They `cd` into `src/platform/` and invoke `scripts/clickhouse.sh`, which resolves the worker from `ansible/inventory/hosts.ini` and reads the ClickHouse password from SOPS.

```bash
make inventory-check
make clickhouse-query QUERY='SHOW TABLES' DATABASE=forge_metal
make clickhouse-shell
```

OTel logs live in `default.otel_logs`, not `forge_metal.otel_logs`:

```bash
make clickhouse-query QUERY='SELECT Timestamp, Body FROM default.otel_logs ORDER BY Timestamp DESC LIMIT 10'
```

### 6. Debug with traces

`make traces` pulls recent HTTP traces and structured logs from ClickHouse in a single command:

```bash
make traces                              # Last 5 min, all services
make traces SERVICE=billing-service      # Filter to one service
make traces MINUTES=30                   # Last 30 minutes
make traces ERRORS=1                     # Errors only (4xx/5xx + ERROR/WARN logs)
make traces SERVICE=sandbox-rental ERRORS=1  # Combine filters
```

### TLS with a real domain (Cloudflare)

```bash
make setup-domain DOMAIN=anveio.com
cd src/platform/ansible && ansible-playbook playbooks/dev-single-node.yml
```

Services get subdomains automatically:

| Subdomain | Service |
|-----------|---------|
| `admin.<domain>` | ClickStack dashboard |
| `git.<domain>` | Forgejo |
| `auth.<domain>` | Zitadel |
| `mail.<domain>` | Stalwart (JMAP API + webmail) |

### Server Profile

All server software is managed by the `deploy_profile` Ansible role. It populates `/opt/forge-metal/profile/bin/` via three strategies:

- **Go binaries** (forge-metal, billing-service): built on the controller via `go build`, copied to server
- **Caddy** (with Coraza WAF plugin): built on the controller via `xcaddy`, copied to server
- **Static binaries** (ClickHouse, TigerBeetle, Zitadel, Forgejo, forgejo-runner, otelcol-contrib, containerd, Node.js, Stalwart, stalwart-cli): pinned in `src/platform/server-tools.json` with URLs and SHA256 hashes, downloaded and verified on the server
- **apt packages** (PostgreSQL 16, wireguard-tools): installed from PGDG/Ubuntu repos, symlinked into fm_bin

The only other `apt install` is `zfsutils-linux` (kernel-dependent, must match running kernel).

### Architecture

```
go build / xcaddy build   --> compile Go binaries on controller
ansible-playbook           --> download static binaries, install apt packages, configure + enable services
```


### Wide Events

Every CI job produces one denormalized row in `ci_events` with ~50 columns. No JOINs needed.

Compression codecs per column type:
- Timestamps: `DoubleDelta + ZSTD(3)`
- Durations (Int64): `Delta(8) + ZSTD(3)`
- Byte counters: `T64 + ZSTD(3)`
- Low-cardinality strings: `LowCardinality + ZSTD(3)`
- Floats: `Gorilla + ZSTD(3)`

## Ansible Playbooks

All remote orchestration is done via Ansible playbooks. Run from the `src/platform/ansible/` directory.

read the Makefile for other common task automation.

| Playbook | Description |
|----------|-------------|
| `playbooks/setup-dev.yml` | Install pinned dev tools from dev-tools.json |
| `playbooks/setup-sops.yml` | Bootstrap SOPS+Age encryption for secrets |
| `playbooks/provision.yml` | Provision bare metal via OpenTofu, generate inventory |
| `playbooks/deprovision.yml` | Destroy bare metal infrastructure, remove inventory |
| `playbooks/dev-single-node.yml` | Deploy to single node (idempotent) |
| `playbooks/site.yml` | Deploy to multi-node cluster (workers + infra) |
| `playbooks/guest-rootfs.yml` | Build guest rootfs and stage CI artifacts |
| `playbooks/hyperdx-dashboards.yml` | Sync HyperDX dashboards without full redeploy |
| `playbooks/ci-fixtures.yml` | Run CI fixture suites |
| `playbooks/ci-fixtures-pass.yml` | Run positive fixture suite |
| `playbooks/ci-fixtures-fail.yml` | Run negative fixture suite |
| `playbooks/ci-fixtures-full.yml` | Refresh artifacts, then run pass + fail suites |
| `playbooks/vm-guest-telemetry-dev.yml` | Hot-swap vm-guest-telemetry, boot + probe in Firecracker VM (~10s) |
| `playbooks/security-patch.yml` | Rolling OS security updates |
| `playbooks/mirror-update.yml` | Update and scan Verdaccio mirror |
| `playbooks/billing-reset.yml` | Exhaustively wipe TigerBeetle + billing PostgreSQL state and restart billing callers |
| `playbooks/seed-demo.yml` | Seed demo environment: human user, billing catalog, credits, mailboxes, auth verify (supports `--tags user,billing,stalwart,verify`) |

All deploy playbooks support `--tags` for targeting individual roles (e.g. `--tags caddy`, `--tags clickhouse`). Preflight checks run regardless of tag selection.

## Project Structure

```
forge-metal/                            # Monorepo root
├── go.work                             # Go workspace (all src/*/ Go modules)
├── Makefile                            # Dev commands (wraps paths into src/)
├── docs/                               # Cross-cutting architecture docs
│
│   ── Shared libraries ──────────────────────────────────────────────────
│
├── src/auth-middleware/                 # OIDC JWT validation (Go library)
│   └── go.mod                          # github.com/forge-metal/auth-middleware
├── src/billing/                        # Billing domain: Reserve/Settle/Void (Go library)
│   └── go.mod                          # github.com/forge-metal/billing
├── src/vm-orchestrator/                # Firecracker + ZFS VM orchestrator (Go library)
│   ├── go.mod                          # github.com/forge-metal/vm-orchestrator
│   ├── orchestrator.go                 # Run(JobConfig) / RunDataset(JobConfig, dataset)
│   ├── zvol.go                         # ZFS clone/destroy/snapshot/written
│   ├── network.go                      # TAP + CIDR lease allocator
│   ├── vmproto/                        # Host-guest vsock wire protocol
│   └── cmd/vm-init/                    # Guest PID 1
│
│   ── Services ──────────────────────────────────────────────────────────
│
├── src/billing-service/                # Billing HTTP API (Go/Huma)
│   ├── go.mod                          # imports: billing, auth-middleware
│   ├── cmd/billing-service/            # systemd LoadCredential= for secrets
│   ├── cmd/tb-inspect/                 # TigerBeetle account inspector
│   └── migrations/                     # Billing PostgreSQL schema
├── src/sandbox-rental-service/         # Sandbox product backend (Go/Huma) [planned]
│   ├── go.mod                          # imports: vm-orchestrator, billing (HTTP), auth-middleware
│   ├── cmd/sandbox-rental-service/     # Job orchestration, billing integration
│   └── migrations/                     # job_runs, job_logs PostgreSQL schemas
│
│   ── Frontends ─────────────────────────────────────────────────────────
│
├── src/vite-plus-monorepo/             # Vite+ workspace for frontend applications
│   ├── apps/rent-a-sandbox/            # Customer-facing sandbox product frontend
│   └── packages/ui/                    # Shared frontend UI package
│
│   ── Standalone tools ──────────────────────────────────────────────────
│
├── src/vm-guest-telemetry/             # Firecracker VM guest telemetry agent (Zig)
│   ├── build.zig
│   └── src/                            # 60Hz /proc sampler, vsock 10790 streamer
│
│   ── Platform ──────────────────────────────────────────────────────────
│
└── src/platform/                       # Infrastructure + deployment
    ├── go.mod                          # imports: vm-orchestrator (CI manager uses it)
    ├── cmd/forge-metal/                # CLI: doctor, setup-domain, CI warm/exec, fixtures
    ├── internal/ci/                    # CI domain: Warm/Exec, golden images, toolchain detection
    ├── ansible/
    │   ├── playbooks/                  # All orchestration (deploy, provision, CI, vm-guest-telemetry-dev)
    │   └── roles/                      # Flat directory — deployment is a platform concern
    │       ├── deploy_profile/         # Build + download + install all server binaries
    │       ├── base/                   # OS hardening, users, credstore, SSH
    │       ├── nftables/               # Host firewall (forge-metal-firewall.target)
    │       ├── zfs/                    # Pool creation, golden/ci datasets
    │       ├── caddy/                  # Edge proxy, TLS, WAF, route allowlist
    │       ├── postgresql/             # Shared PostgreSQL (one DB per service)
    │       ├── clickhouse/             # ClickHouse config + schema bootstrap
    │       ├── tigerbeetle/            # Financial ledger service
    │       ├── otelcol/                # OTel Collector → ClickHouse
    │       ├── billing_service/        # Billing service deploy + Zitadel auth project
    │       ├── sandbox_rental_service/ # Sandbox product deploy [planned]
    │       ├── rent_a_sandbox/         # TanStack Start frontend deploy [planned]
    │       ├── stalwart/              # Receive-only mail: SMTP + JMAP + cert sync
    │       ├── forgejo/                # Git server + CI runner
    │       ├── zitadel/                # Identity provider (OIDC)
    │       ├── hyperdx/                # Observability UI + MongoDB
    │       ├── verdaccio/              # Sealed npm registry mirror
    │       ├── firecracker/            # KVM, jailer, golden zvol, vm-orchestrator
    │       └── ...                     # guest_rootfs, ci_fixtures, wireguard, etc.
    ├── terraform/                      # Latitude.sh provisioning
    ├── scripts/                        # clickhouse.sh, build-guest-rootfs.sh, etc.
    ├── migrations/                     # ClickHouse schemas (platform-level)
    ├── server-tools.json               # Pinned server binary versions + SHA256
    └── dev-tools.json                  # Pinned dev tool versions + SHA256
```

## Assistant Contract

* Ground proposals, plans, API references, and all technical discussion in primary sources. Then, think from the perspective of the user of the system. The users of this repo will be sole operators of a single-person software company operating all services off a single bare metal box (with upgrade path to 3 for higher availability).
* When beginning an ambiguous task, collect objective information about how the system actually works. There are a lot of technologies being stitched together so its important to understand how everything connects.
* You are expected to push back on poor technical decisions. Technical decisions are poor when they couple too much to a specific workflow (e.g. hardcoding Postgres in every Firecracker VM), attempt to use technology in ways its not meant to be used (e.g. using Nix inside of a firecracker VM), or are technically infeasible or "not even wrong".
* Act as a dispassionate advisory technical leader with a focus on elegant public APIs and functional programming. 
* You may be asked series of questions. Not all questions need to be answered individually. Consider the gestalt of the discussion and take a step back and address the core question underneath the questions.
* You are not alone in this repo. Expect parallel changes in unrelated files by the user.
* This repo is currently private and serves no customers or users. There is no backwards compatibility to maintain. This means: no compatibility wrappers, no legacy shims, no temporary plumbing. All changes must be performed via a full cutover. 
* Ensure old or outdated code is deleted each time we upgrade technology, abstractions, or logic. Eliminating contradictory approaches is a high priority.
* Avoid simplifying technical explanations. Details matter and the user cares about arcane versioning issues, subtle race conditions, preventing timing attacks, optimizing GC pressure, understanding when abstractions leak and so on. Simplicity should be saved for code and architecture.

## Tool Use Contract

* When executing long-running tasks, execute them in the background and check in every 30 - 60 seconds.
* Dev tools are system-installed via `ansible-playbook playbooks/setup-dev.yml`. No `nix develop` prefix needed.
* Apply the scientific method: create a bar-raising verification protocol for your planned task *prior* to impelementing changes. The verification protocol should fail, and only then begin implementing until green.
* Avoid using one off non-syntax-aware scripts to do large parallel changes or refactors. Use subagents for that class of tasks instead as unexpected edge cases are likely and judgement is often required.

## Output Contract

* When providing a recommendation, consider different plausible options and provide a differentiated recommendation that leans towards a simpler solution that best fits the long term goal of this project.
* Speculating that your code changes work as expected is not allowed. Unit tests and successful builds are low signal and are not to be trusted. Real observability traces in ClickHouse that exercise your modified code is the only admitted proof of code task-completion. ClickHouse currently exists for the purpose of producing verifiable completion artifacts. If a new schema is needed, you are permitted to create one.
* Do not speculate about host-level causes (resource exhaustion, network issues, etc.) without evidence. Logs, traces, and host metrics are queryable in ClickHouse via `make clickhouse-query` — check them before attributing failures to environmental factors.
* Do not stop work short of verifying your changes with a live rehearsal of a playbook to execute fresh rebuild and redeploy. You have full authority to wipe databases and recreate them as needed. In fact, prefer to do that over time-consuming and tricky migrations during this early phase of development.
* The repo has a fixture flow that seeds Forgejo repos, warms their goldens, opens PRs, and waits for CI.
* When writing design documents, code comments, system architecture diagrams, API documentation, or any other kind of technical writing, ensure that the writing style targets the following audience: distinguished engineers that are experts in the relevant technologies but mostly just need information on how the system being described is different or deviates from standard practice. Avoid throat-clearing, get straight into the information.
* When editing byte-layouts, avoid piecemeal edits as that's how you end up with contradictions.

## Coding Contract

* Prefer Ansible over shell scripts, except in extreme bootstrap cases.
* Ansible playbook files must have a newline at the end. This will be caught by `ansible-lint`.
* Treat errors as data. Use tagged and structured errors to aid in control flow.
* Avoid fallbacks and defaults in Ansible code. Ansible should fail fast with useful logging.
* Remember the philosophy that tests will never be able to assert that a system works correctly. They only assert the absence of some set of bugs. Prefer fewer high-signal top-contour tests and pair happy-path tests with sad-path tests to improve the signal of both sides.
* Package management for python must be done with `uv` do not use pip or conda.
* Don't resolve failures through silent no-ops and imperative checks. Failures should be loud and signals should be followed in order to address root causes.
* PostgreSQL migrations live with the service that owns the schema (e.g. `src/billing-service/migrations/`), one database per service; the platform provisions databases and roles, the service's Ansible role applies its migrations.
* ClickHouse inserts must use `batch.AppendStruct` with `ch:"column_name"` struct tags. Never use positional `batch.Append` — it silently corrupts data when columns are added or reordered.
* ClickHouse queries must pass dynamic values (including Map keys) through driver parameter binding (`$1`, `$2`, ...); never interpolate values into query strings with `fmt.Sprintf` — use `arrayElement(map_col, $N)` instead of `map_col['{interpolated}']`.
* ClickHouse schema design: ORDER BY columns are sorted on disk and control compression — order keys by ascending cardinality (low-cardinality columns first). Avoid `Nullable` (it adds a hidden UInt8 column per row); use empty-value defaults instead. Use `LowCardinality(String)` for columns with fewer than ~10k distinct values. Use the smallest sufficient integer type (UInt8 over Int32 when the range fits).
