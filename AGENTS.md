<repo_overview>
See @README.md
See @src/services/iam-service/schema/verself.zed for Zanzibar policies

console: verself.sh
auth portal: auth.verself.sh
services: <service>.api.verself.sh
company website: guardianintelligence.org

* `aspect` contains lots of helpful commands under `.aspect/`. Run `aspect` to get the list of tasks and task groups and `aspect <task> --help` for more details.
* Run `bazelisk query 'kind(".*", ...)` to learn more about how systems link together (expect large output, filter accordingly)

Current product: Blacksmith.sh clone (GitHub app that runs on bare metal) + persisting build artifacts with ZFS.

Benchmarks:

Blacksmith.sh + Sticky Disks # Currently disabled to save on cost - ~2m10s
GitHub with `actions/cache`# currently disabled to save on cost  - ~20m
our internal CI - ~1m30s

If we ever become slower than either platform, that becomes a top concern as speeding up our customers is a top priority.

This is a polyglot monorepo structured as a modular monolith.

General Structure:

 Smithy IDL + Verself traits (`src/smithy/models/verself`)
    -> Smithy semantic model
    -> Verself validators
    -> compact route catalog read model for runtimes and conformance
    -> official Smithy OpenAPI projection for public HTTP tooling
    -> hand-written routes that conform to the Smithy operation model
    -> service-local typed clients/adapters for repo-owned calls
    -> public SDK transports through OpenAPI tooling where reliable + curated wrappers

Layers:

1. Host layer: machine + OS configuration and bootstrap substrate like vm-orchestrator, guest telemetry staging, HAProxy, nftables, ClickHouse initial schema, ZFS, SPIRE, Nomad, WireGuard, and site/domain facts. Ansible operates on bootstrap host substrate. Nomad manages platform components, services, and frontends beyond that layer. Directories: `src/host`, `src/integrations`
2. Contract layer: Smithy models under `src/smithy/models/verself` describe public and internal service APIs, resource shapes, auth expectations, Zanzibar/IAM metadata, audit metadata, idempotency, pagination, rate limits, error sets, SDK behavior, generated projections, and conformance cases.
3. Service API layer: services expose the Smithy-modeled HTTP APIs at <service>.api.<domain>. Go services may use Huma during the cutover, but Huma/OpenAPI output is an implementation/projection artifact rather than the semantic contract.
4. Client/projection layer: OpenAPI compatibility artifacts are generated from the contract model for docs, ecosystem tooling, and public SDK transport generation where reliable. Repo-owned service calls use service-local typed clients/adapters with caller-owned SPIFFE mTLS transports.
5. Curated SDK layer: stable hand-written exports that wrap public transport implementations and own auth, idempotency keys, retries, pagination, waiters, error normalization, tracing headers, and DTO conversion.
6. Facades: the verself-web app and the CLI and, in the future, mobile apps.

IAM:

GCP-style IAM API
   `getIamPolicy` / `setIamPolicy` / `testIamPermissions`
   predefined roles + custom roles + role bindings

compiled by iam-service into

Zanzibar/SpiceDB relationships
   `resource#relation@subject`
   `resource#relation@role#member`
   parent edges

Zitadel for human identity, organization multi-tenancy & OIDC/SAML with third parties.

Tech Stack (partial description):

* ClickHouse for all time series data (host process metrics, time-series data from APIs), logs, traces, metrics (Wide Event pattern a. la Majors et. al/Honeycomb), miscellaneous append only event ledger where realtime policy decisions or UX isn't critical. ClickHouse rows never get updated
* TigerBeetle for financial OLTP. Currently using for financial truth and treating as a ledger -- we model debits/credits.
* Verdaccio to mirror NPM within our system to avoid north/south traffic being routine and to enforce minimum dependency age
* HAProxy (AWS-LC build) terminates public TLS with certificates issued by lego (Cloudflare DNS-01) and renewed by the typed `haproxy-lego-renew` Go unit; Ansible renders bootstrap `haproxy.cfg`, and Nomad-managed upstream reconciliation owns dynamic workload backends.
* SPIRE for our SPIFFE implementation, x509-SVIDs everywhere except services that don't support SPIFFE where we use short-lived JWT-SVIDs.
* Golang's River library for background jobs within a service. NATS JetStream for messaging/fan-out batch jobs between services.
* Stalwart over JMAP for inbound mail, Resend API integration for outbound

Invariant patterns:

* Do not add shell scripts. The only shell scripts allowed are the platform bootstrap entrypoints under `src/tools/dev/bootstrap/`. Scripts are load-bearing tooling and infrastructure. We control the execution environment and the installed binaries catalog both in the development environment and on the fleet. Choose the right tool for the job (it's never a shell script).
* Generic CI jobs are secretless. Build/test workflows running under GitHub Actions, Blacksmith, or Verself runners must not inject repository, organization, or environment secrets such as `VERSELF_TOKEN`, npm registry tokens, cloud credentials, SSH keys, database URLs, or deployment API keys. Jobs that need real staging or production authority run in a separate trusted lane gated by protected refs and GitHub Environments, and they acquire short-lived scoped credentials through OIDC/JWT exchange with the owning service.
* Efficient rebuilding & Independent deployments through ref-based GitOps -- Every deployable unit must be able to be deployed atomically without worrying about the rest of the topology. Bazel's job is to cache and decide when to run a unit's build pipeline. Nomad orchestrates deployments for non-host concerns. Ansible's job is to configure the host and ensure convergence. We rebuild only what we need by teaching Bazel about inputs and outputs. Deploys are just `aspect deploy` and Bazel and Nomad take over. Let each bazel boundary decide how to build itself. We finetune our build process per unit.
* Ansible mutates the host for bootstrapping the machine and installing initial binaries.
* Canonical API contracts live under `src/smithy/models/verself` as Smithy models. OpenAPI is generated for docs, ecosystem tooling, and transitional generators; it is not semantic truth.
* Smithy-first: OpenAPI is good at describing HTTP shapes, but Verself needs one contract to also carry correctness-critical semantics: Zanzibar permissions, OIDC audience and auth mode, SPIFFE-only internal surfaces, audit event names, idempotency policy, pagination shape, rate-limit class, request body limits, stable problem types, SDK behavior, and conformance cases. Smithy gives us a protocol-aware model with custom traits so those invariants can be validated and generated instead of re-declared in prose, route metadata, SDK code, and audit code.

* Service-oriented-architecture: with notable exceptions, repo-owned services talk to each other through service-local typed clients/adapters that implement the Smithy-modeled internal HTTP surface. Internal routes use SPIFFE mTLS and may include repo-only operations; public routes use Zitadel bearer auth.
* Every modeled operation should declare auth scheme, audience, permission, resource kind, action, org-scope derivation, rate-limit class, idempotency policy, audit event, request body budget, stable error set, SDK behavior, and conformance case coverage. Missing metadata is a contract bug, not a documentation gap.
* Service-local Go clients/adapters are the boundary for repo-owned service-to-service calls. Their consumers should be other services, with auth carried by caller-owned transports such as SPIFFE mTLS `http.Client` values from `service-runtime/workload`; do not hand-write `http.NewRequest` service calls or mint Zitadel machine-user bearer tokens for repo-owned service-to-service traffic. Curated customer/operator SDKs are handwritten only under `src/sdks/` or frontend SDK packages and may use generated public OpenAPI transports where tooling is reliable. Product services must not import those SDKs.
* Connect/protobuf belongs under `src/smithy/proto` for RPC-shaped internal surfaces, streaming, binary payloads, and privileged substrate protocols where protobuf is the primary protocol. For the public product-control-plane contract we project OpenAPI 3.1.
* Non-retrievable product token material belongs in `secrets-service` as an opaque credential. Product services may keep metadata/projection rows, but token generation, verifier storage, roll/revoke semantics, and verification must go through the service-local secrets-service client over SPIFFE.
* Dogfood as much as possible, even if it involves hairpinning requests through the internet. We are a customer on our platform. We go through the same billing abstractions, rate limits, and edge cases that a customer would face. We model ourselves as a platform org and receive a showback invoice with a 100% discount.
* Sync-engine pattern: PostgreSQL owns state, ClickHouse records the append-only ledger/traces, Electric/TanStack expose live read projections, and writes go through typed service commands whose conflict behavior matches the domain (strict observed-state rejection for security-critical resources, monotonic/idempotent collapse for notification-style cursors and dismissals).
* Generated artifacts in ignored directories are cacheable infrastructure, not disposable outputs. Do not fix stale generated imports by deleting `__generated` or other golden workspace state. A source dependency on generated output must have a current generator owner/manifest in the build graph; when removing a generator, update every source import in the same change. See `docs/architecture/generated-artifact-governance.md`.

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

<product_context>
Conceptually, the core product can be simplified as follows:

1. You onboard, switch to our runner and our custom checkout action.
2. You open a PR. You CI as normal.
3. Your CI goes green, you merge, target branch updates. CI runs on target branch and goes green. We generate a golden zvol of the target branch. We take your CI VM's repo artifacts and set that as the golden zvol for the next checkout. If it went red, the golden zvol stays on the last green CI run.
4. You open a new PR, it CIs but checkout is instant because we mount the entire repo instantly and all your migrations DB seeds, and so on, are already done. No more manual actions/cache per directory.
5. You CI but you only execute tests, no scaffolding to get your repo setup.
6. Your CI goes red, golden zvol stays where it is. You push some commits to your PR, we start from the golden zvol of the target branch.
7. Every time CI on a branch goes green we snapshot the result as that branch's new golden zvol. Merging is not a separate promotion step — it triggers CI on the target branch like any other push, and the green snapshot becomes that branch's golden. 

For repos with workflow yamls like 

```
   jobs:                                                                                                                                                  
      test-node-20:                                                                                                                                       
      test-node-22:                                                                                                                                       
      lint:                                                                                                                                               
      integration:                                                                                                                                        
      build-docker:
```

- test-node-20: Node 20 + node_modules built against Node 20's ABI + jest/vitest cache. Some packages (sharp, better-sqlite3, anything with prebuilds)
have different binaries per Node major, so this image genuinely differs from the Node 22 one.
- test-node-22: same shape but on Node 22.
- lint: Node + node_modules + .eslintcache + tsconfig.tsbuildinfo. No DB, no services. Smallest image in the set — and the one where the speedup vs. a
cold run looks least dramatic, because lint scaffolding is already light.
- integration: everything from test plus a running postgres with migrations and seed data, redis, anything else the suite touches. Heaviest image,
biggest speedup multiplier.
- build-docker: docker daemon, buildx layer cache, base image layers. None of the Node toolchain. Totally different disk shape.

We only promote the VM's GITHUB_WORKSPACE + durable volumes if *all* jobs go green on the commit to the trunk branch. A Bazel/npm/cache directory is allowed to be partially stale or corrupt. If it is bad, the tool should miss/rebuild. The cache is not semantic truth.

All mounts are rebuildable. Promotion is best-effort previous golden remains authoritative ambiguous seal skips promotion. We will expose cache misses and warnings so customers can go in and debug their CI themselves when things fail.

`getRepoZvolForPR`, therefore, takes `(organization, project, repo, target-branch, workflow-id, job-id, matrix-key)`. Our action's job is to go from our golden image (if it finds one) to make the working copy in `GITHUB_WORKSPACE` match the tree at the head SHA of the PR branch. 

Not every PR will have matrix-key. A workflow yaml edit is a non event -- if we have a zvol for that workflow job, then we have it. if not, then we don't, and if the edit gets merged in, we'll now have zvol for it for future PRs once CI passes.

Tree-hash is metadata on the snapshot, used for two specific things:
    a. At boot, we compute the diff between the snapshot's tree and the current tree, apply it as the "checkout" step.
    b. At merge, if the post-merge tree on the target branch exactly matches a snapshot we have, we retag without re-running the workflow (the step 7 fast
   path).

On `services: ` -- when a customer writes `services: postgres:16`, GitHub starts a fresh container per job. We honor that as written. The snapshotted-postgres speedup applies to the customer's own setup scripts (the postgres they start and seed themselves) — not to GitHub's managed service containers. 

Note: DB seeds, Docker layers, local services are not in GITHUB_WORKSPACE.

The customer's mental model becomes: "my CI YAML stays exactly the same, the runner type changes, and the steps that used to take minutes now take  seconds because the work was already done." They don't learn a new caching API. They don't declare inputs. They don't tag things. The only Verself-specific surface is the checkout action.

We can offer (in the future):

1. An SDK to list golden zvols and get metadata and to create/delete them
2. An SDK to spin up a VM with the ID of a zvol
2a. SSH access to VMs running on our metal, gated by Pomerium.=
3. An SDK to download a golden zvol

All of the above can help with debugging.

In addition to copying the entire repo, we also provide a durable mount API. The customer-facing promise:

> Any directory your CI job writes outside GITHUB_WORKSPACE can be declared as durable. Verself
> mounts the latest trusted version before the job starts, lets the job mutate it normally, then
> snapshots it after success. Pull requests start from the target branch’s last green durable
> state, but their writes cannot poison the target branch

We can also provide a simple API to prevent certain files or directories from being part of the golden zvol. We can design that later as it requires care and, like most everything else we offer, it will have an SDK/CLI/HTTP API to our services.

- Today's surface is a Blacksmith.sh-style GitHub Actions runner replacement: customers point CI at Verself and workflows run on Verself Firecracker VMs for a 2–10x speedup. We dogfood it on every merge to main, comparing against Blacksmith.sh and GitHub Actions to verify we are faster.
- Verself does not host customer applications. Customer code runs only inside short-lived sandboxes the customer rents (CI workflow runs today; Lambda-style invocations and persistent dev VMs later) on the same isolation, billing, and telemetry substrate.
- The bootstrap CLI is a separate offering. It renders site artifacts onto operator-supplied Latitude.sh bare metal so an operator can stand up an independent Verself installation. Once deployed, that installation runs at its own domain under its own name and has no runtime coupling to verself.sh: there is no tenant relationship, no upstream control plane, no shared identity, no shared data. See `docs/verself-cli.md`.
</product_context>

<product_policy>

Public commitments for Data Processing, Acceptable Use, Security, SLA, and Data Retention live in `src/websites/apps/verself-web/src/routes/_workshop/policy`.

</product_policy>

<product_direction>

Where the platform is headed: open-source-per-subdirectory, privileged-host / product-service split, multi-tenant + customer dogfooding, three customer-facing sandbox products (CI runner, Lambda-like workload, long-running VM), self-hosted Forgejo/CI; agents merge to `main` continuously and environments deploy whichever SHA the `staging-tip`/`prod-tip` refs point at, advancing only after a canary soak passes — no long-lived release branches, unfinished work hidden behind feature flags.

See `docs/product/future-state.md`.

</product_direction>

<system_context>

Service topology, three safety rings, self-hosted mandate + allowed third-party providers (Cloudflare, Latitude.sh, Resend, Stripe), dual-write pattern, billing model summary, supply chain, founder focus areas, bare-metal OS/arch invariants.

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

If you are doing work that involves pulling logs or interacting with infrastructure you may be presented a URL to log in to Pomerium. If that happens, please pause and present the URL to me and remind me to open it in Firefox.

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

Nomad deploys are driven directly by the checked-in `nomad_component` targets for the requested SHA:

```shell
aspect deploy --site=prod --sha=HEAD
```

`aspect deploy` builds the Bazel-discovered descriptors, uploads missing content-addressed artifacts to the private Garage origin, resolves each `nomad.json`, and submits the resulting payloads to Nomad with ClickHouse evidence for each job decision.

### High-signal Documents.

@README.md -- map to other documents.

Recommended that you read relevant ones directly. You can have a subagent summarize the ones that are not related to your task.

- **Inbound mail, Stalwart, mailbox-service, JMAP, SMTP, inbound routing, tenant isolation:** `src/services/mailbox-service/docs/inbound-mail.md`
- **vm-orchestrator privilege boundary, Firecracker VM networking, TAP allocator, host service plane, nftables, guest CIDR, lease/exec model, vm-bridge control:** `src/substrate/vm-orchestrator/AGENTS.md`
- **ZFS golden environment lifecycle, zvol, clone, snapshot, promote:** `src/substrate/vm-orchestrator/docs/zfs-volume-lifecycle.md`
- **Canonical API contracts, Smithy models, route catalog, OpenAPI projections, public SDK transport generation, Connect/protobuf boundary:** `src/smithy/README.md`
- **VM execution control plane, sandbox-rental-service ↔ vm-orchestrator split, attempt state machine, billing windows, execution lifecycle:** `src/services/sandbox-rental-service/docs/vm-execution-control-plane.md`
- Billing architecture, credit subscription, entitlements, metering, TigerBeetle, PostgreSQL, Reconcile, refunds, plan change, dual-write, Stripe webhooks, invoices:** `src/services/billing-service/docs/billing-architecture.md`
- **Governance audit data contract, HMAC chain, OCSF, CloudTrail parity, tamper evidence, SIEM export, audit ledger:** `src/services/governance-service/docs/audit-data-contract.md`

</operational_runbook>

<assistant_contract>
- Ground proposals, plans, API references, and all technical discussion in primary sources. Then think from the perspective of the user of the system: a non-technical startup founder running all services off a single bare-metal box (with upgrade path to a 3-node topology).
- When beginning an ambiguous task, collect objective information about how the system actually works. There are a lot of technologies stitched together; understand how everything connects.
- Act as a dispassionate advisory technical leader with a focus on elegant public APIs and functional programming.
- You are not alone in this repo. Expect parallel changes in unrelated files by the user. Leave them alone (don't stash them) and continue with your work. Do not stash parallel work.
- This software is currently pre-release and serves no customers or users. There is no backwards compatibility to maintain. No compatibility wrappers, no legacy shims, no temporary plumbing. All changes must be performed via a full cutover.
- It's important to delete old or outdated code when we upgrade technology, abstractions, or logic. Eliminating contradictory approaches must uphold the bar: no trace of a contradicting or legacy implementation can be left in the code base after a change is pushed to main. The reader must not be able to tell the previous implementation ever existed, unless they spelunk through the git history.
- Details matter. The founder cares about arcane versioning issues, subtle race conditions, timing-attack vulnerabilities, GC pressure, and abstraction leaks. Simplicity is for code and architecture, not for technical argument.
- Some directories have their own `AGENTS.md` file. When working inside those directories, read them — they contain juicy context.
- Incidental edits from running linters and formatters are expected. Don't worry about them.
- When in doubt, use the industry-standard pattern. Everything has boring, battle-tested solutions and we should prefer to use those. Don't reinvent the wheel. Open standards and protocols underneath FOSS are the gold standard.
- `.aspect/`, `README.md`, `AGENTS.md`, schema migration files, and Smithy models are high signal documents. Read them directly; avoid summarizing them with a subagent as important detail may be lost.
- Do not provide time estimates.
- Prefer to make incorrect behavior impossible by construction.
- My 'd' key is broken so you may see frequently see the letter 'd' missing from user messages
- Avoid excitement around counting commits/LOC changed/number of tests passing. Maintain an intellectually curious, skeptical posture.
</assistant_contract>

<writing_guidelines>
Before writing markdown architecture in docs/ directories, please read docs/agents/writing-guidelines.md
</writing_guidelines>

<tool_use_contract>
- Dev tools are system-installed via `aspect dev install`.
- Avoid one-off, non-syntax-aware scripts for large parallel changes or refactors. Use subagents for that class of task — unexpected edge cases are likely and judgement is often required.
- Use `aspect tidy` to format the codebase efficiently. Use `aspect bazel update` for Gazelle/Bzlmod metadata refreshes
</tool_use_contract>

<output_contract>
- When providing a recommendation, consider different plausible options and provide a differentiated recommendation leaning toward the simplest solution that best sets this project up for the *long term* in terms of functionality, elegance of architecture, security, performance, and best-practices.
- Unit tests and successful `bazelisk` and `aspect` commands are low signal and are not to be trusted. Real observability traces in ClickHouse post-deployment that exercise the modified code are the only admissible completion evidence. ClickHouse exists for producing verifiable completion artifacts. If a new schema is needed you can create one.
- Do not speculate without evidence. Logs, traces, and host metrics are queryable in ClickHouse via `aspect db ch query --query='...'` — check them before attributing failures to transient or pre-existing factors.
- Do not stop work short of verifying changes with a live rehearsal of a deployment via `aspect deploy`. You have full authority to wipe databases and recreate them as needed. Prefer that over time-consuming, tricky migrations during this early phase.
</output_contract>


<coding_contract>
Before writing code, please read docs/agents/coding-guidelines.md and apply the relevant rules to your reasoning.
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

## Adding a site

Site names are `prod`, `beta`, `gamma`, or `dev-<operator>`. The apex domain, Pomerium route name, Cloudflare zone scope, and allowed Stripe environment are site-level facts in `src/host/sites/<site>/vars.yml`.
