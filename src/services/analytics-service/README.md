# Analytics Service

`analytics-service` is Verself's OpenTelemetry-compatible ingestion and query service for append-only analytics events. It accepts standard OTLP payloads, applies Verself policy at the service boundary, stamps organization/project/dataset identity, and stores queryable wide events in ClickHouse.

The current write path is private to trusted producers while the economics, governance, retention, and organization-scoped query model are proven. The read path is a public bearer-auth API guarded by IAM resource checks on `analytics_dataset` resources.

## Mental model

Every observation is a labeled event.

```ts
analytics.recordEvent("build.typecheck", {
  "build.tool": "typescript",
  "build.package": "//src/viteplus-monorepo/packages/brand",
  "build.config_path": "src/viteplus-monorepo/packages/brand/tsconfig.json",
  "cache.result": "hit",
  "duration_ms": 184,
  "status": "succeeded",
})
```

The same model applies to product analytics:

```ts
analytics.recordEvent("page.view", {
  "page.path": "/pricing",
  "page.referrer": "https://github.com/",
  "utm.source": "github",
})
```

An event is an immutable fact with a name, timestamp, resource context, trace context when available, and typed attributes. Dashboards, funnels, latency charts, traces, logs, and build-performance reports are different read models over the same append-only facts.

## OpenTelemetry compatibility

The wire protocol is OTLP/HTTP. The first private implementation accepts OTLP Logs for `build.*` events:

```text
POST /v1/logs
```

The primary event path uses OTLP Logs. OpenTelemetry models events as log records with event names, attributes, resource context, and optional trace/span correlation. `recordEvent` is SDK ergonomics over that standard payload shape.

Duration-bearing events may also be represented as spans when causal trace views are useful. Numeric event attributes can be aggregated into metrics by ClickHouse read models. Native OTLP Traces and Metrics should be added only when the dataset policy allows their cardinality and temporality.

References:

- OTLP protocol: <https://opentelemetry.io/docs/specs/otlp/>
- OpenTelemetry logs data model: <https://opentelemetry.io/docs/specs/otel/logs/data-model/>
- OpenTelemetry event semantic conventions: <https://opentelemetry.io/docs/specs/semconv/general/events/>
- W3C trace context: <https://www.w3.org/TR/trace-context/>

## Service boundary

`analytics-service` is the product boundary for telemetry ingestion. It does the work that cannot be delegated to a raw collector endpoint:

- authenticates the producer
- applies CORS and origin policy for browser-safe write keys
- maps credentials to organization, project, dataset, and environment
- overwrites reserved Verself attributes
- validates event names, attribute keys, value types, and payload sizes
- enforces quotas and rate limits
- records accepted and rejected event counts
- writes canonical rows to ClickHouse
- emits its own OTLP telemetry about ingestion behavior

Client-supplied organization identity is never trusted. A client may describe the application or package that emitted an event. The service stamps Verself-owned identity from the credential.

## Credential classes

There are two ingest credential classes.

| Credential | Intended use | Properties |
|---|---|---|
| Public write key | Browser and other untrusted clients | Insert-only, origin-bound, dataset-bound, strict payload limits, no read access |
| Private ingest token | CI, servers, internal tools, trusted automation | Insert-only, stronger authentication, richer resource context, stricter accountability |

Public write keys are safe to embed in browser code because they cannot query data, select arbitrary organizations, or write outside their configured dataset. They are still abuse surfaces, so origin checks, rate limits, event-name policy, and payload caps are mandatory.

Private ingest tokens are used by build tooling and services. For example, Verself CI can emit TypeScript, Vite/Rolldown, Bazel, Go, and Zig build telemetry without teaching the runner lifecycle about those tools.

The deployed CI ingestion path currently uses GitHub Actions OIDC for the Verself monorepo dataset. Browser write keys and customer private ingest tokens use the same dataset model when those producer classes are enabled.

## Event names

Event names identify the kind of fact. They should be stable and low-cardinality.

Good event names:

```text
page.view
signup.started
checkout.completed
build.typecheck
build.bundle
deploy.completed
durable_volume.promoted
```

Variable data belongs in attributes:

```text
build.package = //src/viteplus-monorepo/packages/brand
deployment.environment.name = prod
http.route = /api/v1/projects/{project_id}
```

Event names should not contain user IDs, request IDs, package paths, timestamps, commit SHAs, or random suffixes.

## Attributes

Attributes are typed key-value pairs. The admitted queryable subset is intentionally small:

- string
- boolean
- signed integer
- unsigned integer
- floating-point number

Nested JSON is not queryable event structure. Producers should flatten useful dimensions into explicit attributes.

Recommended naming:

```text
build.tool
build.package
build.config_path
cache.result
duration_ms
page.path
user.id_hash
session.id
deployment.environment.name
```

Reserved prefixes:

```text
analytics.*
verself.*
```

`analytics.*` and `verself.*` attributes are owned by the service. If a payload includes them, the service replaces them with values derived from the credential or rejects the record according to dataset policy.

## Canonical storage

ClickHouse stores one canonical wide-event table. Exact column names are implementation details, but the data shape is:

```text
observed_at
timestamp
org_id
project_id
dataset_id
environment
source_kind
event_name
signal_kind
trace_id
span_id
parent_span_id
service_name
status
severity
duration_ms
body
body_redaction_status
string_attributes
int_attributes
float_attributes
bool_attributes
```

Access and ingestion evidence are stored separately:

```text
verself.analytics_ingest_events
verself.analytics_access_events
```

The canonical event row is append-only. Product-specific projections are reproducible from `verself.analytics_events`.

## Browser analytics

Browser analytics uses the same event model with browser-safe policy.

The browser SDK should:

- batch events
- flush on page hide and visibility changes
- include W3C trace context when available
- avoid collecting raw secrets, cookies, auth headers, or full DOM state
- respect configured consent hooks
- avoid high-cardinality event names
- retry only when the OTLP response indicates retryable failure

The browser SDK should not require customers to understand OpenTelemetry. It exposes `recordEvent`, `identify`, and `flush`. Advanced users can point standard OTel web instrumentation at the OTLP endpoint when the dataset policy allows it.

## Build telemetry

Build tooling is a first internal use case.

Build-tool integrations should live at the build-tool boundary and emit generic build labels:

```text
event_name = build.typecheck
build.tool = typescript
build.package = @verself/brand
build.config_path = packages/brand/tsconfig.json
cache.source = native-tool-cache
cache.result = hit
duration_ms = 184
status = succeeded
```

Tool-specific facts remain attributes under tool-owned prefixes. Vite/Rolldown, TypeScript, Go, Zig, and Bazel should use their native timing and cache surfaces. The runner service and analytics service should not encode language-specific behavior. They provide execution, authentication, policy, and storage for events emitted by the build boundary.

## Responses

The service follows OTLP response semantics.

Full success:

```text
HTTP 200
partial_success unset
```

Partial success:

```text
HTTP 200
partial_success.rejected_log_records = <count>
partial_success.error_message = "..."
```

Bad data:

```text
HTTP 400
```

Rate limited:

```text
HTTP 429
Retry-After: <seconds>
```

Temporary service failure:

```text
HTTP 503
Retry-After: <seconds>
```

Clients should not retry partial-success responses. They should retry only retryable failures and should drop invalid telemetry after recording local diagnostics.

## Privacy and governance

The first public version should collect less data than it technically can.

Default policy:

- no raw IP address in canonical event rows
- no request headers except an explicit allowlist
- no cookies
- no bearer tokens
- no DOM snapshots
- bounded string lengths
- bounded attribute count
- bounded batch size
- short raw retention
- longer aggregate retention
- dataset-level retention policy
- dataset-level allowed-origin policy

PII and user identifiers should be explicit. Browser SDKs should support anonymous IDs, session IDs, and hashed product user IDs without requiring raw email addresses or names.

## Query model

The first query surfaces should answer product and engineering questions directly:

```sql
SELECT
  event_name,
  count() AS events
FROM verself.analytics_events
WHERE org_id = {org_id:String}
  AND project_id = {project_id:String}
  AND dataset_id = {dataset_id:String}
  AND timestamp >= now() - INTERVAL 1 DAY
GROUP BY event_name
ORDER BY events DESC;
```

Build telemetry:

```sql
SELECT
  string_attributes['build.package'] AS package,
  quantile(0.95)(duration_ms) AS p95_ms,
  count() AS runs
FROM verself.analytics_events
WHERE event_name = 'build.typecheck'
  AND string_attributes['build.tool'] = 'typescript'
GROUP BY package
ORDER BY p95_ms DESC;
```

Product analytics:

```sql
SELECT
  string_attributes['page.path'] AS path,
  count() AS views
FROM verself.analytics_events
WHERE event_name = 'page.view'
GROUP BY path
ORDER BY views DESC;
```

Customer-facing querying goes through service APIs and typed read models before direct ClickHouse access is considered. ClickHouse row policies and quotas are useful defense-in-depth, but `analytics-service` remains the authorization and governance boundary.

## Deployment stance

Initial deployment is private:

- internal Verself datasets only
- GitHub Actions OIDC write path
- bearer-auth query API
- IAM resource checks on `analytics_dataset`
- no customer-facing UI
- explicit allowlist of producers
- ClickHouse evidence for accepted, rejected, denied, and allowed records

The API is still designed for public use. Keeping the public shape early prevents internal-only shortcuts from becoming the architecture.
