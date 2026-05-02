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
- `sandbox-rental-service/` — compute product control plane (executions, checkpoint refs, billing windows).
- `billing-service/` — Reserve/Settle/Void on TigerBeetle + PostgreSQL.
- `identity-service/`, `mailbox-service/`, `workload/` — service-owned databases, migrations, and Huma APIs.
- `domain-transfer-objects/` — shared DTOs, protobuf schemas, OpenAPI-compatible wire-language types, and generated contract conventions.
- `service-runtime/` — shared service startup/runtime packages such as Go env loading and HTTP listener policy.
- `observability/` — shared telemetry packages and operational query tools.
- `auth-middleware/` — local JWT validation against Zitadel JWKS plus shared SPIFFE workload identity helpers.
- `deployment-tools/` — typed deploy orchestration binary and Nomad job resolution rules.
- `dev-tools/` — controller development tool catalog plus operator/bootstrap command binaries.

## Frontend (`src/viteplus-monorepo/`)

- `apps/` — TanStack Start applications:
  - `company` — Guardian Intelligence company site on `company_domain` (guardianintelligence.org). Owns landing, `/design`, `/letters` (+ RSS), `/solutions`, `/company`, `/careers`, `/press`, `/changelog`, `/contact`, `/og/*` dynamic OG cards. Forker-friendly split: `src/content/`, `src/brand/`, `src/routes/`, `src/components/`.
  - `verself-web` — the unified product app on the `verself_domain` apex. Owns the authenticated browser console (sandbox, billing, identity, profile, notifications, mail, source, future product workflows behind TanStack Start server functions), the public docs at `/docs` and `/docs/reference`, and the canonical legal tree at `/policy/*` (Terms, Privacy, DPA, AUP, Cookies, Security, SLA, Subprocessors, Data Retention, Policy Changelog).
- `packages/` — shared UI, brand marks, generated OpenAPI clients, Valibot validators.

## Provisioning Tools (`src/provisioning-tools/`)

- `terraform/` — OpenTofu bare-metal provisioning for Latitude.sh.
- `ansible/` — local controller playbooks that apply/destroy the OpenTofu
  state and write host inventory.
- `scripts/` — provisioning helpers such as inventory generation.

Provisioning tools own physical machine allocation and inventory production.
They do not converge host packages or deploy services.

## Host Configuration (`src/host-configuration/`)

- `ansible/` — current private runner for host, daemon, and per-component
  prerequisite convergence.
- `migrations/clickhouse/` — host convergence ClickHouse schema.
- `scripts/` — founder/agent wrappers invoked by AXL tasks for deploy,
  persona, billing, mail, database access, observability, and host evidence.
Topology vars are authored under `src/host-configuration/ansible/group_vars/all/generated/`.
Host firewall files are source-owned under `src/host-configuration/ansible/rendered/`.
Nomad base jobs live under `src/deployment-tools/nomad/sites/<site>/jobs/`
and are resolved by Bazel with the service-owned artifact and rollout inputs.

## Service- and host-local docs

Host convergence, OpenTofu provisioning, and deploy wrappers live in
`src/host-configuration/`, `src/provisioning-tools/`, and `.aspect/`.

Bazel-owned package definitions live with their owners:
`src/host-configuration/binaries/` for server and host configuration tools,
`src/dev-tools/binaries/` for controller dev tools, and
`src/vm-orchestrator/guest-images/` for guest-image inputs.

Service-local docs live under each service's `docs/` directory (e.g. `src/sandbox-rental-service/docs/`). Directory-specific conventions are captured in per-directory `AGENTS.md` files.
