#!/usr/bin/env bash
# Run the observability smoke playbook and wait for ansible spans in ClickHouse.
set -euo pipefail

cd "$(dirname "$0")/.."

inventory="ansible/inventory/hosts.ini"
if [[ ! -f "${inventory}" ]]; then
  echo "ERROR: ${inventory} not found." >&2
  exit 1
fi

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

output_file="$(mktemp)"
callback_dir="$(mktemp -d)"
trap 'rm -rf "${output_file}" "${callback_dir}"' EXIT
ln -s "$(pwd)/ansible/plugins/callback/deploy_events.py" "${callback_dir}/deploy_events.py"

(
  cd ansible
  export ANSIBLE_CALLBACK_PLUGINS="${callback_dir}"
  ansible-playbook -i inventory/hosts.ini playbooks/observability-smoke.yml
) | tee "${output_file}"

if ! grep -Eq 'deploy_events: .*inserted into ClickHouse|deploy_events: insert failed' "${output_file}"; then
  echo "ERROR: deploy_events callback did not emit its run marker." >&2
  exit 1
fi

for _ in $(seq 1 30); do
  query_output="$(./scripts/clickhouse.sh --database default --query "
    SELECT
      countIf(SpanName = 'ansible.playbook') AS playbooks,
      countIf(SpanName = 'ansible.play') AS plays,
      countIf(SpanName = 'ansible.task') AS tasks
    FROM default.otel_traces
    WHERE ServiceName = 'ansible'
      AND Timestamp > now() - INTERVAL 10 MINUTE
    FORMAT JSONEachRow
  " || true)"
  if printf '%s\n' "${query_output}" | python3 -c '
import json
import sys

lines = [line for line in sys.stdin.read().splitlines() if line.strip()]
row = json.loads(lines[0]) if lines else {}
raise SystemExit(0 if row.get("playbooks", 0) >= 1 and row.get("plays", 0) >= 1 and row.get("tasks", 0) >= 1 else 1)
'; then
    exit 0
  fi
  sleep 1
done

echo "ERROR: timed out waiting for ansible spans in default.otel_traces." >&2
exit 1
