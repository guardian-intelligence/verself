# Operator Workflows

## Deployment Surface

Use `make deploy` for normal platform iteration. It rebuilds the Nix server profile, pushes it to the current worker, and reapplies the Ansible roles without wiping host state.

Use `make deploy-dashboards` when the change is limited to HyperDX sources or dashboards. That path exists specifically so dashboard iteration does not require a full platform redeploy.

## ClickHouse Access

ClickHouse is not exposed for unauthenticated remote access. Query it over SSH on the worker and use the controller-side password from `ansible/.credentials/clickhouse_password`.

The stable client path on the worker is `/opt/forge-metal/profile/bin/clickhouse-client`. Do not rely on `clickhouse-client` being on the default SSH `PATH`, and do not hardcode a `/nix/store/...` path.

Use this pattern from the repo root:

```bash
CLICKHOUSE_PASSWORD=$(cat ansible/.credentials/clickhouse_password)
ssh ubuntu@64.34.84.75 \
  "sudo /opt/forge-metal/profile/bin/clickhouse-client \
    --user default \
    --password '$CLICKHOUSE_PASSWORD' \
    --database forge_metal \
    --query 'SHOW TABLES'"
```

The current database layout is:

- `forge_metal.ci_events`
- `forge_metal.smelter_rehearsals`
- `default.otel_logs`
- `default.otel_traces`
- `default.otel_metrics_gauge`
- `default.otel_metrics_sum`
- `default.otel_metrics_histogram`

The OTel tables live in `default`, not in an `otel` database.

## CI Fixture Surface

Use `make ci-fixtures-pass` for the common operator loop: seed the controlled example repositories, warm their goldens if needed, open PRs, and verify that the positive fixture suite succeeds on the already-deployed host.

Use `make ci-fixtures-fail` for deterministic negative-path verification against the same deployed host state once fail fixtures exist. It runs the fixture runner only; it does not reapply the broader platform roles.

Use `make ci-fixtures-refresh` when the guest kernel, rootfs, or staged CI artifacts changed. It rebuilds and restages the Firecracker guest artifacts without touching the rest of the platform.

Use `make ci-fixtures-full` when you want the composed rehearsal: refresh guest artifacts first, then run the configured fixture target set from `CI_FIXTURE_FULL_TARGETS`. Today that defaults to `ci-fixtures-pass`. The orchestration is suite-based so additional suites such as `fail` can be added without changing the operator entrypoints.

## Suite Model

The current suite is `pass`. It contains the positive example repositories that are expected to complete with a successful Forgejo Actions result.

`fail` is the negative-path suite name. The runner and playbook surface are in place; add fixtures to it when you want deterministic CI failures to count as a passing rehearsal outcome.

`full` is not a suite itself; it is the orchestration target that refreshes artifacts and then runs the configured target list.
