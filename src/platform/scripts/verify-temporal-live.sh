#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-temporal-verify-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/temporal-verify}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/postgres"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
trust_domain="spiffe.${VERIFICATION_DOMAIN}"
spire_socket="unix:///run/spire-agent/sockets/agent.sock"
temporal_server_spiffe_id="spiffe://${trust_domain}/svc/temporal-server"
temporal_frontend_address="127.0.0.1:7233"
temporal_metrics_address="127.0.0.1:9001"
temporal_metrics_host="${temporal_metrics_address%:*}"
temporal_metrics_port="${temporal_metrics_address##*:}"
temporal_bootstrap_bin="/opt/verself/profile/bin/temporal-bootstrap"
temporal_sandbox_namespace="sandbox-rental-service"
temporal_billing_namespace="billing-service"

remote_psql() {
  local db="$1"
  local sql="$2"
  verification_ssh "sudo -u postgres psql -d ${db} -X -A -t -F \$'\\t' -P footer=off -c \"$sql\""
}

remote_temporal_bootstrap() {
  local argv=(
    sudo -u temporal_server
    env
    "SPIFFE_ENDPOINT_SOCKET=${spire_socket}"
    "OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4317"
    "VERSELF_TEMPORAL_FRONTEND_ADDRESS=${temporal_frontend_address}"
    "VERSELF_TEMPORAL_BOOTSTRAP_NAMESPACES=${temporal_sandbox_namespace},${temporal_billing_namespace}"
    "VERSELF_TEMPORAL_BOOTSTRAP_NAMESPACE_RETENTION=24h"
    "${temporal_bootstrap_bin}"
  )
  local remote_cmd=""
  local arg
  for arg in "${argv[@]}"; do
    remote_cmd+=" $(printf '%q' "${arg}")"
  done
  verification_ssh "bash -lc 'exec${remote_cmd}'"
}

wait_for_remote_tcp() {
  local name="$1"
  local host="$2"
  local port="$3"
  verification_ssh "python3 - <<'PY'
import socket
import sys
host = ${host@Q}
port = int(${port@Q})
for _ in range(60):
    try:
        with socket.create_connection((host, port), 1):
            raise SystemExit(0)
    except OSError:
        pass
    import time
    time.sleep(1)
raise SystemExit('timeout waiting for ${name}')
PY"
}

wait_for_clickhouse_count() {
  local database="$1"
  local query="$2"
  local min_count="$3"
  local output_path="$4"
  shift 4
  local extra_args=("$@")
  local count="0"
  for _ in $(seq 1 60); do
    (
      cd "${VERIFICATION_PLATFORM_ROOT}"
      ./scripts/clickhouse.sh \
        --database "${database}" \
        --param_window_start="${window_start}" \
        --param_window_end="${window_end}" \
        "${extra_args[@]}" \
        --query "${query}"
    ) >"${output_path}"
    count="$(tail -n 1 "${output_path}" | tr -d '[:space:]')"
    if [[ "${count}" =~ ^[0-9]+$ ]] && (( count >= min_count )); then
      return 0
    fi
    sleep 2
  done
  echo "ClickHouse assertion failed for ${output_path}: got ${count}, expected >= ${min_count}" >&2
  return 1
}

wait_for_remote_tcp "Temporal frontend" "127.0.0.1" "7233"
wait_for_remote_tcp "Temporal metrics" "${temporal_metrics_host}" "${temporal_metrics_port}"

verification_ssh "sudo systemctl is-active temporal-server temporal-web" \
  >"${artifact_dir}/systemd-active.txt"

verification_ssh "test ! -e /opt/verself/profile/bin/temporal-proof"
printf 'absent\n' >"${artifact_dir}/temporal-proof-binary.txt"

verification_ssh "test ! -e /etc/systemd/system/temporal-proof-worker.service"
printf 'absent\n' >"${artifact_dir}/temporal-proof-worker-unit.txt"

if verification_ssh "sudo /opt/verself/profile/bin/spire-server entry show -socketPath /run/spire-server/private/api.sock | grep -q 'verself-temporal-proof'"; then
  echo "unexpected retired verself-temporal-proof SPIRE entry remains" >&2
  exit 1
fi
printf 'absent\n' >"${artifact_dir}/temporal-proof-spire-entry.txt"

remote_temporal_bootstrap >"${artifact_dir}/bootstrap-before-restart.txt"

verification_ssh "sudo systemctl restart temporal-server"
wait_for_remote_tcp "Temporal frontend after restart" "127.0.0.1" "7233"
wait_for_remote_tcp "Temporal metrics after restart" "${temporal_metrics_host}" "${temporal_metrics_port}"
verification_ssh "sudo systemctl is-active temporal-server" >"${artifact_dir}/temporal-server-after-restart.txt"

remote_temporal_bootstrap >"${artifact_dir}/bootstrap-after-restart.txt"

remote_psql temporal "
SELECT name, id
FROM namespaces
WHERE name IN ('sandbox-rental-service', 'billing-service')
ORDER BY name;
" >"${artifact_dir}/postgres/namespaces.tsv"

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-bootstrap'
" 2 "${artifact_dir}/clickhouse/temporal-bootstrap-span-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'auth.spiffe.mtls.server'
    AND SpanAttributes['spiffe.peer_id'] = {temporal_server_spiffe_id:String}
" 2 "${artifact_dir}/clickhouse/temporal-server-mtls-server-count.tsv" --param_temporal_server_spiffe_id="${temporal_server_spiffe_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'temporal.auth.authorize'
    AND SpanAttributes['temporal.namespace'] IN ('${temporal_sandbox_namespace}', '${temporal_billing_namespace}')
    AND SpanAttributes['temporal.authz.decision'] = 'allow'
" 2 "${artifact_dir}/clickhouse/temporal-authz-allow-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_logs
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName IN ('temporal-server', 'temporal-bootstrap')
" 1 "${artifact_dir}/clickhouse/temporal-logs-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_metric_catalog_live
  WHERE LastSeenAt BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND (startsWith(ServiceName, 'temporal-server') OR ServiceName = 'temporal-web')
" 1 "${artifact_dir}/clickhouse/temporal-metric-catalog-count.tsv"

proof_trace_count="$(
  (
    cd "${VERIFICATION_PLATFORM_ROOT}"
    ./scripts/clickhouse.sh \
      --database default \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT count()
        FROM otel_traces
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
          AND ServiceName IN ('temporal-proof', 'temporal-proof-worker')
      "
  ) | tr -d '[:space:]'
)"
if [[ "${proof_trace_count}" != "0" ]]; then
  echo "unexpected temporal-proof traces found in the verification window" >&2
  exit 1
fi

proof_log_count="$(
  (
    cd "${VERIFICATION_PLATFORM_ROOT}"
    ./scripts/clickhouse.sh \
      --database default \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT count()
        FROM otel_logs
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
          AND ServiceName IN ('temporal-proof', 'temporal-proof-worker')
      "
  ) | tr -d '[:space:]'
)"
if [[ "${proof_log_count}" != "0" ]]; then
  echo "unexpected temporal-proof logs found in the verification window" >&2
  exit 1
fi

python3 - "${artifact_dir}/postgres/namespaces.tsv" <<'PY'
import csv
import sys

rows = list(csv.reader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
names = {row[0] for row in rows if row}
required = {"sandbox-rental-service", "billing-service"}
missing = sorted(required - names)
if missing:
    raise SystemExit(f"missing expected namespaces: {missing}")
PY

python3 - "${run_id}" "${window_start}" "${window_end}" "${artifact_dir}" >"${artifact_dir}/run.json" <<'PY'
import json
import sys

run_id, window_start, window_end, artifact_dir = sys.argv[1:5]
print(json.dumps({
    "run_id": run_id,
    "window_start": window_start,
    "window_end": window_end,
    "artifact_dir": artifact_dir,
}, indent=2, sort_keys=True))
PY

echo "temporal verify ok: ${artifact_dir}"
