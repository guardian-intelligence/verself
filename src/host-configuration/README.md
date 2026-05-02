# Host Configuration

Host configuration owns host and daemon convergence inputs:

- `ansible/playbooks/site.yml` is the canonical substrate convergence graph.
  Play order and role order encode host foundation, userspace, substrate
  daemons, external API reconciliation, per-component bindings, and local
  reconcilers.
- `migrations/clickhouse/` contains host convergence ClickHouse schema.
- `scripts/` contains maintenance helpers that operate on the controller
  directly: `clickhouse.sh` / `pg.sh` / `tigerbeetle.sh` for ssh-tunneled
  DB shells, `wipe-server.sh` for fleet teardown, and
  `reconcile-cloudflare-dns.sh` for the Cloudflare DNS reconciler.

`aspect deploy --site=<site>` refreshes the operator SSH certificate, stages
reviewable render output, runs `verself-deploy run`, and then lets the Go
orchestrator execute the Ansible site playbook before Nomad fan-out. The
deploy succeeds when the site playbook, local reconcilers, Nomad fan-out, and
the typed ClickHouse `deploy_events` succeeded row all return cleanly.

Use explicit substrate commands when touching this package:

```bash
aspect substrate converge --site=prod   # run playbooks/site.yml without Nomad
aspect substrate verify   --site=prod   # syntax-check playbooks/site.yml
```
