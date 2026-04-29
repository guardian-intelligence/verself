#!/usr/bin/env bash
# Render the per-site deploy cache and export the inventory path for
# operator/canary scripts that invoke ansible-playbook directly.
#
# Sourced by scripts that need the same rendered layout aspect deploy
# consumes. After site_cache_init returns, callers may use:
#
#   VERSELF_SITE                — deployment instance name (default: prod)
#   VERSELF_RENDER_CACHE_DIR    — absolute path to .cache/render/<site>/
#   VERSELF_ANSIBLE_INVENTORY   — absolute path to <cache>/inventory/
#   VERSELF_ANSIBLE_HOSTS_INI   — absolute path to <cache>/inventory/hosts.ini
#
# Set VERSELF_SITE in the environment to deploy a different instance.
# Set VERSELF_SKIP_RENDER=1 to bypass the renderer when iterating on a
# stale cache (debug-only — failures inside Ansible are likely).

site_cache_init() {
  : "${VERSELF_SITE:=prod}"

  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  local repo_root
  repo_root="$(cd "${script_dir}/../../../.." && pwd)"
  if [[ ! -d "${repo_root}/src/cue-renderer" ]]; then
    echo "site-cache.sh: failed to locate repo root from ${script_dir}" >&2
    return 1
  fi

  VERSELF_RENDER_CACHE_DIR="${repo_root}/.cache/render/${VERSELF_SITE}"
  VERSELF_ANSIBLE_INVENTORY="${VERSELF_RENDER_CACHE_DIR}/inventory"
  VERSELF_ANSIBLE_HOSTS_INI="${VERSELF_ANSIBLE_INVENTORY}/hosts.ini"

  local inv_source="${repo_root}/src/platform/ansible/inventory/${VERSELF_SITE}.ini"
  if [[ ! -f "${inv_source}" ]]; then
    echo "site-cache.sh: inventory ${inv_source} not found. Run: aspect platform provision" >&2
    return 1
  fi

  if [[ "${VERSELF_SKIP_RENDER:-0}" == "1" && -f "${VERSELF_ANSIBLE_HOSTS_INI}" ]]; then
    export VERSELF_SITE VERSELF_RENDER_CACHE_DIR VERSELF_ANSIBLE_INVENTORY VERSELF_ANSIBLE_HOSTS_INI
    return 0
  fi

  rm -rf "${VERSELF_RENDER_CACHE_DIR}"
  (
    cd "${repo_root}"
    bazelisk run //src/cue-renderer/cmd/cue-renderer -- \
      generate \
        --instance="${VERSELF_SITE}" \
        --output-dir=".cache/render/${VERSELF_SITE}" >/dev/null
  )
  cp "${inv_source}" "${VERSELF_ANSIBLE_HOSTS_INI}"
  export VERSELF_SITE VERSELF_RENDER_CACHE_DIR VERSELF_ANSIBLE_INVENTORY VERSELF_ANSIBLE_HOSTS_INI
}
