# Verself

Set of services + console + marketing page for a PaaS software business (that builds itself through dogfooding), almost entirely self-hosted on bare-metal with Forgejo, fast CI via Firecracker + deep ZFS optimizations, Grafana + ClickHouse observability (logs + traces + metrics), TigerBeetle for financial OLTP, Stripe integration, Zitadel for enterprise-grade auth, and PostgreSQL for general purpose RDBMS.

The unified product app lives at `https://<domain>` (authenticated browser console, public docs, and policy in one TanStack Start app). Public service APIs use per-service origins such as `https://billing.api.<domain>`, `https://sandbox.api.<domain>`, and `https://identity.api.<domain>`. Protocol origins include `git.<domain>`, `auth.<domain>`, `mail.<domain>`, and `dashboard.<domain>`. See [`docs/architecture/public-origins.md`](docs/architecture/public-origins.md).

This README is a map. Per-task documentation lives in `aspect <task> --help`.

## Quickstart

```bash
# 1. Toolchain (one time per controller).
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

## Bootstrap

`scripts/bootstrap` is the only sanctioned shell script in the repo. Everything else routes through `aspect`. It pins the three bootstrap_pivot binaries that have to land before any Bazel- or Aspect-driven channel can run:

- **bazelisk** — sha256-pinned download. Symlinked to `/usr/local/bin/bazel` so the Aspect CLI's `ctx.bazel.{build,test,run,query}` (which spawn `bazel` directly) resolve through bazelisk's version-pinned downloader.
- **aspect CLI** — sha256-pinned download. Hosts every task surface enumerated below.
- **vp (Vite+)** — owns `vp` / `vite` / `rolldown` / `vitest` invocation in the JS workspace at `~/.vite-plus/`. Uses `vp upgrade <version>` for catalog pinning.

Idempotent: short-circuits when the existing binary already matches the pinned sha256 / version. Falls back to `~/.local/bin` when `/usr/local/bin` is non-writable and `sudo` is unavailable, with a PATH warning.

Versions of record live as constants at the top of `scripts/bootstrap`. The dev-tools catalog under `src/dev-tools/` is the version-of-record for everything else; `aspect dev install` lays those down.

## Aspect command map

`aspect` (no args) lists every group; `aspect <group>` lists its tasks; `aspect <task> --help` documents flags. The listing below mirrors the registration in [`.aspect/config.axl`](.aspect/config.axl).

### Top-level

| Task | Description |
| --- | --- |
| `aspect deploy` | Run the canonical deploy path from authored inputs (`--site`, `--sha`). |
| `aspect check` | Run a verification gate (`--kind=go-test\|go-vet\|go-lint\|conversions\|edge\|ansible\|voice\|supply-chain\|all`). |
| `aspect observe` | Discover or query telemetry (`--what catalog\|queries\|describe\|metric\|trace\|logs\|http\|service\|errors\|mail\|deploy\|supply-chain\|workload-identity\|temporal`). |
| `aspect detect-intrusions` | Scan `verself.host_auth_events` for accepted SSH sessions that bypassed Pomerium. |

### `aspect provision`

| Task | Description |
| --- | --- |
| `apply` | Provision bare metal through OpenTofu and write host inventory. |
| `destroy` | Destroy OpenTofu-managed bare metal and remove host inventory. |

### `aspect host-configuration`

| Task | Description |
| --- | --- |
| `edit-secrets` | Open encrypted host configuration secrets in `$EDITOR` via sops. |

Host convergence, OS security patching, guest-image staging, and Nomad fan-out
are deploy internals of `aspect deploy`.

### `aspect db`

| Task | Description |
| --- | --- |
| `pg list` | List PostgreSQL databases on the worker (authoritative via `\l`). |
| `pg shell` | Open interactive psql against a service database. |
| `pg query` | Run a SQL query against a service PostgreSQL database. |
| `ch query` | Run a ClickHouse query on the worker. |
| `ch schemas` | Print `CREATE TABLE` statements for every project ClickHouse table. |
| `tb shell` | Open the TigerBeetle REPL (`Ctrl+D` to exit). |
| `tb query-accounts` | Query TigerBeetle accounts through the official client over the operator SSH tunnel. |
| `tb lookup-account` | Lookup a TigerBeetle account by ID through the official client. |

### `aspect operator`

| Task | Description |
| --- | --- |
| `device` | Configure this checkout/device for Pomerium operator SSH and aspect commands. |
| `edge` | Validate or emit the derived public edge contract. |
| `platform` | Check or seed the dogfooded platform organization and source repository. |

`aspect operator device` is the entry point for getting a checkout (laptop or new dev VM) onto the host access plane through Pomerium + Zitadel. If the device key is passphrase-protected, load it into `ssh-agent` before running operator commands:

```bash
ssh-add ~/.ssh/id_ed25519
# macOS: ssh-add --apple-use-keychain ~/.ssh/id_ed25519
aspect db ch query --query="SELECT now()"
```

Use the existing founder/operator Zitadel login during the first Pomerium SSH sign-in; the device key is what becomes newly bound, not a separate human user.

End-to-end design and failure modes: [`docs/architecture/onboarding-device-or-vm.md`](docs/architecture/onboarding-device-or-vm.md).

### `aspect persona`

| Task | Description |
| --- | --- |
| `assume` | Write a persona env file: `aspect persona assume <platform-admin\|acme-admin\|acme-member>`. |
| `user-state` | Set billing fixture state for a persona (plan tier, balance, business-time override). |

`platform-admin` is the dogfooded internal org; `acme-*` are the customer rehearsal personas. Output env files land under `smoke-artifacts/personas/` with `0600` perms.

### `aspect billing`

| Task | Description |
| --- | --- |
| `seed` | Seed billing product catalog and a fixture org. |
| `clock` | Inspect or mutate billing business time (`--set`, `--advance-seconds`, `--clear`, `--wall-clock`). |
| `state` | Inspect billing state for an org. |
| `documents` | List billing documents for an org. |
| `finalizations` | List billing finalizations for an org. |
| `events` | Query recent billing events in ClickHouse. |

Naming is deliberately split: `--product-id=sandbox` is the product catalog/metering ID; `--db=billing` is the billing-service PostgreSQL database; `--db=sandbox_rental` is the sandbox-rental-service database.

### `aspect mail`

| Task | Description |
| --- | --- |
| `list` | List recent emails (defaults to agents inbox). |
| `accounts` | List synced mailbox accounts. |
| `mailboxes` | List mailboxes for an account (defaults to agents). |
| `read` | Read a specific email by ID (get IDs from `aspect mail list`). |
| `code` | Extract latest 2FA/verification code (defaults to agents). |
| `send` | Send via Resend (e.g. `--to=agents --subject=hello --body='...'`). |
| `passwords` | Print Stalwart mailbox passwords for ceo and agents. |

### `aspect artifacts`

Supply-chain admission and content-addressed artifact publishing.

| Task | Description |
| --- | --- |
| `publish` | Build and publish content-addressed Nomad artifacts to private Garage. |
| `inventory` | Inventory supply-chain install/fetch paths or render the artifact policy. |
| `evidence` | Assert deploy-time supply-chain rows and spans exist in ClickHouse. |
| `admission-evidence` | Assert artifact admission/install rows and spans exist in ClickHouse. |

Artifact admission and install verification are deploy-flow internals. The
operator-facing checks assert the ClickHouse evidence emitted by that flow.

### `aspect bazel`

| Task | Description |
| --- | --- |
| `gazelle` | Regenerate Bazel Go BUILD files via `gazelle update`. |
| `tidy` | Update Bzlmod repository wiring (`bazelisk mod tidy --lockfile_mode=update`). |
| `update` | Run `aspect bazel gazelle` then `aspect bazel tidy`. |

### `aspect dev`

| Task | Description |
| --- | --- |
| `install` | Install pinned controller development tools from the dev-tools catalog. |
| `sops-init` | Bootstrap SOPS + Age encryption. |
| `hooks-install` | Install repo git hooks via pre-commit. |
| `verself-web` | Start local verself-web dev tunnels and HMR server (console + docs + policy). |

### Aspect-built-in groups

| Group | Description |
| --- | --- |
| `aspect auth login\|logout\|whoami` | Aspect Workflows authentication. |
| `aspect axl add` | Add an AXL dependency to `MODULE.aspect`. |
| `aspect github token` | Mint Aspect-issued GitHub tokens. |
| `aspect delivery` | Aspect Workflows delivery (CI-only; deduplicated by action digest per commit). |
| `aspect build`/`test`/`lint`/`format` | Aspect-default Bazel passes. |

## Architecture references

High-signal documents to read directly:

- Repo layout: [`docs/architecture/directory-structure.md`](docs/architecture/directory-structure.md)
- Public origins and edge contract: [`docs/architecture/public-origins.md`](docs/architecture/public-origins.md)
- Onboarding device or VM (operator SSH, Pomerium + Zitadel): [`docs/architecture/onboarding-device-or-vm.md`](docs/architecture/onboarding-device-or-vm.md)
- Identity and IAM (Zitadel, SCIM, three-role model, API credentials): [`src/platform/docs/identity-and-iam.md`](src/platform/docs/identity-and-iam.md)
- Workload identity (SPIFFE/SPIRE, OpenBao): [`docs/architecture/workload-identity.md`](docs/architecture/workload-identity.md)
- Billing architecture (TigerBeetle ledger, dual-write, Stripe webhooks): [`src/billing-service/docs/billing-architecture.md`](src/billing-service/docs/billing-architecture.md)
- VM execution control plane (sandbox-rental-service ↔ vm-orchestrator): [`src/sandbox-rental-service/docs/vm-execution-control-plane.md`](src/sandbox-rental-service/docs/vm-execution-control-plane.md)
- vm-orchestrator privilege boundary, Firecracker networking, jailer: [`src/vm-orchestrator/AGENTS.md`](src/vm-orchestrator/AGENTS.md)
- ZFS volume lifecycle (zvol, clone, snapshot, checkpoint, restore): [`src/vm-orchestrator/docs/zfs-volume-lifecycle.md`](src/vm-orchestrator/docs/zfs-volume-lifecycle.md)
- Wire contracts and DTO patterns: [`src/domain-transfer-objects/docs/wire-contracts.md`](src/domain-transfer-objects/docs/wire-contracts.md)
- Inbound mail (Stalwart, JMAP/SMTP, tenant isolation): [`src/mailbox-service/docs/inbound-mail.md`](src/mailbox-service/docs/inbound-mail.md)
- Audit data contract (HMAC chain, OCSF, SIEM export): [`src/governance-service/docs/audit-data-contract.md`](src/governance-service/docs/audit-data-contract.md)
- Secrets service (OIDC provider role, KMS alternative): [`src/platform/docs/secrets-service.md`](src/platform/docs/secrets-service.md)
- Agent workspace (QEMU/KVM, AI coding agent VMs): [`docs/architecture/agent-workspace.md`](docs/architecture/agent-workspace.md)
- Product direction: [`docs/product-direction.md`](docs/product-direction.md)
- System context (service topology, allowed third parties, billing, supply chain): [`docs/system-context.md`](docs/system-context.md)

## Licensing

This project is open-source MIT. Most bundled server components (ClickHouse, TigerBeetle, Forgejo, PostgreSQL) use permissive or weak-copyleft licenses with no network-interaction obligations.

**Grafana OSS** and **Stalwart Mail Server** are licensed under AGPL-3.0. If you run upstream binaries unmodified (as pinned in the substrate/devtools catalogs), your obligation is to provide users with source links: `github.com/grafana/grafana` and `github.com/stalwartlabs/stalwart`.

Your own application code that talks to these services over HTTP/JMAP/SMTP/IMAP remains a separate work. If you modify Grafana or Stalwart and provide the modified services over a network, you must make those modifications available to interacting users. Consult a lawyer for production licensing/compliance obligations.
