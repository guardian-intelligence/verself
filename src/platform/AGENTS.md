# platform

All remote orchestration lives here: Ansible roles + playbooks, OpenTofu modules, operator CLI (`cmd/verself/`, being trimmed in favor of services), and the deploy catalog source. `src/cue-renderer` is the CUE source for the current single-node topology and deploy catalog; `aspect render --site=<site>` materialises typed Ansible inputs under `.cache/render/<site>/inventory/group_vars/all/generated/` (`catalog.yml`, `ops.yml`, `dns.yml`, `spire.yml`, `postgres.yml`, `endpoints.yml`, `routes.yml`, and related topology artifacts).

## Server profile

All server software is managed by the `deploy_profile` Ansible role, which populates `/opt/verself/profile/bin/` via three strategies:

- **Go service binaries** (billing-service, sandbox-rental-service, mailbox-service, identity-service, vm-orchestrator): built on the controller via `go build`, copied to server.
- **Static binaries** (Caddy, ClickHouse, TigerBeetle, Zitadel, Forgejo, Grafana, grafana-clickhouse-datasource plugin, otelcol-contrib, containerd, Node.js, Stalwart, stalwart-cli): pinned and fetched by Bazel under `//src/cue-renderer/binaries`, packed as `//src/cue-renderer/binaries:server_tools.tar.zst`, copied to the server, and unpacked by Ansible.
- **apt packages** (PostgreSQL 16, wireguard-tools, unpack/runtime support such as zstd): installed from PGDG / Ubuntu repos, with selected binaries symlinked into `verself_bin`.

The only other `apt install` is `zfsutils-linux` (kernel-dependent, must match the running kernel). Ubuntu 24.04 only.

## Runtime privilege boundary

Deploy-time Ansible may perform privileged host mutations. Runtime product services must not. Service units run as dedicated system users, not `root` or `ansible_user`, unless the unit is an explicitly privileged infrastructure daemon such as vm-orchestrator.

Non-vm-orchestrator services must not receive `zfs allow`, `/dev/zvol`, `/dev/kvm`, TAP, Firecracker, jailer, host network administration, or broad Linux capabilities. The vm-orchestrator Unix socket group (`vm-clients`) is root-equivalent for VM/ZFS lifecycle operations; membership is limited to approved internal control-plane callers and must be audited in Ansible.

## Ansible playbooks

Run from `src/platform/ansible/`. `--tags` targets individual roles (e.g. `--tags caddy`, `--tags clickhouse`). Global preflight checks run regardless of tag selection; Firecracker guest artifact preflight runs with the `firecracker` tag.

| Playbook | Purpose |
|---|---|
| `setup-dev.yml` | Install dev tools from the generated topology catalog vars |
| `setup-sops.yml` | Bootstrap SOPS + Age encryption for secrets |
| `provision.yml` | Provision bare metal via OpenTofu, generate inventory |
| `deprovision.yml` | Destroy bare metal infrastructure, remove inventory |
| `site.yml` | Canonical idempotent deploy for the current inventory topology |
| `guest-rootfs.yml` | Build guest rootfs, stage Firecracker guest artifacts |
| `observability-smoke.yml` | Minimal smoke probe used by `scripts/telemetry-smoke-test.sh` (`debug/assert` + `verself_uri`) |
| `security-patch.yml` | Rolling OS security updates |
| `billing-reset.yml` | Exhaustively wipe TigerBeetle + billing PostgreSQL database `billing` and restart callers |
| `identity-reset.yml` | Exhaustively wipe identity-service PG state, re-apply migrations, restart |
| `seed-system.yml` | Seed platform tenant + Acme tenant, billing, mailboxes, auth verify. `--tags identity,billing,stalwart,verify,dev-oidc` |

Read `aspect` (no args) for the full task surface; `aspect <task> --help` documents flags.

## Query ClickHouse

Use the AXL wrappers instead of hand-typing the SSH + client-cert prefix. They invoke `src/platform/scripts/clickhouse.sh`, which resolves the worker from `ansible/inventory/<site>.ini` (default `prod`) and runs `clickhouse-client` on the worker as the SPIFFE-authenticated `clickhouse_operator` user.

```bash
aspect db ch query --database=verself --query='SHOW TABLES'
```

OTel logs live in `default.otel_logs`, not `verself.otel_logs`:

```bash
aspect db ch query --query='SELECT Timestamp, Body FROM default.otel_logs ORDER BY Timestamp DESC LIMIT 10'
```

## Query PostgreSQL

Use the AXL wrappers instead of hand-typing SSH, passwords, and deployed
client paths. The billing-service database is `billing`; `sandbox` is a product
ID, not a PostgreSQL database name. The sandbox-rental-service database remains
`sandbox_rental`.

```bash
aspect db pg list
aspect db pg query --db=billing --query='SELECT count(*) FROM orgs'
aspect db pg query --db=sandbox_rental --query='SELECT count(*) FROM executions'
aspect db pg shell --db=billing
```

## Debug with observe

`aspect observe` is the blessed operator query surface for ClickHouse-backed telemetry. It is discoverability-first: begin with the query registry and signal catalogs, then use explicit operational queries for recent errors, services, HTTP access, mail, deploys, and traces.

```bash
aspect observe
aspect observe --what=queries
aspect observe --what=catalog --signal=metrics
aspect observe --what=catalog --signal=traces
aspect observe --what=describe --query=metric.latest
aspect observe --what=describe --metric=system.cpu.time
aspect observe --what=service --service=billing-service
aspect observe --what=errors
aspect observe --what=mail
aspect observe --what=deploy --run-key=<deploy-run-key>
```

Use `aspect db ch query` only when the observe surface does not yet cover the question. Interactive ClickHouse shells are intentionally unsupported because agent workflows need replayable commands.

Deploy playbook telemetry smoke probes:

```bash
src/platform/scripts/telemetry-smoke-test.sh                                    # success path: ansible + service correlation
TELEMETRY_SMOKE_TEST_EXPECT_FAIL=1 src/platform/scripts/telemetry-smoke-test.sh # sad path: assert Error spans are emitted
```

**Deterministic deploy correlation**:

- `deploy_run_key` = `YYYY-MM-DD.<counter>@<controller-host>`
- `deploy_id` = UUIDv5 over `verself:${deploy_run_key}`
- `scripts/deploy_identity.sh` exports `TRACEPARENT=00-<deploy_id_hex>-<stable>-01` and `OTEL_RESOURCE_ATTRIBUTES=verself.deploy_id=…,verself.deploy_run_key=…,…`. The upstream `community.general.opentelemetry` Ansible callback and `verself_uri` probes both anchor to that trace-id.
- The otelcol `transform/ansible_spans` processor renames upstream `<playbook>.yml` / `<task.name>` spans to `ansible.playbook` / `ansible.task` and mirrors `verself.*` from `ResourceAttributes` onto `SpanAttributes`. Single query shape: `SpanAttributes['verself.deploy_id']` works for ansible and service spans alike.
- `verselfotel.baggageSpanProcessor` projects W3C baggage members with the `verself.` prefix onto every span a service creates — `verself_uri` emits `baggage: verself.deploy_id=…,…` and services get the attribute without per-endpoint wiring.

## TLS with a real domain (Cloudflare)

```bash
cd src/platform/ansible
sops group_vars/all/secrets.sops.yml  # set verself_domain, company_domain, and cloudflare_api_token
ansible-playbook playbooks/site.yml
```

Services get subdomains automatically:

| Subdomain | Service |
|---|---|
| `dashboard.<domain>` | Grafana |
| `git.<domain>` | Forgejo |
| `auth.<domain>` | Zitadel |
| `mail.<domain>` | Stalwart (JMAP API + mailbox-service) — webmail frontend retired; surfaces will be folded into console |

## nftables boot-order

All listeners require `nftables.service` at boot. Future programmatic enforcement is planned — do not regress service unit ordering.

## otelcol

`roles/*/templates/otelcol-config.yaml.j2` holds the custom collection pipelines. Changes here land in every service's trace/metric/log story simultaneously.
