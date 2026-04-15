#!/usr/bin/env bash
set -euo pipefail

# Live vm-orchestrator verification uses the public sandbox execution API and
# asserts the host lease/exec spans and vm_lease_evidence projection.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

export VERIFICATION_KIND="${VERIFICATION_KIND:-vm-orchestrator-proof}"
SANDBOX_PROOF_SUBMISSIONS="${SANDBOX_PROOF_SUBMISSIONS:-1}" \
  "${script_dir}/verify-sandbox-live.sh"
