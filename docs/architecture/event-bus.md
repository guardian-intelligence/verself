# Service Event Bus

The platform adopts [NATS JetStream] as its service event bus: the pub/sub
and work-queue substrate for cross-service domain events, starting with
`notifications-service`.

The event bus is one of three async-infrastructure planes being added
alongside [durable execution](durable-execution.md) and
[change data capture](change-data-capture.md). This document covers the
event bus only.

[NATS JetStream]: https://docs.nats.io/nats-concepts/jetstream

## What NATS JetStream is

NATS is a lightweight messaging system; JetStream is its persistent
streaming layer. A single Go binary runs the broker. Streams are durable
append logs keyed by subject pattern; consumers are either push or pull,
with at-least-once semantics and configurable retention (limits,
interest, or work-queue). Apache 2.0 licensed.

Two features matter to us beyond basic pub/sub:

- **Native delayed message delivery** (NATS 2.12+). A publish can carry a
  schedule; the broker holds it until the delivery time. This removes the
  need for a side-channel scheduler for time-delayed notifications.
- **JetStream KV** — a key/value store backed by a stream. Suitable for
  idempotency keys, user-preference cache, and consumer position
  snapshots. Collapses what would otherwise be an extra Redis dependency.

## What the event bus enables

`notifications-service` is the first consumer. Services emit domain
events (e.g. `billing.invoice.paid`, `sandbox.attempt.finalized`) to
subjects under the `events.*` hierarchy; `notifications-service`
subscribes, matches against templates and user preferences, and fans out
to providers (Resend for email, Twilio for SMS, APNs/FCM for push).

The same bus is the substrate for future cross-service event-driven
patterns: analytics enrichment pipelines, audit fan-out for
`governance-service`, and any other "when X happens in service A, do Y
in service B" flow that does not need Temporal's durability-of-execution
guarantees.

## What the event bus is not

- **Not Temporal.** JetStream has durable storage; it does not run durable
  code. Multi-step orchestration belongs on [durable
  execution](durable-execution.md).
- **Not Kafka.** The [durable execution](durable-execution.md) and
  [CDC](change-data-capture.md) planes already cover the two use cases
  that would otherwise drive Kafka adoption. The Kafka protocol buys
  nothing on top of what JetStream provides here, and costs a
  multi-component stack (brokers + Connect + registry) on bare metal.
- **Not River.** River remains inside each service for transactional
  intra-service jobs. JetStream is the inter-service bus; River is the
  intra-service queue. `notifications-service` runs River internally for
  per-provider retry and scheduling at the moment of delivery, while
  subscribing to JetStream for the inbound event stream.

## SPIFFE posture

NATS does not speak SPIFFE natively. [Upstream integration is
tracked][issue1928] but has not landed. It is wrapped by the
`spiffe-helper` pattern already in production use for ClickHouse: certs
and trust bundle rendered to disk, `nats-server` reloaded on rotation.

Identity shape:

```
spiffe://<td>/svc/nats
spiffe://<td>/svc/notifications-service
```

Per-client subject authorization is driven off the SPIFFE URI-SAN in the
client certificate. A publishing service is allowed to publish only to
subjects under its own service prefix (e.g. `events.billing.*` for
`svc/billing-service`); `notifications-service` subscribes under
`events.>` with a bounded consumer group.

Standard invariants apply: no shared bearer tokens, no static passwords.
SVID TTL and fail-closed startup semantics from
[`workload-identity.md`](workload-identity.md) carry through unchanged.

[issue1928]: https://github.com/nats-io/nats-server/issues/1928

## Observability

`make observe WHAT=nats` surfaces:

- `nats-server` systemd state, SVID TTL.
- JetStream stream state: bytes, messages, consumers per stream.
- Per-subject authorization failures.
- Consumer lag per subject pattern.

Grafana receives one dashboard under
`src/platform/ansible/roles/grafana/dashboards/nats.json`.

## Proof artifact

NATS ships with a publish/subscribe proof that asserts:

- A publish under an authorized subject succeeds; a publish under an
  unauthorized subject fails with the expected permission error.
- A message persists across a broker restart (JetStream durability).
- The message's trace propagates to the subscriber with
  `spiffe.peer_id` attributes on both ends.

The proof is invoked by `make telemetry-proof-nats`. The brick is not
laid until the query returns green.

## Known unknowns

The implementing agent must answer these before the brick is considered
laid:

1. Does `nats-server --signal reload` re-read TLS material in-process, or
   does rotation require a full restart?
2. Stream retention and storage sizing for the single-node deployment.
   Default disk budget per stream, default `max_age`, cleanup policy per
   subject hierarchy.
3. Subject taxonomy — a fixed scheme (`events.<domain>.<aggregate>.<event>`)
   versus per-service freedom under a service prefix. Favor the fixed
   scheme for grep-ability, but confirm against early consumer needs.
4. JetStream KV boundaries — what legitimately belongs there
   (idempotency, consumer cursors) versus what belongs in a service's
   own Postgres.

## Source notes

- Workload identity contract: [`workload-identity.md`](workload-identity.md).
- Related planes:
  [`durable-execution.md`](durable-execution.md),
  [`change-data-capture.md`](change-data-capture.md).
- NATS JetStream documentation: <https://docs.nats.io/nats-concepts/jetstream>.
- JetStream message scheduler: <https://www.synadia.com/blog/delayed-message-scheduling-nats-jetstream>.
- NATS + SPIRE integration tracking:
  <https://github.com/nats-io/nats-server/issues/1928>.
