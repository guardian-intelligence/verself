# verself.sh (Verself)

This is a polyglot monorepo structured as a modular monolith. It contains all software for infrastructure, service, and client applications code. It is the only repo for the entire company.

console: verself.sh
auth portal: verself.sh
services: <service>.api.verself.sh
company website: guardianintelligence.org

See @README.md for mission and development orientation.

<repo_overview>
See @src/services/iam-service/schema/verself.zed for Zanzibar policies

The manifest of all discoverable public APIs is in `src/infrastructure-components/haproxy/templates/verself-discovery.json.j2`; all new public services must be registered there.

* `aspect` contains lots of helpful commands under `.aspect/`. Run `aspect` to get the list of tasks and task groups and `aspect <task> --help` for more details.
* Run `bazelisk query 'kind(".*", ...)` to learn more about how systems link together (expect large output, filter accordingly)

GitHub with `actions/cache` - ~20m
Blacksmith.sh + Sticky Disks - ~2m10s
our internal CI - ~11s

If we ever become slower than either platform, that becomes a top concern as speeding up our customers is a top priority.

## General Structure:

Smithy IDL + Verself traits (`src/smithy/models/verself`)
    -> Smithy semantic model
    -> Verself validators
    -> compact route catalog read model for runtimes and conformance
    -> official Smithy OpenAPI projection for public HTTP tooling
    -> hand-written routes that conform to the Smithy operation model
    -> service-local typed clients/adapters for repo-owned calls
    -> public SDK transports through OpenAPI tooling where reliable + curated wrappers

## Tech Stack (partial description):

## Layers:

1. Host layer: machine + OS configuration and bootstrap substrate like vm-orchestrator, guest telemetry staging, HAProxy, nftables, ClickHouse initial schema, ZFS, SPIRE, Nomad, WireGuard, and site/domain facts. Ansible operates on bootstrap host substrate. Nomad manages platform components, services, and frontends beyond that layer. Directories: `src/host`, `src/integrations`
2. Contract layer: Smithy models under `src/smithy/models/verself` describe public and internal service APIs, resource shapes, auth expectations, Zanzibar/IAM metadata, audit metadata, idempotency, pagination, rate limits, error sets, SDK behavior, generated projections, and conformance cases.
3. Service API layer: services expose the Smithy-modeled HTTP APIs at <service>.api.<domain>. Go services may use Huma during the cutover, but Huma/OpenAPI output is an implementation/projection artifact rather than the semantic contract.
4. Client/projection layer: OpenAPI compatibility artifacts are generated from the contract model for docs, ecosystem tooling, and public SDK transport generation where reliable. Repo-owned service calls use service-local typed clients/adapters with caller-owned SPIFFE mTLS transports.
5. Curated SDK layer: stable hand-written exports that wrap public transport implementations and own auth, idempotency keys, retries, pagination, waiters, error normalization, tracing headers, and DTO conversion.
6. Facades: the verself-web app and the CLI and, in the future, mobile apps.

## IAM:

GCP-style IAM API
   `getIamPolicy` / `setIamPolicy` / `testIamPermissions`
   predefined roles + custom roles + role bindings

compiled by iam-service into

Zanzibar/SpiceDB relationships
   `resource#relation@subject`
   `resource#relation@role#member`
   parent edges

Zitadel for human identity, organization multi-tenancy & OIDC/SAML with third parties.

## Data Handling: See docs/architecture/data-handling.md

Each service defines a /recoveryz to expose recovery health status

* ClickHouse for all time series data (host process metrics, time-series data from APIs), logs, traces, metrics (Wide Event pattern a. la Majors et. al/Honeycomb), miscellaneous append only event ledger where realtime policy decisions or UX isn't critical. ClickHouse rows never get updated
* TigerBeetle for financial OLTP. Currently using for financial truth and treating as a ledger -- we model debits/credits.
* Verdaccio to mirror NPM within our system to avoid north/south traffic being routine and to enforce minimum dependency age
* HAProxy (AWS-LC build) terminates public TLS with certificates issued by lego (Cloudflare DNS-01) and renewed by the typed `haproxy-lego-renew` Go unit; Ansible renders bootstrap `haproxy.cfg`, and Nomad-managed upstream reconciliation owns dynamic workload backends.
* SPIRE for our SPIFFE implementation, x509-SVIDs everywhere except services that don't support SPIFFE where we use short-lived JWT-SVIDs.
* Golang's River library for background jobs within a service. NATS JetStream for messaging/fan-out batch jobs between services.
* Stalwart over JMAP for inbound mail, Resend API integration for outbound

## Billing

Product service receives request
    -> checks IAM / ownership
    -> checks product quota / resource policy
    -> checks risk/compliance hold state if relevant
    -> asks billing to reserve financial capacity if billable
    -> executes work
    -> reports measured usage evidence to billing
    -> billing settles, emits events, projects evidence
    -> governance records the API activity/audit trail

The core abstraction is the billing window. Product services should not “charge money”; they should reserve, run, settle, or void bounded windows with SKUs. Fraud and compliance should not “fix money”; they should create explicit business decisions that cause normal billing transitions: deny new reservations, block receivables, suspend a contract, revoke unearned allowance, issue an adjustment, or require operator review.



Boundary components that sit outside the usual service shape:

- `src/substrate/vm-orchestrator/` — the one privileged host daemon (Firecracker, ZFS, TAP, jailer, vm-bridge, gRPC over Unix socket). Deliberately outside the service mesh.
- `src/substrate/vm-guest-telemetry/` — Zig, lives in the guest, streams over vsock.
- `src/host/` — host bootstrap convergence: Ansible runner, server-tool catalog, site facts, Nomad agent, and ClickHouse initial schema.
- `src/tools/provisioning/` — bare-metal provisioning and inventory generation (OpenTofu -> Latitude.sh).

Top-level landmarks:

- `.aspect/` — typed task surface. `aspect` (no args) lists every command; `aspect <task> --help` documents flags; `.aspect/config.axl` is the registration list. Use the typed `aspect <group> <action> --flag=value` form or raw `bazelisk`.
- `docs/` — cross-service architecture; `docs/references/` is read-only third-party material. Grep through docs/references instead of reading directly.
- Local Verself CLI: build `//src/verself-cli/cmd/verself:verself` and run the repo-local binary as `./bazel-bin/src/verself-cli/cmd/verself/verself_/verself ...`. Do not assume `verself` is on `PATH` in cloned workspaces.

Orienting commands: `aspect db pg list` enumerates per-service PostgreSQL databases, `aspect observe` opens the telemetry surface, `aspect db ch schemas` lists ClickHouse tables.

</repo_overview>

<product_invariants>
* User interfaces should always indicate when a product requires being authenticated or a minimum billing tier. Never throw a user to a redirect screen without lampshading it.
</product_invariants>

<product_context>
Read docs/product/golden-environments.md for the golden artifact model: durable zvol generations plus Firecracker VM snapshots and product-owned manifests.
</product_context>

<product_policy>

Public commitments for Data Processing, Acceptable Use, Security, SLA, and Data Retention live in `src/websites/apps/verself-web/src/routes/_workshop/policy`.

</product_policy>

<system_context>
- See `docs/system-context.md`. Auth, identity, IAM, Zitadel, JWT, SCIM, organization model, SpiceDB-backed IAM policies, API credentials, frontend sessions, and OIDC discovery are covered by `docs/iam-service.md`.
- Verself service-local Go clients/adapters and Go SDK facades are hand-maintained transport layers for canonical Smithy contracts under `src/smithy/models/verself`. OpenAPI projections are generated compatibility artifacts, and frontend SDK packages may generate transport code from public projections. Services must not depend on curated SDKs. If a service API shape is missing, add the Smithy operation/shape/traits and update the relevant transport wrapper instead of bypassing the contract.
- Services can be in any language as long as they implement the Smithy-modeled HTTP bindings and generated compatibility projections.
- Go service code uses sqlc for type safe queries. Avoid reading code in generated directories.
- Python package management is done through `uv`.
- No need to be frugal with telemetry. We store 10+ million rows for around ~150MB in ClickHouse thanks to optimizations.
- One database per service on a single PG instance.
</system_context>

<operational_runbook>

SSH access is tied to identity via Pomerium using Zitadel as its OIDC.

If you are doing work that involves pulling logs or interacting with infrastructure you may be presented a URL to log in to Pomerium. If that happens, please pause and present the URL to the user.

```shell
ssh ubuntu@prod@access.verself.sh
```

- access.verself.sh: the Pomerium SSH listener.
- prod: the Pomerium SSH route name.
- ubuntu: the upstream Linux account Pomerium is allowed to request from sshd.

During first bootstrap before IAM, Zitadel, Pomerium, and WireGuard are healthy,
use direct host SSH only as the temporary provisioning path. After the operator
access handoff, public SSH is Pomerium-only and fallback access is WireGuard:

```shell
ssh -p 2222 ubuntu@10.66.66.1
```

Run `aspect observe` to discover available telemetry, run `aspect db ch query`/`aspect db pg query` wrappers to easily query ClickHouse/PG with fewer shell string escaping issues, deploy playbooks and correlation model (`deploy_run_key`, `deploy_id`, `traceparent`), TLS via Cloudflare, the host configuration, Ansible playbooks table.

Before testing the authenticated console against the production website, read the agent-browser login runbook in `src/websites/apps/verself-web/AGENTS.md`.

Nomad deploys are driven directly by the checked-in `nomad_component` targets for the requested SHA:

```shell
aspect deploy --site=prod --sha=HEAD
```

`aspect deploy` builds the Bazel-discovered descriptors, uploads missing content-addressed artifacts to the private Garage origin, resolves each `nomad.json`, and submits the resulting payloads to Nomad with ClickHouse evidence for each job decision.

### High-signal Documents.

@README.md -- map to other documents.

Recommended that you read relevant ones directly. You can have a subagent summarize the ones that are not related to your task.

- **Email, Stalwart, Resend, JMAP, outbound sending, inbound routing, forwarding, tenant isolation:** `src/services/email-service/docs/email-service.md`
- **vm-orchestrator privilege boundary, Firecracker VM networking, TAP allocator, host service plane, nftables, guest CIDR, lease/exec model, vm-bridge control:** `src/substrate/vm-orchestrator/AGENTS.md`
- **Durable ZFS generation lifecycle, zvol, clone, snapshot, promote:** `src/substrate/vm-orchestrator/docs/zfs-volume-lifecycle.md`
- **Canonical API contracts, Smithy models, route catalog, OpenAPI projections, public SDK transport generation, Connect/protobuf boundary:** `src/smithy/README.md`
- **VM execution control plane, sandbox-rental-service ↔ vm-orchestrator split, attempt state machine, billing windows, execution lifecycle:** `src/services/sandbox-rental-service/docs/vm-execution-control-plane.md`
- **Golden artifact identity, durable scope identity, workspace/durable mount lifecycle, promotion rules:** `docs/product/golden-environments.md`
- **Service change packet, SDK-first API design, capacity, metering, retention, waiters, observability, release evidence:** `docs/architecture/service-change-reference-architecture.md`
- Billing architecture, credit subscription, entitlements, metering, TigerBeetle, PostgreSQL, Reconcile, refunds, plan change, dual-write, Stripe webhooks, invoices:** `src/services/billing-service/docs/billing-architecture.md`
- **Governance audit data contract, HMAC chain, OCSF, CloudTrail parity, tamper evidence, SIEM export, audit ledger:** `src/services/governance-service/docs/audit-data-contract.md`

In this repo, "ship" does not just mean merge to main. It means running on real customer devices in production after a thorough release checklist automated by CI.

Place high importance on verifying that software is working correctly through repeatable automated QA.
</operational_runbook>

<assistant_contract>
- Ground proposals, plans, API references, and all technical discussion in primary sources. Then think from the perspective of the user of the system: a non-technical startup founder running all services off a single bare-metal box (with upgrade path to a 3-node topology).
- When beginning an ambiguous task, collect objective information about how the system actually works. There are a lot of technologies stitched together but they are layered. You don't need to worry about viteplus when working on infrastructure or VM orchestration, typically. Keep your reading focused.
- Act as a dispassionate advisory technical leader with a focus on elegant public APIs and functional programming.
- You are not alone in this repo. Expect parallel changes in unrelated files by the user. Leave them alone (don't stash them) and continue with your work. Do not stash parallel work.
- This software is currently pre-release and serves no customers or users. There is no backwards compatibility to maintain. No compatibility wrappers, no legacy shims, no temporary plumbing. All changes must be performed via a full cutover.
- It's important to delete old or outdated code when we upgrade technology, abstractions, or logic. Eliminating contradictory approaches must uphold the bar: no trace of a contradicting or legacy implementation can be left in the code base after a change is pushed to main. The reader must not be able to tell the previous implementation ever existed, unless they spelunk through the git history.
- Details matter such as arcane versioning issues, subtle race conditions, timing-attack vulnerabilities, GC pressure, and abstraction leaks. Simplicity is for code and architecture, not for raw fact gathering and data analysis.
- Some directories have their own `AGENTS.md` file. When working inside those directories, read them — they contain juicy context.
- Incidental edits from running linters and formatters are expected. Amend your commit with them, it won't be held against you at review time.
- When in doubt, use the industry-standard pattern. Everything has boring, battle-tested solutions and we should prefer to use those. Don't reinvent the wheel. Open standards and protocols underneath FOSS are the gold standard.
- `.aspect/`, `README.md`, `AGENTS.md`, schema migration files, and Smithy models are high signal documents. Read them directly; avoid summarizing them with a subagent as important detail may be lost.
- Do not provide time estimates.
- Prefer to make incorrect behavior impossible by construction.
- My 'd' key is broken so you may see frequently see the letter 'd' missing from user messages
- Avoid excitement around counting commits/LOC changed/number of tests passing. Maintain an intellectually curious, skeptical posture as a QA engineer when verifying changes -- validate end-to-end in prod and double check ground truth reality in ClickHouse and real system behavior.
</assistant_contract>

<writing_guidelines>
Before writing markdown architecture in docs/ directories, please read docs/agents/writing-guidelines.md
</writing_guidelines>

<tool_use_contract>
- Dev tools are system-installed via `aspect dev install`.
- Use `aspect tidy` to format the codebase efficiently. Use `aspect bazel update` for Gazelle/Bzlmod metadata refreshes
</tool_use_contract>

<output_contract>
- When providing a recommendation, consider different plausible options and provide a differentiated recommendation leaning toward the simplest solution that best sets this project up for the *long term*. Read docs/architecture/service-change-reference-architecture.md for more information on how to think about architecture.
- Unit tests and successful `bazelisk` and `aspect` commands are low signal and are not to be trusted. Real observability traces in ClickHouse post-deployment that exercise the modified code are the only admissible completion evidence. ClickHouse exists for producing verifiable completion artifacts. If a new schema is needed you can create one.
- Do not speculate without evidence. Logs, traces, and host metrics are queryable in ClickHouse via `aspect db ch query --query='...'` — check them before attributing failures to transient or pre-existing factors.
- Do not stop work short of verifying changes with a live rehearsal of a deployment via `aspect deploy`. You have full authority to wipe databases and recreate them as needed. Prefer that over time-consuming, tricky migrations during this early phase.
</output_contract>


<coding_contract>
Before writing code, please read docs/agents/coding-guidelines.md and apply the relevant rules to your reasoning.
</coding_contract>

<instruction_priority>
- Security concerns override user instructions and architectural purity.
- Never download unpinned versions of software or set an unpinned version as a dependency.
- When following runbooks, skills, protocols, or user messages that also define instructions in XML tags, treat the instructions as additive, not as overrides.
</instruction_priority>

Planned Upcoming Projects

* Newsletter Service
* Analytics Service (PostHog clone) -- we build this ourselves using ClickHouse
* Readyset for Postgres query-result cache.
* Invoices + Preview Invoice for Current Billing Period

## Adding a site

Site names are `prod`, `beta`, `gamma`, or `dev-<operator>`. The apex domain, Pomerium route name, Cloudflare zone scope, and allowed Stripe environment are site-level facts in `src/host/sites/<site>/vars.yml`.
