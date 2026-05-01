#!/usr/bin/env bash
# Run one layer of the substrate converge with hash-gating.
#
# Replaces the monolithic ansible-playbook playbooks/box.yml call with a
# per-layer primitive: read the layer's last-applied input_hash from
# verself.deploy_layer_runs, compare to the supplied input_hash, and either
# short-circuit (skipped) or invoke ansible-with-otel.sh against the layer's
# playbook (succeeded/failed). Either way emit one row to
# verself.deploy_layer_runs so the divergence canary and the next deploy's
# hash-gate decision have a queryable record.
#
# Usage:
#   run-layer.sh \
#     --site=prod \
#     --layer=l1_os \
#     --input-hash=<64-hex> \
#     --playbook=playbooks/l1_os.yml \
#     --inventory=/abs/path/to/inventory \
#     [--force]                 # ignore the hash; always run
#     [--ansible-arg=...]*      # extra args passed to ansible-playbook
#
# Required env (threaded by aspect deploy):
#   VERSELF_DEPLOY_RUN_KEY, OTEL_*, TRACEPARENT, VERSELF_*
set -euo pipefail

site=""
layer=""
input_hash=""
playbook=""
inventory=""
force="0"
ansible_args=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --site=*)         site="${1#*=}"; shift ;;
    --layer=*)        layer="${1#*=}"; shift ;;
    --input-hash=*)   input_hash="${1#*=}"; shift ;;
    --playbook=*)     playbook="${1#*=}"; shift ;;
    --inventory=*)    inventory="${1#*=}"; shift ;;
    --force)          force="1"; shift ;;
    --ansible-arg=*)  ansible_args+=("${1#*=}"); shift ;;
    *)
      echo "ERROR: unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

for var in site layer input_hash playbook inventory; do
  if [[ -z "${!var}" ]]; then
    echo "ERROR: --${var//_/-} is required" >&2
    exit 2
  fi
done
if [[ ! "${input_hash}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "ERROR: --input-hash must be a 64-character sha256 hex string" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 1. Read the most recent succeeded/skipped input_hash for this layer.
#    Empty when no prior run; layer-last-applied exits 20 on ClickHouse error.
last_applied=""
if last_applied="$(${script_dir}/layer-last-applied.sh --site="${site}" --layer="${layer}")"; then
  :
else
  rc=$?
  echo "WARN: layer-last-applied exited ${rc}; treating as no prior evidence and forcing run" >&2
  last_applied=""
  force="1"
fi

# 2. Skip-path: hash matches and not forced.
if [[ "${force}" == "0" && -n "${last_applied}" && "${last_applied}" == "${input_hash}" ]]; then
  ${script_dir}/record-layer-run.sh \
    --site="${site}" \
    --layer="${layer}" \
    --input-hash="${input_hash}" \
    --last-applied-hash="${last_applied}" \
    --event-kind="skipped" \
    --skipped="1" \
    --skip-reason="input_hash matches last_applied_hash" \
    --duration-ms="0" \
    --changed-count="0"
  echo "[run-layer] layer=${layer} skipped (hash=${input_hash:0:12}…)" >&2
  exit 0
fi

# 3. Run-path: invoke ansible-with-otel.sh and time it. The wrapper writes the
#    parsed PLAY RECAP changed-task count to VERSELF_SUBSTRATE_CHANGED_TASKS_FILE
#    on success (and on a parseable PLAY RECAP failure).
changed_tasks_file="$(mktemp -t "verself-${layer}-changed.XXXXXX")"
trap 'rm -f "${changed_tasks_file}"' EXIT

start_ns="$(date +%s%N)"
set +e
VERSELF_ANSIBLE_INVENTORY="${inventory}" \
  VERSELF_SUBSTRATE_CHANGED_TASKS_FILE="${changed_tasks_file}" \
  VERSELF_LAYER="${layer}" \
  "${script_dir}/ansible-with-otel.sh" "${playbook}" "${ansible_args[@]}"
ansible_rc=$?
set -e
end_ns="$(date +%s%N)"
duration_ms="$(( (end_ns - start_ns) / 1000000 ))"

changed_count="0"
if [[ -s "${changed_tasks_file}" ]]; then
  changed_count="$(tr -d '[:space:]' < "${changed_tasks_file}")"
  if [[ ! "${changed_count}" =~ ^[0-9]+$ ]]; then
    echo "WARN: changed-tasks file ${changed_tasks_file} held non-integer; defaulting to 0" >&2
    changed_count="0"
  fi
fi

if [[ "${ansible_rc}" -ne 0 ]]; then
  ${script_dir}/record-layer-run.sh \
    --site="${site}" \
    --layer="${layer}" \
    --input-hash="${input_hash}" \
    --last-applied-hash="${last_applied}" \
    --event-kind="failed" \
    --skipped="0" \
    --duration-ms="${duration_ms}" \
    --changed-count="${changed_count}" \
    --error-message="ansible-playbook ${playbook} exited ${ansible_rc}"
  exit "${ansible_rc}"
fi

${script_dir}/record-layer-run.sh \
  --site="${site}" \
  --layer="${layer}" \
  --input-hash="${input_hash}" \
  --last-applied-hash="${last_applied}" \
  --event-kind="succeeded" \
  --skipped="0" \
  --duration-ms="${duration_ms}" \
  --changed-count="${changed_count}"
