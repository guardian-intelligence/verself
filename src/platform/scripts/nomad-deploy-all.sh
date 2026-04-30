#!/usr/bin/env bash
# nomad-deploy-all.sh — invoke /opt/verself/profile/bin/nomad-deploy on the
# bare-metal node for every component the cue-renderer marked as
# deployment.supervisor=="nomad". One SSH-exec per component; the tool
# itself reads the rendered spec from /etc/verself/jobs/<id>.nomad.json
# (placed there by the substrate playbook), computes the binary digest,
# and submits to the local Nomad agent.
#
# The script returns a non-zero exit on the FIRST failure and propagates
# the underlying nomad-deploy error so aspect deploy can mark
# verself.deploy_events.event_kind=failed with a useful error_message.
set -euo pipefail

usage() {
    echo "usage: $0 --site=<site> [--host=<ssh-host>]" >&2
    exit 2
}

SITE=""
HOST=""
for arg in "$@"; do
    case "${arg}" in
        --site=*) SITE="${arg#--site=}" ;;
        --host=*) HOST="${arg#--host=}" ;;
        -h|--help) usage ;;
        *) echo "unknown arg: ${arg}" >&2; usage ;;
    esac
done

if [[ -z "${SITE}" ]]; then
    SITE="prod"
fi

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
CACHE_DIR="${REPO_ROOT}/.cache/render/${SITE}"
JOBS_DIR="${CACHE_DIR}/jobs"
INVENTORY="${REPO_ROOT}/src/platform/ansible/inventory/${SITE}.ini"

if [[ ! -d "${JOBS_DIR}" ]]; then
    echo "[nomad-deploy-all] no ${JOBS_DIR} — no nomad-supervised components in this render" >&2
    exit 0
fi

if [[ -z "${HOST}" ]]; then
    # Single-node deployment: use the first host in the [infra] group.
    HOST=$(awk '/^\[infra\]/{flag=1; next} /^\[/{flag=0} flag && NF && $1 !~ /^#/ {print $1; exit}' "${INVENTORY}")
fi
if [[ -z "${HOST}" ]]; then
    echo "[nomad-deploy-all] could not resolve SSH host; pass --host=<alias>" >&2
    exit 1
fi

shopt -s nullglob
specs=("${JOBS_DIR}"/*.nomad.json)
if [[ ${#specs[@]} -eq 0 ]]; then
    echo "[nomad-deploy-all] no *.nomad.json under ${JOBS_DIR}" >&2
    exit 0
fi

for spec in "${specs[@]}"; do
    component_id=$(basename "${spec}" .nomad.json)
    component=${component_id//-/_}
    echo "[nomad-deploy-all] ${component} -> ${HOST}"
    ssh -o BatchMode=yes "${HOST}" "/opt/verself/profile/bin/nomad-deploy --component=${component}"
done
