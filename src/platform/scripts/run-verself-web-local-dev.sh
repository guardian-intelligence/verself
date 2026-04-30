#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
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

local_tcp_port_in_use() {
  local port="$1"
  # macOS does not ship Linux `ss`; binding the loopback candidates is the portable preflight.
  python3 - "${port}" <<'PY'
import errno
import socket
import sys

port = int(sys.argv[1])
sockets = []
try:
    for family, host in ((socket.AF_INET, "127.0.0.1"), (socket.AF_INET6, "::1")):
        try:
            sock = socket.socket(family, socket.SOCK_STREAM)
            sock.bind((host, port))
            sockets.append(sock)
        except OSError as exc:
            if exc.errno in (errno.EADDRINUSE, errno.EACCES):
                raise SystemExit(0)
    raise SystemExit(1)
finally:
    for sock in sockets:
        sock.close()
PY
}

local_tcp_port_accepts() {
  local port="$1"
  python3 - "${port}" <<'PY'
import socket
import sys

port = int(sys.argv[1])
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.settimeout(0.25)
    raise SystemExit(0 if sock.connect_ex(("127.0.0.1", port)) == 0 else 1)
PY
}

ensure_local_port_free() {
  local port="$1"
  if local_tcp_port_in_use "${port}"; then
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
    if ! local_tcp_port_in_use "${port}"; then
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
    if local_tcp_port_accepts "${port}"; then
      return 0
    fi
    sleep 0.5
  done
  echo "${name} tunnel did not open in time on 127.0.0.1:${port}" >&2
  return 1
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

if [[ ! -f "${job_spec_path}" ]]; then
  echo "missing rendered Nomad job spec: ${job_spec_path}" >&2
  exit 1
fi

auth_project_id="${AUTH_PROJECT_ID:-$(job_env_value AUTH_PROJECT_ID)}"
if [[ -z "${auth_project_id}" ]]; then
  echo "failed to resolve AUTH_PROJECT_ID from ${job_spec_path}" >&2
  exit 1
fi
identity_service_auth_audience="${IDENTITY_SERVICE_AUTH_AUDIENCE:-$(job_env_value IDENTITY_SERVICE_AUTH_AUDIENCE)}"
if [[ -z "${identity_service_auth_audience}" ]]; then
  echo "failed to resolve IDENTITY_SERVICE_AUTH_AUDIENCE from ${job_spec_path}" >&2
  exit 1
fi
auth_database_password_file="${AUTH_DATABASE_PASSWORD_FILE:-$(job_env_value AUTH_DATABASE_PASSWORD_FILE)}"
if [[ -z "${auth_database_password_file}" ]]; then
  echo "failed to resolve AUTH_DATABASE_PASSWORD_FILE from ${job_spec_path}" >&2
  exit 1
fi

fetch_dev_client_id() {
  local admin_pat="$1"
  local auth_project_id="$2"
  local response

  response="$(
    curl -fsS \
      -H "Authorization: Bearer ${admin_pat}" \
      -H "Content-Type: application/json" \
      -d '{"queries":[{"nameQuery":{"name":"verself-web-dev","method":"TEXT_QUERY_METHOD_EQUALS"}}]}' \
      "https://auth.${VERIFICATION_DOMAIN}/management/v1/projects/${auth_project_id}/apps/_search"
  )"

  python3 - <<'PY' "${response}"
import json
import sys

payload = json.loads(sys.argv[1])
apps = payload.get("result") or []
if not apps:
    raise SystemExit(1)

client_id = ((apps[0].get("oidcConfig") or {}).get("clientId") or "").strip()
if not client_id:
    raise SystemExit(1)

print(client_id)
PY
}

local_pg_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_PG_PORT:-}" 15432 25432 35432 45432 55432)"
local_sandbox_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_SANDBOX_PORT:-}" 14243 24243 34243 44243 54243)"
local_identity_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_IDENTITY_PORT:-}" 14248 24248 34248 44248 54248)"
local_profile_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_PROFILE_PORT:-}" 14258 24258 34258 44258 54258)"
local_governance_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_GOVERNANCE_PORT:-}" 14250 24250 34250 44250 54250)"
local_electric_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_ELECTRIC_PORT:-}" 13010 23010 33010 43010 53010)"
local_otel_http_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_OTEL_HTTP_PORT:-}" 14318 24318 34318 44318 54318)"
local_app_port="$(choose_local_port "${CONSOLE_DEV_LOCAL_APP_PORT:-}" 4244 5244 6244 7244 8244)"

frontend_auth_password="$(
  verification_remote_sudo_cat "${auth_database_password_file}"
)"
admin_pat="$(
  verification_remote_sudo_cat /etc/zitadel/admin.pat
)"

if ! auth_client_id="$(fetch_dev_client_id "${admin_pat}" "${auth_project_id}" 2>/dev/null)"; then
  (
    cd "${VERIFICATION_PLATFORM_ROOT}/ansible"
    ansible-playbook -i "${VERIFICATION_INVENTORY_DIR}" playbooks/seed-system.yml --tags dev-oidc
  )
  auth_client_id="$(fetch_dev_client_id "${admin_pat}" "${auth_project_id}")"
fi

if [[ "${print_env_only}" != "1" ]]; then
  ssh -fN -M -S "${control_socket}" \
    -o IPQoS=none \
    -o StrictHostKeyChecking=no \
    -o ExitOnForwardFailure=yes \
    -L "${local_pg_port}:127.0.0.1:5432" \
    -L "${local_sandbox_port}:127.0.0.1:4243" \
    -L "${local_identity_port}:127.0.0.1:4248" \
    -L "${local_profile_port}:127.0.0.1:4258" \
    -L "${local_governance_port}:127.0.0.1:4250" \
    -L "${local_electric_port}:127.0.0.1:3010" \
    -L "${local_otel_http_port}:127.0.0.1:4318" \
    "${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}"

  wait_for_local_tcp_port "frontend_auth PostgreSQL" "${local_pg_port}"
  wait_for_local_tcp_port "sandbox-rental-service" "${local_sandbox_port}"
  wait_for_local_tcp_port "identity-service" "${local_identity_port}"
  wait_for_local_tcp_port "profile-service" "${local_profile_port}"
  wait_for_local_tcp_port "governance-service" "${local_governance_port}"
  wait_for_local_tcp_port "Electric" "${local_electric_port}"
  wait_for_local_tcp_port "OTLP HTTP" "${local_otel_http_port}"
fi

export VERSELF_DOMAIN="${VERSELF_DOMAIN:-${VERIFICATION_DOMAIN}}"
export AUTH_SUBDOMAIN="${AUTH_SUBDOMAIN:-auth}"
export AUTH_CLIENT_ID="${AUTH_CLIENT_ID:-${auth_client_id}}"
export AUTH_PROJECT_ID="${AUTH_PROJECT_ID:-${auth_project_id}}"
export IDENTITY_SERVICE_AUTH_AUDIENCE="${IDENTITY_SERVICE_AUTH_AUDIENCE:-${identity_service_auth_audience}}"
export AUTH_DATABASE_URL="${AUTH_DATABASE_URL:-postgresql://frontend_auth:${frontend_auth_password}@127.0.0.1:${local_pg_port}/frontend_auth?sslmode=disable}"
export AUTH_SESSION_SECRET="${AUTH_SESSION_SECRET:-$(python3 - <<'PY'
import secrets
print(secrets.token_urlsafe(48))
PY
)}"
export SANDBOX_RENTAL_SERVICE_BASE_URL="${SANDBOX_RENTAL_SERVICE_BASE_URL:-http://127.0.0.1:${local_sandbox_port}}"
export IDENTITY_SERVICE_BASE_URL="${IDENTITY_SERVICE_BASE_URL:-http://127.0.0.1:${local_identity_port}}"
export PROFILE_SERVICE_BASE_URL="${PROFILE_SERVICE_BASE_URL:-http://127.0.0.1:${local_profile_port}}"
export PROFILE_SERVICE_AUTH_AUDIENCE="${PROFILE_SERVICE_AUTH_AUDIENCE:-${IDENTITY_SERVICE_AUTH_AUDIENCE}}"
export GOVERNANCE_SERVICE_BASE_URL="${GOVERNANCE_SERVICE_BASE_URL:-http://127.0.0.1:${local_governance_port}}"
export ELECTRIC_URL="${ELECTRIC_URL:-http://127.0.0.1:${local_electric_port}}"
export OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:${local_otel_http_port}}"
export OTEL_SERVICE_NAME="${OTEL_SERVICE_NAME:-verself-web}"
export VERSELF_WEB_DEV_LOCAL_APP_PORT="${local_app_port}"
export BASE_URL="${BASE_URL:-http://127.0.0.1:${local_app_port}}"

cat >"${state_file_tmp}" <<EOF
export VERSELF_DOMAIN=${VERSELF_DOMAIN}
export AUTH_SUBDOMAIN=${AUTH_SUBDOMAIN}
export AUTH_CLIENT_ID=${AUTH_CLIENT_ID}
export AUTH_PROJECT_ID=${AUTH_PROJECT_ID}
export IDENTITY_SERVICE_AUTH_AUDIENCE=${IDENTITY_SERVICE_AUTH_AUDIENCE}
export AUTH_DATABASE_URL=${AUTH_DATABASE_URL}
export AUTH_SESSION_SECRET=${AUTH_SESSION_SECRET}
export SANDBOX_RENTAL_SERVICE_BASE_URL=${SANDBOX_RENTAL_SERVICE_BASE_URL}
export IDENTITY_SERVICE_BASE_URL=${IDENTITY_SERVICE_BASE_URL}
export PROFILE_SERVICE_BASE_URL=${PROFILE_SERVICE_BASE_URL}
export PROFILE_SERVICE_AUTH_AUDIENCE=${PROFILE_SERVICE_AUTH_AUDIENCE}
export GOVERNANCE_SERVICE_BASE_URL=${GOVERNANCE_SERVICE_BASE_URL}
export ELECTRIC_URL=${ELECTRIC_URL}
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
  auth:      https://auth.${VERSELF_DOMAIN}
  pg tunnel: 127.0.0.1:${local_pg_port}
  api:       ${SANDBOX_RENTAL_SERVICE_BASE_URL}
  identity:  ${IDENTITY_SERVICE_BASE_URL}
  profile:   ${PROFILE_SERVICE_BASE_URL}
  governance: ${GOVERNANCE_SERVICE_BASE_URL}
  electric:  ${ELECTRIC_URL}
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
