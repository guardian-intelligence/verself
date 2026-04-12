#!/usr/bin/env bash
# Run the observability smoke playbook and wait for ansible spans in ClickHouse.
set -euo pipefail

cd "$(dirname "$0")/.."

inventory="ansible/inventory/hosts.ini"
if [[ ! -f "${inventory}" ]]; then
  echo "ERROR: ${inventory} not found." >&2
  exit 1
fi

remote_host="$(grep -m1 'ansible_host=' "${inventory}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "${inventory}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"
if [[ -z "${remote_host}" || -z "${remote_user}" ]]; then
  echo "ERROR: failed to resolve ansible_host/ansible_user from ${inventory}." >&2
  exit 1
fi

ssh_opts=(
  -o IPQoS=none
  -o StrictHostKeyChecking=no
  -o ExitOnForwardFailure=yes
  -o ServerAliveInterval=15
  -o ServerAliveCountMax=3
  -o ConnectTimeout=10
)

if ! ssh "${ssh_opts[@]}" "${remote_user}@${remote_host}" "python3 - <<'PY'
import socket
with socket.create_connection(('127.0.0.1', 4317), 2):
    pass
PY"; then
  echo "ERROR: remote OTLP endpoint 127.0.0.1:4317 is not reachable on ${remote_host}." >&2
  exit 1
fi

otlp_local_port="$(python3 - <<'PY'
import socket

sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
)"

run_date="$(date -u +%Y-%m-%d)"
run_host="$(hostname -s 2>/dev/null || hostname)"
counter_dir="${XDG_CACHE_HOME:-$HOME/.cache}/forge-metal/telemetry-proof"
mkdir -p "${counter_dir}"
counter_file="${counter_dir}/${run_date}.counter"
lock_file="${counter_dir}/${run_date}.lock"

run_counter="$(python3 -c '
import fcntl
import pathlib
import sys

counter_path = pathlib.Path(sys.argv[1])
lock_path = pathlib.Path(sys.argv[2])
lock_path.parent.mkdir(parents=True, exist_ok=True)
with lock_path.open("a+") as lock_file:
    fcntl.flock(lock_file, fcntl.LOCK_EX)
    try:
        current = int(counter_path.read_text(encoding="utf-8").strip() or "0")
    except FileNotFoundError:
        current = 0
    except ValueError:
        current = 0
    current += 1
    counter_path.write_text(str(current), encoding="utf-8")
    print(f"{current:06d}")
' "${counter_file}" "${lock_file}")"

deploy_run_key="${run_date}.${run_counter}@${run_host}"
deploy_id="$(python3 -c '
import sys
import uuid

run_key = sys.argv[1]
print(uuid.uuid5(uuid.NAMESPACE_URL, f"forge-metal:{run_key}"))
' "${deploy_run_key}")"

export FORGE_METAL_DEPLOY_ID="${deploy_id}"
export FORGE_METAL_DEPLOY_RUN_KEY="${deploy_run_key}"
export FORGE_METAL_VERIFICATION_RUN="${deploy_run_key}"
export FORGE_METAL_CORRELATION_ID="${deploy_id}"
export FORGE_METAL_DEPLOY_KIND="telemetry-proof"
export FORGE_METAL_OTLP_ENDPOINT="127.0.0.1:${otlp_local_port}"
expect_fail="${TELEMETRY_PROOF_EXPECT_FAIL:-0}"
if [[ "${expect_fail}" == "1" ]]; then
  export EXPECT_FAIL=1
else
  unset EXPECT_FAIL
fi

output_file="$(mktemp)"
callback_dir="$(mktemp -d)"
tunnel_log="$(mktemp)"
tunnel_pid=""
cleanup() {
  if [[ -n "${tunnel_pid}" ]] && kill -0 "${tunnel_pid}" >/dev/null 2>&1; then
    kill "${tunnel_pid}" >/dev/null 2>&1 || true
    wait "${tunnel_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${output_file}" "${callback_dir}"
  rm -f "${tunnel_log}"
}
trap cleanup EXIT
ln -s "$(pwd)/ansible/plugins/callback/deploy_events.py" "${callback_dir}/deploy_events.py"
ln -s "$(pwd)/ansible/plugins/callback/deploy_traces.py" "${callback_dir}/deploy_traces.py"

ssh "${ssh_opts[@]}" \
  -N \
  -L "${otlp_local_port}:127.0.0.1:4317" \
  "${remote_user}@${remote_host}" \
  >"${tunnel_log}" 2>&1 &
tunnel_pid="$!"

for _ in $(seq 1 20); do
  if python3 -c '
import socket, sys
try:
    with socket.create_connection(("127.0.0.1", int(sys.argv[1])), 1):
        pass
except OSError:
    raise SystemExit(1)
' "${otlp_local_port}"; then
    break
  fi
  sleep 0.25
done
if ! python3 -c '
import socket, sys
with socket.create_connection(("127.0.0.1", int(sys.argv[1])), 1):
    pass
' "${otlp_local_port}"; then
  echo "ERROR: OTLP SSH tunnel did not come up on 127.0.0.1:${otlp_local_port}." >&2
  cat "${tunnel_log}" >&2 || true
  exit 1
fi

(
  cd ansible
  export ANSIBLE_CALLBACK_PLUGINS="${callback_dir}"
  set +e
  ansible-playbook -i inventory/hosts.ini playbooks/observability-smoke.yml
  ansible_rc=$?
  set -e
  if [[ "${expect_fail}" == "1" ]]; then
    if [[ "${ansible_rc}" -eq 0 ]]; then
      echo "ERROR: observability-smoke succeeded but TELEMETRY_PROOF_EXPECT_FAIL=1." >&2
      exit 1
    fi
  elif [[ "${ansible_rc}" -ne 0 ]]; then
    echo "ERROR: observability-smoke failed unexpectedly (exit ${ansible_rc})." >&2
    exit "${ansible_rc}"
  fi
) | tee "${output_file}"

if ! grep -Eq 'deploy_events: .*inserted into ClickHouse|deploy_events: insert failed|deploy_events: inserted using legacy schema' "${output_file}"; then
  echo "ERROR: deploy_events callback did not emit its run marker." >&2
  exit 1
fi

for _ in $(seq 1 45); do
  query_output="$(./scripts/clickhouse.sh --database default --query "
    SELECT
      countIf(SpanName = 'ansible.playbook') AS playbooks,
      countIf(SpanName = 'ansible.play') AS plays,
      countIf(SpanName IN ('ansible.task', 'ansible.handler')) AS tasks,
      countIf(StatusCode = 'Error') AS errors,
      anyIf(StatusCode, SpanName = 'ansible.playbook') AS root_status,
      countIf(SpanAttributes['forge_metal.deploy_run_key'] = '${deploy_run_key}') AS run_key_attrs
    FROM default.otel_traces
    WHERE ServiceName = 'ansible'
      AND SpanAttributes['cicd.pipeline.run.id'] = '${deploy_id}'
    FORMAT JSONEachRow
  " || true)"
  if printf '%s\n' "${query_output}" | python3 -c '
import json
import sys

lines = [line for line in sys.stdin.read().splitlines() if line.strip()]
row = json.loads(lines[0]) if lines else {}
ready = (
    row.get("playbooks", 0) == 1
    and row.get("plays", 0) >= 1
    and row.get("tasks", 0) >= 1
    and row.get("run_key_attrs", 0) >= 1
)
if sys.argv[1] == "1":
    ready = ready and row.get("errors", 0) >= 1 and row.get("root_status", "") == "Error"
else:
    ready = ready and row.get("errors", 0) == 0 and row.get("root_status", "") == "Ok"
raise SystemExit(0 if ready else 1)
' "${expect_fail}"; then
    break
  fi
  sleep 1
done

if ! printf '%s\n' "${query_output}" | python3 -c '
import json
import sys

lines = [line for line in sys.stdin.read().splitlines() if line.strip()]
row = json.loads(lines[0]) if lines else {}
ready = (
    row.get("playbooks", 0) == 1
    and row.get("plays", 0) >= 1
    and row.get("tasks", 0) >= 1
    and row.get("run_key_attrs", 0) >= 1
)
if sys.argv[1] == "1":
    ready = ready and row.get("errors", 0) >= 1 and row.get("root_status", "") == "Error"
else:
    ready = ready and row.get("errors", 0) == 0 and row.get("root_status", "") == "Ok"
raise SystemExit(0 if ready else 1)
' "${expect_fail}"; then
  echo "ERROR: timed out waiting for verified ansible spans in default.otel_traces." >&2
  printf 'Last query row: %s\n' "${query_output}" >&2
  exit 1
fi

deploy_event_row="$(./scripts/clickhouse.sh --database forge_metal --query "
  SELECT
    count() AS rows,
    any(trace_id) AS trace_id,
    any(deploy_run_key) AS run_key
  FROM forge_metal.deploy_events
  WHERE deploy_id = '${deploy_id}'
  FORMAT JSONEachRow
" || true)"
if ! printf '%s\n' "${deploy_event_row}" | python3 -c '
import json
import sys

lines = [line for line in sys.stdin.read().splitlines() if line.strip()]
row = json.loads(lines[0]) if lines else {}
ready = (
    row.get("rows", 0) == 1
    and str(row.get("trace_id", "")).strip() != ""
    and str(row.get("run_key", "")).strip() != ""
)
raise SystemExit(0 if ready else 1)
'; then
  echo "ERROR: deploy_events row is missing trace identity fields." >&2
  printf 'deploy_events row: %s\n' "${deploy_event_row}" >&2
  exit 1
fi

if [[ "${expect_fail}" != "1" ]]; then
  for _ in $(seq 1 30); do
    billing_corr_row="$(./scripts/clickhouse.sh --database default --query "
      SELECT
        count() AS rows
      FROM default.otel_traces
      WHERE ServiceName = 'billing-service'
        AND SpanAttributes['forge_metal.deploy_id'] = '${deploy_id}'
        AND Timestamp > now() - INTERVAL 20 MINUTE
      FORMAT JSONEachRow
    " || true)"
    if printf '%s\n' "${billing_corr_row}" | python3 -c '
import json
import sys

lines = [line for line in sys.stdin.read().splitlines() if line.strip()]
row = json.loads(lines[0]) if lines else {}
raise SystemExit(0 if row.get("rows", 0) >= 1 else 1)
'; then
      break
    fi
    sleep 1
  done

  if ! printf '%s\n' "${billing_corr_row}" | python3 -c '
import json
import sys

lines = [line for line in sys.stdin.read().splitlines() if line.strip()]
row = json.loads(lines[0]) if lines else {}
raise SystemExit(0 if row.get("rows", 0) >= 1 else 1)
'; then
    echo "ERROR: billing-service spans missing deterministic deploy correlation attributes." >&2
    printf 'billing correlation row: %s\n' "${billing_corr_row}" >&2
    exit 1
  fi
fi

echo "telemetry-proof: verified deploy_id=${deploy_id} deploy_run_key=${deploy_run_key}"
