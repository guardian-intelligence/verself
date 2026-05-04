# Host Configuration

Host configuration owns host and daemon convergence inputs:

- `ansible/playbooks/site.yml` is the canonical substrate convergence graph.
  Play order and role order encode host foundation, userspace, substrate
  daemons, external API reconciliation, per-component bindings, and local
  reconcilers.
- `migrations/clickhouse/` contains host convergence ClickHouse schema.
- `cmd/` contains typed host-configuration operators such as the Cloudflare
  DNS reconciler. Operator database access goes through `aspect db pg|ch|tb`,
  backed by `aspect-operator` and `src/tools/operator-runtime/go`.

`aspect deploy --site=<site>` refreshes the operator SSH certificate, runs
`verself-deploy run`, and then lets the Go orchestrator execute the Ansible
site playbook before Nomad fan-out. The deploy succeeds when the site playbook,
guest-image staging, local reconcilers, Nomad fan-out, and the typed ClickHouse
`deploy_events` succeeded row all return cleanly.

The only public host-configuration maintenance task is secret editing:

```bash
aspect host-configuration edit-secrets
```
