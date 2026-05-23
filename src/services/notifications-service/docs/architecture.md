# Notifications Service Architecture

`notifications-service` is the product notification plane. It receives
notification intents, evaluates delivery policy for the target recipient, stores
the in-app inbox projection, emits append-only evidence, and delivers through
configured channels.

Alert evaluation, runner state machines, billing state, source workflow state,
and observability queries remain owned by their producing services. Those
systems send notification intents to `notifications-service` after they have
classified an event as user-visible.

## HTTP API

The HTTP API is the authenticated human inbox API described by the Smithy models
under `src/smithy/models/verself/notifications.smithy`, projected to OpenAPI
through official Smithy tooling, and implemented by handwritten Huma routes in
`internal/api/routes.go`.

| Method | Path | Behavior |
| --- | --- | --- |
| `GET` | `/api/v1/notifications` | List current human notifications, ordered by per-recipient sequence descending. |
| `GET` | `/api/v1/notifications/summary` | Return unread count, latest sequence, read cursor, preferences, and latest notification. |
| `PUT` | `/api/v1/notifications/preferences` | Replace the current subject preference state using optimistic versioning. |
| `POST` | `/api/v1/notifications/read-cursor` | Advance the read cursor up to a supplied sequence. |
| `POST` | `/api/v1/notifications/{notification_id}/read` | Mark one notification read. |
| `POST` | `/api/v1/notifications/{notification_id}/dismiss` | Dismiss one notification. |
| `POST` | `/api/v1/notifications/clear` | Dismiss all current notifications for the subject. |
| `POST` | `/api/v1/notifications/test` | Publish a synthetic test event for the current subject. |

Authentication is a Zitadel OIDC access token for a human subject. The service
rejects API credentials for human inbox routes and requires the Zitadel generic
project roles claim as the current human-token discriminator. Each operation's
permission, resource, rate-limit class, audit event name, product area, risk
level, and data classification belong in Smithy operation traits and the
generated route catalog consumed by service-runtime policy.

Mutating routes require `Idempotency-Key`. Request bodies for the small mutation
routes are capped at 16 KiB. The process-level request cap is 1 MiB. The inbox
keeps a per-recipient ring buffer of 999 notifications.

## Current Event Intake

The service also consumes the platform domain-event stream:

- NATS JetStream stream: `DOMAIN_EVENTS`.
- Subject pattern: `events.>`.
- Consumer durable: `notifications-service`.
- Message content type:
  `application/vnd.verself.domain-event+json;version=1`.
- Retention: JetStream file storage with a 7-day `MaxAge`.

Current `DomainEvent` fields:

| Field | Meaning |
| --- | --- |
| `event_id` | Producer-generated UUID used as the NATS message id and Postgres primary-key component. |
| `event_source` | Producer or transport source name. Defaults to `nats` when omitted. |
| `subject` | Event subject, normalized under `events.*` for publish. |
| `org_id` | Tenant boundary for the notification. |
| `actor_subject_id` | Optional actor who caused the event. |
| `recipient_subject_id` | Human subject receiving the inbox row. |
| `dedupe_key` | Globally unique notification dedupe key. |
| `kind` | Product-level notification kind. |
| `priority` | `low`, `normal`, or `high`. |
| `title` / `body` | Rendered user-facing copy. |
| `action_url` | Optional console URL. |
| `resource_kind` / `resource_id` | Product resource identity. |
| `payload` | Structured producer payload. |
| `traceparent` | W3C trace context for async continuation. |

The current pipeline persists `notification_events`, enqueues River fanout work,
checks the recipient preference record, creates `user_notifications`, and
projects append-only ledger rows into ClickHouse table
`verself.notification_events`. Preference evaluation currently supports a single
global enabled flag per `(org_id, subject_id)`.

## Internal Workflow API

The internal API follows a workflow-trigger shape. Callers trigger a named
workflow. The notification service owns templates, preferences, recipient
expansion, channel selection, dedupe, suppression, delivery attempts, and
evidence. The wire contract is the Smithy internal projection for
`TriggerNotificationWorkflow`.

```http
POST /internal/v1/workflows/{workflow_key}/trigger
Idempotency-Key: runner:workflow-failed:<workflow-run-id>
```

```json
{
  "org_id": "org_123",
  "recipients": [{"subject_id": "user_456"}],
  "title": "Workflow failed",
  "body": "guardian-intelligence/verself CI failed",
  "priority": "high",
  "targetResourceName": "urn:verself:inst_01H...:orgs/org_123/source/workflow-runs/018f...",
  "data": {
    "repository": "guardian-intelligence/verself",
    "workflow": "ci",
    "status": "failed"
  },
  "traceparent": "00-e3a..."
}
```

Response:

```json
{
  "workflow_run_id": "018f..."
}
```

### Request Model

| Field | Requirement |
| --- | --- |
| `workflow_key` | Stable product workflow key such as `runner.workflow_failed` or `platform.alert_firing`. |
| `org_id` | Required Verself org id. This is the tenant boundary used for preferences and governance. |
| `recipients` | Required recipient records. Initial recipients can carry `subject_id` or `email`. |
| `title` / `body` | Rendered notification copy accepted by the workflow. |
| `priority` | Optional `low`, `normal`, or `high`; omitted values use service defaults. |
| `targetResourceName` | Optional Verself resource name for the related product object. |
| `data` | Workflow data document persisted as producer context. |
| `traceparent` | Optional W3C trace context; also accepted from the HTTP header. |

Producers never choose SMTP providers, inbox rows, or notification templates.

## API Design References

The design follows these external APIs:

- [Knock workflow trigger API](https://docs.knock.app/send-notifications/triggering-workflows/api):
  `key`, `recipients`, `actor`, `tenant`, `data`, and `cancellation_key`.
- [Knock workflow API reference](https://docs.knock.app/api-reference/workflows/trigger):
  asynchronous workflow runs through a trigger endpoint.
- [Knock workflow cancellation reference](https://docs.knock.app/api-reference/workflows/cancel):
  cancellation by `cancellation_key`.
- [Knock preferences overview](https://docs.knock.app/preferences/overview):
  preferences evaluated by workflow, category, channel, and channel type.
- [Grafana contact points](https://grafana.com/docs/grafana/latest/alerting/fundamentals/notifications/contact-points/):
  alert notifications routed through contact-point integrations, including
  webhook.
- [Grafana file provisioning](https://grafana.com/docs/grafana/latest/alerting/set-up/provision-alerting-resources/file-provisioning/):
  alert rules, contact points, notification policies, mute timings, and
  templates managed as version-controlled files.
- Email-service internal send API: outbound email request shape, provider
  idempotency, provider failover, and provider rate-limit handling live behind
  `email-service`.

Slack-style incoming webhooks inform the Grafana adapter's low-friction
ingestion shape. The product notification API follows Knock because Verself
needs tenant scoping, recipient expansion, preferences, in-app feed state, and
multi-channel delivery.

## Grafana Integration

Grafana should evaluate alert rules directly against ClickHouse and send alert
notifications to a webhook contact point. The contact point calls a narrow
adapter:

```http
POST /internal/v1/integrations/grafana/alerts
Authorization: Bearer <grafana-notifications-webhook-token>
```

The adapter accepts Grafana's native webhook payload, validates labels and
annotations, and triggers notification workflows internally:

- `platform.alert_firing`
- `platform.alert_resolved`

Required Grafana labels for customer-routed alerts:

| Label | Requirement |
| --- | --- |
| `verself_org_id` | Target org id. |
| `verself_notification_topic` | Notification topic, for example `runner.workflow` or `platform.alert`. |
| `severity` | `info`, `warning`, or `critical`. |
| `resource_kind` | Product resource kind. |
| `resource_id` | Product resource id. |

The adapter rejects alerts missing routing labels. Platform-owned alerts can be
bound to the `platform-admin` org and configured to notify
`integrations.anveio@gmail.com` through a verified org contact.

Grafana's webhook token is a scoped integration credential in the component
credstore. It can trigger only the Grafana integration endpoint and only the
allowed alert workflows. Grafana does not receive database credentials beyond
its existing read-only ClickHouse datasource.

## Delivery Policy Model

Delivery policy is the notification-service decision graph applied after a
producer submits a workflow trigger:

1. Validate workflow key and data schema.
2. Resolve tenant and recipient identifiers.
3. Expand recipient groups such as org roles or resource subscribers.
4. Load recipient preferences.
5. Evaluate workflow, category, channel, and channel-type preferences.
6. Enforce topic and severity thresholds.
7. Apply dedupe and cancellation keys.
8. Choose delivery channels: in-app first, email next, webhook or chat later.
9. Persist notification and delivery attempt state.
10. Project append-only ClickHouse evidence.

The initial preference schema can remain compact:

```text
notification_preferences
  org_id
  subject_id
  version
  enabled
  topics_json
  channels_json
```

`enabled=false` suppresses all non-mandatory workflows for the subject.
Mandatory security and billing workflows require an explicit workflow-level
policy marker and should be rare.

Email recipients are resolved from verified notification contacts. Producers
submit `contact:<contact_id>` or semantic recipients such as an org role; they
do not submit arbitrary email addresses in notification intents.

## Data Model Direction

The current Postgres model remains the foundation:

- `notification_inbox_state` owns per-recipient sequence and read cursor.
- `notification_preferences` owns mutable preference state.
- `notification_events` owns accepted producer facts and dedupe keys.
- `user_notifications` owns in-app inbox rows.
- `notification_projection_queue` owns ClickHouse projection retries.
- River tables own background fanout and projection jobs.

The workflow-trigger cutover should add:

- `notification_workflows`: workflow key, category, schema, mandatory marker,
  default channels.
- `notification_contacts`: org-owned contact points such as email addresses.
- `notification_contact_verifications`: verification state for external
  addresses.
- `notification_subscriptions`: org role, resource, or subject subscriptions to
  topics.
- `notification_delivery_attempts`: per-channel send attempts, provider message
  ids, status, error class, and retry schedule.
- `notification_suppression_events`: explicit evidence for preference, dedupe,
  rate-limit, and policy suppression.

ClickHouse keeps the append-only ledger. Postgres owns current state and
transactional queues.

## TPS Limits

These are single-node safety budgets for the current Nomad shape: two service
allocations, `VERSELF_PG_MAX_CONNS=8` per allocation, River fanout worker count
4 per allocation, projection worker count 2 per allocation, NATS fetch batches
of 32, and ClickHouse projection claims of 100 rows per job. They are admission
targets until live ClickHouse evidence replaces them.

| Surface | Initial sustained cap | Burst cap | Notes |
| --- | ---: | ---: | --- |
| Human inbox reads | 100 req/s per installation | 300 req/s | Mostly indexed Postgres reads by `(org_id, recipient_subject_id)`. |
| Human inbox mutations | 25 req/s per installation | 100 req/s | Each mutation writes Postgres and projection queue rows. |
| Internal workflow triggers, in-app only | 100 triggers/s per installation | 500 triggers/s for 30s | One trigger expands to one workflow run per recipient. Initial request limit should cap recipients at 100. |
| Internal workflow triggers per org | 10 triggers/s | 50 triggers/s for 30s | Prevents one tenant from consuming the single-node queue. |
| Grafana alert webhook | 10 payloads/s | 60 payloads/min | Grafana should group related alerts before webhook delivery. |
| Email delivery handoff | 20 sends/s | 100 sends/s for 30s | Notifications hands accepted workflow decisions to `email-service`; provider-specific rate limits and retries are owned there. |
| ClickHouse projection | 40 rows/s maintenance floor | 200 rows/s during event-triggered scans | Projection workers claim 100 rows/job. Alert when projection lag exceeds 60s. |

Recipient fanout is the main multiplicative factor. A workflow trigger with 100
recipients can create 100 inbox rows and 100 delivery decisions. High-fanout
announcements should use a separate broadcast API with explicit batching,
unsubscribe, and quota semantics.

Backpressure should occur in this order:

1. Per-org and per-service trigger rate limits reject at the API boundary.
2. Queue-depth admission rejects before Postgres/River lag becomes unbounded.
3. Email workers pause on provider `429` or `retry-after`.
4. Projection workers alert on lag and keep retrying from Postgres state.

## Security Model

Human inbox routes use Zitadel bearer tokens and service-runtime authorization.
The subject and org come from the validated token, not from request bodies.
Human routes reject API credential subjects because API credentials do not own
human inboxes.

Internal workflow triggers use SPIFFE mTLS and service-local typed clients.
Each producer service is authorized for an allowlist of workflow keys and, when
needed, recipient forms. Example:

| Caller SPIFFE id | Allowed workflows |
| --- | --- |
| `spiffe://spiffe.verself.sh/svc/sandbox-rental-service` | `runner.*`, `sandbox.execution_*` |
| `spiffe://spiffe.verself.sh/svc/source-code-hosting-service` | `source.workflow_*` |
| `spiffe://spiffe.verself.sh/svc/billing-service` | `billing.*` |
| `spiffe://spiffe.verself.sh/svc/alerting-service` | `platform.alert_*`, `platform.canary_*` |

Grafana's webhook integration uses a scoped bearer token because Grafana contact
points are configured through webhook headers. The token is stored in credstore,
rotated through the same secret distribution path as component credentials, and
accepted only on `/internal/v1/integrations/grafana/alerts`.

Notification payloads are controller data. Producers must send stable resource
ids, trace ids, workflow names, and short summaries. Secrets, bearer tokens,
provider installation tokens, webhook signatures, and raw logs are excluded from
notification data. Action URLs point to authenticated console pages; they do not
carry authorization material.

Idempotency is required for all producer-triggered workflows. The
`Idempotency-Key` header scopes retry safety for the API call. `dedupe_key` or
`cancellation_key` scopes product-level duplicate suppression for a workflow
run. Email sends also pass provider idempotency keys through `email-service`.

## Governance Model

Every accepted trigger creates durable evidence:

- Postgres accepted-event row with event id, workflow key, tenant, recipient,
  dedupe key, content hash, resource identity, and trace context.
- Postgres inbox row or suppression event for each recipient decision.
- Postgres delivery-attempt row for each channel attempt.
- ClickHouse ledger rows for event accepted, suppressed, inbox created, delivery
  attempted, delivery succeeded, delivery failed, read, dismissed, and pruned.
- OTel spans for trigger acceptance, fanout, preference evaluation, provider
  delivery, and ClickHouse projection.

Route metadata declares the IAM permission, audit event name, operation display,
risk level, and data classification. Governance audit rows should use the same
operation ids and include actor identity, tenant, target resource, idempotency
key hash, and trace id.

Retention is split by purpose:

- NATS stream: 7 days for at-least-once async intake.
- Human inbox: latest 999 notifications per recipient.
- Postgres workflow, suppression, and delivery-attempt state: operational
  retention according to the service data-retention policy.
- ClickHouse notification ledger: append-only analytical and forensic evidence.

The platform org uses the same product model as customers. `platform-admin`
owns alert workflows and verified contacts; `integrations.anveio@gmail.com` is
an org contact, not a hard-coded operator email.

## Completion Evidence For The Cutover

The first implementation slice is complete when live evidence shows:

1. A sandbox-rental runner failure triggers `runner.workflow_failed`.
2. The workflow creates an in-app notification for the configured user/contact.
3. The workflow sends email through the verified contact path.
4. Preference disablement suppresses a non-mandatory workflow and records a
   suppression event.
5. Grafana fires a provisioned ClickHouse alert into the Grafana adapter.
6. ClickHouse contains the expected trigger, suppression or inbox, delivery, and
   projection rows with the same trace id.

Unit tests are useful for schema validation and permission allowlists. The
acceptance gate is the live ClickHouse trace and ledger sequence for the
customer-visible workflow.
