# Verself

Set of services + console + marketing page for a PaaS software business (that builds itself through dogfooding), almost entirely self-hosted on bare-metal with Forgejo, fast CI via Firecracker + deep ZFS optimizations, Grafana + ClickHouse observability (logs + traces + metrics), TigerBeetle for financial OLTP, Stripe integration, Zitadel for enterprise-grade auth, and PostgreSQL for general purpose RDBMS.

Features:

- Bootstrapping: single command to go from a laptop to a bare-metal instance with all services and frontends deployed.
- Git hosting + fast CI through Forgejo, Firecracker, and ZFS.
- Billing layered on top of Stripe and TigerBeetle so products can move from idea to revenue without rebuilding metering, transaction processing, tax, accounts receivable, dunning, and invoicing.
- Public surface split: `<domain>` serves the unified product app — authenticated browser console, public docs, and policy in one TanStack Start app — with `<service>.api.<domain>` for customer/SDK/CLI APIs, and protocol origins such as `git.<domain>`, `auth.<domain>`, `mail.<domain>`, and `dashboard.<domain>`.

## Quickstart

```bash
# 1. Toolchain (one time per controller). Installs pinned Bazelisk + Aspect.
./scripts/bootstrap
bazelisk mod tidy

# 2. Tell OpenTofu where to provision (one time per environment).
cp src/provisioning-tools/terraform/terraform.tfvars.example.json \
   src/provisioning-tools/terraform/terraform.tfvars.json
$EDITOR src/provisioning-tools/terraform/terraform.tfvars.json   # set project_id

# 3. Provision bare metal + render inventory.
aspect dev sops-init
aspect provision apply

# 4. Deploy. Idempotent; safe to repeat.
aspect deploy

# 5. Mint a persona env file and start working.
aspect persona assume platform-admin
```

Run `aspect` (no args) to see the full task surface; `aspect <task> --help`
documents flags. Bazel graph maintenance lives under `aspect bazel ...`.

The authenticated product console, the public docs, and the policy tree all
live at `https://<domain>` in a single TanStack Start app. Public service APIs
use per-service origins such as `https://billing.api.<domain>`,
`https://sandbox.api.<domain>`, and `https://identity.api.<domain>`. See
[`docs/architecture/public-origins.md`](docs/architecture/public-origins.md).

## Personas

`aspect persona assume <name>` mints a short-lived, project-scoped Zitadel
token from the deployed credential store and writes a `0600` env file under
`smoke-artifacts/personas/`. `platform-admin` is the dogfooding org for
internal platform operations; `acme-*` are the customer rehearsal personas.

```bash
aspect persona assume platform-admin
aspect persona assume acme-admin
aspect persona assume acme-member
aspect persona assume platform-admin --output=/tmp/platform-admin.env
```

## Billing fixtures

`aspect persona user-state` parks a seeded user at a known plan tier or prepaid
balance for end-to-end scenarios. The helper builds and runs
`src/billing-service/cmd/billing-set-user-state` on the target node so contract,
cycle, entitlement, grant, clock override, and billing event rows use the same
ID rules as billing-service. It is an operator/test fixture, not a customer API.

```bash
DOMAIN="$(awk -F'"' '/^verself_domain:/{print $2}' src/host-configuration/ansible/group_vars/all/main.yml)"
aspect persona user-state --email="ceo@${DOMAIN}" --org=platform --state=free
aspect persona user-state --email="ceo@${DOMAIN}" --org=platform --state=pro --balance-cents=10000
aspect persona user-state --email=ceo@example.com --org-id=123 --plan-id=sandbox-pro \
    --balance-units=500000000 --business-now=2026-04-13T12:00:00Z
```

Useful flags: `--email` (required), `--org` or `--org-id` (required), `--state`,
`--plan-id`, `--balance-units` or `--balance-cents`, `--business-now`,
`--product-id` (default `sandbox`), `--overage-policy`, `--trust-tier`,
`--org-name`.

`aspect billing clock` moves billing time without resetting the user's contract
or balances. The `--wall-clock` form is the repair path for browser and
operator testing: it clears the override, voids current test cycles that no
longer overlap wall-clock time, preserves paid plan state and account purchase
balances, rematerializes current-period entitlements, and emits
`billing_clock_reset_to_wall_clock`.

```bash
aspect billing clock --org-id=123
aspect billing clock --org-id=123 --set=2026-05-01T00:00:00Z --reason=e2e-rollover
aspect billing clock --org-id=123 --advance-seconds=2678400 --reason=e2e-rollover
aspect billing clock --org-id=123 --clear --reason=e2e-cleanup
aspect billing clock --org=platform --wall-clock --reason=e2e-cleanup
```

Inspect live billing state after a test:

```bash
aspect billing state --org=platform
aspect billing documents --org=platform
aspect billing finalizations --org=platform
aspect billing events --event=billing_clock_reset_to_wall_clock --minutes=30
aspect db pg query --db=billing --query='SELECT current_database()'
aspect observe --what=service --service=billing-service
```

Use `aspect deploy` plus ClickHouse queries through `aspect observe` or
`aspect db ch query` for live completion evidence. The old handwritten
`verify-*` shell canaries were removed with the Nomad cutover.

Billing naming is intentionally split: `--product-id=sandbox` is the product
catalog/metering ID, `--db=billing` is the billing-service PostgreSQL database,
`--db=sandbox_rental` is the sandbox-rental-service database.

## Logging in

Normal browser login goes through Zitadel at `https://auth.<domain>`. Grafana
keeps a local bootstrap admin for recovery only:

```bash
ssh ubuntu@<server-ip> 'sudo cat /etc/credstore/grafana/admin-password'
```

Open `https://dashboard.<domain>` for Grafana. Use `https://<ip>` for direct
host access only when DNS is not configured (self-signed cert for IPs, auto
Let's Encrypt for domains).

## SSH to the bare metal

Public `:22` is closed. `sshd` binds to the wg-ops mesh and accepts only
short-lived certs signed by the OpenBao SSH CA — no static `authorized_keys`.

- **Onboard a device:** `aspect operator onboard --device=<name> --wg-address=<unused 10.66.66.X>`. Generates ed25519 + WireGuard keypairs locally, pulls trust anchors from `https://<domain>/.well-known/verself-*`, prints a topology YAML snippet for the trusted operator to PR, OIDCs to Zitadel, and writes `~/.ssh/config.d/verself.conf`. Cert valid 24h, periodic Vault token valid 14d (renewing) up to 30d (re-OIDC).
- **Day-to-day:** `aspect deploy` pre-flight calls `aspect operator refresh` silently every run — token renewed, cert re-signed, no prompt. Browser pops once per ~30d at the `token_explicit_max_ttl` boundary. Between deploys the 24h cert covers normal interactive work; if you go more than a day without deploying, run `aspect operator refresh` manually to mint a fresh cert from the still-valid Vault token.
- **Workload VMs (Devin, Cursor, CI):** `aspect operator enroll-workload` claims a wg-ops slot, mints a single-use 15-min AppRole secret-id, and prints an env block to inject. The VM runs `verself-workload-bootstrap` once at boot to trade the secret for a 24h SSH cert. No human OIDC.
- **Audit:** `aspect detect-intrusions` flags any accepted SSH event whose `cert_id` is outside the rendered trust set — every cert is stamped `verself-<principal>-<device-or-slot>` for direct attribution from `verself.host_auth_events`.

End-to-end design + failure modes: [docs/architecture/onboarding-device-or-vm.md](docs/architecture/onboarding-device-or-vm.md).

## Snapshot-Backed VM Farm

Verself's runtime direction is a checkpoint-backed Firecracker VM farm. CI,
direct shell execution, canaries, scheduled automation, and customer workloads
compile to the same execution model:

```text
checkpoint ref -> immutable checkpoint version -> writable zvol clone -> VM segment
```

The first product smoke test is a Postgres checkpoint demo: boot a VM from a large
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
- `src/viteplus-monorepo/apps/verself-web/` - product console + docs + policy UI (verself.sh apex).

Hard runtime boundaries:

- ZFS snapshots are immutable; customer-facing checkpoint refs are mutable.
- Guests may request `vm-bridge snapshot save <ref>`; they never send or receive host dataset paths.
- `vm-bridge` and `vm-guest-telemetry` are untrusted guest clienst; vm-orchestrator accepts checkpoint saves only for host-authorized refs.
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
you run upstream binaries unmodified (as pinned in the substrate/devtools
catalogs), your obligation is to provide users with source links:
`github.com/grafana/grafana` and `github.com/stalwartlabs/stalwart`.

Your own application code that talks to these services over HTTP/JMAP/SMTP/IMAP
remains a separate work. If you modify Grafana or Stalwart and provide the
modified services over a network, you must make those modifications available to
interacting users. Consult a lawyer for production licensing/compliance
obligations.
