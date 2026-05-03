<repo_overview>
Set of services + console + marketing page for a software business, almost entirely self-hosted on a single bare metal node.

* `aspect` contains lots of helpful commands under .aspect/.
* Run `bazelisk query 'kind(".*", ...)` to learn more about how systems link together (expect large output)

Polyglot monorepo structured as a modular monolith.

Layers:

1. Host layer: machine + OS configuration and binaries like vm-orchestrator, guest telemetry, HAProxy, nftables, ClickHouse, Postgres, Forgejo, domain registration, SPIRE and so on. Ansible operates only here (target state, not necessarily the case today). Nomad manages everything beyond this layer.
2. Product API layer: service-owned Go Huma APIs at <service>.api.<domain>, with internal SPIFFE-only APIs separate.
3. Generated client layer: pure transport clients, validators, DTOs, schemas.
4. Curated SDK layer: stable hand-written exports that wrap generated clients and own auth, idempotency keys, retries, pagination, waiters, error normalization, tracing headers, and DTO conversion.
5. Facades: the verself-web app on the `<domain>` apex (console + docs + policy), CLI, docs examples, Terraform provider later. These use the SDK, not private service shortcuts.

Tech Stack:

* ClickHouse for all time series data (host process metrics, time-series data from APIs), logs, traces, metrics (Wide Event pattern a. la Majors et. al/Honeycomb), miscellaneous append only event ledger where realtime policy decisions or UX isn't critical. ClickHouse rows never get updated
* TigerBeetle for OTLP. Currently using for financial truth and treating as a ledger -- we model debits/credits for 
* Zitadel for human identity & OIDC/SAML with third parties. We support multi-tenancy in that all users belong to an org (users belonging to multiple orgs not yet supported)
* Verdaccio to mirror NPM within our system to avoid north/south traffic being routine and to enforce minimum dependency age
* HAProxy (AWS-LC build) terminates public TLS with certificates issued by lego (Cloudflare DNS-01) and renewed by the typed `haproxy-lego-renew` Go unit; Ansible renders `haproxy.cfg` and the per-service nftables drop-ins from Jinja2 templates
* SPIRE for our SPIFFE implementation, x509-SVIDs everywhere except services that don't support SPIFFE where we use short-lived JWT-SVIDs.
* Golang's River library for background jobs within a service. NATS JetStream for messaging/fan-out batch jobs between services.

Invariant patterns:

* Do not add shell scripts. The only shell scripts allowed are the platform bootstrap entrypoints under `scripts/bootstrap-*`. Scripts are load-bearing tooling and infrastructure. We control the execution environment and the installed binaries catalog both in the development environment and on the fleet. Choose the right tool for the job (it's never a shell script).
* Efficient rebuilding: Bazel's job is to cache and decide when to run a unit's build pipeline. Nomad orchestrates deployments for non-host-configuration concerns. Ansible's job is to configure the host and ensure convergence. We rebuild only what we need by teaching Bazel about inputs and outputs. This also means deploys don't need the user to know what to deploy. They just merge to main or run `aspect deploy` and Bazel (sometimes Ansible) and Nomad take over. Let each bazel boundary decide how to build itself. We finetune our build process per unit.
* Ansible mutates the host for bootstrapping the machine and installing initial binaries.
* New server tools such as SpiceDB enter through the host-configuration server-tools catalog and artifact admission flow, with policy/evidence recorded before Ansible installs them; see `docs/architecture/artifact-admission.md`.
* Deployments and ref-based GitOps is done through Nomad, executed via `aspect`.
* Service-oriented-architecture: with notable exceptions, all of our services talk to each other through the same APIs as the ones customers use. Despite having a notion of internal and external clients, the only difference is the auth method (SPIFFE mTLS for internal clients, Zitadel-based auth for public)
* Generated clients are the only supported Go service SDKs. Customer/human routes use the committed public OpenAPI specs and `client` packages; repo-owned SPIFFE routes use committed internal OpenAPI specs and `internalclient` packages. The caller injects a SPIFFE mTLS `http.Client` from `auth-middleware/workload`; do not hand-write `http.NewRequest` service calls or mint Zitadel machine-user bearer tokens for repo-owned service-to-service traffic.
* Non-retrievable product token material belongs in `secrets-service` as an opaque credential. Product services may keep metadata/projection rows, but token generation, verifier storage, roll/revoke semantics, and verification must go through generated secrets-service clients over SPIFFE.
* Dogfood as much as possible, even if it involves hairpinning requests through the internet. We are a customer on our platform. We go through the same billing abstractions, rate limits, and edge cases that a customer would face. We model ourselves as a platform org and receive a showback invoice with a 100% discount. 
* Sync-engine pattern: PostgreSQL owns state, ClickHouse records the append-only ledger/traces, Electric/TanStack expose live read projections, and writes go through typed service commands whose conflict behavior matches the domain (strict observed-state rejection for security-critical resources, monotonic/idempotent collapse for notification-style cursors and dismissals).

Boundary components that sit outside the usual service shape:

- `src/vm-orchestrator/` — the one privileged host daemon (Firecracker, ZFS, TAP, jailer, vm-bridge, gRPC over Unix socket). Deliberately outside the service mesh.
- `src/vm-guest-telemetry/` — Zig, lives in the guest, streams over vsock.
- `src/host-configuration/` — host and daemon convergence: Ansible runner, host scripts, controller OTLP agent, and ClickHouse schema.
- `src/provisioning-tools/` — bare-metal provisioning and inventory generation (OpenTofu -> Latitude.sh).

Top-level landmarks:

- `.aspect/` — typed task surface. `aspect` (no args) lists every command; `aspect <task> --help` documents flags; `.aspect/config.axl` is the registration list. Use the typed `aspect <group> <action> --flag=value` form or raw `bazelisk`.
- `docs/` — cross-service architecture; `docs/references/` is read-only third-party material. Grep through docs/references instead of reading directly.

Orienting commands: `aspect db pg list` enumerates per-service PostgreSQL databases, `aspect observe` opens the telemetry surface, `aspect db ch schemas` lists ClickHouse tables.

</repo_overview>

<product_policy>

Public commitments for Data Processing, Acceptable Use, Security, SLA, and Data Retention live in `src/viteplus-monorepo/apps/verself-web/src/routes/_workshop/policy`.

</product_policy>

<product_direction>

Where the platform is headed: open-source-per-subdirectory, privileged-host / product-service split, multi-tenant + customer dogfooding, three customer-facing sandbox products (CI runner, Lambda-like workload, long-running VM), self-hosted Forgejo/CI; agents merge to `main` continuously and environments deploy whichever SHA the `staging-tip`/`prod-tip` refs point at, advancing only after a canary soak passes — no long-lived release branches, unfinished work hidden behind feature flags.

See `docs/product-direction.md`.

</product_direction>

<system_context>

Service topology, three safety rings, self-hosted mandate + allowed third-party providers (Cloudflare, Latitude.sh, Resend, Stripe), dual-write pattern, billing model summary, supply chain, founder focus areas, bare-metal OS/arch invariants.

- See `docs/system-context.md`. Auth, identity, IAM, Zitadel, JWT, SCIM, organization model, three-role (owner/admin/member), API credentials, frontend sessions, OIDC discovery — all in `src/platform/docs/identity-and-iam.md`.
- Verself Go service clients are generated from committed OpenAPI 3.0 specs with `oapi-codegen`; consumers must use those generated `client` or `internalclient` packages, with SPIFFE carried by the underlying `http.Client` instead of handwritten transport code. If a service API shape is missing, add the Huma route/OpenAPI spec and regenerate instead of bypassing the SDK. 
- Services can be in any language as long as they expose OpenAPI-compatible endpoints.
- Go service code uses sqlc for type safe queries. Avoid reading code in generated directories.
- Python package management is done through `uv`.
- No need to be frugal with telemetry. We store 10+ million rows for around ~150MB in ClickHouse thanks to optimizations.
- One database per service on a single PG instance
</system_context>

<operational_runbook>

SSH access is tied to identity via Pomerium using Zitadel as its OIDC

```shell
ssh ubuntu@prod@access.verself.sh
```

- access.verself.sh: the Pomerium SSH listener.
- prod: the Pomerium SSH route name.
- ubuntu: the upstream Linux account Pomerium is allowed to request from sshd.

Run `aspect observe` to discover available telemetry, run `aspect db ch query`/`aspect db pg query` wrappers to easily query ClickHouse/PG with fewer shell string escaping issues, deploy playbooks and correlation model (`deploy_run_key`, `deploy_id`, `traceparent`), TLS via Cloudflare, the host configuration, Ansible playbooks table.

### High-signal Documents.

@README.md -- mp

Recommended that you read relevant ones directly. You can have a subagent summarize the ones that are not related to your task.

- **Inbound mail, Stalwart, mailbox-service, JMAP, SMTP, inbound routing, tenant isolation:** `src/mailbox-service/docs/inbound-mail.md`
- **vm-orchestrator privilege boundary, Firecracker VM networking, TAP allocator, host service plane, nftables, guest CIDR, lease/exec model, vm-bridge control:** `src/vm-orchestrator/AGENTS.md`
- **ZFS volume lifecycle, zvol, clone, snapshot, checkpoint, restore:** `src/vm-orchestrator/docs/zfs-volume-lifecycle.md`
- **Wire contracts, DTO patterns, protobuf schemas, numeric safety, 64-bit, DecimalUint64, DecimalInt64, generated contract gate:** `src/domain-transfer-objects/docs/wire-contracts.md`
- **VM execution control plane, sandbox-rental-service ↔ vm-orchestrator split, attempt state machine, billing windows, execution lifecycle:** `src/sandbox-rental-service/docs/vm-execution-control-plane.md`
- **Identity and IAM, Zitadel, SCIM 2.0, SSO, authentication, organization model, three-role owner/admin/member, capability catalog, API credentials, Zitadel Actions, pre-access-token, frontend sessions, OIDC discovery, Verself policy split:** `src/platform/docs/identity-and-iam.md`
- **Workload identity, SPIFFE/SPIRE trust domain, service mTLS, OpenBao relying-party model, runtime secret cleanup:** `docs/architecture/workload-identity.md`
- **Secrets service, identity model, OIDC provider role, resource model, billing, KMS alternative:** `src/platform/docs/secrets-service.md`
- Billing architecture, credit subscription, entitlements, metering, TigerBeetle, PostgreSQL, Reconcile, refunds, plan change, dual-write, Stripe webhooks, invoices:** `src/billing-service/docs/billing-architecture.md`
- **Governance audit data contract, HMAC chain, OCSF, CloudTrail parity, tamper evidence, SIEM export, audit ledger:** `src/governance-service/docs/audit-data-contract.md`
- **Service topology, port assignments, SPIRE identities, runtime users, Ansible inputs:** `src/host-configuration/ansible/group_vars/all/topology/` plus service-owned Nomad metadata.
- **Directory structure, repo layout:** `docs/architecture/directory-structure.md`
- **Agent workspace, QEMU/KVM, AI coding agent VMs:** `docs/architecture/agent-workspace.md`

</operational_runbook>

<assistant_contract>
- Ground proposals, plans, API references, and all technical discussion in primary sources. Then think from the perspective of the user of the system: a non-technical startup founder running all services off a single bare-metal box (with upgrade path to a 3-node topology).
- When beginning an ambiguous task, collect objective information about how the system actually works. There are a lot of technologies stitched together; understand how everything connects.
- Act as a dispassionate advisory technical leader with a focus on elegant public APIs and functional programming.
- You are not alone in this repo. Expect parallel changes in unrelated files by the user. Leave them alone (don't stash them) and continue with your work. Do not stash parallel work.
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
- Avoid excitement around counting commits/LOC changed/number of tests passing. Maintain an intellectually curious, skeptical posture.
</assistant_contract>

<writing_guidelines>
The contained instructions in this block are guidelines that apply to writing markdown architecture documents in docs/ directories.

- Avoid framing rhetoric, e.g. "X is Y, not Z". Just write "X is Y".
- Avoid attention-grabby language like "The same X and Y that do A also does B" or "both X and Y". Or short punchy sentences like "One binary. Five nodes. Infinite possibilities." Prefer to be straightforward: "X and Y do A and B via a single binary across five nodes and are designed to be extensible."
- Avoid 
- Preferred writing style: advanced industry-level textbook sans historical context.
- Write for an audience of expert engineers in the relevant technologies. Avoid throat-clearing around current status, "why this is important," date headers, or "who this is for" — get straight into the information that they need.
</writing_guidelines>

<tool_use_contract>
- Dev tools are system-installed via `aspect dev install`.
- Apply the scientific method: create a bar-raising verification protocol for the planned task *prior* to implementing changes. The verification protocol should fail, and only then begin implementing until green.
- Avoid one-off, non-syntax-aware scripts for large parallel changes or refactors. Use subagents for that class of task — unexpected edge cases are likely and judgement is often required.
- Use `aspect bazel tidy` to run `go mod tidy` and other language-specific formatters across the code base.
- When using agent-browser, don't use the sandbox (`--no-sandbox`)
- Deploy frontend changes to prod fearlessly (e.g. `aspect deploy site=prod`) -- I can't see your dev server.
</tool_use_contract>

<output_contract>
- When providing a recommendation, consider different plausible options and provide a differentiated recommendation leaning toward the simplest solution that best sets this project up for the *long term* in terms of functionality, elegance of architecture, security, performance, and best-practices.
- Unit tests and successful builds are low signal and are not to be trusted. Real observability traces in ClickHouse that exercise the modified code are the only admissible completion evidence. ClickHouse exists for producing verifiable completion artifacts. If a new schema is needed, create one.
- Do not speculate without evidence. Logs, traces, and host metrics are queryable in ClickHouse via `aspect db ch query --query='...'` — check them before attributing failures to transient or pre-existing factors.
- Do not stop work short of verifying changes with a live rehearsal of a deployment via `aspect deploy`. You have full authority to wipe databases and recreate them as needed. Prefer that over time-consuming, tricky migrations during this early phase.
- Avoid emojis.
</output_contract>

<coding_contract>
- When you run into a footgun, leave a comment around the code (no more than a sentence) explaining the footgun and how the code works around it.
- Treat errors as data. Use tagged and structured errors to aid control flow.
- Avoid fallbacks and defaults. Runtime behavior should fail fast with useful logging.
- Avoid verbosity. When solving a specific problem, the patch should solve the general case. E.g. if solving a TOCTOU vuln, don't write a function named `fix_toctou_bug`, make the simple patch to use the toctou-safe call and optionally leave a comment (no more than a few words).
- Do not check in Ansible "clean up" tasks. Just clean up the host directly and remove Ansible "clean up legacy X" and "assert old Y isn't there" steps.
- 1 e2e test of the website is worth 1000 unit tests. Avoid checking in unit tests; they provide some benefit in a tiny set of niche cases, but a comprehensive suite of e2e tests is preferred. <note>We are moving to ongoing e2e canaries instead of our verify/smoke test scripts. Keep using the scripts in the meantime.</note>
- Don't resolve failures through silent no-ops and imperative checks. Failures should be loud; signals should be followed to address root causes. Failures are useful data!
- ClickHouse inserts must use `batch.AppendStruct` with `ch:"column_name"` struct tags. `batch.Append` silently corrupts data when columns are added or reordered.
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
