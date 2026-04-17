#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
# shellcheck disable=SC1091
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

kind="${VERIFICATION_KIND:-observe-live}"
run_id="${VERIFICATION_RUN_ID:-${kind}-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/${kind}}"
artifact_dir="${artifact_root}/${run_id}"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
expected_view_count=4
expected_invocation_count=6
expected_query_count=8

mkdir -p "${artifact_dir}"

ch_query() {
  (cd "${VERIFICATION_PLATFORM_ROOT}" && ./scripts/clickhouse.sh \
    --database default \
    --param_run_id="${run_id}" \
    --param_started_at="${started_at}" \
    --query "$1")
}

scalar_ch_query() {
  ch_query "$1" | tr -d '[:space:]'
}

run_observe() {
  local name="$1"
  shift

  (
    cd "${VERIFICATION_PLATFORM_ROOT}"
    FM_OBSERVE_RUN_ID="${run_id}" \
      FORGE_METAL_DEPLOY_RUN_KEY="${run_id}" \
      ./scripts/observe.sh "$@"
  ) >"${artifact_dir}/observe-${name}.txt" 2>&1
}

run_observe catalog --what catalog --limit 10
run_observe metric --what metric --metric system.cpu.time --limit 10
run_observe service --what service --service billing-service --minutes 60 --limit 10
run_observe errors --what errors --minutes 60 --limit 10
run_observe mail --what mail --minutes 60 --limit 10
run_observe deploy --what deploy --minutes 60 --limit 10

view_count="$(scalar_ch_query "
SELECT count()
FROM system.tables
WHERE database = 'default'
  AND engine = 'View'
  AND name IN (
    'otel_metric_scalar',
    'otel_metric_latest',
    'otel_metric_catalog_live',
    'otel_signal_errors'
  )
FORMAT TabSeparated
")"

if [[ "${view_count}" -ne "${expected_view_count}" ]]; then
  echo "Expected ${expected_view_count} observability views, got ${view_count}" >&2
  exit 1
fi

ch_query "SYSTEM FLUSH LOGS" >/dev/null

successful_queries=0
sequenced_queries=0
root_spans=0
query_spans=0
sequenced_traces=0
error_spans=0

for _ in $(seq 1 30); do
  successful_queries="$(scalar_ch_query "
  SELECT count()
  FROM system.query_log
  WHERE event_time >= parseDateTimeBestEffort({started_at:String})
    AND type = 'QueryFinish'
    AND exception_code = 0
    AND query_id LIKE concat('fm-observe:', {run_id:String}, ':%')
  FORMAT TabSeparated
  ")"

  sequenced_queries="$(scalar_ch_query "
  SELECT count()
  FROM (
    SELECT
      query_id,
      minIf(event_time_microseconds, type = 'QueryStart') AS started_at_us,
      minIf(event_time_microseconds, type = 'QueryFinish') AS finished_at_us,
      countIf(type = 'QueryStart') AS starts,
      countIf(type = 'QueryFinish') AS finishes
    FROM system.query_log
    WHERE event_time >= parseDateTimeBestEffort({started_at:String})
      AND query_id LIKE concat('fm-observe:', {run_id:String}, ':%')
    GROUP BY query_id
    HAVING starts >= 1
      AND finishes >= 1
      AND finished_at_us >= started_at_us
  )
  FORMAT TabSeparated
  ")"

  root_spans="$(scalar_ch_query "
  SELECT count()
  FROM default.otel_traces
  WHERE Timestamp >= parseDateTime64BestEffort({started_at:String})
    AND ServiceName = 'fm-observe'
    AND SpanName = 'fm.observe'
    AND SpanAttributes['observe.run_id'] = {run_id:String}
  FORMAT TabSeparated
  ")"

  query_spans="$(scalar_ch_query "
  SELECT count()
  FROM default.otel_traces
  WHERE Timestamp >= parseDateTime64BestEffort({started_at:String})
    AND ServiceName = 'fm-observe'
    AND SpanName = 'clickhouse.query'
    AND SpanAttributes['observe.run_id'] = {run_id:String}
  FORMAT TabSeparated
  ")"

  sequenced_traces="$(scalar_ch_query "
  SELECT count()
  FROM (
    SELECT
      TraceId,
      countIf(SpanName = 'fm.observe') AS roots,
      countIf(SpanName = 'clickhouse.query') AS queries,
      minIf(Timestamp, SpanName = 'fm.observe') AS root_ts,
      minIf(Timestamp, SpanName = 'clickhouse.query') AS query_ts
    FROM default.otel_traces
    WHERE Timestamp >= parseDateTime64BestEffort({started_at:String})
      AND ServiceName = 'fm-observe'
      AND SpanAttributes['observe.run_id'] = {run_id:String}
    GROUP BY TraceId
    HAVING roots = 1
      AND queries >= 1
      AND query_ts >= root_ts
  )
  FORMAT TabSeparated
  ")"

  error_spans="$(scalar_ch_query "
  SELECT count()
  FROM default.otel_traces
  WHERE Timestamp >= parseDateTime64BestEffort({started_at:String})
    AND ServiceName = 'fm-observe'
    AND SpanAttributes['observe.run_id'] = {run_id:String}
    AND StatusCode IN ('Error', 'STATUS_CODE_ERROR')
  FORMAT TabSeparated
  ")"

  if [[ "${successful_queries}" -ge "${expected_query_count}" \
    && "${sequenced_queries}" -ge "${expected_query_count}" \
    && "${root_spans}" -ge "${expected_invocation_count}" \
    && "${query_spans}" -ge "${expected_query_count}" \
    && "${sequenced_traces}" -ge "${expected_invocation_count}" \
    && "${error_spans}" -eq 0 ]]; then
    break
  fi
  sleep 1
done

ch_query "
SELECT
  event_time,
  type,
  initial_user,
  query_id,
  exception_code,
  query
FROM system.query_log
WHERE event_time >= parseDateTimeBestEffort({started_at:String})
  AND query_id LIKE concat('fm-observe:', {run_id:String}, ':%')
ORDER BY event_time, type
FORMAT TSVWithNames
" >"${artifact_dir}/clickhouse-query-log.tsv"

ch_query "
SELECT
  Timestamp,
  TraceId,
  SpanId,
  ParentSpanId,
  ServiceName,
  SpanName,
  StatusCode,
  SpanAttributes['observe.what'] AS observe_what,
  SpanAttributes['clickhouse.query_id'] AS query_id,
  SpanAttributes['clickhouse.query_name'] AS query_name
FROM default.otel_traces
WHERE Timestamp >= parseDateTime64BestEffort({started_at:String})
  AND ServiceName = 'fm-observe'
  AND SpanAttributes['observe.run_id'] = {run_id:String}
ORDER BY Timestamp
FORMAT TSVWithNames
" >"${artifact_dir}/otel-traces.tsv"

cat >"${artifact_dir}/summary.tsv" <<TSV
view_count	expected_view_count	successful_queries	expected_query_count	sequenced_queries	root_spans	expected_invocation_count	query_spans	sequenced_traces	error_spans
${view_count}	${expected_view_count}	${successful_queries}	${expected_query_count}	${sequenced_queries}	${root_spans}	${expected_invocation_count}	${query_spans}	${sequenced_traces}	${error_spans}
TSV

if [[ "${successful_queries}" -lt "${expected_query_count}" ]]; then
  echo "observe emitted ${successful_queries}/${expected_query_count} successful ClickHouse QueryFinish rows" >&2
  exit 1
fi

if [[ "${sequenced_queries}" -lt "${expected_query_count}" ]]; then
  echo "observe emitted ${sequenced_queries}/${expected_query_count} ordered QueryStart->QueryFinish rows" >&2
  exit 1
fi

if [[ "${root_spans}" -lt "${expected_invocation_count}" ]]; then
  echo "observe emitted ${root_spans}/${expected_invocation_count} root spans" >&2
  exit 1
fi

if [[ "${query_spans}" -lt "${expected_query_count}" ]]; then
  echo "observe emitted ${query_spans}/${expected_query_count} ClickHouse query spans" >&2
  exit 1
fi

if [[ "${sequenced_traces}" -lt "${expected_invocation_count}" ]]; then
  echo "observe emitted ${sequenced_traces}/${expected_invocation_count} root->query trace sequences" >&2
  exit 1
fi

if [[ "${error_spans}" -ne 0 ]]; then
  echo "observe emitted ${error_spans} error spans" >&2
  exit 1
fi

cat >"${artifact_dir}/run.json" <<JSON
{
  "verification_run_id": "${run_id}",
  "started_at": "${started_at}",
  "expected_view_count": ${expected_view_count},
  "expected_invocation_count": ${expected_invocation_count},
  "expected_query_count": ${expected_query_count},
  "status": "succeeded"
}
JSON

echo "Observe verification evidence: ${artifact_dir}"
