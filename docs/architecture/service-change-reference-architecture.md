# Service Change Reference Architecture

Every service change is described as a resource-lifecycle change before it is
described as an implementation change. The review object is a Service Change
Packet: one document or PR section that starts with the SDK method and follows
the operation through Smithy, authorization, product state, billing, capacity,
retention, deployment, observability, and release evidence.

The packet is required for changes that add or alter a customer-visible
resource, service-to-service API, quota, billing behavior, durable artifact,
background worker, retention rule, or product state transition. Sections may be
marked `not_applicable` only with a concrete reason.

## Review Spine

```text
SDK resource method
  -> Smithy public or internal operation
  -> service-local transport and handler
  -> product state machine
  -> IAM and security enforcement
  -> quota and billing admission
  -> capacity plan and retention policy
  -> observability, audit, recovery, and deploy evidence
```

The first stable API is the curated SDK. Smithy is the semantic authority for
public and internal service contracts. The service implementation, CLI, web
server functions, and generated OpenAPI projections conform to the Smithy model.

## Service Change Packet

| Section | Required output |
| --- | --- |
| Customer API | Resource noun, SDK module and method names, selector shape, response DTO, example call, and CLI or web facade use. |
| Lifecycle | Resource states, allowed transitions, terminal states, retries, reconciliation, concurrent mutation behavior, and observed-state conflict behavior. |
| Smithy contract | Operation, input, output, errors, auth mode, Zanzibar permission, audit event, rate-limit bucket, request body budget, idempotency, pagination, SDK hints, and recovery traits when state is material. |
| IAM story | Protected resource, parent edge, permission names, predefined/custom role impact, organization switching behavior, Zitadel actor mapping, and SpiceDB relationship writes. |
| Security story | Threat model delta, secret handling, high-risk operation classification, 2FA escalation, SSRF/path traversal/injection review, tenant boundary, and penetration-test scenario. |
| Network story | Public HAProxy route or internal SPIFFE-only route, service discovery entry, east/west caller identity, customer fabric impact, and egress requirements. |
| Capacity plan | RAM, vCPU, ZFS/block storage, object storage, PostgreSQL rows and indexes, ClickHouse rows, TigerBeetle transfers, NATS/Temporal/River work, file descriptors, network traffic, and worst-case payload size. |
| Quota and admission | Product quota being checked, reserve/admit/deny point, hard-cap behavior, free versus paid tier behavior, and repair path for stale reservations. |
| Billing and metering | SKU, usage evidence, billing window reserve/activate/settle/void calls, rate context capture, no-consent behavior, finalization impact, and ClickHouse projection. |
| Retention and recovery | State class, product retention, backup immutability, RPO, RTO, tombstone behavior, physical reaping, `Describe` and `List` behavior after deletion, and disaster-recovery drill evidence. |
| Caching | Cached object, authority source, invalidation trigger, freshness bound, tenant key, negative-cache behavior, and proof that stale data cannot authorize or bill incorrectly. |
| Observability | Spans, logs, metrics, ClickHouse evidence rows, dashboard/update path, missing-metric alert, and traces that join SDK request to service and substrate work. |
| Audit | OCSF activity, actor, credential metadata, resource names, permission, denied-request evidence, HMAC chain verification, and export impact. |
| Deployability | Owning Bazel target, migrations, Nomad job, route registration, config/secrets, recovery status endpoint, independent deploy path, rollback or full-cutover plan, and generated artifact ownership. |
| Load and failure testing | Product-level load test, maximum payload test, failure injection, worker retry test, reconciliation test, and saturation behavior. |
| Documentation and communication | Public docs, internal docs, AGENTS guidance, SDK examples, policy updates, release note, customer notification, and lead-time rule for customer-impacting runtime changes. |
| Release evidence | Canary scenario, `aspect deploy` evidence, ClickHouse queries, PostgreSQL/TigerBeetle checks, host metrics, billing projection checks, audit rows, and SLO gate. |

## SDK And API Ergonomics

Service changes start with the SDK call the customer or operator should write.

```ts
const snapshot = await verself.runs.snapshots.invalidate(snapshotRef, {
  reason: "capacity_reclaim",
}, {
  idempotencyKey: "invalidate-snapshot-2026-05-22",
});

await snapshot.waitUntilReaped();
```

Required SDK properties:

- Resource modules use product nouns rather than service names.
- Durable resources return `id`, `resourceName`, optional `slug`, and
  `displayName`.
- Lists are cursor-paginated from the first version of the API.
- Mutations accept idempotency keys and preserve request ID and trace ID.
- Errors normalize to stable RFC 9457 problem codes.
- Waiters are first-class SDK methods for asynchronous state transitions.
- SDK examples use workload identity or credential files for production paths.

If the SDK method is awkward, the Smithy operation should be redesigned before
the CLI, web facade, or docs grow around it.

## Long-Running Operations And Waiters

Operations that cross a service boundary, call an external provider, run a
background worker, wait for substrate state, or commonly exceed an interactive
request deadline should return a durable operation handle or a resource in a
non-terminal state. The resource must be visible through `Get` and `List` while
the operation is in progress.

Every long-running mutation declares:

- operation resource identity and retention;
- target resource state enum;
- metadata fields that expose progress, partial failure, retry time, request
  ID, and trace ID;
- terminal success states;
- terminal failure states;
- cancellation behavior;
- whether parallel operations are rejected, queued, or superseded;
- waiter names and default polling/backoff policy.

Waiters belong in the curated SDK and should be derivable from the Smithy
operation model. A waiter polls a read operation until a resource enters a
terminal success or terminal failure state. Non-terminal errors are exposed as
metadata or retried according to `Retry-After` and SDK retry policy. Terminal
failure means the desired state cannot be reached without a new user or
operator action.

Useful waiter names are state-oriented:

```text
waitUntilRunSucceeded
waitUntilSnapshotReaped
waitUntilExportReady
waitUntilBillingDocumentIssued
waitUntilCredentialRevoked
```

The waiter contract is part of the API design. It is not CLI polling logic
hidden in a command implementation.

## Lifecycle And History

Every durable resource has a lifecycle table in the Service Change Packet. The
minimum table is:

| State | Entered by | Visible to `Get` | Visible to `List` | Billable | Reapable | Terminal |
| --- | --- | --- | --- | --- | --- | --- |
| `creating` | create accepted | yes | yes | policy-specific | no | no |
| `active` | create completed | yes | yes | yes | no | no |
| `updating` | update accepted | yes | yes | yes | no | no |
| `invalidated` | user or policy invalidation | yes | filtered by default | no new usage | yes | no |
| `reaping` | retention worker | yes | filtered by default | no new usage | in progress | no |
| `reaped` | physical destroy completed | yes until history expiry | filtered by default | no | no | yes |
| `tombstoned` | history retention expired | 404 after authorized lookup | no | no | no | yes |

`Get` and `Describe` should preserve product history through the declared
history retention window. Physical bytes can be destroyed while the product
record remains readable as a tombstone. A customer or operator who asks about a
recently invalidated snapshot should see `invalidated`, `reaping`, or `reaped`
with timestamps, reason, and reclaim evidence rather than an unexplained 404.

`List` defaults to active resources and exposes explicit filters such as
`state=invalidated`, `state=reaped`, or `include_tombstones=true` when history is
part of the product support surface.

## Capacity Planning

Capacity estimates start at the product unit and include the backing database
and telemetry footprint. The packet must define a unit of growth and a peak
scenario.

| Unit | Questions |
| --- | --- |
| Compute | vCPU, RAM, process count, concurrent leases, Firecracker overhead, worker concurrency, thread pools, and GC pressure. |
| Block storage | zvol size, snapshots, clones, vmstate/memory artifacts, compression, encryption, ZFS reservation/refquota, and reclaim latency. |
| Object storage | artifact count, byte size, key fanout, manifest size, immutability, and delete-marker behavior. |
| PostgreSQL | rows per product action, indexes touched, transaction width, lock scope, queue rows, and retention sweep cadence. |
| ClickHouse | wide-event rows, audit rows, metering rows, partition key, sort key, compression expectation, and TTL if any. |
| TigerBeetle | transfer count, account count, idempotency key/correlation ID use, and reconcile path. |
| Network | north/south request and response bytes, provider webhook bytes, service-to-service bytes, NATS/Temporal traffic, and customer egress. |
| Runtime queues | River jobs, Temporal workflows, retry fanout, dead-letter behavior, and repair scanner bounds. |
| Host limits | file descriptors, TAP slots, loop devices, ZFS dataset count, jailer directories, Nomad allocation count, and disk free thresholds. |

Rough estimates are acceptable, but the assumptions must be explicit. Include
ClickHouse rows and indexes in storage sizing because telemetry and evidence
are part of the product design.

## Quota, Billing, And Metering

Product services perform product admission. Billing performs financial capacity
reservation and settlement. The normal billable flow is:

```text
check IAM and ownership
  -> check product quota, risk, and resource policy
  -> reserve billing capacity when usage is billable
  -> execute the product operation
  -> settle or void the billing window with measured evidence
  -> project metering and billing events
```

The packet must state whether the resource consumes:

- hard product quota, such as snapshot count, VM concurrency, or durable bytes;
- prepaid or subscription entitlement;
- internal capacity only;
- billable customer usage;
- non-billable rebuildable acceleration capacity.

ClickHouse usage rows are evidence and read models. PostgreSQL product state,
TigerBeetle balance facts, and billing windows remain the authority for
admission, settlement, and document issuance.

## Retention, Reaping, And Recovery

Retention has three separate decisions:

1. Product history retention: how long `Get`, `Describe`, audit, exports, and
   support tooling can explain what happened.
2. Physical byte retention: when block devices, snapshots, object bodies, and
   temporary artifacts can be destroyed.
3. Backup retention: whether the bytes are protected by a recovery mechanism
   and immutable backup window.

`rebuildable_acceleration` artifacts such as CI golden zvol generations and VM
snapshots are excluded from backup catalogs unless a product policy explicitly
changes that. Loss is a cache miss and rebuild. Their product metadata still
needs retention, audit, and reaper evidence.

Every reaper is idempotent and evidence-producing:

- selects only resources that are not referenced by current pointers, manifests,
  billing evidence, exports, or active operations;
- records the observed reference set and source generation;
- calls the substrate through a typed service-local or privileged API;
- records bytes before, bytes after, error code, retry time, and trace ID;
- leaves the product row in `reaped` or `reap_failed`;
- emits ClickHouse evidence and audit activity when customer-visible.

For ZFS-backed artifacts, retention must preserve durable generations referenced
by current durable pointers or golden VM manifests. Firecracker vmstate and
memory artifacts are retained through the golden VM manifest.

## IAM And Security

The Service Change Packet must identify the authorization object before code is
written. The owning service stores product state; `iam-service` owns
authorization graph writes and checks; SpiceDB remains private substrate.

Required IAM decisions:

- protected resource type and parent resource;
- permission names and role impact;
- relationship writes and idempotency keys;
- consistency/freshness requirement for checks;
- list authorization strategy: coarse gate, lookup, bulk check, or bounded
  projection;
- behavior when the same browser or SDK client switches organizations;
- denied-request audit evidence.

Security review covers:

- tenant isolation;
- user, service account, workload, provider, and repo-owned service actors;
- sensitive DTO fields and log redaction;
- high-risk operation 2FA escalation;
- public route abuse, body size, rate-limit bucket, and bot-defense posture;
- network boundary: HAProxy, SPIFFE mTLS, checkout grants, provider webhooks,
  or customer fabric;
- supply-chain posture, including pinned dependencies, Bazel/toolchain
  integration, internal package mirrors, and base guest image impact.

## Observability, Audit, And SLOs

The observability plan follows three product questions:

- Are customers having a good time?
- Is the hardware secure and healthy?
- Are customers being charged correctly?

Each changed path needs spans, wide events, metrics, and ClickHouse evidence
that join on request ID, trace ID, resource name, org ID, and operation ID.
Missing SLO-bound metrics should alert during canary, not after an incident.

Audit coverage follows the governance API Activity contract:

- successful operation row with OCSF class, actor, resource, permission, status,
  trace, payload hash, and HMAC chain;
- denied request row with the requested permission and no handler mutation;
- credential metadata without secret material;
- export rows and manifest checks when data export is affected.

SLO gates use customer-facing SLIs first: availability, latency, correctness,
freshness, and task completion. Host metrics, security detections, queue depth,
and database health explain SLI movement but do not replace product SLIs.

## Deployment And Release

Every service remains independently deployable. The packet must confirm:

- owning Bazel target and deployable unit;
- Nomad job and service registration;
- HAProxy discovery entry for new public services;
- SPIFFE identity and service-local clients for repo-owned calls;
- migrations, generated artifacts, and validators;
- recovery status endpoint for declared recoverable sources;
- config and secrets source;
- post-deploy canary declarations and rollback or full-cutover behavior.

Deployable units declare live checks at the Bazel edge, next to the
`nomad_component`, with `post_deploy_canary`. Medium checks are the normal
rollback gate for a deploy. Large checks are deeper browser/CLI canaries for
release candidates, high-risk migrations, and soak validation. Canary targets
should exercise product behavior through the same public or service-local
contracts customers and repo-owned callers use; Nomad allocation health alone is
not release health.

Release classification is explicit: additive public API, breaking public API,
internal-only API, policy-only behavior change, operator-only change, runtime
image change, billing change, retention change, or security change. RC priority
follows customer impact, data risk, security risk, billing correctness, and
deployment blast radius.

Pre-release status removes backwards-compatibility obligations, but it does not
allow contradictory implementations to coexist. A change that replaces an
abstraction removes the retired path in the same cutover.

Customer communication is required when a change affects customer-visible
runtime behavior, data collected from customer workloads, guest image contents,
base image size, billing interpretation, retention, quota, API contract, or
security posture. Guest image and agent changes should announce the future
activation point, then rely on VM/artifact retention to phase out old images.

## Load And Failure Testing

Tests exercise the highest product abstraction that changed. A service-boundary
change needs load at the public or internal API surface. Handler unit tests are
supporting evidence. The packet states:

- target TPS and concurrency;
- maximum payload size;
- hostile pagination and filtering cases;
- repeated idempotency-key calls;
- conflict and stale-version calls;
- rate-limit behavior;
- billing denial, settlement failure, and void paths;
- substrate failure or provider timeout;
- reaper retry and reconciliation after process death.

The target stress case is intentionally beyond expected early traffic. If a
100,000 TPS test is infeasible on the current single-node topology, the packet
states the bottleneck, runs the highest meaningful product-level test, and
records the capacity gap as product evidence.

## Example: Golden VM Snapshot Ring Buffer

The snapshot-retention change is a standard Service Change Packet:

| Section | Decision |
| --- | --- |
| Customer API | `verself.runs.snapshots.get`, `list`, `invalidate`, and `waitUntilSnapshotReaped`; CLI filters default to active/current snapshots. |
| Scope | Define the ring-buffer key explicitly, such as organization, repository, target ref, workflow job shape, matrix key, trust class, runner class, platform image, durable generation set, and Firecracker ABI. |
| Product policy | Free tier retains one current compatible golden snapshot per scope. Paid tier retains two. Unreferenced candidates are retained only for debugging until retention policy makes them reapable. |
| State | `candidate`, `current`, `superseded`, `invalidated`, `reaping`, `reaped`, `reap_failed`, `tombstoned`. |
| `Describe` | Authorized callers see invalidated and reaped history until product history retention expires, including reason, invalidated time, reaped time, bytes reclaimed, and manifest refs safe to expose. |
| Physical reaping | Destroy Firecracker vmstate/memory artifacts and ZFS generations only after no current durable pointer or golden VM manifest references them. Reaper is idempotent by generation and manifest ref. |
| Capacity | Estimate per snapshot: root zvol reference, workspace/durable generation refs, vmstate bytes, memory bytes, PostgreSQL rows, ClickHouse evidence rows, and ZFS metadata. Track disk free before and after reaping. |
| Billing/quota | Treat golden snapshots as internal acceleration capacity unless a future durable-storage product admits them as a billable SKU. Enforce count quota separately from billable durable bytes. |
| Observability | Emit durable hit/miss, golden hit/miss, invalidation, reaper selected, destroy attempted, destroy completed, bytes reclaimed, and pointer promotion events. |
| Audit | Invalidation is customer-visible and audited. Automatic policy reaping emits governance or operator evidence according to whether the operation is exposed to the org. |
| Release evidence | After deployment, query PostgreSQL state, ClickHouse durable/golden events, ZFS referenced bytes, host disk free, audit rows, and a waiter path that observes `invalidated -> reaped`. |

This separates the ring buffer from the historical product record. The ring
buffer decides which snapshots remain current and restorable. The retention
worker decides when unreferenced bytes are destroyed. Product history explains
the resource after the bytes are gone.

## Agent Invocation

Use this prompt for service changes:

```text
Use docs/architecture/service-change-reference-architecture.md.
Work backwards from the curated SDK API and produce a Service Change Packet
before implementation. For every resource lifecycle transition, cover Smithy,
IAM, security, capacity, quota, billing, retention, waiters, observability,
audit, deployability, load testing, docs, customer communication, and live
evidence. Mark a section not_applicable only with a specific reason.
```

For narrow bugs, the packet can be a short PR comment. For new product behavior,
it should be a checked-in doc or a design section in the implementation PR.

External reference points:

- Smithy waiters: <https://smithy.io/2.0/additional-specs/waiters.html>
- Google AIP-151 long-running operations: <https://google.aip.dev/151>
- Google AIP-158 pagination: <https://google.aip.dev/158>
- Google AIP-135 delete semantics: <https://google.aip.dev/135>
- AWS Well-Architected pillars: <https://docs.aws.amazon.com/wellarchitected/latest/framework/the-pillars-of-the-framework.html>
- Google SRE service level objectives: <https://sre.google/sre-book/service-level-objectives/>
