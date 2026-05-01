# Substrate

Substrate owns host and daemon convergence inputs:

- `ansible/` contains the layered convergence playbooks (`l1_os.yml`,
  `l2_userspace.yml`, `l3_binaries.yml`, `l4a_components.yml`) plus the
  per-component dispatch in `tasks/component_substrate.yml`.
- `migrations/clickhouse/` contains substrate-owned ClickHouse schema.
- `scripts/` contains the per-layer runner (`run-layer.sh`), the ledger
  writer (`record-layer-run.sh`), the hash-gate read path
  (`layer-last-applied.sh`), and the post-deploy canary
  (`divergence-canary.sh`). The OTLP buffer agent and the Ansible
  streaming parser live in `src/deployment-tooling/internal/otelagent`
  and `src/deployment-tooling/internal/ansible`; `verself-deploy
  ansible run` wraps `ansible-playbook` with both, replacing the
  prior `ansible-with-otel.sh` + `with-otel-agent.sh` shell pair.

`aspect deploy --site=<site>` walks the four substrate layers in order. For
each layer it builds the per-layer Bazel digest target
(`//src/substrate:<site>_<layer>_digest`), reads the most recent
`last_applied_hash` from `verself.deploy_layer_runs`, and either short-
circuits (writes a `skipped` row, never invokes Ansible) or runs the layer's
playbook through `verself-deploy ansible run` (writes a `succeeded` /
`failed` row). `--substrate=always` forces every layer to run regardless of
hash. After a successful converge the post-deploy canary asserts that all
four layer rows exist for the run and none recorded `failed`; the deploy
fails loudly if the ledger is incomplete.

Use explicit substrate commands when touching this package:

```bash
aspect substrate converge --site=prod   # force every layer regardless of hash
aspect substrate verify   --site=prod   # exit 10 if any layer is stale
```
