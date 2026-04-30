# platform

All remote substrate configuration lives here: Ansible roles + playbooks, OpenTofu modules, operator CLI (`cmd/verself/`, being trimmed in favor of services), and the deploy catalog source. `src/cue-renderer` is the CUE source for the current single-node topology and deploy catalog; `aspect render --site=<site>` materialises typed Ansible inputs under `.cache/render/<site>/inventory/group_vars/all/generated/` (`catalog.yml`, `ops.yml`, `dns.yml`, `spire.yml`, `postgres.yml`, `endpoints.yml`, `routes.yml`, and related topology artifacts).

## Server Profile

Ansible installs host and substrate software only. `substrate_profile`
populates `/opt/verself/profile/bin/` for long-lived substrate daemons and
deploy helper binaries; application and frontend artifacts are Bazel-produced
Nomad artifacts published by `scripts/nomad-deploy-all.sh`.

- **Go substrate binaries and service helper tools** (Temporal platform, vm-orchestrator, `nomad-deploy`, and deploy-time helper commands): built on the controller via Bazel and copied to the server by `substrate_profile`.
- **Static binaries** (Caddy, ClickHouse, TigerBeetle, Zitadel, OpenBao, SPIRE server + agent, spiffe-helper, NATS, Garage, Forgejo, Grafana, grafana-clickhouse-datasource plugin, otelcol-contrib, Temporal, containerd, Node.js, Stalwart, stalwart-cli, bazel-remote, Nomad): pinned and fetched by Bazel under `//src/cue-renderer/binaries`, packed as `//src/cue-renderer/binaries:server_tools.tar.zst`, copied to the server, and unpacked by Ansible.
- **apt packages** (PostgreSQL 16, wireguard-tools, unpack/runtime support such as zstd): installed from PGDG / Ubuntu repos, with selected binaries symlinked into `verself_bin`.

The only other `apt install` is `zfsutils-linux` (kernel-dependent, must match the running kernel). Ubuntu 24.04 only.

## Runtime privilege boundary

Deploy-time Ansible may perform privileged host mutations. Runtime product services must not. Service units run as dedicated system users, not `root` or `ansible_user`, unless the unit is an explicitly privileged infrastructure daemon such as vm-orchestrator.

Non-vm-orchestrator services must not receive `zfs allow`, `/dev/zvol`, `/dev/kvm`, TAP, Firecracker, jailer, host network administration, or broad Linux capabilities. The vm-orchestrator Unix socket group (`vm-clients`) is root-equivalent for VM/ZFS lifecycle operations; membership is limited to approved internal control-plane callers and must be audited in Ansible.

## Ansible playbooks

Run from `src/platform/ansible/` for low-level debugging. The public deploy surface is `aspect deploy`; it renders the cache, applies `playbooks/box.yml` for host/substrate configuration, and then rolls application jobs through Nomad.

| Playbook | Purpose |
|---|---|
| `setup-dev.yml` | Install dev tools from the generated topology catalog vars |
| `setup-sops.yml` | Bootstrap SOPS + Age encryption for secrets |
| `provision.yml` | Provision bare metal via OpenTofu, generate inventory |
| `deprovision.yml` | Destroy bare metal infrastructure, remove inventory |
| `box.yml` | Idempotent host/substrate configuration for the current inventory topology |
| `guest-rootfs.yml` | Build guest rootfs, stage Firecracker guest artifacts |
| `security-patch.yml` | Rolling OS security updates |
| `billing-reset.yml` | Exhaustively wipe TigerBeetle + billing PostgreSQL database `billing` and restart callers |
| `identity-reset.yml` | Exhaustively wipe identity-service PG state, re-apply migrations, restart |
| `seed-system.yml` | Seed platform tenant + Acme tenant, billing, mailboxes, auth verify. |

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

Deploy completion evidence comes from `verself.deploy_events`, Nomad job state,
and ClickHouse OTel traces/metrics queried through `aspect observe` or
`aspect db ch query`. The old handwritten `scripts/verify-*` canaries were
removed with the Nomad cutover.

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
aspect deploy --site=prod
```

Services get subdomains automatically:

| Subdomain | Service |
|---|---|
| `dashboard.<domain>` | Grafana |
| `git.<domain>` | Forgejo |
| `auth.<domain>` | Zitadel |
| `mail.<domain>` | Stalwart (JMAP API + mailbox-service) — webmail frontend retired; surfaces will be folded into verself-web |

## nftables boot-order

All listeners require `nftables.service` at boot. Future programmatic enforcement is planned — do not regress service unit ordering.

## otelcol

`roles/*/templates/otelcol-config.yaml.j2` holds the on-server otelcol pipelines. Changes here land in every service's trace/metric/log story simultaneously.

`controller-agent/otelcol.yaml` is the controller-side twin: a single-process otelcol-contrib that `scripts/with-otel-agent.sh` stands up around `aspect deploy` and other operator commands that need controller-originated telemetry. Receivers bind 127.0.0.1:14317 (gRPC OTLP); the exporter ships to the bare-metal otelcol over an SSH `-L`. The `file_storage` extension persists the sending queue under `.cache/render/<site>/otelcol-data/`, so a flush-after-exit race between ansible's BSP atexit hook and the wrapper's tunnel teardown can no longer drop spans.
