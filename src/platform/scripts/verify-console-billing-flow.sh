#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

kind="${VERIFICATION_KIND:-console-billing}"
run_id="${VERIFICATION_RUN_ID:-${kind}-$(date -u +%Y%m%dT%H%M%SZ)}"
base_url="${TEST_BASE_URL:-${BASE_URL:-https://console.${VERIFICATION_DOMAIN}}}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/${kind}}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
billing_log_path="${artifact_dir}/billing-flow.log"
cleanup_log_path="${artifact_dir}/billing-cleanup.log"

mkdir -p "${artifact_dir}"
verification_print_artifacts "${artifact_dir}" "${billing_log_path}" "${run_json_path}"
echo "cleanup log: ${cleanup_log_path}"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

verification_wait_for_http "console UI" "${base_url}" "200"

(
  cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
  ansible-playbook -i inventory/hosts.ini playbooks/seed-system.yml --tags billing
)

ceo_password="$(
  verification_remote_sudo_cat /etc/credstore/seed-system/ceo-password
)"

set +e
# shellcheck disable=SC2016 # Positional args are expanded inside the child shell.
env \
  VERIFICATION_RUN_ID="${run_id}" \
  VERIFICATION_RUN_JSON_PATH="${run_json_path}" \
  BASE_URL="${base_url}" \
  TEST_BASE_URL="${base_url}" \
  VERSELF_DOMAIN="${VERIFICATION_DOMAIN}" \
  ZITADEL_BASE_URL="https://auth.${VERIFICATION_DOMAIN}" \
  TEST_EMAIL="ceo@${VERIFICATION_DOMAIN}" \
  TEST_USERNAME="ceo" \
  TEST_FIRST_NAME="CEO" \
  TEST_LAST_NAME="Operator" \
  TEST_PASSWORD="${ceo_password}" \
  VERSELF_RECORD_ARTIFACTS="1" \
  bash -lc '
    cd "$1"
    vp exec playwright test e2e/billing.live.spec.ts \
      --project=chromium \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/console" "${artifact_dir}/playwright-results" \
  >"${billing_log_path}" 2>&1
billing_status=$?
set -e
ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

set +e
verification_collect_run_or_window_evidence "${run_json_path}" "${artifact_dir}/evidence" "${started_at}" "${ended_at}"
evidence_status=$?
set -e

set +e
"${script_dir}/billing-clock.sh" \
  --org platform \
  --product-id sandbox \
  --wall-clock \
  --reason "${run_id}-cleanup" \
  >"${cleanup_log_path}" 2>&1
cleanup_status=$?
set -e
if [[ "${cleanup_status}" -ne 0 ]]; then
  echo "billing clock cleanup failed with status ${cleanup_status}; log: ${cleanup_log_path}" >&2
  tail -n 120 "${cleanup_log_path}" >&2 || true
fi

verification_tail_log_on_failure "${billing_status}" "${billing_log_path}" "200"

if [[ "${billing_status}" -eq 0 && "${evidence_status}" -ne 0 ]]; then
  echo "evidence collection failed after successful billing flow: ${artifact_dir}/evidence" >&2
  exit "${evidence_status}"
fi

if [[ "${billing_status}" -eq 0 && "${cleanup_status}" -ne 0 ]]; then
  exit "${cleanup_status}"
fi

exit "${billing_status}"
