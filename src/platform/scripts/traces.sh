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

# --- resolve inventory + credentials ---

inventory="${INVENTORY:-ansible/inventory/hosts.ini}"
secrets_file="${SOPS_SECRETS_FILE:-ansible/group_vars/all/secrets.sops.yml}"

if [[ ! -f "$inventory" ]]; then
  echo "ERROR: $inventory not found." >&2; exit 1
fi

remote_host="$(grep -m1 'ansible_host=' "$inventory" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "$inventory" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"
ch_password="$(sops -d --extract '["clickhouse_password"]' "$secrets_file")"

# POST query to ClickHouse HTTP API via SSH — avoids all shell quoting issues.
ch_query() {
  ssh -o StrictHostKeyChecking=no "${remote_user}@${remote_host}" \
    "curl -sf 'http://127.0.0.1:8123/?user=default&password=${ch_password}' --data-binary @-" <<< "$1"
}

# --- build filters ---

svc_filter=""
if [[ -n "$SERVICE" ]]; then
  svc_filter="AND ServiceName = '${SERVICE}'"
fi

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
      formatDateTime(Timestamp, '%H:%M:%S') AS time,
      ServiceName AS service,
      SpanAttributes['http.method'] AS method,
      SpanAttributes['http.target'] AS path,
      SpanAttributes['http.status_code'] AS status,
      intDiv(Duration, 1000000) AS ms
    FROM default.otel_traces
    WHERE Timestamp > now() - INTERVAL ${MINUTES} MINUTE
      ${svc_filter}
      ${error_filter}
      AND SpanAttributes['http.target'] != ''
    ORDER BY Timestamp DESC
    LIMIT ${LIMIT}
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
      formatDateTime(Timestamp, '%H:%M:%S') AS time,
      ServiceName AS service,
      SeverityText AS level,
      Body AS message,
      toString(LogAttributes) AS attrs
    FROM default.otel_logs
    WHERE Timestamp > now() - INTERVAL ${MINUTES} MINUTE
      ${svc_filter}
      ${error_filter}
    ORDER BY Timestamp DESC
    LIMIT ${LIMIT}
    FORMAT PrettyCompact
  "
fi
