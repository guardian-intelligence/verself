#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-recurring-schedule-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/recurring-schedule-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/payloads" "${artifact_dir}/postgres" "${artifact_dir}/responses"

interval_seconds="${RECURRING_SCHEDULE_INTERVAL_SECONDS:-15}"
paused_probe_seconds="${RECURRING_SCHEDULE_PAUSED_PROBE_SECONDS:-20}"
execution_timeout_seconds="${RECURRING_SCHEDULE_EXECUTION_TIMEOUT_SECONDS:-240}"
clickhouse_timeout_seconds="${RECURRING_SCHEDULE_CLICKHOUSE_TIMEOUT_SECONDS:-180}"
proof_persona="${RECURRING_SCHEDULE_PERSONA:-platform-admin}"
proof_log_marker="${RECURRING_SCHEDULE_LOG_MARKER:-forge-metal-recurring-proof}"
proof_log_line="${proof_log_marker} run_id=${run_id} from=temporal-schedule"
api_base_url="${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}"
api_base_url="${api_base_url%/}"
window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
trust_domain="spiffe.${VERIFICATION_DOMAIN}"
sandbox_service_spiffe_id="spiffe://${trust_domain}/svc/sandbox-rental-service"

case "${proof_persona}" in
  platform-admin)
    proof_billing_email="ceo@${VERIFICATION_DOMAIN}"
    proof_billing_org="platform"
    ;;
  acme-admin | acme-member)
    proof_billing_email="acme-admin@${VERIFICATION_DOMAIN}"
    proof_billing_org="Acme Corp"
    ;;
  *)
    echo "unsupported RECURRING_SCHEDULE_PERSONA=${proof_persona}" >&2
    exit 1
    ;;
esac

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" "${proof_persona}" --print)

billing_fixture_path="${artifact_dir}/billing-fixture.json"
"${script_dir}/set-user-state.sh" \
  --email "${proof_billing_email}" \
  --org "${proof_billing_org}" \
  --product-id "sandbox" \
  --state "pro" \
  --balance-units "500000000000" >"${billing_fixture_path}"

org_id="$(
  python3 - "${billing_fixture_path}" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["org_id"])
PY
)"

remote_psql() {
  local db="$1"
  local sql="$2"
  verification_ssh "sudo -u postgres psql -d ${db} -X -A -t -F \$'\\t' -P footer=off -c \"$sql\""
}

api_request() {
  local method="$1"
  local path="$2"
  local output_path="$3"
  local body_path="${4:-}"
  local idempotency_key="${5:-}"
  local curl_args=(
    -fsS
    -X "${method}"
    -H "Authorization: Bearer ${SANDBOX_RENTAL_TOKEN}"
    -H "baggage: forge_metal.verification_run=${run_id}"
  )
  if [[ -n "${body_path}" ]]; then
    curl_args+=(
      -H "Content-Type: application/json"
      --data-binary "@${body_path}"
    )
  fi
  if [[ -n "${idempotency_key}" ]]; then
    curl_args+=(-H "Idempotency-Key: ${idempotency_key}")
  fi
  curl "${curl_args[@]}" "${api_base_url}${path}" >"${output_path}"
}

wait_for_clickhouse_count() {
  local database="$1"
  local query="$2"
  local min_count="$3"
  local output_path="$4"
  shift 4
  local extra_args=("$@")
  local count="0"
  local attempts=$((clickhouse_timeout_seconds / 2))
  if (( attempts < 1 )); then
    attempts=1
  fi
  for _ in $(seq 1 "${attempts}"); do
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

verification_wait_for_loopback_api "sandbox-rental-service" \
  "http://127.0.0.1:4243/api/v1/billing/entitlements" "401"

payload_path="${artifact_dir}/payloads/create-schedule.json"
python3 - "${run_id}" "${proof_log_line}" "${interval_seconds}" >"${payload_path}" <<'PY'
import json
import sys

run_id, log_line, interval_seconds = sys.argv[1:4]
print(json.dumps({
    "display_name": f"Recurring proof {run_id}",
    "idempotency_key": run_id,
    "interval_seconds": int(interval_seconds),
    "max_wall_seconds": 120,
    "paused": True,
    "run_command": f"printf '{log_line}\\n'",
}, indent=2, sort_keys=True))
PY

api_request "POST" "/api/v1/execution-schedules" "${artifact_dir}/responses/create-schedule.json" "${payload_path}"

read -r schedule_id temporal_schedule_id created_state <<<"$(
  python3 - "${artifact_dir}/responses/create-schedule.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload["schedule_id"], payload["temporal_schedule_id"], payload["state"])
PY
)"

if [[ "${created_state}" != "paused" ]]; then
  echo "expected newly created recurring schedule to start paused, got ${created_state}" >&2
  exit 1
fi

api_request "GET" "/api/v1/execution-schedules" "${artifact_dir}/responses/list-before-resume.json"

list_before_match="$(
  python3 - "${artifact_dir}/responses/list-before-resume.json" "${schedule_id}" <<'PY'
import json
import sys

schedule_id = sys.argv[2]
for item in json.load(open(sys.argv[1], encoding="utf-8")):
    if item.get("schedule_id") == schedule_id:
        print(len(item.get("dispatches") or []), item.get("state", ""))
        raise SystemExit(0)
raise SystemExit(f"schedule {schedule_id} missing from list response")
PY
)"
read -r list_before_dispatches list_before_state <<<"${list_before_match}"
if [[ "${list_before_dispatches}" != "0" || "${list_before_state}" != "paused" ]]; then
  echo "expected paused recurring schedule to list with zero dispatches before resume" >&2
  exit 1
fi

sleep "${paused_probe_seconds}"
api_request "GET" "/api/v1/execution-schedules/${schedule_id}" "${artifact_dir}/responses/detail-paused.json"

paused_dispatch_count="$(
  python3 - "${artifact_dir}/responses/detail-paused.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload.get("state") != "paused":
    raise SystemExit(f"expected paused schedule state, got {payload.get('state')!r}")
print(len(payload.get("dispatches") or []))
PY
)"
if [[ "${paused_dispatch_count}" != "0" ]]; then
  echo "paused recurring schedule dispatched unexpectedly before resume" >&2
  exit 1
fi

api_request \
  "POST" \
  "/api/v1/execution-schedules/${schedule_id}/resume" \
  "${artifact_dir}/responses/resume-schedule.json" \
  "" \
  "${run_id}-resume"

resumed_state="$(
  python3 - "${artifact_dir}/responses/resume-schedule.json" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["state"])
PY
)"
if [[ "${resumed_state}" != "active" ]]; then
  echo "expected resumed recurring schedule to be active, got ${resumed_state}" >&2
  exit 1
fi

dispatch_id=""
execution_id=""
attempt_id=""
temporal_workflow_id=""
temporal_run_id=""
for _ in $(seq 1 $((execution_timeout_seconds / 2))); do
  api_request "GET" "/api/v1/execution-schedules/${schedule_id}" "${artifact_dir}/responses/detail-active.json"
  dispatch_state="$(
    python3 - "${artifact_dir}/responses/detail-active.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
dispatches = payload.get("dispatches") or []
if not dispatches:
    print("waiting")
    raise SystemExit(0)
dispatch = dispatches[0]
if dispatch.get("state") == "failed":
    reason = dispatch.get("failure_reason") or "unknown"
    raise SystemExit(f"dispatch failed: {reason}")
if dispatch.get("state") == "submitted" and dispatch.get("execution_id") and dispatch.get("attempt_id"):
    print("\t".join([
        "submitted",
        dispatch["dispatch_id"],
        dispatch["execution_id"],
        dispatch["attempt_id"],
        dispatch.get("temporal_workflow_id", ""),
        dispatch.get("temporal_run_id", ""),
    ]))
    raise SystemExit(0)
print("waiting")
PY
  )"
  if [[ "${dispatch_state}" == "waiting" ]]; then
    sleep 2
    continue
  fi
  read -r dispatch_status dispatch_id execution_id attempt_id temporal_workflow_id temporal_run_id <<<"${dispatch_state}"
  if [[ "${dispatch_status}" == "submitted" ]]; then
    break
  fi
done

if [[ -z "${execution_id}" || -z "${attempt_id}" || -z "${dispatch_id}" || -z "${temporal_workflow_id}" || -z "${temporal_run_id}" ]]; then
  echo "recurring schedule did not submit an execution in time" >&2
  exit 1
fi

api_request "GET" "/api/v1/execution-schedules" "${artifact_dir}/responses/list-after-dispatch.json"
list_after_dispatches="$(
  python3 - "${artifact_dir}/responses/list-after-dispatch.json" "${schedule_id}" <<'PY'
import json
import sys

schedule_id = sys.argv[2]
for item in json.load(open(sys.argv[1], encoding="utf-8")):
    if item.get("schedule_id") == schedule_id:
        print(len(item.get("dispatches") or []))
        raise SystemExit(0)
raise SystemExit(f"schedule {schedule_id} missing from list response")
PY
)"
if [[ ! "${list_after_dispatches}" =~ ^[0-9]+$ || "${list_after_dispatches}" -lt 1 ]]; then
  echo "expected schedule list response to include at least one dispatch preview after resume" >&2
  exit 1
fi

api_request \
  "POST" \
  "/api/v1/execution-schedules/${schedule_id}/pause" \
  "${artifact_dir}/responses/pause-schedule.json" \
  "" \
  "${run_id}-pause"

paused_cleanup_state="$(
  python3 - "${artifact_dir}/responses/pause-schedule.json" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["state"])
PY
)"
if [[ "${paused_cleanup_state}" != "paused" ]]; then
  echo "expected recurring schedule cleanup pause to return paused state, got ${paused_cleanup_state}" >&2
  exit 1
fi

execution_status=""
for _ in $(seq 1 "${execution_timeout_seconds}"); do
  api_request "GET" "/api/v1/executions/${execution_id}" "${artifact_dir}/responses/execution.json"
  execution_status="$(
    python3 - "${artifact_dir}/responses/execution.json" "${attempt_id}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
expected_attempt_id = sys.argv[2]
latest_attempt = payload.get("latest_attempt") or {}
if latest_attempt.get("attempt_id") != expected_attempt_id:
    raise SystemExit(
        f"execution latest_attempt mismatch: expected {expected_attempt_id!r}, got {latest_attempt.get('attempt_id')!r}"
    )
status = payload.get("status", "")
print(status)
PY
  )"
  case "${execution_status}" in
    succeeded)
      break
      ;;
    failed | canceled | timed_out)
      echo "recurring schedule execution finished with terminal status ${execution_status}" >&2
      exit 1
      ;;
  esac
  sleep 1
done

if [[ "${execution_status}" != "succeeded" ]]; then
  echo "recurring schedule execution did not reach succeeded status in time" >&2
  exit 1
fi

log_marker_seen="0"
for _ in $(seq 1 30); do
  api_request "GET" "/api/v1/executions/${execution_id}/logs" "${artifact_dir}/responses/execution-logs.json"
  log_marker_seen="$(
    python3 - "${artifact_dir}/responses/execution-logs.json" "${proof_log_line}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
needle = sys.argv[2]
print("1" if needle in (payload.get("logs") or "") else "0")
PY
  )"
  if [[ "${log_marker_seen}" == "1" ]]; then
    break
  fi
  sleep 1
done

if [[ "${log_marker_seen}" != "1" ]]; then
  echo "recurring schedule execution logs did not contain the proof marker" >&2
  exit 1
fi

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

remote_psql sandbox_rental "
SELECT schedule_id, org_id, state, temporal_schedule_id, temporal_namespace, task_queue, interval_seconds, max_wall_seconds
FROM execution_schedules
WHERE schedule_id = '${schedule_id}';
" >"${artifact_dir}/postgres/execution_schedule.tsv"

remote_psql sandbox_rental "
SELECT dispatch_id, schedule_id, temporal_workflow_id, temporal_run_id, execution_id, attempt_id, state, failure_reason
FROM execution_schedule_dispatches
WHERE dispatch_id = '${dispatch_id}';
" >"${artifact_dir}/postgres/execution_schedule_dispatch.tsv"

remote_psql sandbox_rental "
SELECT e.execution_id, a.attempt_id, e.org_id, e.source_kind, e.workload_kind, e.source_ref, e.state, a.trace_id
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
WHERE e.execution_id = '${execution_id}'
ORDER BY a.attempt_seq DESC
LIMIT 1;
" >"${artifact_dir}/postgres/execution_state.tsv"

remote_psql sandbox_rental "
SELECT attempt_id, seq, stream, chunk
FROM execution_logs
WHERE execution_id = '${execution_id}'
ORDER BY seq;
" >"${artifact_dir}/postgres/execution_logs.tsv"

remote_psql temporal_visibility "
SELECT workflow_id, run_id, workflow_type_name, task_queue, status, start_time, close_time, execution_duration, history_length
FROM executions_visibility
WHERE workflow_id = '${temporal_workflow_id}'
  AND run_id = '${temporal_run_id}';
" >"${artifact_dir}/postgres/temporal_visibility.tsv"

if [[ ! -s "${artifact_dir}/postgres/temporal_visibility.tsv" ]]; then
  echo "temporal visibility row missing for recurring workflow ${temporal_workflow_id}/${temporal_run_id}" >&2
  exit 1
fi

schedule_state_row="$(
  python3 - "${artifact_dir}/postgres/execution_schedule.tsv" "${org_id}" "${temporal_schedule_id}" <<'PY'
import csv
import io
import sys

text = open(sys.argv[1], encoding="utf-8").read().rstrip("\r\n")
if not text:
    raise SystemExit("execution_schedules row missing")
row = next(csv.reader(io.StringIO(text), delimiter="\t"))
schedule_id, org_id, state, temporal_schedule_id, temporal_namespace, task_queue, interval_seconds, max_wall_seconds = row
if org_id != sys.argv[2]:
    raise SystemExit(f"unexpected org_id {org_id!r}")
if temporal_schedule_id != sys.argv[3]:
    raise SystemExit(f"unexpected temporal_schedule_id {temporal_schedule_id!r}")
if temporal_namespace != "sandbox-rental-service":
    raise SystemExit(f"unexpected temporal namespace {temporal_namespace!r}")
if task_queue != "sandbox-rental-service.recurring-vm":
    raise SystemExit(f"unexpected task queue {task_queue!r}")
print(state)
PY
)"
if [[ "${schedule_state_row}" != "paused" ]]; then
  echo "expected recurring schedule row to end paused for cleanup, got ${schedule_state_row}" >&2
  exit 1
fi

dispatch_row_status="$(
  python3 - "${artifact_dir}/postgres/execution_schedule_dispatch.tsv" "${execution_id}" "${attempt_id}" "${temporal_workflow_id}" "${temporal_run_id}" <<'PY'
import csv
import io
import sys

text = open(sys.argv[1], encoding="utf-8").read().rstrip("\r\n")
if not text:
    raise SystemExit("execution_schedule_dispatches row missing")
row = next(csv.reader(io.StringIO(text), delimiter="\t"))
_, _, temporal_workflow_id, temporal_run_id, execution_id, attempt_id, state, _ = row
if execution_id != sys.argv[2]:
    raise SystemExit(f"unexpected execution_id {execution_id!r}")
if attempt_id != sys.argv[3]:
    raise SystemExit(f"unexpected attempt_id {attempt_id!r}")
if temporal_workflow_id != sys.argv[4] or temporal_run_id != sys.argv[5]:
    raise SystemExit("unexpected temporal workflow linkage")
print(state)
PY
)"
if [[ "${dispatch_row_status}" != "submitted" ]]; then
  echo "expected recurring dispatch row to be submitted, got ${dispatch_row_status}" >&2
  exit 1
fi

execution_view_status="$(
  python3 - "${artifact_dir}/postgres/execution_state.tsv" "${schedule_id}" <<'PY'
import csv
import io
import sys

text = open(sys.argv[1], encoding="utf-8").read().rstrip("\r\n")
if not text:
    raise SystemExit("execution state row missing")
row = next(csv.reader(io.StringIO(text), delimiter="\t"))
_, _, _, source_kind, workload_kind, source_ref, state, _ = row
if source_kind != "execution_schedule":
    raise SystemExit(f"unexpected source_kind {source_kind!r}")
if workload_kind != "direct":
    raise SystemExit(f"unexpected workload_kind {workload_kind!r}")
if source_ref != sys.argv[2]:
    raise SystemExit(f"unexpected source_ref {source_ref!r}")
print(state)
PY
)"
if [[ "${execution_view_status}" != "succeeded" ]]; then
  echo "expected recurring execution row to be succeeded, got ${execution_view_status}" >&2
  exit 1
fi

execution_log_marker_count="$(
  python3 - "${artifact_dir}/postgres/execution_logs.tsv" "${proof_log_line}" <<'PY'
import csv
import io
import sys

text = open(sys.argv[1], encoding="utf-8").read().rstrip("\r\n")
if not text:
    raise SystemExit("execution_logs rows missing")
needle = sys.argv[2]
count = 0
for row in csv.reader(io.StringIO(text), delimiter="\t"):
    if needle in row[3]:
        count += 1
print(count)
PY
)"
if [[ ! "${execution_log_marker_count}" =~ ^[0-9]+$ || "${execution_log_marker_count}" -lt 1 ]]; then
  echo "expected at least one PostgreSQL execution_logs chunk with the recurring proof marker" >&2
  exit 1
fi

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM job_events
  WHERE execution_id = toUUID({execution_id:String})
    AND source_kind = 'execution_schedule'
    AND status = 'succeeded'
" 1 "${artifact_dir}/clickhouse/job_event_count.tsv" --param_execution_id="${execution_id}"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM job_logs
  WHERE execution_id = toUUID({execution_id:String})
    AND position(chunk, {proof_log_line:String}) > 0
" 1 "${artifact_dir}/clickhouse/job_log_marker_count.tsv" --param_execution_id="${execution_id}" --param_proof_log_line="${proof_log_line}"

api_request "GET" "/api/v1/runs?limit=20&source_kind=execution_schedule" "${artifact_dir}/responses/runs-list.json"
api_request "GET" "/api/v1/runs/${execution_id}" "${artifact_dir}/responses/run-detail.json"
api_request "GET" "/api/v1/run-logs/search?run_id=${execution_id}&query=${proof_log_marker}&limit=20" "${artifact_dir}/responses/run-logs-search.json"
api_request "GET" "/api/v1/run-analytics/jobs?start=${window_start}" "${artifact_dir}/responses/jobs-analytics.json"
api_request "GET" "/api/v1/run-analytics/costs?start=${window_start}" "${artifact_dir}/responses/costs-analytics.json"
api_request "GET" "/api/v1/run-analytics/caches?start=${window_start}" "${artifact_dir}/responses/caches-analytics.json"
api_request "GET" "/api/v1/run-analytics/runner-sizing?start=${window_start}" "${artifact_dir}/responses/runner-sizing-analytics.json"
api_request "GET" "/api/v1/sticky-disks?limit=20" "${artifact_dir}/responses/sticky-disks.json"

python3 - \
  "${artifact_dir}/responses/runs-list.json" \
  "${artifact_dir}/responses/run-detail.json" \
  "${artifact_dir}/responses/run-logs-search.json" \
  "${artifact_dir}/responses/jobs-analytics.json" \
  "${artifact_dir}/responses/costs-analytics.json" \
  "${artifact_dir}/responses/caches-analytics.json" \
  "${artifact_dir}/responses/runner-sizing-analytics.json" \
  "${artifact_dir}/responses/sticky-disks.json" \
  "${execution_id}" \
  "${attempt_id}" \
  "${schedule_id}" \
  "${proof_log_line}" <<'PY'
import json
import sys

runs_list = json.load(open(sys.argv[1], encoding="utf-8"))
run_detail = json.load(open(sys.argv[2], encoding="utf-8"))
log_search = json.load(open(sys.argv[3], encoding="utf-8"))
jobs_analytics = json.load(open(sys.argv[4], encoding="utf-8"))
costs_analytics = json.load(open(sys.argv[5], encoding="utf-8"))
caches_analytics = json.load(open(sys.argv[6], encoding="utf-8"))
runner_sizing = json.load(open(sys.argv[7], encoding="utf-8"))
sticky_inventory = json.load(open(sys.argv[8], encoding="utf-8"))
execution_id = sys.argv[9]
attempt_id = sys.argv[10]
schedule_id = sys.argv[11]
proof_log_line = sys.argv[12]

matching = [item for item in (runs_list.get("runs") or []) if item.get("execution_id") == execution_id]
if not matching:
    raise SystemExit("runs list did not include recurring execution")
if runs_list.get("filters", {}).get("source_kind") != "execution_schedule":
    raise SystemExit("runs list filters.source_kind mismatch")

if run_detail.get("execution_id") != execution_id:
    raise SystemExit("run detail execution_id mismatch")
if run_detail.get("run_id") != execution_id:
    raise SystemExit("run detail run_id mismatch")
if (run_detail.get("schedule") or {}).get("schedule_id") != schedule_id:
    raise SystemExit("run detail schedule metadata mismatch")
summary = run_detail.get("billing_summary") or {}
if int(summary.get("window_count") or 0) < 1:
    raise SystemExit("expected billing_summary.window_count >= 1")

results = log_search.get("results") or []
if not results:
    raise SystemExit("run log search returned no results")
if not any(item.get("attempt_id") == attempt_id and proof_log_line in (item.get("chunk") or "") for item in results):
    raise SystemExit("run log search did not include the recurring proof marker")

def bucket_count(payload, key):
    for bucket in payload:
        if bucket.get("key") == key:
            return int(bucket.get("count") or "0")
    return 0

if bucket_count(jobs_analytics.get("by_source") or [], "execution_schedule") < 1:
    raise SystemExit("jobs analytics missing execution_schedule bucket")
if bucket_count(costs_analytics.get("by_source") or [], "execution_schedule") < 1:
    raise SystemExit("costs analytics missing execution_schedule bucket")
if int(caches_analytics.get("checkout_requests") or "0") != 0:
    raise SystemExit("expected recurring proof checkout_requests == 0")
if sticky_inventory.get("disks") is None:
    raise SystemExit("sticky disk inventory response missing disks array")
if runner_sizing.get("by_runner_class") is None:
    raise SystemExit("runner sizing analytics response missing by_runner_class")
PY

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-read-path-logs.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.execution_schedule.create'
    AND SpanAttributes['sandbox.schedule_id'] = {schedule_id:String}
" 1 "${artifact_dir}/clickhouse/create-span-count.tsv" --param_schedule_id="${schedule_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'auth.spiffe.mtls.server'
    AND SpanAttributes['spiffe.peer_id'] = {sandbox_service_spiffe_id:String}
" 1 "${artifact_dir}/clickhouse/temporal-mtls-count.tsv" --param_sandbox_service_spiffe_id="${sandbox_service_spiffe_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'temporal.auth.authorize'
    AND SpanAttributes['temporal.namespace'] = 'sandbox-rental-service'
    AND SpanAttributes['temporal.authz.decision'] = 'allow'
" 1 "${artifact_dir}/clickhouse/temporal-authz-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'sandbox-rental-recurring-worker'
    AND SpanName = 'sandbox-rental.execution_schedule.dispatch.submit'
    AND SpanAttributes['sandbox.dispatch_id'] = {dispatch_id:String}
    AND SpanAttributes['execution.id'] = {execution_id:String}
" 1 "${artifact_dir}/clickhouse/dispatch-span-count.tsv" --param_dispatch_id="${dispatch_id}" --param_execution_id="${execution_id}"

	wait_for_clickhouse_count default "
	  SELECT count()
	  FROM otel_traces
	  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
	    AND ServiceName = 'sandbox-rental-recurring-worker'
	    AND SpanName = 'sandbox-rental.execution.submit'
	    AND SpanAttributes['execution.id'] = {execution_id:String}
	" 1 "${artifact_dir}/clickhouse/submit-span-count.tsv" --param_execution_id="${execution_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.execution.run'
    AND SpanAttributes['execution.id'] = {execution_id:String}
" 1 "${artifact_dir}/clickhouse/run-span-count.tsv" --param_execution_id="${execution_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.runs.list'
" 1 "${artifact_dir}/clickhouse/runs-list-span-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.runs.get'
    AND SpanAttributes['execution.id'] = {execution_id:String}
" 1 "${artifact_dir}/clickhouse/runs-get-span-count.tsv" --param_execution_id="${execution_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.logs.search'
" 1 "${artifact_dir}/clickhouse/log-search-span-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName IN (
      'sandbox-rental.analytics.jobs',
      'sandbox-rental.analytics.costs',
      'sandbox-rental.analytics.caches',
      'sandbox-rental.analytics.runner_sizing',
      'sandbox-rental.stickydisks.list'
    )
" 5 "${artifact_dir}/clickhouse/read-api-span-count.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_schedule_id="${schedule_id}" \
    --param_execution_id="${execution_id}" \
    --param_sandbox_service_spiffe_id="${sandbox_service_spiffe_id}" \
    --query "
      SELECT
        Timestamp,
        ServiceName,
        SpanName,
        TraceId,
        SpanId,
        ParentSpanId,
        SpanAttributes['sandbox.schedule_id'] AS schedule_id,
        SpanAttributes['sandbox.dispatch_id'] AS dispatch_id,
        SpanAttributes['execution.id'] AS execution_id,
        SpanAttributes['temporal.schedule_id'] AS temporal_schedule_id,
        SpanAttributes['temporal.namespace'] AS temporal_namespace,
        SpanAttributes['spiffe.peer_id'] AS spiffe_peer_id
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
        AND (
          SpanAttributes['sandbox.schedule_id'] = {schedule_id:String}
          OR SpanAttributes['execution.id'] = {execution_id:String}
          OR SpanAttributes['spiffe.peer_id'] = {sandbox_service_spiffe_id:String}
        )
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/recurring_schedule_spans.tsv"

run_id="${run_id}" \
org_id="${org_id}" \
schedule_id="${schedule_id}" \
dispatch_id="${dispatch_id}" \
execution_id="${execution_id}" \
attempt_id="${attempt_id}" \
temporal_schedule_id="${temporal_schedule_id}" \
temporal_workflow_id="${temporal_workflow_id}" \
temporal_run_id="${temporal_run_id}" \
window_start="${window_start}" \
window_end="${window_end}" \
artifact_dir="${artifact_dir}" \
python3 - <<'PY' >"${artifact_dir}/run.json"
import json
import os

print(json.dumps({
    "artifact_dir": os.environ["artifact_dir"],
    "attempt_id": os.environ["attempt_id"],
    "dispatch_id": os.environ["dispatch_id"],
    "execution_id": os.environ["execution_id"],
    "org_id": int(os.environ["org_id"]),
    "schedule_id": os.environ["schedule_id"],
    "temporal_run_id": os.environ["temporal_run_id"],
    "temporal_schedule_id": os.environ["temporal_schedule_id"],
    "temporal_workflow_id": os.environ["temporal_workflow_id"],
    "verification_run_id": os.environ["run_id"],
    "window_end": os.environ["window_end"],
    "window_start": os.environ["window_start"],
}, indent=2, sort_keys=True))
PY

printf 'recurring schedule proof passed: run_id=%s schedule_id=%s execution_id=%s artifacts=%s\n' \
  "${run_id}" "${schedule_id}" "${execution_id}" "${artifact_dir}"
