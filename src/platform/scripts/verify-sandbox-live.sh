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

(
  cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
  ansible-playbook -i inventory/hosts.ini playbooks/verification-reset.yml \
    --tags deploy_profile,clickhouse,tigerbeetle,postgresql,billing_service,sandbox_rental_service,otelcol,grafana
  ansible-playbook -i inventory/hosts.ini playbooks/guest-rootfs.yml
  ansible-playbook -i inventory/hosts.ini playbooks/dev-single-node.yml \
    --tags deploy_profile,caddy,firecracker,clickhouse,billing_service,sandbox_rental_service,identity_service,mailbox_service,otelcol,forgejo
  verification_wait_for_loopback_api "billing-service" "http://127.0.0.1:4242/readyz" "200"
  # verification-reset restarts the service stack; wait for the loopback API
  # before seed-system starts probing authz behavior against sandbox-rental.
  verification_wait_for_loopback_api "sandbox-rental-service" \
    "http://127.0.0.1:4243/api/v1/billing/entitlements" "401"
  ansible-playbook -i inventory/hosts.ini playbooks/seed-system.yml
)

VERIFICATION_RUN_ID="${run_id}" \
VERIFICATION_ARTIFACT_ROOT="${artifact_root}" \
  "${script_dir}/verify-sandbox-public-api.sh"
