#!/usr/bin/env bash
set -euo pipefail

# Live vm-orchestrator verification uses recurring sandbox executions and
# asserts the host lease/exec spans and vm_lease_evidence projection.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

export VERIFICATION_KIND="${VERIFICATION_KIND:-vm-orchestrator-proof}"
base_run_id="${VERIFICATION_RUN_ID:-${VERIFICATION_KIND}-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_PROOF_ARTIFACT_ROOT}/${VERIFICATION_KIND}}"

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

(
  cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
  ansible-playbook -i inventory/hosts.ini playbooks/verification-reset.yml \
    --tags deploy_profile,clickhouse,tigerbeetle,postgresql,billing_service,sandbox_rental_service,otelcol,grafana
  ansible-playbook -i inventory/hosts.ini playbooks/guest-rootfs.yml
  ansible-playbook -i inventory/hosts.ini playbooks/site.yml \
    --tags deploy_profile,caddy,firecracker,clickhouse,billing_service,sandbox_rental_service,identity_service,mailbox_service,otelcol,forgejo
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

run_telemetry_fault_proof() {
  local label="$1"
  local profile="$2"

  set_telemetry_fault_profile "${profile}"
  VERIFICATION_RUN_ID="${base_run_id}-${label}" \
  VERIFICATION_ARTIFACT_ROOT="${artifact_root}" \
  SANDBOX_PROOF_TELEMETRY_FAULT_PROFILE="${profile}" \
    "${script_dir}/verify-recurring-schedule-live.sh"
}

run_telemetry_fault_proof "telemetry-gap" "gap_once@3"
run_telemetry_fault_proof "telemetry-regression" "regression_once@3"

clear_telemetry_fault_profile
trap - EXIT
