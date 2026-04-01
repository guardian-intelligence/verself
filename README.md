# forge-metal

`forge-metal` is a self-hosted bare-metal platform for running Forgejo, fast CI on Firecracker + ZFS, and ClickHouse/HyperDX observability.

The CI path is built around repo-specific golden images:

1. start from a generic guest image
2. cold-bootstrap a repo's `main` branch inside Firecracker
3. snapshot that warmed state as the repo golden
4. clone the golden with ZFS for each PR job
5. run CI inside an isolated Firecracker microVM
6. emit wide events to ClickHouse for inspection in HyperDX

This repo is no longer a benchmark harness. The active path is the real Forgejo + Firecracker + ZFS workflow exercised by `make e2e`.

## Current Scope

Today the proven path is:

- Forgejo-hosted repos
- Firecracker microVM execution
- ZFS zvol clones for repo goldens
- Node-based workloads using `npm`, `pnpm`, or `bun`
- optional `postgres` service
- fixture E2E that seeds controlled repos into Forgejo and verifies the warm path

That is intentionally narrower than "arbitrary CI for any repo". The cleanup work to remove remaining legacy assumptions is tracked in [docs/architecture/legacy-assumptions-audit.md](docs/architecture/legacy-assumptions-audit.md).

## Minimal Workload Contract

The intended user-facing contract is small:

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
- `prepare`: optional command used when warming the repo golden
- `services`: optional local services required inside the VM
- `env`: optional environment variable names expected by the workload
- `profile`: optional override when auto-detection is wrong

This is the boundary we want to stabilize around.

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

## Current Cleanup Direction

The main technical direction is:

1. keep one boring, generic execution substrate
2. make repo-specific warming happen from the repo's default branch
3. keep user config minimal
4. derive toolchain and cache details where possible
5. move fixture/E2E metadata out of the runtime manifest

The detailed audit and cleanup order live in [docs/architecture/legacy-assumptions-audit.md](docs/architecture/legacy-assumptions-audit.md).

## Basic Commands

```bash
nix develop
make provision
make deploy
make e2e
```

- `make deploy`: normal idempotent deploy path
- `make e2e`: deploy CI artifacts and run the real Forgejo fixture end-to-end validation
