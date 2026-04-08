# Operator Workflows

## Deployment Surface

Use `dev-single-node.yml` for normal platform iteration. It rebuilds the Nix server profile, pushes it to the current worker, and reapplies the Ansible roles without wiping host state. Target individual roles with `--tags` (e.g. `--tags caddy`).

Use `hyperdx-dashboards.yml` when the change is limited to HyperDX sources or dashboards. That path exists specifically so dashboard iteration does not require a full platform redeploy.

## ClickHouse Access

ClickHouse is not exposed for unauthenticated remote access. Use the repo wrapper so you do not have to manually prefix SSH, the password file, or the stable worker client path each time.

The wrapper resolves the worker from `ansible/inventory/hosts.ini`, decrypts the ClickHouse password from `ansible/group_vars/all/secrets.sops.yml` via SOPS, and invokes `/opt/forge-metal/profile/bin/clickhouse-client` on the worker. Do not hardcode a `/nix/store/...` path.

Use it from the repo root:

```bash
make clickhouse-query QUERY='SHOW TABLES' DATABASE=forge_metal
make clickhouse-shell
./scripts/clickhouse.sh --database forge_metal --query 'SHOW TABLES'
```

The current database layout is:

- `forge_metal.ci_events`
- `forge_metal.vm_orchestrator_rehearsals`
- `default.otel_logs`
- `default.otel_traces`
- `default.otel_metrics_gauge`
- `default.otel_metrics_sum`
- `default.otel_metrics_histogram`

The OTel tables live in `default`, not in an `otel` database.

## CI Fixture Surface

Use `ci-fixtures-pass.yml` for the common operator loop: seed the controlled example repositories, warm their goldens if needed, open PRs, and verify that the positive fixture suite succeeds on the already-deployed host.

Use `ci-fixtures-fail.yml` for deterministic negative-path verification against the same deployed host state. It runs the fixture runner only; it does not reapply the broader platform roles.

Use `guest-rootfs.yml` when the guest kernel, rootfs, or staged CI artifacts changed. It rebuilds and restages the Firecracker guest artifacts without touching the rest of the platform.

Use `ci-fixtures-full.yml` when you want the composed rehearsal: refresh guest artifacts first, then run the pass and fail fixture suites in one bounded-parallel fixture run. The suite list is still driven by `ci_fixtures_suites` in the Ansible role.

## Suite Model

`pass` contains the positive example repositories that are expected to complete with a successful Forgejo Actions result.

`fail` is the negative-path suite name. The first fixture exercises a deterministic run-phase test failure and asserts the exact failure signature from exec telemetry.

`full` is not a suite itself; it is the orchestration playbook that refreshes artifacts and then runs the pass and fail suites together.
