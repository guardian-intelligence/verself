#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-openbao-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_PROOF_ARTIFACT_ROOT}/openbao-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

verification_ssh "sudo python3 -c '
import json
import os
import ssl
import subprocess
import sys
import time
import uuid
import urllib.error
import urllib.request

base = \"https://127.0.0.1:8200\"
ctx = ssl._create_unverified_context()

def get_json(path, token=None, headers=None):
    headers = dict(headers or {})
    if token:
        headers[\"X-Vault-Token\"] = token
    request = urllib.request.Request(base + path, headers=headers)
    with urllib.request.urlopen(request, context=ctx, timeout=5) as response:
        return json.loads(response.read().decode())

def get_json_status(path, token=None):
    headers = {}
    if token:
        headers[\"X-Vault-Token\"] = token
    request = urllib.request.Request(base + path, headers=headers)
    try:
        with urllib.request.urlopen(request, context=ctx, timeout=5) as response:
            return response.status, json.loads(response.read().decode())
    except urllib.error.HTTPError as exc:
        return exc.code, {}

def get_text(path):
    request = urllib.request.Request(base + path)
    with urllib.request.urlopen(request, context=ctx, timeout=5) as response:
        return response.read().decode()

token = open(\"/etc/credstore/openbao/root-token\", encoding=\"utf-8\").read().strip()
health = get_json(\"/v1/sys/health\")
metrics = get_text(\"/v1/sys/metrics?format=prometheus\")
audited_headers = get_json(\"/v1/sys/config/auditing/request-headers\", token).get(\"headers\", {})
audited_headers_lower = {key.lower(): value for key, value in audited_headers.items()}
correlation_header = \"openbao-proof:\" + str(uuid.uuid4())
mounts_response = get_json(\"/v1/sys/mounts\", token, {\"X-Verself-Request-Id\": correlation_header})
mounts = sorted(mounts_response.get(\"data\", mounts_response).keys())
legacy_internal_token_status, _ = get_json_status(\"/v1/platform-internal/data/service-credentials/secrets-service/internal-injection-token\", token)
legacy_internal_token_metadata_status, _ = get_json_status(\"/v1/platform-internal/metadata/service-credentials/secrets-service/internal-injection-token\", token)
unsafe_bootstrap_statuses = {
    \"openbao-root-token\": get_json_status(\"/v1/platform-internal/data/openbao/root-token\", token)[0],
    \"openbao-unseal-key-1\": get_json_status(\"/v1/platform-internal/data/openbao/unseal-key-1\", token)[0],
    \"zitadel-masterkey\": get_json_status(\"/v1/platform-internal/data/zitadel/masterkey\", token)[0],
}
unsafe_bootstrap_metadata_statuses = {
    \"openbao-root-token\": get_json_status(\"/v1/platform-internal/metadata/openbao/root-token\", token)[0],
    \"openbao-unseal-key-1\": get_json_status(\"/v1/platform-internal/metadata/openbao/unseal-key-1\", token)[0],
    \"zitadel-masterkey\": get_json_status(\"/v1/platform-internal/metadata/zitadel/masterkey\", token)[0],
}
systemd_state = subprocess.check_output([\"systemctl\", \"is-active\", \"openbao\"], text=True).strip()
nft_ruleset = subprocess.check_output([\"nft\", \"list\", \"ruleset\"], text=True)
credential_stats = {}
for name in [\"root-token\", \"unseal-key-1\", \"unseal-key-2\", \"unseal-key-3\"]:
    stat = os.stat(\"/etc/credstore/openbao/\" + name)
    credential_stats[name] = {
        \"mode\": oct(stat.st_mode & 0o777),
        \"uid\": stat.st_uid,
        \"gid\": stat.st_gid,
        \"bytes\": stat.st_size,
    }
audit_stat = os.stat(\"/var/log/openbao/audit.log\")
for _ in range(10):
    audit_grep = subprocess.run(
        [\"grep\", \"-F\", correlation_header, \"/var/log/openbao/audit.log\"],
        text=True,
        capture_output=True,
        check=False,
    )
    if audit_grep.returncode == 0:
        break
    time.sleep(0.2)
payload = {
    \"health\": health,
    \"metrics_has_unsealed\": \"vault_core_unsealed\" in metrics or \"vault.core.unsealed\" in metrics,
    \"mounts\": mounts,
    \"internal_service_credentials\": {
        \"mount_present\": \"platform-internal/\" in mounts,
        \"legacy_injection_token_status\": legacy_internal_token_status,
        \"legacy_injection_token_metadata_status\": legacy_internal_token_metadata_status,
        \"unsafe_bootstrap_statuses\": unsafe_bootstrap_statuses,
        \"unsafe_bootstrap_metadata_statuses\": unsafe_bootstrap_metadata_statuses,
    },
    \"systemd_state\": systemd_state,
    \"nft_has_loopback_drop\": \"tcp dport { 8200, 8201 } iifname != \\\"lo\\\" drop\" in nft_ruleset,
    \"credential_stats\": credential_stats,
    \"audit_log_bytes\": audit_stat.st_size,
    \"audit_request_header\": audited_headers_lower.get(\"x-verself-request-id\", {}),
    \"audit_header_correlation\": correlation_header,
}
if not health.get(\"initialized\") or health.get(\"sealed\") or health.get(\"standby\"):
    raise SystemExit(\"OpenBao health is not initialized, unsealed, active\")
if systemd_state != \"active\":
    raise SystemExit(\"openbao systemd unit is not active\")
if not payload[\"metrics_has_unsealed\"]:
    raise SystemExit(\"OpenBao Prometheus metrics did not include an unsealed gauge\")
if not payload[\"nft_has_loopback_drop\"]:
    raise SystemExit(\"OpenBao nftables loopback-only rule was not present\")
required_mounts = {\"cubbyhole/\", \"identity/\", \"sys/\"}
if not required_mounts.issubset(set(mounts)):
    raise SystemExit(f\"OpenBao required system mounts missing: {mounts}\")
if \"platform-internal/\" in mounts:
    if legacy_internal_token_status != 404 or legacy_internal_token_metadata_status != 404:
        raise SystemExit(\"legacy OpenBao internal injection token document or metadata is still present\")
    for name, status in unsafe_bootstrap_statuses.items():
        if status != 404:
            raise SystemExit(f\"unsafe bootstrap credential {name} was unexpectedly present in OpenBao\")
    for name, status in unsafe_bootstrap_metadata_statuses.items():
        if status != 404:
            raise SystemExit(f\"unsafe bootstrap credential metadata {name} was unexpectedly present in OpenBao\")
for name, stat in credential_stats.items():
    if stat[\"mode\"] != \"0o640\" or stat[\"bytes\"] <= 0:
        raise SystemExit(f\"OpenBao credential {name} has bad mode or is empty\")
if audit_stat.st_size <= 0:
    raise SystemExit(\"OpenBao audit log is empty\")
if payload[\"audit_request_header\"].get(\"hmac\") is not False:
    raise SystemExit(\"OpenBao is not auditing X-Verself-Request-Id in plaintext\")
if audit_grep.returncode != 0:
    raise SystemExit(\"OpenBao audit log did not contain the proof request correlation header\")
json.dump(payload, sys.stdout, indent=2, sort_keys=True)
print()
'" >"${artifact_dir}/openbao-remote-state.json"

emit_span() {
  local span_name="$1"
  local attrs_json="$2"
  (
    cd "${VERIFICATION_REPO_ROOT}"
    PROOF_SPAN_SERVICE="platform-ansible" \
    PROOF_SPAN_NAME="${span_name}" \
    PROOF_SPAN_ATTRS_JSON="${attrs_json}" \
      go run ./src/otel/cmd/proof-span
  )
}

with_otlp_tunnel() {
  local port
  port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
  ssh -N \
    -o ExitOnForwardFailure=yes \
    -o ServerAliveInterval=15 \
    -o ServerAliveCountMax=3 \
    -o StrictHostKeyChecking=no \
    -L "${port}:127.0.0.1:4317" \
    "${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}" </dev/null >/dev/null 2>&1 &
  local tunnel_pid=$!
  trap 'kill "${tunnel_pid}" 2>/dev/null || true; wait "${tunnel_pid}" 2>/dev/null || true' RETURN

  for _ in $(seq 1 20); do
    if python3 -c "import socket; socket.create_connection(('127.0.0.1', ${port}), 1).close()" 2>/dev/null; then
      break
    fi
    sleep 0.25
  done
  if ! python3 -c "import socket; socket.create_connection(('127.0.0.1', ${port}), 1).close()" 2>/dev/null; then
    echo "ERROR: OTLP tunnel to ${VERIFICATION_REMOTE_HOST} did not come up on 127.0.0.1:${port}" >&2
    return 1
  fi

  export VERSELF_OTLP_ENDPOINT="127.0.0.1:${port}"
  export VERSELF_DEPLOY_RUN_KEY="${run_id}"
  export VERSELF_DEPLOY_KIND="openbao-proof"
  # shellcheck source=src/platform/scripts/deploy_identity.sh
  source "${script_dir}/deploy_identity.sh"

  local attrs
  attrs="$(python3 - "${run_id}" "${artifact_dir}/openbao-remote-state.json" <<'PY'
import json
import sys

run_id, state_path = sys.argv[1:3]
state = json.load(open(state_path, encoding="utf-8"))
print(json.dumps({
    "verself.proof_run_id": run_id,
    "bao.sealed": bool(state["health"].get("sealed")),
    "bao.active": not bool(state["health"].get("standby")),
    "bao.version": state["health"].get("version", ""),
}))
PY
)"
  emit_span "openbao.bootstrap.init" "${attrs}"
  emit_span "openbao.bootstrap.unseal" "${attrs}"
  emit_span "openbao.bootstrap.ready" "${attrs}"
}

with_otlp_tunnel
window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

wait_for_clickhouse_count() {
  local database="$1"
  local query="$2"
  local min_count="$3"
  local output_path="$4"
  local count="0"
  for _ in $(seq 1 45); do
    (
      cd "${VERIFICATION_PLATFORM_ROOT}"
      ./scripts/clickhouse.sh \
        --database "${database}" \
        --param_window_start="${window_start}" \
        --param_window_end="${window_end}" \
        --param_run_id="${run_id}" \
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

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'platform-ansible'
    AND SpanName IN ('openbao.bootstrap.init', 'openbao.bootstrap.unseal', 'openbao.bootstrap.ready')
    AND SpanAttributes['verself.proof_run_id'] = {run_id:String}
" 3 "${artifact_dir}/clickhouse/openbao-proof-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT countDistinct(MetricName)
  FROM otel_metrics_gauge
  WHERE TimeUnix > now() - INTERVAL 30 MINUTE
    AND ServiceName = 'openbao'
    AND (
      (MetricName = 'up' AND Value = 1)
      OR (MetricName = 'vault_core_unsealed' AND Value = 1)
    )
" 2 "${artifact_dir}/clickhouse/openbao-metrics-count.tsv"

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

echo "openbao proof ok: ${artifact_dir}"
