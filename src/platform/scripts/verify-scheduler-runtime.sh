#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-scheduler-runtime-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/scheduler-runtime}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/postgres"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)

api_base_url="${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}"
api_url="${api_base_url%/}/api/v1/scheduler/probes"
submitted_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

payload="$(
  VERIFICATION_RUN_ID="${run_id}" python3 - <<'PY'
import json
import os

print(json.dumps({
    "message": f"scheduler runtime proof {os.environ['VERIFICATION_RUN_ID']}",
}))
PY
)"

response="$(
  curl -fsS \
    -H "Authorization: Bearer ${SANDBOX_RENTAL_TOKEN}" \
    -H "Content-Type: application/json" \
    -H "baggage: forge_metal.verification_run=${run_id}" \
    -d "${payload}" \
    "${api_url}"
)"
printf '%s\n' "${response}" >"${artifact_dir}/probe-response.json"

job_id="$(python3 -c '
import json
import sys

payload = json.load(sys.stdin)
job_id = payload.get("job_id")
if isinstance(job_id, str) and job_id.isdecimal():
    job_id = int(job_id)
if not isinstance(job_id, int) or job_id <= 0:
    raise SystemExit(f"probe response did not include a positive decimal job_id: {payload!r}")
print(job_id)
' <<<"${response}")"

remote_psql() {
  local sql="$1"
  verification_ssh "sudo -u postgres psql -d sandbox_rental -X -A -t -P footer=off -c \"$sql\""
}

for _ in $(seq 1 60); do
  job_state="$(remote_psql "SELECT state::text FROM river_job WHERE id = ${job_id};" | tr -d '[:space:]')"
  if [[ "${job_state}" == "completed" ]]; then
    break
  fi
  sleep 1
done

if [[ "${job_state}" != "completed" ]]; then
  remote_psql "SELECT id, kind, queue, state::text, errors FROM river_job WHERE id = ${job_id};" >"${artifact_dir}/postgres/river_job_failed.tsv" || true
  echo "scheduler probe job ${job_id} did not complete; final state: ${job_state:-missing}" >&2
  exit 1
fi

remote_psql "
COPY (
  SELECT name, paused_at
  FROM river_queue
  WHERE name IN ('execution','orchestrator','runner','scheduler','reconcile','webhook')
  ORDER BY name
) TO STDOUT WITH (FORMAT csv, HEADER true);
" >"${artifact_dir}/postgres/river_queue.csv"

remote_psql "
COPY (
  SELECT id, kind, queue, state::text, attempt, max_attempts, created_at, finalized_at
  FROM river_job
  WHERE id = ${job_id}
) TO STDOUT WITH (FORMAT csv, HEADER true);
" >"${artifact_dir}/postgres/river_job.csv"

ch_query() {
  (cd "${VERIFICATION_PLATFORM_ROOT}" && ./scripts/clickhouse.sh --query "$1")
}

submit_trace_id=""
for _ in $(seq 1 60); do
  submit_trace_id="$(
    ch_query "
SELECT TraceId
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp >= parseDateTime64BestEffort('${submitted_at}')
  AND SpanName = 'sandbox-rental.scheduler.probe.submit'
  AND SpanAttributes['verification.run_id'] = '${run_id}'
ORDER BY Timestamp DESC
LIMIT 1
FORMAT TSVRaw
" | tr -d '[:space:]'
  )"
  if [[ -n "${submit_trace_id}" ]]; then
    break
  fi
  sleep 1
done

if [[ -z "${submit_trace_id}" ]]; then
  echo "scheduler probe submit trace not found for ${run_id}" >&2
  exit 1
fi

insert_span_count="$(
  ch_query "
SELECT count()
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND TraceId = '${submit_trace_id}'
  AND SpanName = 'river.insert_many'
FORMAT TSVRaw
" | tr -d '[:space:]'
)"
if [[ ! "${insert_span_count}" =~ ^[0-9]+$ || "${insert_span_count}" -lt 1 ]]; then
  echo "scheduler probe river.insert_many span not found in trace ${submit_trace_id}" >&2
  exit 1
fi

worker_span_counts=""
for _ in $(seq 1 60); do
  worker_span_counts="$(
    ch_query "
SELECT
  countIf(SpanName = 'river.work/scheduler.probe'),
  countIf(SpanName = 'sandbox-rental.scheduler.probe.complete')
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp >= parseDateTime64BestEffort('${submitted_at}')
  AND SpanName IN ('river.work/scheduler.probe', 'sandbox-rental.scheduler.probe.complete')
  AND (SpanAttributes['id'] = '${job_id}' OR SpanAttributes['river.job_id'] = '${job_id}' OR SpanAttributes['verification.run_id'] = '${run_id}')
FORMAT TSVRaw
" | tr '\t' ' '
  )"
  read -r work_span_count complete_span_count <<<"${worker_span_counts}"
  if [[ "${work_span_count}" =~ ^[0-9]+$ && "${complete_span_count}" =~ ^[0-9]+$ && "${work_span_count}" -ge 1 && "${complete_span_count}" -ge 1 ]]; then
    break
  fi
  sleep 1
done

if [[ ! "${work_span_count:-}" =~ ^[0-9]+$ || ! "${complete_span_count:-}" =~ ^[0-9]+$ || "${work_span_count:-0}" -lt 1 || "${complete_span_count:-0}" -lt 1 ]]; then
  echo "scheduler probe worker spans not found for job ${job_id}: work=${work_span_count:-missing} complete=${complete_span_count:-missing}" >&2
  exit 1
fi

ch_query "
SELECT
  Timestamp,
  TraceId,
  SpanName,
  SpanAttributes['messaging.operation.name'] AS messaging_operation,
  SpanAttributes['queue'] AS river_queue,
  SpanAttributes['kind'] AS river_kind,
  SpanAttributes['id'] AS river_job_id,
  SpanAttributes['river.job_id'] AS manual_job_id,
  SpanAttributes['verification.run_id'] AS verification_run_id
FROM default.otel_traces
WHERE ServiceName = 'sandbox-rental-service'
  AND Timestamp >= parseDateTime64BestEffort('${submitted_at}')
  AND (
    TraceId = '${submit_trace_id}'
    OR SpanAttributes['id'] = '${job_id}'
    OR SpanAttributes['river.job_id'] = '${job_id}'
    OR SpanAttributes['verification.run_id'] = '${run_id}'
  )
ORDER BY Timestamp
FORMAT TSVWithNames
" >"${artifact_dir}/clickhouse/scheduler_spans.tsv"

run_id="${run_id}" job_id="${job_id}" submitted_at="${submitted_at}" artifact_dir="${artifact_dir}" python3 - <<'PY' >"${artifact_dir}/run.json"
import json
import os

print(json.dumps({
    "verification_run_id": os.environ["run_id"],
    "job_id": int(os.environ["job_id"]),
    "submitted_at": os.environ["submitted_at"],
    "artifact_dir": os.environ["artifact_dir"],
}, indent=2, sort_keys=True))
PY

printf 'scheduler runtime proof passed: run_id=%s job_id=%s artifacts=%s\n' "${run_id}" "${job_id}" "${artifact_dir}"
