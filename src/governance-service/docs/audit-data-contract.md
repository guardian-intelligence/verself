# Governance Audit Data Contract

`governance-service` owns the organization data export surface and the
customer-visible audit query surface. Product services still own their operation
catalogs, authorization checks, and resource ownership checks; they emit
governance audit rows after making those decisions. Zitadel remains the identity
provider, while Forge Metal owns product policy and API credential metadata as
described in [identity-and-iam.md](../../platform/docs/identity-and-iam.md).

The canonical implementation writes the schema in
[019_governance_audit_events.up.sql](../../platform/migrations/019_governance_audit_events.up.sql)
from [audit.go](../internal/governance/audit.go). Customer-facing surfaces call
the caller an `Actor`, not a `Principal`; tenant scope is captured separately as
`org_id`.

## Product Surfaces

- Organization audit log: owner/admin UI with filters for time, actor, target,
  operation, result, operation type, risk, source product area, and location.
- High-risk activity view: default audit landing view, filtered to
  `risk_level IN ('high', 'critical')` and writes/errors.
- Data export: org owner/admin generated `tar.gz` artifact with
  `manifest.json`, JSON Lines for normalized records, CSV where business users
  expect spreadsheets, and PDFs where source systems produce canonical billing
  documents.
- SIEM/export feed: same canonical rows as the UI, with raw service names,
  trace IDs, HMAC chain fields, and stable operation IDs retained for machines.
- Secrets audit: OpenBao native audit devices remain enabled, but
  `secrets-service` emits the Forge Metal row used for cross-product search,
  policy debugging, data export, and billing correlation. Secret material is
  never copied into governance rows. See
  [secrets-service.md](../../platform/docs/secrets-service.md).

## Combined Event Shape

Every audited operation records who did what, to which target, from where, under
which policy, and with what result. Static classification comes from the
service-owned operation catalog; request facts come from the authenticated
request boundary; result facts come from the handler or integration.

| Field family | Fields |
| --- | --- |
| Event identity | `schema_version`, `event_id`, `recorded_at`, `event_date`, `org_id`, `environment`, `source_product_area`, `service_name`, `service_version`, `writer_instance_id` |
| Traceability | `request_id`, `trace_id`, `span_id`, `parent_span_id`, `route_template`, `http_method`, `http_status`, `duration_ms`, `idempotency_key_hash` |
| Actor | `actor_type`, `actor_id`, `actor_display`, `actor_org_id`, `actor_owner_id`, `actor_owner_display`, `credential_id`, `credential_name`, `credential_fingerprint`, `auth_method`, `auth_assurance_level`, `mfa_present`, `session_id_hash`, `delegation_chain`, `actor_spiffe_id` |
| Operation | `operation_id`, `audit_event`, `operation_display`, `operation_type`, `event_category`, `risk_level`, `data_classification`, `rate_limit_class` |
| Target | `target_kind`, `target_id`, `target_display`, `target_scope`, `target_path_hash`, `resource_owner_org_id`, `resource_region` |
| Authorization | `permission`, `action`, `org_scope`, `policy_id`, `policy_version`, `policy_hash`, `matched_rule`, `decision`, `result`, `denial_reason`, `trust_class` |
| Request network | `client_ip`, `client_ip_version`, `client_ip_hash`, `ip_chain`, `ip_chain_trusted_hops`, `user_agent_raw`, `user_agent_hash`, `referer_origin`, `origin`, `host`, `tls_subject_hash`, `mtls_subject_hash` |
| Location/enrichment | `geo_country`, `geo_region`, `geo_city`, `asn`, `asn_org`, `network_type`, `geo_source`, `geo_source_version` |
| Change/result | `changed_fields`, `before_hash`, `after_hash`, `content_sha256`, `artifact_sha256`, `artifact_bytes`, `error_code`, `error_class`, `error_message` |
| Secrets-specific | `secret_mount`, `secret_path_hash`, `secret_version`, `secret_operation`, `lease_id_hash`, `lease_ttl_seconds`, `key_id`, `openbao_request_id`, `openbao_accessor_hash` |
| Integrity | `sequence`, `prev_hmac`, `row_hmac`, `hmac_key_id`, `ingested_at`, `retention_class`, `legal_hold` |

`operation_type` is one of `read`, `write`, `delete`, `authn`, `authz`,
`export`, `system`, or `unknown`. Billing is an `event_category` and
`source_product_area`, not an operation type. `risk_level` is
operation-catalog metadata, not a runtime guess: `low`, `medium`, `high`, or
`critical`.
High-risk operations include credential creation/roll/revoke, permission
changes, org membership changes, data export creation/download, payment/billing
changes, cryptographic key use/rotation, secret reads/writes, workload
execution, and privileged operator actions.

`source_product_area` is the customer label, such as `Governance`, `Identity`,
`Billing`, `Sandbox`, or `Secrets`. `service_name` is retained for debugging,
exports, and SIEM correlation, but the UI should not lead with internal service
names.

## Actor Model

The actor is the exact entity that authenticated and performed the operation.
It can be a human user, API credential, workload, internal service, or operator
break-glass subject. The organization is tenant scope, not the actor.

For API keys and API credentials, the actor is the credential/customer
service account itself. The human or automation that created the credential is
recorded as owner metadata:

- `actor_type = 'api_credential'`
- `actor_id = <Zitadel customer/API credential subject or Forge Metal credential id>`
- `credential_id`, `credential_name`, and `credential_fingerprint` identify the
  credential without storing secret material.
- `actor_owner_id` and `actor_owner_display` identify the user or system that
  owns the credential when known.

For repo-owned workload actors, SPIFFE is the workload identity source of
truth:

- `actor_type = 'service'`
- `actor_id = <service-name>`
- `actor_spiffe_id = spiffe://<trust-domain>/svc/<service-name>`
- `auth_method = 'spiffe'`

Repo-owned service-to-service calls use SPIFFE/SPIRE, not Zitadel
service-account client credentials. `actor_spiffe_id` is the forensic join key
between app-layer audit rows, mTLS spans, and OpenBao JWT-SVID login evidence.

Human actors use the Zitadel subject as `actor_id`. Email and display names are
display metadata; they may be redacted or tombstoned later without changing the
immutable actor id recorded for the audit event.

## Collection Rules

- Product services emit one governance row for every public operation whose
  operation catalog declares an audit event. Denials are recorded before the
  handler runs; allowed/error results are recorded after the handler returns.
- Static fields come from the service-owned operation catalog:
  `permission`, `operation_id`, `operation_type`, `risk_level`,
  `event_category`, `target_kind`, `action`, `org_scope`, idempotency policy,
  and rate-limit class.
- Actor and credential fields come from the validated JWT plus Forge
  Metal-owned API credential metadata. Human OAuth scopes are not product
  permissions.
- Request network fields are captured at the service boundary from trusted edge
  headers only. `X-Forwarded-For` is accepted only from Caddy or a configured
  trusted proxy path; untrusted client-supplied chains are retained only as
  untrusted evidence.
- Geolocation and ASN are enriched locally from a pinned database version after
  request handling. No request-path callout to an external geolocation service.
- Store `geo_country` and `geo_region` by default. `geo_city` is allowed but
  should remain empty unless needed for enterprise security workflows. Never
  store latitude/longitude in audit rows.
- `user_agent_raw` is capped and sanitized; `user_agent_hash` supports grouping
  without rendering the full value. IP addresses and user agents are personal
  data in many jurisdictions, so UI and exports must treat them as restricted
  audit data.
- Change details use field names and hashes by default. Full before/after
  values are allowed only when the resource is already safe for audit storage.
- Secrets rows record mount, path hash, version, lease, key, and content hashes.
  Secret values, plaintext request bodies, private keys, raw accessors, and
  OpenBao raw request/response bodies are not governance audit data.

## Data Use

The first UI should answer incident-response questions quickly:

`Time | Risk | Actor | Operation | Target | Result | Location | Source`

The default view is high-risk and recent. Admins can widen the query to all
activity, filter reads vs writes, inspect exact policy decisions, and jump from
an audit event to the matching OpenTelemetry trace via `trace_id`.

The export artifact is for customers, auditors, and incident response. It is
not a direct database dump. It includes stable public records, manifests,
checksums, and source-specific canonical files. It excludes secrets, raw tokens,
credential hashes when they can be abused, provider webhook payloads, payment
method details, River internals, and short-lived runner bootstrap material. The
current export storage schema is in
[001_governance_schema.up.sql](../migrations/001_governance_schema.up.sql), and
the first-pass artifact builder is in
[export.go](../internal/governance/export.go).

## Retention and Privacy Decisions

- Governance audit rows are security records. They are append-only and are not
  edited to satisfy profile or preference updates.
- Display fields such as email and names can be tombstoned or redacted in
  derived views and future exports. Immutable ids, event times, operation facts,
  policy decisions, hashes, and HMAC chain fields remain until the audit
  retention period expires.
- The default product posture is at least one year of queryable audit history,
  with enterprise legal hold and extended retention as policy settings. GitHub's
  enterprise audit log is a useful lower bound at 180 days, but Forge Metal's
  self-hosted ClickHouse posture makes longer retention cheap enough to be the
  default.
- Partition deletion must preserve tamper evidence by keeping chain checkpoints
  per organization and retention window. A deleted partition is represented by
  an auditable retention event, not silent disappearance.
- Audit rows are included in organization exports for the requesting
  organization. A personal data request is a separate product flow because it
  cuts across organization membership, identity profile data, and audit records
  where the user appears as an actor or target.

## Never Store

- Raw API keys, bearer tokens, refresh tokens, session cookies, passwords, SSH
  private keys, signing keys, or OpenBao secret values.
- Full query strings when they can contain tokens or user content.
- Full request/response bodies by default.
- Payment card numbers, bank details, or provider payloads containing payment
  method data.
- Raw DSNs, database passwords, webhook secrets, or repository checkout tokens.
- Precise location coordinates or continuous device identifiers.

## Verification Gates

Every schema or collection change must ship with live evidence in ClickHouse:

- A successful high-risk write has one governance row with the expected
  `actor_*`, `target_*`, `operation_type`, `risk_level`, `client_ip`,
  geolocation, authorization, and HMAC fields.
- A denied request records the actor, requested permission, matched policy rule
  or denial reason, and no handler-side mutation.
- An API credential request records `actor_type = 'api_credential'`,
  credential metadata, and owner metadata without storing secret material.
- A data export create/download sequence records export job rows in Postgres,
  audit rows in ClickHouse, and OpenTelemetry spans in one trace chain:
  public API span, policy enforcement, export build/download, audit record.
- A secrets create/read/rotate/delete sequence records both OpenBao audit output
  and the normalized governance row, with content/path hashes and no secret
  value leakage.
- A reconciliation job verifies per-org `sequence`, `prev_hmac`, `row_hmac`,
  row count continuity, partition checkpoints, and export manifest checksums.

## Source Notes

- AWS CloudTrail records event time, service/source, event name, identity,
  source IP, user agent, request ID, event ID, read-only classification,
  resources, category, and error details:
  <https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-event-reference-record-contents.html>.
- Google Cloud AuditLog separates service/method/resource, authentication,
  authorization, policy violation, and request metadata, including caller IP and
  user agent:
  <https://docs.cloud.google.com/logging/docs/reference/audit/auditlog/rest/Shared.Types/AuditLog>.
- GitHub Enterprise audit logs expose actor, affected user, repository/org,
  action, country, SAML/SCIM identity, auth method, optional source IP, export,
  API, and streaming surfaces:
  <https://docs.github.com/en/enterprise-cloud@latest/admin/concepts/security-and-compliance/audit-log-for-an-enterprise>.
- OWASP's Logging Cheat Sheet calls for consistent application logging, logging
  of authentication/authorization failures and high-risk functions, "when,
  where, who and what" event attributes, and explicit exclusion or masking of
  tokens, passwords, keys, payment data, and sensitive personal data:
  <https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html>.
- GDPR Recital 30 treats IP addresses and similar online identifiers as data
  that can identify or profile natural persons when combined with other server
  data: <https://gdpr-info.eu/recitals/no-30/>.
- OpenBao audit devices log request/response interactions, recommend multiple
  devices, hash most sensitive string values with HMAC-SHA256, and can block
  requests when no audit device can record them:
  <https://openbao.org/docs/audit/>.
- NIST SP 800-53 Rev. 5 is the reference control catalog for audit and
  accountability controls such as AU-2, AU-3, AU-6, AU-9, and AU-11:
  <https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final>.
