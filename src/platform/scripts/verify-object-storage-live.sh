#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-object-storage-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/object-storage-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/postgres"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
trust_domain="spiffe.${VERIFICATION_DOMAIN}"
spire_socket="unix:///run/spire-agent/sockets/agent.sock"
otel_endpoint="http://127.0.0.1:4317"
object_storage_admin_spiffe_id="spiffe://${trust_domain}/svc/object-storage-admin"
secrets_spiffe_id="spiffe://${trust_domain}/svc/secrets-service"
proof_bin="/opt/forge-metal/profile/bin/object-storage-proof"
admin_url="https://127.0.0.1:4257"
s3_url="https://127.0.0.1:4256"
s3_ca_cert="/etc/forge-metal/local-cas/object-storage-s3-ca.pem"
secrets_url="https://127.0.0.1:4253"
garage_s3_urls="http://127.0.0.1:3900,http://127.0.0.1:3910,http://127.0.0.1:3920"
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

remote_object_storage_proof() {
  local user="$1"
  shift
  local argv=(
    sudo -u "${user}"
    env
    "SPIFFE_ENDPOINT_SOCKET=${spire_socket}"
    "OTEL_EXPORTER_OTLP_ENDPOINT=${otel_endpoint}"
    "OBJECT_STORAGE_PROOF_ADMIN_URL=${admin_url}"
    "OBJECT_STORAGE_PROOF_ADMIN_SPIFFE_ID=${object_storage_admin_spiffe_id}"
    "OBJECT_STORAGE_PROOF_S3_URL=${s3_url}"
    "OBJECT_STORAGE_PROOF_S3_CA_CERT=${s3_ca_cert}"
    "OBJECT_STORAGE_PROOF_SECRETS_URL=${secrets_url}"
    "OBJECT_STORAGE_PROOF_SECRETS_SPIFFE_ID=${secrets_spiffe_id}"
    "OBJECT_STORAGE_PROOF_GARAGE_S3_URLS=${garage_s3_urls}"
    "OBJECT_STORAGE_PROOF_GARAGE_REGION=garage"
    "${proof_bin}"
  )
  argv+=("$@")
  local remote_cmd=""
  local arg
  for arg in "${argv[@]}"; do
    remote_cmd+=" $(printf '%q' "${arg}")"
  done
  verification_ssh "bash -lc 'exec${remote_cmd}'"
}

remote_object_storage_proof_with_manifest() {
  local manifest_path="$1"
  local remote_manifest_path
  remote_manifest_path="$(verification_remote_temp_path "object-storage-manifest")"
  verification_ssh "sudo tee $(printf '%q' "${remote_manifest_path}") >/dev/null && sudo chown object_storage_admin:object_storage_admin $(printf '%q' "${remote_manifest_path}")" <"${manifest_path}"
  local status=0
  if remote_object_storage_proof object_storage_admin garage-verify --manifest "${remote_manifest_path}"; then
    status=0
  else
    status=$?
  fi
  verification_remove_remote_path "${remote_manifest_path}"
  return "${status}"
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

verification_ssh "sudo systemctl is-active garage@0 garage@1 garage@2 secrets-service governance-service object-storage-admin object-storage-service" \
  >"${artifact_dir}/systemd-active.txt"
verification_wait_for_loopback_api "secrets-service" "http://127.0.0.1:4251/readyz" "200"
verification_wait_for_loopback_api "governance-service" "http://127.0.0.1:4250/readyz" "200"
verification_ssh "curl --fail --silent --show-error --cacert '${s3_ca_cert}' '${s3_url}/healthz'" \
  >"${artifact_dir}/s3-health.txt"
wait_for_remote_tcp_as object_storage_admin "Garage admin" "127.0.0.1" "${garage_admin_port}"

if verification_ssh "sudo -u nobody curl --max-time 2 --silent --show-error 'http://127.0.0.1:${garage_admin_port}/health'" \
  >"${artifact_dir}/garage-direct-nobody.txt" 2>&1; then
  echo "expected direct Garage admin access as nobody to fail" >&2
  exit 1
fi

remote_object_storage_proof object_storage_admin run --run-id "${run_id}" \
  >"${artifact_dir}/proof-run.json"

remote_object_storage_proof object_storage_admin garage-seed --run-id "${run_id}-garage" \
  >"${artifact_dir}/garage-seed.json"
remote_object_storage_proof_with_manifest "${artifact_dir}/garage-seed.json" \
  >"${artifact_dir}/garage-verify.txt"

verification_ssh "sudo systemctl stop garage@${garage_failed_instance}"
verification_ssh "sudo systemctl is-active garage@${garage_failed_instance} || true" \
  >"${artifact_dir}/garage-stopped.txt"

remote_object_storage_proof object_storage_admin garage-seed --run-id "${run_id}-chaos" \
  >"${artifact_dir}/garage-chaos-seed.json"

verification_ssh "sudo systemctl start garage@${garage_failed_instance}"
wait_for_remote_tcp_as object_storage_admin "Garage admin restart" "127.0.0.1" "${garage_failed_admin_port}"
verification_ssh "sudo systemctl is-active garage@${garage_failed_instance}" \
  >"${artifact_dir}/garage-restarted.txt"

verify_deadline=$((SECONDS + 30))
verify_attempt=0
while true; do
  verify_attempt=$((verify_attempt + 1))
  if remote_object_storage_proof_with_manifest "${artifact_dir}/garage-chaos-seed.json" \
    >"${artifact_dir}/garage-chaos-verify.txt" 2>"${artifact_dir}/garage-chaos-verify.stderr"; then
    printf '%s\n' "${verify_attempt}" >"${artifact_dir}/garage-chaos-verify-attempts.txt"
    break
  fi
  if (( SECONDS >= verify_deadline )); then
    echo "garage recovery verification did not succeed within 30 seconds" >&2
    cat "${artifact_dir}/garage-chaos-verify.stderr" >&2 || true
    exit 1
  fi
  sleep 2
done

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
proof_trace_id="$(
  python3 - "${artifact_dir}/proof-run.json" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["trace_id"])
PY
)"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'object-storage-proof'
    AND SpanName = 'object_storage.proof.run'
    AND SpanAttributes['forge_metal.proof_run_id'] = {run_id:String}
" 1 "${artifact_dir}/clickhouse/object-storage-proof-run-count.tsv" --param_run_id="${run_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'object-storage-service'
    AND TraceId = {trace_id:String}
    AND SpanName = 'object_storage.s3.request'
" 10 "${artifact_dir}/clickhouse/object-storage-s3-span-count.tsv" --param_trace_id="${proof_trace_id}"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM object_access_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND trace_id = {trace_id:String}
" 20 "${artifact_dir}/clickhouse/object-access-events-count.tsv" --param_trace_id="${proof_trace_id}"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM audit_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND trace_id = {trace_id:String}
    AND service_name = 'object-storage-admin'
" 10 "${artifact_dir}/clickhouse/object-storage-audit-count.tsv" --param_trace_id="${proof_trace_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'secrets-service'
    AND SpanName = 'secrets.platform.resolve'
    AND SpanAttributes['spiffe.peer_id'] = {object_storage_admin_spiffe_id:String}
" 3 "${artifact_dir}/clickhouse/secrets-platform-resolve-count.tsv" --param_object_storage_admin_spiffe_id="${object_storage_admin_spiffe_id}"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database forge_metal \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_trace_id="${proof_trace_id}" \
    --query "
      SELECT
        operation,
        auth_mode,
        status,
        count() AS request_count,
        sum(bytes_in) AS bytes_in,
        sum(bytes_out) AS bytes_out
      FROM object_access_events
      WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
        AND trace_id = {trace_id:String}
      GROUP BY operation, auth_mode, status
      ORDER BY operation, auth_mode, status
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/object-access-events.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database forge_metal \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_trace_id="${proof_trace_id}" \
    --query "
      SELECT
        audit_event,
        operation_id,
        operation_type,
        risk_level,
        target_kind,
        target_id,
        actor_spiffe_id,
        result,
        recorded_at
      FROM audit_events
      WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
        AND trace_id = {trace_id:String}
        AND service_name = 'object-storage-admin'
      ORDER BY recorded_at, audit_event
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/object-storage-audit-events.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_trace_id="${proof_trace_id}" \
    --query "
      SELECT
        Timestamp,
        ServiceName,
        SpanName,
        StatusCode,
        SpanAttributes['spiffe.peer_id'] AS spiffe_peer_id,
        SpanAttributes['forge_metal.org_id'] AS org_id,
        SpanAttributes['forge_metal.object_storage.operation'] AS object_storage_operation
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
        AND TraceId = {trace_id:String}
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/object-storage-trace.tsv"

python3 - "${artifact_dir}/clickhouse/object-access-events.tsv" <<'PY'
import csv
import sys

rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
if not rows:
    raise SystemExit("object_access_events.tsv was empty")
static_ok = sum(int(row["request_count"]) for row in rows if row["auth_mode"] == "sigv4_static" and row["status"].startswith("2"))
mtls_ok = sum(int(row["request_count"]) for row in rows if row["auth_mode"] == "spiffe_mtls" and row["status"].startswith("2"))
deny = sum(int(row["request_count"]) for row in rows if row["status"] == "403")
ops = {row["operation"] for row in rows}
required = {
    "PutObject",
    "HeadObject",
    "GetObject",
    "ListObjectsV2",
    "CreateMultipartUpload",
    "UploadPart",
    "ListParts",
    "CompleteMultipartUpload",
    "ListMultipartUploads",
    "AbortMultipartUpload",
    "DeleteObject",
}
missing = sorted(required - ops)
if static_ok < 8:
    raise SystemExit(f"expected at least 8 successful static requests, found {static_ok}")
if mtls_ok < 8:
    raise SystemExit(f"expected at least 8 successful mTLS requests, found {mtls_ok}")
if deny < 3:
    raise SystemExit(f"expected at least 3 denied requests, found {deny}")
if missing:
    raise SystemExit(f"missing expected object access operations: {missing}")
PY

python3 - "${artifact_dir}/clickhouse/object-storage-audit-events.tsv" <<'PY'
import csv
import sys

rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
if len(rows) < 10:
    raise SystemExit(f"expected at least 10 object-storage audit rows, found {len(rows)}")
events = {row["audit_event"] for row in rows}
required = {
    "object_storage.bucket.create",
    "object_storage.bucket.update",
    "object_storage.bucket_alias.create",
    "object_storage.access_key.create",
    "object_storage.mtls_principal.create",
    "object_storage.access_key.revoke",
    "object_storage.bucket_alias.delete",
    "object_storage.bucket.delete",
}
missing = sorted(required - events)
if missing:
    raise SystemExit(f"missing expected audit events: {missing}")
PY

echo "object-storage proof ok: ${artifact_dir}"
