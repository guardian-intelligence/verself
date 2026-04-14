#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-sandbox-live-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/sandbox-live}"
artifact_dir="${artifact_root}/${run_id}"

mkdir -p "${artifact_dir}"

VERIFICATION_REPO_REVISION="${run_id}-seed" \
  "${script_dir}/ensure-verification-repo.sh" "${artifact_dir}/repo.json"

(
  cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
  ansible-playbook -i inventory/hosts.ini playbooks/verification-reset.yml
  ansible-playbook -i inventory/hosts.ini playbooks/guest-rootfs.yml
  ansible-playbook -i inventory/hosts.ini playbooks/dev-single-node.yml \
    --tags deploy_profile,firecracker,clickhouse,billing_service,sandbox_rental_service,identity_service,mailbox_service,electric,frontend_auth_sessions,rent_a_sandbox,otelcol,forgejo
  verification_wait_for_loopback_api "billing-service" "http://127.0.0.1:4242/readyz" "200"
  # verification-reset restarts the service stack; wait for the loopback API
  # before seed-system starts probing authz behavior against sandbox-rental.
  verification_wait_for_loopback_api "sandbox-rental-service" \
    "http://127.0.0.1:4243/api/v1/billing/entitlements" "401"
  ansible-playbook -i inventory/hosts.ini playbooks/seed-system.yml
)

verification_wait_for_http \
  "rent-a-sandbox UI" "${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}" "200"

acme_admin_password="$(verification_remote_sudo_cat /etc/credstore/seed-system/acme-admin-password)"

verification_repo_url="$(
  python3 -c 'import json, sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["public_repo_url"])' \
    "${artifact_dir}/repo.json"
)"

set +e
# shellcheck disable=SC2016 # Positional args are expanded inside the child shell.
env \
  TEST_EMAIL="acme-admin@${VERIFICATION_DOMAIN}" \
  TEST_PASSWORD="${acme_admin_password}" \
  FORGE_METAL_DOMAIN="${VERIFICATION_DOMAIN}" \
  BASE_URL="${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}" \
  FORGE_METAL_RECORD_ARTIFACTS="1" \
  FORGE_METAL_SANDBOX_PROOF="1" \
  VERIFICATION_RUN_ID="${run_id}" \
  VERIFICATION_RUN_JSON_PATH="${artifact_dir}/run.json" \
  VERIFICATION_REPO_URL="${verification_repo_url}" \
  VERIFICATION_REPO_REF="refs/heads/main" \
  VERIFICATION_LOG_MARKER="FORGE_METAL_DIRECT_EXECUTION_COMPLETE" \
  bash -lc '
    cd "$1"
    vp exec playwright test e2e/repo-journeys.live.spec.ts \
      --project=chromium \
      --grep "full lifecycle proof imports executes rescans and executes again" \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/rent-a-sandbox" "${artifact_dir}/playwright-results"
playwright_status=$?
set -e

if [[ -f "${artifact_dir}/run.json" ]]; then
  "${script_dir}/collect-sandbox-verification-evidence.sh" "${artifact_dir}/run.json" "${artifact_dir}/evidence"
fi

exit "${playwright_status}"
