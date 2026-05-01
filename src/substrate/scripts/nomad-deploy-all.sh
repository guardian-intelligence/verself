#!/usr/bin/env bash
# Publish Bazel-resolved Nomad artifacts and submit resolved job specs.
set -euo pipefail

usage() {
    cat >&2 <<'EOF'
usage: nomad-deploy-all.sh --site=<site> [--host=<ssh-host>] [--publish-only]

  --site=<site>     Site inventory under src/substrate/ansible/inventory/.
  --host=<alias>    SSH host alias. Defaults to the first inventory entry
                    in the [infra] group for the site.
  --publish-only    Publish Garage artifacts and exit before Nomad submit.
EOF
    exit 2
}

SITE="prod"
HOST=""
PUBLISH_ONLY=false
for arg in "$@"; do
    case "${arg}" in
        --site=*) SITE="${arg#--site=}" ;;
        --host=*) HOST="${arg#--host=}" ;;
        --publish-only) PUBLISH_ONLY=true ;;
        -h|--help) usage ;;
        *) echo "unknown arg: ${arg}" >&2; usage ;;
    esac
done

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
INVENTORY="${REPO_ROOT}/src/substrate/ansible/inventory/${SITE}.ini"
SSH_TUNNEL_OPTS=(
    -o BatchMode=yes
    -o ExitOnForwardFailure=yes
    # A stale ControlMaster can authenticate command sessions while rejecting
    # direct-tcpip channels; artifact and Nomad tunnels need fresh permissions.
    -o ControlMaster=no
    -o ControlPath=none
)

port_is_listening() {
    local port="$1"
    [[ -n "$(ss -Htlpn "sport = :${port}" 2>/dev/null)" ]]
}

pick_local_port() {
    local requested_port="$1"
    local label="$2"
    if [[ -n "${requested_port}" ]]; then
        if port_is_listening "${requested_port}"; then
            echo "[nomad-deploy-all] requested ${label} tunnel port ${requested_port} is already listening" >&2
            return 1
        fi
        echo "${requested_port}"
        return 0
    fi

    for _ in {1..100}; do
        local port=$((20000 + RANDOM % 20000))
        if ! port_is_listening "${port}"; then
            echo "${port}"
            return 0
        fi
    done
    echo "[nomad-deploy-all] could not find an unused local port for ${label} tunnel" >&2
    return 1
}

wait_for_tunnel() {
    local pid="$1"
    local port="$2"
    local label="$3"
    for _ in {1..50}; do
        if ! kill -0 "${pid}" >/dev/null 2>&1; then
            local status=0
            wait "${pid}" || status=$?
            echo "[nomad-deploy-all] ${label} SSH tunnel exited before readiness with status ${status}" >&2
            return 1
        fi
        if (echo >"/dev/tcp/127.0.0.1/${port}") >/dev/null 2>&1; then
            return 0
        fi
        sleep 0.1
    done
    echo "[nomad-deploy-all] ${label} SSH tunnel did not become ready on 127.0.0.1:${port}" >&2
    return 1
}

if [[ -z "${HOST}" ]]; then
    HOST=$(awk '/^\[infra\]/{flag=1; next} /^\[/{flag=0} flag && NF && $1 !~ /^#/ {print $1; exit}' "${INVENTORY}")
fi
if [[ -z "${HOST}" ]]; then
    echo "[nomad-deploy-all] could not resolve SSH host; pass --host=<alias>" >&2
    exit 1
fi

(cd "${REPO_ROOT}" && bazelisk build --config=remote-writer \
    //src/cue-renderer:prod_nomad_jobs \
    //src/cue-renderer/cmd/artifact-publish:artifact-publish \
    //src/cue-renderer/cmd/nomad-deploy:nomad-deploy)

NOMAD_JOBS_DIR=$(cd "${REPO_ROOT}" && bazelisk cquery --output=files //src/cue-renderer:prod_nomad_jobs 2>/dev/null | tail -1)
ARTIFACT_PUBLISH=$(cd "${REPO_ROOT}" && bazelisk cquery --output=files //src/cue-renderer/cmd/artifact-publish:artifact-publish 2>/dev/null | tail -1)
NOMAD_DEPLOY=$(cd "${REPO_ROOT}" && bazelisk cquery --output=files //src/cue-renderer/cmd/nomad-deploy:nomad-deploy 2>/dev/null | tail -1)
NOMAD_JOBS_DIR="${REPO_ROOT}/${NOMAD_JOBS_DIR}"
ARTIFACT_PUBLISH="${REPO_ROOT}/${ARTIFACT_PUBLISH}"
NOMAD_DEPLOY="${REPO_ROOT}/${NOMAD_DEPLOY}"

publish_manifest="${NOMAD_JOBS_DIR}/publish.json"
submit_manifest="${NOMAD_JOBS_DIR}/submit.tsv"
if [[ ! -s "${submit_manifest}" ]]; then
    echo "[nomad-deploy-all] no nomad-supervised components in resolved job set" >&2
    exit 0
fi

artifact_count="$(jq -r '.artifacts | length' "${publish_manifest}")"
if [[ "${artifact_count}" != "0" ]]; then
    publisher_env_path="$(jq -r '.artifact_delivery.publisher_credentials.environment_file' "${publish_manifest}")"
    ca_bundle_path="$(jq -r '.artifact_delivery.origin.ca_bundle_path' "${publish_manifest}")"
    if [[ -z "${publisher_env_path}" || "${publisher_env_path}" == "null" ]]; then
        echo "[nomad-deploy-all] publish manifest missing artifact_delivery.publisher_credentials.environment_file" >&2
        exit 1
    fi
    if [[ -z "${ca_bundle_path}" || "${ca_bundle_path}" == "null" ]]; then
        echo "[nomad-deploy-all] publish manifest missing artifact_delivery.origin.ca_bundle_path" >&2
        exit 1
    fi

    tmpdir="$(mktemp -d)"
    local_artifact_port="$(pick_local_port "${NOMAD_ARTIFACT_LOCAL_PORT:-}" "artifact")"
    ssh -N -L "127.0.0.1:${local_artifact_port}:127.0.0.1:9443" "${SSH_TUNNEL_OPTS[@]}" "${HOST}" &
    artifact_tunnel_pid=$!
    cleanup_artifacts() {
        kill "${artifact_tunnel_pid}" >/dev/null 2>&1 || true
        rm -rf "${tmpdir}"
    }
    trap cleanup_artifacts EXIT

    wait_for_tunnel "${artifact_tunnel_pid}" "${local_artifact_port}" "artifact"

    ssh -o BatchMode=yes "${HOST}" "sudo cat $(printf '%q' "${publisher_env_path}")" >"${tmpdir}/publisher.env"
    ssh -o BatchMode=yes "${HOST}" "sudo cat $(printf '%q' "${ca_bundle_path}")" >"${tmpdir}/nomad-artifacts-ca.pem"

    set -a
    # shellcheck disable=SC1091
    . "${tmpdir}/publisher.env"
    set +a

    echo "[nomad-deploy-all] publish ${artifact_count} Garage artifacts"
    "${ARTIFACT_PUBLISH}" \
        --manifest="${publish_manifest}" \
        --repo-root="${REPO_ROOT}" \
        --connect-address="127.0.0.1:${local_artifact_port}" \
        --ca-file="${tmpdir}/nomad-artifacts-ca.pem"

    cleanup_artifacts
    trap - EXIT
fi

if [[ "${PUBLISH_ONLY}" == "true" ]]; then
    exit 0
fi

local_nomad_port="$(pick_local_port "${NOMAD_DEPLOY_LOCAL_PORT:-}" "nomad")"
ssh -N -L "127.0.0.1:${local_nomad_port}:127.0.0.1:4646" "${SSH_TUNNEL_OPTS[@]}" "${HOST}" &
tunnel_pid=$!
cleanup() {
    kill "${tunnel_pid}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_for_tunnel "${tunnel_pid}" "${local_nomad_port}" "nomad"

while IFS=$'\t' read -r job_id spec_file; do
    [[ -n "${job_id}" ]] || continue
    spec="${NOMAD_JOBS_DIR}/${spec_file}"
    echo "[nomad-deploy-all] submit ${job_id}"
    "${NOMAD_DEPLOY}" --spec="${spec}" --nomad-addr="http://127.0.0.1:${local_nomad_port}"
done <"${submit_manifest}"
