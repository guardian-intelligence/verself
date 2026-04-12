#!/usr/bin/env bash
set -euo pipefail

# Live vm-orchestrator verification scenarios:
#   baseline (default): normal run, telemetry hello evidence
#   telemetry_gap: host telemetry injector emits one gap diagnostic
#   telemetry_regression: host telemetry injector emits one regression diagnostic
#   bridge_result_seq_zero: vm-bridge emits invalid result seq=0 and host records protocol violation
#
# Usage:
#   make vm-orchestrator-proof
#   VM_ORCHESTRATOR_PROOF_SKIP_DEPLOY=1 make vm-orchestrator-proof-gap
#   VM_ORCHESTRATOR_PROOF_SKIP_DEPLOY=1 make vm-orchestrator-proof-regression
#   VM_ORCHESTRATOR_PROOF_SKIP_DEPLOY=1 make vm-orchestrator-proof-bridge-fault

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

export VERIFICATION_KIND="${VERIFICATION_KIND:-vm-orchestrator-proof}"
scenario="${VM_ORCHESTRATOR_PROOF_SCENARIO:-baseline}"

cd "${VERIFICATION_PLATFORM_ROOT}/ansible"

if [[ "${VM_ORCHESTRATOR_PROOF_SKIP_DEPLOY:-0}" != "1" ]]; then
  ansible-playbook playbooks/dev-single-node.yml --tags firecracker
fi

playbook_args=(playbooks/vm-guest-telemetry-dev.yml)
case "${scenario}" in
  baseline)
    ;;
  telemetry_gap)
    playbook_args+=(
      -e "vm_guest_telemetry_fault_profile=gap_once@12"
      -e "vm_guest_expected_telemetry_diagnostic_kind=gap"
    )
    ;;
  telemetry_regression)
    playbook_args+=(
      -e "vm_guest_telemetry_fault_profile=regression_once@12"
      -e "vm_guest_expected_telemetry_diagnostic_kind=regression"
    )
    ;;
  bridge_result_seq_zero)
    playbook_args+=(
      -e "vm_guest_bridge_fault_mode=result_seq_zero"
      -e "vm_guest_expect_protocol_violation_state=await_guest_result"
      -e "vm_guest_expect_run_failure=true"
    )
    ;;
  *)
    echo "unsupported VM_ORCHESTRATOR_PROOF_SCENARIO: ${scenario}" >&2
    exit 1
    ;;
esac

if [[ -n "${VM_GUEST_TELEMETRY_FAULT_PROFILE:-}" ]]; then
  playbook_args+=(-e "vm_guest_telemetry_fault_profile=${VM_GUEST_TELEMETRY_FAULT_PROFILE}")
fi
if [[ -n "${VM_GUEST_EXPECTED_TELEMETRY_DIAGNOSTIC_KIND:-}" ]]; then
  playbook_args+=(-e "vm_guest_expected_telemetry_diagnostic_kind=${VM_GUEST_EXPECTED_TELEMETRY_DIAGNOSTIC_KIND}")
fi
if [[ -n "${VM_GUEST_BRIDGE_FAULT_MODE:-}" ]]; then
  playbook_args+=(-e "vm_guest_bridge_fault_mode=${VM_GUEST_BRIDGE_FAULT_MODE}")
fi
if [[ -n "${VM_GUEST_EXPECT_PROTOCOL_VIOLATION_STATE:-}" ]]; then
  playbook_args+=(-e "vm_guest_expect_protocol_violation_state=${VM_GUEST_EXPECT_PROTOCOL_VIOLATION_STATE}")
fi
if [[ -n "${VM_GUEST_EXPECT_RUN_FAILURE:-}" ]]; then
  playbook_args+=(-e "vm_guest_expect_run_failure=${VM_GUEST_EXPECT_RUN_FAILURE}")
fi

ansible-playbook "${playbook_args[@]}"
