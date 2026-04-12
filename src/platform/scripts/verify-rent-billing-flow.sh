#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

kind="${VERIFICATION_KIND:-sandbox-billing}"
run_id="${VERIFICATION_RUN_ID:-${kind}-$(date -u +%Y%m%dT%H%M%SZ)}"
base_url="${TEST_BASE_URL:-${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/${kind}}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
billing_log_path="${artifact_dir}/billing-flow.log"

mkdir -p "${artifact_dir}"

verification_wait_for_http "rent-a-sandbox UI" "${base_url}" "200"

(
  cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
  ansible-playbook -i inventory/hosts.ini playbooks/seed-system.yml --tags billing
)

ceo_password="$(
  verification_remote_sudo_cat /etc/credstore/seed-system/ceo-password
)"

set +e
env \
  VERIFICATION_RUN_ID="${run_id}" \
  VERIFICATION_RUN_JSON_PATH="${run_json_path}" \
  BASE_URL="${base_url}" \
  TEST_BASE_URL="${base_url}" \
  FORGE_METAL_DOMAIN="${VERIFICATION_DOMAIN}" \
  ZITADEL_BASE_URL="https://auth.${VERIFICATION_DOMAIN}" \
  TEST_EMAIL="ceo@${VERIFICATION_DOMAIN}" \
  TEST_USERNAME="ceo" \
  TEST_FIRST_NAME="CEO" \
  TEST_LAST_NAME="Operator" \
  TEST_PASSWORD="${ceo_password}" \
  FORGE_METAL_RECORD_ARTIFACTS="1" \
  bash -lc '
    cd "$1"
    vp exec playwright test e2e/billing.live.spec.ts \
      --project=chromium \
      --grep "subscription checkout activates Hobby and leaves it active" \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/rent-a-sandbox" "${artifact_dir}/playwright-results" \
  >"${billing_log_path}" 2>&1
billing_status=$?
set -e

if [[ -f "${run_json_path}" ]]; then
  "${script_dir}/collect-sandbox-verification-evidence.sh" "${run_json_path}" "${artifact_dir}/evidence"
fi

exit "${billing_status}"
