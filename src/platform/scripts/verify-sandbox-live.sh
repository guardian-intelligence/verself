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

verification_deploy_playbook verification-reset \
  --tags deploy_profile,clickhouse,tigerbeetle,postgresql,billing_service,sandbox_rental_service,otelcol,grafana
verification_deploy_playbook guest-rootfs
verification_deploy_playbook site \
  --tags deploy_profile,caddy,firecracker,clickhouse,billing_service,sandbox_rental_service,identity_service,mailbox_service,otelcol,forgejo
verification_wait_for_loopback_api "billing-service" "http://127.0.0.1:4242/readyz" "200"
# verification-reset restarts the service stack; wait for the loopback API
# before seed-system starts probing authz behavior against sandbox-rental.
verification_wait_for_loopback_api "sandbox-rental-service" \
  "http://127.0.0.1:4243/api/v1/billing/entitlements" "401"
verification_deploy_playbook seed-system

VERIFICATION_RUN_ID="${run_id}" \
VERIFICATION_ARTIFACT_ROOT="${artifact_root}" \
  "${script_dir}/verify-recurring-schedule-live.sh"
