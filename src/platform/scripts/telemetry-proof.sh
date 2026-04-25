#!/usr/bin/env bash
# Prove that Ansible deploys emit ClickHouse-queryable spans and that service
# spans can be joined to the deploy via deterministic correlation.
#
# Flow:
#   1. Derive deterministic deploy identity via scripts/deploy_identity.sh.
#      VERSELF_DEPLOY_ID's UUIDv5 becomes the trace-id shared by the
#      upstream Ansible OTel callback (via TRACEPARENT) and verself_uri probes.
#   2. Run observability-smoke.yml through scripts/ansible-with-tunnel.sh.
#   3. Poll ClickHouse for matching ansible.playbook/ansible.task spans
#      (renamed from the upstream plugin's raw names by the otelcol
#      transform/ansible_spans processor).
#   4. Assert the collector transform copied verself.deploy_id from
#      ResourceAttributes onto SpanAttributes, giving ansible and service
#      spans a shared query shape.
#   5. On the happy path, assert billing-service spans carry matching
#      verself.deploy_id (proves verself_uri traceparent+baggage propagation
#      reached the service via otelhttp + verselfotel baggage span processor).
#
# Env:
#   TELEMETRY_PROOF_EXPECT_FAIL=1 — assert the playbook *failed* and the
#     playbook span has Error status.
set -euo pipefail

cd "$(dirname "$0")/.."

# --- Deterministic deploy identity ------------------------------------------
# Pre-seed the identity helper with a telemetry-proof-specific run counter
# so concurrent runs on the same day don't collide.
run_date="$(date -u +%Y-%m-%d)"
run_host="$(hostname -s 2>/dev/null || hostname)"
counter_dir="${XDG_CACHE_HOME:-$HOME/.cache}/verself/telemetry-proof"
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

# Export so ansible-with-tunnel.sh's deploy_identity.sh picks them up instead
# of generating its own.
export VERSELF_DEPLOY_ID="${deploy_id}"
export VERSELF_DEPLOY_RUN_KEY="${deploy_run_key}"
export VERSELF_VERIFICATION_RUN="${deploy_run_key}"
export VERSELF_CORRELATION_ID="${deploy_id}"
export VERSELF_DEPLOY_KIND="telemetry-proof"

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

# --- Verify spans reached ClickHouse ----------------------------------------
# Trace id = deploy id with dashes stripped (the TRACEPARENT our identity
# helper emits). Every ansible and probe-triggered service span shares it.
trace_id_hex="${deploy_id//-/}"

assert_spans_ready() {
  local query_output="$1"
  FORGE_EXPECT_FAIL="${expect_fail}" python3 -c '
import json, os, sys
expect_fail = os.environ.get("FORGE_EXPECT_FAIL", "0")
payload = sys.argv[1].strip()
row = json.loads(payload.splitlines()[0]) if payload else {}
ready = (
    row.get("playbooks", 0) == 1
    and row.get("tasks", 0) >= 1
    and row.get("deploy_id_attrs", 0) >= 1
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
      countIf(SpanName = 'ansible.task') AS tasks,
      countIf(StatusCode = 'Error') AS errors,
      anyIf(StatusCode, SpanName = 'ansible.playbook') AS root_status,
      countIf(SpanAttributes['verself.deploy_id'] = '${deploy_id}') AS deploy_id_attrs
    FROM default.otel_traces
    WHERE ServiceName = 'ansible'
      AND TraceId = '${trace_id_hex}'
    FORMAT JSONEachRow
  " || true)"
  if assert_spans_ready "${query_output}"; then
    break
  fi
  sleep 1
done

if ! assert_spans_ready "${query_output}"; then
  echo "ERROR: timed out waiting for ansible spans in default.otel_traces." >&2
  printf 'Last query row: %s\n' "${query_output}" >&2
  exit 1
fi

# --- Service-level correlation (happy path only) ---------------------------
# verself_uri probes the billing-service /healthz. otelhttp on the service side
# extracts traceparent + baggage; the verselfotel baggage span processor
# projects verself.* baggage members onto every span the service
# creates. A matching SpanAttributes['verself.deploy_id'] row confirms
# the full pipeline: deploy_identity → verself_uri → otelhttp → baggage → span.
if [[ "${expect_fail}" != "1" ]]; then
  billing_corr_row=""
  for _ in $(seq 1 30); do
    billing_corr_row="$(./scripts/clickhouse.sh --database default --query "
      SELECT count() AS rows
      FROM default.otel_traces
      WHERE ServiceName = 'billing-service'
        AND SpanAttributes['verself.deploy_id'] = '${deploy_id}'
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
    echo "ERROR: billing-service spans missing deterministic deploy correlation." >&2
    printf 'billing correlation row: %s\n' "${billing_corr_row}" >&2
    exit 1
  fi
fi

echo "telemetry-proof: verified deploy_id=${deploy_id} deploy_run_key=${deploy_run_key} trace_id=${trace_id_hex}"
