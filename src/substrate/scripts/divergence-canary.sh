#!/usr/bin/env bash
# Post-deploy assertion: every substrate layer must have produced exactly one
# deploy_layer_runs row for this deploy_run_key, each with a non-zero
# input_hash and an event_kind in (succeeded, skipped). A missing or
# malformed row means run-layer.sh / record-layer-run.sh failed silently
# somewhere — gating Nomad rollouts on a half-recorded substrate is the bug
# this canary exists to catch.
#
# Future phases extend the canary to host-drift detection by running each
# skipped layer's playbook in --check mode and asserting zero changes; that
# requires Phase 6's external-API extraction first so --check is fast enough
# to run on every deploy.
set -euo pipefail

site="prod"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --site=*) site="${1#*=}"; shift ;;
    *)
      echo "ERROR: unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

run_key="${VERSELF_DEPLOY_RUN_KEY:-}"
if [[ -z "${run_key}" ]]; then
  echo "ERROR: VERSELF_DEPLOY_RUN_KEY is required (threaded by aspect deploy)" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
substrate_root="$(cd "${script_dir}/.." && pwd)"

escape_ch_string() {
  VALUE="$1" python3 <<'PY'
import os
import sys

sys.stdout.write(os.environ["VALUE"].replace("\\", "\\\\").replace("'", "\\'"))
PY
}

run_key_q="$(escape_ch_string "${run_key}")"
site_q="$(escape_ch_string "${site}")"

# Expected layer set; canary fails if any of these is missing for the run.
expected_layers=(l1_os l2_userspace l3_binaries l4a_components)
expected_count="${#expected_layers[@]}"

query=$(cat <<SQL
WITH
  rows AS
  (
    SELECT
      layer,
      argMax(event_kind, event_at) AS event_kind,
      argMax(input_hash, event_at)  AS input_hash,
      argMax(skipped, event_at)     AS skipped,
      argMax(changed_count, event_at) AS changed_count
    FROM verself.deploy_layer_runs
    WHERE site = '${site_q}' AND deploy_run_key = '${run_key_q}'
    GROUP BY layer
  )
SELECT
  count()                                     AS row_count,
  countIf(event_kind = 'failed')              AS failed_count,
  countIf(input_hash = repeat('0', 64))       AS empty_hash_count,
  groupArray(layer)                           AS observed_layers,
  sumIf(changed_count, skipped = 1)           AS changed_inside_skipped
FROM rows
FORMAT TSVRaw
SQL
)

set +e
result="$(
  cd "${substrate_root}" &&
    INVENTORY="ansible/inventory/${site}.ini" timeout 5s ./scripts/clickhouse.sh \
      --database verself --query "${query}" </dev/null
)"
rc=$?
set -e

if [[ ${rc} -ne 0 ]]; then
  echo "ERROR: divergence canary ClickHouse query failed for run_key=${run_key}" >&2
  exit 30
fi

IFS=$'\t' read -r row_count failed_count empty_hash_count observed_layers changed_inside_skipped <<<"${result}"

problems=()
if [[ "${row_count}" -ne "${expected_count}" ]]; then
  problems+=("expected ${expected_count} layer rows; observed ${row_count} (${observed_layers})")
fi
if [[ "${failed_count}" -gt 0 ]]; then
  problems+=("${failed_count} layer(s) recorded event_kind=failed")
fi
if [[ "${empty_hash_count}" -gt 0 ]]; then
  problems+=("${empty_hash_count} layer row(s) had a zero input_hash (recording bug)")
fi
if [[ "${changed_inside_skipped}" -gt 0 ]]; then
  problems+=("${changed_inside_skipped} task(s) ran changed inside a skipped layer (drift)")
fi

if [[ ${#problems[@]} -gt 0 ]]; then
  echo "DIVERGENCE: deploy_run_key=${run_key} site=${site}" >&2
  for p in "${problems[@]}"; do
    echo "  - ${p}" >&2
  done
  exit 31
fi

echo "[divergence-canary] deploy_run_key=${run_key} site=${site} ledger=clean (${row_count} layers)" >&2
