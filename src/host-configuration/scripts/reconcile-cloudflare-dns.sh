#!/usr/bin/env bash
# Run the reconcile-cloudflare-dns Go binary and record one row per invocation
# to verself.reconciler_runs.
#
# The binary parses --site, reads the authored desired state from
# src/host-configuration/ansible/group_vars/all/generated/{dns,ops}.yml,
# decrypts cloudflare_api_token from the SOPS-encrypted secrets file via
# `sops -d`, and applies any drift in parallel against Cloudflare's API.
#
# Bash captures the binary's stdout summary line:
#   seen=N diffed=M applied=K
# and unpacks the counters into ClickHouse columns.
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
repo_root="$(cd "${substrate_root}/../.." && pwd)"

# bazel-bin is symlinked at the repo root after a build; the binary may not
# exist there yet on a clean checkout, so build it lazily.
target="//src/host-configuration/cmd/reconcile-cloudflare-dns:reconcile-cloudflare-dns"
binary="${repo_root}/bazel-bin/src/host-configuration/cmd/reconcile-cloudflare-dns/reconcile-cloudflare-dns_/reconcile-cloudflare-dns"
if [[ ! -x "${binary}" ]]; then
  ( cd "${repo_root}" && bazelisk build "${target}" >/dev/null )
fi

escape_ch_string() {
  VALUE="$1" python3 <<'PY'
import os
import sys

sys.stdout.write(os.environ["VALUE"].replace("\\", "\\\\").replace("'", "\\'"))
PY
}

write_row() {
  local kind="$1"
  local seen="$2"
  local diffed="$3"
  local applied="$4"
  local duration_ms="$5"
  local error_message="$6"
  local run_key_q site_q kind_q error_q
  run_key_q="$(escape_ch_string "${run_key}")"
  site_q="$(escape_ch_string "${site}")"
  kind_q="$(escape_ch_string "${kind}")"
  error_q="$(escape_ch_string "${error_message}")"
  local query
  query=$(cat <<SQL
INSERT INTO verself.reconciler_runs
  (event_at, deploy_run_key, site, reconciler, event_kind, targets_seen, targets_diffed, targets_applied, duration_ms, error_message)
VALUES
  (now64(9), '${run_key_q}', '${site_q}', 'cloudflare_dns', '${kind_q}', ${seen}, ${diffed}, ${applied}, ${duration_ms}, '${error_q}')
SQL
)
  ( cd "${substrate_root}" && INVENTORY="ansible/inventory/${site}.ini" timeout 5s ./scripts/clickhouse.sh \
      --database verself --query "${query}" </dev/null >/dev/null )
}

write_row "started" 0 0 0 0 ""

start_ns="$(date +%s%N)"
ansible_dir="${repo_root}/src/host-configuration/ansible"
set +e
output="$( "${binary}" --site="${site}" --ansible-dir="${ansible_dir}" 2>&1 )"
rc=$?
set -e
end_ns="$(date +%s%N)"
duration_ms=$(( (end_ns - start_ns) / 1000000 ))

# Echo binary output for the operator's terminal.
printf '%s\n' "${output}" >&2

# Extract counters from the final "seen=N diffed=M applied=K" line.
summary="$(printf '%s\n' "${output}" | grep -E '^seen=[0-9]+ diffed=[0-9]+ applied=[0-9]+' | tail -1 || true)"
seen=0; diffed=0; applied=0
if [[ -n "${summary}" ]]; then
  seen="$(printf '%s' "${summary}"  | sed -nE 's/.*seen=([0-9]+).*/\1/p')"
  diffed="$(printf '%s' "${summary}" | sed -nE 's/.*diffed=([0-9]+).*/\1/p')"
  applied="$(printf '%s' "${summary}" | sed -nE 's/.*applied=([0-9]+).*/\1/p')"
fi

if [[ "${rc}" -ne 0 ]]; then
  err_msg="reconcile-cloudflare-dns exited ${rc}"
  write_row "failed" "${seen:-0}" "${diffed:-0}" "${applied:-0}" "${duration_ms}" "${err_msg}"
  exit "${rc}"
fi

write_row "succeeded" "${seen:-0}" "${diffed:-0}" "${applied:-0}" "${duration_ms}" ""
echo "[reconcile-cloudflare-dns] site=${site} seen=${seen:-0} diffed=${diffed:-0} applied=${applied:-0} duration_ms=${duration_ms}" >&2
