#!/usr/bin/env bash
# detect-intrusions — query verself.host_auth_events for recent
# accepted SSH events whose cert_id is not in the trusted set
# authored in ops.yml's known_cert_id_suffixes.
#
# What this catches (the Blacksmith-CI class):
#   1. outcome='accepted' AND auth_method != 'publickey-cert' — any
#      access path other than an OpenBao-issued cert. Should be 0
#      post-cutover.
#   2. outcome='accepted' AND auth_method='publickey-cert' AND cert_id
#      shape isn't 'verself-(operator|workload|breakglass)-<suffix>' —
#      a cert minted outside the conventions.
#   3. outcome='accepted' AND a valid-shaped cert_id whose suffix is
#      not in known_cert_id_suffixes — a device or slot that exists in
#      OpenBao but not in authored topology (or one that was deleted but
#      hasn't yet been revoked from the OpenBao role).
#
# The list of allowed suffixes is authored at:
#   src/host-configuration/ansible/group_vars/all/generated/ops.yml
# under `known_cert_id_suffixes`. This script slurps that list and
# embeds it in the WHERE clause.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
substrate_root="$(cd "${script_dir}/.." && pwd)"
repo_root="$(cd "${substrate_root}/../.." && pwd)"
site="${VERSELF_SITE:-prod}"
hours=24
# ClickHouse format names match clickhouse-client's --format flag.
# PrettyCompactMonoBlock is the default ergonomic table view; pass
# --format=JSON for machine-readable output, or --format=TSVRaw for
# pipeable text.
format=PrettyCompactMonoBlock

while [[ $# -gt 0 ]]; do
    case "$1" in
        --site=*) site="${1#--site=}"; shift ;;
        --site) site="$2"; shift 2 ;;
        --hours=*) hours="${1#--hours=}"; shift ;;
        --hours) hours="$2"; shift 2 ;;
        --format=*) format="${1#--format=}"; shift ;;
        --format) format="$2"; shift 2 ;;
        *) echo "detect-intrusions: unknown argument: $1" >&2; exit 2 ;;
    esac
done

export VERSELF_SITE="${site}"

ops_yml="${repo_root}/src/host-configuration/ansible/group_vars/all/generated/ops.yml"
if [[ ! -f "${ops_yml}" ]]; then
    echo "ERROR: authored topology ops.yml is missing." >&2
    echo "  expected: ${ops_yml}" >&2
    exit 1
fi

# Extract the trusted suffixes via python (yq isn't a guaranteed dep).
suffixes_csv=$(KNOWN_OPS_YML="${ops_yml}" python3 <<'PY'
import os, sys
import yaml

with open(os.environ["KNOWN_OPS_YML"], "r") as f:
    payload = yaml.safe_load(f)

suffixes = payload.get("known_cert_id_suffixes") or []
if not suffixes:
    sys.stderr.write("known_cert_id_suffixes is empty in authored ops.yml; cannot detect anomalies\n")
    sys.exit(1)

# CSV-safe single-quoted SQL literal list.
def ch_string(value):
    return "'" + str(value).replace("\\", "\\\\").replace("'", "\\'") + "'"

print(",".join(ch_string(s) for s in suffixes))
PY
)

# A cert_id stamped by aspect-operator + verself-workload-bootstrap is
# `verself-<principal>-<suffix>`. The regex below is anchored, so any
# cert with `verself-foo` or `notverself-...` falls into the unstamped
# bucket below.
sql=$(cat <<EOF
SELECT
    recorded_at,
    outcome,
    auth_method,
    cert_id,
    user,
    source_ip,
    body
FROM verself.host_auth_events
WHERE event_date >= today() - 1
  AND recorded_at >= now() - INTERVAL ${hours} HOUR
  AND outcome = 'accepted'
  AND (
       auth_method != 'publickey-cert'
    OR NOT match(cert_id, '^verself-(operator|workload|breakglass)-[a-z0-9-]+\$')
    -- splitByRegexp is overkill; the principal segment is always one
    -- of three known values, so a fixed-length prefix strip (length
    -- of 'verself-operator-' / 'verself-workload-' / 'verself-breakglass-')
    -- is the simplest reliable suffix extractor that supports
    -- multi-dash device names (e.g. 'shovon-mbp').
    OR replaceRegexpOne(cert_id, '^verself-(operator|workload|breakglass)-', '') NOT IN (${suffixes_csv})
  )
ORDER BY recorded_at DESC
FORMAT ${format}
EOF
)

exec "${script_dir}/clickhouse.sh" --query "${sql}"
