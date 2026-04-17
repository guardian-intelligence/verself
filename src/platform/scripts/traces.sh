#!/usr/bin/env bash
# Pull recent OTel traces and logs from ClickHouse for debugging.
#
# Usage:
#   ./traces.sh                          # Last 5 min, all services
#   ./traces.sh -s billing-service       # Filter to one service
#   ./traces.sh -m 30                    # Last 30 minutes
#   ./traces.sh -n 50                    # Show 50 rows (default 25)
#   ./traces.sh -e                       # Errors only (status_code >= 400 or ERROR severity)
#   ./traces.sh -t                       # Traces only (no logs)
#   ./traces.sh -l                       # Logs only (no traces)
set -euo pipefail
cd "$(dirname "$0")/.."

SERVICE=""
MINUTES=5
LIMIT=25
ERRORS_ONLY=false
SHOW_TRACES=true
SHOW_LOGS=true

while getopts "s:m:n:etl" opt; do
  case $opt in
    s) SERVICE="$OPTARG" ;;
    m) MINUTES="$OPTARG" ;;
    n) LIMIT="$OPTARG" ;;
    e) ERRORS_ONLY=true ;;
    t) SHOW_LOGS=false ;;
    l) SHOW_TRACES=false ;;
    *) echo "Usage: $0 [-s service] [-m minutes] [-n limit] [-e errors] [-t traces-only] [-l logs-only]" >&2; exit 1 ;;
  esac
done

if ! [[ "${MINUTES}" =~ ^[1-9][0-9]*$ ]]; then
  echo "ERROR: minutes must be a positive integer" >&2
  exit 1
fi
if ! [[ "${LIMIT}" =~ ^[1-9][0-9]*$ ]]; then
  echo "ERROR: limit must be a positive integer" >&2
  exit 1
fi
if (( LIMIT > 500 )); then
  echo "ERROR: limit must be <= 500" >&2
  exit 1
fi
if [[ -n "${SERVICE}" && ! "${SERVICE}" =~ ^[A-Za-z0-9_.-]+$ ]]; then
  echo "ERROR: service must contain only letters, numbers, dot, underscore, or dash" >&2
  exit 1
fi

ch_query() {
  ./scripts/clickhouse.sh \
    --database default \
    --param_service="${SERVICE}" \
    --param_minutes="${MINUTES}" \
    --param_row_limit="${LIMIT}" \
    --query "$1"
}

# --- traces ---

if $SHOW_TRACES; then
  error_filter=""
  if $ERRORS_ONLY; then
    error_filter="AND (toUInt16OrZero(SpanAttributes['http.status_code']) >= 400 OR StatusCode = 'Error')"
  fi

  echo "=== TRACES (last ${MINUTES}m) ==="
  echo ""
  ch_query "
    SELECT
      formatDateTime(Timestamp, '%H:%i:%S') AS time,
      ServiceName AS service,
      SpanAttributes['http.method'] AS method,
      SpanAttributes['http.target'] AS path,
      SpanAttributes['http.status_code'] AS status,
      intDiv(Duration, 1000000) AS ms
    FROM default.otel_traces
    WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
      AND ({service:String} = '' OR ServiceName = {service:String})
      ${error_filter}
      AND SpanAttributes['http.target'] != ''
    ORDER BY Timestamp DESC
    LIMIT {row_limit:UInt32}
    FORMAT PrettyCompact
  "
  echo ""
fi

# --- logs ---

if $SHOW_LOGS; then
  error_filter=""
  if $ERRORS_ONLY; then
    error_filter="AND SeverityText IN ('ERROR', 'FATAL', 'WARN')"
  fi

  echo "=== LOGS (last ${MINUTES}m) ==="
  echo ""
  ch_query "
    SELECT
      formatDateTime(Timestamp, '%H:%i:%S') AS time,
      ServiceName AS service,
      SeverityText AS level,
      Body AS message,
      toString(LogAttributes) AS attrs
    FROM default.otel_logs
    WHERE Timestamp > now() - toIntervalMinute({minutes:UInt32})
      AND ({service:String} = '' OR ServiceName = {service:String})
      ${error_filter}
    ORDER BY Timestamp DESC
    LIMIT {row_limit:UInt32}
    FORMAT PrettyCompact
  "
fi
