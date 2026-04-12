#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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
      (
        cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
        ansible-playbook -i inventory/hosts.ini playbooks/dev-single-node.yml --tags rent_a_sandbox
      )
      ;;
    service)
      (
        cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
        ansible-playbook -i inventory/hosts.ini playbooks/dev-single-node.yml --tags deploy_profile,sandbox_rental_service
      )
      ;;
    both)
      (
        cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
        ansible-playbook -i inventory/hosts.ini playbooks/dev-single-node.yml \
          --tags deploy_profile,sandbox_rental_service,rent_a_sandbox
      )
      ;;
    *)
      echo "unsupported SANDBOX_DEPLOY_TARGET: ${deploy_target}" >&2
      exit 1
      ;;
  esac
}

seed() {
  if [[ "${seed_verify}" == "1" ]]; then
    (
      cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
      ansible-playbook -i inventory/hosts.ini playbooks/seed-system.yml --tags verify
    )
  fi
}

verify() {
  case "${verify_target}" in
    none)
      ;;
    admin | import | refresh | execute | billing)
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
