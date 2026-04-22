#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-temporal-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/temporal-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/postgres"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
proof_run_id="${run_id}"
denied_run_id="${run_id}-denied"
trust_domain="spiffe.${VERIFICATION_DOMAIN}"
spire_socket="unix:///run/spire-agent/sockets/agent.sock"
temporal_server_spiffe_id="spiffe://${trust_domain}/svc/temporal-server"
temporal_proof_spiffe_id="spiffe://${trust_domain}/svc/temporal-proof"
governance_spiffe_id="spiffe://${trust_domain}/svc/governance-service"
temporal_frontend_address="127.0.0.1:7233"
temporal_metrics_address="127.0.0.1:9001"
governance_internal_url="https://127.0.0.1:4254/internal/v1/audit/events"
temporal_proof_bin="/opt/forge-metal/profile/bin/temporal-proof"
temporal_proof_worker_started=0

remote_psql() {
  local db="$1"
  local sql="$2"
  verification_ssh "sudo -u postgres psql -d ${db} -X -A -t -F \$'\\t' -P footer=off -c \"$sql\""
}

remote_temporal_proof() {
  local user="$1"
  shift
  local argv=(
    sudo -u "${user}"
    env
    "SPIFFE_ENDPOINT_SOCKET=${spire_socket}"
    "OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4317"
    "FM_TEMPORAL_FRONTEND_ADDRESS=${temporal_frontend_address}"
    "FM_TEMPORAL_PROOF_NAMESPACE=temporal-proof"
    "FM_TEMPORAL_SERVER_SPIFFE_ID=${temporal_server_spiffe_id}"
    "FM_TEMPORAL_PROOF_GOVERNANCE_URL=${governance_internal_url}"
    "FM_TEMPORAL_PROOF_GOVERNANCE_SPIFFE_ID=${governance_spiffe_id}"
    "FM_TEMPORAL_PROOF_SERVICE_VERSION=verification-${run_id}"
    "${temporal_proof_bin}"
  )
  argv+=("$@")
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

cleanup_temporal_proof_worker() {
  local status="$1"
  trap - EXIT
  if [[ "${temporal_proof_worker_started}" == "1" ]]; then
    verification_ssh "sudo systemctl stop temporal-proof-worker"
    verification_ssh "sudo systemctl is-active temporal-proof-worker || true" \
      >"${artifact_dir}/temporal-proof-worker-post-stop.txt"
  fi
  exit "${status}"
}

trap 'cleanup_temporal_proof_worker "$?"' EXIT

wait_for_remote_tcp "Temporal frontend" "127.0.0.1" "7233"
wait_for_remote_tcp "Temporal metrics" "127.0.0.1" "9001"
verification_wait_for_loopback_api "governance-service" "http://127.0.0.1:4250/readyz" "200"

verification_ssh "sudo systemctl is-active temporal-server governance-service" \
  >"${artifact_dir}/systemd-active.txt"
# `systemctl is-enabled`/`is-active` return non-zero for the desired
# disabled/inactive states, so capture the rendered state rather than the rc.
verification_ssh "sudo systemctl is-enabled temporal-proof-worker || true" \
  >"${artifact_dir}/temporal-proof-worker-enabled.txt"
verification_ssh "sudo systemctl is-active temporal-proof-worker || true" \
  >"${artifact_dir}/temporal-proof-worker-pre-start.txt"
verification_ssh "sudo ss -ltnH '( sport = :7233 or sport = :9001 )'" \
  >"${artifact_dir}/temporal-listeners.tsv"

if [[ "$(tr -d '[:space:]' <"${artifact_dir}/temporal-proof-worker-enabled.txt")" != "disabled" ]]; then
  echo "expected temporal-proof-worker to remain disabled outside live verification" >&2
  exit 1
fi

if [[ "$(tr -d '[:space:]' <"${artifact_dir}/temporal-proof-worker-pre-start.txt")" != "inactive" ]]; then
  echo "expected temporal-proof-worker to be inactive before live verification" >&2
  exit 1
fi

verification_ssh "sudo systemctl start temporal-proof-worker"
temporal_proof_worker_started=1
verification_ssh "sudo systemctl is-active temporal-proof-worker" \
  >"${artifact_dir}/temporal-proof-worker-started.txt"

remote_temporal_proof temporal_server bootstrap
remote_temporal_proof temporal_proof denied --run-id "${denied_run_id}" \
  >"${artifact_dir}/denied-check.txt"
remote_temporal_proof temporal_proof start --run-id "${proof_run_id}" --sleep 20s \
  >"${artifact_dir}/start.json"

workflow_id="$(
  python3 - "${artifact_dir}/start.json" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["workflow_id"])
PY
)"
workflow_run_id="$(
  python3 - "${artifact_dir}/start.json" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["run_id"])
PY
)"

verification_ssh "sudo systemctl restart temporal-server"
wait_for_remote_tcp "Temporal frontend after restart" "127.0.0.1" "7233"
wait_for_remote_tcp "Temporal metrics after restart" "127.0.0.1" "9001"
verification_ssh "sudo systemctl is-active temporal-server" >"${artifact_dir}/temporal-server-after-restart.txt"

remote_temporal_proof temporal_proof await \
  --workflow-id "${workflow_id}" \
  --run-id "${workflow_run_id}" \
  --timeout 180s \
  >"${artifact_dir}/await.json"

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

audit_event_id="$(
  python3 - "${artifact_dir}/await.json" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["audit_event_id"])
PY
)"
audit_sequence="$(
  python3 - "${artifact_dir}/await.json" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["audit_sequence"])
PY
)"

remote_psql temporal "
SELECT name, id
FROM namespaces
WHERE name IN ('temporal-proof', 'temporal-denied')
ORDER BY name;
" >"${artifact_dir}/postgres/namespaces.tsv"

remote_psql temporal_visibility "
SELECT workflow_id, run_id, workflow_type_name, task_queue, status, start_time, close_time, execution_duration, history_length
FROM executions_visibility
WHERE workflow_id = '${workflow_id}'
ORDER BY start_time DESC;
" >"${artifact_dir}/postgres/executions-visibility.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'auth.spiffe.mtls.server'
    AND SpanAttributes['spiffe.peer_id'] = {temporal_proof_spiffe_id:String}
" 1 "${artifact_dir}/clickhouse/temporal-server-mtls-server-count.tsv" --param_temporal_proof_spiffe_id="${temporal_proof_spiffe_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'temporal.auth.authorize'
    AND SpanAttributes['temporal.namespace'] = 'temporal-proof'
    AND SpanAttributes['temporal.authz.decision'] = 'allow'
" 1 "${artifact_dir}/clickhouse/temporal-authz-allow-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'temporal.auth.authorize'
    AND SpanAttributes['temporal.namespace'] = 'temporal-denied'
    AND SpanAttributes['temporal.authz.decision'] = 'deny'
" 1 "${artifact_dir}/clickhouse/temporal-authz-deny-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-proof'
    AND SpanName = 'temporal.proof.start'
    AND SpanAttributes['forge_metal.proof_run_id'] = {proof_run_id:String}
" 1 "${artifact_dir}/clickhouse/temporal-proof-start-count.tsv" --param_proof_run_id="${proof_run_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-proof'
    AND SpanName = 'temporal.proof.await'
    AND SpanAttributes['temporal.workflow_id'] = {workflow_id:String}
    AND SpanAttributes['temporal.run_id'] = {workflow_run_id:String}
" 1 "${artifact_dir}/clickhouse/temporal-proof-await-count.tsv" --param_workflow_id="${workflow_id}" --param_workflow_run_id="${workflow_run_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-proof-worker'
    AND SpanName = 'temporal.proof.audit_activity'
    AND SpanAttributes['forge_metal.proof_run_id'] = {proof_run_id:String}
" 1 "${artifact_dir}/clickhouse/temporal-proof-audit-activity-count.tsv" --param_proof_run_id="${proof_run_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'governance-service'
    AND SpanName = 'governance-service-internal'
    AND (
      SpanAttributes['http.target'] = '/internal/v1/audit/events'
      OR SpanAttributes['url.path'] = '/internal/v1/audit/events'
    )
" 1 "${artifact_dir}/clickhouse/governance-internal-audit-span-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_logs
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName IN ('temporal-server', 'temporal-proof', 'temporal-proof-worker')
" 1 "${artifact_dir}/clickhouse/temporal-logs-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_metric_catalog_live
  WHERE LastSeenAt BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND startsWith(ServiceName, 'temporal-server')
" 1 "${artifact_dir}/clickhouse/temporal-metric-catalog-count.tsv"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM audit_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND service_name = 'temporal-proof'
    AND audit_event = 'temporal.proof.heartbeat.completed'
    AND actor_spiffe_id = {temporal_proof_spiffe_id:String}
    AND target_id = {workflow_id:String}
    AND event_id = {audit_event_id:String}
    AND sequence = toUInt64({audit_sequence:String})
" 1 "${artifact_dir}/clickhouse/temporal-governance-audit-count.tsv" \
  --param_temporal_proof_spiffe_id="${temporal_proof_spiffe_id}" \
  --param_workflow_id="${workflow_id}" \
  --param_audit_event_id="${audit_event_id}" \
  --param_audit_sequence="${audit_sequence}"

visibility_count="$(
  remote_psql temporal_visibility "
  SELECT count(*)
  FROM executions_visibility
  WHERE workflow_id = '${workflow_id}'
    AND run_id = '${workflow_run_id}'
    AND workflow_type_name = 'ProofHeartbeat'
    AND task_queue = 'proof-heartbeat'
    AND status = 2;
  " | tr -d '[:space:]'
)"
if [[ "${visibility_count}" != "1" ]]; then
  echo "expected exactly one completed visibility row, got ${visibility_count}" >&2
  exit 1
fi

audit_count="$(
  (
    cd "${VERIFICATION_PLATFORM_ROOT}"
    ./scripts/clickhouse.sh \
      --database forge_metal \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --param_workflow_id="${workflow_id}" \
      --query "
        SELECT count()
        FROM audit_events
        WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
          AND service_name = 'temporal-proof'
          AND audit_event = 'temporal.proof.heartbeat.completed'
          AND target_id = {workflow_id:String}
      "
  ) | tr -d '[:space:]'
)"
if [[ "${audit_count}" != "1" ]]; then
  echo "expected exactly one completed audit row, got ${audit_count}" >&2
  exit 1
fi

verification_ssh "sudo systemctl stop temporal-proof-worker"
verification_ssh "sudo systemctl is-active temporal-proof-worker || true" \
  >"${artifact_dir}/temporal-proof-worker-post-stop.txt"
temporal_proof_worker_started=0

if [[ "$(tr -d '[:space:]' <"${artifact_dir}/temporal-proof-worker-post-stop.txt")" != "inactive" ]]; then
  echo "expected temporal-proof-worker to return to inactive after live verification" >&2
  exit 1
fi

python3 - "${run_id}" "${window_start}" "${window_end}" "${artifact_dir}" "${workflow_id}" "${workflow_run_id}" "${audit_event_id}" "${audit_sequence}" >"${artifact_dir}/run.json" <<'PY'
import json
import sys

run_id, window_start, window_end, artifact_dir, workflow_id, workflow_run_id, audit_event_id, audit_sequence = sys.argv[1:9]
print(json.dumps({
    "run_id": run_id,
    "window_start": window_start,
    "window_end": window_end,
    "artifact_dir": artifact_dir,
    "workflow_id": workflow_id,
    "workflow_run_id": workflow_run_id,
    "audit_event_id": audit_event_id,
    "audit_sequence": int(audit_sequence),
}, indent=2, sort_keys=True))
PY

echo "temporal proof ok: ${artifact_dir}"
