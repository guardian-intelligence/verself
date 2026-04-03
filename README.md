# forge-metal

`forge-metal` is a self-hosted bare-metal platform for running Forgejo, fast CI on Firecracker + ZFS, and ClickHouse/HyperDX observability.

The CI path is built around repo-specific golden images:

1. start from a generic guest image
2. cold-bootstrap a repo's `main` branch inside Firecracker
3. snapshot that warmed state as the repo golden
4. clone the golden with ZFS for each PR job
5. run CI inside an isolated Firecracker microVM
6. emit wide events to ClickHouse for inspection in HyperDX

## Current Scope

Today the proven path is:

- Forgejo-hosted repos
- Firecracker microVM execution
- ZFS zvol clones for repo goldens
- Node-based workloads using `npm`, `pnpm`, or `bun`
- optional `postgres` service
- fixture E2E that seeds controlled repos into Forgejo and verifies the warm path

That is intentionally narrower than "arbitrary CI for any repo". The current interface limits and remaining implementation assumptions are tracked in [docs/architecture/legacy-assumptions-audit.md](docs/architecture/legacy-assumptions-audit.md).

## Canonical Workload Contract

The repo-owned workload contract is:

```toml
version = 1

workdir = "."
run = ["bash", "-lc", "npm test"]

prepare = ["bash", "-lc", "npm install"]
services = ["postgres"]
env = ["DATABASE_URL"]
profile = "auto"
```

Meaning:

- `run`: required CI command executed for the job
- `workdir`: optional working directory relative to the repo root
- `prepare`: optional command used when warming the repo golden; defaults to `run`
- `services`: optional local services required inside the VM; currently only `postgres` is supported
- `env`: optional environment variable names expected by the workload; values are copied from the runner environment and missing names fail fast
- `profile`: optional execution-profile override; currently `auto` and `node` are supported, and `auto` resolves to the current Node runtime path

## What The Platform Should Derive

These should not live in repo-owned workload config:

- repo name and description
- default branch
- package manager and version
- runtime version
- lockfile path and cache identity
- base guest selection
- telemetry IDs and run grouping
- generated Forgejo workflow contents

## What Does Not Belong In Workload Config

Fixture-only test metadata should be kept out of the runtime contract:

- PR branch names
- PR titles and commit messages
- find/replace rules used to trigger a fixture PR
- any Forgejo-specific E2E mutation details

Those are fixture orchestration concerns, not workload execution concerns.

## Runtime Notes

- The runtime manifest is read from the checked-out ref, not from the warmed default-branch copy.
- Fixture metadata for Forgejo E2E lives in the internal fixture layer, not in `.forge-metal/ci.toml`.
- Toolchain detection is derived behavior behind the current Node profile; it is not part of the repo-owned config surface.
- The host now sends structured guest phases instead of generating `bash -lc` scripts. Shell is still allowed, but only when the workload explicitly uses it in `run` or `prepare`.
- Per-job guest config is delivered over the host-initiated vsock control stream. MMDS is not part of the steady-state runtime path.

## Basic Commands

```bash
make setup-dev   # one-time: install pinned dev tools
make hooks-install
make provision
make deploy
make ci-fixtures-pass
```

- `make hooks-install`: install the repo's git pre-commit hooks
- `make deploy`: normal idempotent deploy path
- `make ci-fixtures-pass`: run the positive CI fixture suite against the current host
- `make ci-fixtures-fail`: run the negative CI fixture suite against the current host
- `make ci-fixtures-refresh`: rebuild and restage guest artifacts without a full redeploy
- `make ci-fixtures-full`: refresh guest artifacts, then run the configured CI fixture target set

For live operator access patterns, including ClickHouse queries over SSH, see [docs/architecture/operator-workflows.md](docs/architecture/operator-workflows.md).


--- A note on the future ---

We will  want long-running VMs with developer tools installed for agents to work within with full unbounded permissions and access to  If they do something destructive to their sandbox we want to restore from a snapshots. If they attempt to exfiltrate secrets, we tightly controll egress and only provide encrypted secrets that must go through a layer for decryption (unless you can think of something better) If they attempt to perform a destructive action on production systems, we have a policy layer to prevent it.
