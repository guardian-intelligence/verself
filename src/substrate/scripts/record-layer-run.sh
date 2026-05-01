#!/usr/bin/env bash
# Append a row to verself.deploy_layer_runs.
#
# Layered convergence keeps one row per (deploy_run_key, layer, event_kind):
# - started:   layer entered (rare; written for visibility on long layers)
# - skipped:   input_hash matched last_applied_hash; playbook never ran
# - succeeded: playbook ran and exited 0 (or short-circuited on a hash match)
# - failed:    playbook exited non-zero
#
# Hash-gating reads `last_applied_hash` from the most recent succeeded row for
# (site, layer); a new succeeded row is the only thing that advances it. Failed
# runs do not advance it, so a failure followed by an unrelated hash-matching
# input never silently masks a bad state.
set -euo pipefail

site=""
layer=""
input_hash=""
last_applied_hash=""
event_kind=""
skipped="0"
skip_reason=""
duration_ms="0"
changed_count="0"
error_message=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --site=*)               site="${1#*=}"; shift ;;
    --layer=*)              layer="${1#*=}"; shift ;;
    --input-hash=*)         input_hash="${1#*=}"; shift ;;
    --last-applied-hash=*)  last_applied_hash="${1#*=}"; shift ;;
    --event-kind=*)         event_kind="${1#*=}"; shift ;;
    --skipped=*)            skipped="${1#*=}"; shift ;;
    --skip-reason=*)        skip_reason="${1#*=}"; shift ;;
    --duration-ms=*)        duration_ms="${1#*=}"; shift ;;
    --changed-count=*)      changed_count="${1#*=}"; shift ;;
    --error-message=*)      error_message="${1#*=}"; shift ;;
    *)
      echo "ERROR: unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

for var in site layer event_kind input_hash; do
  if [[ -z "${!var}" ]]; then
    echo "ERROR: --${var//_/-} is required" >&2
    exit 2
  fi
done
case "${event_kind}" in
  started|succeeded|failed|skipped) ;;
  *)
    echo "ERROR: --event-kind must be started|succeeded|failed|skipped (got: ${event_kind})" >&2
    exit 2
    ;;
esac
if [[ ! "${input_hash}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "ERROR: --input-hash must be a 64-character sha256 hex string" >&2
  exit 2
fi
# last_applied_hash is empty on first-ever run for the layer.
if [[ -n "${last_applied_hash}" && ! "${last_applied_hash}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "ERROR: --last-applied-hash must be empty or a 64-character sha256 hex string" >&2
  exit 2
fi
if [[ ! "${duration_ms}" =~ ^[0-9]+$ ]]; then
  echo "ERROR: --duration-ms must be an unsigned integer" >&2
  exit 2
fi
if [[ ! "${changed_count}" =~ ^[0-9]+$ ]]; then
  echo "ERROR: --changed-count must be an unsigned integer" >&2
  exit 2
fi
if [[ "${skipped}" != "0" && "${skipped}" != "1" ]]; then
  echo "ERROR: --skipped must be 0 or 1" >&2
  exit 2
fi

run_key="${VERSELF_DEPLOY_RUN_KEY:-}"
if [[ -z "${run_key}" ]]; then
  echo "ERROR: VERSELF_DEPLOY_RUN_KEY is required (threaded in by aspect deploy via derive_deploy_env)" >&2
  exit 2
fi

# Pad last_applied_hash to 64 chars when empty so the FixedString(64) accepts
# the literal. ClickHouse will coerce DEFAULT '' silently when omitted, but the
# explicit insert column list below requires a value.
if [[ -z "${last_applied_hash}" ]]; then
  last_applied_hash="$(printf '0%.0s' {1..64})"
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

run_key_q="$(escape_ch_string "${run_key}")"
site_q="$(escape_ch_string "${site}")"
layer_q="$(escape_ch_string "${layer}")"
input_hash_q="$(escape_ch_string "${input_hash}")"
last_applied_hash_q="$(escape_ch_string "${last_applied_hash}")"
event_kind_q="$(escape_ch_string "${event_kind}")"
skip_reason_q="$(escape_ch_string "${skip_reason}")"
error_q="$(escape_ch_string "${error_message}")"

query=$(cat <<SQL
INSERT INTO verself.deploy_layer_runs
  (event_at, deploy_run_key, site, layer, input_hash, last_applied_hash, event_kind, skipped, skip_reason, duration_ms, changed_count, error_message)
VALUES
  (now64(9), '${run_key_q}', '${site_q}', '${layer_q}', '${input_hash_q}', '${last_applied_hash_q}', '${event_kind_q}', ${skipped}, '${skip_reason_q}', ${duration_ms}, ${changed_count}, '${error_q}')
SQL
)

cd "${substrate_root}"
INVENTORY="ansible/inventory/${site}.ini" timeout 5s ./scripts/clickhouse.sh \
  --database verself --query "${query}" </dev/null >/dev/null
