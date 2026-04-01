# Legacy Assumptions Audit

## Goal

Shrink the platform boundary until the repo-owned config describes only how to execute a workload, not how Forgejo fixtures are mutated or how the current Node-first implementation happens to work.

The target workload contract is:

```toml
version = 1

workdir = "."
run = ["bash", "-lc", "npm test"]

prepare = ["bash", "-lc", "npm install"]
services = ["postgres"]
env = ["DATABASE_URL"]
profile = "auto"
```

Everything outside that boundary should either be derived by the platform or moved into fixture-only test metadata.

## Audit

### 1. The manifest still mixes workload config with fixture orchestration

Current evidence:

- `internal/ci/manifest.go`
- `internal/ci/fixtures.go`
- `internal/ci/manifest_test.go`
- `test/fixtures/*/.forge-metal/ci.toml`

Current assumptions:

- the same manifest carries repo execution fields and fixture PR mutation fields
- `LoadManifest` fills in PR defaults
- `Validate` requires `pr_change_*` fields even for normal `ci warm` and `ci exec`

Why this is legacy:

- PR mutation is a fixture-E2E concern
- runtime execution should not care how a test branch is generated

Cleanup:

1. shrink `.forge-metal/ci.toml` to runtime-only fields
2. move fixture mutation fields into fixture-only metadata or test code
3. stop requiring fixture fields in `Manifest.Validate`
4. update tests so they validate runtime config separately from fixture generation

### 2. Runtime execution is still Node-package-manager oriented

Current evidence:

- `internal/ci/toolchain.go`
- `internal/ci/toolchain_test.go`
- `internal/ci/manager.go`
- `internal/ci/manager_test.go`

Current assumptions:

- supported package managers are only `npm`, `pnpm`, and `bun`
- lockfile detection is repo-root only
- install commands are hardcoded in Go
- install is always modeled as "language toolchain install step" rather than "optional prepare step"

Why this is legacy:

- it makes the platform boundary about package-manager details instead of workload execution
- it blocks non-Node workloads and makes nested repo layouts second-class

Cleanup:

1. treat `prepare` and `run` as the runtime contract
2. keep toolchain detection as derived behavior, not manifest surface
3. make profile selection explicit: `auto`, `node`, and later other profiles
4. stop baking package-manager-specific shell into `buildGuestCommand`

### 3. The guest command model is still shell-heavy and repo-root-install biased

Current evidence:

- `internal/ci/manager.go`
- `internal/ci/manager_test.go`

Current assumptions:

- install always happens from `/workspace`
- execution always passes through `bash -lc`
- the wrapper workdir is fixed to `/workspace` even when the actual workload runs elsewhere

Why this is legacy:

- it conflates the current Node monorepo behavior with the platform contract
- it makes the execution model harder to generalize cleanly

Cleanup:

1. let the runtime manifest describe `prepare` and `run` directly
2. keep service startup separate from command construction
3. reduce the amount of generated shell needed for normal execution

### 4. The base guest image is still biased toward Node + Postgres

Current evidence:

- `scripts/build-guest-rootfs.sh`

Current assumptions:

- the image preinstalls `nodejs`, `npm`, `bun`, and `postgresql`
- Postgres is initialized directly into the base guest image
- a compatibility wrapper still writes `/ci-start.sh`

Why this is legacy:

- it bakes one workload family into the substrate
- it keeps a tracer-bullet compatibility path alive longer than necessary

Cleanup:

1. keep the base guest intentionally boring
2. treat language/runtime selection as profile logic
3. keep service bootstrapping explicit and minimal
4. remove `/ci-start.sh` once no live path depends on it

### 5. Services are modeled as free-form strings but only `postgres` actually works

Current evidence:

- `internal/ci/manifest.go`
- `internal/ci/manager.go`
- `scripts/build-guest-rootfs.sh`

Current assumptions:

- `services` is just a string list
- the guest wrapper only recognizes `postgres`
- other service names have no implementation behind them

Why this is legacy:

- it advertises more generality than the runtime actually supports
- it hides service behavior inside guest shell glue

Cleanup:

1. validate service names explicitly
2. model service startup as a small registry or adapter layer
3. keep the first-class supported set honest

### 6. Forgejo fixture logic still leaks into the platform boundary

Current evidence:

- `internal/ci/fixtures.go`
- `internal/ci/forgejo.go`
- `internal/ci/telemetry.go`
- `ansible/roles/base/tasks/main.yml`

Current assumptions:

- fixtures generate Forgejo workflows that shell directly into `forge-metal ci exec`
- telemetry includes manifest fields that should become derived or fixture-only
- runner execution still relies on a repo-local generated workflow and sudo entry

Why this is legacy:

- Forgejo is part of the product, but fixture orchestration should not define the repo workload schema
- test harness concerns and runtime concerns are still too close together

Cleanup:

1. keep Forgejo integration, but isolate it from the workload manifest
2. make telemetry depend on runtime execution data, not fixture mutation fields
3. keep the generated workflow thin and generic

### 7. There are still compatibility shims and tracer-bullet leftovers

Current evidence:

- `internal/ci/manager.go`
- `internal/firecracker/api.go`
- `internal/firecracker/orchestrator.go`
- `scripts/build-guest-rootfs.sh`
- `migrations/002_firecracker_columns.up.sql`

Current assumptions:

- `legacyRepoGoldenDataset` still exists as a fallback path
- several comments still describe the implementation as a tracer bullet
- the guest rootfs still contains compatibility wrappers

Why this is legacy:

- compatibility paths tend to survive longer than intended
- they obscure which code is truly live and which behavior is transitional

Cleanup:

1. remove the legacy repo-golden fallback once all state has migrated
2. delete compatibility wrappers that are no longer exercised
3. rewrite stale comments so they describe the current system, not the deleted one

## Prioritized Cleanup Order

### Phase 1: shrink the repo workload manifest

Change:

- `internal/ci/manifest.go`
- `internal/ci/manifest_test.go`
- `test/fixtures/*/.forge-metal/ci.toml`

Outcome:

- runtime config contains only workload execution fields
- fixture metadata moves out of the runtime contract

### Phase 2: make `prepare` and `run` the execution boundary

Change:

- `internal/ci/manager.go`
- `internal/ci/toolchain.go`
- `internal/ci/manager_test.go`
- `internal/ci/toolchain_test.go`

Outcome:

- execution stops depending on hardcoded package-manager install templates as the primary interface

### Phase 3: narrow the guest wrapper and service model

Change:

- `scripts/build-guest-rootfs.sh`
- any tests covering `forge-metal-ci-run`

Outcome:

- service handling becomes explicit
- compatibility wrappers are removed
- the substrate becomes less coupled to Node + Postgres

### Phase 4: clean up fixture and telemetry boundaries

Change:

- `internal/ci/fixtures.go`
- `internal/ci/forgejo.go`
- `internal/ci/telemetry.go`

Outcome:

- fixture orchestration remains useful for E2E
- runtime execution no longer inherits fixture-only metadata

### Phase 5: delete transitional fallbacks

Change:

- `internal/ci/manager.go`
- `internal/firecracker/api.go`
- `internal/firecracker/orchestrator.go`
- `scripts/build-guest-rootfs.sh`

Outcome:

- no more legacy repo-golden path
- no more tracer-bullet wording in live files
- no leftover compatibility entrypoints

## Definition Of "Free Of Legacy Assumptions"

This repo is not free of legacy assumptions until all of the following are true:

- the repo-owned workload config contains only execution concerns
- fixture PR mutation data is no longer in `.forge-metal/ci.toml`
- `ci exec` can run from `prepare` + `run` without package-manager-specific manifest fields
- supported services are explicit and validated
- the guest image no longer carries dead compatibility wrappers
- live code and docs stop describing the current system as a tracer bullet
