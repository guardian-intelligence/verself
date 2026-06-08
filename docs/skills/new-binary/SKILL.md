---
name: new-binary
description: Use when adding any binary dependency to the repo — a dev/build tool, a deployed service, a third-party infrastructure component, or a privileged host daemon. Decides how it is pinned, where it is declared, and how it runs. This is the canonical classification procedure for every binary the company ever installs on any host.
---

# Adding a binary

Every binary added to the repo resolves three decisions along two independent axes. The procedure is identical whether the binary is a developer tool, a deployed Go service, a third-party database, or a privileged host daemon.

The two axes are independent and must not be conflated:

- **Source axis** — where the bytes come from and how they are pinned (third-party `.deb` closure, third-party OCI base image, or a repo-built Bazel `go_binary`/`rust_binary`).
- **Run axis** — how the binary executes on a node (isolated in a container, or natively on the host with privilege).

A repo-built Go binary can run containerized (every service) or natively privileged (`nftables-apply`, `vm-orchestrator`). A third-party package can run containerized (postgresql) or natively as substrate (podman, openbao). Source does not determine run model. Decide each axis on its own.

## Decision 1 — Always version pin

The binary is fetched by Bazel from a content-addressed, version-pinned source. No unpinned downloads, no floating tags, no install-time resolution against a drifting archive.

- **Third-party Debian package**: pin the full dependency closure as individual `.deb` files, not `apt install <pkg>`. `apt` is a downloader plus a dependency solver; the solve is performed once at pin time and frozen, so install reads only pinned bytes. The 39-package podman closure under `//src/infrastructure-components/podman:runtime_artifact` is the reference.
- **Third-party OCI image**: pin the base image by digest through `rules_oci` `oci.pull` (see `ubuntu_noble_base` in `MODULE.bazel`). Never a mutable tag.
- **Repo-built binary**: Bazel owns the build graph; the toolchain and every transitive dependency are already commit-pinned by the workspace.

## Decision 2 — Colocate, no central binary directory

The binary is declared in the Bazel target of the component or tool that uses it. There is no shared `/usr/local/bin`, no global bin manifest, no hand-managed install list.

- **Dev / controller tooling** (runs on a developer or CI controller box, never deployed to a node): declare under `src/tools/dev/binaries`, resolve through a Bazel toolchain, expose to the caller with `aspect dev install --install-shims --bin-dir=$HOME/.local/bin`. The shim points back at the Bazel-resolved output and does not duplicate version state. This class has no run-model decision — it never lands on a deploy host.
- **Deployed binary** (runs on a node): declare in the owning component's targets under `src/infrastructure-components/<name>`, `src/services/<name>`, `src/integrations/<name>`, or `src/substrate/<name>`. Proceed to Decision 3.

## Decision 3 — Permission level

A deployed binary runs at exactly one of two levels. The level is determined by a single question:

```
deployed binary
  │
  ├─ Does it require host privilege a default-deny container cannot be
  │  granted, OR must it exist before the container runtime / scheduler?
  │        │
  │        ├─ no ───────────────────────────────► LEVEL 1: Containerized
  │        │
  │        └─ yes ──────────────────────────────► LEVEL 2: Native
  │
  └─ "yes" resolves to exactly one of two reasons:
       • SUBSTRATE  — must exist before Nomad/podman can run anything
                       (bootstrap paradox: podman cannot install podman
                       into a podman container).
       • PRIVOPS    — needs host kernel or device control: kernel netfilter
                       tables, raw devices, ZFS, Firecracker/jailer, TAP,
                       kernel modules, host network-namespace manipulation.
```

The current Nomad driver is not the classification. Most Level 1 components still run under the `raw_exec` driver (Nomad executing the repo-built binary directly on the host). `raw_exec` is the pre-migration state, not a permission level. The target for every Level 1 component is the `podman` driver with an OCI image; migrating a component is building its `rules_oci` image and flipping its `nomad.hcl` driver from `raw_exec` to `podman`. A binary is Level 2 only when it trips the privilege or substrate test, never merely because it runs under `raw_exec` today.

### Level 1 — Containerized (default)

Packaged as an OCI image with `rules_oci` and run as a container under the Nomad `podman` driver. Default-deny, unprivileged, no host filesystem mutation, no maintainer scripts on the host. This is the default for every service and for third-party infrastructure that does not control host kernel state. Capabilities a container can hold are granted in the job spec (for example `CAP_NET_BIND_SERVICE` for an edge listener); needing such a capability does not promote a binary to Level 2.

#### Host bind-mounts: minimize to durable and cross-component state

The Nomad `podman` driver does not auto-create bind-mount sources (unlike `docker run -v`); a job that bind-mounts a missing host path fails container creation with `statfs <path>: no such file or directory`. Every host bind-mount therefore implies a host-directory provisioning owner, and an unprovisioned path is the live-assembly failure mode this migration exists to eliminate — acute for paths under `/run` (tmpfs), which a reboot wipes.

A containerized component's bind-mounts fall into three classes:

- **Intra-alloc IPC** (a reconcile reconciler projecting the resource graph, writing `report.json`, sharing a derived config between `setup`/`serve`/`reconcile` tasks): use the Nomad alloc directory, mounted at `/alloc` in every task of the group and created and garbage-collected by Nomad. No host path, no pre-creation, no reboot fragility. The reconciler `--report` path and the `/run/verself/recovery/<name>` convention are host-resident only for `raw_exec` components whose recover binary runs on the host and creates the directory itself; once containerized, that state moves to `/alloc`.
- **Durable state** (a database's data directory): a real host path, provisioned once and restored from offsite backup, never `/alloc` (the alloc directory does not survive alloc GC).
- **Cross-component contract** (a unix socket other components connect through): a host path other containers mount. Containerized peers reaching the component over TCP do not need it.

Keep only the durable and cross-component paths on the host; move everything else to `/alloc`. `postgresql` is the reference: `dataDir` (durable) and `socketDir` (cross-component) are host bind-mounts; the recovery report, projected document, derived pgbackrest config, logs, and spool live under `/alloc`.

### Level 2 — Native (substrate and privops)

Runs directly on the host with privilege, installed at FHS paths. Two admissible reasons, both narrow:

- **Substrate**: the binary must exist before the container runtime or scheduler. For third-party substrate, install the pinned `.deb` closure with `dpkg -i` at native paths so maintainer scripts and triggers run — `ldconfig` rebuilding `/etc/ld.so.cache`, `update-alternatives`, system-user creation — and the dynamic linker finds libraries where the binaries expect them. Extraction to a prefix with `dpkg-deb -x` skips those triggers and forces hand-written loader-cache injection and binary-path overrides; native install removes that compensation.
- **Privops**: the binary controls host kernel or device state and cannot run inside a default-deny container. These are commonly repo-built daemons (`nftables-apply`, `vm-orchestrator`) run under `raw_exec` or with explicit host privilege.

Native installation runs in a **build-time sandbox**, not on a live host. `dpkg -i` executes against the golden node image's rootfs during the Bazel image build, the result is validated and diffed in CI, and the host materializes that image through the zfs-clone golden-environment machinery. Maintainer scripts never execute on a running production node. The pinned closure is finite, version-locked, and auditable, and its binaries already run as root at runtime, so the build sandbox bounds blast radius and reproducibility rather than defending against a hostile maintainer. Until the golden image build exists, the interim path is `dpkg -i` of the pinned closure on the host during `preflight`; the closure is frozen, so it is no more privileged than the binaries it installs.

## The recover binary: domain-state convergence only

A component's `*-recover` binary reconciles **runtime/domain state**, never host-install state. It is the long-lived `reconcile` sidecar from the CLAUDE.md contract, and its only legitimate job is to observe live component state and converge *domain* facts a scheduler and a config-management layer cannot: restore from offsite backup, unseal, bootstrap a trust bundle, create a schema or auth role. `postgresql/internal/recoveryfsm` is the reference — a ~250-line **pure state machine** (`BuildPlan(facts)`, `Advance(state, facts)`) that classifies observed facts and emits transitions, mutating nothing itself.

Four responsibilities accrete into recover binaries written before a component's Level 1 migration. Each has a natural home, and for three of the four it is **not** the recover binary and **not** Ansible. Migrating a component to Level 1 deletes those three — it does not relocate them. Do not add them back, and do not move them into Ansible:

| Responsibility | Real sink | Smell to delete from the recover binary |
| --- | --- | --- |
| Runtime install | Bazel / the OCI image (L1), or `dpkg -i` into the golden image (L2) | `extractTar` / `promoteRuntime` / `installRuntime` / a `current` symlink flip |
| Host provisioning | the golden-image build; Nomad `/alloc` for ephemeral dirs | `ensureAccount` / `ensureGroup` / `prepareDirectories` / `chown` |
| Process supervision | Nomad (job spec, lifecycle, health) | `pidsListeningOnPort` / `terminateProcess` / scanning `/proc` |
| Domain-state convergence | **a small typed FSM** (folded into the image, or a thin Nomad sidecar/prestart) | the pure classifier — keep it; never push it into Ansible or HCL |

The fourth responsibility cannot move to Ansible (one-shot push, not the long-lived in-cluster loop the DR contract requires) or to pure HCL (declarative gating, not the imperative restore-vs-init branch), and shell scripts are forbidden — so a domain FSM stays a typed binary by elimination. `postgresql/internal/recoveryfsm` is the reference: a pure `BuildPlan`/`Advance` classifier that mutates nothing.

The migration therefore splits by state:

- **Stateless component** (zot, grafana, verdaccio, otelcol, nats, …): the recover binary is *entirely* install + provision + supervise. OCI-migrate and **delete it outright** — the image plus Nomad fully replace it, zero residue.
- **Stateful component** (postgresql, clickhouse, tigerbeetle, openbao, spire): OCI-migrate and **collapse the recover binary to its domain FSM**, run as a Nomad sidecar/prestart; delete the install/provision/supervise code, keep the classifier.

A recover binary that extracts a tarball, creates a system user, or kills a process by PID is reimplementing the OCI runtime, the golden-image build, and the scheduler respectively — the live-assembly class this migration exists to eliminate. There is no shared recover framework (`src/recovery` is empty), and there should not be one: the target is per-component deletion or a small per-component FSM, not a shared host-plumbing library. Ansible's residual role shrinks toward materializing the golden image, never hosting reconcile loops.

## Inventory

Every deployed binary in the repo, classified by target level. `raw_exec` in the migration column marks a Level 1 component not yet moved to the `podman` driver. Extend this table whenever a binary is added.

### Level 2 — Native

| Binary | Colocated at | Reason | Source | Status |
| --- | --- | --- | --- | --- |
| podman, crun, conmon, catatonit (+ 39-deb closure) | `src/infrastructure-components/podman` | Substrate (container runtime) | deb closure | Native prefix today; target golden-image `dpkg -i` |
| nomad agent | profile node tree (`/opt/verself/profile/bin/nomad`) | Substrate (scheduler) | third-party | Native |
| openbao | `src/infrastructure-components/openbao` | Substrate (pre-Nomad, seal/unseal host integration; host `openbao.service`) | deb closure | Native |
| nftables-apply | `src/infrastructure-components/nftables` | Privops (host kernel netfilter) | repo-built Go | `raw_exec`, stays native |
| vm-orchestrator | `src/substrate/vm-orchestrator` | Privops (Firecracker, ZFS, jailer, TAP) | repo-built Go | `raw_exec`, stays native |

### Level 1 — Containerized (target)

| Binary / component | Colocated at | Source | Migration status |
| --- | --- | --- | --- |
| postgresql | `src/infrastructure-components/postgresql` | OCI image | `podman` (done); reference for the alloc-dir bind-mount pattern. Verified on gamma: container runs and serves TCP 5432; recovery IPC in `/alloc`, only `dataDir`+`socketDir` on host. |
| cloudflare control-plane | `src/integrations/cloudflare/control-plane` | OCI image | `podman` (done); verified on gamma as a live container (`localhost/guardian/cloudflare-control-plane`) |
| distribution-service | `src/services/distribution-service` | OCI image | `podman` (done) |
| profile-service | `src/services/profile-service` | OCI image | `podman` (done) |
| clickhouse | `src/infrastructure-components/clickhouse` | third-party | `raw_exec` → podman |
| electric | `src/infrastructure-components/electric` | third-party | `raw_exec` → podman |
| forgejo | `src/infrastructure-components/forgejo` | third-party | `raw_exec` → podman |
| grafana | `src/infrastructure-components/grafana` | third-party | `raw_exec` → podman |
| haproxy | `src/infrastructure-components/haproxy` | third-party | `raw_exec` → podman (edge; `CAP_NET_BIND_SERVICE`) |
| nats | `src/infrastructure-components/nats` | third-party | `raw_exec` → podman |
| nomad-observer | `src/infrastructure-components/nomad-observer` | repo-built Go | `raw_exec` → podman |
| otelcol | `src/infrastructure-components/otelcol` | third-party | `raw_exec` → podman |
| spicedb | `src/infrastructure-components/spicedb` | third-party | `raw_exec` → podman |
| spire | `src/infrastructure-components/spire` | third-party | `raw_exec` → podman (identity root; verify Nomad workload attestor access in-container before flip) |
| stalwart | `src/infrastructure-components/stalwart` | third-party | `raw_exec` → podman |
| temporal-platform | `src/infrastructure-components/temporal-platform` | third-party | `raw_exec` → podman |
| tigerbeetle | `src/infrastructure-components/tigerbeetle` | third-party | `raw_exec` → podman |
| verdaccio | `src/infrastructure-components/verdaccio` | third-party | `raw_exec` → podman |
| zitadel | `src/infrastructure-components/zitadel` | third-party | `raw_exec` → podman |
| zot | `src/infrastructure-components/zot` | third-party | `raw_exec` → podman (image registry; must be reachable before pulls — preload its image into the golden local store) |
| analytics, billing, deployment, email, github-integration, governance, iam, notifications, object-storage, projects, sandbox-rental, secrets, source-code-hosting | `src/services/<name>` | repo-built Go | `raw_exec` → podman |
| company, verself-web | `src/viteplus-monorepo/apps/<name>` | repo-built (TS/Vite) | `raw_exec` → podman |

### Dev / controller tooling (no run-model decision)

| Binary | Colocated at | Notes |
| --- | --- | --- |
| bazel, aspect, guardian | `src/tools/dev/bootstrap`, `src/tools/dev/binaries` | Stage-zero bootstrap plus Bazel-resolved shims to `$HOME/.local/bin`. |
| rsync | `src/tools/dev/binaries` (also in the podman deb closure) | Preflight transport prerequisite; seeded onto the target over ssh. |
| stripe tooling | `src/tools/dev/binaries` | Bazel-resolved, never on a deploy host. |

### Outside the host-install model

| Binary | Colocated at | Notes |
| --- | --- | --- |
| vm-guest-telemetry | `src/substrate/vm-guest-telemetry` | Zig binary that runs inside the customer guest VM and streams over vsock; installed into the guest image, never onto a host. |
