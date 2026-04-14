# Forge Metal

Forge Metal is the open reference architecture for a single-operator software company that fundamentally changes the math around starting a business. It raises the revenue ceiling of a single-person business from ~$10M to ~$1B by eliminating the integration, compliance, and operations tax that everyone else rebuilds from scratch.

The smartest 

This new type of company consists of just two entities:

1. "The Operator" -- the human owner and principle making executive decisions.
2. The Agent -- the executor behind the SDLC, marketing and customer support.

The principles of this new type of company:

1. Revenue is metered at machine speed. Every inference, every agent action, every tool call is a billing event. Stripe Billing chokes on this; Chargebee wasn't built for it; hybrid credit+subscription+usage is the default shape, not an edge case.

2. Execution is untrusted by default. AI-native companies run code they didn't write — agent-generated code, customer-uploaded workflows, LLM tool calls. Sandboxed execution where everything is appended to an audit log is the default. 
    * ZFS + Firecracker. 
    * Managed PaaSvendors cannot offer this without rebuilding their stack.

3. Operations are agent-managed. In 2026, no human should ever be paged first. The operator's labor is agent-multiplied, which means infrastructure has to be legible to agents (structured APIs, wide-event telemetry, deterministic deploys, reversible state). Bash-driven ops and click-through dashboards are dead ends. Huma+OpenAPI-everywhere + ClickHouse-wide-events thesis is exactly this

When you're making a more than $1B/year from your business, you can hire experts to manage a high-availability multi-region multi-cluster K8s infrastructure. Forge Metal is how you get there.

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

Use the helper below to move a seeded user's billing fixture state quickly when
you need to run end-to-end scenarios against a known plan tier or prepaid
balance:

```bash
DOMAIN="$(cd src/platform && awk -F'"' '/^forge_metal_domain:/{print $2}' ansible/group_vars/all/main.yml)"
make set-user-state EMAIL="ceo@${DOMAIN}" ORG=platform STATE=free
make set-user-state EMAIL="ceo@${DOMAIN}" ORG=platform STATE=hobby
make set-user-state EMAIL="ceo@${DOMAIN}" ORG=platform STATE=pro BALANCE_CENTS=10000
make set-user-state EMAIL=ceo@example.com ORG_ID=123 PLAN_ID=sandbox-pro BALANCE_UNITS=500000000 BUSINESS_NOW=2026-04-13T12:00:00Z
```

The helper is implemented at `src/platform/scripts/set-user-state.sh`. It builds
and runs `src/billing-service/cmd/billing-set-user-state` on the target node so
contract, cycle, entitlement, grant, clock override, and billing event rows use
the same ID rules as billing-service. It is an operator/test fixture helper, not
a customer API.

Useful overrides:

- `EMAIL` (required; written to `orgs.billing_email`)
- `ORG` or `ORG_ID` (required; `ORG=platform` resolves the platform billing org)
- `BILLING_PRODUCT_ID` (default: `sandbox`)
- `STATE` (`free`, `hobby`, `pro`, or another plan tier)
- `PLAN_ID` (exact plan id; `free`/`none` clears paid contracts)
- `BALANCE_UNITS` or `BALANCE_CENTS` (exact account purchase balance)
- `BUSINESS_NOW` (RFC3339/RFC3339Nano org-product billing clock override)
- `OVERAGE_POLICY`, `TRUST_TIER`, `ORG_NAME`

Use `billing-clock` when you want to move billing time without resetting the
user's contract or balances:

```bash
make billing-clock ORG_ID=123
make billing-clock ORG_ID=123 SET=2026-05-01T00:00:00Z REASON=e2e-rollover
make billing-clock ORG_ID=123 ADVANCE_SECONDS=2678400 REASON=e2e-rollover
make billing-clock ORG_ID=123 CLEAR=1 REASON=e2e-cleanup
```

The clock helper builds and runs `src/billing-service/cmd/billing-clock` on the
target node. It calls billing-service code paths against billing PostgreSQL, so
clock changes can synchronously apply due cycle rollover, scheduled
downgrades/cancellations, current-period grants, and corresponding
`billing_events`.

Billing naming is intentionally split:

- `BILLING_PRODUCT_ID=sandbox` is the product catalog/product-metering ID.
- `DB=billing` is the billing-service PostgreSQL database.
- `DB=sandbox_rental` is the sandbox-rental-service PostgreSQL database.
- `DB=sandbox` is a legacy billing database name and should not exist.

Use the PostgreSQL wrapper for direct inspection:

```bash
make pg-query DB=billing QUERY='SELECT count(*) FROM orgs'
make pg-query DB=billing QUERY='SELECT event_type, count(*) FROM billing_events GROUP BY event_type ORDER BY event_type'
make pg-query DB=sandbox_rental QUERY='SELECT count(*) FROM executions'
```

### 5. Log in

```bash
# Grafana keeps a local bootstrap admin for recovery; normal login uses Zitadel.
ssh ubuntu@<server-ip> 'sudo cat /etc/credstore/grafana/admin-password'
```

Open `https://dashboard.<domain>` for Grafana. Use `https://<ip>` only for
direct host access when DNS is not configured (self-signed cert for IP
addresses, auto Let's Encrypt for domains).

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

This project is open-source. Most bundled server components (ClickHouse,
TigerBeetle, Forgejo, PostgreSQL) use permissive or weak-copyleft licenses with
no network-interaction obligations.

**Grafana OSS** and **Stalwart Mail Server** are licensed under AGPL-3.0. If
you run upstream binaries unmodified (as pinned in `server-tools.json`), your
obligation is to provide users with source links:
`github.com/grafana/grafana` and `github.com/stalwartlabs/stalwart`.

Your own application code that talks to these services over HTTP/JMAP/SMTP/IMAP
remains a separate work. If you modify Grafana or Stalwart and provide the
modified services over a network, you must make those modifications available to
interacting users. Consult a lawyer for production licensing/compliance
obligations.
