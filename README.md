# Verself

Verself is two things:

1. Prima facie, a PaaS selling stateful, suspendable compute with near-serverless economics via fast-launching Firecracker VMs with hot-swappable filesystems via `zfs clone`.

2. The "golden image" of a self-contained self-replicating software company that can clone itself via an API call to `source-code-hosting-service`, which clones the repo with the user's configured company name, founder details, domain name, Resend API key, Stripe API key, etc. and uses all that to configure the repository for the caller, culminating in a download link. The user can then download their white-labelled clone configured for their providers and execute a shell script to bootstrap a replica of Verself for themselves. IOW: technology that converts any bare metal into structured, useful general purpose compute with economically valuable systems already set up for the user like auth, billing/payments, CI, observability, and a fully-functioning end-to-end revenue-generating product.

The main product offering is a (hopefully) better Blacksmith.sh: a GitHub App where we run customer's GitHub actions on our bare metal. Where we differ is that instead of distributed storage via Rook/Ceph + dmapper, we colocate stored golden zfs images custom GitHub action that replaces the standard `actions/checkout` with our custom checkout action that:

1. Runs the customer's workload on our bare metal, inside a firecracker VM that boots with a composed set of zvols mounted at user-configured directories + the repo's main branch checked out and ready before the CI job even starts. Once the CI Job starts, our custom `checkout` action applies the TIP of the `github.event.pull_request.head.sha` against the base branch.

2. When default branch's latest commit's CI goes green, we promote the repo file system post-CI (`GITHUB_WORKSPACE`) + durable VM directories outside the `GITHUB_WORKSPACE` such as installed binaries, Bazel caches, DB files, and so on. The next PR that executes CI will now have its VM mount its starting file-system from the golden ZFS volumes composed prior to the job running. 

2a. `getRepoZvolForPR` is approximately `(organization, project, repo, target-branch, workflow-id, job-id, matrix-key)` for GitHub. (Forgejo/Codeberg/GitLab support pending)

* Verself does not host customer applications as managed long-lived services (yet, but as you may imagine by the `verself` branding we are poised to do that soon via Open vSwitch or something similar). 

The software offerings are layered as follows:

a. Internal Core -- Infrastructure, bootstrap configuration, integration with 3p APIs, privileged processes
b. Services -- OpenAPI HTTP, SPIFFE mTLS for cross-service communication over public+internal APIs.
c. SDK -- Programmatic multi-language wrappers over our services
d. Clients -- websites, mobile apps, CLI. Call the SDK under the hood. See [`docs/verself-cli.md`](docs/verself-cli.md) for more on the CLI.

(Note on above, the structure is still WIP and we are at maybe 5% parity in terms of having even just a golang SDK over our services.)

The web app lives at `https://<domain>` (cnsole, public docs, and policy in one TanStack Start app). Public service APIs use per-service origins such as `https://billing.api.<domain>`, `https://sandbox.api.<domain>`, and `https://iam.api.<domain>`. Protocol origins include `git.<domain>`, `auth.<domain>`, `mail.<domain>`, and `dashboard.<domain>`. See [`docs/architecture/public-origins.md`](docs/architecture/public-origins.md).

Per-task documentation lives in `aspect <task> --help`.

## Quickstart

Choose the controller platform that is running the repo commands.

### Linux x86_64 controller

```bash
# 1. Toolchain (one time per controller).
./scripts/bootstrap-linux-amd64
bazelisk mod tidy
```

### macOS Apple Silicon controller

```bash
# 1. Toolchain (one time per controller).
./scripts/bootstrap-darwin-arm64
bazelisk mod tidy
```

### (optional) Cloning this repo onto your own infrastructure

```bash
# 2. Tell OpenTofu where to provision (one time per environment).
cp src/tools/provisioning/terraform/terraform.tfvars.example.json \
   src/tools/provisioning/terraform/terraform.tfvars.json
$EDITOR src/tools/provisioning/terraform/terraform.tfvars.json   # set project_id

# 3. Provision bare metal + render inventory.
aspect dev sops-init
aspect provision apply

# 4. Deploy. Idempotent; safe to repeat.
aspect deploy

# 5. Mint a persona env file and start working.
aspect persona assume platform-admin # Will become `verself cli 
```

`scripts/bootstrap-linux-amd64` and `scripts/bootstrap-darwin-arm64` are the only sanctioned shell scripts in the repo. Everything else is done through `aspect` and `bazelisk`. The two scripts just get any fresh developer/agent environment set up and good to at least start authenticating as a user and rocking and rolling.

- **bazelisk** — sha256-pinned download. Installed alongside a bazel → bazelisk symlink on PATH, so tools that invoke bazel directly (Aspect CLI's ctx.bazel.{build,test,run,query}, IDE plugins, rules_* scripts) resolve through bazelisk's version-pinned launcher.
- **aspect CLI** — sha256-pinned download. Hosts every task surface enumerated below.
- **vp (Vite+)** — owns `vp` / `vite` / `rolldown` / `vitest` invocation in the JS workspace at `~/.vite-plus/`. Uses `vp upgrade <version>` for catalog pinning.

Idempotent: short-circuits when the existing binary already matches the pinned sha256 / version. Falls back to `~/.local/bin` when the install directory is non-writable and `sudo` is unavailable, with a PATH warning. Set `BOOTSTRAP_INSTALL_DIR` to override the default `/usr/local/bin`.

## Aspect command map

`aspect` (no args) lists every group; `aspect <group>` lists its tasks; `aspect <task> --help` documents flags. The listing below mirrors the registration in [`.aspect/config.axl`](.aspect/config.axl).

### Top-level

| Task | Description |
| --- | --- |
| `aspect deploy` | Run the canonical deploy path from authored inputs (`--site`, `--sha`). |
| `aspect check` | Run a verification gate (`--kind=go-test\|go-vet\|go-lint\|conversions\|ansible\|supply-chain\|all`). |
| `aspect observe` | Discover or query telemetry (`--what catalog\|queries\|describe\|metric\|trace\|logs\|http\|service\|errors\|mail\|deploy\|supply-chain\|workload-identity\|temporal`). |
| `aspect detect-intrusions` | Scan `verself.host_auth_events` for accepted SSH sessions that bypassed Pomerium. |

### `aspect provision`

| Task | Description |
| --- | --- |
| `apply` | Provision bare metal through OpenTofu and write host inventory. |
| `destroy` | Destroy OpenTofu-managed bare metal and remove host inventory. |

### `aspect host`

| Task | Description |
| --- | --- |
| `edit-secrets` | Open encrypted host configuration secrets in `$EDITOR` via sops. |
| `firewall` | Converge host and owner-local nftables rulesets. |
| `operator-access-handoff` | Manually hand public SSH :22 to Pomerium and keep direct recovery on :2222. |

### `aspect integrations`

| Task | Description |
| --- | --- |
| `cloudflare-dns` | Reconcile Cloudflare DNS records from site integration inputs. |

Nomad fan-out is the deploy internal of `aspect deploy`. Host convergence,
OS security patching, guest-image staging, and external integration
reconciliation are explicit operator tasks.

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
The supply-chain policy is generated output and lives under
`src/host/supply-chain/__generated/policy.json` (gitignored).
Supply-chain checks regenerate it on demand if missing; rerun
`aspect artifacts inventory --format=policy --write-policy=src/host/supply-chain/__generated/policy.json`
after changing inventory inputs.

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
- Nomad-managed substrate migration: [`docs/architecture/nomad-managed-substrate-migration.md`](docs/architecture/nomad-managed-substrate-migration.md)
- Public origins: [`docs/architecture/public-origins.md`](docs/architecture/public-origins.md)
- Onboarding device or VM (operator SSH, Pomerium + Zitadel): [`docs/architecture/onboarding-device-or-vm.md`](docs/architecture/onboarding-device-or-vm.md)
- Identity and IAM (Zitadel, SCIM, three-role model, API credentials): [`src/platform/docs/identity-and-iam.md`](src/platform/docs/identity-and-iam.md)
- Workload identity (SPIFFE/SPIRE, OpenBao): [`docs/architecture/workload-identity.md`](docs/architecture/workload-identity.md)
- Billing architecture (TigerBeetle ledger, dual-write, Stripe webhooks): [`src/services/billing-service/docs/billing-architecture.md`](src/services/billing-service/docs/billing-architecture.md)
- VM execution control plane (sandbox-rental-service ↔ vm-orchestrator): [`src/services/sandbox-rental-service/docs/vm-execution-control-plane.md`](src/services/sandbox-rental-service/docs/vm-execution-control-plane.md)
- vm-orchestrator privilege boundary, Firecracker networking, jailer: [`src/substrate/vm-orchestrator/AGENTS.md`](src/substrate/vm-orchestrator/AGENTS.md)
- ZFS golden environment lifecycle (zvol, clone, snapshot, promote): [`src/substrate/vm-orchestrator/docs/zfs-volume-lifecycle.md`](src/substrate/vm-orchestrator/docs/zfs-volume-lifecycle.md)
- Wire contracts and DTO patterns: [`src/domain-transfer-objects/docs/wire-contracts.md`](src/domain-transfer-objects/docs/wire-contracts.md)
- Inbound mail (Stalwart, JMAP/SMTP, tenant isolation): [`src/services/mailbox-service/docs/inbound-mail.md`](src/services/mailbox-service/docs/inbound-mail.md)
- Audit data contract (HMAC chain, OCSF, SIEM export): [`src/services/governance-service/docs/audit-data-contract.md`](src/services/governance-service/docs/audit-data-contract.md)
- Secrets service (OIDC provider role, KMS alternative): [`src/platform/docs/secrets-service.md`](src/platform/docs/secrets-service.md)
- Agent workspace (QEMU/KVM, AI coding agent VMs): [`docs/architecture/agent-workspace.md`](docs/architecture/agent-workspace.md)
- Product direction: [`docs/product-direction.md`](docs/product-direction.md)
- System context (service topology, allowed third parties, billing, supply chain): [`docs/system-context.md`](docs/system-context.md)

## Licensing

This project is open-source MIT. Most bundled server components (ClickHouse, TigerBeetle, Forgejo, PostgreSQL) use permissive or weak-copyleft licenses with no network-interaction obligations.

**Grafana OSS** and **Stalwart Mail Server** are licensed under AGPL-3.0. If you run upstream binaries unmodified (as pinned in the substrate/devtools catalogs), your obligation is to provide users with source links: `github.com/grafana/grafana` and `github.com/stalwartlabs/stalwart`.

Your own application code that talks to these services over HTTP/JMAP/SMTP/IMAP remains a separate work. If you modify Grafana or Stalwart and provide the modified services over a network, you must make those modifications available to interacting users. Consult a lawyer for production licensing/compliance obligations.
