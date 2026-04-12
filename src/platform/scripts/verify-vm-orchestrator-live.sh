#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

export VERIFICATION_KIND="${VERIFICATION_KIND:-vm-orchestrator-proof}"

cd "${VERIFICATION_PLATFORM_ROOT}/ansible"

if [[ "${VM_ORCHESTRATOR_PROOF_SKIP_DEPLOY:-0}" != "1" ]]; then
  ansible-playbook playbooks/dev-single-node.yml --tags firecracker
fi

ansible-playbook playbooks/vm-guest-telemetry-dev.yml
