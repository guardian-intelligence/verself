# host-configuration

Host configuration owns host and daemon convergence. Ansible is the private
runner here, and the public deploy entry point is `aspect deploy`.

## Boundaries

- `src/tools/provisioning/` owns OpenTofu and bare-metal allocation.
- Nomad owns application and frontend rollout.
- Host configuration owns base OS packages, host networking, ZFS, trust roots,
  stateful daemons, operator daemons, external reconcilers, and per-component
  prerequisites.

## Convergence

Host convergence is the canonical Ansible site playbook:

| playbook | scope |
|----------|-------|
| `playbooks/site.yml` | controller preflight, host foundation, worker substrate, userspace, substrate daemons, external API reconciliation, per-component bindings, and local reconcilers |

Play order and role order are the dependency graph. Roles must not pull host
foundation work through `meta/main.yml` dependencies; a role that needs a
prerequisite runs after that prerequisite in `site.yml` or fails loudly when
invoked outside the site graph.

`verself-deploy run` (under `src/tools/deployment/`) is the deploy-flow
process: it derives identity, runs `playbooks/site.yml`, fans out to Nomad,
and writes `verself.deploy_events` through a typed ClickHouse writer.

`verself-deploy ansible run` wraps Ansible with the in-process OTel SDK; spans
go through `internal/runtime`'s SSH-forwarded OTLP channel to the bare-metal
otelcol on `:4317`. The Go-side streaming parser in `internal/ansible` reads
ansible-playbook stdout, emits per-task spans, and writes
`verself.ansible_task_events` rows directly.

## ClickHouse

Host configuration ClickHouse migrations live in `migrations/clickhouse/`. The
ClickHouse role applies that directory with `--database verself`; fully
qualified `default.*` tables remain valid for OTel exporter tables.

| table                         | written by                          | read by |
|-------------------------------|-------------------------------------|---------|
| `verself.deploy_events`       | `verself-deploy run` (internal/ledger) | observability dashboards |
| `verself.ansible_task_events` | `verself-deploy ansible run` (internal/ansible streaming parser) | live deploy views, drift triage |
| `verself.reconciler_runs`     | retired host-configuration script ledger | legacy deploy views |
