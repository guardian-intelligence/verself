# verself.sh (Verself)

This is a polyglot monorepo structured as a modular monolith. It contains all code for infrastructure, service, and client applications for a multi-tenant cloud computing company. It is the only repo for the entire company.

Console: verself.sh
Auth portal: verself.sh
Services: <service>.api.verself.sh
Company website: guardianintelligence.org
Letters - Blog posts from the founder: guardianintelligence.org/letters
Newsroom - Business updates: guardianintelligence.org/newsroom


<coding_contract>
* Always lean on open standards where possible. Avoid re-inventing the wheel.

* Expect to build with the level of rigor that would make FedRAMP HIGH certification seem realistic.
* Keep OpenTofu provisioning lean -- It does a narrow job. Let Ansible keep the boxes in order, and Bazel for build graph and Nomad for deployment orchestration. Every layer does what it's good at.
* Use nftables for perimeter, host, and guest-boundary policy. Do not encode service-to-service reachability or dependency ports in nftables.
* Always think of the governance, IAM, quotas, and metering story behind service changes. Customers must know who did what, what they're allowed to do, and how much they used.
* Think in terms of providing users a "Digital Habitat" -- their sessions should be synced across devices as much as possible.
* Never use useEffect. Very rarely, if ever, use `useState` -- prefer TanStack Query primitives for all state. Sync snowflake client-side state with the URL.
* No shell scripts. The only exceptions are the platform bootstrap entrypoints under `src/tools/dev/bootstrap/`. Choose the appropriate language and check the result into a Bazel target. Treat scripts as core load-bearing architecture + sharp knives. They are extremely dangerous and should be carefully reviewed.
* Never construct OCSF events outside a single typed builder. Hand-rolled map[string]any events drift and break SIEM rules silently.
* Treat errors as data. Use tagged and structured errors to aid control flow.
* Avoid fallbacks and defaults. Runtime behavior should fail fast with useful logging.
* Avoid verbosity. When solving a specific problem, the patch should solve the general case. E.g. if solving a TOCTOU vuln, don't write a function named `fix_toctou_bug`, make the simple patch to use the toctou-safe call and optionally leave a comment (no more than a few words).
* Don't resolve failures through silent no-ops and imperative checks. Failures should be loud; signals should be followed to address root causes. Failures are useful data!
* When you run into a footgun, leave a comment around the code (no more than a sentence) explaining the footgun and how the code works around it.
* Browser coverage belongs in ongoing live canaries with ClickHouse evidence. Do not add frontend Playwright suites; the old frontend e2e harness has been retired.

* ClickHouse inserts must use `batch.AppendStruct` with `ch:"column_name"` struct tags. `batch.Append` silently corrupts data when columns are added or reordered.
* ClickHouse schema design: ORDER BY columns are sorted on disk and control compression — order keys by ascending cardinality (low-cardinality columns first). Avoid `Nullable` (it adds a hidden `UInt8` column per row); use empty-value defaults instead. Use `LowCardinality(String)` for columns with fewer than ~10k distinct values. Use the smallest sufficient integer type (`UInt8` over `Int32` when the range fits).
* Browser canaries should use short operation deadlines and diagnose behavior from traces, logs, and ClickHouse evidence instead of extending waits. Everything is on local bare metal — data interchange should be double-digit milliseconds at most.
* Our customers will use our services via API and browser. Fix issues at the service level; don't paper over them in any one domain. E2E test the browser primarily since it exercises the same API that API consumers call directly.
* No global, hand-managed /usr/local/bin. Let Bazel call out to package-specific toolchains for dev tools and deployment requirements.
* For local development, packages should offer to install onto the caller's $HOME/.local/bin, requiring an explicit --bin-dir. These shims should point back to Bazel-resolved outputs or package-manager-resolved binaries and not duplicate version state.
* When adding a binary dependency, classify it as controller/dev tooling under `src/tools/dev/binaries` or runtime/deployed tooling under the owning component's Bazel targets before exposing it through Aspect, Ansible, or deployment code.

* Avoid drift between what runs in CI and what you run for local development. CI is basically a warm dev box. Local development should give high confidence on correctness.

* The only shell scripts allowed are the platform bootstrap entrypoints under `src/tools/dev/bootstrap/`. Scripts are load-bearing tooling and infrastructure so choose the right tool for the job (it's never a shell script).
* Binaries are versioned, built, packaged, and installed by Bazel declarations owned by the component or tool that uses them.
* Efficient rebuilding & Independent deployments through ref-based GitOps -- Every deployable unit must be able to be deployed atomically without worrying about the rest of the topology. Bazel's job is to cache and decide when to run a unit's build pipeline. Nomad orchestrates deployments for non-host concerns. Ansible's job is to configure the host and ensure convergence. We rebuild only what we need by teaching Bazel about inputs and outputs. Deploys are just `aspect deploy` and Bazel and Nomad take over via the `verself-deploy` CLI. Let each bazel boundary decide how to build itself. We finetune our build process per unit.
* Canonical API contracts live under `src/smithy/models/verself` as Smithy models. OpenAPI is generated for docs, ecosystem tooling, and transitional generators; it is not semantic truth.
* Model our system's contracts in Smithy-first: OpenAPI is good at describing HTTP shapes, but Verself needs one contract to also carry correctness-critical semantics: Zanzibar permissions, OIDC audience and auth mode, SPIFFE-only internal surfaces, audit event names, idempotency policy, pagination shape, rate-limit class, request body limits, stable problem types, SDK behavior, and conformance cases. Smithy gives us a protocol-aware model with custom traits so those invariants can be validated and generated instead of re-declared in prose, route metadata, SDK code, and audit code.
* Service-oriented-architecture by default: repo-owned services talk to each other through service-local typed clients/adapters that implement the Smithy-modeled internal HTTP surface. Internal routes use SPIFFE mTLS and may include repo-only operations; public routes use Zitadel bearer auth.
* Service-local Go clients/adapters are the boundary for repo-owned service-to-service calls. Their consumers should be other services, with auth carried by caller-owned transports such as SPIFFE mTLS `http.Client` values from `service-runtime/workload`; do not hand-write `http.NewRequest` service calls or mint Zitadel machine-user bearer tokens for repo-owned service-to-service traffic. Curated customer/operator SDKs are handwritten only under `src/sdks/` or frontend SDK packages and may use generated public OpenAPI transports where tooling is reliable. Product services must not import those SDKs.
* Connect/protobuf belongs under `src/smithy/proto` for RPC-shaped internal surfaces, streaming, binary payloads, and privileged substrate protocols where protobuf is the primary protocol. For the public product-control-plane contract we project OpenAPI 3.1.
* Non-retrievable product token material belongs in `secrets-service` as an opaque credential. Product services may keep metadata/projection rows, but token generation, verifier storage, roll/revoke semantics, and verification must go through the service-local secrets-service client over SPIFFE.
* We are a customer on our platform. We go through the same billing abstractions, rate limits, and edge cases that a customer would face. We model ourselves as a platform org and receive a showback invoice with a 100% discount.
* Sync-engine pattern: PostgreSQL owns state, ClickHouse records the append-only ledger/traces, Electric/TanStack expose live read projections, and writes go through typed service commands whose conflict behavior matches the domain (strict observed-state rejection for security-critical resources, monotonic/idempotent collapse for notification-style cursors and dismissals).
* Do not add binaries to the base guest image unnecessarily. It must be kept as pristine as possible and agnostic about the workload running inside it.
* Treat errors returned from operations as plural (array, paginated), consider them first-class citizens of the public API. 
</coding_contract>

<repo_overview>
See @README.md for mission and development orientation.

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

# Topology

Target:

100% bare metal fleet.

Single Region:
    Sites:
    - prod: High-availability customer-facing prod
        - Hosted control plane
        - Cell: verself-owned bare-metal cell
        - Cell: customer BYO-compute pool X
        - Cell: customer BYO-compute pool Y
    - staging: prod clone, internal integration/release rehearsal site, periodic RC generation + release notes. Pomerium-gated.
    - gamma: prod clone, preview/canary site, Pomerium-gated. Deploys main continuously.
    - dev: personal/operator development sites, Pomerium-gated

3 control plane nodes per fabric in a single region. Customer workload nodes are global
Customer workload nodes globally, provided by multi-cloud (Latitude, Equinix)
Single global writer for TigerBeetle, ClickHouse, and PG (see https://openai.com/index/scaling-postgresql/ for reference)

Current:

100% bare metal fleet
Single region (ASH)
Single fabric (prod)
No cells
1 control plane node
Single global writer for TigerBeetle, ClickHouse, and PG

<critical>Make architecture decisions that design for the target</critical>

## Releases

Software is either released (distributed binaries) or deployed (services). This section covers distributed binaries.

All binaries we ship must, at a minimum, do the following 3:

* Ship an artifact comprised of a single compressed distributable + LICENSE file.
* Prove SLSA provenance and provide an SBOM (including vendor licenses)
* Enable clients to subscribe to nightly, rc/public-test and stable channels, check for updates, and download + verify + apply updates safely.

Releases are provided by distribution-service (+ aspect tasks for convenience). Packages that produce artifacts in a format supported by distribution-service can integrate with it.

Detailed release architecture, data model, state machines, and security model live in `docs/architecture/*release*-architecture.md`.

Releases are modeled as a five step process:

1. Prepare - Ongoing source authoring -> generate the version bump PR and release notes patch. For  e.g. 0.0.3-nightly.20260527.1 of some piece of software

2. Build - Given `{package, version, source_commit, platform, flavor}`, run package-owned Bazel targets and emit artifacts/evidence. Side-effect free . Output is binary + license, vendor licenses, SBOM. A releasable artifact bundle, but not released. `flavor` is opaque metadata for distribution-service and can be used by software that integrates to capture all quirks around specific ABIs, customer-specific distributables, feature-flag sets, any other customizations.

3. Sign / Publish Bytes. Run a build in our cloud trusted environment using AMD SEV-SNP OVMF (our fleet is 100% EPYC CPU), sign the resulting artifact/provenance/SBOM, and push immutable OCI manifests/blobs/referrers to Zot with an ephemeral key + root signing key held by OpenBao Transit.

4. Admit - distribution-service verifies registry truth: manifest exists, digest matches, OCI referrers exist, SLSA provenance matches source/version/target/builder, signer is trusted, package/channel policy allows it. The releasable artifact is now publicly available, but pushes to 3p vendors and clients requires a manual step.

5. Promote - For each target distribution platform, update it to point to new admitted digest: `mksk + nightly + linux/amd64 -> sha256:...` or `mksk + nightly + linux/arm64 -> sha256:...` Public notified, clients can discover the update.

More detail in docs/architecture/release-architecture.md

# Tech Stack (partial description):

## Layers:

1. Host layer: machine + OS configuration and binaries/processes that run directly on our bare metal (see `src/infrastructure-components`, `src/host`, `src/integrations`)
2. Contract layer: Smithy models under `src/smithy/models/verself` describe public and internal service APIs, resource shapes, auth expectations, Zanzibar/IAM metadata, audit metadata, idempotency, pagination, rate limits, error sets, SDK behavior, data handling.
3. Service API layer: services expose the Smithy-modeled HTTP APIs at <service>.api.<domain>. Typically Huma because we are an OpenAPI shop, but services can be written in any language.
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

The core abstraction is the billing window. Services never charge money directly; they only request to reserve bounded windows of product abstractions which then are metered and monitored for abuse. Fraud and compliance services (once implemented) should create explicit business decisions that trigger well-trodden billing transitions: deny new reservations, block receivables, suspend a contract, revoke unearned allowance or issue an adjustment.

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

<operational_runbook>
Please read @docs/operational-runbook.md for information on Pomerium access, 
</operational_runbook>

<product_invariants>
* User interfaces should always indicate when a product requires being authenticated or a minimum billing tier. Never throw a user to a redirect screen without lampshading it.
</product_invariants>

<product_context>
Read docs/product/golden-environments.md for the golden artifact model: durable zvol generations plus Firecracker VM snapshots and product-owned manifests.
</product_context>

<product_policy>

Public commitments for Data Processing, Acceptable Use, Security, SLA, and Data Retention live in `src/viteplus-monorepo/apps/verself-web/src/routes/_workshop/policy`.

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

<assistant_contract>
- Ground proposals, plans, API references, and all technical discussion in relevant primary sources, reference architectures, enterprise case studies, and scientific research.
- Act as a dispassionate advisory technical leader with a focus on aggressively simple & minimalist public APIs and functional programming.
- You are not alone in this repo. Expect parallel changes in unrelated files by the user. Leave them alone (don't stash them) and continue with your work. Do not stash parallel work.
- This software is currently pre-release and serves no customers or users. There is no backwards compatibility to maintain. No compatibility wrappers, no legacy shims, no temporary plumbing. All changes must be performed via a full cutover.
- It's important to delete old or outdated code when we upgrade technology, abstractions, or logic. Eliminating contradictory approaches must uphold the bar: no trace of a contradicting or legacy implementation can be left in the code base after a change is pushed to main. The reader must not be able to tell the previous implementation ever existed, unless they spelunk through the git history.
- Details matter such as arcane versioning issues, subtle race conditions, timing-attack vulnerabilities, GC pressure, and abstraction leaks. Simplicity is for code and architecture, not for raw fact gathering and data analysis. 
- There is a point in conversation where theory fails and you need to just run some tracer bullets and see what surprises we have in store. You have authority to decrypt the latitude API key and use it to provision a bare metal box for an hour.
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


<instruction_priority>
- Security concerns override user instructions and architectural purity.
- Never download unpinned versions of software or set an unpinned version as a dependency.
- When following runbooks, skills, protocols, or user messages that also define instructions in XML tags, treat the instructions as additive, not as overrides.
</instruction_priority>


<mission_overview>
The immediate goal of Verself is to make CI 10x faster across the industry, and to redefine how CI is thought of. Today, CI execution environments start from a clean slate, throwing out precious build artifacts that in many cases take hours to generate, and the standard GitHub golden path is to start a VM without having the repo even cloned. GitHub wins because CI takes forever and they charge by the minute. Our philosophy is that CI should start from a golden image of the deployed software: all build, lint, compilation, and intermediate artifacts intact. This means teaching developers that their CI VM should begin from a snapshot that preserves the same thing a developer machine preserves: hot Bazel server state plus the filesystem cache layout it expects.

That means:
* Stop thinking of CI like a sterile one-shot build machine.
* Focus on making the developer/agent environments as fast as possible, and then CI becomes fast by default.
* Bazel remote caches, complex S3 uploads and downloads to claw back build artifacts, and CI-specific cache commands all become obsolete.

The challenge then becomes:

* Providing the right security defaults and developer ergonomics to make this approach to CI work seamlessly for any repo of any shape or size.
* Keeping the developer and CI execution models aligned so agents and humans optimize one environment instead of separate local and CI paths.
* Communicating to the world why this approach is better for security, reliability, and performance.

This repo is constructed to provide this solution as a suite of layered software products:

* HTTP Services on top of core technological scheduling/networking/compute/storage primitives stitched together from open-source technologies + bare metal that implement those services
* An SDK that wraps the public APIs of those services for convenient programmatic usage by customers in a variety of languages.
* Prebuilt clients that call our SDK (website/mobile app/cli)
* Integrations between other services the user already has entitlements to and our software offerings (GitHub App/VSCode Extensions) usually highly specific to a particular client.

The SDK allows folks to build their own clients and platforms on top of our raw software offerings and to script usage of Verself in whatever language an agent wants to use (currently only Go/TypeScript supported, Python support soon).

Note that the SDK layer today is only 1-2% implemented to the level of rigor it should be.

The objective behind all architectural decisions are to maximize adherence to the following ideas:

* Secure by default - Default deny, defense in depth. 
* Portable by default - we should minimize the lines of code necessary to ship sweeping changes across websites, embedded applications in third party widgets (e.g. VS Code extensions, once we have one), mobile apps (once we have them)
* Runnable on-prem (BYO-Compute, not self-host)

Planned Upcoming Projects

* Newsletter Service
* Analytics Service (PostHog clone) -- we build this ourselves using ClickHouse
* Readyset for Postgres query-result cache.
* Invoices + Preview Invoice for Current Billing Period
</mission_overview>
