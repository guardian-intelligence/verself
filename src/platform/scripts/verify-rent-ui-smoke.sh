#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

kind="${VERIFICATION_KIND:-sandbox-ui-smoke}"
run_id="${VERIFICATION_RUN_ID:-${kind}-$(date -u +%Y%m%dT%H%M%SZ)}"
base_url="${TEST_BASE_URL:-}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/${kind}}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
smoke_log_path="${artifact_dir}/shell-smoke.log"

if [[ -z "${base_url}" ]]; then
  echo "TEST_BASE_URL is required" >&2
  exit 1
fi

mkdir -p "${artifact_dir}"
verification_print_artifacts "${artifact_dir}" "${smoke_log_path}" "${run_json_path}"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

verification_wait_for_http "rent-a-sandbox UI" "${base_url}" "200"

acme_admin_password="$(
  verification_remote_sudo_cat /etc/credstore/seed-system/acme-admin-password
)"

set +e
# shellcheck disable=SC2016 # Positional args are expanded inside the child shell.
env \
  VERIFICATION_RUN_ID="${run_id}" \
  VERIFICATION_RUN_JSON_PATH="${run_json_path}" \
  BASE_URL="${base_url}" \
  TEST_BASE_URL="${base_url}" \
  FORGE_METAL_DOMAIN="${VERIFICATION_DOMAIN}" \
  ZITADEL_BASE_URL="https://auth.${VERIFICATION_DOMAIN}" \
  TEST_EMAIL="acme-admin@${VERIFICATION_DOMAIN}" \
  TEST_PASSWORD="${acme_admin_password}" \
  bash -lc '
    cd "$1"
    vp exec playwright test e2e/shell.live.spec.ts \
      --project=chromium \
      --grep "authenticated shell lands on executions and navigates via the rail" \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/rent-a-sandbox" "${artifact_dir}/playwright-results" \
  >"${smoke_log_path}" 2>&1
smoke_status=$?
set -e
ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

set +e
verification_collect_run_or_window_evidence "${run_json_path}" "${artifact_dir}/evidence" "${started_at}" "${ended_at}"
evidence_status=$?
set -e

verification_tail_log_on_failure "${smoke_status}" "${smoke_log_path}" "160"

if [[ "${smoke_status}" -eq 0 && "${evidence_status}" -ne 0 ]]; then
  echo "evidence collection failed after successful UI smoke: ${artifact_dir}/evidence" >&2
  exit "${evidence_status}"
fi

exit "${smoke_status}"
