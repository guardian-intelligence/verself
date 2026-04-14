#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

usage() {
  cat >&2 <<'USAGE'
Usage:
  billing-clock.sh --org-id 123 [--product-id sandbox]
  billing-clock.sh --org-id 123 --set 2026-05-01T00:00:00Z [--reason e2e]
  billing-clock.sh --org-id 123 --advance-seconds 2678400 [--reason e2e]
  billing-clock.sh --org-id 123 --clear [--reason e2e]

Runs the billing business-clock helper on the target node. The helper mutates
billing PostgreSQL through billing-service code paths and emits billing_events;
it does not expose a browser-callable test control surface.
USAGE
}

org_id=""
product_id="${BILLING_PRODUCT_ID:-sandbox}"
set_at=""
advance_seconds=""
clear=""
reason="billing-clock"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --org-id)
      org_id="${2:-}"
      shift 2
      ;;
    --product-id)
      product_id="${2:-}"
      shift 2
      ;;
    --set)
      set_at="${2:-}"
      shift 2
      ;;
    --advance-seconds)
      advance_seconds="${2:-}"
      shift 2
      ;;
    --clear)
      clear="1"
      shift
      ;;
    --reason)
      reason="${2:-}"
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "${org_id}" ]]; then
  echo "ERROR: --org-id is required" >&2
  usage
  exit 1
fi
selected=0
[[ -n "${set_at}" ]] && selected=$((selected + 1))
[[ -n "${advance_seconds}" ]] && selected=$((selected + 1))
[[ -n "${clear}" ]] && selected=$((selected + 1))
if [[ "${selected}" -gt 1 ]]; then
  echo "ERROR: choose only one of --set, --advance-seconds, or --clear" >&2
  usage
  exit 1
fi

binary_dir="${VERIFICATION_REPO_ROOT}/artifacts/bin"
binary_path="${binary_dir}/billing-clock"
remote_path="/tmp/forge-metal-billing-clock"
mkdir -p "${binary_dir}"

(
  cd "${VERIFICATION_REPO_ROOT}/src/billing-service"
  go build -o "${binary_path}" ./cmd/billing-clock
)

verification_ssh "cat > '${remote_path}' && chmod 0755 '${remote_path}'" <"${binary_path}"

remote_args=(
  sudo "${remote_path}"
  --pg-dsn-file /etc/credstore/billing/pg-dsn
  --org-id "${org_id}"
  --product-id "${product_id}"
  --reason "${reason}"
)

if [[ -n "${set_at}" ]]; then
  remote_args+=(--set "${set_at}")
elif [[ -n "${advance_seconds}" ]]; then
  remote_args+=(--advance-seconds "${advance_seconds}")
elif [[ -n "${clear}" ]]; then
  remote_args+=(--clear)
fi

printf -v remote_cmd '%q ' "${remote_args[@]}"
verification_ssh "${remote_cmd}"
