# Founder Workflows

## Deployment Surface

Use `site.yml` for normal platform iteration. The current single-node inventory
places the same host in both `workers` and `infra`; a multi-node deployment uses
the same playbook with distinct hosts in those groups. The playbook rebuilds the
server profile, pushes it to the current site, and reapplies the Ansible roles
without wiping host state. Target individual roles with `--tags` (e.g.
`--tags caddy` or `--tags grafana`).

Grafana dashboards are provisioned by the `grafana` role and exercised by
`make grafana-proof`; no separate dashboard-sync playbook exists.

After `make grafana-proof`, verify ClickHouse evidence with:

```sql
SELECT event_time, type, initial_user, query
FROM system.query_log
WHERE event_time >= now() - INTERVAL 15 MINUTE
  AND query LIKE '%verself:grafana verify=%'
ORDER BY event_time;
```

## ClickHouse Access

ClickHouse is not exposed for unauthenticated remote access. Use the repo wrapper so you do not have to manually prefix SSH, the remote client-certificate config, or the stable worker client path each time.

The wrapper resolves the worker from `ansible/inventory/hosts.ini` and invokes `/opt/verself/profile/bin/clickhouse-client` on the worker as the SPIFFE-authenticated `clickhouse_operator` user. Do not hardcode a `/nix/store/...` path.

Use it from the repo root:

```bash
make clickhouse-query QUERY='SHOW TABLES' DATABASE=verself
./src/platform/scripts/clickhouse.sh --database verself --query 'SHOW TABLES'
```

Interactive ClickHouse shells are intentionally unsupported. Use replayable
`make clickhouse-query` invocations instead.

## Observe

Use `make observe` before raw ClickHouse when the question starts with
"what telemetry exists?" or "which query should I run?" The no-arg output is a
discoverability index, not a recency dashboard.

```bash
make observe
make observe WHAT=queries
make observe WHAT=catalog SIGNAL=metrics
make observe WHAT=catalog SIGNAL=logs SERVICE=observe
make observe WHAT=describe METRIC=system.cpu.time
make observe WHAT=describe SERVICE=observe
make observe WHAT=metric METRIC=system.cpu.time GROUP_BY=state FORMAT=json
```

Operational recent-window queries are explicit:

```bash
make observe WHAT=errors MINUTES=15
make observe WHAT=logs SERVICE=observe FIELD=query_id MINUTES=15
make observe WHAT=service SERVICE=billing-service MINUTES=15
make observe WHAT=http STATUS_MIN=400 MINUTES=15
make observe WHAT=deploy RUN_KEY=<deploy-run-key>
make observe WHAT=trace TRACE_ID=<trace-id>
make observe WHAT=workload-identity
```

Every ClickHouse-backed observe query emits an `observe` trace and a
`clickhouse.query` span with `observe.query_id`,
`observe.query_family`, `clickhouse.query_id`, and
`clickhouse.query_sha256` attributes.

**Target state.** `WHAT=workload-identity` is the operator surface for the
SPIFFE/SPIRE control loop: SPIRE server and bundle endpoint state, agent;
Workload API reachability; desired-vs-live registrations; SVID TTLs; mTLS
edges; JWT-SVID OpenBao login results; and governance rows grouped by
`actor_spiffe_id`. The contract is declared in
[`docs/architecture/workload-identity.md`](workload-identity.md).
Implementation follows the in-flight SPIRE deploy commit.

The current database layout is:

- `verself.job_events`
- `verself.job_logs`
- `verself.metering`
- `default.otel_logs`
- `default.otel_traces`
- `default.otel_metrics_gauge`
- `default.otel_metrics_sum`
- `default.otel_metrics_histogram`

The OTel tables live in `default`, not in an `otel` database.

## PostgreSQL Access

PostgreSQL is not exposed directly. Use the repo wrapper so you do not have to
manually prefix SSH, the admin password, or the stable worker `psql` path.

Use it from the repo root:

```bash
make pg-list
make pg-query DB=billing QUERY='SELECT count(*) FROM orgs'
make pg-query DB=billing QUERY='SELECT count(*) FROM billing_events'
make pg-query DB=sandbox_rental QUERY='SELECT count(*) FROM executions'
make pg-shell DB=billing
```

Current service-owned database names:

- `billing`: billing-service PostgreSQL state, River tables, billing events,
  contracts, cycles, grants, windows, finalizations, and document artifacts.
- `sandbox_rental`: sandbox-rental-service product control-plane state.
- `identity_service`: identity-service state.
- `mailbox_service`: mailbox-service state.
- `frontend_auth`: TanStack Start server-owned OAuth session state.
- `storefront`: reserved billing-owned database for future storefront surfaces.

The billing product ID remains `sandbox`. Do not use `DB=sandbox`; that was a
legacy billing database name and should not exist on a current deployment.

Use `guest-rootfs.yml` when the guest kernel, rootfs, or staged Firecracker guest artifacts changed. It builds the Bazel guest artifact bundle, rebuilds the rootfs, and restages the Firecracker guest artifacts without touching the rest of the platform.

## Telemetry Proof

Deploy playbook telemetry smoke probes:

```bash
make telemetry-proof           # success path: ansible + service correlation
make telemetry-proof-fail      # sad path: assert Error spans are emitted
```

## Company Site Proof

End-to-end canary for the Guardian Intelligence company site at
`company_domain` (guardianintelligence.org). Walks every IA route in a
headless browser, hits the dynamic OG generator, downloads the press
brand kit, and asserts the corresponding `company.*` spans land in
ClickHouse within 60 seconds of emit.

```bash
make company-proof
```

Assertions (all on `default.otel_traces` with `ServiceName = 'company-web'`):

- `SpanName = 'company.route_view'` emitted for every walked route
  (/, /design, /letters, /solutions, /company, /careers, /press,
  /changelog, /contact, /letters/<seeded-slug>).
- `SpanName = 'company.landing.hero_view'` emitted once per landing
  load.
- `SpanName = 'company.og.render'` emitted with
  `SpanAttributes['og.voice_pass'] = 'true'` for each OG card the
  canary visits.
- `SpanName = 'company.press.kit_download'` emitted when the canary
  clicks the brand kit link on `/press`.

## Deploy Correlation Model

Deterministic deploy correlation:

- `deploy_run_key`: `YYYY-MM-DD.<counter>@<controller-host>`
- `deploy_id`: UUIDv5 over `verself:${deploy_run_key}`
- `scripts/deploy_identity.sh` exports `TRACEPARENT=00-<deploy_id_hex>-<stable>-01` and `OTEL_RESOURCE_ATTRIBUTES=verself.deploy_id=…,verself.deploy_run_key=…,…`, anchoring the upstream `community.general.opentelemetry` Ansible callback and `verself_uri` probes to the same trace-id.
- The otelcol `transform/ansible_spans` processor renames upstream-emitted `<playbook>.yml` / `<task name>` spans to `ansible.playbook` / `ansible.task` and mirrors `verself.*` from `ResourceAttributes` onto `SpanAttributes`, so the same query shape (`SpanAttributes['verself.deploy_id']`) works for both ansible and service spans.
- Service spans pick up `verself.*` via the `verselfotel` baggage span processor (`src/otel/otel.go`), which projects every W3C baggage member with the `verself.` prefix onto spans it sees.

## TLS with a Real Domain (Cloudflare)

```bash
cd src/platform/ansible
sops group_vars/all/secrets.sops.yml   # set verself_domain, company_domain, and cloudflare_api_token
ansible-playbook playbooks/site.yml
```

The product apex (`verself_domain`) serves platform docs/policy. Product
services get subdomains configured via Cloudflare:

| Subdomain | Service |
|-----------|---------|
| `console.<domain>` | Authenticated Verself product console |
| `<service>.api.<domain>` | Public service APIs (`billing.api`, `sandbox.api`, `identity.api`, etc.) |
| `dashboard.<domain>` | Grafana |
| `git.<domain>` | Forgejo |
| `auth.<domain>` | Zitadel |
| `mail.<domain>` | Stalwart JMAP/SMTP protocol surface |

See [`public-origins.md`](public-origins.md) for the browser/API/protocol
origin split.

## Server Profile

All server software is managed by the `deploy_profile` Ansible role. It populates `/opt/verself/profile/bin/` via three strategies:

- **Go service binaries** (billing-service, sandbox-rental-service, mailbox-service): built on the controller via `go build`, copied to server.
- **Caddy** (with Coraza WAF plugin): built on the controller via `xcaddy`, copied to server.
- **Static binaries** (ClickHouse, TigerBeetle, Zitadel, Forgejo, Grafana, grafana-clickhouse-datasource plugin, otelcol-contrib, containerd, Node.js, Stalwart, stalwart-cli): pinned in `src/platform/topology/catalog/versions.cue`, rendered to generated topology catalog vars, downloaded and verified on the server.
- **apt packages** (PostgreSQL 16, wireguard-tools): installed from PGDG/Ubuntu repos, symlinked into `verself_bin`.

The only other `apt install` is `zfsutils-linux` (kernel-dependent, must match the running kernel).

## Ansible Playbooks

All remote orchestration runs via Ansible playbooks from `src/platform/ansible/`.

| Playbook | Description |
|----------|-------------|
| `playbooks/setup-dev.yml` | Install dev tools from the generated topology catalog vars. |
| `playbooks/setup-sops.yml` | Bootstrap SOPS+Age encryption for secrets. |
| `playbooks/provision.yml` | Provision bare metal via OpenTofu, generate inventory. |
| `playbooks/deprovision.yml` | Destroy bare-metal infrastructure, remove inventory. |
| `playbooks/site.yml` | Canonical idempotent deploy for the current inventory topology. |
| `playbooks/guest-rootfs.yml` | Build guest rootfs and stage Firecracker guest artifacts. |
| `playbooks/observability-smoke.yml` | Minimal smoke probe used by `telemetry-proof` (`debug/assert` + `verself_uri`). |
| `playbooks/vm-guest-telemetry-dev.yml` | Hot-swap `vm-guest-telemetry`, boot + probe in Firecracker VM (~10s). |
| `playbooks/security-patch.yml` | Rolling OS security updates. |
| `playbooks/billing-reset.yml` | Exhaustively wipe TigerBeetle + billing PostgreSQL database `billing` and restart billing callers. |
| `playbooks/seed-system.yml` | Seed the platform tenant plus Acme tenant, billing, mailboxes, and auth verify (supports `--tags identity,billing,stalwart,verify,dev-oidc`). |

The site deploy supports `--tags` for targeting individual roles (e.g. `--tags caddy`, `--tags clickhouse`). Global preflight checks run regardless of tag selection; Firecracker guest artifact preflight runs with the `firecracker` tag.
