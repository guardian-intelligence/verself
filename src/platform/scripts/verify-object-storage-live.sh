#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-object-storage-verify-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/object-storage-verify}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
s3_url="https://127.0.0.1:4256"
s3_ca_cert="/etc/forge-metal/local-cas/object-storage-s3-ca.pem"
proof_bin="/opt/forge-metal/profile/bin/object-storage-proof"
garage_admin_port="3903"
garage_failed_instance="1"
garage_failed_admin_port="3913"

wait_for_remote_tcp_as() {
  local user="$1"
  local name="$2"
  local host="$3"
  local port="$4"
  verification_ssh "sudo -u ${user@Q} python3 - <<'PY'
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

assert_clickhouse_zero() {
  local database="$1"
  local query="$2"
  local output_path="$3"
  shift 3
  local extra_args=("$@")
  (
    cd "${VERIFICATION_PLATFORM_ROOT}"
    ./scripts/clickhouse.sh \
      --database "${database}" \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      "${extra_args[@]}" \
      --query "${query}"
  ) >"${output_path}"
  local count
  count="$(tail -n 1 "${output_path}" | tr -d '[:space:]')"
  if [[ ! "${count}" =~ ^[0-9]+$ ]] || (( count != 0 )); then
    echo "ClickHouse zero assertion failed for ${output_path}: got ${count}, expected 0" >&2
    return 1
  fi
}

verification_ssh "sudo systemctl is-active garage@0 garage@1 garage@2 secrets-service governance-service object-storage-admin object-storage-service" \
  >"${artifact_dir}/systemd-active.txt"
verification_wait_for_loopback_api "secrets-service" "http://127.0.0.1:4251/readyz" "200"
verification_wait_for_loopback_api "governance-service" "http://127.0.0.1:4250/readyz" "200"
verification_ssh "sudo systemctl restart object-storage-service"
verification_ssh "for _ in \$(seq 1 60); do code=\$(curl -s -o /dev/null -w '%{http_code}' --cacert '${s3_ca_cert}' '${s3_url}/readyz' || true); if [[ \"\${code}\" == '200' ]]; then exit 0; fi; sleep 1; done; echo 'object-storage-service did not become ready in time' >&2; exit 1"
verification_ssh "curl --fail --silent --show-error --cacert '${s3_ca_cert}' '${s3_url}/healthz'" \
  >"${artifact_dir}/s3-health.txt"
verification_ssh "curl --fail --silent --show-error --cacert '${s3_ca_cert}' '${s3_url}/readyz'" \
  >"${artifact_dir}/s3-ready.txt"

verification_ssh "test ! -e ${proof_bin@Q}"
printf 'absent\n' >"${artifact_dir}/object-storage-proof-binary.txt"

wait_for_remote_tcp_as object_storage_admin "Garage admin" "127.0.0.1" "${garage_admin_port}"

if verification_ssh "sudo -u nobody curl --max-time 2 --silent --show-error 'http://127.0.0.1:${garage_admin_port}/health'" \
  >"${artifact_dir}/garage-direct-nobody.txt" 2>&1; then
  echo "expected direct Garage admin access as nobody to fail" >&2
  exit 1
fi

verification_ssh "sudo systemctl stop garage@${garage_failed_instance}"
verification_ssh "sudo systemctl is-active garage@${garage_failed_instance} || true" \
  >"${artifact_dir}/garage-stopped.txt"

verification_ssh "sudo systemctl start garage@${garage_failed_instance}"
wait_for_remote_tcp_as object_storage_admin "Garage admin restart" "127.0.0.1" "${garage_failed_admin_port}"
verification_ssh "sudo systemctl is-active garage@${garage_failed_instance}" \
  >"${artifact_dir}/garage-restarted.txt"

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'object-storage-service'
    AND (
      SpanAttributes['http.target'] IN ('/healthz', '/readyz')
      OR SpanAttributes['url.path'] IN ('/healthz', '/readyz')
    )
" 2 "${artifact_dir}/clickhouse/object-storage-health-span-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'secrets-service'
    AND SpanName = 'secrets.platform.resolve'
    AND SpanAttributes['forge_metal.runtime_secret_consumer'] = 'object-storage-service'
" 1 "${artifact_dir}/clickhouse/object-storage-secrets-resolve-count.tsv"

assert_clickhouse_zero default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'object-storage-proof'
" "${artifact_dir}/clickhouse/object-storage-proof-traces-count.tsv"

assert_clickhouse_zero default "
  SELECT count()
  FROM otel_logs
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'object-storage-proof'
" "${artifact_dir}/clickhouse/object-storage-proof-logs-count.tsv"

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

echo "object-storage verify ok: ${artifact_dir}"
