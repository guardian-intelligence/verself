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
# The render itself is delegated to `aspect render` so the bash callers,
# `aspect deploy` and `aspect substrate <task>` materialise the cache
# the same way. Set VERSELF_SKIP_RENDER=1 to reuse a stale cache (debug
# only — secrets and hand-written group_vars may be missing if the
# previous render predates a SOPS or playbook edit).

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

  if [[ "${VERSELF_SKIP_RENDER:-0}" == "1" && -f "${VERSELF_ANSIBLE_HOSTS_INI}" ]]; then
    export VERSELF_SITE VERSELF_RENDER_CACHE_DIR VERSELF_ANSIBLE_INVENTORY VERSELF_ANSIBLE_HOSTS_INI
    return 0
  fi

  (
    cd "${repo_root}" || return
    aspect render --site="${VERSELF_SITE}" >/dev/null
  )
  if [[ ! -f "${VERSELF_ANSIBLE_HOSTS_INI}" ]]; then
    echo "site-cache.sh: aspect render did not produce ${VERSELF_ANSIBLE_HOSTS_INI}" >&2
    return 1
  fi
  export VERSELF_SITE VERSELF_RENDER_CACHE_DIR VERSELF_ANSIBLE_INVENTORY VERSELF_ANSIBLE_HOSTS_INI
}
