# forge-metal

Self-hosted "software company in a box": Forgejo + Fast CI via Firecracker/ZFS, Grafana + ClickHouse observability, TigerBeetle + Stripe billing, Zitadel auth, PostgreSQL. Single-node bare metal default, upgrade path to 3-node k3s. Free OSS, operator owns what they deploy.

`.claude/CLAUDE.md` is a symlink to this file.

## Polyglot Layout

- `src/viteplus-monorepo/` — TypeScript frontends (Vite Plus + TanStack Start/DB/Query/Router).
- `src/*-service/` — Go services (Huma v2).
- `src/vm-orchestrator/` — Go host daemon (Firecracker / ZFS / TAP / jailer / vm-bridge / telemetry aggregation). gRPC over Unix socket. Only Linux-root service.
- `src/vm-guest-telemetry/` — Zig guest agent streaming 60Hz health frames over vsock port 10790.
- `src/platform/` — Ansible + OpenTofu + operator CLI. Owns all remote orchestration.

**Always check per-directory `AGENTS.md` before editing inside a subtree.** They carry the specifics this file deliberately omits.

## Safety Rings

- **Internet-exposed**: frontends, sandbox-rental-service, mailbox-service, billing webhook, Caddy (Coraza WAF), Forgejo, Grafana. nftables-hardened.
- **Private / Linux userspace**: billing-service, Postgres, ClickHouse, TigerBeetle, Zitadel, Stalwart.
- **Linux root**: ZFS, vm-orchestrator.

## External Providers

Hard product requirement: everything self-hosted. The only permitted external dependencies:

| Concern | Provider | Status |
|---|---|---|
| Domain Registrar | Cloudflare | Required |
| Compute | Latitude.sh | Required |
| Email Delivery | Resend (inbound via Stalwart) | Required |
| Payments / Tax / Dunning / Invoicing | Stripe | Required |
| Backups | B2 / R2 / S3 via `zfs send` | Planned, not implemented |

## Service Contract

Every Go service:

- **Huma v2** framework (https://pkg.go.dev/github.com/danielgtaylor/huma/v2). Review Huma docs before writing boundary code.
- Ships both **OpenAPI 3.0** (`oapi-codegen` Go clients) and **OpenAPI 3.1** (`@hey-api/openapi-ts` TS + Valibot) specs in `openapi/`. Never hand-write clients.
- Uses shared wire DTOs from `src/apiwire/` at boundaries. Owns the numeric-safety contract.
- Imports `src/auth-middleware/` for Zitadel JWT validation. Do not fork.
- Imports `src/otel/` for OTel + deploy-trace correlation.
- `pgx` + `database/sql`, Postgres-per-service. Migrations live in the service (e.g. `src/billing-service/postgresql-migrations/`). Platform provisions DB + role; service role applies migrations.
- Loads secrets via systemd `LoadCredential=` from `/etc/credstore/{service}/`, fed from SOPS (`group_vars/all/secrets.sops.yml`).

**Auth boundary**: services validate JWTs and enforce authorization. Frontend `beforeLoad` checks are SSR gating + UX only — never the final enforcement layer. Violations are critical security bugs. TanStack Start uses server-owned OAuth sessions in the `frontend_auth_sessions` PG database; browser code does not persist Zitadel tokens. JWKS mechanics: `src/auth-middleware/AGENTS.md`.

**Product IAM**: fixed three-role model (`owner`, `admin`, `member`). Admins toggle code-owned member-capability bundles via a switchboard — no customer-editable policy grid. Zitadel owns identity / org / role-assignment state; each service owns and enforces its operation catalog. See `src/platform/docs/identity-and-iam.md`.

**Dual-write**: services producing data for both real-time UX and analytics write to PostgreSQL (live sync via ElectricSQL → TanStack DB) and ClickHouse (dashboards, metering, history) in the same request path, reconciled periodically. Rationale and 3-node evolution: `src/billing-service/AGENTS.md`.

**Dogfooding philosophy** (aspirational, not yet upheld): every service should be multi-tenant and org-based, and we should be its principal customer via the same policy + billing abstractions, with usage unlimited by entitlement and invoices netting to zero via adjustment — not by bypassing control planes.

## Directory Map

Shared Go libraries (no `cmd/`, imported by every service):

- `src/apiwire/` — cross-service DTO wire language. Numeric-safety contract. See `src/apiwire/AGENTS.md`.
- `src/auth-middleware/` — Zitadel JWKS validation. Single source of truth. See `src/auth-middleware/AGENTS.md`.
- `src/otel/` — OTel bootstrap + deploy-trace correlation.

Go services:

- `src/identity-service/` — Forge Metal IAM layered on Zitadel.
- `src/billing-service/` — credit-based subscription billing (Stripe + TigerBeetle + PG tri-store). Includes `tb-inspect/` debugger.
- `src/sandbox-rental-service/` — product control plane for the three sandbox products (River queue, execution state machine, billing windows).
- `src/mailbox-service/` — inbound mail control plane fronting Stalwart JMAP.
- `src/vm-orchestrator/` — privileged root daemon.

Frontends (`src/viteplus-monorepo/apps/`): `rent-a-sandbox`, `letters`, `mail`. Shared packages (`packages/`): `auth-web`, `ui`, `nitro-plugins`, `web-env`.

## Where To Look

| | |
|---|---|
| Ports | `src/platform/ansible/group_vars/all/services.yml` |
| ClickHouse schemas (ground truth) | `make clickhouse-schemas` |
| Query ClickHouse / traces / deploy telemetry | `src/platform/AGENTS.md` |
| Secrets layout | SOPS → `/etc/credstore/{service}/` via `LoadCredential=` |
| vm-orchestrator gRPC wire | `src/vm-orchestrator/proto/`, `src/vm-orchestrator/vmproto/` |
| OpenAPI contracts | each service's `openapi/` (3.0 + 3.1) |
| Ansible playbooks + `make` targets | `src/platform/AGENTS.md` |
| Bare-metal inventory | `src/platform/ansible/inventory/hosts.ini` |
| Cross-cutting arch docs | `src/platform/docs/` |
| Per-service arch docs | `src/{service}/docs/*.md` |

## Working Agreement

- **Pre-customer repo.** No backwards compatibility, compat wrappers, legacy shims, or temporary plumbing. Full cutover only. Delete outdated code when you upgrade abstractions — contradictions are higher-priority than symmetry.
- **Verification is ClickHouse traces, not tests.** Unit tests and successful builds are low signal. Real OTel traces in ClickHouse that exercise modified code are the only admitted completion proof. Create a new schema if needed. A task is not done until a fresh playbook rehearsal (rebuild + redeploy) has produced trace evidence. Databases may be wiped freely during this phase; prefer wipes over tricky migrations.
- **Scientific method first.** Define a bar-raising verification protocol *before* implementing. It must fail initially, then implement until green.
- **No host-cause speculation** (resource exhaustion, network) without first checking ClickHouse via `make clickhouse-query` / `make traces` / host metrics.
- **Destructive commands are blocked** (`git restore`, `git checkout -- <file>`, `rm -rf`). Don't try to bypass — investigate root cause. Failures should be loud, not silent no-ops.
- **Refactors**: use subagents rather than one-shot non-syntax-aware scripts. Judgement is usually required.
- **Long-running work** runs in the background, checked every 30–60s.
- **Dev tools** are system-installed via `ansible-playbook playbooks/setup-dev.yml`. No `nix develop` prefix.
- **Formatting**: `make tidy` for Go + TS. Edits from linter/formatter runs beyond what you intended are expected and not your problem.
- **Parallel work**: you are not alone in this repo. Expect concurrent edits in unrelated files.
- **Tone**: dispassionate advisory technical leader. Simplicity for code and architecture; keep technical explanations detailed — the reader cares about subtle race conditions, timing-attack vulnerabilities, GC pressure, leaky abstractions. The user is a non-technical startup founder running everything on one bare-metal box, so also think from the operator's perspective on product decisions.
- **Boring beats novel.** Use industry-standard patterns — pagination, idempotency, rate limiting, OpenAPI, OTel, state machines — they're solved. The only genuinely novel piece here is ZFS + Firecracker for customer workloads.

## Coding Footguns

- **ClickHouse inserts**: use `batch.AppendStruct` with `ch:"column_name"` tags. Positional `batch.Append` silently corrupts data on column reorder/add.
- **ClickHouse queries**: bind dynamic values (including Map keys) via `$1`, `$2`. Never `fmt.Sprintf` interpolate. Use `arrayElement(map_col, $N)` instead of `map_col['{x}']`.
- **ClickHouse schema**: ORDER BY ascending cardinality (low-card first). Avoid `Nullable` (hidden UInt8 per row); use empty defaults. `LowCardinality(String)` under ~10k distinct values. Smallest sufficient integer type.
- **Playwright timeouts**: start at 1s, never above 5s. Playwright reports every failure as a timeout — the cause is always wrong behavior, not slow response. Everything is local bare metal.
- **Python package management**: `uv` only. No pip/conda.
- **Ansible**: fail fast with useful logging. No fallbacks or defaults. Files must end in a newline (`ansible-lint` enforces). Prefer Ansible over shell scripts except in extreme bootstrap.
- **Errors as data**: tagged/structured errors for control flow.
- **When you hit a footgun**, leave a single-sentence comment next to the workaround.

## Technical Writing

Target: distinguished engineers already expert in the underlying tech who just need to know how this system deviates from standard practice. Skip throat-clearing.
