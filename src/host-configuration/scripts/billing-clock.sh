#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/host-configuration/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

usage() {
  cat >&2 <<'USAGE'
Usage:
  billing-clock.sh --org-id 123 [--product-id sandbox]
  billing-clock.sh --org-id 123 --set 2026-05-01T00:00:00Z [--reason e2e]
  billing-clock.sh --org-id 123 --advance-seconds 2678400 [--reason e2e]
  billing-clock.sh --org-id 123 --clear [--reason e2e]
  billing-clock.sh --org platform --wall-clock [--reason e2e-cleanup]

Runs the billing business-clock helper on the target node. The helper mutates
billing PostgreSQL through billing-service code paths and emits billing_events;
it does not expose a browser-callable test control surface.
USAGE
}

org_id=""
org=""
product_id="${BILLING_PRODUCT_ID:-sandbox}"
set_at=""
advance_seconds=""
clear=""
wall_clock=""
reason="billing-clock"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --org-id)
      org_id="${2:-}"
      shift 2
      ;;
    --org)
      org="${2:-}"
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
    --wall-clock)
      wall_clock="1"
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

if [[ -z "${org_id}" && -z "${org}" ]]; then
  echo "ERROR: --org-id or --org is required" >&2
  usage
  exit 1
fi
selected=0
[[ -n "${set_at}" ]] && selected=$((selected + 1))
[[ -n "${advance_seconds}" ]] && selected=$((selected + 1))
[[ -n "${clear}" ]] && selected=$((selected + 1))
[[ -n "${wall_clock}" ]] && selected=$((selected + 1))
if [[ "${selected}" -gt 1 ]]; then
  echo "ERROR: choose only one of --set, --advance-seconds, --clear, or --wall-clock" >&2
  usage
  exit 1
fi

binary_dir="${VERIFICATION_DEPLOY_ARTIFACT_ROOT}/bin"
binary_path="${binary_dir}/billing-clock"
remote_path=""
mkdir -p "${binary_dir}"

(
  cd "${VERIFICATION_REPO_ROOT}/src/billing-service"
  go build -o "${binary_path}" ./cmd/billing-clock
)

remote_path="$(verification_upload_executable "${binary_path}" verself-billing-clock)"
cleanup_remote() {
  verification_remove_remote_path "${remote_path}"
}
trap cleanup_remote EXIT

remote_args=(
  sudo -u billing "${remote_path}"
  --pg-dsn "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable"
  --product-id "${product_id}"
  --reason "${reason}"
)

if [[ -n "${org_id}" ]]; then
  remote_args+=(--org-id "${org_id}")
else
  remote_args+=(--org "${org}")
fi
if [[ -n "${set_at}" ]]; then
  remote_args+=(--set "${set_at}")
elif [[ -n "${advance_seconds}" ]]; then
  remote_args+=(--advance-seconds "${advance_seconds}")
elif [[ -n "${clear}" ]]; then
  remote_args+=(--clear)
elif [[ -n "${wall_clock}" ]]; then
  remote_args+=(--wall-clock)
fi

printf -v remote_cmd '%q ' "${remote_args[@]}"
verification_ssh "${remote_cmd}"
