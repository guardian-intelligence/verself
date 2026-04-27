# Durable Execution

The platform adopts [Temporal] as its durable execution plane: the substrate
for workflows that span service boundaries, survive node failure mid-flight,
and need introspection after the fact.

Temporal is one of three async-infrastructure planes being added alongside
[change data capture](change-data-capture.md) and the
[domain event stream](domain-event-stream.md). This document covers
durable execution only.

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
- **Pub/sub fan-out.** That is what the [domain event
  stream](domain-event-stream.md) is for. Temporal is not a broker.
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

## Current deployment

The current deployment is intentionally narrow:

- One loopback-only Temporal cluster on the single-node host.
- One repo-owned `temporal-server` systemd unit running Temporal in
  combined mode.
- One repo-owned `temporal-web` systemd unit for operator access.
- One repo-owned `temporal-bootstrap` command used during deploys and
  live verification to ensure the sandbox and billing namespaces exist.
- Frontend gRPC on `127.0.0.1:7233`, metrics on `127.0.0.1:9001`,
  private membership ports reserved in `src/cue-renderer` for a later split
  into dedicated roles.

The current operator surface is Grafana, Temporal Web,
`make observe WHAT=temporal`, `tdbg`, and SQL against the visibility
database.

## SPIFFE posture

Temporal 1.30.4 does not provide a repo-grade SPIFFE integration out of
the box. Static file-backed TLS is enough for encrypted transport, but it
does not give Workload API-driven certificate management or SPIFFE-based
authorization decisions in the shape the platform needs.

The platform therefore ships a repo-owned wrapper binary,
`verself-temporal-server`, around the upstream server library. It injects:

- Workload API-backed TLS configs via `WithTLSConfigFactory`.
- A claim mapper that extracts the peer SPIFFE URI-SAN into Temporal
  claims.
- A tracing authorizer that maps SPIFFE identities to Temporal system and
  namespace roles.
- Frontend gRPC interceptors that emit explicit mTLS/auth spans into
  ClickHouse.
- A PostgreSQL unix-socket DSN override so Temporal uses the same
  peer-auth posture as the rest of the repo.

Current identities:

```
spiffe://<td>/svc/temporal-server
spiffe://<td>/svc/temporal-web
```

Single-node combined mode deliberately collapses frontend, history,
matching, and worker into `svc/temporal-server`. That keeps the first
brick small and avoids inventing an internal multi-identity story on one
box before the three-node split exists.

One observability wrinkle matters: combined-mode Temporal performs local
startup polls and task-queue work that do not cross the frontend mTLS
boundary. Those authorization spans can appear with an empty
`spiffe.peer_id`. External callers must not. The deployment gate asserts
the external path, not the local bootstrap path.

## Persistence and schema

Temporal persists into two platform-owned PostgreSQL databases:

- `temporal` — core execution history, namespaces, task queues, shards.
- `temporal_visibility` — operator-facing visibility rows.

Both connect over `/var/run/postgresql` with peer auth as role
`temporal`. Password DSNs are prohibited.

The repo also ships a small `temporal-schema` wrapper instead of calling
the upstream schema tool directly. The wrapper reuses the unix-socket DSN
override, emits OTel spans/logs, and keeps schema bootstrap under the
same execution model as the rest of the platform.

Relevant PostgreSQL tables:

- `temporal.namespaces` — namespace registry and IDs. Useful to confirm
  bootstrap and namespace ownership.
- `temporal_visibility.executions_visibility` — the stable operator
  query surface for workflow ID, run ID, workflow type name, task queue,
  status, start time, and close time.

Treat the rest of Temporal's persistence schema as Temporal internals.
Tables such as `history_node`, task queues, and shard state are not a
stable product-facing contract and should not be used for application
logic or long-lived operator dashboards.

## Current 1.30.4 learnings

- SPIFFE works, but not by configuration alone. Owning the wrapper
  binary is the pragmatic path today.
- Server-side mTLS needed explicit `ClientCAs` population. In live
  testing, `VerifyPeerCertificate` alone was not sufficient for a
  fail-closed frontend listener.
- The stock PostgreSQL connection path is not enough for this repo's
  unix-socket peer-auth posture. The DSN builder had to be overridden
  centrally.
- Combined mode opens more idle PostgreSQL connections than a naive
  "single service on one box" reading suggests. SQL pool sizing and role
  connection limits must be tuned together.
- Temporal SDK clients should always set an explicit logger in repo-owned
  tools. The default SDK logger writes to stdout/stderr and can corrupt
  machine-readable command output.
- Workflow type names in visibility depend on the registered name used at
  execution start. Use explicit stable names for operator-facing
  workflows.

## Capacity and current tuning

Temporal's honest unit is **state transitions per second**, not
workflows/sec. Workflow volume alone is misleading; workflow shape varies
substantially.

The current single-node deployment is tuned conservatively:

- `numHistoryShards = 4`
- default persistence pool `maxConns = 20`, `maxIdleConns = 20`
- visibility persistence pool `maxConns = 10`, `maxIdleConns = 10`
- PostgreSQL role connection limit for `temporal` = `80`

Two implications matter for the next stage:

- `numHistoryShards` is immutable after bootstrap. The current value is
  intentionally small because the cluster is still pre-release and can be
  recreated. Revisit it before higher-volume real workflows land.
- PostgreSQL is acceptable for this phase but is still the first scaling
  bottleneck. If durable workflow volume moves materially beyond the
  current use cases, persistence is the first thing to revisit.

## Observability

The current operator surface is ClickHouse-first:

- `make observe WHAT=temporal` for recent auth spans, bootstrap runs,
  logs, and live metric inventory.
- Grafana dashboard `verself-temporal`.
- `default.otel_traces`, `default.otel_logs`, and
  `default.otel_metric_catalog_live` for Temporal traffic and health.
- `temporal_visibility.executions_visibility` for workflow status and
  timing.

This is enough to answer the practical operator questions:

- Is the cluster up?
- Who is calling it?
- Which namespace is active?
- Did namespace bootstrap succeed after restart?

## Verification

`make temporal-smoke-test` is the Temporal deployment gate. It does three
concrete things:

1. Asserts the retired `temporal-proof` binary, `temporal-proof-worker`
   unit, and `verself-temporal-proof` SPIRE entry are absent.
2. Runs `temporal-bootstrap`, restarts `temporal-server`, and runs
   `temporal-bootstrap` again to prove the supported namespace-admin path
   is healthy after restart.
3. Asserts ClickHouse traces/logs/metrics and PostgreSQL namespace rows
   for the supported bootstrap surface.

`make grafana-smoke-test` and `make workload-identity-smoke-test` are the two
supporting gates. Together they verify that Temporal is visible from the
standard operator surface and participates correctly in the repo's SPIFFE
boundary model.

## Current drawbacks and tailwinds

Drawbacks:

- The wrapper binary is now part of the platform contract. Temporal minor
  upgrades need live validation against the custom TLS/authz hooks.
- There is no Temporal Web yet. That keeps the attack surface smaller,
  but it also means Grafana/ClickHouse are the only first-class operator
  UI today.
- Combined mode makes startup noisy. Internal authorize spans with empty
  `spiffe.peer_id` are expected and need to be filtered mentally when
  reading auth traces.
- The current shard count is intentionally disposable. If the cluster
  becomes durable customer infrastructure before that is revisited, the
  repo will have painted itself into a needless migration.

Tailwinds:

- The hard part is solved. Any repo-owned workload can now become a
  Temporal client with the same SPIFFE X.509 client pattern.
- Additional repo-owned clients can reuse the Temporal frontend authz
  path instead of inventing separate trust models for workflow
  orchestration.
- Namespace bootstrap is already instrumented, so deploy-time
  administrative traffic shows up on the same auth/authz and transport
  surfaces as future repo-owned clients.
- Grafana and `make observe` already expose the relevant traces, logs,
  metrics, and visibility rows in one place.

## Source notes

- Workload identity contract: [`workload-identity.md`](workload-identity.md).
- Related planes:
  [`change-data-capture.md`](change-data-capture.md),
  [`domain-event-stream.md`](domain-event-stream.md).
- Implementation references:
  `src/temporal-platform/cmd/verself-temporal-server/main.go`,
  `src/temporal-platform/internal/tlsprovider/tlsprovider.go`,
  `src/temporal-platform/internal/spiffeauth/spiffeauth.go`,
  `src/platform/ansible/roles/temporal/*`,
  `src/platform/scripts/verify-temporal-live.sh`.
- Temporal self-hosted security and mTLS configuration:
  <https://docs.temporal.io/self-hosted-guide/security>.
- Temporal platform documentation: <https://docs.temporal.io/>.
- Temporal persistence backends and version support:
  <https://docs.temporal.io/temporal-service/persistence>.
- Shard count, scaling bottlenecks, and tuning progression:
  <https://temporal.io/blog/scaling-temporal-the-basics>.
- Postgres throughput numbers and DB-CPU ceiling (community benchmark):
  <https://community.temporal.io/t/running-temporal-postgres-benchmark/836>.
