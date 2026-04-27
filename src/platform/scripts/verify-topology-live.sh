#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

run_date="$(date -u +%Y-%m-%d)"
run_host="$(hostname -s 2>/dev/null || hostname)"
counter_dir="${XDG_CACHE_HOME:-$HOME/.cache}/verself/topology-smoke-test"
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
export VERSELF_DEPLOY_KIND="topology-smoke-test"

./scripts/ansible-with-tunnel.sh playbooks/topology-smoke-test.yml

assert_ready() {
  local query_output="$1"
  python3 -c '
import json
import re
import sys

payload = sys.argv[1].strip()
row = json.loads(payload.splitlines()[0]) if payload else {}
graph_sha = row.get("graph_sha", "")
clusters_sha = row.get("clusters_sha", "")
ready = (
    row.get("playbooks", 0) == 1
    and row.get("tasks", 0) >= 1
    and row.get("root_spans", 0) == 1
    and row.get("export_spans", 0) == 1
    and row.get("fresh_checks", 0) >= 1
    and row.get("clusters_fresh_checks", 0) == 1
    and bool(re.fullmatch(r"[0-9a-f]{64}", graph_sha))
    and bool(re.fullmatch(r"[0-9a-f]{64}", clusters_sha))
    and row.get("errors", 0) == 0
)
raise SystemExit(0 if ready else 1)
' "${query_output}"
}

query_output=""
for _ in $(seq 1 45); do
  query_output="$(./scripts/clickhouse.sh \
    --database default \
    --param_deploy_id "${deploy_id}" \
    --query "
    SELECT
      countIf(ServiceName = 'ansible' AND SpanName = 'ansible.playbook') AS playbooks,
      countIf(ServiceName = 'ansible' AND SpanName = 'ansible.task') AS tasks,
      countIf(ServiceName = 'cue-renderer' AND SpanName = 'cue_renderer.run') AS root_spans,
      countIf(ServiceName = 'cue-renderer' AND SpanName = 'topology.cue.export_graph') AS export_spans,
      countIf(ServiceName = 'cue-renderer' AND SpanName = 'topology.generated.freshness_check') AS fresh_checks,
      countIf(
        ServiceName = 'cue-renderer'
        AND SpanName = 'topology.generated.freshness_check'
        AND SpanAttributes['topology.artifact'] = 'clusters'
        AND SpanAttributes['topology.generated_fresh'] = 'true'
      ) AS clusters_fresh_checks,
      anyIf(
        SpanAttributes['topology.graph_sha256'],
        ServiceName = 'cue-renderer' AND SpanName = 'cue_renderer.run'
      ) AS graph_sha,
      anyIf(
        SpanAttributes['topology.generated_sha256'],
        ServiceName = 'cue-renderer'
          AND SpanName = 'topology.generated.freshness_check'
          AND SpanAttributes['topology.artifact'] = 'clusters'
      ) AS clusters_sha,
      countIf(StatusCode = 'Error') AS errors
    FROM default.otel_traces
    WHERE Timestamp > now() - INTERVAL 20 MINUTE
      AND (
        SpanAttributes['verself.deploy_id'] = {deploy_id:String}
        OR ResourceAttributes['verself.deploy_id'] = {deploy_id:String}
      )
    FORMAT JSONEachRow
  " || true)"
  if assert_ready "${query_output}"; then
    echo "topology-smoke-test: verified deploy_id=${deploy_id} deploy_run_key=${deploy_run_key}"
    exit 0
  fi
  sleep 1
done

echo "ERROR: timed out waiting for topology smoke-test spans in default.otel_traces." >&2
printf 'Last query row: %s\n' "${query_output}" >&2
exit 1
