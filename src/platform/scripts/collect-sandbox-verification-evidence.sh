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
repo_root="$(cd "${script_dir}/../../.." && pwd)"
platform_root="${repo_root}/src/platform"
inventory="${platform_root}/ansible/inventory/hosts.ini"
output_dir="${2:-$(dirname "${run_json_path}")/evidence}"

mkdir -p "${output_dir}/clickhouse" "${output_dir}/postgres" "${output_dir}/tigerbeetle"

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

remote_host="$(grep -m1 'ansible_host=' "${inventory}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "${inventory}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"
ssh_opts=(-o IPQoS=none -o StrictHostKeyChecking=no)

ch_query() {
  (cd "${platform_root}" && ./scripts/clickhouse.sh --query "$1")
}

remote_psql() {
  local db="$1"
  local sql="$2"
  ssh "${ssh_opts[@]}" "${remote_user}@${remote_host}" \
    "sudo -u postgres psql -d ${db} -X -A -P footer=off" <<<"${sql}"
}

remote_psql_tsv() {
  local db="$1"
  local sql="$2"
  ssh "${ssh_opts[@]}" "${remote_user}@${remote_host}" \
    "sudo -u postgres psql -d ${db} -X -A -t -F $'\t' -P footer=off -c \"$sql\""
}

if [[ -n "${execution_id}" && ( -z "${repo_id}" || -z "${attempt_id}" ) ]]; then
  mapfile -t execution_identity < <(remote_psql_tsv sandbox_rental "
    SELECT
      COALESCE(e.repo_id::text, ''),
      COALESCE(a.attempt_id::text, '')
    FROM executions e
    LEFT JOIN execution_attempts a ON a.attempt_id = e.latest_attempt_id
    WHERE e.execution_id = '${execution_id}';
  ")
  if [[ ${#execution_identity[@]} -gt 0 ]]; then
    IFS=$'\t' read -r derived_repo_id derived_attempt_id <<<"${execution_identity[0]}"
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
      e.commit_sha,
      e.created_at,
      e.updated_at,
      a.attempt_id,
      a.state,
      a.billing_job_id,
      a.orchestrator_job_id,
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
      active_golden_generation_id,
      last_ready_sha,
      last_error,
      created_at,
      updated_at
    FROM repos
    WHERE repo_id = '${repo_id}'
  ) TO STDOUT WITH (FORMAT csv, HEADER true);
  " >"${output_dir}/postgres/repo.csv"

  remote_psql sandbox_rental "
  COPY (
    SELECT
      golden_generation_id,
      repo_id,
      runner_profile_slug,
      source_ref,
      source_sha,
      state,
      trigger_reason,
      execution_id,
      attempt_id,
      orchestrator_job_id,
      snapshot_ref,
      activated_at,
      superseded_at,
      failure_reason,
      failure_detail,
      created_at,
      updated_at
    FROM golden_generations
    WHERE repo_id = '${repo_id}'
    ORDER BY created_at
  ) TO STDOUT WITH (FORMAT csv, HEADER true);
  " >"${output_dir}/postgres/golden_generations.csv"
else
  printf 'repo_id,missing\n,true\n' >"${output_dir}/postgres/repo.csv"
  printf 'repo_id,missing\n,true\n' >"${output_dir}/postgres/golden_generations.csv"
fi

if [[ -n "${attempt_id}" ]]; then
  remote_psql sandbox_rental "
  COPY (
    SELECT
      attempt_id,
      window_seq,
      window_seconds,
      actual_seconds,
      pricing_phase,
      state,
      created_at,
      settled_at
    FROM execution_billing_windows
    WHERE attempt_id = '${attempt_id}'
    ORDER BY window_seq
  ) TO STDOUT WITH (FORMAT csv, HEADER true);
  " >"${output_dir}/postgres/execution_billing_windows.csv"

  remote_psql sandbox_rental "
  COPY (
    SELECT reservation::text
    FROM execution_billing_windows
    WHERE attempt_id = '${attempt_id}'
    ORDER BY window_seq
    LIMIT 1
  ) TO STDOUT;
  " >"${output_dir}/postgres/reservation.json"
else
  printf 'attempt_id,missing\n,true\n' >"${output_dir}/postgres/execution_billing_windows.csv"
  : >"${output_dir}/postgres/reservation.json"
fi

mapfile -t grant_ids < <(python3 - "${output_dir}/postgres/reservation.json" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
raw = path.read_text(encoding="utf-8").strip()
if not raw:
    sys.exit(0)
reservation = json.loads(raw)
for leg in reservation.get("grant_legs", []):
    grant_id = leg.get("grant_id", "").strip()
    if grant_id:
        print(grant_id)
PY
)

for grant_id in "${grant_ids[@]}"; do
  [[ -n "${grant_id}" ]] || continue
  ssh "${ssh_opts[@]}" "${remote_user}@${remote_host}" \
    "sudo /opt/forge-metal/profile/bin/tb-inspect '${grant_id}'" \
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
- postgres/execution.csv
- postgres/execution_billing_windows.csv
- postgres/reservation.json
- tigerbeetle/grant-*.txt
EOF
