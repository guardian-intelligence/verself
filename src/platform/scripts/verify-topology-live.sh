#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

run_date="$(date -u +%Y-%m-%d)"
run_host="$(hostname -s 2>/dev/null || hostname)"
counter_dir="${XDG_CACHE_HOME:-$HOME/.cache}/verself/topology-proof"
counter_file="${counter_dir}/${run_date}.counter"
lock_file="${counter_dir}/${run_date}.lock"
mkdir -p "${counter_dir}"

run_counter="$(python3 - "${counter_file}" "${lock_file}" <<'PY'
import fcntl
import pathlib
import sys

counter_path = pathlib.Path(sys.argv[1])
lock_path = pathlib.Path(sys.argv[2])
with lock_path.open("a+") as lock_file:
    fcntl.flock(lock_file, fcntl.LOCK_EX)
    try:
        current = int(counter_path.read_text(encoding="utf-8").strip() or "0")
    except (FileNotFoundError, ValueError):
        current = 0
    current += 1
    counter_path.write_text(str(current), encoding="utf-8")
    print(f"{current:06d}")
PY
)"

deploy_run_key="${run_date}.${run_counter}@${run_host}"
deploy_id="$(python3 -c 'import sys, uuid; print(uuid.uuid5(uuid.NAMESPACE_URL, f"verself:{sys.argv[1]}"))' "${deploy_run_key}")"

export VERSELF_DEPLOY_ID="${deploy_id}"
export VERSELF_DEPLOY_RUN_KEY="${deploy_run_key}"
export VERSELF_VERIFICATION_RUN="${deploy_run_key}"
export VERSELF_CORRELATION_ID="${deploy_id}"
export VERSELF_DEPLOY_KIND="topology-proof"

./scripts/ansible-with-tunnel.sh playbooks/topology-proof.yml

assert_ready() {
  local query_output="$1"
  python3 -c '
import json
import sys

payload = sys.argv[1].strip()
row = json.loads(payload.splitlines()[0]) if payload else {}
ready = (
    row.get("playbooks", 0) == 1
    and row.get("tasks", 0) >= 1
    and row.get("fmt_checks", 0) >= 1
    and row.get("schema_vets", 0) >= 1
    and row.get("instance_vets", 0) >= 1
    and row.get("graph_validations", 0) >= 1
    and row.get("artifact_exports", 0) >= 10
    and row.get("fresh_checks", 0) >= 10
    and row.get("errors", 0) == 0
)
raise SystemExit(0 if ready else 1)
' "${query_output}"
}

query_output=""
for _ in $(seq 1 45); do
  query_output="$(./scripts/clickhouse.sh --database default --query "
    SELECT
      countIf(ServiceName = 'ansible' AND SpanName = 'ansible.playbook') AS playbooks,
      countIf(ServiceName = 'ansible' AND SpanName = 'ansible.task') AS tasks,
      countIf(ServiceName = 'topology-compiler' AND SpanName = 'topology.cue.fmt_check') AS fmt_checks,
      countIf(ServiceName = 'topology-compiler' AND SpanName = 'topology.cue.vet_schema') AS schema_vets,
      countIf(ServiceName = 'topology-compiler' AND SpanName = 'topology.cue.vet_instance') AS instance_vets,
      countIf(ServiceName = 'topology-compiler' AND SpanName = 'topology.graph.validate') AS graph_validations,
      countIf(ServiceName = 'topology-compiler' AND SpanName = 'topology.cue.export_artifact') AS artifact_exports,
      countIf(ServiceName = 'topology-compiler' AND SpanName = 'topology.generated.freshness_check' AND SpanAttributes['topology.generated_fresh'] = 'true') AS fresh_checks,
      countIf(StatusCode = 'Error') AS errors
    FROM default.otel_traces
    WHERE Timestamp > now() - INTERVAL 20 MINUTE
      AND (
        SpanAttributes['verself.deploy_id'] = '${deploy_id}'
        OR ResourceAttributes['verself.deploy_id'] = '${deploy_id}'
      )
    FORMAT JSONEachRow
  " || true)"
  if assert_ready "${query_output}"; then
    echo "topology-proof: verified deploy_id=${deploy_id} deploy_run_key=${deploy_run_key}"
    exit 0
  fi
  sleep 1
done

echo "ERROR: timed out waiting for topology proof spans in default.otel_traces." >&2
printf 'Last query row: %s\n' "${query_output}" >&2
exit 1
