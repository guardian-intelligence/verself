#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

mode="${1:-admin}"

case "${mode}" in
  admin)
    export VERIFICATION_KIND="${VERIFICATION_KIND:-sandbox-fast}"
    export TEST_BASE_URL="${TEST_BASE_URL:-https://console.${VERIFICATION_DOMAIN}}"
    "${script_dir}/verify-console-ui-smoke.sh"
    ;;
  schedule)
    export VERIFICATION_KIND="${VERIFICATION_KIND:-sandbox-schedule}"
    "${script_dir}/verify-recurring-schedule-live.sh"
    ;;
  billing)
    export VERIFICATION_KIND="${VERIFICATION_KIND:-console-billing}"
    export TEST_BASE_URL="${TEST_BASE_URL:-https://console.${VERIFICATION_DOMAIN}}"
    "${script_dir}/verify-console-billing-flow.sh"
    ;;
  *)
    echo "usage: $0 [admin|schedule|billing]" >&2
    exit 1
    ;;
esac
