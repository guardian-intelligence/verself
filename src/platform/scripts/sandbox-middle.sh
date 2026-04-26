#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

deploy_target="${SANDBOX_DEPLOY_TARGET:-ui}"
verify_target="${SANDBOX_VERIFY_TARGET:-admin}"
seed_verify="${SANDBOX_SEED_VERIFY:-0}"

deploy() {
  case "${deploy_target}" in
    none)
      ;;
    ui)
      verification_deploy_playbook site --tags console
      ;;
    service)
      verification_deploy_playbook site --tags deploy_profile,sandbox_rental_service
      ;;
    both)
      verification_deploy_playbook site --tags deploy_profile,sandbox_rental_service,console
      ;;
    *)
      echo "unsupported SANDBOX_DEPLOY_TARGET: ${deploy_target}" >&2
      exit 1
      ;;
  esac
}

seed() {
  if [[ "${seed_verify}" == "1" ]]; then
    verification_deploy_playbook seed-system --tags verify
  fi
}

verify() {
  case "${verify_target}" in
    none)
      ;;
    admin | schedule | billing)
      "${script_dir}/verify-sandbox-fast.sh" "${verify_target}"
      ;;
    *)
      echo "unsupported SANDBOX_VERIFY_TARGET: ${verify_target}" >&2
      exit 1
      ;;
  esac
}

deploy
seed
verify
