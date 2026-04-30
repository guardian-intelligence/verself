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
# Return code: non-zero on the FIRST failure (binary build, scp, or
# nomad-deploy). The aspect deploy verb propagates the rc into
# verself.deploy_events.error_message.
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
COMPONENTS_YML="${CACHE_DIR}/inventory/group_vars/all/generated/components.yml"
INVENTORY="${REPO_ROOT}/src/platform/ansible/inventory/${SITE}.ini"

if [[ ! -d "${JOBS_DIR}" ]]; then
    echo "[nomad-deploy-all] no ${JOBS_DIR} — no nomad-supervised components in this render" >&2
    exit 0
fi

if [[ -z "${HOST}" ]]; then
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

# Resolve the bazel_label for a component name from the rendered
# components.yml. Lets --push-binaries skip a separate label lookup.
bazel_label_for() {
    local component="$1"
    python3 - "$component" "$COMPONENTS_YML" <<'PY'
import sys, yaml
component = sys.argv[1]
with open(sys.argv[2]) as f:
    data = yaml.safe_load(f) or {}
for c in data.get("topology_components", []):
    if c.get("name") != component:
        continue
    for src in (c.get("converge", {}).get("systemd", {}).get("units", []) or []):
        # No bazel_label on units; fall through to topology_deploy below.
        pass
PY
}

resolve_artifact() {
    local component="$1"
    python3 - "$component" "$CACHE_DIR/inventory/group_vars/all/generated/deploy.yml" <<'PY'
import sys, yaml
component = sys.argv[1]
with open(sys.argv[2]) as f:
    data = yaml.safe_load(f) or {}
for art in data.get("topology_deploy", {}).get("artifacts", []):
    if art.get("component") != component or art.get("source") != "primary":
        continue
    a = art.get("artifact", {}) or {}
    if a.get("kind") != "go_binary":
        continue
    print(a.get("bazel_label", ""))
    print(a.get("output", ""))
    sys.exit(0)
sys.exit("no go_binary primary artifact for {}".format(component))
PY
}

for spec in "${specs[@]}"; do
    component_id=$(basename "${spec}" .nomad.json)
    component=${component_id//-/_}

    echo "[nomad-deploy-all] ${component} -> ${HOST}"

    if [[ "${PUSH_BINARIES}" == "1" ]]; then
        mapfile -t artifact < <(resolve_artifact "${component}")
        bazel_label="${artifact[0]}"
        output_name="${artifact[1]}"
        if [[ -z "${bazel_label}" || -z "${output_name}" ]]; then
            echo "[nomad-deploy-all] ${component}: no go_binary artifact in deploy.yml" >&2
            exit 1
        fi
        echo "[nomad-deploy-all]   build ${bazel_label}"
        (cd "${REPO_ROOT}" && bazelisk build --config=remote-writer "${bazel_label}")
        bin_path=$(cd "${REPO_ROOT}" && bazelisk cquery --output=files "${bazel_label}" 2>/dev/null | tail -1)
        if [[ -z "${bin_path}" || ! -f "${REPO_ROOT}/${bin_path}" ]]; then
            echo "[nomad-deploy-all] ${component}: bazel cquery did not resolve to a file" >&2
            exit 1
        fi
        echo "[nomad-deploy-all]   scp ${output_name} (sha256=$(sha256sum "${REPO_ROOT}/${bin_path}" | awk '{print substr($1,1,12)}'))"
        scp -q -o BatchMode=yes "${REPO_ROOT}/${bin_path}" "${HOST}:/tmp/${output_name}"
        ssh -o BatchMode=yes "${HOST}" "sudo install -o root -g root -m 0755 /tmp/${output_name} /opt/verself/profile/bin/${output_name} && rm -f /tmp/${output_name}"
    fi

    echo "[nomad-deploy-all]   scp ${component_id}.nomad.json"
    scp -q -o BatchMode=yes "${spec}" "${HOST}:/tmp/${component_id}.nomad.json"
    ssh -o BatchMode=yes "${HOST}" "sudo install -o root -g root -m 0644 /tmp/${component_id}.nomad.json /etc/verself/jobs/${component_id}.nomad.json && rm -f /tmp/${component_id}.nomad.json"

    ssh -o BatchMode=yes "${HOST}" "/opt/verself/profile/bin/nomad-deploy --component=${component}"
done
