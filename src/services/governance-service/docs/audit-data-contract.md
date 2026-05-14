# Governance API Activity Data Contract

`governance-service` owns the customer-visible audit query and organization
data export surfaces. Product services own operation catalogs, authorization
checks, and resource ownership checks. They send typed API activity facts to the
internal governance endpoint after the enforcement boundary reaches a decision.
Governance converts those facts into OCSF API Activity events through the single
typed builder in
[internal/ocsf](../internal/ocsf), backed by `github.com/telophasehq/go-ocsf`
schema structs, and persists the resulting envelope.

Tenant scope is `org_id`. The authenticated subject is projected to
`actor.user.uid` or `actor.app_uid` depending on principal kind. Credential
material is never an authorization subject; credential identifiers appear only
as `actor.user.credential_uid` metadata.

## OCSF Shape

The canonical class is OCSF API Activity:

| Field | Verself value |
| --- | --- |
| `class_uid`, `class_name` | `6003`, `API Activity` |
| `activity_id` | `1` create, `2` read, `3` update, `4` delete, `99` other |
| `actor` | Human subject, service account, workload principal, or repo service workload |
| `api` | `service.name`, operation uid, operation name, optional version |
| `resources` | Organization, project, secret, run, data export, or domain resource names |
| `http_request` | Method, route, selected safe query parameters, request uid, user agent |
| `http_response` | HTTP status code |
| `status_id`, `status` | `1` success or `2` failure |
| `status_code` | HTTP status or stable Verself problem code |
| `unmapped.trace` | OpenTelemetry trace uid and span uid. The current Go OCSF `v1_5_0` API Activity struct does not expose the Trace profile as a class field. |
| `unmapped` | Namespaced Verself details such as idempotency key hashes |

Large audit systems keep searchable identifiers in a narrow row and place large
request-specific context behind a structured payload boundary. AWS CloudTrail,
Google Cloud Audit Logs, and OCSF all separate event class identity, actor,
resource, activity, status, time, request metadata, and provider extensions.
Verself follows that split with ClickHouse sort keys selected for tenant/time
queries and per-organization HMAC verification.

## Tables

### `verself.api_activity_events`

Hot query table. One row is appended for each OCSF API Activity event:

| Column | Purpose |
| --- | --- |
| `time`, `event_date` | UTC event time and partition date. |
| `metadata_uid` | OCSF event uid. |
| `org_id` | Tenant boundary and first sort key. |
| `sequence` | Per-organization monotonic sequence. |
| `ocsf_version`, `category_uid`, `class_uid`, `type_uid` | OCSF class identity. |
| `activity_id`, `activity_name` | OCSF API activity. |
| `action_id`, `action` | Authorization decision projected as OCSF action. |
| `status_id`, `status`, `status_code` | Success/failure and stable status code. |
| `api_service`, `api_operation` | Service and operation identifiers. |
| `actor_type`, `actor_uid`, `credential_uid` | Authenticated subject and credential metadata. |
| `primary_resource_type`, `primary_resource_uid`, `primary_resource_full_name` | Main authorized resource. |
| `permission` | Permission checked at the enforcement boundary. |
| `http_method`, `http_route`, `http_response_code` | HTTP request/response projection. |
| `trace_uid`, `span_uid` | OpenTelemetry join keys. |
| `ocsf_sha256` | Hash of the canonical OCSF JSON payload. |
| `prev_hmac`, `row_hmac`, `hmac_key_id` | Per-organization tamper-evidence chain. |

The table is ordered by `(org_id, event_date, api_service, status_id, time,
sequence, metadata_uid)`. Low-cardinality columns lead the sort key for
compression, while tenant/date/time locality keeps organization queries and
chain verification contiguous.

### `verself.api_activity_payloads`

Canonical OCSF payload table. One row is appended for each API Activity event:

| Column | Purpose |
| --- | --- |
| `metadata_uid` | Joins to `api_activity_events.metadata_uid`. |
| `org_id`, `event_date` | Tenant/date locality for export joins. |
| `ocsf_json` | Canonical OCSF API Activity JSON, ZSTD compressed. |
| `ocsf_sha256` | Hash checked against the hot row. |

### `verself.api_activity_resources`

Resource projection table. One row is appended for each resource in the OCSF
payload:

| Column | Purpose |
| --- | --- |
| `metadata_uid` | Joins to `api_activity_events.metadata_uid`. |
| `resource_index` | Resource order from the canonical payload. |
| `resource_type`, `resource_uid`, `resource_name`, `resource_full_name` | Searchable resource identifiers. |

## Write Path

1. The API wrapper authenticates the caller and derives actor, organization,
   credential, request, response, status, and trace facts.
2. Product services send `APIActivityRecord` to
   `POST /internal/v1/ocsf/api-activities` through the governance internal
   transport client.
3. Governance validates the typed facts, builds the OCSF API Activity event,
   assigns the next organization sequence, computes `ocsf_sha256`, and computes
   the HMAC chain.
4. Governance stores the pending row in Postgres outbox state.
5. The projector inserts hot rows, payload rows, and resource rows into
   ClickHouse using `batch.AppendStruct`. The Postgres outbox row is marked
   projected only after every ClickHouse projection is present.

ClickHouse rows are append-only. Corrections are represented by new API
Activity events.

## Read Path

The console, CLI, and SDK call
`GET /api/v1/governance/ocsf/api-activities`. Supported filters are exact
predicates on:

- `actor_uid`
- `actor_type`
- `api_service`
- `api_operation`
- `activity_id`
- `credential_uid`
- `resource_uid`
- `resource_type`
- `status_id`
- `status_code`
- `trace_uid`

Pagination is cursor based. The API returns the hot row fields only. Full OCSF
payloads are available through data export and forensic tooling.

## Export Path

Organization exports read `api_activity_events`, join `api_activity_payloads`, and emit
`governance/ocsf_api_activities.jsonl`. Each JSONL row contains the chain envelope
and the canonical OCSF payload. Export manifests include row counts, byte
counts, and file hashes.

## Actor Model

Actors are typed subjects:

- `user`: Zitadel human subject.
- `service_account`: non-human customer subject used by API keys or other
  customer credentials.
- `workload`: customer workload subject.
- `service_workload`: repo-owned internal service authenticated by SPIFFE.

API keys, private keys, client secrets, and session cookies are authenticators.
They are not authorization subjects. Product services check the typed actor
against permissions and record the credential identifier only as API Activity
metadata.

Repo-owned service-to-service calls use SPIFFE/SPIRE. The internal governance
append endpoint records the caller from the peer SPIFFE identity and includes
the peer ID under `unmapped.verself.spiffe_id`.

## Verification Gates

Every audited API change must produce live evidence:

- A successful public operation inserts one `api_activity_events` row with
  `class_uid = 6003`, expected actor, resource, permission, status, trace,
  `ocsf_sha256`, and HMAC fields.
- A denied request records the requested permission and actor without executing
  the handler mutation.
- An API credential request records the service account actor and credential uid
  without recording secret material.
- A data export records the export job in Postgres, OCSF API Activity rows in
  ClickHouse, and trace spans joined by `trace_uid`.
- A reconciliation query verifies per-organization `sequence`, `prev_hmac`,
  `row_hmac`, row count continuity, payload hash integrity, and export manifest
  checksums.
