# forge-metal

Free Open-Source Software for a turnkey "software company in a box": fully self-hosted bare-metal platform with Forgejo, Fast CI via Firecracker + deep ZFS optimizations, ClickStack observability (logs + traces + metrics), TigerBeetle for financial OTLP, Stripe integration, Zitadel for enterprise-grade auth, PostgreSQL for general purpose RDBMs. This is not a PaaS -- the user owns what they deploy.

Bootstrapping UX: turnkey from 0 -> bare metal instance -> all services + 2 deployed frontend apps reading/writing off the same DB (frontends not yet implemented).

Hard product design requirement: everything must be self-hosted.

Exceptions:

Optional - Backblaze B2, Cloudflare R2, AWS S3 for backups (done through `zfs send`, not LINSTOR + DRBD)
Required - Domain Registar (Cloudflare only for now)
Required - Compute Provider (Latitude.sh only for now)
Required (not implemented) - Email Delivery (Resend only in the future)

## Context

- homestead-smelter is a guest agent + host Firecracker mVM agent written in Zig. The guest agent collects heartbeat health diagnostics and runs on each Firecracker VM, streaming data up continuously to the host agent, which then writes data to a socket for consumers.
- Our current working bare metal box is available at `ssh ubuntu@64.34.84.75`
- Auth: Zitadel
- Payments: Stripe + TigerBeetle + PostgreSQL
- otelcol-config.yaml.j2 contains a lot of our custom otel collection config.

## CI Architecture

The optimization stack is, at a high level:

- keep a repo-specific golden image of `main` with warmed caches
- zfs for instant copy of the warmed golden image
- local Forgejo fetches and mirrors for deterministic refs
- warm default-branch database state when a fixture requests Postgres
- turbo cache locally when the repo uses it

Each CI job runs in a Firecracker microVM whose root disk is a ZFS zvol clone of a golden image. The host orchestrator manages ZFS at the kernel level; guests have no awareness of ZFS.

```
Host (CI Orchestrator — bare metal, root, ZFS kernel module)
│
├── ZFS Pool (NVMe-backed)
│   ├── golden-zvol@ready                 ← golden image: ext4 inside zvol, warm caches, node_modules
│   ├── ci/job-abc                        ← zvol clone (~1.7ms COW, metadata-only)
│   ├── ci/job-def                        ← zvol clone
│   └── ci/job-ghi                        ← zvol clone
│
├── Firecracker VM (job-abc)
│   └── /dev/vda ← /dev/zvol/pool/ci/job-abc
│       └── ext4 (from golden image, COW diverges on write)
│
├── Firecracker VM (job-def)
│   └── /dev/vda ← /dev/zvol/pool/ci/job-def
│
└── Firecracker VM (job-ghi)
    └── /dev/vda ← /dev/zvol/pool/ci/job-ghi
```

### Why this layering

| Layer | Provides | Latency |
|-------|----------|---------|
| ZFS zvol clone | Instant COW rootfs from golden image | ~1.7ms kernel, ~5.7ms end-to-end |
| Firecracker microVM | Process/memory/kernel isolation, deterministic execution | ~125ms from snapshot, ~3s cold boot |
| gVisor (inside VM) | Syscall-level sandboxing for untrusted build scripts | Negligible on top of Firecracker |

### Key distinctions

- **zvol, not dataset.** Firecracker takes block devices (`/dev/zvol/...`), not mounted filesystems. A zvol is a ZFS block device — clone/destroy/written all work identically to datasets.
- **Golden image is a zvol with ext4 inside.** Built from the generic guest image produced by `scripts/build-guest-rootfs.sh`, then refreshed by the `forge-metal ci warm` path into repo-specific goldens.
- **Guest is unaware of ZFS.** It sees `/dev/vda` with ext4. No ZFS tooling needed in the VM image.
- **Orchestrator owns all ZFS operations.** Allocate (clone), monitor (`written` bytes), teardown (destroy). Implemented in the `forge-metal` Go binary.

### Orchestrator flow per job

```
1. zfs clone pool/golden-zvol@ready pool/ci/job-abc         # ~1.7ms
2. firecracker --drive path=/dev/zvol/pool/ci/job-abc        # boot VM
3. (job runs inside VM: git clone, npm install, npm test)
4. VM exits
5. zfs get written pool/ci/job-abc                           # bytes dirtied → ClickHouse wide event
6. zfs destroy pool/ci/job-abc                               # cleanup
```

### What this does NOT use

- **CRIU** — process checkpointing is fragile with Node.js/V8 (timer FDs, JIT pages, epoll). Not worth the complexity when ZFS clone already eliminates the expensive part (warm caches, pre-installed deps). Process startup of `node` is ~50ms.
- **libzfs** — shells out to `zfs` CLI like every production ZFS project (OpenZFS, Incus, DBLab, OBuilder).
- **Nested ZFS in guest** — the guest runs ext4 on a raw block device. ZFS stays on the host where it belongs.

## ZFS

This project makes heavy use of ZFS. Research notes are in `docs/research/`.


## Quick Start

### 1. Install dev tools

```bash
make setup-dev  # installs Go, OpenTofu, Ansible, protoc, clickhouse-client, etc.
```

### 2. Provision bare metal

```bash
# Set your Latitude.sh API token
export LATITUDESH_AUTH_TOKEN="your-token-here"

# Create your tfvars (one-time)
cp terraform/terraform.tfvars.example.json terraform/terraform.tfvars.json
# Edit terraform/terraform.tfvars.json — set project_id to your Latitude.sh project

# Provision server + generate Ansible inventory
make provision
```

This provisions a bare metal server via OpenTofu and auto-generates `ansible/inventory/hosts.ini` from the outputs.

### 3. Deploy

```bash
make deploy  # idempotent, no wipe — this is the normal workflow
```

This builds the Nix server profile, pushes it over SSH, and configures services via Ansible. Safe to run repeatedly.

> **`make ci-fixtures-pass` and `make ci-fixtures-fail` run lightweight fixture suites against the current host state.** Use `make ci-fixtures-refresh` when guest artifacts changed, and `make ci-fixtures-full` when you want the refresh + suite orchestration together.

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

Current table locations:

- `forge_metal.ci_events`
- `forge_metal.smelter_rehearsals`
- `default.otel_logs`
- `default.otel_traces`
- `default.otel_metrics_gauge`
- `default.otel_metrics_sum`
- `default.otel_metrics_histogram`

The OTel tables live in `default`, not in an `otel` database.

### TLS with a real domain (Cloudflare)

```bash
make setup-domain DOMAIN=anveio.com
make deploy
```

`setup-domain` walks you through everything: initializes secrets if needed, guides you through creating a Cloudflare API token, validates it, and writes the config. Then `deploy` creates DNS records and provisions TLS.

Services get subdomains automatically:

| Subdomain | Service |
|-----------|---------|
| `admin.<domain>` | ClickStack dashboard |
| `git.<domain>` | Forgejo (when enabled) |

### Deprovision

```bash
make deprovision  # destroys server + SSH key via OpenTofu, removes inventory
```

This runs `tofu destroy` and cleans up the generated Ansible inventory. DNS records (if any) must be removed separately via the Cloudflare dashboard or API.

## How It Works

### Nix Server Profile

All server software (Caddy, Forgejo, ClickHouse, etc.) is packaged in a single Nix closure (`flake.nix` -> `server-profile`). This means:

- **Reproducible**: `flake.lock` pins every dependency transitively. Same lock = same binaries, always.
- **Fast deploys**: The closure is pushed once over SSH. No apt repos, no GitHub downloads, no `yarn install`.
- **Atomic updates**: `nix flake update` + `make deploy` upgrades everything. Rollback = `git revert flake.lock`.
- **No apt, no get_url, no build-from-source at deploy time** (except HyperDX, which builds from source using Nix-provided Node.js).

The only `apt install` that remains is `zfsutils-linux` (kernel-dependent, must match running kernel).

### Version Updates

```bash
nix flake update           # update all packages
make deploy --limit canary # deploy to one node, verify services come up
make deploy                # roll out fleet-wide
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

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make setup-dev` | Install pinned dev tools from dev-tools.json via Ansible |
| `make provision` | Provision bare metal via OpenTofu, generate Ansible inventory |
| `make deprovision` | Destroy all bare metal infrastructure |
| `make setup-domain` | Configure Cloudflare domain (interactive wizard) |
| `make server-profile` | Build Nix server profile (golden image closure) |
| `make deploy` | Deploy to all nodes (idempotent, no wipe) — **use this normally** |
| `make deploy-dashboards` | Sync HyperDX dashboards and sources without a full platform redeploy |
| `make ci-fixtures-refresh` | Rebuild and stage CI guest artifacts on the existing host |
| `make ci-fixtures-pass` | Run the positive CI fixture suite against the existing host |
| `make ci-fixtures-fail` | Run the negative CI fixture suite against the existing host |
| `make ci-fixtures-full` | Refresh CI artifacts, then run the pass and fail fixture suites together |
| `make build` | Build the `forge-metal` Go binary locally |
| `make test` | Run Go tests |
| `make guest-rootfs` | Build Alpine guest rootfs on the server |
| `make deploy-ci-artifacts` | Deploy rootfs to /var/lib/ci/ on the server |
| `make smelter-build` | Build homestead-smelter Zig host/guest binaries locally |
| `make smelter-dev` | Hot-swap smelter guest, boot + probe in Firecracker VM (~10s) |

## Developing homestead-smelter

`make smelter-dev` is the best way to test homestead-smelter guest changes. It provides a ~10 second edit-test loop by hot-swapping the Zig binary into a dev golden zvol, bypassing the full rootfs rebuild (~90s).

### What it does

1. Builds `homestead-smelter-guest` locally via `zig build` (~2s)
2. SCPs the binary to the server (~1s)
3. Runs `ansible/playbooks/smelter-dev.yml` which:
   - Clones `benchpool/golden-zvol@ready` to a temporary `smelter-dev-zvol`
   - Mounts the clone, replaces `/usr/local/bin/homestead-smelter-guest`, unmounts
   - Snapshots as `smelter-dev-zvol@ready`
   - Boots a Firecracker VM from the dev zvol via `forge-metal firecracker-test`
   - Waits for the VM's vsock bridge socket to appear
   - Waits for `homestead-smelter-host check-live` to observe the VM
   - Prints `homestead-smelter-host snapshot` output for the live VM
   - Prints PASS/FAIL, waits for VM exit, destroys the dev zvol

### Prerequisites

The server must have been deployed at least once with a valid golden image:

```bash
make guest-rootfs && make deploy-ci-artifacts && make deploy
```

### Usage

```bash
# Edit homestead-smelter/src/guest.zig, then:
make smelter-dev
```

Expected output on success:

```
→ building homestead-smelter guest (zig)
→ uploading guest binary
→ running smelter dev playbook
→ dev golden ready: benchpool/smelter-dev-zvol@ready
HELLO job_id=<job-id> stream_generation=3 host_seq=8 guest_seq=0 boot_id=<boot-id> mem_total_kb=2039556
SAMPLE job_id=<job-id> stream_generation=3 host_seq=100 guest_seq=92 mem_available_kb=1935768 cpu_user_ticks=0
SNAPSHOT_END host_seq=101
PASS: host agent observed live guest telemetry
```

### How it compares to other targets

| Path | Time | When to use |
|------|------|-------------|
| `make smelter-dev` | ~10s | Iterating on guest Zig code |
| `make guest-rootfs && make deploy-ci-artifacts` | ~90s | Changed forgevm-init, Alpine packages, or kernel |
| `make ci-fixtures-pass` | ~3-5min | Re-run the positive fixture suite against the current host |
| `make ci-fixtures-fail` | ~3-5min | Re-run the negative fixture suite against the current host |
| `make ci-fixtures-full` | ~5min+ | Refresh guest artifacts, then run the pass and fail fixture suites together |

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
│   ├── playbooks/         # ci-e2e, dev-single-node, site
│   └── roles/
│       ├── nix_deploy/    # Install Nix + push server profile closure
│       ├── base/          # System config (ZFS, users, npm registry, sudoers)
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
* Dev tools are system-installed via `make setup-dev`. No `nix develop` prefix needed.
* Apply the scientific method: create a bar-raising verification protocol for your planned task *prior* to impelementing changes. The verification protocol should fail, and only then begin implementing until green.

## Output Contract

* When providing a recommendation, consider different plausible options and provide a differentiated recommendation.
* Speculating that your code changes work as expected is not allowed. Unit tests and successful builds are low signal and are not to be trusted. Real observability traces in ClickHouse that exercise your modified code is the only admitted proof of code task-completion. ClickHouse currently exists for the purpose of producing verifiable completion artifacts. If a new schema is needed, you are permitted to create one.
* Do not stop work short of verifying your changes with a live rehearsal of our CI infrastructure with fresh rebuild and redeploy.
* The repo has a fixture flow that seeds Forgejo repos, warms their goldens, opens PRs, and waits for CI.
* When writing design documents, code comments, system architecture diagrams, API documentation, or any other kind of technical writing, ensure that the writing style targets the following audience: distinguished engineers that are experts in the relevant technologies but mostly just need information on how the system being described is different or deviates from standard practice. Avoid throat-clearing, get straight into the information.

## Coding Contract

* Prefer Ansible over shell scripts, except in extreme bootstrap cases.
* Ansible playbook files must have a newline at the end. This will be caught by `ansible-lint`.
* Avoid fallbacks and defaults in Ansible code. Ansible should fail fast with useful logging.

## License

MIT
