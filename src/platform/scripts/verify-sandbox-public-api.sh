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
submit_parallel="${SANDBOX_PROOF_SUBMIT_PARALLEL:-8}"
proof_timeout_seconds="${SANDBOX_PROOF_TIMEOUT_SECONDS:-1800}"
clickhouse_timeout_seconds="${SANDBOX_PROOF_CLICKHOUSE_TIMEOUT_SECONDS:-180}"
max_wall_seconds="${SANDBOX_PROOF_MAX_WALL_SECONDS:-7200}"
proof_persona="${SANDBOX_PROOF_PERSONA:-platform-admin}"
workload_profile="${SANDBOX_PROOF_WORKLOAD_PROFILE:-echo}"
# Keep guest/kernel/Python headroom inside the 16 GiB default runner class while
# still making cpu-mem stress materially exercise memory admission and billing.
workload_memory_mib="${SANDBOX_PROOF_WORKLOAD_MEMORY_MIB:-8192}"
workload_cpu_seconds="${SANDBOX_PROOF_WORKLOAD_CPU_SECONDS:-3}"
workload_cpu_workers="${SANDBOX_PROOF_WORKLOAD_CPU_WORKERS:-1}"
workload_disk_mib="${SANDBOX_PROOF_WORKLOAD_DISK_MIB:-256}"
workload_disk_block_kib="${SANDBOX_PROOF_WORKLOAD_DISK_BLOCK_KIB:-1024}"
proof_log_marker="${SANDBOX_PROOF_LOG_MARKER:-forge-metal-proof}"
telemetry_fault_profile="${SANDBOX_PROOF_TELEMETRY_FAULT_PROFILE:-}"
require_telemetry_hello="${SANDBOX_PROOF_REQUIRE_TELEMETRY_HELLO:-0}"
telemetry_fault_kind=""
telemetry_fault_expected_seq="0"
telemetry_fault_observed_seq="0"
telemetry_fault_missing_samples="0"

if [[ -n "${telemetry_fault_profile}" ]]; then
  telemetry_fault_expectation="$(
    python3 - "${telemetry_fault_profile}" <<'PY'
import re
import sys

profile = sys.argv[1]
match = re.fullmatch(r"(gap_once|regression_once)@([0-9]+)", profile)
if not match:
    raise SystemExit("SANDBOX_PROOF_TELEMETRY_FAULT_PROFILE must be gap_once@N or regression_once@N")

kind, raw_seq = match.groups()
seq = int(raw_seq)
if kind == "gap_once":
    if seq >= 2**32 - 1:
        raise SystemExit("gap_once@N requires N <= 4294967294")
    print(f"gap,{seq},{seq + 1},1")
else:
    if seq == 0 or seq > 2**32 - 1:
        raise SystemExit("regression_once@N requires 1 <= N <= 4294967295")
    print(f"regression,{seq},{seq - 1},0")
PY
  )"
  IFS=',' read -r telemetry_fault_kind telemetry_fault_expected_seq telemetry_fault_observed_seq telemetry_fault_missing_samples <<<"${telemetry_fault_expectation}"
fi

case "${workload_profile,,}" in
  disk | disk-write | disk_write)
    workload_memory_mib="0"
    workload_cpu_seconds="0"
    workload_cpu_workers="1"
    ;;
esac

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
    echo "unsupported SANDBOX_PROOF_PERSONA=${proof_persona}" >&2
    exit 1
    ;;
esac

mkdir -p "${artifact_dir}/payloads" "${artifact_dir}/responses" "${artifact_dir}/clickhouse" "${artifact_dir}/postgres"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" "${proof_persona}" --print)

billing_fixture_path="${artifact_dir}/billing-fixture.json"
"${script_dir}/set-user-state.sh" \
  --email "${proof_billing_email}" \
  --org "${proof_billing_org}" \
  --product-id "sandbox" \
  --state "pro" \
  --balance-units "500000000000" >"${billing_fixture_path}"
billing_org_id="$(
  python3 - "${billing_fixture_path}" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["org_id"])
PY
)"

platform_billing_org_id="${billing_org_id}"
if [[ "${proof_billing_org}" != "platform" ]]; then
  platform_billing_fixture_path="${artifact_dir}/platform-billing-fixture.json"
  "${script_dir}/set-user-state.sh" \
    --email "ceo@${VERIFICATION_DOMAIN}" \
    --org "platform" \
    --product-id "sandbox" \
    --state "pro" \
    --balance-units "500000000000" >"${platform_billing_fixture_path}"
  platform_billing_org_id="$(
    python3 - "${platform_billing_fixture_path}" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["org_id"])
PY
  )"
fi

api_base_url="${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}"
api_url="${api_base_url%/}/api/v1/executions"
submitted_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

submit_one() {
  local index="$1"
  local payload_path="${artifact_dir}/payloads/${index}.json"
  local response_path="${artifact_dir}/responses/${index}.json"
  local idempotency_key="${run_id}-${index}"

  python3 - "${idempotency_key}" "${index}" "${max_wall_seconds}" "${workload_profile}" "${workload_memory_mib}" "${workload_cpu_seconds}" "${workload_cpu_workers}" "${workload_disk_mib}" "${workload_disk_block_kib}" >"${payload_path}" <<'PY'
import base64
import json
import shlex
import sys

(
    idempotency_key,
    index,
    max_wall_seconds,
    workload_profile,
    workload_memory_mib,
    workload_cpu_seconds,
    workload_cpu_workers,
    workload_disk_mib,
    workload_disk_block_kib,
) = sys.argv[1:10]
workload_profile = workload_profile.strip().lower()


def echo_command(index_value: str) -> str:
    return f"printf 'forge-metal-proof workload=echo index={index_value} ok\\n'"


def resource_workload_command(
    profile_value: str,
    index_value: str,
    memory_mib_value: str,
    cpu_seconds_value: str,
    cpu_workers_value: str,
    disk_mib_value: str,
    disk_block_kib_value: str,
) -> str:
    index_int = int(index_value)
    memory_mib_int = int(memory_mib_value)
    cpu_seconds_float = float(cpu_seconds_value)
    cpu_workers_int = int(cpu_workers_value)
    disk_mib_int = int(disk_mib_value)
    disk_block_kib_int = int(disk_block_kib_value)
    normalized_profile = profile_value.replace("_", "-")
    if normalized_profile == "cpu-mem" and memory_mib_int < 1:
        raise SystemExit("SANDBOX_PROOF_WORKLOAD_MEMORY_MIB must be >= 1 for cpu-mem")
    if normalized_profile in ("disk", "mixed") and disk_mib_int < 1:
        raise SystemExit("SANDBOX_PROOF_WORKLOAD_DISK_MIB must be >= 1 for disk workloads")
    if memory_mib_int < 0:
        raise SystemExit("SANDBOX_PROOF_WORKLOAD_MEMORY_MIB must be >= 0")
    if cpu_seconds_float < 0:
        raise SystemExit("SANDBOX_PROOF_WORKLOAD_CPU_SECONDS must be >= 0")
    if cpu_workers_int < 1:
        raise SystemExit("SANDBOX_PROOF_WORKLOAD_CPU_WORKERS must be >= 1")
    if disk_block_kib_int < 4:
        raise SystemExit("SANDBOX_PROOF_WORKLOAD_DISK_BLOCK_KIB must be >= 4")
    source = """import argparse
import hashlib
import multiprocessing
import os
import resource
import time

parser = argparse.ArgumentParser()
parser.add_argument("--profile", required=True)
parser.add_argument("--index", type=int, required=True)
parser.add_argument("--memory-mib", type=int, required=True)
parser.add_argument("--cpu-seconds", type=float, required=True)
parser.add_argument("--cpu-workers", type=int, required=True)
parser.add_argument("--disk-mib", type=int, required=True)
parser.add_argument("--disk-block-kib", type=int, required=True)
args = parser.parse_args()

PROFILE = args.profile
INDEX = args.index
MEMORY_MIB = args.memory_mib
CPU_SECONDS = args.cpu_seconds
CPU_WORKERS = args.cpu_workers
DISK_MIB = args.disk_mib
DISK_BLOCK_KIB = args.disk_block_kib
PAGE_BYTES = 4096
TOTAL_BYTES = MEMORY_MIB * 1024 * 1024
DISK_BYTES = DISK_MIB * 1024 * 1024
DISK_BLOCK_BYTES = DISK_BLOCK_KIB * 1024
scratch_path = f"/workspace/forge-metal-proof-{INDEX}.bin"

started = time.perf_counter()
buf = bytearray(TOTAL_BYTES) if TOTAL_BYTES > 0 else bytearray(1024 * 1024)
if TOTAL_BYTES > 0:
    for offset in range(0, TOTAL_BYTES, PAGE_BYTES):
        buf[offset] = ((offset // PAGE_BYTES) + INDEX) & 0xFF
touched = time.perf_counter()

view = memoryview(buf)
CPU_BYTES = len(buf)
cpu_seed = bytes(view[:min(CPU_BYTES, 1024 * 1024)])
loops = 0
digest = "0" * 64

def run_cpu_worker(worker_index, seconds, seed):
    local = bytearray(seed)
    deadline = time.perf_counter() + seconds
    worker_loops = 0
    worker_digest = "0" * 64
    while time.perf_counter() < deadline:
        h = hashlib.sha256()
        for _ in range(64):
            h.update(local)
        digest_bytes = h.digest()
        worker_digest = digest_bytes.hex()
        local[(worker_loops * 7919 + worker_index) % len(local)] ^= digest_bytes[0]
        worker_loops += 1
    return worker_loops, worker_digest

if CPU_SECONDS > 0:
    with multiprocessing.Pool(processes=CPU_WORKERS) as pool:
        results = pool.starmap(run_cpu_worker, [(worker, CPU_SECONDS, cpu_seed) for worker in range(CPU_WORKERS)])
    loops = sum(item[0] for item in results)
    digest = hashlib.sha256("".join(item[1] for item in results).encode("ascii")).hexdigest()

disk_write_seconds = 0.0
disk_read_seconds = 0.0
disk_digest = "0" * 64
if DISK_BYTES > 0:
    write_started = time.perf_counter()
    pattern = bytes(((INDEX + offset) & 0xFF for offset in range(256)))
    block = (pattern * ((DISK_BLOCK_BYTES // len(pattern)) + 1))[:DISK_BLOCK_BYTES]
    written = 0
    with open(scratch_path, "wb", buffering=0) as f:
        while written < DISK_BYTES:
            n = min(len(block), DISK_BYTES - written)
            f.write(block[:n])
            written += n
        os.fsync(f.fileno())
    disk_write_seconds = time.perf_counter() - write_started

    read_started = time.perf_counter()
    h = hashlib.sha256()
    with open(scratch_path, "rb", buffering=0) as f:
        while True:
            chunk = f.read(DISK_BLOCK_BYTES)
            if not chunk:
                break
            h.update(chunk)
    disk_digest = h.hexdigest()
    disk_read_seconds = time.perf_counter() - read_started

rss_kib = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
print(
    "forge-metal-proof "
    f"workload={PROFILE} index={INDEX} memory_mib={MEMORY_MIB} "
    f"cpu_seconds={CPU_SECONDS:g} cpu_workers={CPU_WORKERS} touch_seconds={touched - started:.3f} "
    f"loops={loops} disk_mib={DISK_MIB} disk_write_seconds={disk_write_seconds:.3f} "
    f"disk_read_seconds={disk_read_seconds:.3f} rss_kib={rss_kib} "
    f"digest={digest[:16]} disk_digest={disk_digest[:16]} ok",
    flush=True,
)
"""
    encoded = base64.b64encode(source.encode("utf-8")).decode("ascii")
    return (
        f"printf %s {shlex.quote(encoded)} | base64 -d | python3 - "
        f"--profile {shlex.quote(normalized_profile)} "
        f"--index {index_int} --memory-mib {memory_mib_int} "
        f"--cpu-seconds {cpu_seconds_float:g} --cpu-workers {cpu_workers_int} "
        f"--disk-mib {disk_mib_int} "
        f"--disk-block-kib {disk_block_kib_int}"
    )


if workload_profile == "echo":
    run_command = echo_command(index)
elif workload_profile in ("cpu-mem", "cpu_mem", "disk", "disk-write", "disk_write", "mixed"):
    normalized = "disk" if workload_profile in ("disk-write", "disk_write") else workload_profile.replace("_", "-")
    if normalized == "disk":
        workload_memory_mib = "0"
        workload_cpu_seconds = "0"
    run_command = resource_workload_command(
        normalized,
        index,
        workload_memory_mib,
        workload_cpu_seconds,
        workload_cpu_workers,
        workload_disk_mib,
        workload_disk_block_kib,
    )
else:
    raise SystemExit(f"unsupported SANDBOX_PROOF_WORKLOAD_PROFILE={workload_profile!r}")

print(json.dumps({
    "kind": "direct",
    "idempotency_key": idempotency_key,
    "runner_class": "metal-4vcpu-ubuntu-2404",
    "product_id": "sandbox",
    "run_command": run_command,
    "max_wall_seconds": int(max_wall_seconds),
}))
PY

  curl -fsS \
    -H "Authorization: Bearer ${SANDBOX_RENTAL_TOKEN}" \
    -H "Content-Type: application/json" \
    -H "baggage: forge_metal.verification_run=${run_id}" \
    -d @"${payload_path}" \
    "${api_url}" >"${response_path}"
}

export -f submit_one
export artifact_dir max_wall_seconds run_id SANDBOX_RENTAL_TOKEN api_url workload_profile workload_memory_mib workload_cpu_seconds workload_cpu_workers workload_disk_mib workload_disk_block_kib

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

pg_log_marker_literal="$(
  python3 - "${proof_log_marker}" <<'PY'
import sys

print("'" + sys.argv[1].replace("'", "''") + "'")
PY
)"

remote_psql() {
  local sql="$1"
  verification_ssh "sudo -u postgres psql -d sandbox_rental -X -A -t -P footer=off -c \"$sql\""
}

billing_psql() {
  local sql="$1"
  verification_ssh "sudo -u postgres psql -d billing -X -A -t -P footer=off -c \"$sql\""
}

platform_active_sku_rates="$(
  billing_psql "COPY (
    WITH business_clock AS (
      SELECT COALESCE(
        (
          SELECT business_now
          FROM billing_clock_overrides
          WHERE scope_kind = 'org_product'
            AND scope_id = '${platform_billing_org_id}:sandbox'
        ),
        now()
      ) AS now_at
    ), active_phase AS (
      SELECT cp.plan_id
      FROM contract_phases cp
      JOIN contracts c ON c.contract_id = cp.contract_id
      CROSS JOIN business_clock clock
      WHERE cp.org_id = '${platform_billing_org_id}'
        AND cp.product_id = 'sandbox'
        AND cp.state IN ('active','grace')
        AND cp.effective_start <= clock.now_at
        AND (cp.effective_end IS NULL OR cp.effective_end > clock.now_at)
        AND c.state IN ('active','past_due','cancel_scheduled')
      ORDER BY cp.effective_start DESC, cp.phase_id DESC
      LIMIT 1
    ), chosen AS (
      SELECT COALESCE(
        (SELECT plan_id FROM active_phase),
        (SELECT plan_id FROM plans WHERE product_id = 'sandbox' AND active AND is_default ORDER BY plan_id LIMIT 1)
      ) AS plan_id
    )
    SELECT count(*)
    FROM plan_sku_rates r
    CROSS JOIN business_clock clock
    WHERE r.plan_id = (SELECT plan_id FROM chosen)
      AND r.sku_id IN (
        'sandbox_compute_amd_epyc_4484px_vcpu_ms',
        'sandbox_memory_standard_gib_ms',
        'sandbox_execution_root_storage_premium_nvme_gib_ms'
      )
      AND r.active
      AND r.active_from <= clock.now_at
      AND (r.active_until IS NULL OR r.active_until > clock.now_at)
  ) TO STDOUT WITH CSV;"
)"
if [[ "${platform_active_sku_rates}" -ne 3 ]]; then
  echo "platform billing fixture has ${platform_active_sku_rates} active sandbox SKU rates; expected 3" >&2
  exit 1
fi

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
      AND ((w.reservation_jsonb->'Allocation') ? 'sandbox_execution_root_storage_premium_nvme_gib_ms')
      AND NOT ((w.reservation_jsonb->'Allocation') ? 'vcpu')
      AND NOT ((w.reservation_jsonb->'Allocation') ? 'memory_mib')
      AND NOT ((w.reservation_jsonb->'Allocation') ? 'rootfs_bytes')
  ) TO STDOUT WITH CSV;"
)"
if [[ "${sku_mapped_windows}" -ne "${submission_count}" ]]; then
  echo "expected ${submission_count} billing reservations with SKU-mapped allocations, found ${sku_mapped_windows}" >&2
  exit 1
fi

remote_psql "COPY (
  SELECT w.billing_window_id
  FROM execution_billing_windows w
  JOIN execution_attempts a ON a.attempt_id = w.attempt_id
  WHERE a.execution_id = ANY(${pg_uuid_array})
  ORDER BY w.window_start, w.attempt_id, w.window_seq
) TO STDOUT WITH CSV;" >"${artifact_dir}/postgres/billing-window-ids.csv"

billing_window_array="$(
  python3 - "${artifact_dir}/postgres/billing-window-ids.csv" <<'PY'
import pathlib
import sys

ids = [line.strip() for line in pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").splitlines() if line.strip()]
if not ids:
    raise SystemExit("no billing window IDs were exported from sandbox_rental")
print("ARRAY[" + ",".join("'" + item.replace("'", "''") + "'" for item in ids) + "]::text[]")
PY
)"

billing_psql "COPY (
  SELECT
    l.window_id,
    l.leg_seq,
    COALESCE(b.sort_order, 9999) AS bucket_order,
    l.component_bucket_id,
    l.component_sku_id,
    l.source,
    CASE l.source
      WHEN 'free_tier' THEN 1
      WHEN 'contract' THEN 2
      WHEN 'promo' THEN 3
      WHEN 'refund' THEN 4
      WHEN 'purchase' THEN 5
      WHEN 'receivable' THEN 6
      ELSE 7
    END AS source_order,
    l.amount_reserved,
    l.amount_posted,
    l.amount_voided,
    l.state
  FROM billing_window_ledger_legs l
  JOIN billing_windows w ON w.window_id = l.window_id
  LEFT JOIN credit_buckets b ON b.bucket_id = l.component_bucket_id
  WHERE l.window_id = ANY(${billing_window_array})
  ORDER BY l.window_id, l.leg_seq
) TO STDOUT WITH CSV HEADER;" >"${artifact_dir}/postgres/billing_window_ledger_legs.csv"

funding_order_violations="$(
  billing_psql "COPY (
    WITH ordered AS (
      SELECT
        l.window_id,
        l.leg_seq,
        COALESCE(b.sort_order, 9999) AS bucket_order,
        l.component_bucket_id,
        l.component_sku_id,
        l.source,
        CASE l.source
          WHEN 'free_tier' THEN 1
          WHEN 'contract' THEN 2
          WHEN 'promo' THEN 3
          WHEN 'refund' THEN 4
          WHEN 'purchase' THEN 5
          WHEN 'receivable' THEN 6
          ELSE 7
        END AS source_order
      FROM billing_window_ledger_legs l
      LEFT JOIN credit_buckets b ON b.bucket_id = l.component_bucket_id
      WHERE l.window_id = ANY(${billing_window_array})
    ), violations AS (
      SELECT DISTINCT before_leg.window_id
      FROM ordered before_leg
      JOIN ordered after_leg
        ON after_leg.window_id = before_leg.window_id
       AND after_leg.leg_seq > before_leg.leg_seq
      WHERE (
        before_leg.bucket_order,
        before_leg.source_order,
        before_leg.component_bucket_id,
        before_leg.source,
        before_leg.component_sku_id
      ) > (
        after_leg.bucket_order,
        after_leg.source_order,
        after_leg.component_bucket_id,
        after_leg.source,
        after_leg.component_sku_id
      )
    )
    SELECT count(*) FROM violations
  ) TO STDOUT WITH CSV;"
)"
if [[ "${funding_order_violations}" -ne 0 ]]; then
  echo "billing funding waterfall order violations: ${funding_order_violations}; see ${artifact_dir}/postgres/billing_window_ledger_legs.csv" >&2
  exit 1
fi

billing_statement_path="${artifact_dir}/postgres/billing-statement.json"
verification_ssh "curl -fsS 'http://127.0.0.1:4242/internal/billing/v1/orgs/${billing_org_id}/statement?product_id=sandbox'" >"${billing_statement_path}"
python3 - "${billing_statement_path}" <<'PY'
import json
import sys

statement = json.load(open(sys.argv[1], encoding="utf-8"))
line_items = statement.get("line_items") or []
observed = [item.get("bucket_id") for item in line_items]
expected = ["compute", "memory", "execution_root_storage"]
if observed[: len(expected)] != expected:
    raise SystemExit(f"billing statement line order {observed!r} did not start with {expected!r}")
PY

proof_marker_logs="$(
  remote_psql "COPY (
    SELECT count(*)
    FROM execution_logs l
    WHERE l.execution_id = ANY(${pg_uuid_array})
      AND l.chunk LIKE '%' || ${pg_log_marker_literal} || '%'
  ) TO STDOUT WITH CSV;"
)"
if [[ "${proof_marker_logs}" -ne "${submission_count}" ]]; then
  echo "expected ${submission_count} proof marker log chunks, found ${proof_marker_logs}" >&2
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

if [[ "${require_telemetry_hello}" == "1" || "${require_telemetry_hello,,}" == "true" ]]; then
  telemetry_hello_target="${submission_count}"
else
  telemetry_hello_target="0"
fi

lease_evidence="0,0,0,0"
IFS=',' read -r lease_ready_count lease_exec_started_count lease_telemetry_hello_count lease_cleanup_count <<<"${lease_evidence}"
clickhouse_deadline=$((SECONDS + clickhouse_timeout_seconds))
while [[ "${SECONDS}" -lt "${clickhouse_deadline}" ]]; do
  lease_evidence="$(
    ch_query --database forge_metal \
      --param_lease_ids="${lease_ids_csv}" \
      --query "
        SELECT
          countIf(evidence_type = 'lease_ready'),
          countIf(evidence_type = 'exec_started'),
          countIf(evidence_type = 'telemetry_hello'),
          countIf(evidence_type = 'lease_cleanup')
        FROM vm_lease_evidence
        WHERE has(splitByChar(',', {lease_ids:String}), lease_id)
        FORMAT TSVRaw
      " | tr '\t' ','
  )"
  IFS=',' read -r lease_ready_count lease_exec_started_count lease_telemetry_hello_count lease_cleanup_count <<<"${lease_evidence}"
  if [[ "${lease_ready_count}" -ge "${submission_count}" && "${lease_exec_started_count}" -ge "${submission_count}" && "${lease_telemetry_hello_count}" -ge "${telemetry_hello_target}" && "${lease_cleanup_count}" -ge "${submission_count}" ]]; then
    break
  fi
  sleep 1
done
if [[ "${lease_ready_count}" -lt "${submission_count}" || "${lease_exec_started_count}" -lt "${submission_count}" || "${lease_telemetry_hello_count}" -lt "${telemetry_hello_target}" || "${lease_cleanup_count}" -lt "${submission_count}" ]]; then
  echo "vm_lease_evidence incomplete: ready=${lease_ready_count} exec_started=${lease_exec_started_count} telemetry_hello=${lease_telemetry_hello_count} cleanup=${lease_cleanup_count}" >&2
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

if [[ -n "${telemetry_fault_profile}" ]]; then
  telemetry_diagnostic_count="0"
  clickhouse_deadline=$((SECONDS + clickhouse_timeout_seconds))
  while [[ "${SECONDS}" -lt "${clickhouse_deadline}" ]]; do
    telemetry_diagnostic_count="$(
      ch_query --database forge_metal \
        --param_lease_ids="${lease_ids_csv}" \
        --param_diagnostic_kind="${telemetry_fault_kind}" \
        --param_expected_seq="${telemetry_fault_expected_seq}" \
        --param_observed_seq="${telemetry_fault_observed_seq}" \
        --param_missing_samples="${telemetry_fault_missing_samples}" \
        --query "
          SELECT count()
          FROM vm_lease_evidence
          WHERE has(splitByChar(',', {lease_ids:String}), lease_id)
            AND evidence_type = 'telemetry_diagnostic'
            AND diagnostic_kind = {diagnostic_kind:String}
            AND expected_seq = {expected_seq:UInt32}
            AND observed_seq = {observed_seq:UInt32}
            AND missing_samples = {missing_samples:UInt32}
          FORMAT TSVRaw
        " | tr -d '[:space:]'
    )"
    if [[ "${telemetry_diagnostic_count}" -ge "${submission_count}" ]]; then
      break
    fi
    sleep 1
  done
  if [[ "${telemetry_diagnostic_count}" -lt "${submission_count}" ]]; then
    echo "expected ${submission_count} telemetry ${telemetry_fault_profile} diagnostics, found ${telemetry_diagnostic_count}" >&2
    exit 1
  fi

  ch_query --database forge_metal \
    --param_lease_ids="${lease_ids_csv}" \
    --param_diagnostic_kind="${telemetry_fault_kind}" \
    --param_expected_seq="${telemetry_fault_expected_seq}" \
    --param_observed_seq="${telemetry_fault_observed_seq}" \
    --param_missing_samples="${telemetry_fault_missing_samples}" \
    --query "
      SELECT
        evidence_time,
        lease_id,
        evidence_type,
        diagnostic_kind,
        expected_seq,
        observed_seq,
        missing_samples,
        trace_id
      FROM vm_lease_evidence
      WHERE has(splitByChar(',', {lease_ids:String}), lease_id)
        AND evidence_type = 'telemetry_diagnostic'
        AND diagnostic_kind = {diagnostic_kind:String}
        AND expected_seq = {expected_seq:UInt32}
        AND observed_seq = {observed_seq:UInt32}
        AND missing_samples = {missing_samples:UInt32}
      ORDER BY evidence_time, lease_id
      FORMAT TSVWithNames
    " >"${artifact_dir}/clickhouse/vm_telemetry_diagnostics.tsv"
fi

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

python3 - "${artifact_dir}/run.json" "${run_id}" "${submitted_at}" "${submission_count}" "${artifact_dir}" "${workload_profile}" "${workload_memory_mib}" "${workload_cpu_seconds}" "${workload_cpu_workers}" "${workload_disk_mib}" "${workload_disk_block_kib}" "${telemetry_fault_profile}" "${telemetry_fault_kind}" "${telemetry_fault_expected_seq}" "${telemetry_fault_observed_seq}" "${telemetry_fault_missing_samples}" <<'PY'
import json
import sys

out, run_id, submitted_at, submission_count, artifact_dir, workload_profile, workload_memory_mib, workload_cpu_seconds, workload_cpu_workers, workload_disk_mib, workload_disk_block_kib, telemetry_fault_profile, telemetry_fault_kind, telemetry_fault_expected_seq, telemetry_fault_observed_seq, telemetry_fault_missing_samples = sys.argv[1:17]
print(json.dumps({
    "verification_run_id": run_id,
    "submitted_at": submitted_at,
    "submission_count": int(submission_count),
    "artifact_dir": artifact_dir,
    "workload_profile": workload_profile,
    "workload_memory_mib": int(workload_memory_mib),
    "workload_cpu_seconds": float(workload_cpu_seconds),
    "workload_cpu_workers": int(workload_cpu_workers),
    "workload_disk_mib": int(workload_disk_mib),
    "workload_disk_block_kib": int(workload_disk_block_kib),
    "telemetry_fault_profile": telemetry_fault_profile,
    "telemetry_fault_kind": telemetry_fault_kind,
    "telemetry_fault_expected_seq": int(telemetry_fault_expected_seq),
    "telemetry_fault_observed_seq": int(telemetry_fault_observed_seq),
    "telemetry_fault_missing_samples": int(telemetry_fault_missing_samples),
}, indent=2, sort_keys=True), file=open(out, "w", encoding="utf-8"))
PY

printf 'sandbox public API proof passed: run_id=%s submissions=%s artifacts=%s\n' "${run_id}" "${submission_count}" "${artifact_dir}"
