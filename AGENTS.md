<repo_overview>
Set of services + console + marketing page for a software business, almost entirely self-hosted on a single bare metal node.

Most commands should begin with either `aspect`. Use regular `bazelisk` for basic build/test.

Canonical layout in `docs/architecture/directory-structure.md`. Read that file directly if exploring the repo.

Polyglot monorepo structured as a modular monolith.
Layers:

1. Substrate layer: vm-orchestrator, guest telemetry, Caddy, nftables, ClickHouse, Postgres, Forgejo
2. Product API layer: service-owned Huma APIs at <service>.api.<domain>, with internal SPIFFE-only APIs separate.
3. Generated client layer: pure transport clients, validators, DTOs, schemas.
4. Curated SDK layer: stable hand-written exports that wrap generated clients and own auth, idempotency keys, retries, pagination, waiters, error normalization, tracing headers, and DTO conversion.
5. Facades: console.<domain> webapp, CLI, docs examples, Terraform provider later. These use the SDK, not private service shortcuts.

Tech Stack:

* ClickHouse for all time series data (host process metrics, time-series data from APIs), logs, traces, metrics (Wide Event pattern a. la Majors et. al/Honeycomb), miscellaneous append only event ledger where realtime policy decisions or UX isn't critical. ClickHouse rows never get updated
* TigerBeetle for OTLP. Currently using for financial truth and treating as a ledger -- we model debits/credits for 
* Zitadel for human identity & OIDC/SAML with third parties. We support multi-tenancy in that all users belong to an org (users belonging to multiple orgs not yet supported)
* Verdaccio to mirror NPM within our system to avoid north/south traffic being routine and to enforce minimum dependency age
* Caddy for ingress using nftables config with Jinja2 templates
* SPIRE for our SPIFFE implementation, everything speaks x509-SVIDs to eachother except for services that don't support SPIFFE, then we use short-lived JWT-SVIDs.
* Golang's River library for background jobs *within* a service. NATS JetStream for messaging/fan-out batch jobs between services.

Invariant patterns:

* Bazel owns build artifacts and dependency-scoped rebuilds: deploy code requests specific Bazel targets, so a frontend rebuild must not rebuild unrelated substrate binaries such as TigerBeetle.
* CUE owns desired platform shape and non-secret deployment configuration in `src/cue-renderer/`: components, processes, endpoints, routes, identities, ports, runtime users, required config, and references to Bazel artifact labels. Generated files under `src/platform/ansible/group_vars/all/generated/` are projections, not authority.
* Ansible owns host mutations and convergence only: it consumes generated CUE/Bazel manifests plus SOPS secret values, mutates the host idempotently, and must not invent topology, rebuild scope, ports, users, routes, or service relationships. SOPS owns encrypted secret values such as Cloudflare API tokens; CUE may declare that they are required.
* Service-oriented-architecture: with notable exceptions, all of our services talk to each other through the same APIs as the ones customers use. 
* Generated clients are the only supported Go service SDKs. Customer/human routes use the committed public OpenAPI specs and `client` packages; repo-owned SPIFFE routes use committed internal OpenAPI specs and `internalclient` packages. The caller injects a SPIFFE mTLS `http.Client` from `auth-middleware/workload`; do not hand-write `http.NewRequest` service calls or mint Zitadel machine-user bearer tokens for repo-owned service-to-service traffic.
* Non-retrievable product token material belongs in `secrets-service` as an opaque credential. Product services may keep metadata/projection rows, but token generation, verifier storage, roll/revoke semantics, and verification must go through generated secrets-service clients over SPIFFE.
* Dogfood as much as possible, even if it involves hairpinning requests through the internet. We are a customer on our platform. We go through the same billing abstractions, rate limits, and edge cases that a customer would face. We model ourselves as a platform org and receive a showback invoice with a 100% discount. 
* Sync-engine pattern: PostgreSQL owns state, ClickHouse records the append-only ledger/traces, Electric/TanStack expose live read projections, and writes go through typed service commands whose conflict behavior matches the domain (strict observed-state rejection for security-critical resources, monotonic/idempotent collapse for notification-style cursors and dismissals).

Boundary components that sit outside the usual service shape:

- `src/vm-orchestrator/` — the one privileged host daemon (Firecracker, ZFS, TAP, jailer, vm-bridge, gRPC over Unix socket). Deliberately outside the service mesh.
- `src/vm-guest-telemetry/` — Zig, lives in the guest, streams over vsock.
- `src/platform/ansible/`, `src/platform/terraform/` — deploys and bare-metal provisioning (OpenTofu → Latitude.sh).

Top-level landmarks:

- `.aspect/` — typed task surface. `aspect` (no args) lists every command; `aspect <task> --help` documents flags; `.aspect/config.axl` is the registration list. Use the typed `aspect <group> <action> --flag=value` form or raw `bazelisk`.
- `docs/` — cross-service architecture; `docs/references/` is read-only third-party material. Grep through docs/references instead of reading directly.

Orienting commands: `aspect db pg list` enumerates per-service PostgreSQL databases, `aspect observe` opens the telemetry surface, `aspect db ch schemas` lists ClickHouse tables.

</repo_overview>

<product_policy>

Public commitments for Data Processing, Acceptable Use, Security, SLA, and Data Retention live in `src/viteplus-monorepo/apps/platform/src/routes/policy`.

</product_policy>

<product_direction>

Where the platform is headed: open-source-per-subdirectory, privileged-host / product-service split, multi-tenant + customer dogfooding, three customer-facing sandbox products (CI runner, Lambda-like workload, long-running VM), self-hosted Forgejo/CI; agents merge to `main` continuously and environments deploy whichever SHA the `staging-tip`/`prod-tip` refs point at, advancing only after a canary soak passes — no long-lived release branches, unfinished work hidden behind feature flags.

See `docs/product-direction.md`.

</product_direction>

<system_context>

Service topology, three safety rings, self-hosted mandate + allowed third-party providers (Cloudflare, Latitude.sh, Resend, Stripe), dual-write pattern, billing model summary, supply chain, founder focus areas, bare-metal OS/arch invariants.

See `docs/system-context.md`. Auth, identity, IAM, Zitadel, JWT, SCIM, organization model, three-role (owner/admin/member), API credentials, frontend sessions, OIDC discovery — all in `src/platform/docs/identity-and-iam.md`.
Verself Go service clients are generated from committed OpenAPI 3.0 specs with `oapi-codegen`; consumers must use those generated `client` or `internalclient` packages, with SPIFFE carried by the underlying `http.Client` instead of handwritten transport code. If a service API shape is missing, add the Huma route/OpenAPI spec and regenerate instead of bypassing the SDK.
Go service code uses sqlc for type safe queries. Avoid reading code in generated directories.
Python package management is done through `uv`.
No need to be frugal with telemetry. We store 10+ million rows for around ~150MB in ClickHouse thanks to optimizations.

</system_context>

<read_directly>
The below documents contain critical sources of truth about the system, extremely high signal. Recommended that you read these directly regardless of your task instead of trusting summaries. Note that they contents are not exhaustive as the system is migrating away from handwritten ansible to CUE with a golang renderer.
```
@src/cue-renderer/instances/prod/topology.cue
@src/cue-renderer/instances/prod/config.cue
@src/cue-renderer/instances/prod/site.cue
@src/cue-renderer/catalog/versions.cue
```
</read_directly>

<operational_runbook>

Run `aspect observe` to discover available telemetry, run `aspect db ch query`/`aspect db pg query` wrappers to easily query ClickHouse/PG with fewer shell string escaping issues, deploy playbooks and correlation model (`deploy_run_key`, `deploy_id`, `traceparent`), TLS via Cloudflare, the `deploy_profile` server-binaries strategy, Ansible playbooks table.

The repo started as a CI orchestrator; that history lives in `README.md`.

### High-signal Documents.

Recommended that you read relevant ones directly. You can have a subagent summarize the ones that are not related to your task.

- **Inbound mail, Stalwart, mailbox-service, JMAP, SMTP, inbound routing, tenant isolation:** `src/mailbox-service/docs/inbound-mail.md`
- **vm-orchestrator privilege boundary, Firecracker VM networking, TAP allocator, host service plane, nftables, guest CIDR, lease/exec model, vm-bridge control:** `src/vm-orchestrator/AGENTS.md`
- **ZFS volume lifecycle, zvol, clone, snapshot, checkpoint, restore:** `src/vm-orchestrator/docs/zfs-volume-lifecycle.md`
- **Wire contracts, apiwire, DTO patterns, numeric safety, 64-bit, DecimalUint64, DecimalInt64, openapi-wire-check, generated contract gate:** `src/apiwire/docs/wire-contracts.md`
- **VM execution control plane, sandbox-rental-service ↔ vm-orchestrator split, attempt state machine, billing windows, execution lifecycle:** `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
- **Identity and IAM, Zitadel, SCIM 2.0, SSO, authentication, organization model, three-role owner/admin/member, capability catalog, API credentials, Zitadel Actions, pre-access-token, frontend sessions, OIDC discovery, Verself policy split:** `src/platform/docs/identity-and-iam.md`
- **Workload identity, SPIFFE/SPIRE trust domain, service mTLS, OpenBao relying-party model, runtime secret cleanup:** `docs/architecture/workload-identity.md`
- **Secrets service, identity model, OIDC provider role, resource model, billing, KMS alternative:** `src/platform/docs/secrets-service.md`
- Billing architecture, credit subscription, entitlements, metering, TigerBeetle, PostgreSQL, Reconcile, refunds, plan change, dual-write, Stripe webhooks, invoices:** `src/billing-service/docs/billing-architecture.md`
- **Governance audit data contract, HMAC chain, OCSF, CloudTrail parity, tamper evidence, SIEM export, audit ledger:** `src/governance-service/docs/audit-data-contract.md`
- **Service topology, port assignments, SPIRE identities, runtime users, generated Ansible inputs:** `src/cue-renderer/`
- **Directory structure, repo layout:** `docs/architecture/directory-structure.md`
- **Agent workspace, QEMU/KVM, AI coding agent VMs:** `docs/architecture/agent-workspace.md`

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
- `.aspect/`, `README.md`, `AGENTS.md`, schema migration files, and OpenAPI 3.1 YAML files are high signal per token. Read them directly; avoid summarizing them with a subagent as important detail may be lost.
- Do not provide time estimates.
- We work backwards from ensuring proper systems are in place to make incorrect behavior impossible by construction. E.g. to prevent bearer tokens from appearing in logs, we use a mixture of strategies: configure Otel HTTP instrumentation to sanitize it, harden read access to logs, structure our logging abstractions to avoid it, and (aspirational) execute a canary that asserts safety systems omit the token even if one system fails.
- My 'd' key is broken so you may see frequently see the letter 'd' missing from user messages
</assistant_contract>

<tool_use_contract>
- Dev tools are system-installed via `ansible-playbook playbooks/setup-dev.yml`.
- Apply the scientific method: create a bar-raising verification protocol for the planned task *prior* to implementing changes. The verification protocol should fail, and only then begin implementing until green.
- Avoid one-off, non-syntax-aware scripts for large parallel changes or refactors. Use subagents for that class of task — unexpected edge cases are likely and judgement is often required.
- use `aspect tidy` to run `go mod tidy` per service and format the JS monorepo.
- When using agent-browser, don't use the sandbox (`--no-sandbox`)
- Deploy frontend changes to prod fearlessly (e.g. `aspect deploy --tags=company` to deploy the company marketing website) -- I can't see your dev server.
</tool_use_contract>

<output_contract>
- When providing a recommendation, consider different plausible options and provide a differentiated recommendation leaning toward the simplest solution that best sets this project up for the *long term* in terms of functionality, elegance of architecture, security, performance, and best-practices.
- Unit tests and successful builds are low signal and are not to be trusted. Real observability traces in ClickHouse that exercise the modified code are the only admissible completion evidence. ClickHouse exists for producing verifiable completion artifacts. If a new schema is needed, create one.
- Do not speculate without evidence. Logs, traces, and host metrics are queryable in ClickHouse via `aspect db ch query --query='...'` — check them before attributing failures to transient or pre-existing factors.
- Do not stop work short of verifying changes with a live rehearsal of a playbook to execute fresh rebuild and redeploy. You have full authority to wipe databases and recreate them. Prefer that over time-consuming, tricky migrations during this early phase.
- The repo has a fixture flow that seeds Forgejo repos, submits direct VM executions through `sandbox-rental-service`, and verifies ClickHouse evidence.
- Design docs, code comments, architecture diagrams, and API documentation target expert engineers in the relevant technologies. Avoid throat-clearing around current status, "why this is important," date headers, or "who this is for" — get straight into the information that they need.
- Risky commands like `git restore`, `git checkout -- <file>`, and `rm -rf` are blocked.
</output_contract>

<coding_contract>
- When you run into a footgun, leave a comment around the code (no more than a sentence) explaining the footgun and how the code works around it.
- Prefer Ansible over shell scripts when configuring infrastructure. All logic to execute deployments or regular tasks on the provisioned node should be done thorugh Ansible, not through golang binaries.
- Ansible playbook files must have a newline at the end (caught by `ansible-lint`).
- Treat errors as data. Use tagged and structured errors to aid control flow.
- Avoid fallbacks and defaults in Ansible code. Ansible should fail fast with useful logging.
- 1 e2e test of the website is worth 1000 unit tests. Avoid checking in unit tests; they provide some benefit in a tiny set of niche cases, but a comprehensive suite of e2e tests is preferred.
- Don't resolve failures through silent no-ops and imperative checks. Failures should be loud; signals should be followed to address root causes. Failures are useful data!
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


Planned Upcoming Projects

* Newsletter Service
* Analytics Service (PostHog clone) -- we build this ourselves using ClickHouse
* Readyset for Postgres query-result cache.
* Invoices + Preview Invoice for Current Billing Period
