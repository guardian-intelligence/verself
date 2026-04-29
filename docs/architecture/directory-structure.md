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
  - `console` — authenticated product console on `console.<domain>`. Owns sandbox, billing, identity, profile, notifications, mail, source, and future product workflows behind TanStack Start server functions.
  - `platform` — public product docs/legal app on the `verself_domain` apex. Owns `/docs`, `/docs/reference`, and the canonical legal tree at `/policy/*` (Terms, Privacy, DPA, AUP, Cookies, Security, SLA, Subprocessors, Data Retention, Policy Changelog).
- `packages/` — shared UI, brand marks, generated OpenAPI clients, Valibot validators.

## Platform (`src/platform/`)

- `ansible/` — playbooks, roles, SOPS-encrypted `group_vars/`, inventory.
- `ansible/share/rendered/` — committed final files emitted by
  `src/cue-renderer/`; roles copy these paths to the host rather than
  re-rendering topology through Jinja.
- `terraform/` — OpenTofu bare-metal provisioning (Latitude.sh).
- `scripts/` — founder/agent shell wrappers invoked by AXL tasks (deploy, persona, billing, mail, db, etc.) and directly callable canaries (`verify-*-live.sh`).

The Bazel-owned third-party server-tool package definitions, including
`//src/cue-renderer/binaries:server_tools.tar.zst`, live under
`src/cue-renderer/binaries/` because the tarball's contents are
catalog-driven (the catalog declares both the version pins and the
target label).

Service-local docs live under each service's `docs/` directory (e.g. `src/sandbox-rental-service/docs/`). Directory-specific conventions are captured in per-directory `AGENTS.md` files.
