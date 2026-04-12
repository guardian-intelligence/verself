
## Quick Start

### 1. Install dev tools

```bash
cd src/platform/ansible && ansible-playbook playbooks/setup-dev.yml
```

### 2. Provision bare metal

```bash
# Create your tfvars (one-time)
cp src/platform/terraform/terraform.tfvars.example.json src/platform/terraform/terraform.tfvars.json
# Edit terraform.tfvars.json — set project_id to your Latitude.sh project

# Provision server + generate Ansible inventory
cd src/platform/ansible && ansible-playbook playbooks/provision.yml
```

This provisions a bare metal server via OpenTofu and auto-generates the gitignored `src/platform/ansible/inventory/hosts.ini` from the outputs. The Latitude.sh auth token is read from SOPS-encrypted secrets.

### 3. Deploy

```bash
cd src/platform/ansible && ansible-playbook playbooks/dev-single-node.yml
```

Idempotent, no wipe. Safe to run repeatedly. Deploy a single role with `--tags`:

```bash
cd src/platform/ansible && ansible-playbook playbooks/dev-single-node.yml --tags caddy
```

### 4. Seed and assume rehearsal personas

After deploy, seed the platform tenant, Acme tenant, billing state, mailboxes,
and auth fixtures:

```bash
make seed-system
```

The `assume-*` targets are extremely useful utility scripts for operators and
agents. They mint short-lived, project-scoped Zitadel tokens from the deployed
credential store and write a `0600` env file under `artifacts/personas/`.

```bash
make assume-platform-admin
make assume-acme-admin
make assume-acme-member
make assume-persona PERSONA=platform-admin OUTPUT=/tmp/platform-admin.env
```

`platform-admin` is for dogfooding internal platform operations. It has
sandbox-rental, webmail/mailbox-service, Letters, and Forgejo OIDC project
access, plus operator command hints for ClickHouse and provider-native Forgejo
automation. `acme-admin` and `acme-member` rehearse the customer org roles used
by rent-a-sandbox.

These scripts do not export the Zitadel admin PAT, ClickHouse password,
Stalwart direct protocol passwords, or Forgejo provider API token. Those remain
behind the existing operator wrappers and remote credstore files.

### 5. Log in

```bash
# HyperDX admin credentials are in the SOPS-encrypted secrets file
sops -d --extract '["hyperdx_admin_email"]' src/platform/ansible/group_vars/all/secrets.sops.yml
sops -d --extract '["hyperdx_admin_password"]' src/platform/ansible/group_vars/all/secrets.sops.yml
```

Open `https://<ip>` in your browser (self-signed cert for IP addresses, auto Let's Encrypt for domains).

## Snapshot-Backed VM Farm

Forge Metal's runtime direction is a checkpoint-backed Firecracker VM farm. CI,
direct shell execution, canaries, scheduled automation, and customer workloads
compile to the same execution model:

```text
checkpoint ref -> immutable checkpoint version -> writable zvol clone -> VM segment
```

The first product proof is a Postgres checkpoint demo: boot a VM from a large
Postgres zvol, print `pg_size_pretty(pg_database_size(current_database()))`,
mutate a counter, call `vm-bridge snapshot save pg-demo`, then run again
and observe the advanced counter without copying the full database image.

Authoritative code entry points:

- `.forgejo/workflows/ci.yml` - first Forgejo Actions tracer for `runs-on: forge-metal`.
- `src/sandbox-rental-service/internal/jobs/` - customer execution state, billing, workflow/checkpoint policy.
- `src/sandbox-rental-service/migrations/` - PostgreSQL state machines for executions, VM segments, checkpoint refs, checkpoint versions, and save requests.
- `src/vm-orchestrator/` - privileged host daemon for Firecracker, TAP networking, ZFS clone/snapshot/destroy, and guest telemetry.
- `src/vm-orchestrator/vmproto/` - host/guest vsock protocol.
- `src/vm-orchestrator/cmd/vm-bridge/` - guest PID 1 and user-facing in-guest snapshot CLI.
- `src/vm-guest-telemetry/` - guest telemetry sampler.
- `src/viteplus-monorepo/apps/rent-a-sandbox/` - VM farm UI.

Hard runtime boundaries:

- ZFS snapshots are immutable; customer-facing checkpoint refs are mutable.
- Guests may request `vm-bridge snapshot save <ref>`; they never send or receive host dataset paths.
- `vm-bridge` is an untrusted guest client; vm-orchestrator accepts checkpoint saves only for host-authorized refs.
- vm-orchestrator constructs all ZFS paths from trusted host-side IDs and operates only on the active segment's known writable zvol.
- The host never mounts or fscks untrusted guest filesystems in the default checkpoint path.
- Host-local services are exposed through the host-service plane, not DNAT to `127.0.0.1`.

Current implementation still has direct execution paths while the checkpoint
state model is being cut in. Treat docs that describe direct-only execution as
stale unless they point back to the code above.

## Licensing

This project is open-source. Most bundled server components (ClickHouse, TigerBeetle, Forgejo, PostgreSQL) use permissive or weak-copyleft licenses with no network-interaction obligations.

**Stalwart Mail Server** is licensed under AGPL-3.0. If you run Stalwart unmodified (deployed as a pinned binary from `server-tools.json`), your obligation is to provide users with a link to the upstream source at `github.com/stalwartlabs/stalwart`. Your own application code that communicates with Stalwart over JMAP/SMTP/IMAP is a separate work and is not covered by AGPL. If you modify Stalwart's source and serve it over a network, you must make your modifications available to users who interact with it. Consult a lawyer if you are offering hosted email as a closed-source commercial product built on this stack.
