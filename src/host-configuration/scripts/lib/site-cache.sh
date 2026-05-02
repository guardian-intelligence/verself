#!/usr/bin/env bash
# Resolve the per-site authored inventory paths for operator/canary scripts
# that invoke ansible-playbook directly.
#
# Sourced by scripts that need the same authored layout verself-deploy
# consumes. After site_cache_init returns, callers may use:
#
#   VERSELF_SITE                — deployment instance name (default: prod)
#   VERSELF_RENDER_CACHE_DIR    — absolute path to src/host-configuration/ansible
#   VERSELF_ANSIBLE_INVENTORY   — absolute path to src/host-configuration/ansible/inventory
#   VERSELF_ANSIBLE_HOSTS_INI   — absolute path to the per-site inventory file

site_cache_init() {
  : "${VERSELF_SITE:=prod}"

  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  local repo_root
  repo_root="$(cd "${script_dir}/../../../.." && pwd)"
  if [[ ! -d "${repo_root}/src/host-configuration/ansible" ]]; then
    echo "site-cache.sh: failed to locate repo root from ${script_dir}" >&2
    return 1
  fi

  VERSELF_RENDER_CACHE_DIR="${repo_root}/src/host-configuration/ansible"
  VERSELF_ANSIBLE_INVENTORY="${VERSELF_RENDER_CACHE_DIR}/inventory"
  VERSELF_ANSIBLE_HOSTS_INI="${VERSELF_ANSIBLE_INVENTORY}/${VERSELF_SITE}.ini"

  if [[ ! -f "${VERSELF_ANSIBLE_HOSTS_INI}" ]]; then
    echo "site-cache.sh: missing inventory ${VERSELF_ANSIBLE_HOSTS_INI}" >&2
    return 1
  fi
  export VERSELF_SITE VERSELF_RENDER_CACHE_DIR VERSELF_ANSIBLE_INVENTORY VERSELF_ANSIBLE_HOSTS_INI
}
