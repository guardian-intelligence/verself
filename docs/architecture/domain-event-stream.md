# Domain Event Stream

The platform adopts [NATS JetStream] as its domain event stream: durable
pub/sub for cross-service domain facts. Services publish immutable facts
such as `billing.invoice.paid` or `sandbox.attempt.finalized`; downstream
services react asynchronously. This plane is for fan-out only. It is not
orchestration, CDC, or intra-service transactional work.

The domain event stream is one of three async-infrastructure planes
alongside [durable execution](durable-execution.md) and
[change data capture](change-data-capture.md). This document covers the
domain event stream only.

[NATS JetStream]: https://docs.nats.io/nats-concepts/jetstream

## Boundary

- Use this plane when a service needs to publish a fact and should not
  wait for downstream reactions.
- Use this plane when there may be zero, one, or many consumers with
  independent retry policy and independent failure modes.
- Consumers must be idempotent and tolerate at-least-once delivery,
  duplicates, reordering, and delay.
- A consumer being down must not invalidate the producer's primary
  transaction.

If any of those are false, this is probably the wrong abstraction.

## First consumer

`notifications-service` is the first consumer. Services publish domain
events to subjects under `events.*`; `notifications-service` subscribes,
matches templates and user preferences, and fans out to providers.

## Not This Plane

- Multi-step workflows, compensation, long waits, and "where is this
  request stuck?" introspection belong on [durable
  execution](durable-execution.md).
- Postgres→ClickHouse replication belongs on [change data
  capture](change-data-capture.md).
- Intra-service transactional background work belongs in River inside the
  owning service.
- If exactly one downstream service must act synchronously for
  correctness, call it directly over the service API instead of
  publishing an event.

## Why JetStream

JetStream provides durable streams and consumers with at-least-once
delivery in a single broker binary. That is enough for the narrow job
defined here: publish a domain fact once, let independent consumers react
later, survive broker restart, and expose lag and authorization failures
as first-class signals.

## SPIFFE posture

NATS does not speak SPIFFE natively. [Upstream integration is
tracked][issue1928] but has not landed. It is wrapped by the
`spiffe-helper` pattern used for file-backed consumers: certs and trust
bundle are rendered to disk, and `spiffe-helper` signals `nats-server`
with `SIGHUP` through the server PID file when material renews. NATS
reloads configuration on `SIGHUP`, which avoids dropping JetStream state
or disconnecting clients for routine SVID rotation.

Identity shape:

```
spiffe://<td>/svc/nats
spiffe://<td>/svc/notifications-service
```

Per-client subject authorization is driven off the SPIFFE URI-SAN in the
client certificate. A publishing service is allowed to publish only to
its permitted `events.*` subjects; consumers receive only the subject
patterns they own.

Standard invariants apply: no shared bearer tokens, no static passwords.
SVID TTL and fail-closed startup semantics from
[`workload-identity.md`](workload-identity.md) carry through unchanged.

[issue1928]: https://github.com/nats-io/nats-server/issues/1928

## Observability

`aspect observe --what=nats` surfaces:

- `nats-server` systemd state, SVID TTL.
- JetStream stream state: bytes, messages, consumers per stream.
- Per-subject authorization failures.
- Consumer lag per subject pattern.

Grafana receives one dashboard under
`src/platform/ansible/roles/grafana/dashboards/nats.json`.

## Live verification

Live evidence asserts the NATS rotation contract: no legacy restart watcher
units exist, `spiffe-helper` is
configured with `pid_file_name` plus `renew_signal = "SIGHUP"`, and
`systemctl reload nats.service` leaves the broker PID stable while the
health endpoint remains ready. The check should emit
`workload_identity.rotation.*` spans and assert them in ClickHouse.

A NATS publish/subscribe canary (authorized vs. unauthorized subjects,
JetStream durability across broker restart, trace propagation with
`spiffe.peer_id` on both peers) is not yet implemented; when added it
should assert via spans rather than via Ansible self-checks.

## Source notes

- Workload identity contract: [`workload-identity.md`](workload-identity.md).
- Related planes:
  [`durable-execution.md`](durable-execution.md),
  [`change-data-capture.md`](change-data-capture.md).
- NATS JetStream documentation: <https://docs.nats.io/nats-concepts/jetstream>.
- NATS signal handling:
  <https://docs.nats.io/running-a-nats-service/nats_admin/signals>.
- JetStream message scheduler: <https://www.synadia.com/blog/delayed-message-scheduling-nats-jetstream>.
- NATS + SPIRE integration tracking:
  <https://github.com/nats-io/nats-server/issues/1928>.
- SPIFFE Helper:
  <https://github.com/spiffe/spiffe-helper/tree/v0.11.0>.
