# Directory Structure

Monorepo rooted at the repo top level. Bazel owns the repo-level build graph; each Go service keeps its own `go.mod`, and the TypeScript monorepo is pnpm-driven under `src/websites/`.

## Top level

- `src/` — all first-party source.
- `docs/` — cross-service architecture docs and vendored references (`docs/references/` is read-only third-party material).
- `artifacts/` — gitignored materialized build/deploy outputs.
- `smoke-artifacts/` — gitignored live smoke-test bundles, personas, browser evidence, and query evidence.
- `scripts/` — platform bootstrap shell entrypoints only (`bootstrap-linux-amd64`, `bootstrap-darwin-arm64`).
- `MODULE.aspect` + `.aspect/` — canonical task surface for founder/agent workflows. Run `aspect` (no args) for the full list; read before reaching for ad-hoc scripts.

## Source Owners (`src/`)

- `host/` — Ansible host bootstrap, server tool admission, site facts, SOPS
  bootstrap material, bootstrap ClickHouse schema, and host-local operators.
- `components/` — platform components such as PostgreSQL, Garage, OpenBao,
  Zitadel, NATS, TigerBeetle, Electric, ClickHouse migrations, and HAProxy
  upstream reconciliation. Component Nomad descriptors live with their owning
  component.
- `domain-transfer-objects/` — shared data-transfer contracts for service
  boundaries, OpenAPI-compatible DTOs, shared protobuf schemas, numeric wire
  primitives, and generated-client contract rules.
- `websites/` — browser applications and shared web packages.
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
  golden workspace generations, billing windows).
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

## Frontend (`src/websites/`)

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

## Host Configuration (`src/host/`)

- `ansible/` — host bootstrap roles and manual host playbooks.
- `binaries/` — Bazel-owned host/server tool catalog inputs.
- `sites/<site>/` — site facts, inventory, provisioning input, and SOPS bags.
- `migrations/` — bootstrap ClickHouse schema needed before the deploy evidence path exists.
The former centralized topology vars have been split: host bootstrap facts live
under `src/host/sites/<site>/`, while component/service/frontend deployment
metadata lives with the owning package.
Host firewall foundation files are authored in `src/host/ansible/host-files/`;
component, service, frontend, and privileged-substrate nftables snippets live
with the owning package.
The host bootstrap boundary covers the machine foundation, recovery access,
ZFS, SPIRE, HAProxy, ClickHouse initial schema, Nomad, and devtools.
Nomad jobs live with their owning service, frontend, or component as
`nomad.hcl`. The deploy runner wires owner-local jobs to artifact delivery and
rollout inputs directly through Bazel and Nomad.

## Platform Components (`src/infrastructure-components/`)

- `<component>/BUILD.bazel` — component descriptor, Nomad packaging, runtime
  user metadata, SPIRE identities, credential bindings, route metadata, and
  dependency `requires`/`provides`.
- `<component>/nomad.hcl` — Nomad job for service tasks, prestart tasks, batch
  migrations, and component-local service registration.
- `<component>/tasks/` — temporary Ansible substrate tasks while a component is
  being cut over; the target owner for runtime convergence is the component's
  Nomad job or batch reconciler.
- `<component>/cmd/` — component-specific reconcilers or lifecycle helpers
  written as typed binaries.

ClickHouse keeps the server bootstrap split in `src/host` and owns subsequent
migrations under `src/infrastructure-components/clickhouse`. HAProxy keeps the edge process in
host bootstrap and owns Nomad-discovered upstream reconciliation under
`src/infrastructure-components/haproxy`. PostgreSQL and Garage are regular Nomad components;
Garage also participates in the pre-artifact deploy wave.

## Service- and host-local docs

Host convergence, OpenTofu provisioning, and deploy wrappers live in
`src/host/`, `src/tools/provisioning/`, and `.aspect/`.

Bazel-owned package definitions live with their owners:
`src/host/binaries/` for server and host configuration tools,
`src/tools/dev/binaries/` for controller dev tools, and
`src/substrate/vm-orchestrator/guest-images/` for guest-image inputs.

Service-local docs live under each service's `docs/` directory (e.g. `src/services/sandbox-rental-service/docs/`). Directory-specific conventions are captured in per-directory `AGENTS.md` files.
