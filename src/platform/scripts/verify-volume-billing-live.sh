#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-volume-billing-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/volume-billing}"
artifact_dir="${artifact_root}/${run_id}"
api_base_url="${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}"
api_base_url="${api_base_url%/}"
mkdir -p "${artifact_dir}/payloads" "${artifact_dir}/responses" "${artifact_dir}/postgres" "${artifact_dir}/clickhouse"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" "platform-admin" --print)

billing_fixture_path="${artifact_dir}/billing-fixture.json"
"${script_dir}/set-user-state.sh" \
  --email "ceo@${VERIFICATION_DOMAIN}" \
  --org "platform" \
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

remote_psql() {
  local sql="$1"
  verification_ssh "sudo -u postgres psql -d sandbox_rental -X -A -t -P footer=off -c \"$sql\""
}

billing_psql() {
  local sql="$1"
  verification_ssh "sudo -u postgres psql -d billing -X -A -t -P footer=off -c \"$sql\""
}

ch_query() {
  (cd "${VERIFICATION_PLATFORM_ROOT}" && ./scripts/clickhouse.sh "$@")
}

volume_payload="${artifact_dir}/payloads/volume.json"
volume_response="${artifact_dir}/responses/volume.json"
python3 - "${run_id}" >"${volume_payload}" <<'PY'
import json
import sys

run_id = sys.argv[1]
print(json.dumps({
    "idempotency_key": f"{run_id}-volume",
    "product_id": "sandbox",
    "display_name": f"billing proof {run_id}",
}))
PY

curl -fsS \
  -H "Authorization: Bearer ${SANDBOX_RENTAL_TOKEN}" \
  -H "Content-Type: application/json" \
  -H "baggage: forge_metal.verification_run=${run_id}" \
  -d @"${volume_payload}" \
  "${api_base_url}/api/v1/volumes" >"${volume_response}"

volume_id="$(
  python3 - "${volume_response}" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["volume_id"])
PY
)"

tick_payload="${artifact_dir}/payloads/meter-tick.json"
tick_response="${artifact_dir}/responses/meter-tick.json"
python3 - "${run_id}" >"${tick_payload}" <<'PY'
import json
import sys

run_id = sys.argv[1]
print(json.dumps({
    "idempotency_key": f"{run_id}-tick-1",
    "window_millis": 60000,
    "used_bytes": str(1024 * 1024 * 1024),
    "usedbysnapshots_bytes": str(256 * 1024 * 1024),
    "written_bytes": str(512 * 1024 * 1024),
    "provisioned_bytes": str(80 * 1024 * 1024 * 1024),
}))
PY

curl -fsS \
  -H "Authorization: Bearer ${SANDBOX_RENTAL_TOKEN}" \
  -H "Content-Type: application/json" \
  -H "baggage: forge_metal.verification_run=${run_id}" \
  -d @"${tick_payload}" \
  "${api_base_url}/api/v1/volumes/${volume_id}/meter-ticks" >"${tick_response}"

meter_tick_id="$(
  python3 - "${tick_response}" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["meter_tick"]["meter_tick_id"])
PY
)"

deadline=$((SECONDS + ${VOLUME_BILLING_PROOF_TIMEOUT_SECONDS:-180}))
tick_state=""
billing_window_id=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  tick_row="$(
    remote_psql "SELECT state || '|' || billing_window_id || '|' || billed_charge_units FROM volume_meter_ticks WHERE meter_tick_id = '${meter_tick_id}'::uuid"
  )"
  IFS='|' read -r tick_state billing_window_id billed_charge_units <<<"${tick_row}"
  if [[ "${tick_state}" == "billing_settled" && -n "${billing_window_id}" ]]; then
    break
  fi
  sleep 2
done

if [[ "${tick_state}" != "billing_settled" ]]; then
  echo "volume meter tick ${meter_tick_id} did not settle; last state=${tick_state}" >&2
  exit 1
fi

remote_psql "COPY (
  SELECT volume_id, state, used_bytes, usedbysnapshots_bytes, billable_live_bytes, billable_retained_bytes, last_metered_at
  FROM volumes
  WHERE volume_id = '${volume_id}'::uuid
) TO STDOUT WITH CSV HEADER;" >"${artifact_dir}/postgres/volumes.csv"

remote_psql "COPY (
  SELECT meter_tick_id, volume_id, state, window_millis, source_type, source_ref, billing_window_id, billed_charge_units, clickhouse_projected_at
  FROM volume_meter_ticks
  WHERE meter_tick_id = '${meter_tick_id}'::uuid
) TO STDOUT WITH CSV HEADER;" >"${artifact_dir}/postgres/volume_meter_ticks.csv"

billing_psql "COPY (
  SELECT window_id, source_type, source_ref, source_fingerprint, reserved_quantity, actual_quantity, billable_quantity, billed_charge_units
  FROM billing_windows
  WHERE window_id = '${billing_window_id}'
) TO STDOUT WITH CSV HEADER;" >"${artifact_dir}/postgres/billing_windows.csv"

python3 - "${artifact_dir}/postgres/billing_windows.csv" <<'PY'
import csv
import sys

rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8")))
if len(rows) != 1:
    raise SystemExit(f"expected one billing window row, found {len(rows)}")
row = rows[0]
for field in ("reserved_quantity", "actual_quantity", "billable_quantity"):
    if row[field] != "60000":
        raise SystemExit(f"{field}={row[field]} want 60000")
if not row["source_fingerprint"]:
    raise SystemExit("source_fingerprint is empty")
PY

source_ref="$(
  python3 - "${artifact_dir}/postgres/volume_meter_ticks.csv" <<'PY'
import csv
import sys

rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8")))
print(rows[0]["source_ref"])
PY
)"

deadline=$((SECONDS + ${VOLUME_BILLING_CLICKHOUSE_TIMEOUT_SECONDS:-180}))
volume_ch_row=""
metering_ch_row=""
while [[ "${SECONDS}" -lt "${deadline}" ]]; do
  volume_ch_row="$(
    ch_query --database forge_metal \
      --param_tick_id="${meter_tick_id}" \
      --query "
        SELECT
          state,
          window_millis,
          billable_live_bytes,
          billable_retained_bytes,
          arrayElement(component_quantities, 'sandbox_durable_volume_live_storage_gib_ms'),
          arrayElement(component_quantities, 'sandbox_durable_volume_retained_snapshot_gib_ms')
        FROM volume_meter_ticks
        WHERE meter_tick_id = toUUID({tick_id:String})
        ORDER BY recorded_at DESC
        LIMIT 1
        FORMAT TSV
      " || true
  )"
  metering_ch_row="$(
    ch_query --database forge_metal \
      --param_source_ref="${source_ref}" \
      --query "
        SELECT
          reserved_quantity,
          actual_quantity,
          billable_quantity,
          arrayElement(component_quantities, 'sandbox_durable_volume_live_storage_gib_ms'),
          arrayElement(component_quantities, 'sandbox_durable_volume_retained_snapshot_gib_ms')
        FROM metering
        WHERE source_type = 'volume_meter_tick'
          AND source_ref = {source_ref:String}
        ORDER BY recorded_at DESC
        LIMIT 1
        FORMAT TSV
      " || true
  )"
  if [[ -n "${volume_ch_row}" && -n "${metering_ch_row}" ]]; then
    break
  fi
  sleep 2
done

printf '%s\n' "${volume_ch_row}" >"${artifact_dir}/clickhouse/volume_meter_ticks.tsv"
printf '%s\n' "${metering_ch_row}" >"${artifact_dir}/clickhouse/metering.tsv"

python3 - "${artifact_dir}/clickhouse/volume_meter_ticks.tsv" "${artifact_dir}/clickhouse/metering.tsv" <<'PY'
import sys

volume = open(sys.argv[1], encoding="utf-8").read().strip().split("\t")
if len(volume) != 6:
    raise SystemExit(f"volume_meter_ticks row missing or malformed: {volume!r}")
if volume[:4] != ["billing_settled", "60000", str(768 * 1024 * 1024), str(256 * 1024 * 1024)]:
    raise SystemExit(f"unexpected volume tick row: {volume!r}")
if abs(float(volume[4]) - 45000.0) > 0.0001:
    raise SystemExit(f"live component quantity={volume[4]} want 45000")
if abs(float(volume[5]) - 15000.0) > 0.0001:
    raise SystemExit(f"retained component quantity={volume[5]} want 15000")

metering = open(sys.argv[2], encoding="utf-8").read().strip().split("\t")
if len(metering) != 5:
    raise SystemExit(f"metering row missing or malformed: {metering!r}")
if metering[:3] != ["60000", "60000", "60000"]:
    raise SystemExit(f"unexpected billing metering quantities: {metering!r}")
if abs(float(metering[3]) - 45000.0) > 0.0001:
    raise SystemExit(f"billing live component quantity={metering[3]} want 45000")
if abs(float(metering[4]) - 15000.0) > 0.0001:
    raise SystemExit(f"billing retained component quantity={metering[4]} want 15000")
PY

cat >"${artifact_dir}/run.json" <<JSON
{
  "run_id": "${run_id}",
  "org_id": "${billing_org_id}",
  "volume_id": "${volume_id}",
  "meter_tick_id": "${meter_tick_id}",
  "billing_window_id": "${billing_window_id}",
  "billed_charge_units": "${billed_charge_units}"
}
JSON

echo "volume billing proof passed: ${artifact_dir}/run.json"
