#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-sandbox-live-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_SMOKE_ARTIFACT_ROOT}/sandbox-live}"
artifact_dir="${artifact_root}/${run_id}"

mkdir -p "${artifact_dir}"

# Deploy fan-out covers every role the sandbox lifecycle exercises end-to-end.
deploy_tags="deploy_profile,clickhouse,tigerbeetle,postgresql,billing_service,sandbox_rental_service,otelcol,grafana,caddy,firecracker,identity_service,mailbox_service,forgejo"

site_extra_vars=()
if [[ "${VERIFICATION_RESET:-0}" == "1" ]]; then
  # The wipe drops Temporal/ClickHouse schema-derived state. Force the
  # follow-up site.yml to rebuild schemas instead of re-running migrations
  # against an empty store.
  site_extra_vars=(-e "temporal_force_schema_reset=true" -e "clickhouse_force_schema_reset=true")
fi

# Deploys and seed-system reseeds are opt-in. By default we exercise
# whatever is already on the host. Pass VERIFICATION_DEPLOY=1 to
# rebuild + redeploy before the checks; VERIFICATION_RESET=1 also runs
# verification-reset + schema resets. VERIFICATION_RESEED=1 reruns
# seed-system without touching the deploy.
if [[ "${VERIFICATION_DEPLOY:-0}" == "1" || "${VERIFICATION_RESET:-0}" == "1" ]]; then
  (
    cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
    if [[ "${VERIFICATION_RESET:-0}" == "1" ]]; then
      ansible-playbook -i "${VERIFICATION_INVENTORY_DIR}" playbooks/verification-reset.yml
    fi
    ansible-playbook -i "${VERIFICATION_INVENTORY_DIR}" playbooks/guest-rootfs.yml
    ansible-playbook -i "${VERIFICATION_INVENTORY_DIR}" playbooks/site.yml \
      --tags "${deploy_tags}" "${site_extra_vars[@]}"
    # site.yml restarts the service stack; wait for the loopback API before
    # seed-system starts probing authz behavior against sandbox-rental.
    verification_wait_for_loopback_api "billing-service" "http://127.0.0.1:4242/readyz" "200"
    verification_wait_for_loopback_api "sandbox-rental-service" \
      "http://127.0.0.1:4243/api/v1/billing/entitlements" "401"
    ansible-playbook -i "${VERIFICATION_INVENTORY_DIR}" playbooks/seed-system.yml
  )
elif [[ "${VERIFICATION_RESEED:-0}" == "1" ]]; then
  (
    cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
    ansible-playbook -i "${VERIFICATION_INVENTORY_DIR}" playbooks/seed-system.yml
  )
fi

VERIFICATION_RUN_ID="${run_id}" \
VERIFICATION_ARTIFACT_ROOT="${artifact_root}" \
  "${script_dir}/verify-recurring-schedule-live.sh"
