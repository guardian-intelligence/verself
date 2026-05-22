# Deployment Observability

Deployment evidence starts with the `deploy_run_key`. `verself-deploy` emits
that key as an OpenTelemetry resource attribute on deploy spans, Bazel
translation spans, and Nomad CLI job-run spans. Raw ClickHouse queries should
filter on `ResourceAttributes['verself.deploy_run_key']`; service logs and
Nomad observer rows are usually joined by the deployment time window and job
IDs instead of relying on per-job mutable metadata.

## Operator Queries

Use `aspect observe` before raw SQL. It wraps the common ClickHouse joins and
keeps the query vocabulary discoverable:

```shell
aspect observe --what=deploy --minutes=120
aspect observe --what=deploy --run-key=<deploy_run_key>
aspect observe --what=trace --trace-id=<trace_id>
aspect observe --what=errors --minutes=30
aspect observe --what=service --service=<service> --minutes=30
aspect observe --what=metric --metric=<metric> --minutes=30
```

`aspect observe --what=deploy --run-key=<deploy_run_key>` returns the deploy
timeline, Nomad CLI job-run events, Nomad observer rows for submitted jobs,
Bazel codegen actions, and rebuild blast-radius evidence.

Use the catalog when the needed signal name is unknown:

```shell
aspect observe
aspect observe --what=queries
aspect observe --what=catalog --signal=deploys
aspect observe --what=catalog --signal=traces --service=verself-deploy
aspect observe --what=catalog --signal=logs --service=<service>
aspect observe --what=catalog --signal=metrics --service=<service>
```

## Raw Tables

The raw OTel tables are:

| Signal | Table | Time column | Primary filters |
| --- | --- | --- | --- |
| Traces | `default.otel_traces` | `Timestamp` | `ServiceName`, `SpanName`, `TraceId`, `ResourceAttributes`, `SpanAttributes` |
| Logs | `default.otel_logs` | `Timestamp` | `ServiceName`, `TraceId`, `ResourceAttributes`, `LogAttributes`, `Body` |
| Sum metrics | `default.otel_metrics_sum` | `TimeUnix` | `ServiceName`, `MetricName`, `Attributes` |
| Gauge metrics | `default.otel_metrics_gauge` | `TimeUnix` | `ServiceName`, `MetricName`, `Attributes` |
| Histograms | `default.otel_metrics_histogram` | `TimeUnix` | `ServiceName`, `MetricName`, `Attributes` |
| Exponential histograms | `default.otel_metrics_exponential_histogram` | `TimeUnix` | `ServiceName`, `MetricName`, `Attributes` |
| HTTP access | `default.http_access_logs` | `Timestamp` | `Host`, `Status`, `Path`, `TraceId` |

Inspect schema drift before writing a one-off query:

```shell
aspect db ch schemas
```

## Deploy Runs

List recent deploy run keys:

```shell
aspect db ch query --query="
SELECT
  ResourceAttributes['verself.deploy_run_key'] AS deploy_run_key,
  ResourceAttributes['verself.site'] AS site,
  SpanAttributes['verself.deploy_sha'] AS sha,
  StatusCode AS status,
  toUInt64OrZero(SpanAttributes['verself.submitted_job_count']) AS submitted_jobs,
  min(Timestamp) AS started_at,
  max(Timestamp + toIntervalNanosecond(Duration)) AS ended_at,
  any(TraceId) AS trace_id,
  left(any(StatusMessage), 180) AS error
FROM default.otel_traces
WHERE ServiceName = 'verself-deploy'
  AND SpanName = 'verself_deploy.run'
  AND Timestamp >= now() - toIntervalHour(24)
GROUP BY deploy_run_key, site, sha, status, submitted_jobs
ORDER BY started_at DESC
LIMIT 20"
```

Pull the timeline for one deploy:

```shell
aspect db ch query --query="
SELECT
  Timestamp,
  ServiceName,
  SpanName,
  StatusCode,
  round(Duration / 1000000, 2) AS duration_ms,
  if(
    SpanAttributes['nomad.job_id'] != '',
    SpanAttributes['nomad.job_id'],
    if(SpanAttributes['bazel.target_label'] != '', SpanAttributes['bazel.target_label'], SpanAttributes['verself.submitted_jobs'])
  ) AS item,
  TraceId,
  SpanId,
  left(StatusMessage, 180) AS error
FROM default.otel_traces
WHERE ResourceAttributes['verself.deploy_run_key'] = '<deploy_run_key>'
  AND ServiceName IN ('verself-deploy', 'bazel')
ORDER BY Timestamp, ServiceName, SpanName
LIMIT 500"
```

## Nomad Job Runs

Nomad CLI submission evidence is stored as span events on
`verself_deploy.nomad.job_run` spans:

```shell
aspect db ch query --query="
SELECT
  event_time,
  event_name,
  event_attrs['nomad.job_id'] AS job_id,
  event_attrs['verself.deploy_wave'] AS wave,
  event_attrs['verself.spec_sha256'] AS spec_sha256,
  toUInt64OrZero(event_attrs['verself.duration_ms']) AS duration_ms,
  left(multiIf(
    event_name = 'verself.nomad.run_failed', event_attrs['nomad.stderr'],
    event_name = 'verself.nomad.run_succeeded', event_attrs['nomad.stdout'],
    ''
  ), 240) AS output,
  left(event_attrs['error.message'], 180) AS error
FROM default.otel_traces
ARRAY JOIN Events.Timestamp AS event_time, Events.Name AS event_name, Events.Attributes AS event_attrs
WHERE ResourceAttributes['verself.deploy_run_key'] = '<deploy_run_key>'
  AND ServiceName = 'verself-deploy'
  AND event_name IN ('verself.nomad.run_started', 'verself.nomad.run_succeeded', 'verself.nomad.run_failed')
ORDER BY event_time, job_id, event_name
LIMIT 500"
```

Nomad observer logs are joined by the deploy span's time bounds and the job IDs
submitted by that deploy:

```shell
aspect db ch query --query="
WITH
  deploy AS
  (
    SELECT
      Timestamp AS started_at,
      Timestamp + toIntervalNanosecond(Duration) AS ended_at,
      JSONExtract(SpanAttributes['verself.submitted_jobs'], 'Array(String)') AS submitted_jobs
    FROM default.otel_traces
    WHERE ResourceAttributes['verself.deploy_run_key'] = '<deploy_run_key>'
      AND ServiceName = 'verself-deploy'
      AND SpanName = 'verself_deploy.run'
    ORDER BY Timestamp DESC
    LIMIT 1
  )
SELECT
  l.Timestamp,
  l.LogAttributes['nomad.event_name'] AS event,
  l.LogAttributes['nomad.event.type'] AS event_type,
  l.LogAttributes['nomad.job_id'] AS job_id,
  l.LogAttributes['nomad.alloc.client_status'] AS alloc_status,
  l.LogAttributes['nomad.deployment.status'] AS deployment_status,
  l.LogAttributes['nomad.eval.status'] AS eval_status,
  l.LogAttributes['nomad.alloc_id'] AS alloc_id,
  l.TraceId,
  left(l.Body, 240) AS message
FROM default.otel_logs AS l
CROSS JOIN deploy AS d
WHERE l.ServiceName = 'nomad-observer'
  AND l.Timestamp >= d.started_at - toIntervalSecond(5)
  AND l.Timestamp <= d.ended_at + toIntervalMinute(2)
  AND has(d.submitted_jobs, l.LogAttributes['nomad.job_id'])
ORDER BY l.Timestamp, event, job_id
LIMIT 500"
```

## Service Logs

Use the deployment window to inspect logs for a service that changed during a
deploy:

```shell
aspect db ch query --query="
WITH deploy AS
(
  SELECT
    min(Timestamp) AS started_at,
    max(Timestamp + toIntervalNanosecond(Duration)) AS ended_at
  FROM default.otel_traces
  WHERE ResourceAttributes['verself.deploy_run_key'] = '<deploy_run_key>'
)
SELECT
  l.Timestamp,
  l.ServiceName,
  l.SeverityText,
  l.TraceId,
  l.SpanId,
  left(l.Body, 320) AS message
FROM default.otel_logs AS l
CROSS JOIN deploy AS d
WHERE l.ServiceName = '<service>'
  AND l.Timestamp >= d.started_at - toIntervalMinute(2)
  AND l.Timestamp <= d.ended_at + toIntervalMinute(10)
ORDER BY l.Timestamp DESC
LIMIT 200"
```

For HTTP-facing symptoms, query the normalized access projection:

```shell
aspect db ch query --query="
WITH deploy AS
(
  SELECT
    min(Timestamp) AS started_at,
    max(Timestamp + toIntervalNanosecond(Duration)) AS ended_at
  FROM default.otel_traces
  WHERE ResourceAttributes['verself.deploy_run_key'] = '<deploy_run_key>'
)
SELECT
  h.Timestamp,
  h.Host,
  h.Method,
  h.Status,
  h.Path,
  round(h.DurationMs, 2) AS duration_ms,
  h.TraceId
FROM default.http_access_logs AS h
CROSS JOIN deploy AS d
WHERE h.Timestamp >= d.started_at - toIntervalMinute(2)
  AND h.Timestamp <= d.ended_at + toIntervalMinute(10)
  AND h.Status >= 400
ORDER BY h.Timestamp DESC
LIMIT 200"
```

## Metrics Around A Deploy

Discover metric names first:

```shell
aspect observe --what=catalog --signal=metrics --service=<service>
aspect observe --what=describe --metric=<metric>
```

Pull gauge samples around the deploy:

```shell
aspect db ch query --query="
WITH deploy AS
(
  SELECT
    min(Timestamp) AS started_at,
    max(Timestamp + toIntervalNanosecond(Duration)) AS ended_at
  FROM default.otel_traces
  WHERE ResourceAttributes['verself.deploy_run_key'] = '<deploy_run_key>'
)
SELECT
  m.TimeUnix,
  m.ServiceName,
  m.MetricName,
  m.Attributes,
  m.Value
FROM default.otel_metrics_gauge AS m
CROSS JOIN deploy AS d
WHERE m.ServiceName = '<service>'
  AND m.MetricName = '<metric>'
  AND m.TimeUnix >= d.started_at - toIntervalMinute(10)
  AND m.TimeUnix <= d.ended_at + toIntervalMinute(20)
ORDER BY m.TimeUnix
LIMIT 1000"
```

Pull monotonic counter rates from sum metrics:

```shell
aspect db ch query --query="
WITH
  deploy AS
  (
    SELECT
      min(Timestamp) AS started_at,
      max(Timestamp + toIntervalNanosecond(Duration)) AS ended_at
    FROM default.otel_traces
    WHERE ResourceAttributes['verself.deploy_run_key'] = '<deploy_run_key>'
  ),
  samples AS
  (
    SELECT
      toStartOfMinute(TimeUnix) AS minute,
      ServiceName,
      MetricName,
      Attributes,
      max(Value) - min(Value) AS delta
    FROM default.otel_metrics_sum AS m
    CROSS JOIN deploy AS d
    WHERE m.ServiceName = '<service>'
      AND m.MetricName = '<metric>'
      AND m.TimeUnix >= d.started_at - toIntervalMinute(10)
      AND m.TimeUnix <= d.ended_at + toIntervalMinute(20)
      AND m.IsMonotonic
    GROUP BY minute, ServiceName, MetricName, Attributes
  )
SELECT minute, ServiceName, MetricName, Attributes, delta
FROM samples
ORDER BY minute
LIMIT 1000"
```

## Trace Follow-Up

Use trace IDs from deploy rows, service logs, or HTTP access logs:

```shell
aspect observe --what=trace --trace-id=<trace_id>
```

Raw trace expansion:

```shell
aspect db ch query --query="
SELECT
  Timestamp,
  ServiceName,
  SpanName,
  ParentSpanId,
  StatusCode,
  round(Duration / 1000000, 2) AS duration_ms,
  SpanAttributes,
  left(StatusMessage, 180) AS error
FROM default.otel_traces
WHERE TraceId = '<trace_id>'
ORDER BY Timestamp
LIMIT 500"
```
