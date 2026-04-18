# Operator Workflows

## Deployment Surface

Use `dev-single-node.yml` for normal platform iteration. It rebuilds the
server profile, pushes it to the current worker, and reapplies the Ansible roles
without wiping host state. Target individual roles with `--tags` (e.g.
`--tags caddy` or `--tags grafana`).

Grafana dashboards are provisioned by the `grafana` role and exercised by
`make grafana-proof`; no separate dashboard-sync playbook exists.

After `make grafana-proof`, verify ClickHouse evidence with:

```sql
SELECT event_time, type, initial_user, query
FROM system.query_log
WHERE event_time >= now() - INTERVAL 15 MINUTE
  AND query LIKE '%fm:grafana verify=%'
ORDER BY event_time;
```

## ClickHouse Access

ClickHouse is not exposed for unauthenticated remote access. Use the repo wrapper so you do not have to manually prefix SSH, the password file, or the stable worker client path each time.

The wrapper resolves the worker from `ansible/inventory/hosts.ini`, decrypts the ClickHouse password from `ansible/group_vars/all/secrets.sops.yml` via SOPS, and invokes `/opt/forge-metal/profile/bin/clickhouse-client` on the worker. Do not hardcode a `/nix/store/...` path.

Use it from the repo root:

```bash
make clickhouse-query QUERY='SHOW TABLES' DATABASE=forge_metal
./src/platform/scripts/clickhouse.sh --database forge_metal --query 'SHOW TABLES'
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
```

Every ClickHouse-backed observe query emits an `observe` trace and a
`clickhouse.query` span with `observe.query_id`,
`observe.query_family`, `clickhouse.query_id`, and
`clickhouse.query_sha256` attributes.

The current database layout is:

- `forge_metal.job_events`
- `forge_metal.job_logs`
- `forge_metal.metering`
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

Use `guest-rootfs.yml` when the guest kernel, rootfs, or staged Firecracker guest artifacts changed. It rebuilds and restages the guest artifacts without touching the rest of the platform.
