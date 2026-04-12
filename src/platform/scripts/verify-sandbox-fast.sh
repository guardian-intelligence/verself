#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

mode="${1:-admin}"

case "${mode}" in
  admin)
    export VERIFICATION_KIND="${VERIFICATION_KIND:-sandbox-fast}"
    export TEST_BASE_URL="${TEST_BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}"
    "${script_dir}/verify-rent-ui-smoke.sh"
    ;;
  import | refresh | execute)
    export SANDBOX_FAST_FLOW="${mode}"
    export VERIFICATION_KIND="${VERIFICATION_KIND:-sandbox-${mode}}"
    export TEST_BASE_URL="${TEST_BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}"
    "${script_dir}/verify-sandbox-repo-flow.sh"
    ;;
  billing)
    export VERIFICATION_KIND="${VERIFICATION_KIND:-sandbox-billing}"
    export TEST_BASE_URL="${TEST_BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}"
    "${script_dir}/verify-rent-billing-flow.sh"
    ;;
  *)
    echo "usage: $0 [admin|import|refresh|execute|billing]" >&2
    exit 1
    ;;
esac
