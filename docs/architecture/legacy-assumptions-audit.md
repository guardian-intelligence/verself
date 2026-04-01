# Legacy Assumptions Audit

## Current Runtime Contract

The canonical repo-owned config is now:

```toml
version = 1

workdir = "."
run = ["bash", "-lc", "npm test"]

prepare = ["bash", "-lc", "npm install"]
services = ["postgres"]
env = ["DATABASE_URL"]
profile = "auto"
```

Current semantics:

- `run` is required
- `prepare` defaults to `run`
- `workdir` defaults to `.`
- `services` is validated explicitly; currently only `postgres` is supported
- `env` names must exist in the runner environment and are forwarded into the guest
- `profile` supports `auto` and `node`; `auto` resolves to the current Node runtime path

Everything outside that boundary is either derived by the platform or owned by fixture-only orchestration.

## Completed Cleanup

### 1. Runtime manifest is now fixture-agnostic

- `internal/ci/manifest.go`
- `internal/ci/fixtures.go`
- `internal/ci/manifest_test.go`
- `test/fixtures/*/.forge-metal/ci.toml`

- `.forge-metal/ci.toml` now carries only runtime execution fields.
- Fixture PR mutation data moved into `internal/ci/fixture_metadata.go`.
- Fixture repo identity, descriptions, default branches, and PR mutation rules no longer leak through the runtime manifest.

### 2. `ci exec` now reads the checked-out ref manifest

- `internal/ci/manager.go`

- `ci exec` now fetches and checks out the requested ref before loading `.forge-metal/ci.toml`.
- Branch-specific runtime config changes are now honored.
- The legacy repo-golden dataset fallback was removed; the active repo-golden state path is the only supported lookup.

### 3. Runtime env and telemetry are explicit

- `internal/ci/manager.go`
- `internal/ci/telemetry.go`

- Required env names now fail fast during warm and exec instead of surfacing as opaque guest failures.
- Telemetry no longer records removed manifest fields.
- Telemetry now records manifest env names rather than env values.

### 4. Guest compatibility shims were removed

- `scripts/build-guest-rootfs.sh`
- `internal/firecracker/api.go`
- `internal/firecracker/orchestrator.go`
- `migrations/002_firecracker_columns.up.sql`

- `/ci-start.sh` was removed from the guest image.
- The remaining runtime comments now describe the live system directly.

## Remaining Assumptions

### 1. Runtime profiles are still Node-first

- `internal/ci/toolchain.go`
- `internal/ci/manager.go`

Current state:

- `profile = "auto"` currently resolves to the Node runtime path
- derived toolchain support is still limited to `npm`, `pnpm`, and `bun`
- install behavior is still implemented as Node-package-manager logic behind the current profile

### 2. The guest substrate is still Node + Postgres biased

- `scripts/build-guest-rootfs.sh`

Current state:

- the base image still preinstalls `nodejs`, `npm`, `bun`, and `postgresql`
- Postgres is still initialized in the guest image
- this is honest now, but it remains a scope limitation for broader workload diversity

### 3. Service support is intentionally narrow

- `internal/ci/manifest.go`
- `scripts/build-guest-rootfs.sh`

Current state:

- supported services are validated explicitly
- the only supported service is `postgres`

### 4. Execution is now structured, but still Node-profile-biased

- `internal/ci/manager.go`
- `cmd/forgevm-init/main.go`

Current state:

- the platform no longer generates `bash -lc`
- prepare and run execute as direct argv phases inside the guest
- the current Node profile still installs from repo root and then runs from the configured workdir

### 5. Forgejo fixture orchestration remains repo-local

- `internal/ci/fixtures.go`
- `internal/ci/fixture_metadata.go`
- `internal/ci/forgejo.go`

Current state:

- Forgejo E2E is still owned in-repo
- fixture metadata is now isolated correctly, but it is still Forgejo-specific by design
