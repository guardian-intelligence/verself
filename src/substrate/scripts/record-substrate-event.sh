#!/usr/bin/env bash
# Append substrate convergence evidence to verself.substrate_convergence_events.
set -euo pipefail

site="prod"
digest=""
mode=""
event_kind=""
inventory=""
changed_tasks="0"
error_message=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --site=*) site="${1#*=}"; shift ;;
    --digest=*) digest="${1#*=}"; shift ;;
    --mode=*) mode="${1#*=}"; shift ;;
    --event-kind=*) event_kind="${1#*=}"; shift ;;
    --inventory=*) inventory="${1#*=}"; shift ;;
    --changed-tasks=*) changed_tasks="${1#*=}"; shift ;;
    --error-message=*) error_message="${1#*=}"; shift ;;
    *)
      echo "ERROR: unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

if [[ ! "${digest}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "ERROR: --digest must be a 64-character sha256 hex string" >&2
  exit 2
fi
case "${mode}" in
  auto|always|skip) ;;
  *) echo "ERROR: --mode must be auto|always|skip" >&2; exit 2 ;;
esac
case "${event_kind}" in
  started|succeeded|failed|skipped) ;;
  *) echo "ERROR: --event-kind must be started|succeeded|failed|skipped" >&2; exit 2 ;;
esac
if [[ ! "${changed_tasks}" =~ ^[0-9]+$ ]]; then
  echo "ERROR: --changed-tasks must be an unsigned integer" >&2
  exit 2
fi

run_key="${VERSELF_DEPLOY_RUN_KEY:-}"
if [[ -z "${run_key}" ]]; then
  echo "ERROR: VERSELF_DEPLOY_RUN_KEY is required" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"
substrate_root="${repo_root}/src/substrate"
if [[ -z "${inventory}" ]]; then
  inventory="${substrate_root}/ansible/inventory/${site}.ini"
fi
if [[ -d "${inventory}" ]]; then
  inventory="${inventory}/hosts.ini"
fi

nodes="$(
  INVENTORY_PATH="${inventory}" python3 <<'PY'
import os

path = os.environ["INVENTORY_PATH"]
nodes = []
section = ""
with open(path, encoding="utf-8") as f:
    for raw in f:
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("[") and line.endswith("]"):
            section = line[1:-1]
            continue
        if section.endswith(":vars") or "=" in line.split()[0]:
            continue
        node = line.split()[0]
        if node not in nodes:
            nodes.append(node)
print(",".join(nodes))
PY
)"

if [[ -z "${nodes}" ]]; then
  echo "ERROR: no inventory nodes found in ${inventory}" >&2
  exit 2
fi

escape_ch_string() {
  VALUE="$1" python3 <<'PY'
import os
import sys

sys.stdout.write(os.environ["VALUE"].replace("\\", "\\\\").replace("'", "\\'"))
PY
}

run_key_q="$(escape_ch_string "${run_key}")"
site_q="$(escape_ch_string "${site}")"
digest_q="$(escape_ch_string "${digest}")"
mode_q="$(escape_ch_string "${mode}")"
event_kind_q="$(escape_ch_string "${event_kind}")"
error_q="$(escape_ch_string "${error_message}")"

values="$(
  NODES="${nodes}" python3 <<'PY'
import os

def esc(value: str) -> str:
    return value.replace("\\", "\\\\").replace("'", "\\'")

rows = []
for node in os.environ["NODES"].split(","):
    if node:
        rows.append("'{node}'".format(node=esc(node)))
print(",".join(rows))
PY
)"

query=$(cat <<SQL
INSERT INTO verself.substrate_convergence_events
  (event_at, deploy_run_key, site, node, substrate_digest, mode, event_kind, changed_tasks, error_message)
SELECT
  now64(9),
  '${run_key_q}',
  '${site_q}',
  node,
  '${digest_q}',
  '${mode_q}',
  '${event_kind_q}',
  ${changed_tasks},
  '${error_q}'
FROM (SELECT arrayJoin([${values}]) AS node)
SQL
)

cd "${substrate_root}"
INVENTORY="${inventory}" timeout 5s ./scripts/clickhouse.sh \
  --database verself --query "${query}" >/dev/null
