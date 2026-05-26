# BYOC On-Prem Compute Cells

BYOC on-prem is a cell-based compute extension of the hosted Verself control
plane. Customer hardware contributes execution capacity while the hosted control
plane remains the authority for organizations, IAM, billing admission, scheduling
policy, audit evidence, and release orchestration.

A detached self-hosted Verself deployment is a separate installation mode. It
owns its own substrate, identity, data, billing, and operations, and therefore
gets its own `installation_id`. A BYOC on-prem cell is an org-scoped resource
inside the hosted installation.

## Plane Model

```text
hosted Verself installation
  -> organization
  -> compute pool
  -> cell
  -> workload node
  -> Firecracker execution
```

The hosted control plane:

- owns API contracts, org membership, IAM policy, quotas, billing windows,
  release channels, deployment manifests, and audit export;
- admits work before it reaches a customer site;
- signs desired cell manifests and records release evidence;
- receives append-only telemetry, metering evidence, and audit events.

The customer compute cell:

- runs customer CI jobs, durable workspaces, local caches, ZFS generations,
  Firecracker snapshots, and short-lived execution leases;
- reconciles a last-known-good signed manifest;
- reports host health, execution evidence, metering evidence, and deploy status;
- continues accepted work during hosted-control-plane outages within lease caps.

The cell's product authority is limited to local execution of admitted leases.
It has no authority to mint organizations, expand quota, generate product
credentials, rewrite billing state, or authorize work outside leases admitted by
the hosted control plane.

## Resource Identity

The existing `installation_id` remains the namespace root for public resource
names. New BYOC resources should follow the same URN contract:

```text
urn:verself:<installation-id>:orgs/<org-id>/computePools/<compute-pool-id>
urn:verself:<installation-id>:orgs/<org-id>/computePools/<compute-pool-id>/cells/<cell-id>
urn:verself:<installation-id>:orgs/<org-id>/computePools/<compute-pool-id>/cells/<cell-id>/nodes/<node-id>
```

Initial resource nouns:

| Resource | Scope | Purpose |
| --- | --- | --- |
| `computePool` | Organization | Customer-owned capacity boundary, billing policy, scheduling eligibility, release channel, and maintenance policy. |
| `cell` | Compute pool | Failure, rollout, and observability boundary. A cell contains one or more workload nodes in one customer site or network domain. |
| `workloadNode` | Cell | Physical or virtual host enrolled into the cell with attested capabilities and a drain state. |
| `runnerClass` | Compute pool | Schedulable shape such as CPU, RAM, disk, accelerator, architecture, isolation, and cache policy. |
| `cellManifest` | Cell | Signed desired-state document applied by the cell agent. |

The stable selector for customer placement should be `computePool` or
`runnerClass`. Raw node IDs remain internal scheduling state unless a customer
explicitly requests dedicated hardware semantics.

## Connectivity

The default connectivity model is outbound-only from customer infrastructure to
Verself over mutually authenticated TLS on TCP 443. This matches the operational
shape used by hybrid CI runners and avoids customer inbound firewall openings.

The cell agent maintains long-lived control streams for:

- manifest discovery and reconciliation;
- lease admission and assignment;
- execution status;
- telemetry and evidence upload;
- artifact and cache metadata exchange.

Private connectivity variants can layer on top of the same protocol:

- customer VPN or private WAN;
- Netbird-managed overlay for operator-approved cells;
- cloud private connectivity when the customer site is a public-cloud VPC;
- disconnected package mirrors for regulated installations.

Every protocol message carries the org ID, compute pool ID, cell ID, manifest
digest, and trace context. Service authorization must validate these fields
against IAM, pool state, and lease state rather than trusting network location.

## Static Stability

Cells reconcile from signed manifests and keep the last-known-good manifest
available on disk. Control-plane unavailability affects new admissions and
configuration changes; it does not interrupt already accepted work.

Static stability requirements:

- accepted leases include bounded duration, resource ceilings, secret scopes,
  and billing reservation IDs;
- no work starts without a live or recently issued lease admitted by the hosted
  control plane;
- a disconnected cell can finish accepted work and spool evidence locally;
- evidence upload is idempotent and replayable after reconnect;
- manifest rollback is a normal release operation, not an emergency-only path;
- emergency revocation can mark a manifest or cell version ineligible for new
  work while already running jobs are drained or killed according to policy.

The cell must fail closed for missing manifests, invalid signatures, expired
leases, stale trust bundles, unknown orgs, and unavailable secret envelopes.

## Release Channels And Waves

Release channel describes a customer's selected freshness and support contract.
Rollout wave describes Verself's fleet sequencing for a specific release.

Recommended release channels:

| Channel | Use |
| --- | --- |
| `preview` | Internal and design-partner cells that intentionally receive early cell-agent and substrate changes. |
| `rapid` | Customers that want fast feature access and accept shorter soak. |
| `regular` | Default production channel after hosted and rapid soak. |
| `stable` | Conservative production channel for critical workloads. |
| `extended` | Long-support channel for regulated or change-controlled sites. Security fixes still advance through the channel. |

Recommended rollout waves:

```text
internal dev cell
  -> Verself-owned prod cell
  -> hosted low-criticality production jobs
  -> design-partner BYOC cells
  -> rapid customer cells
  -> regular customer cells
  -> stable and extended customer cells
```

Hosted production is an early wave because Verself controls remediation,
observability, and rollback. Customer on-prem cells receive changes after the
release has produced evidence in controlled environments.

## Manifest-Driven Configuration

Feature flags select typed, signed manifest revisions. Unstructured remote
configuration is out of scope for cell rollout.

Each cell manifest should include:

- schema version and manifest digest;
- intended installation, org, compute pool, and cell;
- release channel and rollout wave;
- cell-agent binary digest and allowed previous digests for rollback;
- vm-orchestrator, guest telemetry, Firecracker, jailer, ZFS, nftables, and
  host package catalog digests;
- runner classes and scheduling constraints;
- egress policy, artifact endpoints, and package mirror policy;
- telemetry schemas and required ClickHouse evidence fields;
- lease ceilings, offline grace bounds, and drain behavior;
- compatibility constraints against host kernel, CPU flags, filesystem layout,
  and hardware capabilities.

The cell agent applies manifests transactionally:

```text
download manifest
  -> verify signature, target, and trust root
  -> validate host compatibility
  -> stage artifacts by digest
  -> run local preflight
  -> apply to inactive slot where possible
  -> report evidence
  -> admit new work only after health gates pass
```

The control plane should model manifest promotion as a durable operation with a
waiter and ClickHouse release evidence. A failed promotion keeps the previous
manifest active and records the failing phase.

## Scheduling And Admission

The control plane admits work against an org, project, repository, runner class,
compute pool policy, quota, and billing reservation. The cell receives only
accepted leases.

Admission checks:

- IAM permission for the actor and repository binding;
- org and project policy;
- compute pool state, release channel, and maintenance window;
- runner class capacity and hardware compatibility;
- risk, compliance, and abuse holds;
- billing reservation and lease budget;
- secret and checkout grant availability;
- provider event identity, such as GitHub installation and repository IDs.

The cell scheduler handles local placement after admission. It can reject work
for capacity or host health, but it cannot broaden the lease.

## Secrets And Data Residency

Customer code, workspaces, durable caches, golden artifacts, and execution logs
remain in the customer cell unless the product contract explicitly exports them.
The hosted control plane receives metadata, audit events, metering evidence,
health signals, and customer-visible summaries.

Secrets sent to a cell should be just-in-time envelopes scoped to one execution
lease, one org, one cell, and one expiry. Secret material must be unavailable to
the cell after lease expiry except where the customer has explicitly configured
a cell-local secret source.

The control plane should store enough evidence to answer:

- who admitted the work;
- which org, repository, compute pool, cell, and manifest handled it;
- which secret envelopes were issued;
- what resources were reserved and consumed;
- which artifacts were produced or promoted;
- whether the cell was healthy and within policy.

## Failure Modes

| Failure | Required behavior |
| --- | --- |
| Hosted control plane unavailable | Cell finishes accepted leases, spools evidence, and rejects new work after admission grace expires. |
| Cell loses outbound network | Running leases continue within bounds; new work is rejected; health state becomes stale in the control plane. |
| Manifest apply fails | Previous manifest remains active; cell reports failing phase and refuses new work if the failure affects safety. |
| Bad cell-agent release | Roll back by manifest digest; pause rollout waves; mark affected version ineligible for new admissions. |
| Host node failure | Cell drains or fails active leases according to execution policy; control plane records explicit failure evidence. |
| Customer site outage | Control plane stops assigning new work to the cell and may route eligible work to other pools. |
| Evidence upload delayed | Cell persists append-only evidence locally and replays idempotently after reconnect. |
| Trust bundle stale | Cell refuses new leases and reports recovery status until trust is refreshed. |

## Observability And Evidence

Every cell action that affects customer-visible state should emit traceable
evidence with stable IDs:

- control-plane admission span and billing reservation;
- manifest promotion operation and target digest;
- cell preflight and apply phases;
- node health and drain state;
- execution lease lifecycle;
- guest boot, checkout, job start, job finish, and cleanup;
- metering rows for CPU, memory, disk, network, and golden artifact storage;
- audit rows for admission, denial, manifest promotion, operator override, and
  emergency revocation.

ClickHouse is the completion-evidence surface for rollout health. A release
should not advance to the next customer wave unless the previous wave has
produced enough live evidence for successful manifest apply, lease execution,
metering, audit, and recovery status.

## Shared Responsibility

Verself owns:

- hosted control plane availability;
- public API and SDK contracts;
- cell-agent and substrate release artifacts;
- signed manifests and rollout orchestration;
- IAM, audit, billing, quota, and metering semantics;
- support diagnostics that can operate from uploaded evidence.

The customer owns:

- physical site availability, power, cooling, and network;
- host procurement and hardware lifecycle unless contracted otherwise;
- local firewall and outbound connectivity policy;
- customer-managed package mirrors, object stores, and secret sources when used;
- compliance controls for data that remains in the customer environment.

The product should make this boundary explicit in UI, API, docs, and support
exports. Customers need to know whether an incident is a hosted-control-plane
issue, a cell connectivity issue, a local hardware issue, or a product release
issue.

## Design References

- PlanetScale describes isolation, redundancy, static stability, and
  database-by-database progressive delivery in
  [The principles of extreme fault tolerance](https://planetscale.com/blog/the-principles-of-extreme-fault-tolerance).
- Buildkite documents the hybrid CI pattern of a SaaS control plane with
  customer-hosted agents in
  [Buildkite Pipelines architecture](https://buildkite.com/docs/pipelines/architecture).
- GitHub Actions documents outbound self-hosted runner communication in
  [Self-hosted runners reference](https://docs.github.com/en/actions/reference/runners/self-hosted-runners).
- Redpanda describes managed BYOC clusters that keep data in the customer
  environment in
  [BYOC Architecture](https://docs.redpanda.com/redpanda-cloud/get-started/byoc-arch/).
- Databricks describes customer-managed compute-plane networking in
  [Network reference architecture overview](https://docs.databricks.com/aws/en/security/network/deployment-architecture).
- Aiven describes custom clouds and BYOC operational tradeoffs in
  [Bring your own cloud](https://aiven.io/docs/platform/concepts/byoc).
- OpenShift Hosted Control Planes describes decoupled control planes, data
  planes, and node pools in
  [Control plane architecture](https://docs.redhat.com/en/documentation/openshift_container_platform/4.15/html/architecture/control-plane).
- Google SRE documents canarying and release automation in
  [Canarying Releases](https://sre.google/workbook/canarying-releases/).
