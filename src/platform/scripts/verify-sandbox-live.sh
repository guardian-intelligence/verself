#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../../.." && pwd)"
platform_root="${repo_root}/src/platform"
inventory="${platform_root}/ansible/inventory/hosts.ini"
vars_file="${platform_root}/ansible/group_vars/all/main.yml"

run_id="${VERIFICATION_RUN_ID:-sandbox-live-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${repo_root}/artifacts/sandbox-live}"
artifact_dir="${artifact_root}/${run_id}"

mkdir -p "${artifact_dir}"

domain="$(awk -F'"' '/^forge_metal_domain:/{print $2}' "${vars_file}")"
remote_host="$(grep -m1 'ansible_host=' "${inventory}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "${inventory}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"

wait_for_loopback_api() {
  local name="$1"
  local url="$2"
  local expected_status="$3"
  ssh -o IPQoS=none -o StrictHostKeyChecking=no "${remote_user}@${remote_host}" \
    "for _ in \$(seq 1 60); do \
       code=\$(curl -s -o /dev/null -w '%{http_code}' '${url}' || true); \
       if [[ \"\${code}\" == '${expected_status}' ]]; then exit 0; fi; \
       sleep 1; \
     done; \
     echo '${name} did not return ${expected_status} in time' >&2; \
     exit 1"
}

wait_for_public_site() {
  local url="$1"
  for _ in $(seq 1 60); do
    code="$(curl -k -L -s -o /dev/null -w '%{http_code}' "${url}" || true)"
    if [[ "${code}" == "200" ]]; then
      return 0
    fi
    sleep 2
  done
  echo "public site did not resolve to 200 in time: ${url}" >&2
  return 1
}

VERIFICATION_REPO_REVISION="${run_id}-seed" \
  "${script_dir}/ensure-verification-repo.sh" "${artifact_dir}/repo.json"

(
  cd "${platform_root}/ansible"
  ansible-playbook -i inventory/hosts.ini playbooks/verification-reset.yml
  ansible-playbook -i inventory/hosts.ini playbooks/dev-single-node.yml \
    --tags deploy_profile,clickhouse,billing_service,sandbox_rental_service,electric,frontend_auth_sessions,rent_a_sandbox,otelcol,forgejo
  wait_for_loopback_api "billing-service" "http://127.0.0.1:4242/readyz" "200"
  # verification-reset restarts the service stack; wait for the loopback API
  # before seed-system starts probing authz behavior against sandbox-rental.
  wait_for_loopback_api "sandbox-rental-service" "http://127.0.0.1:4243/api/v1/billing/balance" "401"
  ansible-playbook -i inventory/hosts.ini playbooks/seed-system.yml
)

wait_for_public_site "${BASE_URL:-https://rentasandbox.${domain}}"

acme_user_password="$(
  ssh -o IPQoS=none -o StrictHostKeyChecking=no "${remote_user}@${remote_host}" \
    "sudo cat /etc/credstore/seed-system/acme-user-password"
)"

verification_repo_url="$(
  python3 -c 'import json, sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["loopback_repo_url"])' \
    "${artifact_dir}/repo.json"
)"

set +e
TEST_PASSWORD="${acme_user_password}" \
FORGE_METAL_DOMAIN="${domain}" \
BASE_URL="${BASE_URL:-https://rentasandbox.${domain}}" \
FORGE_METAL_RECORD_ARTIFACTS="1" \
VERIFICATION_RUN_ID="${run_id}" \
VERIFICATION_RUN_JSON_PATH="${artifact_dir}/run.json" \
VERIFICATION_REPO_URL="${verification_repo_url}" \
VERIFICATION_REPO_REF="refs/heads/main" \
VERIFICATION_LOG_MARKER="FORGE_METAL_VERIFICATION_NEXT_BUN_COMPLETE" \
pnpm -C "${repo_root}/src/viteplus-monorepo" --filter @forge-metal/rent-a-sandbox \
  exec playwright test e2e/repo-exec-live.spec.ts --project=chromium --output "${artifact_dir}/playwright-results"
playwright_status=$?
set -e

if [[ -f "${artifact_dir}/run.json" ]]; then
  "${script_dir}/collect-sandbox-verification-evidence.sh" "${artifact_dir}/run.json" "${artifact_dir}/evidence"
fi

exit "${playwright_status}"
