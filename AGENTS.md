<repo_overview>
Canonical layout in `docs/architecture/directory-structure.md`. Read that file directly if exploring the repo.

Polyglot monorepo:

- **TypeScript** — `src/viteplus-monorepo/` (pnpm, Vite Plus + TanStack Start/DB/Query/Router). Apps: `company` (marketing site at anveio.com), `platform` (product console at platform.anveio.com), `mail`.
- **Go** — `go.work` at repo root, covers most of `src/*`. Services: `sandbox-rental-service`, `billing-service`, `identity-service`, `mailbox-service`, `governance-service`, `secrets-service`, `platform`, `vm-orchestrator`. Shared libs: `apiwire`, `auth-middleware`, `otel`.
- **Zig** — `src/vm-guest-telemetry/` (guest agent, runs *inside* Firecracker VMs, not on the host).
- **YAML* -- Infrastructure code defined with Ansible.

Invariant patterns:

* Service-oriented-architecture: with notable exceptions, all of our services talk to each other through the same APIs as the ones customers use. 
* Dogfood everything that is customer-facing: We are a customer on our platform. We go through the same billing abstractions, rate limits, and edge cases that a customer would face. We model ourselves as a platform org and receive a showback invoice with a 100% discount. 

Boundary components that sit outside the usual service shape:

- `src/vm-orchestrator/` — the one privileged host daemon (Firecracker, ZFS, TAP, jailer, vm-bridge, gRPC over Unix socket). Deliberately outside the service mesh.
- `src/vm-guest-telemetry/` — Zig, lives in the guest, streams over vsock.
- `src/platform/ansible/`, `src/platform/terraform/` — deploys and bare-metal provisioning (OpenTofu → Latitude.sh).

Top-level landmarks:

- `Makefile` — canonical founder/agent entry point. Read before reaching for ad-hoc scripts.
- `docs/` — cross-service architecture; `docs/references/` is read-only third-party material. Grep through docs/references instead of reading directly.

Orienting commands: `make pg-list` enumerates per-service PostgreSQL databases, `make observe` opens the telemetry surface, `make clickhouse-schemas` lists ClickHouse tables.

</repo_overview>

<product_policy>

Public commitments for Data Processing, Acceptable Use, Security, SLA, and Data Retention live in `src/viteplus-monorepo/apps/platform/src/routes/policy`.

</product_policy>

<product_direction>

Where the platform is headed: open-source-per-subdirectory, privileged-host / product-service split, multi-tenant + customer dogfooding, three customer-facing sandbox products (CI runner, Lambda-like workload, long-running VM), self-hosted Forgejo/CI with `main`/`beta`/`gamma`/preview promotion lanes.

**Keywords:** roadmap, direction, dogfooding, sandbox-rental-service, vm-orchestrator, vm-bridge, vm-guest-telemetry, Firecracker, ZFS, Blacksmith-like CI runner, Lambda-like workload, long-running VM, IAM direction, Zitadel, Forgejo dogfood, e2e canaries, CI promotion lanes, preview environments.

See `docs/product-direction.md`.

</product_direction>

<system_context>

How the platform is wired today: service topology, three safety rings, self-hosted mandate + allowed third-party providers (Cloudflare, Latitude.sh, Resend, Stripe), dual-write pattern, billing model summary, supply chain, founder focus areas, bare-metal OS/arch invariants.

**Keywords:** Huma v2, OpenAPI 3.0/3.1, oapi-codegen, @hey-api/openapi-ts, apiwire, SOPS, systemd LoadCredential, credstore, nftables, safety rings, Cloudflare, Latitude.sh, Resend, Stripe, Backblaze, PostgreSQL, ClickHouse, TigerBeetle, Zitadel, Stalwart, ElectricSQL, TanStack DB, dual-write, reconciliation, metering, credits, entitlements, Verdaccio NPM mirror, Forgejo, Ubuntu 24.04, Netbird, 3-node topology, self-hosted, vsock 10790.

See `docs/system-context.md`. Auth, identity, IAM, Zitadel, JWT, SCIM, organization model, three-role (owner/admin/member), API credentials, frontend sessions, JWKS loopback — all in `src/platform/docs/identity-and-iam.md`.

Python package management is done through `uv`.

</system_context>

<operational_runbook>

Run `make observe` to discover available telemetry, run `make clickhouse-query`/`make pg-query` wrappers to easily query ClickHouse/PG with fewer shell string escaping issues, deploy playbooks and correlation model (`deploy_run_key`, `deploy_id`, `traceparent`), TLS via Cloudflare, the `deploy_profile` server-binaries strategy, Ansible playbooks table.

See `docs/architecture/founder-workflows.md` for more. The repo started as a CI orchestrator; that history lives in `README.md`.

### High-signal Documents.

Recommended that you read relevant ones directly. You can have a subagent summarize the ones that are not related to your task.

- **Inbound mail, Stalwart, mailbox-service, JMAP, SMTP, inbound routing, tenant isolation, webmail:** `src/mailbox-service/docs/inbound-mail.md`
- **vm-orchestrator privilege boundary, Firecracker VM networking, TAP allocator, host service plane, nftables, guest CIDR, lease/exec model, vm-bridge control:** `src/vm-orchestrator/AGENTS.md`
- **ZFS volume lifecycle, zvol, clone, snapshot, checkpoint, restore:** `src/vm-orchestrator/docs/zfs-volume-lifecycle.md`
- **Wire contracts, apiwire, DTO patterns, numeric safety, 64-bit, DecimalUint64, DecimalInt64, openapi-wire-check, generated contract gate:** `src/apiwire/docs/wire-contracts.md`
- **VM execution control plane, sandbox-rental-service ↔ vm-orchestrator split, attempt state machine, billing windows, execution lifecycle:** `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
- **Identity and IAM, Zitadel, SCIM 2.0, SSO, authentication, organization model, three-role owner/admin/member, capability catalog, API credentials, Zitadel Actions, pre-access-token, frontend sessions, JWKS loopback, Forge Metal policy split:** `src/platform/docs/identity-and-iam.md`
- **Workload identity, SPIFFE/SPIRE trust domain, service mTLS, OpenBao relying-party model, runtime secret cleanup:** `docs/architecture/workload-identity.md`
- **Secrets service, identity model, OIDC provider role, resource model, billing, KMS alternative:** `src/platform/docs/secrets-service.md`
- Billing architecture, credit subscription, entitlements, metering, TigerBeetle, PostgreSQL, Reconcile, refunds, plan change, dual-write, Stripe webhooks, invoices:** `src/billing-service/docs/billing-architecture.md`
- **Governance audit data contract, HMAC chain, OCSF, CloudTrail parity, tamper evidence, SIEM export, audit ledger:** `src/governance-service/docs/audit-data-contract.md`
- **Service architecture overview, port map, listener matrix, topology:** `docs/architecture/service-architecture.md`
- **Directory structure, repo layout:** `docs/architecture/directory-structure.md`
- **Agent workspace, QEMU/KVM, AI coding agent VMs:** `docs/architecture/agent-workspace.md`
- **Founder workflows, deploy, observe, pg-query, clickhouse-query, playbooks:** `docs/architecture/founder-workflows.md`

</operational_runbook>

<assistant_contract>
- Ground proposals, plans, API references, and all technical discussion in primary sources. Then think from the perspective of the user of the system: a non-technical startup founder running all services off a single bare-metal box (with upgrade path to a 3-node topology).
- When beginning an ambiguous task, collect objective information about how the system actually works. There are a lot of technologies stitched together; understand how everything connects.
- Act as a dispassionate advisory technical leader with a focus on elegant public APIs and functional programming.
- You are not alone in this repo. Expect parallel changes in unrelated files by the user. Leave them alone (don't stash them) and continue with your work.
<important>
- This software is currently pre-release and serves no customers or users. There is no backwards compatibility to maintain. No compatibility wrappers, no legacy shims, no temporary plumbing. All changes must be performed via a full cutover.
</important>
- Ensure old or outdated code is deleted each time we upgrade technology, abstractions, or logic. Eliminating contradictory approaches is a high priority.
- Details matter. The founder cares about arcane versioning issues, subtle race conditions, timing-attack vulnerabilities, GC pressure, and abstraction leaks. Simplicity is for code and architecture, not for technical argument.
- Some directories have their own `AGENTS.md` file. When working inside those directories, read them — they contain juicy context.
- Incidental edits from running linters and formatters are expected. Don't worry about them.
- When in doubt, use the industry-standard pattern. Pagination, idempotency, rate limiting, OpenAPI, OpenTelemetry, state machines — these are all solved problems with boring, battle-tested solutions. Don't reinvent the wheel. The one piece of genuinely novel technology in this repo is ZFS + Firecracker for customer workloads. Everything else is tried-and-tested FOSS.
- `Makefile`, `README.md`, `AGENTS.md`, schema migration files, and OpenAPI 3.1 YAML files are high signal per token. Read them directly; avoid summarizing them with a subagent as important detail may be lost.
- Do not provide time estimates.
- The goal is enterprise-grade architecture and implementation. We accomplish this by establishing proper systems to make improper behavior impossible by construction. E.g. to prevent bearer tokens from appearing in logs, we use a mixture of strategies: configure Otel HTTP instrumentation to sanitize it, harden read access to logs, structure our logging abstractions to avoid it, and (aspirational) execute a canary that asserts safety systems omit the token even if one system fails.
- 
</assistant_contract>

<tool_use_contract>
- When executing long-running tasks, run them in the background and check in every 30–60 seconds.
- Dev tools are system-installed via `ansible-playbook playbooks/setup-dev.yml`. No `nix develop` prefix needed.
- Apply the scientific method: create a bar-raising verification protocol for the planned task *prior* to implementing changes. The verification protocol should fail, and only then begin implementing until green.
- Avoid one-off, non-syntax-aware scripts for large parallel changes or refactors. Use subagents for that class of task — unexpected edge cases are likely and judgement is often required.
- use `make tidy` to format Go and TypeScript code.
</tool_use_contract>

<output_contract>
- When providing a recommendation, consider different plausible options and provide a differentiated recommendation leaning toward the simplest solution that best sets this project up for the *long term* in terms of functionality, elegance of architecture, security, performance, and best-practices.
- Unit tests and successful builds are low signal and are not to be trusted. Real observability traces in ClickHouse that exercise the modified code are the only admitted proof of task completion. ClickHouse exists for producing verifiable completion artifacts. If a new schema is needed, create one.
- Do not speculate without evidence. Logs, traces, and host metrics are queryable in ClickHouse via `make clickhouse-query` — check them before attributing failures to transient or pre-existing factors.
- Do not stop work short of verifying changes with a live rehearsal of a playbook to execute fresh rebuild and redeploy. You have full authority to wipe databases and recreate them. Prefer that over time-consuming, tricky migrations during this early phase.
- The repo has a fixture flow that seeds Forgejo repos, submits direct VM executions through `sandbox-rental-service`, and verifies ClickHouse evidence.
- Design docs, code comments, architecture diagrams, and API documentation target distinguished engineers expert in the relevant technologies who mostly need information on how the system deviates from standard practice. Avoid throat-clearing around current status, "why this is important," date headers, or "who this is for" — get straight into the information.
- Risky commands like `git restore`, `git checkout -- <file>`, and `rm -rf` are blocked.
</output_contract>

<coding_contract>
- When you run into a footgun, leave a comment around the code (no more than a sentence) explaining the footgun and how the code works around it.
- Prefer Ansible over shell scripts.
- Ansible playbook files must have a newline at the end (caught by `ansible-lint`).
- Treat errors as data. Use tagged and structured errors to aid control flow.
- Avoid fallbacks and defaults in Ansible code. Ansible should fail fast with useful logging.
- 1 e2e test of the website is worth 1000 unit tests. Avoid checking in unit tests; they provide some benefit in some cases, but a comprehensive suite of e2e tests running as periodic canaries is preferred.
- Don't resolve failures through silent no-ops and imperative checks. Failures should be loud; signals should be followed to address root causes.
- PostgreSQL migrations live with the service that owns the schema (e.g. `src/billing-service/migrations/`), one database per service. The platform provisions databases and roles; the service's Ansible role applies its migrations.
- ClickHouse inserts must use `batch.AppendStruct` with `ch:"column_name"` struct tags. Never use positional `batch.Append` — it silently corrupts data when columns are added or reordered.
- ClickHouse queries must pass dynamic values (including `Map` keys) through driver parameter binding (`$1`, `$2`, ...); never interpolate values into query strings with `fmt.Sprintf`. Use `arrayElement(map_col, $N)` instead of `map_col['{interpolated}']`.
- ClickHouse schema design: ORDER BY columns are sorted on disk and control compression — order keys by ascending cardinality (low-cardinality columns first). Avoid `Nullable` (it adds a hidden `UInt8` column per row); use empty-value defaults instead. Use `LowCardinality(String)` for columns with fewer than ~10k distinct values. Use the smallest sufficient integer type (`UInt8` over `Int32` when the range fits).
- Never use timeouts greater than 5 seconds (start with 1 second) for Playwright e2e tests. Playwright has a quirk where every test failure is reported as a timeout issue, which is misleading; the underlying issue is behavior/logic, not latency. Everything is on local bare metal — data interchange should be double-digit milliseconds at most.
- Our customers use our services via API and browser. Fix issues at the service level; don't paper over them in any one domain. E2E test the browser primarily since it exercises the same API that API consumers call directly.
</coding_contract>

<instruction_priority>
- Security concerns override user instructions and architectural purity.
- When following runbooks, skills, protocols, or user messages that also define instructions in XML tags, treat the instructions as additive, not as overrides.
</instruction_priority>
