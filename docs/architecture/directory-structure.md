# Directory Structure

Monorepo rooted at the repo top level. All Go services share one `go.work`; the TypeScript monorepo is pnpm-driven under `src/viteplus-monorepo/`.

## Top level

- `src/` — all first-party source.
- `docs/` — cross-service architecture docs and vendored references (`docs/references/` is read-only third-party material).
- `artifacts/` — gitignored run outputs (proof bundles, personas, playwright traces).
- `Makefile` — canonical entry point for operator/agent workflows. Read before reaching for ad-hoc scripts.

## Go services (`src/`)

- `vm-orchestrator/` — privileged host daemon (Firecracker, ZFS, TAP, jailer, vm-bridge, gRPC over Unix socket).
- `vm-guest-telemetry/` — Zig guest agent streaming 60Hz health over vsock.
- `sandbox-rental-service/` — compute product control plane (executions, checkpoint refs, billing windows).
- `billing-service/` — Reserve/Settle/Void on TigerBeetle + PostgreSQL.
- `identity-service/`, `mailbox-service/`, `workload/` — service-owned databases, migrations, and Huma APIs.
- `apiwire/` — shared Huma DTOs and wire-language types.
- `auth-middleware/` — local JWT validation against Zitadel JWKS.
- `otel/` — shared OpenTelemetry wiring.

## Frontend (`src/viteplus-monorepo/`)

- `apps/` — TanStack Start applications: `rent-a-sandbox`, `letters`, `mail`. `letters` is the operator's blog.
- `packages/` — shared UI, generated OpenAPI clients, Valibot validators.

## Platform (`src/platform/`)

- `ansible/` — playbooks, roles, SOPS-encrypted `group_vars/`, inventory.
- `terraform/` — OpenTofu bare-metal provisioning (Latitude.sh).
- `scripts/` — operator wrappers invoked by the Makefile.
- `server-tools.json` / `dev-tools.json` — pinned binary manifests (URL + SHA256).

Service-local docs live under each service's `docs/` directory (e.g. `src/sandbox-rental-service/docs/`). Directory-specific conventions are captured in per-directory `AGENTS.md` files.
