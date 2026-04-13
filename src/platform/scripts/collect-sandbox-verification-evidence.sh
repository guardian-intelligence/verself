#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <run-json-path> [output-dir]" >&2
  exit 1
fi

run_json_path="$1"
if [[ ! -f "${run_json_path}" ]]; then
  echo "run json not found: ${run_json_path}" >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"
output_dir="${2:-$(dirname "${run_json_path}")/evidence}"

mkdir -p "${output_dir}/clickhouse" "${output_dir}/postgres" "${output_dir}/tigerbeetle"
: >"${output_dir}/clickhouse/execution_scheduler_span_sequence.tsv"

mapfile -t run_meta < <(python3 - "${run_json_path}" <<'PY'
import json
import sys
from datetime import datetime, timedelta, timezone

def parse_iso(value: str) -> datetime:
    value = value.replace("Z", "+00:00")
    return datetime.fromisoformat(value)

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    run = json.load(fh)

submit = parse_iso(run["submit_requested_at"]) - timedelta(seconds=60)
terminal = parse_iso(run["terminal_observed_at"]) + timedelta(seconds=120)

print(run.get("verification_run_id", ""))
print(run.get("repo_id", ""))
print(run.get("execution_id", ""))
print(run.get("attempt_id", ""))
print(run.get("status", ""))
print(run.get("repo_url", ""))
print(run.get("ref", ""))
print(run.get("log_marker", ""))
print(run.get("submit_requested_at", ""))
print(run.get("terminal_observed_at", ""))
print(submit.astimezone(timezone.utc).isoformat().replace("+00:00", "Z"))
print(terminal.astimezone(timezone.utc).isoformat().replace("+00:00", "Z"))
print(str(run.get("started_balance", "")))
print(str(run.get("finished_balance", "")))
print(run.get("error", ""))
PY
)

verification_run_id="${run_meta[0]}"
repo_id="${run_meta[1]}"
execution_id="${run_meta[2]}"
attempt_id="${run_meta[3]}"
run_status="${run_meta[4]}"
repo_url="${run_meta[5]}"
repo_ref="${run_meta[6]}"
log_marker="${run_meta[7]}"
submit_requested_at="${run_meta[8]}"
terminal_observed_at="${run_meta[9]}"
window_start="${run_meta[10]}"
window_end="${run_meta[11]}"
started_balance="${run_meta[12]}"
finished_balance="${run_meta[13]}"
run_error="${run_meta[14]}"

ch_query() {
  (cd "${VERIFICATION_PLATFORM_ROOT}" && ./scripts/clickhouse.sh --query "$1")
}

remote_psql() {
  local db="$1"
  local sql="$2"
  verification_ssh "sudo -u postgres psql -d ${db} -X -A -P footer=off" <<<"${sql}"
}

remote_psql_tsv() {
  local db="$1"
  local sql="$2"
  verification_ssh "sudo -u postgres psql -d ${db} -X -A -t -F \$'\\t' -P footer=off -c \"$sql\""
}

if [[ -n "${execution_id}" && ( -z "${repo_id}" || -z "${attempt_id}" ) ]]; then
  mapfile -t execution_identity < <(remote_psql sandbox_rental "
    COPY (
    SELECT
      COALESCE(e.repo_id::text, ''),
      COALESCE(a.attempt_id::text, '')
    FROM executions e
    LEFT JOIN execution_attempts a ON a.attempt_id = e.latest_attempt_id
    WHERE e.execution_id = '${execution_id}'
    ) TO STDOUT WITH (FORMAT csv);
  ")
  if [[ ${#execution_identity[@]} -gt 0 ]]; then
    IFS=',' read -r derived_repo_id derived_attempt_id <<<"${execution_identity[0]}"
    if [[ -z "${repo_id}" && -n "${derived_repo_id}" ]]; then
      repo_id="${derived_repo_id}"
    fi
    if [[ -z "${attempt_id}" && -n "${derived_attempt_id}" ]]; then
      attempt_id="${derived_attempt_id}"
    fi
  fi
fi

if [[ -n "${verification_run_id}" ]]; then
  ch_query "
  SELECT *
  FROM forge_metal.job_events
  WHERE verification_run_id = '${verification_run_id}'
  ORDER BY created_at, execution_id
  FORMAT TSVWithNames
  " >"${output_dir}/clickhouse/job_events.tsv"
else
  printf 'execution_id\tmissing\n\ttrue\n' >"${output_dir}/clickhouse/job_events.tsv"
fi

if [[ -n "${verification_run_id}" ]]; then
  ch_query "
  SELECT *
  FROM forge_metal.job_logs
  WHERE attempt_id IN (
    SELECT toString(attempt_id)
    FROM forge_metal.job_events
    WHERE verification_run_id = '${verification_run_id}'
  )
  ORDER BY attempt_id, seq
  FORMAT TSVWithNames
  " >"${output_dir}/clickhouse/job_logs.tsv"

  ch_query "
  SELECT *
  FROM forge_metal.metering
  WHERE source_ref IN (
    SELECT toString(attempt_id)
    FROM forge_metal.job_events
    WHERE verification_run_id = '${verification_run_id}'
  )
  ORDER BY recorded_at, source_ref
  FORMAT TSVWithNames
  " >"${output_dir}/clickhouse/metering.tsv"
else
  printf 'attempt_id\tmissing\n\ttrue\n' >"${output_dir}/clickhouse/job_logs.tsv"
  printf 'attempt_id\tmissing\n\ttrue\n' >"${output_dir}/clickhouse/metering.tsv"
fi

ch_query "
SELECT
  Timestamp,
  ServiceName,
  SeverityText,
  Body,
  toString(LogAttributes) AS attrs
FROM default.otel_logs
WHERE (
    Body LIKE '%${verification_run_id}%'
    OR toString(LogAttributes) LIKE '%${verification_run_id}%'
    OR Body LIKE '%${execution_id}%'
    OR toString(LogAttributes) LIKE '%${execution_id}%'
    OR Body LIKE '%${attempt_id}%'
    OR toString(LogAttributes) LIKE '%${attempt_id}%'
  )
  OR (
    Timestamp BETWEEN parseDateTime64BestEffort('${window_start}') AND parseDateTime64BestEffort('${window_end}')
    AND ServiceName IN ('rent-a-sandbox', 'sandbox-rental-service', 'billing-service')
  )
ORDER BY Timestamp
FORMAT TSVWithNames
" >"${output_dir}/clickhouse/otel_logs.tsv"

ch_query "
SELECT
  Timestamp,
  ServiceName,
  SpanName,
  StatusCode,
  intDiv(Duration, 1000000) AS duration_ms,
  SpanAttributes['http.method'] AS http_method,
  SpanAttributes['http.target'] AS http_target,
  SpanAttributes['http.status_code'] AS http_status_code
FROM default.otel_traces
WHERE Timestamp BETWEEN parseDateTime64BestEffort('${window_start}') AND parseDateTime64BestEffort('${window_end}')
  AND ServiceName IN ('rent-a-sandbox', 'sandbox-rental-service', 'billing-service')
ORDER BY Timestamp
FORMAT TSVWithNames
" >"${output_dir}/clickhouse/otel_traces.tsv"

ch_query "
SELECT
  event_id,
  event_type,
  aggregate_type,
  aggregate_id,
  org_id,
  product_id,
  occurred_at,
  payload,
  recorded_at
FROM forge_metal.billing_events
WHERE occurred_at BETWEEN parseDateTime64BestEffort('${window_start}') AND parseDateTime64BestEffort('${window_end}')
ORDER BY occurred_at, event_id
FORMAT TSVWithNames
" >"${output_dir}/clickhouse/billing_events.tsv"

if [[ -n "${execution_id}" ]]; then
  remote_psql sandbox_rental "
  COPY (
    SELECT
      e.execution_id,
      e.org_id,
      e.actor_id,
      e.kind,
	      e.status,
	      e.repo_url,
	      e.ref,
	      e.created_at,
      e.updated_at,
      a.attempt_id,
      a.state,
      a.billing_job_id,
      a.orchestrator_run_id,
      a.exit_code,
      a.duration_ms,
      a.trace_id,
      a.started_at,
      a.completed_at
    FROM executions e
    JOIN execution_attempts a ON a.attempt_id = e.latest_attempt_id
    WHERE e.execution_id = '${execution_id}'
  ) TO STDOUT WITH (FORMAT csv, HEADER true);
  " >"${output_dir}/postgres/execution.csv"
else
  printf 'execution_id,missing\n,true\n' >"${output_dir}/postgres/execution.csv"
fi

if [[ -n "${repo_id}" ]]; then
  remote_psql sandbox_rental "
  COPY (
    SELECT
      repo_id,
      org_id,
      provider,
      full_name,
      clone_url,
      default_branch,
      state,
      compatibility_status,
      last_scanned_sha,
      last_error,
      created_at,
      updated_at
    FROM repos
    WHERE repo_id = '${repo_id}'
  ) TO STDOUT WITH (FORMAT csv, HEADER true);
  " >"${output_dir}/postgres/repo.csv"

else
  printf 'repo_id,missing\n,true\n' >"${output_dir}/postgres/repo.csv"
fi

if [[ -n "${attempt_id}" ]]; then
  remote_psql sandbox_rental "
  COPY (
    SELECT
      attempt_id,
      billing_window_id,
      window_seq,
      reservation_shape,
      reserved_quantity,
      actual_quantity,
      pricing_phase,
      reservation_jsonb <> '{}'::jsonb AS has_reservation_payload,
      state,
      window_start,
      activated_at,
      created_at,
      settled_at
    FROM execution_billing_windows
    WHERE attempt_id = '${attempt_id}'
    ORDER BY window_seq
  ) TO STDOUT WITH (FORMAT csv, HEADER true);
  " >"${output_dir}/postgres/execution_billing_windows.csv"

  remote_psql sandbox_rental "
  COPY (
    SELECT
      event_seq,
      from_state,
      to_state,
      reason,
      trace_id,
      created_at
    FROM execution_events
    WHERE attempt_id = '${attempt_id}'
    ORDER BY event_seq
  ) TO STDOUT WITH (FORMAT csv, HEADER true);
  " >"${output_dir}/postgres/execution_events.csv"

  remote_psql sandbox_rental "
  COPY (
    SELECT
      id,
      kind,
      queue,
      state::text,
      attempt,
      max_attempts,
      args->>'execution_id' AS execution_id,
      args->>'attempt_id' AS attempt_id,
      created_at,
      finalized_at
    FROM river_job
    WHERE kind = 'execution.advance'
      AND queue = 'execution'
      AND args->>'execution_id' = '${execution_id}'
    ORDER BY created_at
  ) TO STDOUT WITH (FORMAT csv, HEADER true);
  " >"${output_dir}/postgres/river_execution_jobs.csv"

  remote_psql sandbox "
  COPY (
    SELECT
      window_id,
      org_id,
      product_id,
      plan_id,
      source_type,
      source_ref,
      window_seq,
      state,
      reservation_shape,
      reserved_quantity,
      actual_quantity,
      billable_quantity,
      writeoff_quantity,
      reserved_charge_units,
      billed_charge_units,
      writeoff_charge_units,
      pricing_phase,
      usage_summary,
      funding_legs,
      window_start,
      activated_at,
      expires_at,
      renew_by,
      settled_at,
      metering_projected_at,
      created_at
    FROM billing_windows
    WHERE source_ref = '${attempt_id}'
    ORDER BY window_seq
  ) TO STDOUT WITH (FORMAT csv, HEADER true);
  " >"${output_dir}/postgres/billing_windows.csv"

  remote_psql sandbox "
  COPY (
    SELECT funding_legs::text
    FROM billing_windows
    WHERE source_ref = '${attempt_id}'
    ORDER BY window_seq
  ) TO STDOUT;
  " >"${output_dir}/postgres/billing_window_funding_legs.jsonl"
else
  printf 'attempt_id,missing\n,true\n' >"${output_dir}/postgres/execution_billing_windows.csv"
  printf 'attempt_id,missing\n,true\n' >"${output_dir}/postgres/execution_events.csv"
  printf 'attempt_id,missing\n,true\n' >"${output_dir}/postgres/river_execution_jobs.csv"
  printf 'window_id,missing\n,true\n' >"${output_dir}/postgres/billing_windows.csv"
  : >"${output_dir}/postgres/billing_window_funding_legs.jsonl"
fi

if [[ "${run_status}" == "succeeded" && -n "${execution_id}" && -n "${attempt_id}" ]]; then
  event_sequence="$(
    remote_psql_tsv sandbox_rental "
      SELECT string_agg(to_state, '>' ORDER BY event_seq)
      FROM execution_events
      WHERE attempt_id = '${attempt_id}';
    " | tr -d '[:space:]'
  )"
  expected_event_sequence="queued>reserved>launching>running>finalizing>succeeded"
  if [[ "${event_sequence}" != "${expected_event_sequence}" ]]; then
    echo "unexpected execution_events sequence for ${attempt_id}: got '${event_sequence}', want '${expected_event_sequence}'" >&2
    exit 1
  fi

  river_completed_count="$(
    remote_psql_tsv sandbox_rental "
      SELECT count(*)
      FROM river_job
      WHERE kind = 'execution.advance'
        AND queue = 'execution'
        AND state::text = 'completed'
        AND args->>'execution_id' = '${execution_id}'
        AND args->>'attempt_id' = '${attempt_id}';
    " | tr -d '[:space:]'
  )"
  if [[ ! "${river_completed_count}" =~ ^[0-9]+$ || "${river_completed_count}" -lt 1 ]]; then
    echo "completed execution.advance River job not found for execution ${execution_id} attempt ${attempt_id}" >&2
    exit 1
  fi

  sandbox_window_count="$(
    remote_psql_tsv sandbox_rental "
      SELECT count(*)
      FROM execution_billing_windows
      WHERE attempt_id = '${attempt_id}';
    " | tr -d '[:space:]'
  )"
  billing_window_count="$(
    remote_psql_tsv sandbox "
      SELECT count(*)
      FROM billing_windows
      WHERE source_type = 'execution_attempt'
        AND source_ref = '${attempt_id}';
    " | tr -d '[:space:]'
  )"
  if [[ ! "${sandbox_window_count}" =~ ^[0-9]+$ || ! "${billing_window_count}" =~ ^[0-9]+$ || "${sandbox_window_count}" != "${billing_window_count}" ]]; then
    echo "billing window handoff mismatch for attempt ${attempt_id}: sandbox_rental=${sandbox_window_count} billing=${billing_window_count}" >&2
    exit 1
  fi
  sandbox_reservation_payload_count="$(
    remote_psql_tsv sandbox_rental "
      SELECT count(*)
      FROM execution_billing_windows
      WHERE attempt_id = '${attempt_id}'
        AND reservation_jsonb <> '{}'::jsonb;
    " | tr -d '[:space:]'
  )"
  if [[ ! "${sandbox_reservation_payload_count}" =~ ^[0-9]+$ || "${sandbox_reservation_payload_count}" != "${sandbox_window_count}" ]]; then
    echo "billing reservation payload mismatch for attempt ${attempt_id}: payloads=${sandbox_reservation_payload_count} windows=${sandbox_window_count}" >&2
    exit 1
  fi

  ch_projection_count="$(
    ch_query "
      SELECT count()
      FROM forge_metal.job_events
      WHERE toString(execution_id) = '${execution_id}'
        AND toString(attempt_id) = '${attempt_id}'
        AND source_kind = 'api'
        AND workload_kind = 'direct'
        AND external_provider = ''
        AND external_task_id = ''
      FORMAT TSVRaw
    " | tr -d '[:space:]'
  )"
  if [[ ! "${ch_projection_count}" =~ ^[0-9]+$ || "${ch_projection_count}" -lt 1 ]]; then
    echo "job_events source/workload projection missing for execution ${execution_id} attempt ${attempt_id}" >&2
    exit 1
  fi

  execution_trace_id="$(
    ch_query "
      SELECT trace_id
      FROM forge_metal.job_events
      WHERE toString(execution_id) = '${execution_id}'
        AND toString(attempt_id) = '${attempt_id}'
      ORDER BY created_at DESC
      LIMIT 1
      FORMAT TSVRaw
    " | tr -d '[:space:]'
  )"
  if [[ -z "${execution_trace_id}" ]]; then
    echo "job_events trace_id missing for execution ${execution_id} attempt ${attempt_id}" >&2
    exit 1
  fi

  scheduler_span_sequence_path="${output_dir}/clickhouse/execution_scheduler_span_sequence.tsv"
  scheduler_span_sequence_ready=0
  for _ in $(seq 1 30); do
    ch_query "
      SELECT SpanName
      FROM default.otel_traces
      WHERE TraceId = '${execution_trace_id}'
        AND SpanName IN (
          'sandbox-rental.execution.submit',
          'river.insert_many',
          'river.work/execution.advance',
          'sandbox-rental.execution.transition',
          'sandbox-rental.execution.run',
          'vm-orchestrator.EnsureRun',
          'vm-orchestrator.WaitRun',
          'sandbox-rental.execution.finalize'
        )
      ORDER BY Timestamp
      FORMAT TSVRaw
    " >"${scheduler_span_sequence_path}"

    if python3 - "${scheduler_span_sequence_path}" <<'PY'
import sys

observed = [line.strip() for line in open(sys.argv[1], encoding="utf-8") if line.strip()]
expected = [
    "sandbox-rental.execution.submit",
    "river.insert_many",
    "river.work/execution.advance",
    "sandbox-rental.execution.transition",
    "sandbox-rental.execution.run",
    "vm-orchestrator.EnsureRun",
    "vm-orchestrator.WaitRun",
    "sandbox-rental.execution.finalize",
]
cursor = 0
for span in observed:
    if cursor < len(expected) and span == expected[cursor]:
        cursor += 1
raise SystemExit(0 if cursor == len(expected) else 1)
PY
    then
      scheduler_span_sequence_ready=1
      break
    fi
    sleep 1
  done

  if [[ "${scheduler_span_sequence_ready}" != "1" ]]; then
    python3 - "${scheduler_span_sequence_path}" <<'PY'
import sys

observed = [line.strip() for line in open(sys.argv[1], encoding="utf-8") if line.strip()]
expected = [
    "sandbox-rental.execution.submit",
    "river.insert_many",
    "river.work/execution.advance",
    "sandbox-rental.execution.transition",
    "sandbox-rental.execution.run",
    "vm-orchestrator.EnsureRun",
    "vm-orchestrator.WaitRun",
    "sandbox-rental.execution.finalize",
]
raise SystemExit(
    "execution scheduler span sequence missing ordered spans; "
    f"observed={observed!r} expected={expected!r}"
)
PY
  fi
fi

mapfile -t grant_ids < <(python3 - "${output_dir}/postgres/billing_window_funding_legs.jsonl" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
raw = path.read_text(encoding="utf-8").strip()
if not raw:
    sys.exit(0)
seen = set()
for line in raw.splitlines():
    line = line.strip()
    if not line:
        continue
    for leg in json.loads(line):
        grant_id = str(leg.get("grant_id", "")).strip()
        if grant_id and grant_id not in seen:
            seen.add(grant_id)
            print(grant_id)
PY
)

for grant_id in "${grant_ids[@]}"; do
  [[ -n "${grant_id}" ]] || continue
  verification_ssh "sudo /opt/forge-metal/profile/bin/tb-inspect '${grant_id}'" \
    >"${output_dir}/tigerbeetle/grant-${grant_id}.txt"
done

cat >"${output_dir}/summary.md" <<EOF
# Sandbox Verification Evidence

- verification_run_id: ${verification_run_id}
- repo_id: ${repo_id}
- execution_id: ${execution_id}
- attempt_id: ${attempt_id}
- status: ${run_status}
- repo_url: ${repo_url}
- ref: ${repo_ref}
- log_marker: ${log_marker}
- submit_requested_at: ${submit_requested_at}
- terminal_observed_at: ${terminal_observed_at}
- started_balance: ${started_balance}
- finished_balance: ${finished_balance}
- error: ${run_error}

Collected files:

- clickhouse/job_events.tsv
- clickhouse/job_logs.tsv
- clickhouse/metering.tsv
- clickhouse/otel_logs.tsv
- clickhouse/otel_traces.tsv
- clickhouse/execution_scheduler_span_sequence.tsv
- clickhouse/billing_events.tsv
- postgres/execution.csv
- postgres/execution_billing_windows.csv
- postgres/execution_events.csv
- postgres/river_execution_jobs.csv
- postgres/billing_windows.csv
- postgres/billing_window_funding_legs.jsonl
- tigerbeetle/grant-*.txt
EOF
