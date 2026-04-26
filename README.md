# Verself

Set of services + console + marketing page for a PaaS software business (that builds itself through dogfooding), almost entirely self-hosted on bare-metal with Forgejo, fast CI via Firecracker + deep ZFS optimizations, Grafana + ClickHouse observability (logs + traces + metrics), TigerBeetle for financial OLTP, Stripe integration, Zitadel for enterprise-grade auth, and PostgreSQL for general purpose RDBMS.

Features:

- Bootstrapping: single command to go from a laptop to a bare-metal instance with all services and frontends deployed.
- Git hosting + fast CI through Forgejo, Firecracker, and ZFS.
- Billing layered on top of Stripe and TigerBeetle so products can move from idea to revenue without rebuilding metering, transaction processing, tax, accounts receivable, dunning, and invoicing.
- Public surface split: `<domain>` for product docs/policy, `console.<domain>` for the authenticated browser console, `<service>.api.<domain>` for customer/SDK/CLI APIs, and protocol origins such as `git.<domain>`, `auth.<domain>`, `mail.<domain>`, and `dashboard.<domain>`.

## Quick Start

### 1. Install dev tools

```bash
make setup-dev
```

### 2. Provision bare metal

```bash
# Create your tfvars (one-time)
cp src/platform/terraform/terraform.tfvars.example.json src/platform/terraform/terraform.tfvars.json
# Edit terraform.tfvars.json — set project_id to your Latitude.sh project

# Provision server + generate Ansible inventory
make provision
```

This provisions a bare metal server via OpenTofu and auto-generates the gitignored `src/platform/ansible/inventory/hosts.ini` from the outputs. The Latitude.sh auth token is read from SOPS-encrypted secrets.

### 3. Deploy

```bash
make deploy
```

Idempotent, no wipe. Safe to run repeatedly. Deploy a single role with `--tags`:

```bash
make deploy TAGS=caddy
```

### 4. Seed and assume rehearsal personas

After deploy, seed the platform tenant, Acme tenant, billing state, mailboxes,
and auth fixtures:

```bash
make deploy PLAYBOOK=seed-system
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

`platform-admin` is our internal organization for dogfooding internal platform operations.

Use the helper below to move a seeded user's billing fixture state quickly when
you need to run end-to-end scenarios against a known plan tier or prepaid
balance:

```bash
DOMAIN="$(cd src/platform && awk -F'"' '/^verself_domain:/{print $2}' ansible/group_vars/all/main.yml)"
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
make billing-wall-clock ORG=platform REASON=e2e-cleanup
```

The clock helper builds and runs `src/billing-service/cmd/billing-clock` on the
target node. It calls billing-service code paths against billing PostgreSQL, so
clock changes can synchronously apply due cycle rollover, scheduled
downgrades/cancellations, current-period grants, and corresponding
`billing_events`. `billing-wall-clock` is the fixture repair path for browser
and operator testing: it clears the org/product clock override, voids current
test cycles that no longer overlap wall-clock time, preserves paid plan state
and account purchase balances, rematerializes current-period entitlements, and
emits `billing_clock_reset_to_wall_clock`.

Use the billing inspection wrappers when reviewing live state after a test:

```bash
make billing-state ORG=platform
make billing-documents ORG=platform
make billing-finalizations ORG=platform
make billing-events EVENT=billing_clock_reset_to_wall_clock MINUTES=30
make billing-pg-query QUERY='SELECT current_database()'
make billing-proof
```

`billing-proof` runs the deployed billing Playwright flow and writes artifacts
under `artifacts/console-billing/<run-id>/`. If the browser test exits before
it writes a structured run JSON, the wrapper still collects a time-windowed
fallback evidence bundle from ClickHouse and billing PostgreSQL.

Billing naming is intentionally split:

- `BILLING_PRODUCT_ID=sandbox` is the product catalog/product-metering ID.
- `DB=billing` is the billing-service PostgreSQL database.
- `DB=sandbox_rental` is the sandbox-rental-service PostgreSQL database.

Use the PostgreSQL wrapper for direct inspection:

```bash
make pg-query DB=billing QUERY='SELECT count(*) FROM orgs'
make pg-query DB=billing QUERY='SELECT event_type, count(*) FROM billing_events GROUP BY event_type ORDER BY event_type'
make pg-query DB=sandbox_rental QUERY='SELECT count(*) FROM executions'
```

Use `make stress` to burst parallel sandbox submissions through the public API
and land a real distribution (not one-shot evidence) in ClickHouse. The stress
target skips the full identity/billing reseed that `make sandbox-proof` runs
every time, so it finishes in the time the VMs actually take to boot and run:

```bash
make stress                                 # defaults: 200 submissions, 40-way parallel, echo workload
make stress SUBMISSIONS=50 PARALLEL=10      # smaller burst
make stress PROFILE=cpu-mem SUBMISSIONS=100 # 100 leases exercising cpu+memory
make stress PROFILE=disk SUBMISSIONS=100    # 100 leases writing/fsyncing to the rootfs
```

`PROFILE` accepts `echo`, `cpu`, `mem`, `disk`, or `cpu-mem`; the fine-grain
`SANDBOX_PROOF_WORKLOAD_*` env vars described in
`src/platform/scripts/verify-sandbox-public-api.sh` still work when a profile
preset isn't enough. Artifacts land under `artifacts/sandbox-public-api/<run-id>/`;
inspect the resulting span distributions with
`make clickhouse-query DATABASE=default QUERY='SELECT SpanName, quantile(0.5)(Duration/1e6), quantile(0.99)(Duration/1e6), max(Duration/1e6) FROM otel_traces WHERE Timestamp > now() - INTERVAL 30 MINUTE AND SpanName LIKE ''vmorchestrator.%'' GROUP BY SpanName'`.

### 5. Log in

```bash
# Grafana keeps a local bootstrap admin for recovery; normal login uses Zitadel.
ssh ubuntu@<server-ip> 'sudo cat /etc/credstore/grafana/admin-password'
```

Open `https://dashboard.<domain>` for Grafana. Use `https://<ip>` only for
direct host access when DNS is not configured (self-signed cert for IP
addresses, auto Let's Encrypt for domains).

The product docs and policies live at `https://<domain>`, and the authenticated
product console lives at `https://console.<domain>`. Public service APIs use
service-owned API origins such as
`https://billing.api.<domain>`, `https://sandbox.api.<domain>`, and
`https://identity.api.<domain>`. See
[`docs/architecture/public-origins.md`](docs/architecture/public-origins.md).

## Snapshot-Backed VM Farm

Verself's runtime direction is a checkpoint-backed Firecracker VM farm. CI,
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

- `.forgejo/workflows/ci.yml` - first Forgejo Actions tracer for `runs-on: verself`.
- `src/sandbox-rental-service/internal/jobs/` - customer execution state, billing, workflow/checkpoint policy.
- `src/sandbox-rental-service/migrations/` - PostgreSQL state machines for executions, VM segments, checkpoint refs, checkpoint versions, and save requests.
- `src/vm-orchestrator/` - privileged host daemon for Firecracker, TAP networking, ZFS clone/snapshot/destroy, and guest telemetry.
- `src/vm-orchestrator/vmproto/` - host/guest vsock protocol.
- `src/vm-orchestrator/cmd/vm-bridge/` - guest PID 1 and user-facing in-guest snapshot CLI.
- `src/vm-guest-telemetry/` - guest telemetry sampler.
- `src/viteplus-monorepo/apps/console/` - product console UI.

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
you run upstream binaries unmodified (as pinned in `src/platform/topology`), your
obligation is to provide users with source links:
`github.com/grafana/grafana` and `github.com/stalwartlabs/stalwart`.

Your own application code that talks to these services over HTTP/JMAP/SMTP/IMAP
remains a separate work. If you modify Grafana or Stalwart and provide the
modified services over a network, you must make those modifications available to
interacting users. Consult a lawyer for production licensing/compliance
obligations.
