package main

import (
	"errors"
	"fmt"
	"strconv"
)

func buildQueries(cfg config) ([]query, error) {
	params := baseParams(cfg)

	switch cfg.what {
	case "catalog":
		return buildCatalogQueries(cfg, params)
	case "describe":
		return buildDescribeQueries(cfg, params)
	case "metric":
		if cfg.metric == "" {
			return nil, errors.New("WHAT=metric requires METRIC=<metric_name>")
		}
		if cfg.mode == "rate" {
			return []query{newQuery("metric.rate", metricRateSQL, params)}, nil
		}
		if cfg.mode != "latest" {
			return nil, errors.New("WHAT=metric MODE must be latest or rate")
		}
		return []query{newQuery("metric.latest", metricLatestSQL, params)}, nil
	case "errors":
		return []query{newQuery("errors.recent", errorsRecentSQL, params)}, nil
	case "service":
		if cfg.service == "" {
			return nil, errors.New("WHAT=service requires SERVICE=<service_name>")
		}
		if cfg.errorsOnly {
			return []query{newQuery("errors.recent", errorsRecentSQL, params)}, nil
		}
		return []query{
			newQuery("service.http_spans", serviceHTTPSpansSQL, params),
			newQuery("service.logs", serviceLogsSQL, params),
		}, nil
	case "mail":
		return []query{
			newQuery("mail.events", mailEventsSQL, params),
			newQuery("mail.metrics", mailMetricsSQL, params),
		}, nil
	case "workload-identity":
		return []query{
			newQuery("workload_identity.spans", workloadIdentitySpansSQL, params),
			newQuery("workload_identity.spire_logs", workloadIdentitySpireLogsSQL, params),
		}, nil
	case "deploy":
		if cfg.runKey != "" {
			return []query{newQuery("deploy.run", deployRunSQL, params)}, nil
		}
		return []query{newQuery("deploy.tasks", deployTasksSQL, params)}, nil
	case "trace":
		if cfg.traceID == "" {
			return nil, errors.New("WHAT=trace requires TRACE_ID=<trace-id>")
		}
		return []query{newQuery("trace.detail", traceDetailSQL, params)}, nil
	case "http":
		return []query{newQuery("http.access", httpAccessSQL, params)}, nil
	case "logs":
		return []query{newQuery("logs.recent", logsRecentSQL, params)}, nil
	default:
		return nil, fmt.Errorf("unknown WHAT=%q; run `make observe` for the discovery index", cfg.what)
	}
}

func buildCatalogQueries(cfg config, params map[string]string) ([]query, error) {
	switch cfg.signal {
	case "metrics":
		return []query{
			newQuery("catalog.metrics.namespaces", metricNamespaceCatalogSQL, params),
			newQuery("catalog.metrics.names", metricNameCatalogSQL, params),
		}, nil
	case "traces":
		return []query{newQuery("catalog.traces", traceCatalogSQL, params)}, nil
	case "logs":
		return []query{newQuery("catalog.logs", logFieldCatalogSQL, params)}, nil
	case "http":
		return []query{newQuery("catalog.http", httpCatalogSQL, params)}, nil
	case "deploy", "deploys":
		return []query{newQuery("catalog.deploys", deployCatalogSQL, params)}, nil
	default:
		return nil, fmt.Errorf("WHAT=catalog requires SIGNAL=metrics|traces|logs|http|deploys; got %q", cfg.signal)
	}
}

func buildDescribeQueries(cfg config, params map[string]string) ([]query, error) {
	primaryTargets := 0
	for _, value := range []string{cfg.metric, cfg.span, cfg.field} {
		if value != "" {
			primaryTargets++
		}
	}
	if cfg.queryName != "" {
		primaryTargets++
	}
	if cfg.service != "" && primaryTargets == 0 {
		primaryTargets++
	}
	if primaryTargets > 1 {
		return nil, errors.New("WHAT=describe requires exactly one primary target: METRIC, SPAN, FIELD, SERVICE, or QUERY")
	}
	if cfg.queryName != "" {
		return nil, errors.New("WHAT=describe QUERY is handled without ClickHouse; run `make observe WHAT=describe QUERY=<query-id>`")
	}
	if cfg.metric != "" && cfg.service != "" {
		return nil, errors.New("WHAT=describe METRIC does not accept SERVICE; use WHAT=catalog SIGNAL=metrics SERVICE=...")
	}
	if primaryTargets == 0 {
		return nil, errors.New("WHAT=describe requires METRIC, SERVICE, SPAN, FIELD, or QUERY")
	}
	switch {
	case cfg.metric != "":
		return []query{
			newQuery("describe.metric.summary", describeMetricSummarySQL, params),
			newQuery("describe.metric.attributes", describeMetricAttributesSQL, params),
		}, nil
	case cfg.span != "":
		return []query{
			newQuery("describe.span.summary", describeSpanSummarySQL, params),
			newQuery("describe.span.attributes", describeSpanAttributesSQL, params),
		}, nil
	case cfg.field != "":
		return []query{newQuery("describe.field", describeLogFieldSQL, params)}, nil
	case cfg.service != "":
		return []query{
			newQuery("describe.service.signals", describeServiceSignalsSQL, params),
			newQuery("describe.service.metrics", describeServiceMetricsSQL, params),
			newQuery("describe.service.spans", describeServiceSpansSQL, params),
			newQuery("describe.service.log_fields", describeServiceLogFieldsSQL, params),
		}, nil
	default:
		return nil, errors.New("WHAT=describe requires one of METRIC, SERVICE, SPAN, FIELD, or QUERY")
	}
}

func baseParams(cfg config) map[string]string {
	return map[string]string{
		"minutes":    strconv.FormatUint(uint64(cfg.minutes), 10),
		"row_limit":  strconv.FormatUint(uint64(cfg.limit), 10),
		"service":    cfg.service,
		"metric":     cfg.metric,
		"span":       cfg.span,
		"field":      cfg.field,
		"prefix":     cfg.prefix,
		"search":     cfg.search,
		"group_by":   cfg.groupBy,
		"trace_id":   cfg.traceID,
		"run_key":    cfg.runKey,
		"host":       cfg.host,
		"status_min": strconv.FormatUint(uint64(cfg.statusMin), 10),
	}
}

func newQuery(id, sql string, params map[string]string) query {
	return query{
		id:       id,
		title:    queryTitle(id),
		family:   queryFamily(id),
		purpose:  queryPurpose(id),
		database: "default",
		sql:      sql,
		params:   params,
		next:     queryDocNext(id),
	}
}

const metricNamespaceCatalogSQL = `
SELECT
  if(position(MetricName, '.') = 0, concat(MetricName, '.*'), concat(arrayElement(splitByChar('.', MetricName), 1), '.*')) AS namespace,
  countDistinct(MetricName) AS metric_names,
  arrayStringConcat(arraySort(groupUniqArray(MetricKind)), ', ') AS kinds,
  arrayStringConcat(arraySlice(arraySort(groupUniqArray(if(ServiceName = '', '<resource-only>', ServiceName))), 1, 8), ', ') AS services
FROM default.otel_metric_catalog_live
WHERE ({prefix:String} = '' OR startsWith(MetricName, {prefix:String}))
  AND ({service:String} = '' OR ServiceName = {service:String})
  AND ({search:String} = '' OR positionCaseInsensitive(MetricName, {search:String}) > 0)
GROUP BY namespace
ORDER BY namespace
LIMIT {row_limit:UInt32}`

const metricNameCatalogSQL = `
SELECT
  MetricName AS metric,
  arrayStringConcat(arraySort(groupUniqArray(MetricKind)), ', ') AS kinds,
  any(MetricUnit) AS unit,
  arrayStringConcat(arraySlice(arraySort(groupUniqArray(if(ServiceName = '', '<resource-only>', ServiceName))), 1, 8), ', ') AS services,
  sum(Samples) AS samples,
  sum(AttributeSets) AS attribute_sets
FROM default.otel_metric_catalog_live
WHERE ({prefix:String} = '' OR startsWith(MetricName, {prefix:String}))
  AND ({service:String} = '' OR ServiceName = {service:String})
  AND ({search:String} = '' OR positionCaseInsensitive(MetricName, {search:String}) > 0)
GROUP BY MetricName
ORDER BY metric
LIMIT {row_limit:UInt32}`

const traceCatalogSQL = `
SELECT
  ServiceName AS service,
  SpanName AS span,
  SpanKind AS kind,
  count() AS samples,
  arrayStringConcat(arraySort(groupUniqArray(StatusCode)), ', ') AS statuses,
  round(avg(Duration) / 1000000, 2) AS avg_ms
FROM default.otel_traces
WHERE ({service:String} = '' OR ServiceName = {service:String})
  AND ({search:String} = '' OR positionCaseInsensitive(SpanName, {search:String}) > 0)
GROUP BY service, span, kind
ORDER BY service, span
LIMIT {row_limit:UInt32}`

const logFieldCatalogSQL = `
SELECT
  ServiceName AS service,
  attr_key AS field,
  uniqExact(attr_value) AS distinct_values,
  arrayStringConcat(arraySlice(arraySort(groupUniqArray(left(attr_value, 160))), 1, 8), ', ') AS sample_values
FROM
(
  SELECT
    ServiceName,
    attr_key,
    attr_value
  FROM default.otel_logs
  ARRAY JOIN mapKeys(LogAttributes) AS attr_key, mapValues(LogAttributes) AS attr_value
  WHERE ({service:String} = '' OR ServiceName = {service:String})
    AND ({search:String} = '' OR positionCaseInsensitive(attr_key, {search:String}) > 0 OR positionCaseInsensitive(attr_value, {search:String}) > 0)
)
GROUP BY service, field
ORDER BY service, field
LIMIT {row_limit:UInt32}`

const httpCatalogSQL = `
SELECT
  Host AS host,
  Method AS method,
  countDistinct(Path) AS paths,
  min(Status) AS min_status,
  max(Status) AS max_status,
  round(avg(DurationMs), 2) AS avg_ms
FROM default.http_access_logs
WHERE ({host:String} = '' OR Host = {host:String})
  AND ({search:String} = '' OR positionCaseInsensitive(Path, {search:String}) > 0)
GROUP BY host, method
ORDER BY host, method
LIMIT {row_limit:UInt32}`

const deployCatalogSQL = `
SELECT
  extract(SpanAttributes['ansible.task.name'], ': ([A-Za-z0-9_-]+) :') AS role,
  SpanAttributes['forge_metal.deploy_run_key'] AS deploy_run_key,
  count() AS tasks,
  countIf(StatusCode IN ('Error', 'STATUS_CODE_ERROR')) AS errors,
  min(Timestamp) AS first_seen,
  max(Timestamp) AS last_seen
FROM default.otel_traces
WHERE ServiceName = 'ansible'
  AND SpanName = 'ansible.task'
  AND (
    {search:String} = ''
    OR positionCaseInsensitive(extract(SpanAttributes['ansible.task.name'], ': ([A-Za-z0-9_-]+) :'), {search:String}) > 0
    OR positionCaseInsensitive(SpanAttributes['forge_metal.deploy_run_key'], {search:String}) > 0
  )
GROUP BY role, deploy_run_key
ORDER BY deploy_run_key DESC, role
LIMIT {row_limit:UInt32}`

const describeMetricSummarySQL = `
SELECT
  MetricName AS metric,
  arrayStringConcat(arraySort(groupUniqArray(MetricKind)), ', ') AS kinds,
  arrayStringConcat(arraySort(groupUniqArray(MetricUnit)), ', ') AS units,
  arrayStringConcat(arraySlice(arraySort(groupUniqArray(if(ServiceName = '', '<resource-only>', ServiceName))), 1, 12), ', ') AS services,
  anyLast(MetricDescription) AS description,
  sum(Samples) AS samples,
  sum(AttributeSets) AS attribute_sets,
  if(countIf(MetricKind = 'sum') > 0, 'yes-for-monotonic-sums', 'usually-no') AS rate_candidate
FROM default.otel_metric_catalog_live
WHERE MetricName = {metric:String}
GROUP BY MetricName
ORDER BY metric`

const describeMetricAttributesSQL = `
SELECT
  attr_key AS attribute,
  uniqExact(attr_value) AS distinct_values,
  arrayStringConcat(arraySlice(arraySort(groupUniqArray(left(attr_value, 160))), 1, 12), ', ') AS sample_values
FROM
(
  SELECT
    attr_key,
    attr_value
  FROM default.otel_metric_latest
  ARRAY JOIN mapKeys(Attributes) AS attr_key, mapValues(Attributes) AS attr_value
  WHERE MetricName = {metric:String}
)
GROUP BY attribute
ORDER BY attribute
LIMIT {row_limit:UInt32}`

const describeServiceSignalsSQL = `
SELECT 'metrics' AS signal, countDistinct(MetricName) AS names, sum(Samples) AS rows FROM default.otel_metric_catalog_live WHERE ServiceName = {service:String}
UNION ALL
SELECT 'traces' AS signal, countDistinct(SpanName) AS names, count() AS rows FROM default.otel_traces WHERE ServiceName = {service:String}
UNION ALL
SELECT 'logs' AS signal, countDistinct(Body) AS names, count() AS rows FROM default.otel_logs WHERE ServiceName = {service:String}
UNION ALL
SELECT 'http' AS signal, countDistinct(Path) AS names, count() AS rows FROM default.http_access_logs WHERE ServiceName = {service:String}
ORDER BY signal`

const describeServiceMetricsSQL = `
SELECT
  MetricName AS metric,
  arrayStringConcat(arraySort(groupUniqArray(MetricKind)), ', ') AS kinds,
  any(MetricUnit) AS unit,
  sum(AttributeSets) AS attribute_sets
FROM default.otel_metric_catalog_live
WHERE ServiceName = {service:String}
GROUP BY metric
ORDER BY metric
LIMIT {row_limit:UInt32}`

const describeServiceSpansSQL = `
SELECT
  SpanName AS span,
  SpanKind AS kind,
  count() AS samples,
  arrayStringConcat(arraySort(groupUniqArray(StatusCode)), ', ') AS statuses
FROM default.otel_traces
WHERE ServiceName = {service:String}
GROUP BY span, kind
ORDER BY span
LIMIT {row_limit:UInt32}`

const describeServiceLogFieldsSQL = `
SELECT
  attr_key AS field,
  uniqExact(attr_value) AS distinct_values,
  arrayStringConcat(arraySlice(arraySort(groupUniqArray(left(attr_value, 160))), 1, 8), ', ') AS sample_values
FROM default.otel_logs
ARRAY JOIN mapKeys(LogAttributes) AS attr_key, mapValues(LogAttributes) AS attr_value
WHERE ServiceName = {service:String}
GROUP BY field
ORDER BY field
LIMIT {row_limit:UInt32}`

const describeSpanSummarySQL = `
SELECT
  ServiceName AS service,
  SpanName AS span,
  SpanKind AS kind,
  count() AS samples,
  arrayStringConcat(arraySort(groupUniqArray(StatusCode)), ', ') AS statuses,
  round(min(Duration) / 1000000, 2) AS min_ms,
  round(avg(Duration) / 1000000, 2) AS avg_ms,
  round(max(Duration) / 1000000, 2) AS max_ms
FROM default.otel_traces
WHERE SpanName = {span:String}
  AND ({service:String} = '' OR ServiceName = {service:String})
GROUP BY service, span, kind
ORDER BY service
LIMIT {row_limit:UInt32}`

const describeSpanAttributesSQL = `
SELECT
  attr_key AS attribute,
  uniqExact(attr_value) AS distinct_values,
  arrayStringConcat(arraySlice(arraySort(groupUniqArray(left(attr_value, 96))), 1, 5), ', ') AS sample_values
FROM default.otel_traces
ARRAY JOIN mapKeys(SpanAttributes) AS attr_key, mapValues(SpanAttributes) AS attr_value
WHERE SpanName = {span:String}
  AND ({service:String} = '' OR ServiceName = {service:String})
GROUP BY attribute
ORDER BY attribute
LIMIT {row_limit:UInt32}`

const describeLogFieldSQL = `
SELECT
  ServiceName AS service,
  {field:String} AS field,
  uniqExact(arrayElement(LogAttributes, {field:String})) AS distinct_values,
  arrayStringConcat(arraySlice(arraySort(groupUniqArray(left(arrayElement(LogAttributes, {field:String}), 160))), 1, 12), ', ') AS sample_values
FROM default.otel_logs
WHERE arrayElement(LogAttributes, {field:String}) != ''
  AND ({service:String} = '' OR ServiceName = {service:String})
GROUP BY service
ORDER BY service
LIMIT {row_limit:UInt32}`

const metricLatestSQL = `
SELECT
  if({group_by:String} = '', if(ServiceName = '', '<resource-only>', ServiceName), arrayElement(Attributes, {group_by:String})) AS series,
  MetricName AS metric,
  MetricKind AS kind,
  any(MetricUnit) AS unit,
  argMax(CurrentValue, SampledAt) AS value,
  max(SampledAt) AS sampled_at,
  arrayStringConcat(arraySlice(arraySort(groupUniqArray(toString(Attributes))), 1, 4), ' | ') AS attribute_sets
FROM default.otel_metric_latest
WHERE MetricName = {metric:String}
GROUP BY series, metric, kind
ORDER BY series
LIMIT {row_limit:UInt32}`

const metricRateSQL = `
SELECT
  toStartOfInterval(TimeUnix, INTERVAL 30 SECOND) AS time,
  if({group_by:String} = '', if(ServiceName = '', '<resource-only>', ServiceName), arrayElement(Attributes, {group_by:String})) AS series,
  round((max(Value) - min(Value)) / greatest(dateDiff('second', min(TimeUnix), max(TimeUnix)), 1), 6) AS per_second,
  count() AS samples
FROM default.otel_metric_scalar
WHERE MetricName = {metric:String}
  AND TimeUnix > now() - toIntervalMinute({minutes:UInt32})
GROUP BY time, series
ORDER BY time, series
LIMIT {row_limit:UInt32}`

const errorsRecentSQL = `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  SignalKind AS signal,
  ServiceName AS service,
  Severity AS severity,
  HttpStatus AS status,
  Path AS path,
  Name AS name,
  TraceId AS trace_id
FROM default.otel_signal_errors
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ({service:String} = '' OR ServiceName = {service:String})
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}`

const serviceHTTPSpansSQL = `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  SpanName AS span,
  SpanAttributes['http.method'] AS method,
  SpanAttributes['http.target'] AS path,
  SpanAttributes['http.status_code'] AS status,
  intDiv(Duration, 1000000) AS ms,
  TraceId AS trace_id
FROM default.otel_traces
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ServiceName = {service:String}
  AND SpanAttributes['http.target'] != ''
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}`

const serviceLogsSQL = `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  SeverityText AS level,
  Body AS message,
  TraceId AS trace_id
FROM default.otel_logs
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ServiceName = {service:String}
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}`

const mailEventsSQL = `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  Direction AS direction,
  EventType AS event,
  nullIf(MailboxAccount, '') AS mailbox,
  nullIf(Sender, '') AS sender,
  nullIf(Subject, '') AS subject,
  Message AS message
FROM default.mail_events
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}`

const mailMetricsSQL = `
SELECT
  MetricGroup AS metric_group,
  MetricName AS metric,
  CurrentValue AS value,
  SampledAt AS sampled_at
FROM default.mail_metrics_latest
ORDER BY metric_group, metric
LIMIT {row_limit:UInt32}`

const workloadIdentitySpansSQL = `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  ServiceName AS service,
  SpanName AS span,
  nullIf(SpanAttributes['spiffe.peer_id'], '') AS peer_id,
  nullIf(SpanAttributes['spiffe.expected_server_id'], '') AS expected_server_id,
  nullIf(SpanAttributes['spiffe.id'], '') AS svid_id,
  nullIf(SpanAttributes['jwt.audience'], '') AS audience,
  nullIf(SpanAttributes['bao.role'], '') AS bao_role,
  StatusCode AS status,
  intDiv(Duration, 1000000) AS ms,
  TraceId AS trace_id
FROM default.otel_traces
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND (
    startsWith(SpanName, 'auth.spiffe.')
    OR startsWith(SpanName, 'workload.openbao.')
    OR SpanName IN ('secrets.bao.jwt_svid.login', 'secrets.injection.service_token.exchange')
  )
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}`

const workloadIdentitySpireLogsSQL = `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  ServiceName AS service,
  SeverityText AS level,
  Body AS message,
  toString(LogAttributes) AS attributes
FROM default.otel_logs
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ServiceName IN ('spire-server', 'spire-agent')
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}`

const deployTasksSQL = `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  extract(SpanAttributes['ansible.task.name'], ': ([A-Za-z0-9_-]+) :') AS role,
  SpanAttributes['ansible.task.name'] AS task,
  StatusCode AS status,
  SpanAttributes['forge_metal.deploy_run_key'] AS deploy_run_key,
  TraceId AS trace_id
FROM default.otel_traces
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ServiceName = 'ansible'
  AND SpanName = 'ansible.task'
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}`

const deployRunSQL = `
SELECT
  Timestamp,
  extract(SpanAttributes['ansible.task.name'], ': ([A-Za-z0-9_-]+) :') AS role,
  SpanAttributes['ansible.task.name'] AS task,
  StatusCode AS status,
  TraceId AS trace_id
FROM default.otel_traces
WHERE ServiceName = 'ansible'
  AND SpanName = 'ansible.task'
  AND SpanAttributes['forge_metal.deploy_run_key'] = {run_key:String}
ORDER BY Timestamp
LIMIT {row_limit:UInt32}`

const traceDetailSQL = `
SELECT
  Timestamp,
  ServiceName AS service,
  SpanName AS span,
  ParentSpanId AS parent_span_id,
  StatusCode AS status,
  round(Duration / 1000000, 2) AS ms,
  SpanAttributes['http.method'] AS method,
  SpanAttributes['http.target'] AS target,
  SpanAttributes['forge_metal.deploy_run_key'] AS deploy_run_key
FROM default.otel_traces
WHERE TraceId = {trace_id:String}
ORDER BY Timestamp
LIMIT {row_limit:UInt32}`

const httpAccessSQL = `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  Host AS host,
  Method AS method,
  Status AS status,
  Path AS path,
  round(DurationMs, 2) AS ms,
  ClientIP AS client_ip,
  TraceId AS trace_id
FROM default.http_access_logs
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ({host:String} = '' OR Host = {host:String})
  AND Status >= {status_min:UInt16}
  AND ({search:String} = '' OR positionCaseInsensitive(Path, {search:String}) > 0)
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}`

const logsRecentSQL = `
SELECT
  formatDateTime(Timestamp, '%H:%i:%S') AS time,
  ServiceName AS service,
  SeverityText AS level,
  Body AS message,
  TraceId AS trace_id,
  toString(LogAttributes) AS attributes
FROM default.otel_logs
WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
  AND ({service:String} = '' OR ServiceName = {service:String})
  AND ({field:String} = '' OR arrayElement(LogAttributes, {field:String}) != '')
  AND ({search:String} = '' OR positionCaseInsensitive(Body, {search:String}) > 0 OR positionCaseInsensitive(toString(LogAttributes), {search:String}) > 0)
ORDER BY Timestamp DESC
LIMIT {row_limit:UInt32}`
