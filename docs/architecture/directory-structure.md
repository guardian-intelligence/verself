# Directory Structure

Monorepo rooted at the repo top level. Bazel owns the repo-level build graph; each Go service keeps its own `go.mod`, and the TypeScript monorepo is pnpm-driven under `src/frontends/viteplus-monorepo/`.

## Top level

- `src/` — all first-party source.
- `docs/` — cross-service architecture docs and vendored references (`docs/references/` is read-only third-party material).
- `artifacts/` — gitignored materialized build/deploy outputs.
- `smoke-artifacts/` — gitignored live smoke-test bundles, personas, Playwright traces, and query evidence.
- `scripts/` — platform bootstrap shell entrypoints only (`bootstrap-linux-amd64`, `bootstrap-darwin-arm64`).
- `MODULE.aspect` + `.aspect/` — canonical task surface for founder/agent workflows. Run `aspect` (no args) for the full list; read before reaching for ad-hoc scripts.

## Source Owners (`src/`)

- `host-configuration/` — Ansible host convergence, server tool admission,
  host-local operators, host component roles, component-owned ClickHouse
  migrations, and authored HAProxy templates. Ansible lives here only.
- `domain-transfer-objects/` — shared data-transfer contracts for service
  boundaries, OpenAPI-compatible DTOs, shared protobuf schemas, numeric wire
  primitives, and generated-client contract rules.
- `frontends/` — browser and future client applications. The current
  TypeScript workspace is `frontends/viteplus-monorepo/`.
- `sdks/` — generated and curated client layers, validators, and package-local
  SDK adapters.
- `services/` — product API services, service-local workers, service-owned
  databases, migrations, and shared service runtime packages.
- `substrate/` — privileged host and guest substrate binaries that sit outside
  the service mesh, including `vm-orchestrator/` and `vm-guest-telemetry/`.
- `tools/` — controller/operator/deployment tooling, provisioning, shared
  operator runtime packages, and observability query tools.

## Product Services (`src/services/`)

- `sandbox-rental-service/` — compute product control plane (executions,
  checkpoint refs, billing windows).
- `billing-service/` — Reserve/Settle/Void on TigerBeetle + PostgreSQL.
- `iam-service/`, `mailbox-service/`, and other `*-service/` packages —
  service-owned databases, migrations, Huma APIs, and service-local workers.
- `service-runtime/auth/` — local JWT validation against Zitadel JWKS plus shared
  SPIFFE workload identity helpers.
- `service-runtime/` — shared service startup/runtime packages such as Go env
  loading and HTTP listener policy.

## Substrate (`src/substrate/`)

- `vm-orchestrator/` — privileged host daemon (Firecracker, ZFS, TAP, jailer,
  vm-bridge, gRPC over Unix socket).
- `vm-guest-telemetry/` — Zig guest agent streaming 60Hz health over vsock.

## Tooling (`src/tools/`)

- `deployment/` — typed deploy orchestration binary and Nomad job resolution
  rules.
- `dev/` — controller development tool catalog plus operator/bootstrap command
  binaries.
- `operator-runtime/` — shared operator database and evidence access packages.
- `observability/` — shared telemetry packages and operational query tools.
- `provisioning/` — OpenTofu bare-metal allocation and inventory production.

## Frontend (`src/frontends/viteplus-monorepo/`)

- `apps/` — TanStack Start applications:
  - `company` — Guardian Intelligence company site on `company_domain` (guardianintelligence.org). Owns landing, `/design`, `/letters` (+ RSS), `/solutions`, `/company`, `/careers`, `/press`, `/changelog`, `/contact`, `/og/*` dynamic OG cards. Forker-friendly split: `src/content/`, `src/brand/`, `src/routes/`, `src/components/`.
  - `verself-web` — the unified product app on the `verself_domain` apex. Owns the authenticated browser console (sandbox, billing, identity, profile, notifications, mail, source, future product workflows behind TanStack Start server functions), the public docs at `/docs` and `/docs/reference`, and the canonical legal tree at `/policy/*` (Terms, Privacy, DPA, AUP, Cookies, Security, SLA, Subprocessors, Data Retention, Policy Changelog).
- `packages/` — shared UI, brand marks, generated OpenAPI clients, Valibot validators.

## Provisioning Tools (`src/tools/provisioning/`)

- `terraform/` — OpenTofu bare-metal provisioning for Latitude.sh.
- `ansible/` — local controller playbooks that apply/destroy the OpenTofu
  state and write host inventory.
- `scripts/` — provisioning helpers such as inventory generation.

Provisioning tools own physical machine allocation and inventory production.
They do not converge host packages or deploy services.

## Host Configuration (`src/host-configuration/`)

- `ansible/` — current private runner for host, daemon, and per-component
  prerequisite convergence.
- `components/` — platform component Ansible roles and optional
  component-owned Nomad jobs.
- `components/clickhouse/migrations/` — host convergence ClickHouse schema.
- `scripts/` — founder/agent wrappers invoked by AXL tasks for deploy,
  persona, billing, mail, database access, observability, and host evidence.
Topology vars are authored in `src/host-configuration/ansible/group_vars/all/topology/`.
Host firewall files are authored in `src/host-configuration/ansible/host-files/`.
Nomad jobs live with their owning service, frontend, or component as
`nomad.hcl`. The deploy runner wires owner-local jobs to artifact delivery and
rollout inputs directly through Bazel and Nomad.

## Service- and host-local docs

Host convergence, OpenTofu provisioning, and deploy wrappers live in
`src/host-configuration/`, `src/tools/provisioning/`, and `.aspect/`.

Bazel-owned package definitions live with their owners:
`src/host-configuration/binaries/` for server and host configuration tools,
`src/tools/dev/binaries/` for controller dev tools, and
`src/substrate/vm-orchestrator/guest-images/` for guest-image inputs.

Service-local docs live under each service's `docs/` directory (e.g. `src/services/sandbox-rental-service/docs/`). Directory-specific conventions are captured in per-directory `AGENTS.md` files.
