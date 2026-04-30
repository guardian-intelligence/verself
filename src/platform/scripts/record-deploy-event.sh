#!/usr/bin/env bash
# Append a row to verself.deploy_events.
#
# The row carries the deploy_run_key derived by deploy_identity.sh, and
# correlates to the ansible.* / cue_renderer.run span family in
# default.otel_traces on ResourceAttributes['verself.deploy_run_key'].
#
# Usage:
#   record-deploy-event.sh \
#     --site=<site> --sha=<40-char> --scope=<all|affected> \
#     --components=<comma,separated> --event-kind=<started|succeeded|failed> \
#     [--duration-ms=N] [--error-message=...]
#
# Required env (the aspect deploy task sources deploy_identity.sh once and
# threads the result via env={} into this script — see
# .aspect/lib/helpers.axl::derive_deploy_env):
#   VERSELF_DEPLOY_RUN_KEY — correlation key
#   VERSELF_AUTHOR         — git committer email
set -euo pipefail

site=""
sha=""
scope="all"
components=""
event_kind=""
duration_ms="0"
error_message=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --site=*)          site="${1#*=}"; shift ;;
    --sha=*)           sha="${1#*=}"; shift ;;
    --scope=*)         scope="${1#*=}"; shift ;;
    --components=*)    components="${1#*=}"; shift ;;
    --event-kind=*)    event_kind="${1#*=}"; shift ;;
    --duration-ms=*)   duration_ms="${1#*=}"; shift ;;
    --error-message=*) error_message="${1#*=}"; shift ;;
    *)
      echo "ERROR: unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

for var in site sha event_kind; do
  if [[ -z "${!var}" ]]; then
    echo "ERROR: --${var//_/-} is required" >&2
    exit 2
  fi
done
if [[ ! "${sha}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "ERROR: --sha must be a 40-character hex string (got: ${sha})" >&2
  exit 2
fi
case "${event_kind}" in
  started|succeeded|failed) ;;
  *)
    echo "ERROR: --event-kind must be one of started|succeeded|failed (got: ${event_kind})" >&2
    exit 2
    ;;
esac

run_key="${VERSELF_DEPLOY_RUN_KEY:-}"
if [[ -z "${run_key}" ]]; then
  echo "ERROR: VERSELF_DEPLOY_RUN_KEY is required (threaded in by aspect deploy via derive_deploy_env)" >&2
  exit 2
fi
actor="${VERSELF_AUTHOR:-unknown}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"

# Build the components array literal: ['a', 'b'] — escaping for ClickHouse string literals.
escape_ch_string() {
  VALUE="$1" python3 <<'PY'
import os
import sys

sys.stdout.write(os.environ["VALUE"].replace("\\", "\\\\").replace("'", "\\'"))
PY
}
components_literal="[]"
if [[ -n "${components}" ]]; then
  IFS=',' read -ra comp_arr <<<"${components}"
  parts=()
  for c in "${comp_arr[@]}"; do
    c="$(printf "%s" "$c" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
    [[ -z "$c" ]] && continue
    parts+=("'$(escape_ch_string "$c")'")
  done
  components_literal="[$(IFS=,; printf "%s" "${parts[*]}")]"
fi

run_key_q="$(escape_ch_string "${run_key}")"
site_q="$(escape_ch_string "${site}")"
sha_q="$(escape_ch_string "${sha}")"
actor_q="$(escape_ch_string "${actor}")"
scope_q="$(escape_ch_string "${scope}")"
event_kind_q="$(escape_ch_string "${event_kind}")"
error_q="$(escape_ch_string "${error_message}")"

query=$(cat <<SQL
INSERT INTO verself.deploy_events
  (event_at, deploy_run_key, site, sha, actor, scope, affected_components, event_kind, duration_ms, error_message)
VALUES
  (now64(9), '${run_key_q}', '${site_q}', '${sha_q}', '${actor_q}', '${scope_q}', ${components_literal}, '${event_kind_q}', ${duration_ms}, '${error_q}')
SQL
)

# The row in verself.deploy_events is the canonical record; we deliberately
# do not emit a separate `deploy_events.insert` OTel span here because the
# controller-side OTLP agent is owned by scripts/with-otel-agent.sh and is
# not in scope when this script runs (recording happens before/after the
# agent's lifetime). The row carries the same dimensions.

cd "${repo_root}/src/platform"
INVENTORY="ansible/inventory/${site}.ini" timeout 5s ./scripts/clickhouse.sh \
  --database verself --query "${query}" >/dev/null
