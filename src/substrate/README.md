# Substrate

Substrate owns host and daemon convergence inputs:

- `ansible/` contains the current private convergence runner.
- `migrations/clickhouse/` contains substrate-owned ClickHouse schema.
- `scripts/` contains operator wrappers used by Aspect tasks and deploys.
- `controller-agent/` contains the controller-side OTLP buffer used around
  Ansible and other operator-originated telemetry.

`aspect deploy --site=<site>` renders CUE, computes
`//src/substrate:<site>_substrate_digest`, and runs substrate convergence only
when the digest lacks successful ClickHouse evidence for every inventory node,
unless `--substrate=always` or `--substrate=skip` says otherwise. The digest
hashes this package, Bazel-built substrate tool bundles, and rendered substrate
projections under `inventory/` and `share/rendered/`. CUE files are Bazel action
inputs, but raw CUE and rendered Nomad jobs are not digest material; a Nomad-only
CUE change should rebuild the digest target and produce the same digest.

Use explicit substrate commands when touching this package:

```bash
aspect substrate converge --site=prod
aspect substrate verify --site=prod
```
