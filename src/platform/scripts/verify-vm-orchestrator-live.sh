#!/usr/bin/env bash
set -euo pipefail

# Live vm-orchestrator verification uses recurring sandbox executions and
# asserts the host lease/exec spans and vm_lease_evidence projection.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

export VERIFICATION_KIND="${VERIFICATION_KIND:-vm-orchestrator-smoke-test}"
base_run_id="${VERIFICATION_RUN_ID:-${VERIFICATION_KIND}-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_SMOKE_ARTIFACT_ROOT}/${VERIFICATION_KIND}}"

set_telemetry_fault_profile() {
  local profile="$1"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
    ansible-playbook -i inventory/hosts.ini playbooks/vm-orchestrator-telemetry-fault.yml \
      -e "vm_orchestrator_telemetry_fault_profile=${profile}"
  )
}

clear_telemetry_fault_profile() {
  set_telemetry_fault_profile ""
}

cleanup() {
  clear_telemetry_fault_profile >/dev/null 2>&1 || true
}
trap cleanup EXIT

deploy_tags="deploy_profile,clickhouse,tigerbeetle,postgresql,billing_service,sandbox_rental_service,otelcol,grafana,caddy,firecracker,identity_service,mailbox_service,forgejo"

site_extra_vars=()
if [[ "${VERIFICATION_RESET:-0}" == "1" ]]; then
  site_extra_vars=(-e "temporal_force_schema_reset=true" -e "clickhouse_force_schema_reset=true")
fi

(
  cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
  if [[ "${VERIFICATION_RESET:-0}" == "1" ]]; then
    ansible-playbook -i inventory/hosts.ini playbooks/verification-reset.yml
  fi
  ansible-playbook -i inventory/hosts.ini playbooks/guest-rootfs.yml
  ansible-playbook -i inventory/hosts.ini playbooks/site.yml \
    --tags "${deploy_tags}" "${site_extra_vars[@]}"
)

clear_telemetry_fault_profile

(
  cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
  verification_wait_for_loopback_api "billing-service" "http://127.0.0.1:4242/readyz" "200"
  verification_wait_for_loopback_api "sandbox-rental-service" \
    "http://127.0.0.1:4243/api/v1/billing/entitlements" "401"
  ansible-playbook -i inventory/hosts.ini playbooks/seed-system.yml
)

VERIFICATION_RUN_ID="${base_run_id}-normal" \
VERIFICATION_ARTIFACT_ROOT="${artifact_root}" \
  "${script_dir}/verify-recurring-schedule-live.sh"

run_telemetry_fault_smoke_test() {
  local label="$1"
  local profile="$2"

  set_telemetry_fault_profile "${profile}"
  VERIFICATION_RUN_ID="${base_run_id}-${label}" \
  VERIFICATION_ARTIFACT_ROOT="${artifact_root}" \
  SANDBOX_SMOKE_TEST_TELEMETRY_FAULT_PROFILE="${profile}" \
    "${script_dir}/verify-recurring-schedule-live.sh"
}

run_telemetry_fault_smoke_test "telemetry-gap" "gap_once@3"
run_telemetry_fault_smoke_test "telemetry-regression" "regression_once@3"

clear_telemetry_fault_profile
trap - EXIT
