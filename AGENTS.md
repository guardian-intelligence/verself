# forge-metal

Free Open-Source Software for a turnkey "software company in a box": fully self-hosted bare-metal platform with Forgejo, Fast CI via Firecracker + deep ZFS optimizations, ClickStack observability (logs + traces + metrics), TigerBeetle for financial OTLP, Stripe integration, Zitadel for enterprise-grade auth, PostgreSQL for general purpose RDBMs. This is not a PaaS -- the user owns what they deploy.

Bootstrapping UX: single command to go from their laptop -> bare metal instance -> all services + 2 deployed frontend apps reading/writing off the same DB (frontends not yet implemented).

## Deployment Topology

Single-node is the default deployment. Everything runs on one box with no replication. Adding two more nodes (3 total) enables TigerBeetle consensus replication, ClickHouse ReplicatedMergeTree, Postgres streaming replication, and cross-node health monitoring with external paging. The single-node path is what we're currently working on and we will provide in the future a path to seamlessly upgrade to a three node topology with Netbird as the overlay.

Hard product design requirement: everything must be self-hosted.

Exceptions:

Optional - Backblaze B2, Cloudflare R2, AWS S3 for backups (will be done through `zfs send`, not LINSTOR + DRBD) [Backups not yet implemented]
Required - Domain Registar (Cloudflare only for now)
Required - Compute Provider (Latitude.sh only for now)
Required - Email Delivery (Resend only for now)

## Supply Chain Management

* Git repos (including this one) are hosted on the deployed Forgejo instance at git.<domain_name>.com
* We 

## Context

Key focus areas for this project

* Secure by default, above and beyond most SaaS provided options. Security must be regularly audited and verified (still working on this)
* Cheap -- you only pay for compute and object storage which are commodity priced.
* [aspirational, not yet fully implemented] Solves genuinely difficult problems faced by businesses - Lowering a price for a product should be easy and fast: when the oeprator of the company reduces the price of a metered product, customer billing pages should update, marketing pages' pricing sections should update, emails should go out to customers, end-of-month invoices should reflect usage at both old and new prices, metering should update at a specified effective_at field, customer support agents (not yet implemented) should be able to answer questions and query safe tables to pull information about recent price changes and the customer's spend history that may have impacted them. All of this should happen seamlessly via a combination of maintaining a robust system of record and deterministic workflows.

- homestead-smelter is a guest agent + host Firecracker mVM agent written in Zig. The guest agent collects heartbeat health diagnostics and runs on each Firecracker VM, streaming data up continuously to the host agent, which then writes data to a socket for consumers.
- Our current working bare metal box is available at `ssh ubuntu@64.34.84.75`
- Auth: Zitadel
- Payments: Stripe + TigerBeetle + PostgreSQL
- otelcol-config.yaml.j2 contains a lot of our custom otel collection config.

* You can run `make clickhouse-schemas` to read all of our ClickHouse tables, which contains a lot of useful ground truth.

* Less important but useful if editing instructions: ./claude/CLAUDE.md is symlinked from AGENTS.md

## CI Architecture

See README.md for more -- the repo started as a CI orchestrator but has since evolved.

## Quick Start

### 1. Install dev tools

```bash
cd ansible && ansible-playbook playbooks/setup-dev.yml
```

### 2. Provision bare metal

```bash
# Create your tfvars (one-time)
cp terraform/terraform.tfvars.example.json terraform/terraform.tfvars.json
# Edit terraform/terraform.tfvars.json — set project_id to your Latitude.sh project

# Provision server + generate Ansible inventory
cd ansible && ansible-playbook playbooks/provision.yml
```

This provisions a bare metal server via OpenTofu and auto-generates `ansible/inventory/hosts.ini` from the outputs. The Latitude.sh auth token is read from SOPS-encrypted secrets.

### 3. Deploy

```bash
cd ansible && ansible-playbook playbooks/dev-single-node.yml \
  -e nix_server_profile_path=$(nix build .#server-profile --no-link --print-out-paths)
```

Idempotent, no wipe. Safe to run repeatedly. Deploy a single role with `--tags`:

```bash
cd ansible && ansible-playbook playbooks/dev-single-node.yml \
  -e nix_server_profile_path=... --tags caddy
```

### 4. Log in

```bash
# HyperDX admin credentials are in the SOPS-encrypted secrets file
sops -d --extract '["hyperdx_admin_email_slug"]' ansible/group_vars/all/secrets.sops.yml
sops -d --extract '["hyperdx_admin_password_base"]' ansible/group_vars/all/secrets.sops.yml
# Email: admin+{slug}@forge-metal.local, Password: {base}#@F1
```

Open `https://<ip>` in your browser (self-signed cert for IP addresses, auto Let's Encrypt for domains).

### 5. Query ClickHouse

Use the repo wrapper instead of typing the SSH and password prefix by hand. It resolves the worker from `ansible/inventory/hosts.ini`, reads the ClickHouse password from SOPS, and invokes the stable worker path `/opt/forge-metal/profile/bin/clickhouse-client`.

```bash
make clickhouse-query QUERY='SHOW TABLES' DATABASE=forge_metal
make clickhouse-shell
./scripts/clickhouse.sh --query 'SELECT count() FROM otel_logs'
```

### TLS with a real domain (Cloudflare)

```bash
make setup-domain DOMAIN=anveio.com
cd ansible && ansible-playbook playbooks/dev-single-node.yml \
  -e nix_server_profile_path=$(nix build .#server-profile --no-link --print-out-paths)
```

Services get subdomains automatically:

| Subdomain | Service |
|-----------|---------|
| `admin.<domain>` | ClickStack dashboard |
| `git.<domain>` | Forgejo |
| `auth<domain>` | Zitadel |

### Nix Server Profile

All server software (Caddy, Forgejo, ClickHouse, etc.) is packaged in a single Nix closure (`flake.nix` -> `server-profile`). This means:

- **Reproducible**: `flake.lock` pins every dependency transitively. Same lock = same binaries, always.
- **Fast deploys**: The closure is pushed once over SSH. No apt repos, no GitHub downloads, no `yarn install`.
- **Atomic updates**: `nix flake update` + deploy upgrades everything. Rollback = `git revert flake.lock`.
- **No apt, no get_url, no build-from-source at deploy time** (except HyperDX, which builds from source using Nix-provided Node.js).

The only `apt install` that remains is `zfsutils-linux` (kernel-dependent, must match running kernel).

### Version Updates

```bash
nix flake update                                              # update all packages
cd ansible && ansible-playbook playbooks/dev-single-node.yml \
  -e nix_server_profile_path=... --limit canary               # deploy to one node
cd ansible && ansible-playbook playbooks/site.yml \
  -e nix_server_profile_path=...                              # roll out fleet-wide
```

### Architecture

```
nix build .#server-profile --> content-addressed closure (~2GB)
nix copy --to ssh://host   --> push closure to bare metal
Ansible                    --> configure + enable services
```

| Component | Port | Purpose |
|-----------|------|---------|
| Caddy | 443, 80 | Reverse proxy with automatic TLS |
| Forgejo | 3000 | Git server + CI runner |
| Verdaccio | 4873 | Sealed npm registry mirror |
| ClickHouse | 9000, 8123 | Wide event storage with optimized codecs |
| HyperDX | 8080, 8000 | Observability UI + API |
| OTel Collector | 4317, 4318 | OTLP telemetry ingestion |
| MongoDB | 27017 | HyperDX app state |

### Wide Events

Every CI job produces one denormalized row in `ci_events` with ~50 columns. No JOINs needed.

Compression codecs per column type:
- Timestamps: `DoubleDelta + ZSTD(3)`
- Durations (Int64): `Delta(8) + ZSTD(3)`
- Byte counters: `T64 + ZSTD(3)`
- Low-cardinality strings: `LowCardinality + ZSTD(3)`
- Floats: `Gorilla + ZSTD(3)`

## Makefile Targets (local only)

| Target | Description |
|--------|-------------|
| `make build` | Build the `forge-metal` Go binary locally |
| `make test` | Run Go tests |
| `make test-integration` | Run all tests including ZFS integration (requires sudo + zfs) |
| `make lint` | Run golangci-lint |
| `make lint-ansible` | Run ansible-lint on playbooks and roles |
| `make fmt` | Format Go code with gofumpt |
| `make vet` | Run go vet |
| `make tidy` | Run go mod tidy |
| `make doctor` | Check that all required dev tools are present and at the right version |
| `make setup-domain` | Configure Cloudflare domain (interactive wizard) |
| `make server-profile` | Build Nix server profile (golden image closure) |
| `make smelter-build` | Build homestead-smelter Zig host/guest binaries locally |
| `make clickhouse-shell` | Open an interactive clickhouse-client session on the worker |
| `make clickhouse-query` | Run a ClickHouse query on the worker |
| `make clickhouse-schemas` | Print CREATE TABLE statements for all project tables |
| `make edit-secrets` | Open encrypted secrets in $EDITOR via sops |

## Ansible Playbooks

All remote orchestration is done via Ansible playbooks. Run from the `ansible/` directory.

| Playbook | Description |
|----------|-------------|
| `playbooks/setup-dev.yml` | Install pinned dev tools from dev-tools.json |
| `playbooks/setup-sops.yml` | Bootstrap SOPS+Age encryption for secrets |
| `playbooks/provision.yml` | Provision bare metal via OpenTofu, generate inventory |
| `playbooks/deprovision.yml` | Destroy bare metal infrastructure, remove inventory |
| `playbooks/dev-single-node.yml` | Deploy to single node (idempotent) |
| `playbooks/site.yml` | Deploy to multi-node cluster (workers + infra) |
| `playbooks/guest-rootfs.yml` | Build guest rootfs and stage CI artifacts |
| `playbooks/hyperdx-dashboards.yml` | Sync HyperDX dashboards without full redeploy |
| `playbooks/ci-fixtures.yml` | Run CI fixture suites |
| `playbooks/ci-fixtures-pass.yml` | Run positive fixture suite |
| `playbooks/ci-fixtures-fail.yml` | Run negative fixture suite |
| `playbooks/ci-fixtures-full.yml` | Refresh artifacts, then run pass + fail suites |
| `playbooks/smelter-dev.yml` | Hot-swap smelter guest, boot + probe in Firecracker VM (~10s) |
| `playbooks/security-patch.yml` | Rolling OS security updates |
| `playbooks/mirror-update.yml` | Update and scan Verdaccio mirror |

All deploy playbooks support `--tags` for targeting individual roles (e.g. `--tags caddy`, `--tags clickhouse`). Preflight checks run regardless of tag selection.

## Developing homestead-smelter

`smelter-dev.yml` is the fastest way to test homestead-smelter guest changes. It provides a ~10 second edit-test loop by hot-swapping the Zig binary into a dev golden zvol, bypassing the full rootfs rebuild (~90s).

### What it does

The playbook is self-contained — it builds the Zig binary locally, uploads it, then:

1. Clones `forgepool/golden-zvol@ready` to a temporary `smelter-dev-zvol`
2. Mounts the clone, replaces `/usr/local/bin/homestead-smelter-guest`, unmounts
3. Snapshots as `smelter-dev-zvol@ready`
4. Boots a Firecracker VM from the dev zvol via `forge-metal firecracker-test`
5. Waits for the VM's vsock bridge socket to appear
6. Waits for `homestead-smelter-host check-live` to observe the VM
7. Prints `homestead-smelter-host snapshot` output for the live VM
8. Prints PASS/FAIL, waits for VM exit, destroys the dev zvol

### Prerequisites

The server must have been deployed at least once with a valid golden image:

```bash
cd ansible && ansible-playbook playbooks/guest-rootfs.yml
cd ansible && ansible-playbook playbooks/dev-single-node.yml \
  -e nix_server_profile_path=$(nix build .#server-profile --no-link --print-out-paths)
```

### Usage

```bash
# Edit homestead-smelter/src/guest.zig, then:
cd ansible && ansible-playbook playbooks/smelter-dev.yml
```

Expected output on success:

```
→ building homestead-smelter guest (zig)
→ uploading guest binary
→ running smelter dev playbook
→ dev golden ready: forgepool/smelter-dev-zvol@ready
HELLO job_id=<job-id> stream_generation=3 host_seq=8 guest_seq=0 boot_id=<boot-id> mem_total_kb=2039556
SAMPLE job_id=<job-id> stream_generation=3 host_seq=100 guest_seq=92 mem_available_kb=1935768 cpu_user_ticks=0
SNAPSHOT_END host_seq=101
PASS: host agent observed live guest telemetry
```

### How it compares to other targets

| Playbook | Time | When to use |
|----------|------|-------------|
| `smelter-dev.yml` | ~10s | Iterating on guest Zig code |
| `guest-rootfs.yml` | ~90s | Changed forgevm-init, Alpine packages, or kernel |
| `ci-fixtures-pass.yml` | ~3-5min | Re-run the positive fixture suite against the current host |
| `ci-fixtures-fail.yml` | ~3-5min | Re-run the negative fixture suite against the current host |
| `ci-fixtures-full.yml` | ~5min+ | Refresh guest artifacts, then run pass and fail suites together |

## Project Structure

```
forge-metal/
├── cmd/forge-metal/       # CLI entry point (doctor, setup-domain, Firecracker CI, fixture suites)
├── cmd/forgevm-init/      # PID 1 inside Firecracker VMs (mounts, network, fork+exec)
├── internal/
│   ├── clickhouse/        # ClickHouse client, wide event struct
│   ├── cloudflare/        # Cloudflare API client (DNS, zone lookup)
│   ├── config/            # Layered TOML config
│   ├── doctor/            # Dev environment health checks
│   ├── domain/            # Domain setup wizard
│   ├── ci/                # Repo goldens, toolchain detection, Forgejo fixture e2e
│   ├── firecracker/       # Firecracker orchestrator (Go reference impl, used by forge-metal)
│   ├── latitude/          # Latitude.sh API client
│   ├── prompt/            # Shared Prompter interface + TTY implementation
│   └── provision/         # Server provisioning logic
├── ansible/
│   ├── playbooks/         # All orchestration: deploy, provision, CI fixtures, smelter-dev
│   └── roles/
│       ├── nix_deploy/    # Install Nix + push server profile closure
│       ├── base/          # System config (ZFS, users, npm registry, sudoers)
│       ├── guest_rootfs/  # Build Firecracker guest rootfs (local compile + remote build)
│       ├── deploy_ci_artifacts/ # Stage built guest artifacts to /var/lib/ci/
│       ├── cloudflare_dns/ # Cloudflare DNS A record management
│       ├── clickhouse/    # ClickHouse config + schema bootstrap
│       ├── otelcol/       # OTLP ingestion and export to ClickHouse
│       ├── hyperdx/       # HyperDX UI/API plus MongoDB-backed app state
│       ├── hyperdx_dashboards/ # HyperDX sources and dashboard synchronization
│       ├── caddy/         # Edge proxy and TLS
│       ├── zfs/           # Pool creation, golden/ci datasets
│       ├── firecracker/   # KVM, jailer user, golden zvol, CI dataset
│       ├── containerd/    # containerd + gVisor runsc (config only)
│       ├── verdaccio/     # Sealed npm registry mirror (config only)
│       ├── wireguard/     # Mesh networking (config only)
│       └── forgejo/       # Git server + CI runner (config only)
├── scripts/
│   └── build-guest-rootfs.sh # Alpine rootfs builder for the generic guest image
├── ci/
│   └── versions.json      # Pinned Alpine + Firecracker versions with SHA256
├── terraform/             # Latitude.sh provisioning
├── migrations/            # ClickHouse schema (MergeTree + Replicated)
├── internal/config/default.toml # Embedded defaults
├── dev-tools.json         # Pinned dev tool versions, URLs, SHA256 (read by Ansible + doctor)
└── flake.nix              # Server profile only (Nix builds deployed to bare metal, not for dev)
```

## Firecracker CI Status

The current end-to-end proof is the controlled fixture suite under `test/fixtures/`, executed via internal Forgejo Actions and `forge-metal ci warm/exec`.

### Current platform decisions

- `forge-metal` is the current primary, and soon to be replaced, Go binary for Forgejo + Firecracker CI execution.
- Keep the guest base image generic and boring: Node/Next.js-capable substrate only (Node, corepack, pnpm, Bun, git, certs, common service binaries). Do not create a distinct base image per repo or per package manager version.
- Put the optimization boundary at the **repo golden image**, not the base image. For each repo, do one cold bootstrap on the default branch in the same Firecracker environment used for CI, then snapshot that warmed state as the golden image.
- Use ZFS zvol clones + Firecracker as the only copy-on-write strategy. Do not add OverlayFS layering on top.
- Prefer a layered model conceptually: generic guest substrate + repo-specific golden + optional service state derived from the repo's default branch.
- Treat package manager/toolchain detection as a routing problem, not an image explosion problem. Resolve package manager/version from repo metadata (`package.json.packageManager`, lockfiles, and standard version files), then activate the toolchain inside a small set of generic base images.
- Support heuristics with explicit override. Auto-detect package manager, monorepo root, and common Node/Next.js signals, but keep a minimal manifest/override path for working directory, services, env, and install/build/test overrides.
- Run requested services inside the same VM first. Sidecar VMs may come later; host-level shared databases/services are out of scope for untrusted CI.
- For database-backed projects, the warm path should snapshot default-branch database state and apply only branch deltas at job time. Do not hardcode a single app-specific seed path into infrastructure.
- Keep git local. Use internal Forgejo and local mirrors/fetches for repeatable tests rather than pulling live upstream repos into the verification path.
- Define "little to no custom glue" strictly: workflow file and minimal manifest are acceptable; patching app source, hardcoded repo branches in infra, inline app-specific env hacks, and explicit helper-script calls from project workflows are not.
- Verification for this phase: seed four controlled fixtures into internal Forgejo, cold-bootstrap each on `main`, snapshot each as a golden image, open a small PR, and prove the follow-up CI run succeeds from the golden path with no repo source patching.

## Assistant Contract

* Ground proposals, plans, API references, and all technical discussion in primary sources.
* When proposing solutions, think from the perspective of the user of the system. The users of this repo will be sole operators of a single-person software company operating all services off a single bare metal box.
* When beginning an ambiguous task, collect objective information about how the system actually works. There are a lot of technologies being stitched together so its important to understand how everything connects.
* You are expected to push back on poor technical decisions. Technical decisions are poor when they couple too much to a specific workflow (e.g. hardcoding Postgres in every Firecracker VM), attempt to use technology in ways its not meant to be used (e.g. using Nix inside of a firecracker VM)
* Act as a dispassionate advisory technical leader with a focus on elegant public APIs and functional programming. 
* You may be asked series of questions. Not all questions need to be answered individually. Consider the gestalt of the discussion and take a step back and address the core question underneath the questions.
* You are not alone in this repo. Expect parallel changes in unrelated files by the user.
* This repo is currently private and serves no customers or users. There is no backwards compatibility to maintain. This means: no compatibility wrappers, no legacy shims, no temporary plumbing. All changes must be performed via a full cutover. 
* Ensure old or outdated code is deleted each time we upgrade technology, abstractions, or logic. Eliminating contradictory approaches is a high priority.

## Tool Use Contract

* When executing long-running tasks, execute them in the background and check in every 30 - 60 seconds.
* Dev tools are system-installed via `ansible-playbook playbooks/setup-dev.yml`. No `nix develop` prefix needed.
* Apply the scientific method: create a bar-raising verification protocol for your planned task *prior* to impelementing changes. The verification protocol should fail, and only then begin implementing until green.
* Avoid using one off non-syntax-aware scripts to do large parallel changes or refactors. Use subagents for that class of tasks instead as unexpected edge cases are likely and judgement is often required.

## Output Contract

* When providing a recommendation, consider different plausible options and provide a differentiated recommendation that leans towards a simpler solution that best fits the long term goal of this project.
* Speculating that your code changes work as expected is not allowed. Unit tests and successful builds are low signal and are not to be trusted. Real observability traces in ClickHouse that exercise your modified code is the only admitted proof of code task-completion. ClickHouse currently exists for the purpose of producing verifiable completion artifacts. If a new schema is needed, you are permitted to create one.
* Do not speculate about host-level causes (resource exhaustion, network issues, etc.) without evidence. Logs, traces, and host metrics are queryable in ClickHouse via `make clickhouse-query` — check them before attributing failures to environmental factors.
* Do not stop work short of verifying your changes with a live rehearsal of our CI infrastructure with fresh rebuild and redeploy.
* The repo has a fixture flow that seeds Forgejo repos, warms their goldens, opens PRs, and waits for CI.
* When writing design documents, code comments, system architecture diagrams, API documentation, or any other kind of technical writing, ensure that the writing style targets the following audience: distinguished engineers that are experts in the relevant technologies but mostly just need information on how the system being described is different or deviates from standard practice. Avoid throat-clearing, get straight into the information.
* When editing byte-layouts, avoid piecemeal edits as that's how you end up with contradictions.

## Coding Contract

* Prefer Ansible over shell scripts, except in extreme bootstrap cases.
* Ansible playbook files must have a newline at the end. This will be caught by `ansible-lint`.
* Treat errors as data. Use tagged and structured errors to aid in control flow.
* Avoid fallbacks and defaults in Ansible code. Ansible should fail fast with useful logging.
* Remember the philosophy that tests will never be able to assert that a system works correctly. They only assert the absence of some set of bugs. Prefer fewer high-signal top-contour tests and pair happy-path tests with sad-path tests to improve the signal of both sides.
* Package management for python must be done with `uv` do not use pip or conda.
* Don't resolve failures through silent no-ops and imperative checks. Failures should be loud and signals should be followed in order to address root causes.
* ClickHouse inserts must use `batch.AppendStruct` with `ch:"column_name"` struct tags. Never use positional `batch.Append` — it silently corrupts data when columns are added or reordered.
* ClickHouse queries must pass dynamic values (including Map keys) through driver parameter binding (`$1`, `$2`, ...); never interpolate values into query strings with `fmt.Sprintf` — use `arrayElement(map_col, $N)` instead of `map_col['{interpolated}']`.
* ClickHouse schema design: ORDER BY columns are sorted on disk and control compression — order keys by ascending cardinality (low-cardinality columns first). Avoid `Nullable` (it adds a hidden UInt8 column per row); use empty-value defaults instead. Use `LowCardinality(String)` for columns with fewer than ~10k distinct values. Use the smallest sufficient integer type (UInt8 over Int32 when the range fits).
