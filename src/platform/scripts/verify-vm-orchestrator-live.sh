#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Keep the vm-orchestrator proof target stable while delegating to the
# maintained direct execution proof path.
export VERIFICATION_KIND="${VERIFICATION_KIND:-vm-orchestrator-proof}"

exec "${script_dir}/verify-sandbox-fast.sh" execute
