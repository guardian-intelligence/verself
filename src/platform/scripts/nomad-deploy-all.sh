#!/usr/bin/env bash
# nomad-deploy-all.sh — push freshly-rendered job specs onto the bare-metal
# node and invoke /opt/verself/profile/bin/nomad-deploy for every
# component the cue-renderer marked deployment.supervisor=="nomad".
#
# Always-on:
#   - re-render is a caller responsibility (`aspect render --site=<site>`)
#   - scp each rendered .nomad.json to /etc/verself/jobs/<id>.nomad.json
#     so spec drift between controller and box can never sneak past the
#     digest short-circuit
#   - ssh-exec nomad-deploy per component
#
# Optional:
#   --push-binaries  also bazelisk-build + scp the matching service binary
#                    to /opt/verself/profile/bin/<id>. Lets developers
#                    iterate on code without re-running the substrate
#                    playbook.
#
# The component list comes from `nomad-deploy enumerate --index=...`,
# which reads the renderer-emitted .cache/render/<site>/jobs/index.json.
# No YAML parsing or Python heredocs.
set -euo pipefail

usage() {
    cat >&2 <<'EOF'
usage: nomad-deploy-all.sh --site=<site> [--host=<ssh-host>] [--push-binaries]

  --site=<site>     Per-site cache root under .cache/render/<site>/.
  --host=<alias>    SSH host alias. Defaults to the first inventory entry
                    in the [infra] group of src/platform/ansible/inventory/
                    <site>.ini.
  --push-binaries   Also rebuild + scp each component's service binary to
                    /opt/verself/profile/bin/<id> before invoking
                    nomad-deploy. Required when iterating on Go code
                    without re-running the substrate playbook.
EOF
    exit 2
}

SITE=""
HOST=""
PUSH_BINARIES=0
for arg in "$@"; do
    case "${arg}" in
        --site=*) SITE="${arg#--site=}" ;;
        --host=*) HOST="${arg#--host=}" ;;
        --push-binaries) PUSH_BINARIES=1 ;;
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
INDEX="${JOBS_DIR}/index.json"
INVENTORY="${REPO_ROOT}/src/platform/ansible/inventory/${SITE}.ini"

if [[ ! -f "${INDEX}" ]]; then
    echo "[nomad-deploy-all] no ${INDEX} — no nomad-supervised components in this render" >&2
    exit 0
fi

if [[ -z "${HOST}" ]]; then
    HOST=$(awk '/^\[infra\]/{flag=1; next} /^\[/{flag=0} flag && NF && $1 !~ /^#/ {print $1; exit}' "${INVENTORY}")
fi
if [[ -z "${HOST}" ]]; then
    echo "[nomad-deploy-all] could not resolve SSH host; pass --host=<alias>" >&2
    exit 1
fi

# Build nomad-deploy once on the controller; we reuse this binary for the
# `enumerate` subcommand below and ignore it for the per-host invocation
# (the box has its own copy at /opt/verself/profile/bin/nomad-deploy).
(cd "${REPO_ROOT}" && bazelisk build --config=remote-writer //src/cue-renderer/cmd/nomad-deploy:nomad-deploy >/dev/null 2>&1)
NOMAD_DEPLOY=$(cd "${REPO_ROOT}" && bazelisk cquery --output=files //src/cue-renderer/cmd/nomad-deploy:nomad-deploy 2>/dev/null | tail -1)
NOMAD_DEPLOY="${REPO_ROOT}/${NOMAD_DEPLOY}"

# Index format: TSV rows of <component>\t<job_id>\t<bazel_label>\t<output>
mapfile -t INDEX_ROWS < <("${NOMAD_DEPLOY}" enumerate --index="${INDEX}")
if [[ ${#INDEX_ROWS[@]} -eq 0 ]]; then
    echo "[nomad-deploy-all] index.json is empty" >&2
    exit 0
fi

for row in "${INDEX_ROWS[@]}"; do
    IFS=$'\t' read -r component job_id bazel_label output <<<"${row}"
    spec="${JOBS_DIR}/${job_id}.nomad.json"
    echo "[nomad-deploy-all] ${component} -> ${HOST}"

    if [[ "${PUSH_BINARIES}" == "1" ]]; then
        echo "[nomad-deploy-all]   build ${bazel_label}"
        (cd "${REPO_ROOT}" && bazelisk build --config=remote-writer "${bazel_label}")
        bin_path=$(cd "${REPO_ROOT}" && bazelisk cquery --output=files "${bazel_label}" 2>/dev/null | tail -1)
        if [[ -z "${bin_path}" || ! -f "${REPO_ROOT}/${bin_path}" ]]; then
            echo "[nomad-deploy-all] ${component}: bazel cquery did not resolve to a file" >&2
            exit 1
        fi
        echo "[nomad-deploy-all]   scp ${output} (sha256=$(sha256sum "${REPO_ROOT}/${bin_path}" | awk '{print substr($1,1,12)}'))"
        scp -q -o BatchMode=yes "${REPO_ROOT}/${bin_path}" "${HOST}:/tmp/${output}"
        ssh -o BatchMode=yes "${HOST}" "sudo install -o root -g root -m 0755 /tmp/${output} /opt/verself/profile/bin/${output} && rm -f /tmp/${output}"
    fi

    echo "[nomad-deploy-all]   scp ${job_id}.nomad.json"
    scp -q -o BatchMode=yes "${spec}" "${HOST}:/tmp/${job_id}.nomad.json"
    ssh -o BatchMode=yes "${HOST}" "sudo install -o root -g root -m 0644 /tmp/${job_id}.nomad.json /etc/verself/jobs/${job_id}.nomad.json && rm -f /tmp/${job_id}.nomad.json"

    ssh -o BatchMode=yes "${HOST}" "/opt/verself/profile/bin/nomad-deploy --component=${component}"
done
