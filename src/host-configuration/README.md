# Host Configuration

Host configuration owns host and daemon convergence inputs:

- `ansible/` contains the layered convergence playbooks (`l1_os.yml`,
  `l2_userspace.yml`, `l3_binaries.yml`, `l4a_components.yml`) plus the
  per-component dispatch in `tasks/component_substrate.yml`.
- `migrations/clickhouse/` contains host convergence ClickHouse schema.
- `scripts/` contains the maintenance helpers that operate on the
  controller directly: `clickhouse.sh` / `pg.sh` / `tigerbeetle.sh`
  for ssh-tunneled DB shells, `wipe-server.sh` for fleet teardown,
  `reconcile-cloudflare-dns.sh` for the Cloudflare DNS reconciler.
  The deploy critical path — identity, ledger writes, layered
  convergence, OTLP transport, and the Ansible streaming parser —
  lives in `src/deployment-tools/`. `aspect deploy` shells out to
  `verself-deploy run`, which owns the entire flow inside one
  process sharing one SSH session.

`aspect deploy --site=<site>` walks the four substrate layers in order. For
each layer `verself-deploy run` builds the per-layer Bazel digest target
(`//src/host-configuration:<site>_<layer>_digest`), reads the most recent
`last_applied_hash` from `verself.deploy_layer_runs`, and either short-
circuits (writes a `skipped` row, never invokes Ansible) or runs the layer's
playbook through `verself-deploy ansible run` (writes a `succeeded` /
`failed` row). `--substrate=always` forces every layer to run regardless of
hash. The deploy succeeds when all four layers + reconcilers + Nomad fan-out
return cleanly and the typed ClickHouse writer acks the `succeeded`
`deploy_events` row.

Use explicit substrate commands when touching this package:

```bash
aspect substrate converge --site=prod   # force every layer regardless of hash
aspect substrate verify   --site=prod   # exit 10 if any layer is stale
```
