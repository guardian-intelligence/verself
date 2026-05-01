#!/usr/bin/env bash
# Print the input_hash of the most recent succeeded run for (site, layer), or
# empty when no succeeded run has been recorded. Exit 0 on success (including
# the no-row case); exit 20 when the ClickHouse query itself fails so the
# caller can refuse to gate on missing evidence.
#
# Used by aspect deploy to decide whether to skip a layer:
#   if current_input_hash == last_applied_hash: emit a 'skipped' row, do not run
#   else: run the layer playbook, emit succeeded/failed
set -euo pipefail

site="prod"
layer=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --site=*)  site="${1#*=}"; shift ;;
    --layer=*) layer="${1#*=}"; shift ;;
    *)
      echo "ERROR: unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

if [[ -z "${layer}" ]]; then
  echo "ERROR: --layer is required" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"
substrate_root="${repo_root}/src/substrate"

escape_ch_string() {
  VALUE="$1" python3 <<'PY'
import os
import sys

sys.stdout.write(os.environ["VALUE"].replace("\\", "\\\\").replace("'", "\\'"))
PY
}

site_q="$(escape_ch_string "${site}")"
layer_q="$(escape_ch_string "${layer}")"

# argMax over event_at picks the latest by timestamp; restrict to event_kind in
# (succeeded, skipped) because both advance the applied state.
query=$(cat <<SQL
SELECT argMax(input_hash, event_at)
FROM verself.deploy_layer_runs
WHERE site = '${site_q}'
  AND layer = '${layer_q}'
  AND event_kind IN ('succeeded', 'skipped')
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
  echo "ERROR: ClickHouse query failed for layer-last-applied site=${site} layer=${layer}" >&2
  exit 20
fi

# argMax returns the empty string padded to FixedString(64) of zero bytes when
# no rows match; treat that as no evidence.
result="$(printf '%s' "${result}" | tr -d '\r\n' | sed 's/[[:space:]]//g')"
if [[ -z "${result}" || "${result}" =~ ^0+$ ]]; then
  printf ''
else
  printf '%s' "${result}"
fi
