# Directory Structure

Monorepo rooted at the repo top level. Bazel owns the repo-level build graph; each Go service keeps its own `go.mod`, and the TypeScript monorepo is pnpm-driven under `src/viteplus-monorepo/`.

## Top level

- `src/` — all first-party source.
- `docs/` — cross-service architecture docs and vendored references (`docs/references/` is read-only third-party material).
- `artifacts/` — gitignored materialized build/deploy outputs.
- `smoke-artifacts/` — gitignored live smoke-test bundles, personas, Playwright traces, and query evidence.
- `MODULE.aspect` + `.aspect/` — canonical task surface for founder/agent workflows. Run `aspect` (no args) for the full list; read before reaching for ad-hoc scripts.

## Go services (`src/`)

- `vm-orchestrator/` — privileged host daemon (Firecracker, ZFS, TAP, jailer, vm-bridge, gRPC over Unix socket).
- `vm-guest-telemetry/` — Zig guest agent streaming 60Hz health over vsock.
- `cue-renderer/` — CUE topology/catalog source plus the Go renderer for generated platform artifacts.
- `sandbox-rental-service/` — compute product control plane (executions, checkpoint refs, billing windows).
- `billing-service/` — Reserve/Settle/Void on TigerBeetle + PostgreSQL.
- `identity-service/`, `mailbox-service/`, `workload/` — service-owned databases, migrations, and Huma APIs.
- `apiwire/` — shared Huma DTOs and wire-language types.
- `auth-middleware/` — local JWT validation against Zitadel JWKS plus shared SPIFFE workload identity helpers.
- `otel/` — shared OpenTelemetry wiring.

## Frontend (`src/viteplus-monorepo/`)

- `apps/` — TanStack Start applications:
  - `company` — Guardian Intelligence company site on `company_domain` (guardianintelligence.org). Owns landing, `/design`, `/letters` (+ RSS), `/solutions`, `/company`, `/careers`, `/press`, `/changelog`, `/contact`, `/og/*` dynamic OG cards. Forker-friendly split: `src/content/`, `src/brand/`, `src/routes/`, `src/components/`.
  - `verself-web` — the unified product app on the `verself_domain` apex. Owns the authenticated browser console (sandbox, billing, identity, profile, notifications, mail, source, future product workflows behind TanStack Start server functions), the public docs at `/docs` and `/docs/reference`, and the canonical legal tree at `/policy/*` (Terms, Privacy, DPA, AUP, Cookies, Security, SLA, Subprocessors, Data Retention, Policy Changelog).
- `packages/` — shared UI, brand marks, generated OpenAPI clients, Valibot validators.

## Provision (`src/provision/`)

- `terraform/` — OpenTofu bare-metal provisioning for Latitude.sh.
- `ansible/` — local controller playbooks that apply/destroy the OpenTofu
  state and write substrate inventory.
- `scripts/` — provisioning helpers such as inventory generation.

Provision owns physical machine allocation and inventory production. It does
not converge host packages or deploy services.

## Substrate (`src/substrate/`)

- `ansible/` — current private runner for host, daemon, and per-component
  prerequisite convergence.
- `migrations/clickhouse/` — substrate-owned ClickHouse schema.
- `scripts/` — founder/agent wrappers invoked by AXL tasks for deploy,
  persona, billing, mail, database access, observability, and substrate
  evidence.
- `controller-agent/` — controller-side OTLP buffer used while running
  substrate convergence.

Per-deploy generated files materialise under `.cache/render/<site>/` when
`aspect render --site=<site>` runs. The deploy path consumes rendered
inventory, generated group_vars projections, host firewall files, and rendered
Nomad jobs from that cache.

## Platform (`src/platform/`)

`src/platform/` contains platform policy and architecture documents that have
not moved to service-local homes yet. Host convergence, OpenTofu provisioning,
and deploy wrappers live in `src/substrate/`, `src/provision/`, and `.aspect/`.

The Bazel-owned third-party server-tool package definitions, including
`//src/cue-renderer/binaries:server_tools.tar.zst`, live under
`src/cue-renderer/binaries/` because the tarball's contents are
catalog-driven (the catalog declares both the version pins and the
target label).

Service-local docs live under each service's `docs/` directory (e.g. `src/sandbox-rental-service/docs/`). Directory-specific conventions are captured in per-directory `AGENTS.md` files.
