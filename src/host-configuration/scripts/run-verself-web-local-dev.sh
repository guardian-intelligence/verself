#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/host-configuration/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

print_env_only=0
if [[ "${1:-}" == "--print-env" ]]; then
  print_env_only=1
fi

state_file="${VERSELF_WEB_DEV_STATE_FILE:-/tmp/verself-web-dev.env}"
control_dir="$(mktemp -d)"
control_socket="${control_dir}/ssh-control"
state_file_tmp="$(mktemp "${state_file}.XXXXXX")"
job_spec_path="${VERIFICATION_CACHE_DIR}/jobs/verself-web.nomad.json"

cleanup() {
  set +e
  if [[ -S "${control_socket}" ]]; then
    ssh -S "${control_socket}" -O exit \
      -o IPQoS=none -o StrictHostKeyChecking=no \
      "${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}" >/dev/null 2>&1 || true
  fi
  rm -f "${state_file}"
  rm -f "${state_file_tmp}"
  rm -rf "${control_dir}"
}
trap cleanup EXIT INT TERM

ensure_local_port_free() {
  local port="$1"
  if ss -ltn "( sport = :${port} )" | grep -q ":${port}"; then
    echo "local port ${port} is already in use" >&2
    return 1
  fi
}

choose_local_port() {
  local env_value="$1"
  shift

  if [[ -n "${env_value}" ]]; then
    ensure_local_port_free "${env_value}"
    printf '%s\n' "${env_value}"
    return 0
  fi

  local port
  for port in "$@"; do
    if ! ss -ltn "( sport = :${port} )" | grep -q ":${port}"; then
      printf '%s\n' "${port}"
      return 0
    fi
  done

  echo "no free local port available from candidate set: $*" >&2
  return 1
}

wait_for_local_tcp_port() {
  local name="$1"
  local port="$2"
  for _ in $(seq 1 20); do
    if bash -lc "exec 3<>/dev/tcp/127.0.0.1/${port}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  echo "${name} tunnel did not open in time on 127.0.0.1:${port}" >&2
  return 1
}

read_remote_secret() {
  local env_name="$1"
  local remote_path="$2"
  local existing="${!env_name:-}"
  if [[ -n "${existing}" ]]; then
    printf '%s\n' "${existing}"
    return 0
  fi
  ssh -o IPQoS=none -o StrictHostKeyChecking=no \
    "${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}" \
    "sudo cat '${remote_path}'"
}

job_env_value() {
  local key="$1"
  jq -er --arg key "${key}" '
.Job.TaskGroups[]
| .Tasks[]
| select(.Name == "verself-web")
| .Env[$key]
' "${job_spec_path}"
}

require_job_env() {
  local key="$1"
  local value="${!key:-}"
  if [[ -z "${value}" ]]; then
    value="$(job_env_value "${key}")"
  fi
  if [[ -z "${value}" ]]; then
    echo "failed to resolve ${key} from ${job_spec_path}" >&2
    exit 1
  fi
  printf '%s\n' "${value}"
}

if [[ ! -f "${job_spec_path}" ]]; then
  echo "missing resolved Nomad job spec: ${job_spec_path}" >&2
  exit 1
fi

identity_service_auth_audience="$(require_job_env IDENTITY_SERVICE_AUTH_AUDIENCE)"
sandbox_rental_service_auth_audience="$(require_job_env SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE)"
profile_service_auth_audience="$(require_job_env PROFILE_SERVICE_AUTH_AUDIENCE)"
notifications_service_auth_audience="$(require_job_env NOTIFICATIONS_SERVICE_AUTH_AUDIENCE)"
projects_service_auth_audience="$(require_job_env PROJECTS_SERVICE_AUTH_AUDIENCE)"
source_code_hosting_service_auth_audience="$(require_job_env SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE)"

local_sandbox_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_SANDBOX_PORT:-}" 14243 24243 34243 44243 54243)"
local_identity_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_IDENTITY_PORT:-}" 14248 24248 34248 44248 54248)"
local_profile_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_PROFILE_PORT:-}" 14258 24258 34258 44258 54258)"
local_governance_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_GOVERNANCE_PORT:-}" 14250 24250 34250 44250 54250)"
local_notifications_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_NOTIFICATIONS_PORT:-}" 14260 24260 34260 44260 54260)"
local_projects_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_PROJECTS_PORT:-}" 14264 24264 34264 44264 54264)"
local_source_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_SOURCE_PORT:-}" 14261 24261 34261 44261 54261)"
local_electric_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_ELECTRIC_PORT:-}" 13010 23010 33010 43010 53010)"
local_electric_notifications_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_ELECTRIC_NOTIFICATIONS_PORT:-}" 13012 23012 33012 43012 53012)"
local_otel_http_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_OTEL_HTTP_PORT:-}" 14318 24318 34318 44318 54318)"
local_app_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_APP_PORT:-}" 4244 5244 6244 7244 8244)"

electric_api_secret="$(read_remote_secret ELECTRIC_API_SECRET /etc/credstore/electric/api-secret)"
electric_notifications_api_secret="$(read_remote_secret ELECTRIC_NOTIFICATIONS_API_SECRET /etc/credstore/electric-notifications/api-secret)"

if [[ "${print_env_only}" != "1" ]]; then
  ssh -fN -M -S "${control_socket}" \
    -o IPQoS=none \
    -o StrictHostKeyChecking=no \
    -o ExitOnForwardFailure=yes \
    -L "${local_sandbox_port}:127.0.0.1:4243" \
    -L "${local_identity_port}:127.0.0.1:4248" \
    -L "${local_profile_port}:127.0.0.1:4258" \
    -L "${local_governance_port}:127.0.0.1:4250" \
    -L "${local_notifications_port}:127.0.0.1:4260" \
    -L "${local_projects_port}:127.0.0.1:4264" \
    -L "${local_source_port}:127.0.0.1:4261" \
    -L "${local_electric_port}:127.0.0.1:3010" \
    -L "${local_electric_notifications_port}:127.0.0.1:3012" \
    -L "${local_otel_http_port}:127.0.0.1:4318" \
    "${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}"

  wait_for_local_tcp_port "sandbox-rental-service" "${local_sandbox_port}"
  wait_for_local_tcp_port "identity-service" "${local_identity_port}"
  wait_for_local_tcp_port "profile-service" "${local_profile_port}"
  wait_for_local_tcp_port "governance-service" "${local_governance_port}"
  wait_for_local_tcp_port "notifications-service" "${local_notifications_port}"
  wait_for_local_tcp_port "projects-service" "${local_projects_port}"
  wait_for_local_tcp_port "source-code-hosting-service" "${local_source_port}"
  wait_for_local_tcp_port "Electric" "${local_electric_port}"
  wait_for_local_tcp_port "Electric notifications" "${local_electric_notifications_port}"
  wait_for_local_tcp_port "OTLP HTTP" "${local_otel_http_port}"
fi

export VERSELF_DOMAIN="${VERSELF_DOMAIN:-${VERIFICATION_DOMAIN}}"
export PRODUCT_BASE_URL="${PRODUCT_BASE_URL:-https://${VERSELF_DOMAIN}}"
export SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE="${SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE:-${sandbox_rental_service_auth_audience}}"
export IDENTITY_SERVICE_AUTH_AUDIENCE="${IDENTITY_SERVICE_AUTH_AUDIENCE:-${identity_service_auth_audience}}"
export PROFILE_SERVICE_AUTH_AUDIENCE="${PROFILE_SERVICE_AUTH_AUDIENCE:-${profile_service_auth_audience}}"
export NOTIFICATIONS_SERVICE_AUTH_AUDIENCE="${NOTIFICATIONS_SERVICE_AUTH_AUDIENCE:-${notifications_service_auth_audience}}"
export PROJECTS_SERVICE_AUTH_AUDIENCE="${PROJECTS_SERVICE_AUTH_AUDIENCE:-${projects_service_auth_audience}}"
export SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE="${SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE:-${source_code_hosting_service_auth_audience}}"
export SANDBOX_RENTAL_SERVICE_BASE_URL="${SANDBOX_RENTAL_SERVICE_BASE_URL:-http://127.0.0.1:${local_sandbox_port}}"
export IDENTITY_SERVICE_BASE_URL="${IDENTITY_SERVICE_BASE_URL:-http://127.0.0.1:${local_identity_port}}"
export PROFILE_SERVICE_BASE_URL="${PROFILE_SERVICE_BASE_URL:-http://127.0.0.1:${local_profile_port}}"
export GOVERNANCE_SERVICE_BASE_URL="${GOVERNANCE_SERVICE_BASE_URL:-http://127.0.0.1:${local_governance_port}}"
export NOTIFICATIONS_SERVICE_BASE_URL="${NOTIFICATIONS_SERVICE_BASE_URL:-http://127.0.0.1:${local_notifications_port}}"
export PROJECTS_SERVICE_BASE_URL="${PROJECTS_SERVICE_BASE_URL:-http://127.0.0.1:${local_projects_port}}"
export SOURCE_CODE_HOSTING_SERVICE_BASE_URL="${SOURCE_CODE_HOSTING_SERVICE_BASE_URL:-http://127.0.0.1:${local_source_port}}"
export ELECTRIC_BASE_URL="${ELECTRIC_BASE_URL:-http://127.0.0.1:${local_electric_port}}"
export ELECTRIC_NOTIFICATIONS_BASE_URL="${ELECTRIC_NOTIFICATIONS_BASE_URL:-http://127.0.0.1:${local_electric_notifications_port}}"
export ELECTRIC_API_SECRET="${ELECTRIC_API_SECRET:-${electric_api_secret}}"
export ELECTRIC_NOTIFICATIONS_API_SECRET="${ELECTRIC_NOTIFICATIONS_API_SECRET:-${electric_notifications_api_secret}}"
export OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:${local_otel_http_port}}"
export OTEL_SERVICE_NAME="${OTEL_SERVICE_NAME:-verself-web}"
export VERSELF_WEB_DEV_LOCAL_APP_PORT="${local_app_port}"
export BASE_URL="${BASE_URL:-http://127.0.0.1:${local_app_port}}"

cat >"${state_file_tmp}" <<EOF
export VERSELF_DOMAIN=${VERSELF_DOMAIN}
export PRODUCT_BASE_URL=${PRODUCT_BASE_URL}
export SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE=${SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE}
export IDENTITY_SERVICE_AUTH_AUDIENCE=${IDENTITY_SERVICE_AUTH_AUDIENCE}
export PROFILE_SERVICE_AUTH_AUDIENCE=${PROFILE_SERVICE_AUTH_AUDIENCE}
export NOTIFICATIONS_SERVICE_AUTH_AUDIENCE=${NOTIFICATIONS_SERVICE_AUTH_AUDIENCE}
export PROJECTS_SERVICE_AUTH_AUDIENCE=${PROJECTS_SERVICE_AUTH_AUDIENCE}
export SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE=${SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE}
export SANDBOX_RENTAL_SERVICE_BASE_URL=${SANDBOX_RENTAL_SERVICE_BASE_URL}
export IDENTITY_SERVICE_BASE_URL=${IDENTITY_SERVICE_BASE_URL}
export PROFILE_SERVICE_BASE_URL=${PROFILE_SERVICE_BASE_URL}
export GOVERNANCE_SERVICE_BASE_URL=${GOVERNANCE_SERVICE_BASE_URL}
export NOTIFICATIONS_SERVICE_BASE_URL=${NOTIFICATIONS_SERVICE_BASE_URL}
export PROJECTS_SERVICE_BASE_URL=${PROJECTS_SERVICE_BASE_URL}
export SOURCE_CODE_HOSTING_SERVICE_BASE_URL=${SOURCE_CODE_HOSTING_SERVICE_BASE_URL}
export ELECTRIC_BASE_URL=${ELECTRIC_BASE_URL}
export ELECTRIC_NOTIFICATIONS_BASE_URL=${ELECTRIC_NOTIFICATIONS_BASE_URL}
export ELECTRIC_API_SECRET=${ELECTRIC_API_SECRET}
export ELECTRIC_NOTIFICATIONS_API_SECRET=${ELECTRIC_NOTIFICATIONS_API_SECRET}
export OTEL_EXPORTER_OTLP_ENDPOINT=${OTEL_EXPORTER_OTLP_ENDPOINT}
export OTEL_SERVICE_NAME=${OTEL_SERVICE_NAME}
export CONSOLE_DEV_LOCAL_APP_PORT=${local_app_port}
export BASE_URL=${BASE_URL}
export TEST_BASE_URL=${BASE_URL}
EOF
chmod 600 "${state_file_tmp}"

if [[ "${print_env_only}" != "1" ]]; then
  mv -f "${state_file_tmp}" "${state_file}"
fi

cat >&2 <<EOF
verself-web local dev
  app:       ${BASE_URL}
  identity:  ${IDENTITY_SERVICE_BASE_URL}
  sandbox:   ${SANDBOX_RENTAL_SERVICE_BASE_URL}
  profile:   ${PROFILE_SERVICE_BASE_URL}
  governance: ${GOVERNANCE_SERVICE_BASE_URL}
  notifications: ${NOTIFICATIONS_SERVICE_BASE_URL}
  projects:  ${PROJECTS_SERVICE_BASE_URL}
  source:    ${SOURCE_CODE_HOSTING_SERVICE_BASE_URL}
  electric:  ${ELECTRIC_BASE_URL}
  electric notifications: ${ELECTRIC_NOTIFICATIONS_BASE_URL}
  otlp:      ${OTEL_EXPORTER_OTLP_ENDPOINT}
  state:     ${state_file}
EOF

if [[ "${print_env_only}" == "1" ]]; then
  cat "${state_file_tmp}"
  printf '%s\n' "vp run @verself/verself-web#dev"
  exit 0
fi

cd "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo"
vp run @verself/verself-web#dev
