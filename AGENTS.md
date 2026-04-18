# forge-metal

<!--
Where sections seem to disagree, remember: <system_context> describes the system
as it exists today, and <product_direction> describes where it is headed.
Proposals should respect both, not collapse one into the other.
-->

<repo_overview>

Polyglot Repo:

src/apps/viteplus-monorepo -- TypeScript (Vite Plus + TanStack Start/DB/Query/Router)
go.work -- All of those are Golang
src/vm-orchestrator -- Go host daemon for Firecracker, ZFS, TAP networking, jailer lifecycle, vm-bridge, and gRPC control.
src/vm-guest-telemetry -- Zig guest agent that streams health samples from Firecracker VMs.

Run `make pg-list` to list the pg databases.

</repo_overview>

<product_policy>
See src/viteplus-monorepo/apps/platform/src/routes/policy for public commitments regarding:

* Data Processing
* Acceptable Use
* Security
* SLA
* Data Retention

</product_policy>
<product_direction>

## Direction
* Each project under src/ should be treated as it's own public open-source repo
* vm-orchestrator (Go daemon) is the single privileged host process that manages Firecracker VMs: ZFS clones/checkpoints, TAP networking, jailer lifecycle, vm-bridge control, and guest telemetry aggregation. It exposes a gRPC API over a Unix socket for service callers. vm-guest-telemetry (Zig) is the minimal guest agent streaming 60Hz health samples over vsock. sandbox-rental-service is the product control plane layered on that substrate.
* Runtime product services must never receive privileged host access. All ZFS, Firecracker, TAP, jailer, `/dev/kvm`, and `/dev/zvol` operations go through vm-orchestrator; services carry policy-checked refs over the orchestrator API, not host paths, dataset names, device paths, or privileged CLIs.
* Broad direction: every service should do the following:
        1. Be designed for use by customers in a multi-tenant, organization-based fashion and integrated into our policy and billing abstractions.
        2. Be designed such that we are the principal customers (dogfooding, essentially). We go through the same policy and billing abstractions, except our usage is unlimited and our bill at invoice time nets to 0 after applying an adjustment. Currently not doing Mail because that's pretty simple but it would be good to dogfood that too.
        NOTE: this philosophy is not yet upheld today, but it's something to keep in mind as we upgrade the codebase.

* Product IAM direction: Zitadel owns identity, organizations, users, OAuth/OIDC, project roles, and role assignments; Forge Metal owns the product policy model; each Go service owns and enforces its operation catalog. The platform should ship working default role bundles and policy documents, then expose customer editing through a constrained Forge Metal organization console rather than requiring operators to hand-author IAM documents. See src/platform/docs/identity-and-iam.md.
* A core tenet goal is for us to start dogfooding our own Forgejo and running our own CI, establishing a main, beta, gamma, and different preview environments of the entire system for different dev branches -- with automatic promotions: dev branches merge to gamma, gamma bakes and runs more expensive automation tests and promots to beta. Beta may see some private invite-only users and have manual or time-gated promotion to main. Dev branches are accesible only by the operator and their agent.
* in a similar vein we want to start defining e2e canaries of our own infrastructure as repeatable/scheduled workloads

### Sandbox Runtime Products

sandbox-rental-service sells isolated compute products built from the same
vm-orchestrator + vm-bridge + vm-guest-telemetry substrate. Firecracker provides
the isolation boundary; ZFS zvols/checkpoints provide fast clone, restore, and
persistent filesystem semantics; billing, IAM, logs, traces, metrics, and
checkpoint policy stay in service-owned control-plane state.

The three customer-facing products are:

1. A Blacksmith-like clean-room Actions runner: customers install a Forge Metal
   GitHub Action or Forgejo Actions equivalent and run repository workflows on
   Forge Metal Firecracker VMs for a 2-10x CI speedup.
2. Arbitrary workload execution: customers define Lambda-like workloads with a
   persistent filesystem, first invoked manually and later schedulable as
   minimum-60-second loops.
3. Long-running VMs: customers run persistent VM sessions on the same isolation,
   telemetry, billing, and checkpoint substrate.

Dogfood all three through the same org, IAM, billing, telemetry, and checkpoint
paths customers use. Internal usage should be unlimited by entitlement and net
to zero at invoice time via adjustment, not by bypassing product control planes.

</product_direction>

<system_context>

## Deployment Topology

This is a deploy-together system. Single-node is the default deployment. Everything runs on one box with no replication. Adding two more nodes (3 total) enables TigerBeetle consensus replication, ClickHouse ReplicatedMergeTree, Postgres streaming replication, and cross-node health monitoring with external paging. The single-node path is what we're currently working on and we will provide in the future a path to seamlessly upgrade to a three node topology with Netbird as the overlay.

There are three safety rings:

Internet-Exposed: Frontend TanStack (src/viteplus-monorepo/apps/*) + Golang Services (src/sandbox-rental-service, src/mailbox service, src/billing-service's webhook handler). Security hardened through nftables as much as possible (always improving) + Forgejo + Grafana
Private Subnet/Linux Userspace: Golang internal services (billing-service), Databases (PG, ClickHouse, TigerBeetle), Self-Hosted Platform Stuff (Zitadel, Stalwart)
Linux Root: ZFS, src/vm-orchestrator

Hard product design requirement: everything must be self-hosted.

Exceptions:

Optional - Backups (Supported Provider: None, but Backblaze B2, Cloudflare R2, and AWS S3 support planned) (will be done through `zfs send`) [Backups not yet implemented]
Required - Domain Registrar (Supported Provider: Cloudflare only for now)
Required - Compute Provider (Supported Provider: Latitude.sh only for now)
Required - Email Delivery (Supported Provider: Resend only for now, inbound done via Stalwart)
Required - Payments, Dunning, Tax, Invoices, Payment Method Managing (Supported Provider: Stripe)

## Service Architecture

See docs/architecture/service-architecture.md

See src/platform/ansible/group_vars/all/services.yml for port assignments; run `make services-doctor` to cross-check the declared map against live listeners on the box (supports `FORMAT=json|nftables`).

Secrets are SOPS-encrypted in `group_vars/all/secrets.sops.yml`, written by each service's Ansible role to `/etc/credstore/{service}/` (root-owned, service-group-readable), and loaded at runtime via systemd `LoadCredential=` into `$CREDENTIALS_DIRECTORY`.

Go services are written with the Huma v2 framework (https://pkg.go.dev/github.com/danielgtaylor/huma/v2) to support automatic generation of clients via OpenAPI v3.1. Do not write custom clients for go services; generate them from an OpenAPI specification. Each service commits both an OpenAPI 3.0 spec (for Go client generation via oapi-codegen) and a 3.1 spec (for TypeScript client + Valibot validator generation via @hey-api/openapi-ts).

Shared cross-service DTO wire language lives in `src/apiwire`; use it for Huma boundary DTOs and generated-client contracts instead of service-local 64-bit JSON encodings.

When writing Huma services, please review the reference documentation https://pkg.go.dev/github.com/danielgtaylor/huma/v2#section-documentation

### Auth model

Zitadel is the sole IdP. All Go services import `src/auth-middleware/` which validates JWTs against Zitadel's JWKS endpoint (cached, local crypto after first fetch). Identity (subject, org ID, roles, email) is extracted from token claims and attached to request context.

Organization administration and product IAM are Forge Metal product concerns layered on top of Zitadel identity. Services define and enforce operation permissions; Zitadel stores identity, organization, project role, and role-assignment state; customer-editable policy documents belong to Forge Metal and should be managed through a first-party organization console or shared product widget. See src/platform/docs/identity-and-iam.md.

Auth at the web application level is treated *only* as a UX concern. Authentication and authorization is performed by services validating JWTs and calling out to Zitadel, and sometimes at the DB level where possible. Any violation of this principle is to be treated as a critical security concern and should be raised + fixed.

TanStack Start frontends use server-owned OAuth web sessions. The frontend server performs the Zitadel code exchange, stores access/refresh tokens server-side in the `frontend_auth_sessions` PostgreSQL database, and issues an HTTP-only session cookie to the browser. Server functions, loaders, and `beforeLoad` read that session and forward bearer tokens to Go services from the server side. Browser code does not read or persist Zitadel bearer tokens.

Social login (Google/GitHub/Microsoft/Apple), MFA, and passkeys remain Zitadel-side configuration. Go services remain the security boundary for API authorization; frontend `beforeLoad` checks are for SSR gating and UX, not the final enforcement layer.

**Single-node JWKS fetch path:** On a single bare-metal node, Go services fetch JWKS directly from Zitadel's loopback address (`http://127.0.0.1:8085/oauth/v2/keys`) using `oidc.ProviderConfig` with a split issuer/JWKS URL. The `IssuerURL` (`https://auth.<domain>`) validates the JWT `iss` claim; the `JWKSURL` controls where keys are fetched from. A Host-header-overriding HTTP transport sends `Host: auth.<domain>` on JWKS requests so Zitadel's instance router accepts them. This avoids routing JWKS fetches through Caddy (TLS termination, WAF, DNS resolution) and eliminates the need for port-443 and DNS egress rules in per-service nftables. The existing `oifname "lo" tcp dport 8085 accept` rule is sufficient only for the current single-node topology. On a 3-node topology, the JWKS URL and the per-service nftables egress rules both need to become topology-aware; the current loopback-only rule is not sufficient once Zitadel is remote.

### Dual-write pattern

Services that produce data for both real-time UX and long-term analytics use **application-level dual write**: the service writes to PostgreSQL (for live sync via ElectricSQL → TanStack DB in the browser) and to ClickHouse (for dashboards, metering, and historical queries) in the same request path. Consistency between the two stores is verified by periodic reconciliation (same pattern as billing's 6-check `Reconcile()`).

ClickHouse's `MaterializedPostgreSQL` engine was evaluated as a CDC alternative but rejected — it is experimental and carries replication-slot coupling risks on a single node. The 3-node evolution of the system should introduce NATS JetStream or Kafka + Debezium for proper CDC, replacing application-level dual write with WAL-based streaming.

### Billing

The repo strives to solve billing for online businesses.  Billing and sandbox spawning are the two core focuses of this repo. Read src/billing-service/docs/billing-architecture.md for more detail. Note that not all aspects have been implemented.

The specific billing system may best be described as "credit-based subscription billing with entitlements" or "prepaid + metered hybrid".

Key use cases:

* Selling monthly subscriptions which grant entitlements like credits, access to certain digital goods, software licenses, priority lanes
* Credits are consumed via metering events published by services. E.g. token inference, vCPU/RAM/Disk/Network usage, build minutes

### Inbound mail

Self-hosted inbound mail is done via Stalwart. See src/mailbox-service/docs/inbound-mail.md for more.

## Supply Chain Management

* Git repos (including this one) are hosted on the deployed Forgejo instance at git.<domain_name>.com
* We self-host an NPM mirror via Verdaccio

## Context

* Avoid CLIs. Things talk to each other over HTTP. 

Key focus areas for this project

* Secure by default, above and beyond most SaaS provided options. Security must be regularly audited and verified (still working on this)
* Cheap -- the operator, when starting and operating their business. They only pay for compute and object storage which are commodity priced, not for DataDog's operating margin.
* [aspirational, not yet fully implemented] Forge Metal solves the hard problems faced by new businesses. Lowering a price for a product, for example, should be easy and fast: when the operator of the company reduces the price of a metered product, customer billing pages should update, marketing pages' pricing sections should update, emails should go out to customers, end-of-month invoices should reflect usage at both old and new prices, metering should update at a specified effective_at field, customer support agents (not yet implemented) should be able to answer questions and query safe tables to pull information about recent price changes and the customer's spend history that may have impacted them. All of this should happen seamlessly via a combination of maintaining a robust system of record and deterministic workflows.
* Observable - o11y 2.0. Logs, traces, and metrics are one thing: the Wide Event. ClickHouse can handle millions of writes per second, leverage that by instrumenting as much as possible. It's easier to reduce instrumentation that's unnecessary than it is to backfill gaps.

arch at a high level:

- We support only Ubuntu 24.04 on the bare metal box.
- vm-orchestrator is the privileged Go host daemon managing Firecracker VM lifecycle (ZFS, TAP, jailer) and aggregating guest telemetry. vm-guest-telemetry is the Zig guest agent streaming 60Hz health frames over vsock port 10790.
- Our current working bare metal box is available at `ssh ubuntu@64.34.84.75`
- Auth: Zitadel. Everything uses Zitadel for auth except for Stalwart which has a separate auth for JMAP interaction.
- Payments: Stripe + TigerBeetle + PostgreSQL
- otelcol-config.yaml.j2 contains a lot of our custom otel collection config.

* You can run `make observe 

* You can run `make clickhouse-schemas` to read all of our ClickHouse tables, which contains a lot of useful ground truth. Prefer 

* Less important but useful if editing instructions: .claude/CLAUDE.md is symlinked from AGENTS.md

</system_context>

<operational_runbook>

## CI Architecture & Quickstart

See README.md for more -- the repo started as a CI orchestrator but has since evolved.

### Query ClickHouse

Use the Makefile wrappers instead of typing the SSH and password prefix by hand. They `cd` into `src/platform/` and invoke `scripts/clickhouse.sh`, which resolves the worker from `ansible/inventory/hosts.ini` and reads the ClickHouse password from SOPS.

```bash
make inventory-check
make clickhouse-query QUERY='SHOW TABLES' DATABASE=forge_metal
```

OTel logs live in `default.otel_logs`, not `forge_metal.otel_logs`:

```bash
make clickhouse-query QUERY='SELECT Timestamp, Body FROM default.otel_logs ORDER BY Timestamp DESC LIMIT 10'
```

### Debug with observe

`make observe` is the blessed operator query surface for ClickHouse-backed telemetry:

```bash
make observe WHAT=catalog
make observe WHAT=metric METRIC=system.cpu.time
make observe WHAT=service SERVICE=billing-service
make observe WHAT=service SERVICE=sandbox-rental-service ERRORS=1
make observe WHAT=errors
make observe WHAT=mail
make observe WHAT=deploy
```

Use `make clickhouse-query` only when the observe surface does not yet cover the question. Interactive ClickHouse shells are intentionally unsupported because agent workflows need replayable commands.

Deploy playbook telemetry smoke probes remain:

```bash
make telemetry-proof           # success path: ansible + service correlation
make telemetry-proof-fail      # sad path: assert Error spans are emitted
```

Deterministic deploy correlation model:

- `deploy_run_key`: `YYYY-MM-DD.<counter>@<controller-host>`
- `deploy_id`: UUIDv5 over `forge-metal:${deploy_run_key}`
- `scripts/deploy_identity.sh` exports `TRACEPARENT=00-<deploy_id_hex>-<stable>-01` and `OTEL_RESOURCE_ATTRIBUTES=forge_metal.deploy_id=…,forge_metal.deploy_run_key=…,…`, anchoring the upstream `community.general.opentelemetry` Ansible callback and `fm_uri` probes to the same trace-id
- The otelcol `transform/ansible_spans` processor renames upstream-emitted `<playbook>.yml` / `<task name>` spans to `ansible.playbook` / `ansible.task` and mirrors `forge_metal.*` from `ResourceAttributes` onto `SpanAttributes`, so the same query shape (`SpanAttributes['forge_metal.deploy_id']`) works for both ansible and service spans
- Service spans pick up `forge_metal.*` via the `fmotel` baggage span processor (`src/otel/otel.go`), which projects every W3C baggage member with the `forge_metal.` prefix onto spans it sees

### TLS with a real domain (Cloudflare)

```bash
cd src/platform/ansible
sops group_vars/all/secrets.sops.yml # set forge_metal_domain and cloudflare_api_token
ansible-playbook playbooks/dev-single-node.yml
```

Services get subdomains configured via Cloudflare:

| Subdomain | Service |
|-----------|---------|
| `dashboard.<domain>` | Grafana |
| `git.<domain>` | Forgejo |
| `auth.<domain>` | Zitadel |
| `mail.<domain>` | Stalwart (JMAP API + webmail) |

### Server Profile

All server software is managed by the `deploy_profile` Ansible role. It populates `/opt/forge-metal/profile/bin/` via three strategies:

- **Go service binaries** (billing-service, sandbox-rental-service, mailbox-service): built on the controller via `go build`, copied to server
- **Caddy** (with Coraza WAF plugin): built on the controller via `xcaddy`, copied to server
- **Static binaries** (ClickHouse, TigerBeetle, Zitadel, Forgejo, Grafana, grafana-clickhouse-datasource plugin, otelcol-contrib, containerd, Node.js, Stalwart, stalwart-cli): pinned in `src/platform/server-tools.json` with URLs and SHA256 hashes, downloaded and verified on the server
- **apt packages** (PostgreSQL 16, wireguard-tools): installed from PGDG/Ubuntu repos, symlinked into fm_bin

The only other `apt install` is `zfsutils-linux` (kernel-dependent, must match running kernel).

## Ansible Playbooks

All remote orchestration is done via Ansible playbooks. Run from the `src/platform/ansible/` directory.

Read the Makefile for other common task automation.

| Playbook | Description |
|----------|-------------|
| `playbooks/setup-dev.yml` | Install pinned dev tools from dev-tools.json |
| `playbooks/setup-sops.yml` | Bootstrap SOPS+Age encryption for secrets |
| `playbooks/provision.yml` | Provision bare metal via OpenTofu, generate inventory |
| `playbooks/deprovision.yml` | Destroy bare metal infrastructure, remove inventory |
| `playbooks/dev-single-node.yml` | Deploy to single node (idempotent) |
| `playbooks/site.yml` | Deploy to multi-node cluster (workers + infra) |
| `playbooks/guest-rootfs.yml` | Build guest rootfs and stage Firecracker guest artifacts |
| `playbooks/observability-smoke.yml` | Minimal smoke probe used by telemetry-proof (`debug/assert + fm_uri`) |
| `playbooks/vm-guest-telemetry-dev.yml` | Hot-swap vm-guest-telemetry, boot + probe in Firecracker VM (~10s) |
| `playbooks/security-patch.yml` | Rolling OS security updates |
| `playbooks/billing-reset.yml` | Exhaustively wipe TigerBeetle + billing PostgreSQL database `billing` and restart billing callers |
| `playbooks/seed-system.yml` | Seed the platform tenant plus Acme tenant, billing, mailboxes, and auth verify (supports `--tags identity,billing,stalwart,verify,dev-oidc`) |

All deploy playbooks support `--tags` for targeting individual roles (e.g. `--tags caddy`, `--tags clickhouse`). Preflight checks run regardless of tag selection.

## Directory Structure

See docs/architecture/directory-structure.md to understand the project's directory structure

## Architecture Docs

Architecture documents live with the service they describe:

* Inbound mail (Stalwart, mailbox-service boundary, auth, storage): `src/mailbox-service/docs/inbound-mail.md`
* Firecracker VM networking (TAP allocator, host service plane, nftables): `src/vm-orchestrator/docs/firecracker-vm-networking.md`
* Wire contracts (apiwire DTO patterns, numeric safety, generated contract gate): `src/apiwire/docs/wire-contracts.md`
* VM execution control plane (sandbox-rental-service ↔ vm-orchestrator split, attempt state machine, billing windows): `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
* Identity and IAM direction (Zitadel ↔ Forge Metal policy split, org console, invariants): `src/platform/docs/identity-and-iam.md`
* Secrets service direction (identity model, OIDC provider role, resource model, billing): `src/platform/docs/secrets-service.md`

</operational_runbook>

<agent_contract>

<general_conduct>

## Assistant Contract

* Ground proposals, plans, API references, and all technical discussion in primary sources. Then, think from the perspective of the user of the system. The user is a non-technical startup founder -- a sole operator of a small software company operating all services off a single bare metal box (with upgrade path to 3-node k3s for higher availability and additional capabilities).
* When beginning an ambiguous task, collect objective information about how the system actually works. There are a lot of technologies being stitched together so it's important to understand how everything connects.
* Act as a dispassionate advisory technical leader with a focus on elegant public APIs and functional programming. 
* You are not alone in this repo. Expect parallel changes in unrelated files by the user. Leave them alone (don't stash them) and continue with your work.
* This repo is currently private and serves no customers or users. There is no backwards compatibility to maintain. This means: no compatibility wrappers, no legacy shims, no temporary plumbing. All changes must be performed via a full cutover. 
* Ensure old or outdated code is deleted each time we upgrade technology, abstractions, or logic. Eliminating contradictory approaches is a high priority.
* When it comes to technical decisions, setails matter and the user cares about things like arcane versioning issues, subtle race conditions, preventing security issues such as timing attack vulnerability, optimizing GC pressure, understanding when abstractions leak. Simplicity should be saved for code and architecture.
* Some directories have their own AGENTS.md file. When working inside those directories, please read them as they contain juicy context.
* Edit beyond what you intded as a result of runting linters/formatters are expected. You don't have to worry about them.
* When in doubt, use the industry standard pattern. Pagination, idempotency, rate limiting, OpenAPI, OpenTelemetry, state machines -- these and basically everything else are all solved problems with boring and battle-tested solutions. Don't reinvent the wheel. The one piece of genuinely novel technology in this repo is ZFS + Firecracker for customer workloads. Everything else is tried-and-tested FOSS.
* Makefile, README.md files and AGENTS.md files, schema migration files, and openapi 3.1 yml files are high signal per token. Prefer to read them directly and avoid having them be summarized by a subagent as important detail may be lost.
* Do not provide time estimates.

</general_conduct>

<tool_use>

## Tool Use Contract

* When executing long-running tasks, execute them in the background and check in every 30 - 60 seconds.
* Dev tools are system-installed via `ansible-playbook playbooks/setup-dev.yml`. No `nix develop` prefix needed.
* Apply the scientific method: create a bar-raising verification protocol for your planned task *prior* to implementing changes. The verification protocol should fail, and only then begin implementing until green.
* Avoid using one off non-syntax-aware scripts to do large parallel changes or refactors. Use subagents for that class of tasks instead as unexpected edge cases are likely and judgement is often required.
* `make tidy` formats go/typescript code.

</tool_use>

<output>

## Output Contract

* When providing a recommendation, consider different plausible options and provide a differentiated recommendation that leans towards a simpler solution that best fits the long term goal of this project.
* Speculating that your code changes work as expected is not allowed. Unit tests and successful builds are low signal and are not to be trusted. Real observability traces in ClickHouse that exercise your modified code is the only admitted proof of code task-completion. ClickHouse currently exists for the purpose of producing verifiable completion artifacts. If a new schema is needed, you are permitted to create one.
* Do not speculate without evidence. Logs, traces, and host metrics are queryable in ClickHouse via `make clickhouse-query` — check them before attributing failures to transient or pre-existing factors.
* Do not stop work short of verifying your changes with a live rehearsal of a playbook to execute fresh rebuild and redeploy. You have full authority to wipe databases and recreate them as needed. In fact, prefer to do that over time-consuming and tricky migrations during this early phase of development.
* The repo has a fixture flow that seeds Forgejo repos, submits direct VM executions through sandbox-rental-service, and verifies ClickHouse evidence.
* When writing design documents, code comments, system architecture diagrams, API documentation, or any other kind of technical writing, ensure that the writing style targets the following audience: distinguished engineers that are experts in the relevant technologies but mostly just need information on how the system being described is different or deviates from standard practice. Avoid throat-clearing around current status, "why this is important", date headers, "who this is for" etc, get straight into the information.
* Destructive commands like `git restore`, `git checkout -- <file>`, `rm -rf` will be blocked.

</output>

<coding>

## Coding Contract

* When you run into a footgun, leave a comment around the code (no more than a sentence) explaining the footgun and how the code works around it.
* Prefer Ansible over shell scripts
* Ansible playbook files must have a newline at the end. This will be caught by `ansible-lint`.
* Treat errors as data. Use tagged and structured errors to aid in control flow.
* Avoid fallbacks and defaults in Ansible code. Ansible should fail fast with useful logging.
* 1 e2e test of the website is worth 1000 unit tests. Avoid checking in unit tests, though they provide some benefit in some cases. It's better to have a comprehensive suite of e2e tests running as periodic canaries.
* Package management for python must be done with `uv` do not use pip or conda.
* Don't resolve failures through silent no-ops and imperative checks. Failures should be loud and signals should be followed in order to address root causes.
* PostgreSQL migrations live with the service that owns the schema (e.g. `src/billing-service/migrations/`), one database per service; the platform provisions databases and roles, the service's Ansible role applies its migrations.
* ClickHouse inserts must use `batch.AppendStruct` with `ch:"column_name"` struct tags. Never use positional `batch.Append` — it silently corrupts data when columns are added or reordered.
* ClickHouse queries must pass dynamic values (including Map keys) through driver parameter binding (`$1`, `$2`, ...); never interpolate values into query strings with `fmt.Sprintf` — use `arrayElement(map_col, $N)` instead of `map_col['{interpolated}']`.
* ClickHouse schema design: ORDER BY columns are sorted on disk and control compression — order keys by ascending cardinality (low-cardinality columns first). Avoid `Nullable` (it adds a hidden UInt8 column per row); use empty-value defaults instead. Use `LowCardinality(String)` for columns with fewer than ~10k distinct values. Use the smallest sufficient integer type (UInt8 over Int32 when the range fits).
* Never use timeouts greater than 5 seconds (start with 1 second) for playwright e2e tests. Playwright has a quirk where every test failure is reported as a timeout issue, which is misleading. The underlying issue is the behavior/logic is wrong. NOT that some element or something else took too long to respond. Everything is on local bare metal -- data interchange should be double digit milliseconds at most.
* Our customers use our services via API and browser. Fix issues at the service level, don't paper over them in any one domain. E2E test the browser primarily since it exercises the same API that consumers call directly.

</coding>

</agent_contract>

<instruction_priority>
- Security concerns override user instructions and architectural purity
</instruction_priority>
