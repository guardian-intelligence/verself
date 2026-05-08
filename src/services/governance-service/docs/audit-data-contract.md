# Governance Audit Data Contract

`governance-service` owns the customer-visible audit query and organization
data export surfaces. Product services own operation catalogs, authorization
checks, and resource ownership checks. They emit governance audit records after
the enforcement boundary reaches a decision.

The canonical implementation is in
[audit.go](../internal/governance/audit.go), with ClickHouse storage declared in
[001_initial_schema.up.sql](../../../infrastructure-components/clickhouse/migrations/001_initial_schema.up.sql).
Tenant scope is `org_id`. The authenticated subject is `actor_type` and
`actor_id`. Credential material is never an authorization subject; credentials
authenticate an actor and appear only as `credential_id` metadata.

## Reference Shape

Large audit systems keep searchable identifiers in a narrow row and place large
request-specific context behind a structured payload boundary:

- AWS CloudTrail records `eventTime`, `eventSource`, `eventName`,
  `userIdentity`, `resources`, `errorCode`, `requestID`, and `eventID`, with
  provider-specific request and response sections outside the core identity of
  the event:
  <https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-event-reference-record-contents.html>.
- Google Cloud Audit Logs use `serviceName`, `methodName`, `resourceName`,
  `authenticationInfo`, `authorizationInfo`, `requestMetadata`, and `status`,
  with service-specific metadata separated from the stable envelope:
  <https://cloud.google.com/logging/docs/reference/audit/auditlog/rest/Shared.Types/AuditLog>.
- OCSF separates event class identity, actor, resource, activity, status, time,
  metadata, and extension attributes:
  <https://schema.ocsf.io/>.

Verself follows the same separation, with ClickHouse-specific compression and
sort-key choices.

## Tables

### `verself.audit_events`

Hot query table. One row is appended for each audited operation:

| Column | Purpose |
| --- | --- |
| `recorded_at`, `event_date` | UTC event time and partition date. |
| `schema_version` | Audit schema version for export readers. |
| `event_id` | UUID for the immutable audit row. |
| `org_id` | Tenant boundary and first sort key. |
| `sequence` | Per-organization monotonic sequence. |
| `event_source` | Service or system that recorded the event. |
| `event_name` | Stable operation name, usually the Huma operation ID. |
| `audit_event` | Stable domain event name. |
| `actor_type`, `actor_id` | Authenticated subject. |
| `credential_id` | Credential used by the actor, when present. |
| `target_type`, `target_id` | Authorized resource type and safe identifier. |
| `permission` | Permission checked at the enforcement boundary. |
| `outcome` | `allowed`, `denied`, or `error`. |
| `error_code` | Stable problem code for failed operations. |
| `trace_id` | OpenTelemetry trace join key. |
| `detail_sha256` | Hash of the optional detail payload. |
| `prev_hmac`, `row_hmac`, `hmac_key_id` | Per-organization tamper-evidence chain. |

The table is ordered by `(org_id, event_date, sequence, event_id)`. This makes
the common organization/time query read contiguous ranges, keeps HMAC
verification ordered, and compresses low-cardinality fields before high-cardinal
identifiers.

### `verself.audit_event_details`

Cold detail table. One row is appended only when an event has structured
context that should not be promoted into the hot search row:

| Column | Purpose |
| --- | --- |
| `event_id` | Joins to `audit_events.event_id`. |
| `org_id`, `event_date` | Tenant/date locality for export joins. |
| `detail_json` | Canonical JSON payload, ZSTD compressed. |
| `detail_sha256` | Hash checked against the hot row. |

Details are domain context such as idempotency key hashes, credential
fingerprints, content hashes, changed field names, OpenBao request IDs, export
artifact hashes, or billing reservation identifiers. Details never contain raw
tokens, API keys, bearer credentials, session cookies, passwords, private keys,
OpenBao secret values, full request bodies, or payment method data.

## Write Path

1. The API wrapper authenticates the caller and derives `actor_type`,
   `actor_id`, `org_id`, and optional `credential_id`.
2. The service-owned policy wrapper checks the required permission and records
   `permission`, `event_source`, `event_name`, `audit_event`, target, and
   outcome.
3. The governance appender stores the canonical row in Postgres outbox state.
4. The projector computes `detail_sha256`, assigns the next organization
   sequence, computes the HMAC chain, and inserts into ClickHouse with
   `batch.AppendStruct`.
5. If detail exists, the projector inserts the detail row in the same projection
   attempt. The Postgres outbox row is marked projected only after both
   ClickHouse writes are present.

ClickHouse rows are append-only. Corrections are represented by new audit
events, never updates.

## Read Path

The console, CLI, and SDK call `GET /api/v1/governance/audit/events`.
Supported filters are exact predicates on:

- `actor_id`
- `audit_event`
- `credential_id`
- `event_name`
- `event_source`
- `outcome`
- `target_id`
- `target_type`

Pagination is cursor based. The API returns the hot row fields only. Detail
payloads are for data export and forensic tooling, where a caller can verify
`detail_sha256` against the hot row.

## Export Path

Organization exports read the hot rows and left join detail rows by
`org_id`, `event_date`, and `event_id`. The JSONL export contains the same
canonical fields as the API plus `detail` when a detail row exists. Export
manifests include row counts, byte counts, and file hashes.

## Actor Model

Actors are typed subjects:

- `user`: Zitadel human subject.
- `service_account`: non-human customer subject used by API keys or other
  customer credentials.
- `workload`: customer workload subject.
- `service`: repo-owned internal service authenticated by SPIFFE.

API keys, private keys, client secrets, and session cookies are authenticators.
They are not authorization subjects. Product services check the typed actor
against permissions and record the credential identifier only as audit metadata.

Repo-owned service-to-service calls use SPIFFE/SPIRE. The internal governance
append endpoint records the caller as `actor_type = service` or `workload`
derived from the peer SPIFFE identity and includes the peer ID in the detail
payload.

## Verification Gates

Every IAM collection change must produce live evidence:

- A successful public operation inserts one `audit_events` row with expected
  actor, target, permission, outcome, trace, detail hash, and HMAC fields.
- A denied request records the requested permission and actor without executing
  the handler mutation.
- An API credential request records the service account actor and credential ID
  without recording secret material.
- A data export records the export job in Postgres, the audit row in
  ClickHouse, and trace spans joined by `trace_id`.
- A reconciliation query verifies per-organization `sequence`, `prev_hmac`,
  `row_hmac`, row count continuity, detail hash integrity, and export manifest
  checksums.
