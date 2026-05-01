# substrate

Substrate owns host and daemon convergence. Ansible may remain as the private
runner here, but it is not the public architecture. Public entry points are
`aspect substrate ...` and `aspect deploy --substrate=...`.

## Boundaries

- CUE in `src/cue-renderer/` owns topology, ports, runtime users, identities,
  routes, and rendered inputs.
- `src/provision/` owns OpenTofu and bare-metal allocation.
- Nomad owns application and frontend rollout.
- Substrate owns base OS packages, host networking, ZFS, trust roots, stateful
  daemons, operator daemons, external reconcilers, and per-component
  prerequisites.

## Convergence

`ansible/playbooks/box.yml` is the current convergence entrypoint. Keep it
limited to substrate state. Do not add software rollout supervision here; new
application lifecycle work belongs in Nomad jobs and the deploy path.

`scripts/ansible-with-otel.sh` wraps Ansible with the controller-side OTLP
buffer in `controller-agent/otelcol.yaml`. Deploy and substrate commands must
use that wrapper so failures still produce ClickHouse evidence.

## ClickHouse

Substrate ClickHouse migrations live in `migrations/clickhouse/`. The
ClickHouse role applies that directory with `--database verself`; fully
qualified `default.*` tables remain valid for OTel exporter tables.
