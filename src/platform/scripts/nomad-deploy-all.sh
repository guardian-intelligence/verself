#!/usr/bin/env bash
# Publish Bazel-resolved Nomad artifacts and submit resolved job specs.
set -euo pipefail

usage() {
    cat >&2 <<'EOF'
usage: nomad-deploy-all.sh --site=<site> [--host=<ssh-host>]

  --site=<site>     Site inventory under src/platform/ansible/inventory/.
  --host=<alias>    SSH host alias. Defaults to the first inventory entry
                    in the [infra] group for the site.
EOF
    exit 2
}

SITE="prod"
HOST=""
for arg in "$@"; do
    case "${arg}" in
        --site=*) SITE="${arg#--site=}" ;;
        --host=*) HOST="${arg#--host=}" ;;
        -h|--help) usage ;;
        *) echo "unknown arg: ${arg}" >&2; usage ;;
    esac
done

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
INVENTORY="${REPO_ROOT}/src/platform/ansible/inventory/${SITE}.ini"

if [[ -z "${HOST}" ]]; then
    HOST=$(awk '/^\[infra\]/{flag=1; next} /^\[/{flag=0} flag && NF && $1 !~ /^#/ {print $1; exit}' "${INVENTORY}")
fi
if [[ -z "${HOST}" ]]; then
    echo "[nomad-deploy-all] could not resolve SSH host; pass --host=<alias>" >&2
    exit 1
fi

(cd "${REPO_ROOT}" && bazelisk build --config=remote-writer \
    //src/cue-renderer:prod_nomad_jobs \
    //src/cue-renderer/cmd/nomad-deploy:nomad-deploy)

NOMAD_JOBS_DIR=$(cd "${REPO_ROOT}" && bazelisk cquery --output=files //src/cue-renderer:prod_nomad_jobs 2>/dev/null | tail -1)
NOMAD_DEPLOY=$(cd "${REPO_ROOT}" && bazelisk cquery --output=files //src/cue-renderer/cmd/nomad-deploy:nomad-deploy 2>/dev/null | tail -1)
NOMAD_JOBS_DIR="${REPO_ROOT}/${NOMAD_JOBS_DIR}"
NOMAD_DEPLOY="${REPO_ROOT}/${NOMAD_DEPLOY}"

publish_manifest="${NOMAD_JOBS_DIR}/publish.tsv"
submit_manifest="${NOMAD_JOBS_DIR}/submit.tsv"
if [[ ! -s "${submit_manifest}" ]]; then
    echo "[nomad-deploy-all] no nomad-supervised components in resolved job set" >&2
    exit 0
fi

declare -A PUBLISHED_REMOTE_PATHS=()
while IFS=$'\t' read -r output local_path remote_path sha; do
    [[ -n "${output}" ]] || continue
    if [[ -n "${PUBLISHED_REMOTE_PATHS[${remote_path}]+x}" ]]; then
        continue
    fi
    PUBLISHED_REMOTE_PATHS["${remote_path}"]=1
    local_file="${REPO_ROOT}/${local_path}"
    if [[ ! -f "${local_file}" ]]; then
        echo "[nomad-deploy-all] artifact ${output} missing at ${local_file}" >&2
        exit 1
    fi

    remote_dir="$(dirname "${remote_path}")"
    tmp_remote_path="/tmp/${output}.tar"
    remote_dir_q=$(printf "%q" "${remote_dir}")
    remote_path_q=$(printf "%q" "${remote_path}")
    tmp_remote_path_q=$(printf "%q" "${tmp_remote_path}")

    echo "[nomad-deploy-all] publish ${output}.tar sha256=${sha:0:12}"
    scp -q -o BatchMode=yes "${local_file}" "${HOST}:${tmp_remote_path}"
    ssh -o BatchMode=yes "${HOST}" "sudo install -d -o caddy -g caddy -m 0755 ${remote_dir_q} && sudo install -o caddy -g caddy -m 0644 ${tmp_remote_path_q} ${remote_path_q} && rm -f ${tmp_remote_path_q}"
done <"${publish_manifest}"

local_nomad_port="${NOMAD_DEPLOY_LOCAL_PORT:-14646}"
ssh -N -L "127.0.0.1:${local_nomad_port}:127.0.0.1:4646" -o BatchMode=yes "${HOST}" &
tunnel_pid=$!
cleanup() {
    kill "${tunnel_pid}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for _ in {1..50}; do
    if (echo >"/dev/tcp/127.0.0.1/${local_nomad_port}") >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done

while IFS=$'\t' read -r job_id spec_file; do
    [[ -n "${job_id}" ]] || continue
    spec="${NOMAD_JOBS_DIR}/${spec_file}"
    echo "[nomad-deploy-all] submit ${job_id}"
    "${NOMAD_DEPLOY}" --spec="${spec}" --nomad-addr="http://127.0.0.1:${local_nomad_port}"
done <"${submit_manifest}"
