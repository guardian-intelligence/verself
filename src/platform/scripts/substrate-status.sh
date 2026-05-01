#!/usr/bin/env bash
# Exit 0 when every inventory node has converged to the requested substrate
# digest. Exit 10 for stale/missing evidence and 20 when ClickHouse cannot be
# queried, which lets deploy auto mode bootstrap by running substrate.
set -euo pipefail

site="prod"
digest=""
inventory=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --site=*) site="${1#*=}"; shift ;;
    --digest=*) digest="${1#*=}"; shift ;;
    --inventory=*) inventory="${1#*=}"; shift ;;
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

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"
platform_root="${repo_root}/src/platform"
if [[ -z "${inventory}" ]]; then
  inventory="${platform_root}/ansible/inventory/${site}.ini"
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

site_q="$(escape_ch_string "${site}")"
digest_q="$(escape_ch_string "${digest}")"
nodes_literal="$(
  NODES="${nodes}" python3 <<'PY'
import os

def esc(value: str) -> str:
    return value.replace("\\", "\\\\").replace("'", "\\'")

print("[" + ",".join("'" + esc(n) + "'" for n in os.environ["NODES"].split(",") if n) + "]")
PY
)"

query=$(cat <<SQL
WITH
  ${nodes_literal} AS desired_nodes,
  latest AS
  (
    SELECT
      node,
      argMax(substrate_digest, event_at) AS substrate_digest,
      argMax(event_kind, event_at) AS event_kind
    FROM verself.substrate_convergence_events
    WHERE site = '${site_q}' AND has(desired_nodes, node)
    GROUP BY node
  )
SELECT
  length(desired_nodes) AS desired_count,
  countIf(event_kind = 'succeeded' AND substrate_digest = '${digest_q}') AS fresh_count,
  groupArrayIf(node, event_kind = 'succeeded' AND substrate_digest = '${digest_q}') AS fresh_nodes,
  arrayFilter(n -> NOT has(fresh_nodes, n), desired_nodes) AS stale_nodes
FROM latest
FORMAT TSVRaw
SQL
)

set +e
result="$(
  cd "${platform_root}" &&
    INVENTORY="${inventory}" timeout 5s ./scripts/clickhouse.sh --database verself --query "${query}"
)"
rc=$?
set -e

if [[ ${rc} -ne 0 ]]; then
  echo "substrate status unavailable for site=${site}: ClickHouse query failed" >&2
  exit 20
fi

IFS=$'\t' read -r desired_count fresh_count _fresh_nodes stale_nodes <<<"${result}"
if [[ "${desired_count}" == "${fresh_count}" ]]; then
  echo "substrate fresh site=${site} digest=${digest} nodes=${nodes}"
  exit 0
fi

echo "substrate stale site=${site} digest=${digest} stale_nodes=${stale_nodes}" >&2
exit 10
