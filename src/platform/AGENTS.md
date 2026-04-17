# platform

All remote orchestration lives here: Ansible roles + playbooks, OpenTofu modules, operator CLI (`cmd/forge-metal/`, being trimmed in favor of services), pinned binary manifests (`server-tools.json`, `dev-tools.json`).

## Server profile

All server software is managed by the `deploy_profile` Ansible role, which populates `/opt/forge-metal/profile/bin/` via three strategies:

- **Go service binaries** (billing-service, sandbox-rental-service, mailbox-service, identity-service, vm-orchestrator): built on the controller via `go build`, copied to server.
- **Caddy** (with Coraza WAF plugin): built on the controller via `xcaddy`, copied to server.
- **Static binaries** (ClickHouse, TigerBeetle, Zitadel, Forgejo, Grafana, grafana-clickhouse-datasource plugin, otelcol-contrib, containerd, Node.js, Stalwart, stalwart-cli): pinned in `server-tools.json` with URLs and SHA256 hashes, downloaded and verified on the server.
- **apt packages** (PostgreSQL 16, wireguard-tools): installed from PGDG / Ubuntu repos, symlinked into `fm_bin`.

The only other `apt install` is `zfsutils-linux` (kernel-dependent, must match the running kernel). Ubuntu 24.04 only.

## Ansible playbooks

Run from `src/platform/ansible/`. `--tags` targets individual roles (e.g. `--tags caddy`, `--tags clickhouse`); preflight checks run regardless of tag selection.

| Playbook | Purpose |
|---|---|
| `setup-dev.yml` | Install pinned dev tools from `dev-tools.json` |
| `setup-sops.yml` | Bootstrap SOPS + Age encryption for secrets |
| `provision.yml` | Provision bare metal via OpenTofu, generate inventory |
| `deprovision.yml` | Destroy bare metal infrastructure, remove inventory |
| `dev-single-node.yml` | Idempotent single-node deploy |
| `site.yml` | Multi-node deploy (workers + infra) |
| `guest-rootfs.yml` | Build guest rootfs, stage Firecracker guest artifacts |
| `observability-smoke.yml` | Minimal smoke probe used by `telemetry-proof` (`debug/assert` + `fm_uri`) |
| `security-patch.yml` | Rolling OS security updates |
| `billing-reset.yml` | Exhaustively wipe TigerBeetle + billing PostgreSQL database `billing` and restart callers |
| `identity-reset.yml` | Exhaustively wipe identity-service PG state, re-apply migrations, restart |
| `seed-system.yml` | Seed platform tenant + Acme tenant, billing, mailboxes, auth verify. `--tags identity,billing,stalwart,verify,dev-oidc` |

Read the top-level `Makefile` for other common automation wrappers.

## Query ClickHouse

Use the Makefile wrappers instead of hand-typing the SSH + password prefix. They `cd` into `src/platform/` and invoke `scripts/clickhouse.sh`, which resolves the worker from `ansible/inventory/hosts.ini` and reads the ClickHouse password from SOPS.

```bash
make inventory-check
make clickhouse-query QUERY='SHOW TABLES' DATABASE=forge_metal
make clickhouse-shell
```

OTel logs live in `default.otel_logs`, not `forge_metal.otel_logs`:

```bash
make clickhouse-query QUERY='SELECT Timestamp, Body FROM default.otel_logs ORDER BY Timestamp DESC LIMIT 10'
```

## Query PostgreSQL

Use the Makefile wrappers instead of hand-typing SSH, passwords, and deployed
client paths. The billing-service database is `billing`; `sandbox` is a product
ID, not a PostgreSQL database name. The sandbox-rental-service database remains
`sandbox_rental`.

```bash
make pg-list
make pg-query DB=billing QUERY='SELECT count(*) FROM orgs'
make pg-query DB=sandbox_rental QUERY='SELECT count(*) FROM executions'
make pg-shell DB=billing
```

## Debug with traces

`make traces` pulls recent HTTP traces and structured logs from ClickHouse in a single command:

```bash
make traces                                   # Last 5 min, all services
make traces SERVICE=billing-service           # Filter to one service
make traces MINUTES=30                        # Last 30 minutes
make traces ERRORS=1                          # Errors only (4xx/5xx + ERROR/WARN)
make traces SERVICE=sandbox-rental ERRORS=1   # Combine filters
```

Deploy playbook telemetry:

```bash
make deploy-trace QUERY="SpanName = 'ansible.task'"
make telemetry-proof           # success path: ansible + service correlation
make telemetry-proof-fail      # sad path: assert Error spans are emitted
```

**Deterministic deploy correlation**:

- `deploy_run_key` = `YYYY-MM-DD.<counter>@<controller-host>`
- `deploy_id` = UUIDv5 over `forge-metal:${deploy_run_key}`
- `scripts/deploy_identity.sh` exports `TRACEPARENT=00-<deploy_id_hex>-<stable>-01` and `OTEL_RESOURCE_ATTRIBUTES=forge_metal.deploy_id=…,forge_metal.deploy_run_key=…,…`. The upstream `community.general.opentelemetry` Ansible callback and `fm_uri` probes both anchor to that trace-id.
- The otelcol `transform/ansible_spans` processor renames upstream `<playbook>.yml` / `<task.name>` spans to `ansible.playbook` / `ansible.task` and mirrors `forge_metal.*` from `ResourceAttributes` onto `SpanAttributes`. Single query shape: `SpanAttributes['forge_metal.deploy_id']` works for ansible and service spans alike.
- `fmotel.baggageSpanProcessor` projects W3C baggage members with the `forge_metal.` prefix onto every span a service creates — `fm_uri` emits `baggage: forge_metal.deploy_id=…,…` and services get the attribute without per-endpoint wiring.

## TLS with a real domain (Cloudflare)

```bash
cd src/platform/ansible
sops group_vars/all/secrets.sops.yml  # set forge_metal_domain and cloudflare_api_token
ansible-playbook playbooks/dev-single-node.yml
```

Services get subdomains automatically:

| Subdomain | Service |
|---|---|
| `dashboard.<domain>` | Grafana |
| `git.<domain>` | Forgejo |
| `auth.<domain>` | Zitadel |
| `mail.<domain>` | Stalwart (JMAP API + webmail) |

## nftables boot-order

All listeners require `nftables.service` at boot. Future programmatic enforcement is planned — do not regress service unit ordering.

## otelcol

`roles/*/templates/otelcol-config.yaml.j2` holds the custom collection pipelines. Changes here land in every service's trace/metric/log story simultaneously.
