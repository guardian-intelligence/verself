# Operator Workflows

## Deployment Surface

Use `dev-single-node.yml` for normal platform iteration. It rebuilds the
server profile, pushes it to the current worker, and reapplies the Ansible roles
without wiping host state. Target individual roles with `--tags` (e.g.
`--tags caddy` or `--tags grafana`).

Grafana dashboards are provisioned by the `grafana` role and exercised by
`make grafana-proof`; no separate dashboard-sync playbook exists.

## ClickHouse Access

ClickHouse is not exposed for unauthenticated remote access. Use the repo wrapper so you do not have to manually prefix SSH, the password file, or the stable worker client path each time.

The wrapper resolves the worker from `ansible/inventory/hosts.ini`, decrypts the ClickHouse password from `ansible/group_vars/all/secrets.sops.yml` via SOPS, and invokes `/opt/forge-metal/profile/bin/clickhouse-client` on the worker. Do not hardcode a `/nix/store/...` path.

Use it from the repo root:

```bash
make clickhouse-query QUERY='SHOW TABLES' DATABASE=forge_metal
make clickhouse-shell
./src/platform/scripts/clickhouse.sh --database forge_metal --query 'SHOW TABLES'
```

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

Use `guest-rootfs.yml` when the guest kernel, rootfs, or staged Firecracker guest artifacts changed. It rebuilds and restages the guest artifacts without touching the rest of the platform.
