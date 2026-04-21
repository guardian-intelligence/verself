# Durable Execution

The platform adopts [Temporal] as its durable execution plane: the substrate
for workflows that span service boundaries, survive node failure mid-flight,
and need introspection after the fact.

Temporal is one of three async-infrastructure planes being added alongside
[change data capture](change-data-capture.md) and the
[service event bus](event-bus.md). This document covers durable execution
only.

[Temporal]: https://docs.temporal.io/

## What Temporal is

Temporal runs *durable code*. A workflow is a Go (or TypeScript, Python, etc.)
function whose execution state is persisted to storage at every step. If the
process crashes, another worker picks up the workflow and replays its history
to reconstruct in-memory state; it continues from where the crash interrupted
it. A workflow that sleeps for seven days and fires a notification survives
any number of deploys, node reboots, and datacenter incidents in between.

The workflow is deterministic code. The code that touches the world — HTTP
calls, database writes, SDK calls — is factored into *activities*, which
Temporal invokes, retries per policy, and records the result of. Activities
are expected to be idempotent; Temporal's retry machinery assumes it can run
them more than once.

## When to reach for Temporal

- **Multi-step processes that cross service boundaries.** Sandbox
  provisioning: allocate a VM via `vm-orchestrator`, configure network,
  wait for guest boot, register with `sandbox-rental-service`, mark billing
  window open. Any step can fail; compensation for earlier steps must run
  on failure; the whole thing must survive a node bounce mid-provision.
- **Saga patterns.** Anywhere "did step N succeed? If not, undo steps 1..N-1"
  would otherwise be handwritten.
- **Scheduled work with durability guarantees.** `billing-service`'s
  `Reconcile()` today is a goroutine with a ticker; Temporal gives it a
  persistent schedule, structured retries, and a UI to see why a run was
  skipped.
- **Long waits.** Welcome sequences (send now, wait 24h, send follow-up,
  wait 7 days, send reactivation). Approval flows that pause for a webhook
  that may take days.
- **Stripe webhook and other externally-driven flows.** Deduplicate by
  event ID, update ledger, emit domain event, update customer-facing
  status — each step retryable independently, with full history available
  when a customer asks "what happened to my payment."
- **Fan-out / fan-in.** Run N activities in parallel, wait for all, continue.
- **Work that needs introspection.** Temporal's UI answers "where is this
  request, why is it stuck, what was the last error" for free. When the
  alternative is adding structured logging and a status table in Postgres
  to every multi-step process, Temporal is cheaper.

## When not to reach for Temporal

- **Hot-path request handling.** Workflow overhead is milliseconds-scale.
  If it fits in a single HTTP request and doesn't need to survive a crash,
  do it inline.
- **Pub/sub fan-out.** That is what the [event bus](event-bus.md) is for.
  Temporal is not a broker.
- **Streaming or continuous transformation.** That is what
  [change data capture](change-data-capture.md) and ClickHouse
  materialized views are for.
- **Intra-service transactional background jobs.** [River Queue] is the
  right tool: the job is enqueued in the same Postgres transaction as the
  business state change, so commit/rollback keeps the job and the state in
  sync. Temporal cannot do transactional enqueue against a service's own
  database; it operates one layer up.
- **Simple cron on a single service.** A systemd timer or a River
  periodic job is less machinery.
- **Replacing a database for CRUD state.** Workflows are not rows.
  Temporal's history is an execution log, not a query surface.
- **Real-time work with sub-100ms SLOs.** Temporal trades latency for
  durability. If a request blocks on a workflow completing, you will feel
  it.

Rule of thumb: reach for Temporal when the process has more than one step,
the steps cross a failure domain (service, network, external provider),
and you would otherwise be writing a state table plus a retry loop plus a
cron plus structured logging. Don't reach for it when any one of those is
false.

[River Queue]: https://riverqueue.com/

## Relationship to River

River remains, inside each service, for transactional intra-service jobs.
Temporal operates at the inter-service layer, where transactional enqueue
across service boundaries is impossible by construction. The two are
complementary, not competing. A Temporal activity in a given service may
itself enqueue a River job inside that service — River is how the service
does its local transactional work; Temporal is how the process crosses the
service boundary.

## SPIFFE posture

Temporal does not speak SPIFFE natively. It accepts file-backed X.509
certificates for both its internode mTLS (between the frontend, history,
matching, and worker roles) and its frontend mTLS (for clients). The
platform wraps it with the `spiffe-helper` pattern already in production
use for ClickHouse: the helper fetches X.509-SVIDs from the SPIRE
Workload API, writes them to disk, and signals the Temporal process on
rotation.

Identity shape:

```
spiffe://<td>/svc/temporal-frontend
spiffe://<td>/svc/temporal-history
spiffe://<td>/svc/temporal-matching
spiffe://<td>/svc/temporal-worker
```

For the single-node topology these collapse to a single `svc/temporal`
systemd unit running `temporal-server` in combined mode. Identities are
declared separately so the three-node split does not require an identity
migration.

A custom authorizer plugin maps the peer's SPIFFE URI-SAN to a Temporal
namespace. Without this, any workload with a cert from the trust bundle
could hit any namespace; with it, `sandbox-rental-service` may only write
to the `sandbox` namespace, `billing-service` only to `billing`, etc.

Persistence is on the platform Postgres via local Unix socket peer auth
with `pg_ident.conf` mapping — same invariant as every other
repo-owned service. Password DSNs are prohibited.

The standard SVID TTL and fail-closed startup semantics from
[`workload-identity.md`](workload-identity.md) apply: 1h X.509 rotation,
30s startup timeout, readiness flip at `TTL − 2m` if refresh stalls.

## Observability

`make observe WHAT=temporal` surfaces:

- Frontend, history, and matching systemd state; per-role SVID TTL.
- Active task queue backlog, workflow success/failure counts per
  namespace, bucketed by caller SPIFFE ID.
- mTLS edge table rows for `temporal-*` peers.

Grafana receives one dashboard under
`src/platform/ansible/roles/grafana/dashboards/temporal.json`. Panels:
frontend gRPC p99 latency, active workflows by namespace, SVID TTL
remaining per role, internode mTLS errors.

## Proof artifact

Per the repo's output contract, the brick is not laid until a ClickHouse
trace query returns green. Temporal ships with a `ProofHeartbeat`
workflow registered as `spiffe://<td>/svc/temporal-proof`, invoked by
`make telemetry-proof-temporal`. The proof query asserts:

- An `auth.spiffe.mtls.server` span exists with
  `spiffe.peer_id = svc/temporal-proof`.
- All internal hops in the workflow's span tree carry `spiffe.peer_id`.
- A `governance.audit.append` row exists with
  `actor_spiffe_id = svc/temporal-proof`.
- The span tree resolves end-to-end without a missing parent.

## First brick rationale

Temporal is built and validated before the other two planes. It gates
PeerDB (which uses Temporal for its own workflow orchestration) and is
the hardest of the three: two independent TLS configs, stateful across
restarts, multi-role, and a custom authorizer plugin. Establishing the
SPIFFE-wrapped stateful-infrastructure pattern on Temporal makes the
event bus and CDC ports trivial.

Temporal also carries its own standalone value. Scheduled work,
saga-pattern retries, and the billing `Reconcile()` orchestration unlock
the day Temporal is healthy, whether or not PeerDB and NATS ever land.

## Known unknowns

The implementing agent must answer these before the brick is considered
laid:

1. Does `temporal-server` hot-reload TLS material on signal, or only on
   restart? Determines whether 1h SVID rotation is seamless or incurs a
   short window of unavailability per cycle.
2. Can `temporal-server` be run directly on bare metal under systemd
   with `spiffe-helper` sidecars, or does the custom authorizer plugin
   force us to build our own binary embedding the upstream server?
   `server-tools.json` and `go.work` need a concrete answer.
3. Temporal's `WithAuthorizer` plugin API stability — pin a minor and
   maintain the shim.
4. Namespace-per-caller vs shared-namespace authorization. Lean toward
   per-caller for cleaner SPIFFE-to-namespace mapping.
5. Operator CLI access — `/ops/admin-cli` reuse for `temporal` CLI, or a
   dedicated `/ops/temporal-cli` identity.

## Source notes

- Workload identity contract: [`workload-identity.md`](workload-identity.md).
- Related planes:
  [`change-data-capture.md`](change-data-capture.md),
  [`event-bus.md`](event-bus.md).
- Temporal self-hosted security and mTLS configuration:
  <https://docs.temporal.io/self-hosted-guide/security>.
- Temporal platform documentation: <https://docs.temporal.io/>.
- Existing spiffe-helper integration pattern:
  `src/platform/ansible/roles/clickhouse/templates/clickhouse-operator-spiffe-helper.*`.
