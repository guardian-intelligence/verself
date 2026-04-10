#!/usr/bin/env bash

verification_context_init() {
  local caller_path="$1"

  VERIFICATION_SCRIPT_DIR="$(cd "$(dirname "${caller_path}")" && pwd)"
  VERIFICATION_REPO_ROOT="$(cd "${VERIFICATION_SCRIPT_DIR}/../../.." && pwd)"
  VERIFICATION_PLATFORM_ROOT="${VERIFICATION_REPO_ROOT}/src/platform"
  VERIFICATION_INVENTORY="${VERIFICATION_PLATFORM_ROOT}/ansible/inventory/hosts.ini"
  VERIFICATION_VARS_FILE="${VERIFICATION_PLATFORM_ROOT}/ansible/group_vars/all/main.yml"

  if [[ ! -f "${VERIFICATION_INVENTORY}" ]]; then
    echo "inventory not found: ${VERIFICATION_INVENTORY}" >&2
    return 1
  fi

  VERIFICATION_DOMAIN="$(awk -F'"' '/^forge_metal_domain:/{print $2}' "${VERIFICATION_VARS_FILE}")"
  VERIFICATION_REMOTE_HOST="$(grep -m1 'ansible_host=' "${VERIFICATION_INVENTORY}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
  VERIFICATION_REMOTE_USER="$(grep -m1 'ansible_user=' "${VERIFICATION_INVENTORY}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"

  if [[ -z "${VERIFICATION_DOMAIN}" || -z "${VERIFICATION_REMOTE_HOST}" || -z "${VERIFICATION_REMOTE_USER}" ]]; then
    echo "failed to resolve verification context from inventory/group vars" >&2
    return 1
  fi
}

verification_ssh() {
  ssh -o IPQoS=none -o StrictHostKeyChecking=no \
    "${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}" "$@"
}

verification_remote_sudo_cat() {
  local remote_path="$1"
  verification_ssh "sudo cat '${remote_path}'"
}

verification_wait_for_http() {
  local name="$1"
  local url="$2"
  local expected_status="${3:-200}"

  for _ in $(seq 1 60); do
    local code
    code="$(curl -k -L -s -o /dev/null -w '%{http_code}' "${url}" || true)"
    if [[ "${code}" == "${expected_status}" ]]; then
      return 0
    fi
    sleep 1
  done

  echo "${name} did not return ${expected_status} in time: ${url}" >&2
  return 1
}

verification_wait_for_loopback_api() {
  local name="$1"
  local url="$2"
  local expected_status="$3"

  verification_ssh \
    "for _ in \$(seq 1 60); do \
       code=\$(curl -s -o /dev/null -w '%{http_code}' '${url}' || true); \
       if [[ \"\${code}\" == '${expected_status}' ]]; then exit 0; fi; \
       sleep 1; \
     done; \
     echo '${name} did not return ${expected_status} in time' >&2; \
     exit 1"
}

verification_source_env_file_if_present() {
  local env_file="$1"

  if [[ -f "${env_file}" ]]; then
    # shellcheck disable=SC1090
    source "${env_file}"
  fi
}
