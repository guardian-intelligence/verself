#!/usr/bin/env bash
# Prove that Ansible deploys emit ClickHouse-queryable spans and that service
# spans can be joined to the deploy via deterministic correlation attributes.
#
# Flow:
#   1. Generate deterministic deploy_id + deploy_run_key (the deploy_traces
#      callback uses these as span identity; the verify step then queries by
#      the same IDs).
#   2. Run observability-smoke.yml through scripts/ansible-with-tunnel.sh so
#      the OTLP tunnel + FORGE_METAL_OTLP_ENDPOINT are set up consistently
#      with regular `make deploy`.
#   3. Poll ClickHouse for matching ansible.playbook/play/task spans, assert
#      their status matches expectations (ok on the happy path, error on
#      TELEMETRY_PROOF_EXPECT_FAIL=1).
#   4. Assert forge_metal.deploy_events has a row with the same trace id.
#   5. On the happy path, assert billing-service spans carry the matching
#      forge_metal.deploy_id (confirms fm_uri trace propagation works).
#
# Env:
#   TELEMETRY_PROOF_EXPECT_FAIL=1 — assert the playbook *failed* and the root
#     span has Error status. Used by `make telemetry-proof-fail`.
set -euo pipefail

cd "$(dirname "$0")/.."

# --- Deterministic deploy identity ------------------------------------------
# Counter + lock file give each run on a given day a monotonically-increasing
# suffix so deploy_run_key is unique per run, which the deploy_traces callback
# hashes into the trace_id.
run_date="$(date -u +%Y-%m-%d)"
run_host="$(hostname -s 2>/dev/null || hostname)"
counter_dir="${XDG_CACHE_HOME:-$HOME/.cache}/forge-metal/telemetry-proof"
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
deploy_id="$(python3 -c '
import sys, uuid
print(uuid.uuid5(uuid.NAMESPACE_URL, f"forge-metal:{sys.argv[1]}"))
' "${deploy_run_key}")"

export FORGE_METAL_DEPLOY_ID="${deploy_id}"
export FORGE_METAL_DEPLOY_RUN_KEY="${deploy_run_key}"
export FORGE_METAL_VERIFICATION_RUN="${deploy_run_key}"
export FORGE_METAL_CORRELATION_ID="${deploy_id}"
export FORGE_METAL_DEPLOY_KIND="telemetry-proof"

expect_fail="${TELEMETRY_PROOF_EXPECT_FAIL:-0}"
if [[ "${expect_fail}" == "1" ]]; then
  export EXPECT_FAIL=1
else
  unset EXPECT_FAIL || true
fi

# --- Run the smoke playbook -------------------------------------------------
output_file="$(mktemp)"
trap 'rm -f "${output_file}"' EXIT

set +e
./scripts/ansible-with-tunnel.sh playbooks/observability-smoke.yml 2>&1 | tee "${output_file}"
ansible_rc=${PIPESTATUS[0]}
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

if ! grep -Eq 'deploy_events: .*inserted into ClickHouse|deploy_events: insert failed|deploy_events: inserted using legacy schema' "${output_file}"; then
  echo "ERROR: deploy_events callback did not emit its run marker." >&2
  exit 1
fi

# --- Verify spans reached ClickHouse ----------------------------------------
assert_spans_ready() {
  local query_output="$1"
  FORGE_EXPECT_FAIL="${expect_fail}" python3 -c '
import json, os, sys
expect_fail = os.environ.get("FORGE_EXPECT_FAIL", "0")
payload = sys.argv[1].strip()
row = json.loads(payload.splitlines()[0]) if payload else {}
ready = (
    row.get("playbooks", 0) == 1
    and row.get("plays", 0) >= 1
    and row.get("tasks", 0) >= 1
    and row.get("run_key_attrs", 0) >= 1
)
if expect_fail == "1":
    ready = ready and row.get("errors", 0) >= 1 and row.get("root_status", "") == "Error"
else:
    ready = ready and row.get("errors", 0) == 0 and row.get("root_status", "") == "Ok"
raise SystemExit(0 if ready else 1)
' "${query_output}"
}

query_output=""
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
  if assert_spans_ready "${query_output}"; then
    break
  fi
  sleep 1
done

if ! assert_spans_ready "${query_output}"; then
  echo "ERROR: timed out waiting for verified ansible spans in default.otel_traces." >&2
  printf 'Last query row: %s\n' "${query_output}" >&2
  exit 1
fi

# deploy_events row from this run must carry trace_id + run_key. Filter by a
# recent time window so counter-file rollover collisions from older runs
# don't trip the check.
deploy_event_row="$(./scripts/clickhouse.sh --database forge_metal --query "
  SELECT
    count() AS rows,
    any(trace_id) AS trace_id,
    any(deploy_run_key) AS run_key
  FROM forge_metal.deploy_events
  WHERE deploy_id = '${deploy_id}'
    AND started_at >= now() - INTERVAL 10 MINUTE
  FORMAT JSONEachRow
" || true)"
if ! printf '%s\n' "${deploy_event_row}" | python3 -c '
import json, sys
lines = [line for line in sys.stdin.read().splitlines() if line.strip()]
row = json.loads(lines[0]) if lines else {}
raise SystemExit(0 if row.get("rows", 0) >= 1 and str(row.get("trace_id","")).strip() and str(row.get("run_key","")).strip() else 1)
'; then
  echo "ERROR: deploy_events row is missing trace identity fields." >&2
  printf 'deploy_events row: %s\n' "${deploy_event_row}" >&2
  exit 1
fi

# Happy path only: service spans must carry the deploy_id propagated via
# fm_uri's X-Forge-Metal-* headers + traceparent.
if [[ "${expect_fail}" != "1" ]]; then
  billing_corr_row=""
  for _ in $(seq 1 30); do
    billing_corr_row="$(./scripts/clickhouse.sh --database default --query "
      SELECT count() AS rows
      FROM default.otel_traces
      WHERE ServiceName = 'billing-service'
        AND SpanAttributes['forge_metal.deploy_id'] = '${deploy_id}'
        AND Timestamp > now() - INTERVAL 20 MINUTE
      FORMAT JSONEachRow
    " || true)"
    if printf '%s\n' "${billing_corr_row}" | python3 -c '
import json, sys
lines = [line for line in sys.stdin.read().splitlines() if line.strip()]
row = json.loads(lines[0]) if lines else {}
raise SystemExit(0 if row.get("rows", 0) >= 1 else 1)
'; then
      break
    fi
    sleep 1
  done

  if ! printf '%s\n' "${billing_corr_row}" | python3 -c '
import json, sys
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
