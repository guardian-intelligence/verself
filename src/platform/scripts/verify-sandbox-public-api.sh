#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-sandbox-public-api-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/sandbox-public-api}"
artifact_dir="${artifact_root}/${run_id}"
submission_count="${SANDBOX_PROOF_SUBMISSIONS:-200}"
submit_parallel="${SANDBOX_PROOF_SUBMIT_PARALLEL:-40}"
proof_timeout_seconds="${SANDBOX_PROOF_TIMEOUT_SECONDS:-1800}"
clickhouse_timeout_seconds="${SANDBOX_PROOF_CLICKHOUSE_TIMEOUT_SECONDS:-180}"
max_wall_seconds="${SANDBOX_PROOF_MAX_WALL_SECONDS:-7200}"

mkdir -p "${artifact_dir}/payloads" "${artifact_dir}/responses" "${artifact_dir}/clickhouse" "${artifact_dir}/postgres"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)

api_base_url="${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}"
api_url="${api_base_url%/}/api/v1/executions"
submitted_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

submit_one() {
  local index="$1"
  local payload_path="${artifact_dir}/payloads/${index}.json"
  local response_path="${artifact_dir}/responses/${index}.json"
  local idempotency_key="${run_id}-${index}"

  python3 - "${idempotency_key}" "${index}" "${max_wall_seconds}" >"${payload_path}" <<'PY'
import json
import sys

idempotency_key, index, max_wall_seconds = sys.argv[1:4]
print(json.dumps({
    "kind": "direct",
    "idempotency_key": idempotency_key,
    "runner_class": "metal-4vcpu-ubuntu-2404",
    "product_id": "sandbox",
    "run_command": f"echo hello world {index}",
    "max_wall_seconds": int(max_wall_seconds),
}))
PY

  curl -fsS \
    -H "Authorization: Bearer ${SANDBOX_RENTAL_TOKEN}" \
    -H "Content-Type: application/json" \
    -H "X-Forge-Metal-Verification-Run: ${run_id}" \
    -d @"${payload_path}" \
    "${api_url}" >"${response_path}"
}

export -f submit_one
export artifact_dir max_wall_seconds run_id SANDBOX_RENTAL_TOKEN api_url

submit_failed=0
active=0
for index in $(seq 1 "${submission_count}"); do
  submit_one "${index}" &
  active=$((active + 1))
  if [[ "${active}" -ge "${submit_parallel}" ]]; then
    wait -n || submit_failed=1
    active=$((active - 1))
  fi
done
while [[ "${active}" -gt 0 ]]; do
  wait -n || submit_failed=1
  active=$((active - 1))
done
if [[ "${submit_failed}" -ne 0 ]]; then
  echo "one or more sandbox execution submissions failed; see ${artifact_dir}/responses" >&2
  exit 1
fi

python3 - "${artifact_dir}/responses" "${artifact_dir}/execution-ids.txt" <<'PY'
import glob
import json
import pathlib
import sys

response_dir = pathlib.Path(sys.argv[1])
out = pathlib.Path(sys.argv[2])
ids = []
for path in sorted(response_dir.glob("*.json"), key=lambda p: int(p.stem)):
    payload = json.loads(path.read_text(encoding="utf-8"))
    execution_id = payload.get("execution_id")
    attempt_id = payload.get("attempt_id")
    if not execution_id or not attempt_id:
        raise SystemExit(f"{path} missing execution_id/attempt_id: {payload!r}")
    ids.append(execution_id)
if len(ids) != len(set(ids)):
    raise SystemExit("duplicate execution IDs in submission responses")
out.write_text("\n".join(ids) + "\n", encoding="utf-8")
print(len(ids))
PY

submitted_count="$(wc -l <"${artifact_dir}/execution-ids.txt" | tr -d '[:space:]')"
if [[ "${submitted_count}" -ne "${submission_count}" ]]; then
  echo "submitted execution count ${submitted_count} did not match expected ${submission_count}" >&2
  exit 1
fi

pg_uuid_array="$(
  python3 - "${artifact_dir}/execution-ids.txt" <<'PY'
import pathlib
import sys

ids = [line.strip() for line in pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").splitlines() if line.strip()]
print("ARRAY[" + ",".join("'" + item + "'" for item in ids) + "]::uuid[]")
PY
)"

remote_psql() {
  local sql="$1"
  verification_ssh "sudo -u postgres psql -d sandbox_rental -X -A -t -P footer=off -c \"$sql\""
}

deadline=$((SECONDS + proof_timeout_seconds))
final_counts=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  final_counts="$(
    remote_psql "COPY (
      SELECT
        count(*) FILTER (WHERE state = 'succeeded') AS succeeded,
        count(*) FILTER (WHERE state IN ('failed','lost','canceled')) AS failed,
        count(*) AS total
      FROM executions
      WHERE execution_id = ANY(${pg_uuid_array})
    ) TO STDOUT WITH CSV;"
  )"
  IFS=',' read -r succeeded_count failed_count total_count <<<"${final_counts}"
  if [[ "${total_count}" -eq "${submission_count}" && "${succeeded_count}" -eq "${submission_count}" ]]; then
    break
  fi
  if [[ "${failed_count}" -gt 0 ]]; then
    break
  fi
  sleep 2
done

remote_psql "COPY (
  SELECT state, count(*)
  FROM executions
  WHERE execution_id = ANY(${pg_uuid_array})
  GROUP BY state
  ORDER BY state
) TO STDOUT WITH CSV HEADER;" >"${artifact_dir}/postgres/execution_states.csv"

remote_psql "COPY (
  SELECT
    e.execution_id,
    a.attempt_id,
    e.state AS execution_state,
    a.state AS attempt_state,
    COALESCE(a.lease_id, '') AS lease_id,
    COALESCE(a.exec_id, '') AS exec_id,
    a.failure_reason,
    a.exit_code,
    a.duration_ms,
    a.stdout_bytes,
    a.stderr_bytes
  FROM executions e
  JOIN execution_attempts a ON a.execution_id = e.execution_id
  WHERE e.execution_id = ANY(${pg_uuid_array})
  ORDER BY e.created_at, e.execution_id
) TO STDOUT WITH CSV HEADER;" >"${artifact_dir}/postgres/executions.csv"

if [[ "${succeeded_count:-0}" -ne "${submission_count}" || "${failed_count:-0}" -ne 0 ]]; then
  echo "expected all ${submission_count} executions to succeed; final counts: ${final_counts:-missing}" >&2
  exit 1
fi

missing_events="$(
  remote_psql "COPY (
    WITH expected AS (
      SELECT a.execution_id, a.attempt_id, edge.from_state, edge.to_state, edge.reason
      FROM execution_attempts a
      CROSS JOIN (VALUES
        ('queued', 'reserved', 'reserved'),
        ('reserved', 'launching', 'launching'),
        ('launching', 'running', 'exec_started'),
        ('running', 'finalizing', 'exec_finished'),
        ('finalizing', 'succeeded', '')
      ) AS edge(from_state, to_state, reason)
      WHERE a.execution_id = ANY(${pg_uuid_array})
    )
    SELECT count(*)
    FROM expected x
    WHERE NOT EXISTS (
      SELECT 1
      FROM execution_events ev
      WHERE ev.execution_id = x.execution_id
        AND ev.attempt_id = x.attempt_id
        AND ev.from_state = x.from_state
        AND ev.to_state = x.to_state
        AND ev.reason = x.reason
    )
  ) TO STDOUT WITH CSV;"
)"
if [[ "${missing_events}" -ne 0 ]]; then
  remote_psql "COPY (
    SELECT execution_id, attempt_id, from_state, to_state, reason, created_at
    FROM execution_events
    WHERE execution_id = ANY(${pg_uuid_array})
    ORDER BY event_seq
  ) TO STDOUT WITH CSV HEADER;" >"${artifact_dir}/postgres/execution_events.csv"
  echo "missing expected execution event edges: ${missing_events}" >&2
  exit 1
fi

settled_windows="$(
  remote_psql "COPY (
    SELECT count(*)
    FROM execution_billing_windows w
    JOIN execution_attempts a ON a.attempt_id = w.attempt_id
    WHERE a.execution_id = ANY(${pg_uuid_array})
      AND w.state = 'settled'
  ) TO STDOUT WITH CSV;"
)"
if [[ "${settled_windows}" -ne "${submission_count}" ]]; then
  echo "expected ${submission_count} settled billing windows, found ${settled_windows}" >&2
  exit 1
fi

sku_mapped_windows="$(
  remote_psql "COPY (
    SELECT count(*)
    FROM execution_billing_windows w
    JOIN execution_attempts a ON a.attempt_id = w.attempt_id
    WHERE a.execution_id = ANY(${pg_uuid_array})
      AND ((w.reservation_jsonb->'Allocation') ? 'sandbox_compute_amd_epyc_4484px_vcpu_ms')
      AND ((w.reservation_jsonb->'Allocation') ? 'sandbox_memory_standard_gib_ms')
      AND ((w.reservation_jsonb->'Allocation') ? 'sandbox_block_storage_premium_nvme_gib_ms')
      AND NOT ((w.reservation_jsonb->'Allocation') ? 'vcpu')
      AND NOT ((w.reservation_jsonb->'Allocation') ? 'memory_mib')
      AND NOT ((w.reservation_jsonb->'Allocation') ? 'rootfs_bytes')
  ) TO STDOUT WITH CSV;"
)"
if [[ "${sku_mapped_windows}" -ne "${submission_count}" ]]; then
  echo "expected ${submission_count} billing reservations with SKU-mapped allocations, found ${sku_mapped_windows}" >&2
  exit 1
fi

hello_logs="$(
  remote_psql "COPY (
    SELECT count(*)
    FROM execution_logs l
    WHERE l.execution_id = ANY(${pg_uuid_array})
      AND l.chunk LIKE '%hello world%'
  ) TO STDOUT WITH CSV;"
)"
if [[ "${hello_logs}" -ne "${submission_count}" ]]; then
  echo "expected ${submission_count} hello-world log chunks, found ${hello_logs}" >&2
  exit 1
fi

python3 - "${artifact_dir}/postgres/executions.csv" "${artifact_dir}/lease-ids.txt" "${artifact_dir}/exec-ids.txt" <<'PY'
import csv
import pathlib
import sys

rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8")))
pathlib.Path(sys.argv[2]).write_text("\n".join(row["lease_id"] for row in rows if row["lease_id"]) + "\n", encoding="utf-8")
pathlib.Path(sys.argv[3]).write_text("\n".join(row["exec_id"] for row in rows if row["exec_id"]) + "\n", encoding="utf-8")
PY

ids_csv="$(paste -sd, "${artifact_dir}/execution-ids.txt")"
lease_ids_csv="$(paste -sd, "${artifact_dir}/lease-ids.txt")"

ch_query() {
  (cd "${VERIFICATION_PLATFORM_ROOT}" && ./scripts/clickhouse.sh "$@")
}

job_event_count="0"
clickhouse_deadline=$((SECONDS + clickhouse_timeout_seconds))
while [[ "${SECONDS}" -lt "${clickhouse_deadline}" ]]; do
  job_event_count="$(
    ch_query --database forge_metal \
      --param_ids="${ids_csv}" \
      --query "
        SELECT count()
        FROM job_events
        WHERE has(splitByChar(',', {ids:String}), toString(execution_id))
          AND status = 'succeeded'
        FORMAT TSVRaw
      " | tr -d '[:space:]'
  )"
  if [[ "${job_event_count}" -eq "${submission_count}" ]]; then
    break
  fi
  sleep 1
done
if [[ "${job_event_count}" -ne "${submission_count}" ]]; then
  echo "expected ${submission_count} succeeded job_events rows, found ${job_event_count}" >&2
  exit 1
fi

ch_query --database forge_metal \
  --param_ids="${ids_csv}" \
  --query "
    SELECT execution_id, attempt_id, status, exit_code, duration_ms, stdout_bytes, trace_id
    FROM job_events
    WHERE has(splitByChar(',', {ids:String}), toString(execution_id))
    ORDER BY created_at, execution_id
    FORMAT TSVWithNames
  " >"${artifact_dir}/clickhouse/job_events.tsv"

lease_evidence="0,0,0"
IFS=',' read -r lease_ready_count lease_exec_started_count lease_cleanup_count <<<"${lease_evidence}"
clickhouse_deadline=$((SECONDS + clickhouse_timeout_seconds))
while [[ "${SECONDS}" -lt "${clickhouse_deadline}" ]]; do
  lease_evidence="$(
    ch_query --database forge_metal \
      --param_lease_ids="${lease_ids_csv}" \
      --query "
        SELECT
          countIf(evidence_type = 'lease_ready'),
          countIf(evidence_type = 'exec_started'),
          countIf(evidence_type = 'lease_cleanup')
        FROM vm_lease_evidence
        WHERE has(splitByChar(',', {lease_ids:String}), lease_id)
        FORMAT TSVRaw
      " | tr '\t' ','
  )"
  IFS=',' read -r lease_ready_count lease_exec_started_count lease_cleanup_count <<<"${lease_evidence}"
  if [[ "${lease_ready_count}" -ge "${submission_count}" && "${lease_exec_started_count}" -ge "${submission_count}" && "${lease_cleanup_count}" -ge "${submission_count}" ]]; then
    break
  fi
  sleep 1
done
if [[ "${lease_ready_count}" -lt "${submission_count}" || "${lease_exec_started_count}" -lt "${submission_count}" || "${lease_cleanup_count}" -lt "${submission_count}" ]]; then
  echo "vm_lease_evidence incomplete: ready=${lease_ready_count} exec_started=${lease_exec_started_count} cleanup=${lease_cleanup_count}" >&2
  exit 1
fi

ch_query --database forge_metal \
  --param_lease_ids="${lease_ids_csv}" \
  --query "
    SELECT evidence_time, lease_id, exec_id, evidence_type, reason_code, trace_id
    FROM vm_lease_evidence
    WHERE has(splitByChar(',', {lease_ids:String}), lease_id)
    ORDER BY evidence_time, lease_id, exec_id
    FORMAT TSVWithNames
  " >"${artifact_dir}/clickhouse/vm_lease_evidence.tsv"

required_spans=(
  "sandbox-rental.execution.submit"
  "sandbox-rental.execution.run"
  "rpc.AcquireLease"
  "rpc.StartExec"
  "rpc.WaitExec"
  "rpc.ReleaseLease"
  "vmorchestrator.lease.boot"
)

span_counts_ok=0
missing_span_message=""
clickhouse_deadline=$((SECONDS + clickhouse_timeout_seconds))
while [[ "${SECONDS}" -lt "${clickhouse_deadline}" ]]; do
  ch_query --database default \
    --param_submitted_at="${submitted_at}" \
    --query "
      SELECT ServiceName, SpanName, count() AS span_count
      FROM otel_traces
      WHERE Timestamp >= parseDateTime64BestEffort({submitted_at:String})
        AND ServiceName IN ('sandbox-rental-service', 'vm-orchestrator')
        AND SpanName IN (
          'sandbox-rental.execution.submit',
          'sandbox-rental.execution.run',
          'rpc.AcquireLease',
          'rpc.StartExec',
          'rpc.WaitExec',
          'rpc.ReleaseLease',
          'vmorchestrator.lease.boot'
        )
      GROUP BY ServiceName, SpanName
      ORDER BY ServiceName, SpanName
      FORMAT TSVWithNames
    " >"${artifact_dir}/clickhouse/otel_span_counts.tsv"

  span_counts_ok=1
  missing_span_message=""
  for span_name in "${required_spans[@]}"; do
    span_count="$(awk -F'\t' -v span="${span_name}" 'NR > 1 && $2 == span { sum += $3 } END { print sum + 0 }' "${artifact_dir}/clickhouse/otel_span_counts.tsv")"
    if [[ "${span_count}" -lt "${submission_count}" ]]; then
      span_counts_ok=0
      missing_span_message="expected at least ${submission_count} ${span_name} spans, found ${span_count}"
      break
    fi
  done
  if [[ "${span_counts_ok}" -eq 1 ]]; then
    break
  fi
  sleep 1
done
if [[ "${span_counts_ok}" -ne 1 ]]; then
  echo "${missing_span_message}" >&2
  exit 1
fi

python3 - "${artifact_dir}/run.json" "${run_id}" "${submitted_at}" "${submission_count}" "${artifact_dir}" <<'PY'
import json
import sys

out, run_id, submitted_at, submission_count, artifact_dir = sys.argv[1:6]
print(json.dumps({
    "verification_run_id": run_id,
    "submitted_at": submitted_at,
    "submission_count": int(submission_count),
    "artifact_dir": artifact_dir,
}, indent=2, sort_keys=True), file=open(out, "w", encoding="utf-8"))
PY

printf 'sandbox public API proof passed: run_id=%s submissions=%s artifacts=%s\n' "${run_id}" "${submission_count}" "${artifact_dir}"
