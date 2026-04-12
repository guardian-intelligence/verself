#!/usr/bin/env bash
# Query recent Ansible traces from ClickHouse with a fixed ServiceName filter.
set -euo pipefail

cd "$(dirname "$0")/.."

query="${QUERY:-${1:-}}"
limit="${LIMIT:-100}"

if [[ -z "${query}" ]]; then
  echo "ERROR: QUERY is required. Pass an expression fragment, for example:" >&2
  echo "  make deploy-trace QUERY=\"SpanName = 'ansible.task'\"" >&2
  exit 1
fi

./scripts/clickhouse.sh --database default --query "
  SELECT
    Timestamp,
    TraceId,
    SpanId,
    ParentSpanId,
    SpanName,
    Duration,
    StatusCode,
    SpanAttributes
  FROM default.otel_traces
  WHERE ServiceName = 'ansible'
    AND (${query})
  ORDER BY Timestamp DESC
  LIMIT ${limit}
  FORMAT PrettyCompact
"
