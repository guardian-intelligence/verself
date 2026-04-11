#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

flow_mode="${SANDBOX_FAST_FLOW:-}"
if [[ -z "${flow_mode}" ]]; then
  echo "SANDBOX_FAST_FLOW is required" >&2
  exit 1
fi

case "${flow_mode}" in
  import)
    spec_path="e2e/repo-journeys.live.spec.ts"
    grep_pattern="repo import renders a stable repo detail page after bootstrap"
    ;;
  refresh)
    spec_path="e2e/repo-journeys.live.spec.ts"
    grep_pattern="repo refresh activates a new source sha after rescan"
    ;;
  execute)
    spec_path="e2e/repo-journeys.live.spec.ts"
    grep_pattern="repo execution preserves jobs index and job detail through hydration"
    ;;
  *)
    echo "unsupported sandbox repo flow: ${flow_mode}" >&2
    exit 1
    ;;
esac

kind="${VERIFICATION_KIND:-sandbox-${flow_mode}}"
run_id="${VERIFICATION_RUN_ID:-${kind}-$(date -u +%Y%m%dT%H%M%SZ)}"
base_url="${TEST_BASE_URL:-${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/${kind}}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
repo_json_path="${artifact_dir}/repo.json"

mkdir -p "${artifact_dir}"

verification_wait_for_http "rent-a-sandbox UI" "${base_url}" "200"

VERIFICATION_REPO_REVISION="${run_id}-seed" \
  "${script_dir}/ensure-verification-repo.sh" "${repo_json_path}"

verification_repo_url="$(
  python3 -c 'import json, sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["public_repo_url"])' \
    "${repo_json_path}"
)"

acme_admin_password="$(
  verification_remote_sudo_cat /etc/credstore/seed-system/acme-admin-password
)"

set +e
env \
  TEST_EMAIL="acme-admin@${VERIFICATION_DOMAIN}" \
  TEST_PASSWORD="${acme_admin_password}" \
  FORGE_METAL_DOMAIN="${VERIFICATION_DOMAIN}" \
  BASE_URL="${base_url}" \
  FORGE_METAL_RECORD_ARTIFACTS="1" \
  VERIFICATION_RUN_ID="${run_id}" \
  VERIFICATION_RUN_JSON_PATH="${run_json_path}" \
  VERIFICATION_REPO_URL="${verification_repo_url}" \
  VERIFICATION_REPO_REF="refs/heads/main" \
  VERIFICATION_LOG_MARKER="FORGE_METAL_VERIFICATION_NEXT_BUN_COMPLETE" \
  bash -lc '
    cd "$1"
    vp exec playwright test "$2" \
      --project=chromium \
      --grep "$3" \
      --output "$4"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/rent-a-sandbox" "${spec_path}" "${grep_pattern}" "${artifact_dir}/playwright-results"
playwright_status=$?
set -e

if [[ -f "${run_json_path}" ]]; then
  "${script_dir}/collect-sandbox-verification-evidence.sh" "${run_json_path}" "${artifact_dir}/evidence"
fi

exit "${playwright_status}"
