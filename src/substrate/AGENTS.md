# substrate

Substrate owns host and daemon convergence. Ansible may remain as the private
runner here, but it is not the public architecture. Public entry points are
`aspect substrate ...` and `aspect deploy --substrate=...`.

## Boundaries

- `src/provision/` owns OpenTofu and bare-metal allocation.
- Nomad owns application and frontend rollout.
- Substrate owns base OS packages, host networking, ZFS, trust roots, stateful
  daemons, operator daemons, external reconcilers, and per-component
  prerequisites.

## Convergence

The substrate converge is layered. Each layer is its own playbook with its own
content-hash digest target; `aspect deploy` skips a layer when its current
input_hash matches the last_applied_hash recorded in
`verself.deploy_layer_runs`.

| layer            | playbook                          | bazel digest                                      | scope |
|------------------|-----------------------------------|---------------------------------------------------|-------|
| `l1_os`          | `playbooks/l1_os.yml`             | `//src/substrate:prod_l1_os_digest`               | OS, kernel, ZFS, nftables, wireguard, base packages, containerd, firecracker |
| `l2_userspace`   | `playbooks/l2_userspace.yml`      | `//src/substrate:prod_l2_userspace_digest`        | Batched system users + groups for every Nomad-supervised component |
| `l3_binaries`    | `playbooks/l3_binaries.yml`       | `//src/substrate:prod_l3_binaries_digest`         | Substrate daemons (postgres, clickhouse, openbao, zitadel, spire, …) and their foundational config |
| `l4a_components` | `playbooks/l4a_components.yml`    | `//src/substrate:prod_l4a_components_digest`      | External-API reconciliation (cloudflare_dns, openbao_tenancy, zitadel apps, …) and per-component PG/CH/credstore bindings |

`scripts/run-layer.sh` is the per-layer primitive: read last_applied_hash,
compare to input_hash, skip-or-run, write the resulting row to
`verself.deploy_layer_runs`. `scripts/divergence-canary.sh` is the post-deploy
sanity check that gates Nomad rollout on a clean ledger.

`verself-deploy ansible run` wraps Ansible with the in-process OTel
SDK and a controller-side OTLP buffer agent supervised for the
duration of the run. Configuration is embedded at
`src/deployment-tooling/internal/otelagent/otelcol.yaml`. Deploy and
substrate commands route Ansible through this binary so failures
still produce ClickHouse evidence.

`ansible/callback_plugins/verself_otel.py` is a thin subclass of
`community.general.opentelemetry`; the upstream callback hardcodes
`host.status='ok'` regardless of `result['changed']`, so we override
`v2_runner_on_ok` to emit `host.status='changed'` for tasks that mutated state.
The divergence canary depends on this distinction.

## ClickHouse

Substrate ClickHouse migrations live in `migrations/clickhouse/`. The
ClickHouse role applies that directory with `--database verself`; fully
qualified `default.*` tables remain valid for OTel exporter tables.

| table                         | written by                          | read by |
|-------------------------------|-------------------------------------|---------|
| `verself.deploy_events`       | `scripts/record-deploy-event.sh`    | observability dashboards |
| `verself.deploy_layer_runs`   | `scripts/record-layer-run.sh`       | `scripts/layer-last-applied.sh`, `scripts/divergence-canary.sh`, `aspect substrate verify` |
| `verself.substrate_convergence_events` | (no new writers; legacy)   | historical audit only |
