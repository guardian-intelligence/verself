#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

state_file="${CONSOLE_DEV_STATE_FILE:-/tmp/verself-console-dev.env}"
verification_source_env_file_if_present "${state_file}"

export VERIFICATION_KIND="${VERIFICATION_KIND:-sandbox-local-ui}"
export TEST_BASE_URL="${TEST_BASE_URL:-${BASE_URL:-http://127.0.0.1:4244}}"

"${script_dir}/verify-console-ui-smoke.sh"
