#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

kind="${VERIFICATION_KIND:-spiffe-rotation-proof}"
run_id="${VERIFICATION_RUN_ID:-${kind}-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/${kind}}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
marker="fm:spiffe-rotation verify=${run_id}"

verification_ssh "sudo python3 - $(printf '%q' "${window_start}") $(printf '%q' "${marker}")" >"${artifact_dir}/rotation-state.json" <<'PY'
import json
import os
import subprocess
import sys
import time
import urllib.request

window_start, marker = sys.argv[1:3]

def run(args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs)

def output(args):
    return run(args).stdout.strip()

def systemd_value(unit, prop):
    return output(["systemctl", "show", unit, f"--property={prop}", "--value"])

def assert_active(unit):
    state = output(["systemctl", "is-active", unit])
    if state != "active":
        raise SystemExit(f"{unit} is {state}, expected active")

def read(path):
    with open(path, "r", encoding="utf-8") as f:
        return f.read()

active_units = [
    "nats.service",
    "nats-spiffe-helper.service",
    "clickhouse-server.service",
    "clickhouse-server-spiffe-helper.service",
    "clickhouse-operator-spiffe-helper.service",
    "otelcol.service",
    "otelcol-clickhouse-spiffe-helper.service",
    "grafana.service",
    "grafana-clickhouse-spiffe-helper.service",
]
for unit in active_units:
    assert_active(unit)

legacy_units = {}
for unit in ["nats-cert-rotation.path", "nats-cert-rotation.service"]:
    legacy_units[unit] = {
        "unit_file_exists": os.path.exists(f"/etc/systemd/system/{unit}"),
        "active": subprocess.run(["systemctl", "is-active", "--quiet", unit], check=False).returncode == 0,
        "enabled": subprocess.run(["systemctl", "is-enabled", "--quiet", unit], check=False).returncode == 0,
    }
    if any(legacy_units[unit].values()):
        raise SystemExit(f"legacy NATS rotation unit still present or active: {unit} {legacy_units[unit]}")

nats_config = read("/etc/nats/nats-server.conf")
nats_helper_config = read("/etc/nats/nats-spiffe-helper.conf")
clickhouse_config = read("/etc/clickhouse-server/config.d/forge-metal.xml")
clickhouse_helper_config = read("/etc/clickhouse-server/server-spiffe-helper.conf")
grafana_helper_config = read("/etc/grafana/clickhouse-spiffe-helper.conf")
otelcol_config = read("/etc/otelcol/config.yaml")

required_snippets = {
    "nats_config_pid_file": ('pid_file: "/run/nats/nats.pid"', nats_config),
    "nats_helper_pid_file": ('pid_file_name = "/run/nats/nats.pid"', nats_helper_config),
    "nats_helper_signal": ('renew_signal = "SIGHUP"', nats_helper_config),
    "clickhouse_config_spire_bundle": ("/var/lib/clickhouse/spiffe/bundle.pem", clickhouse_config),
    "clickhouse_helper_pid_file": ('pid_file_name = "/run/clickhouse-server/clickhouse-server.pid"', clickhouse_helper_config),
    "clickhouse_helper_signal": ('renew_signal = "SIGHUP"', clickhouse_helper_config),
    "grafana_helper_refresh_cmd": ('cmd = "/usr/local/bin/refresh-grafana-clickhouse-datasource"', grafana_helper_config),
    "otelcol_tls_reload_interval": ("reload_interval: 60s", otelcol_config),
}
for name, (needle, haystack) in required_snippets.items():
    if needle not in haystack:
        raise SystemExit(f"missing {name}: {needle}")

if os.path.exists("/etc/clickhouse-server/tls/client-ca.pem"):
    raise SystemExit("static ClickHouse SPIRE client CA bundle still exists")

material_paths = [
    "/var/lib/nats/spiffe/svid.pem",
    "/var/lib/nats/spiffe/svid_key.pem",
    "/var/lib/nats/spiffe/bundle.pem",
    "/var/lib/clickhouse/spiffe/svid.pem",
    "/var/lib/clickhouse/spiffe/svid_key.pem",
    "/var/lib/clickhouse/spiffe/bundle.pem",
    "/var/lib/clickhouse-operator/spiffe/svid.pem",
    "/var/lib/clickhouse-operator/spiffe/svid_key.pem",
    "/var/lib/clickhouse-operator/spiffe/bundle.pem",
    "/var/lib/otelcol/clickhouse-spiffe/svid.pem",
    "/var/lib/otelcol/clickhouse-spiffe/svid_key.pem",
    "/var/lib/otelcol/clickhouse-spiffe/bundle.pem",
    "/var/lib/grafana/clickhouse-spiffe/svid.pem",
    "/var/lib/grafana/clickhouse-spiffe/svid_key.pem",
    "/var/lib/grafana/clickhouse-spiffe/bundle.pem",
]
materials = {}
for path in material_paths:
    stat = os.stat(path)
    if stat.st_size <= 0:
        raise SystemExit(f"empty SPIFFE material file: {path}")
    materials[path] = {
        "mode": oct(stat.st_mode & 0o777),
        "uid": stat.st_uid,
        "gid": stat.st_gid,
        "bytes": stat.st_size,
        "mtime": stat.st_mtime,
    }

def reload_and_assert_pid(unit):
    pid_before = int(systemd_value(unit, "MainPID"))
    if pid_before <= 0:
        raise SystemExit(f"{unit} has no MainPID before reload")
    run(["systemctl", "reload", unit])
    time.sleep(1)
    pid_after = int(systemd_value(unit, "MainPID"))
    if pid_after != pid_before:
        raise SystemExit(f"{unit} restarted during reload: {pid_before} -> {pid_after}")
    assert_active(unit)
    return {"pid_before": pid_before, "pid_after": pid_after}

nats_reload = reload_and_assert_pid("nats.service")
clickhouse_reload = reload_and_assert_pid("clickhouse-server.service")

with urllib.request.urlopen("http://127.0.0.1:8222/healthz", timeout=5) as response:
    nats_health = {"status": response.status, "body": response.read().decode()}
if nats_health["status"] != 200:
    raise SystemExit(f"NATS health returned {nats_health}")

clickhouse_query = output([
    "sudo",
    "-u",
    "clickhouse_operator",
    "/opt/forge-metal/profile/bin/clickhouse-client",
    "--config-file",
    "/etc/clickhouse-client/operator.xml",
    "--user",
    "clickhouse_operator",
    "--query",
    f"SELECT 1 /* {marker} */",
])
if clickhouse_query != "1":
    raise SystemExit(f"ClickHouse query after reload returned {clickhouse_query!r}")

entries = output([
    "/opt/forge-metal/profile/bin/spire-server",
    "entry",
    "show",
    "-socketPath",
    "/run/spire-server/private/api.sock",
])
expected_ids = [
    "/svc/nats",
    "/svc/clickhouse-server",
    "/svc/clickhouse-operator",
    "/svc/otelcol",
    "/svc/grafana",
]
missing_ids = [suffix for suffix in expected_ids if suffix not in entries]
if missing_ids:
    raise SystemExit("missing SPIRE registrations for " + ", ".join(missing_ids))

payload = {
    "window_start": window_start,
    "active_units": active_units,
    "legacy_units": legacy_units,
    "materials": materials,
    "nats_reload": nats_reload,
    "clickhouse_reload": clickhouse_reload,
    "nats_health": nats_health,
    "clickhouse_query": clickhouse_query,
    "consumer_contracts": {
        "nats": "spiffe-helper pid_file_name + SIGHUP",
        "clickhouse-server": "spiffe-helper trust bundle + SIGHUP",
        "clickhouse-operator": "spiffe-helper file material read per clickhouse-client invocation",
        "otelcol": "spiffe-helper file material + exporter tls.reload_interval",
        "grafana": "spiffe-helper file material + datasource provisioning reload command",
    },
}
print(json.dumps(payload, indent=2, sort_keys=True))
PY

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
  OTLP_TUNNEL_PID="${tunnel_pid}"
  export OTLP_TUNNEL_PID
  trap 'kill "${OTLP_TUNNEL_PID:-}" 2>/dev/null || true; wait "${OTLP_TUNNEL_PID:-}" 2>/dev/null || true' EXIT

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

  export FORGE_METAL_OTLP_ENDPOINT="127.0.0.1:${port}"
  export FORGE_METAL_DEPLOY_RUN_KEY="${run_id}"
  export FORGE_METAL_DEPLOY_KIND="${kind}"
  # shellcheck source=src/platform/scripts/deploy_identity.sh
  source "${script_dir}/deploy_identity.sh"
}

span_attrs() {
  local consumer="$1"
  local strategy="$2"
  python3 - "${run_id}" "${consumer}" "${strategy}" <<'PY'
import json
import sys

run_id, consumer, strategy = sys.argv[1:4]
print(json.dumps({
    "forge_metal.proof_run_id": run_id,
    "workload_identity.consumer": consumer,
    "workload_identity.rotation_strategy": strategy,
}))
PY
}

with_otlp_tunnel

emit_span "workload_identity.rotation.consumer" "$(span_attrs nats "spiffe-helper-pid-sighup")"
emit_span "workload_identity.rotation.reload" "$(span_attrs nats "systemctl-reload-pid-stable")"
emit_span "workload_identity.rotation.consumer" "$(span_attrs clickhouse-server "spiffe-helper-bundle-sighup")"
emit_span "workload_identity.rotation.reload" "$(span_attrs clickhouse-server "systemctl-reload-pid-stable")"
emit_span "workload_identity.rotation.consumer" "$(span_attrs clickhouse-operator "per-invocation-file-read")"
emit_span "workload_identity.rotation.consumer" "$(span_attrs otelcol "otelcol-tls-reload-interval")"
emit_span "workload_identity.rotation.consumer" "$(span_attrs grafana "helper-refresh-command")"

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
        --param_marker="${marker}" \
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
    AND SpanName IN ('workload_identity.rotation.consumer', 'workload_identity.rotation.reload')
    AND SpanAttributes['forge_metal.proof_run_id'] = {run_id:String}
" 7 "${artifact_dir}/clickhouse/rotation-proof-spans-count.tsv"

wait_for_clickhouse_count system "
  SELECT count()
  FROM query_log
  WHERE event_time BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND type = 'QueryFinish'
    AND exception_code = 0
    AND initial_user = 'clickhouse_operator'
    AND query LIKE concat('%', {marker:String}, '%')
" 1 "${artifact_dir}/clickhouse/clickhouse-reload-query-log-count.tsv"

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

echo "SPIFFE rotation proof ok: ${artifact_dir}"
