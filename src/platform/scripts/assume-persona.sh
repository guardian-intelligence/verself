#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"

usage() {
  cat >&2 <<'EOF'
Usage:
  assume-persona.sh platform-admin [--output path|--print]
  assume-persona.sh acme-admin [--output path|--print]
  assume-persona.sh acme-member [--output path|--print]

Writes a 0600 shell env file with browser credentials and short-lived
Zitadel client-credentials JWTs for the selected persona.

In file-output mode, also prints identity-service access metadata JSON to
stdout. --print preserves stdout as shell env only.

These are extremely useful operator and agent utility credentials for live
rehearsal. They cover repo-owned Zitadel IAM surfaces; provider-native
credentials such as ClickHouse, direct Stalwart protocol passwords, and Forgejo
API automation stay behind the existing operator Make wrappers and remote
credstore files.
EOF
}

persona="${1:-}"
if [[ -z "${persona}" ]]; then
  usage
  exit 1
fi
if [[ "${persona}" == "-h" || "${persona}" == "--help" ]]; then
  usage
  exit 0
fi
shift

print_env=0
output_path="${ASSUME_PERSONA_ENV_FILE:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      output_path="${2:-}"
      if [[ -z "${output_path}" ]]; then
        echo "--output requires a path" >&2
        exit 1
      fi
      shift 2
      ;;
    --print)
      print_env=1
      shift
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

verification_context_init "${BASH_SOURCE[0]}"

human_email=""
human_password_path=""
machine_username=""
machine_secret_path=""
include_platform_ops=0
mailbox_account=""
token_projects=()

case "${persona}" in
  platform-admin)
    human_email="agent@${VERIFICATION_DOMAIN}"
    human_password_path="/etc/credstore/seed-system/platform-agent-password"
    machine_username="assume-platform-admin"
    machine_secret_path="/etc/credstore/seed-system/assume-platform-admin-client-secret"
    include_platform_ops=1
    mailbox_account="agents"
    token_projects=(sandbox-rental identity-service secrets-service mailbox-service forgejo)
    ;;
  acme-admin)
    human_email="acme-admin@${VERIFICATION_DOMAIN}"
    human_password_path="/etc/credstore/seed-system/acme-admin-password"
    machine_username="assume-acme-admin"
    machine_secret_path="/etc/credstore/seed-system/assume-acme-admin-client-secret"
    token_projects=(sandbox-rental identity-service secrets-service)
    ;;
  acme-member)
    human_email="acme-user@${VERIFICATION_DOMAIN}"
    human_password_path="/etc/credstore/seed-system/acme-user-password"
    machine_username="assume-acme-member"
    machine_secret_path="/etc/credstore/seed-system/assume-acme-member-client-secret"
    token_projects=(sandbox-rental identity-service secrets-service)
    ;;
  *)
    echo "unknown persona: ${persona}" >&2
    usage
    exit 1
    ;;
esac

if [[ ${print_env} -eq 0 && -z "${output_path}" ]]; then
  output_path="${VERIFICATION_REPO_ROOT}/artifacts/personas/${persona}.env"
fi

auth_base_url="https://auth.${VERIFICATION_DOMAIN}"
admin_pat="$(verification_remote_sudo_cat /etc/zitadel/admin.pat)"
human_password="$(verification_remote_sudo_cat "${human_password_path}")"
machine_secret="$(verification_remote_sudo_cat "${machine_secret_path}")"

project_id() {
  local name="$1"
  local body
  local response
  body="$(
    python3 - "$name" <<'PY'
import json
import sys

print(json.dumps({
    "queries": [
        {
            "nameQuery": {
                "name": sys.argv[1],
                "method": "TEXT_QUERY_METHOD_EQUALS",
            }
        }
    ]
}))
PY
  )"

  response="$(
    curl -fsS \
    -H "Authorization: Bearer ${admin_pat}" \
    -H "Content-Type: application/json" \
    -d "${body}" \
    "${auth_base_url}/management/v1/projects/_search"
  )"
  PROJECT_NAME="${name}" python3 -c '
import json
import os
import sys

project_name = os.environ["PROJECT_NAME"]
payload = json.load(sys.stdin)
matches = payload.get("result") or []
if not matches:
    raise SystemExit(f"Zitadel project not found: {project_name}")
project_id = (matches[0].get("id") or "").strip()
if not project_id:
    raise SystemExit(f"Zitadel project has no id: {project_name}")
print(project_id)
' <<<"${response}"
}

token_for_project() {
  local project_id="$1"
  local scope
  local response
  scope="openid profile urn:zitadel:iam:user:resourceowner urn:zitadel:iam:org:projects:roles urn:zitadel:iam:org:project:id:${project_id}:aud"

  response="$(
    curl -fsS \
    --user "${machine_username}:${machine_secret}" \
    -H "Accept: application/json" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    --data-urlencode "grant_type=client_credentials" \
    --data-urlencode "scope=${scope}" \
    "${auth_base_url}/oauth/v2/token"
  )"
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
token = (payload.get("access_token") or "").strip()
if not token:
    raise SystemExit("Zitadel token response did not include access_token")
print(token)
' <<<"${response}"
}


identity_api_get() {
  local path="$1"
  local token="$2"
  local request_b64

  request_b64="$(
    API_PATH="${path}" API_TOKEN="${token}" python3 -c 'import base64, json, os; print(base64.b64encode(json.dumps({"path": os.environ["API_PATH"], "token": os.environ["API_TOKEN"]}).encode()).decode())'
  )"

  printf '%s\n' "${request_b64}" | verification_ssh "python3 -c 'import base64, json, subprocess, sys; payload = json.loads(base64.b64decode(sys.stdin.readline()).decode()); subprocess.run([\"curl\", \"-fsS\", \"-H\", \"Authorization: Bearer \" + payload[\"token\"], \"http://127.0.0.1:4248\" + payload[\"path\"]], check=True)'"
}

identity_metadata_json() {
  local persona_name="$1"
  local access_json="$2"
  local operations_json="$3"

  IDENTITY_ACCESS_JSON="${access_json}" \
  IDENTITY_OPERATIONS_JSON="${operations_json}" \
  python3 - "${persona_name}" <<'PY'
import json
import os
import sys

persona = sys.argv[1]
access = json.loads(os.environ["IDENTITY_ACCESS_JSON"])
operations = json.loads(os.environ["IDENTITY_OPERATIONS_JSON"])
permissions = set(access.get("permissions") or [])

effective_services = []
operation_permissions = set()
for service in operations.get("services") or []:
    effective_operations = []
    for operation in service.get("operations") or []:
        permission = operation.get("permission") or ""
        if permission:
            operation_permissions.add(permission)
        if permission in permissions:
            effective_operations.append(operation)
    if effective_operations:
        effective_services.append({
            "service": service.get("service") or "",
            "operations": effective_operations,
        })

json.dump({
    "persona": persona,
    "identity_service": {
        "access": access,
        "operations": operations,
        "effective_operations": {
            "org_id": access.get("org_id") or "",
            "caller": access.get("caller") or {},
            "permissions": sorted(permissions),
            "services": effective_services,
            "permissions_without_declared_operation": sorted(
                permissions - operation_permissions
            ),
        },
    },
}, sys.stdout, indent=2, sort_keys=True)
print()
PY
}

declare -A project_ids
declare -A project_tokens
for project in "${token_projects[@]}"; do
  project_ids["${project}"]="$(project_id "${project}")"
  project_tokens["${project}"]="$(token_for_project "${project_ids[${project}]}")"
done

tmp_path=""
tmp_path_finalized=0
if [[ ${print_env} -eq 0 ]]; then
  mkdir -p "$(dirname "${output_path}")"
  tmp_path="$(mktemp "${output_path}.XXXXXX")"
else
  tmp_path="$(mktemp)"
fi

cleanup() {
  if [[ -n "${tmp_path}" && ${tmp_path_finalized} -eq 0 ]]; then
    rm -f "${tmp_path}"
  fi
}
trap cleanup EXIT

write_export() {
  local key="$1"
  local value="$2"
  printf 'export %s=%q\n' "${key}" "${value}" >>"${tmp_path}"
}

: >"${tmp_path}"
chmod 600 "${tmp_path}"

write_export FORGE_METAL_PERSONA "${persona}"
write_export FORGE_METAL_DOMAIN "${VERIFICATION_DOMAIN}"
write_export ZITADEL_ISSUER_URL "${auth_base_url}"
write_export ZITADEL_MACHINE_CLIENT_ID "${machine_username}"
write_export TEST_EMAIL "${human_email}"
write_export TEST_PASSWORD "${human_password}"
write_export BROWSER_EMAIL "${human_email}"
write_export BROWSER_PASSWORD "${human_password}"
write_export RENT_A_SANDBOX_URL "https://rentasandbox.${VERIFICATION_DOMAIN}"
write_export WEBMAIL_URL "https://mail.${VERIFICATION_DOMAIN}"
write_export FORGEJO_URL "https://git.${VERIFICATION_DOMAIN}"

if [[ -n "${project_tokens[sandbox-rental]:-}" ]]; then
  write_export SANDBOX_RENTAL_AUTH_AUDIENCE "${project_ids[sandbox-rental]}"
  write_export SANDBOX_RENTAL_ACCESS_TOKEN "${project_tokens[sandbox-rental]}"
  write_export SANDBOX_RENTAL_TOKEN "${project_tokens[sandbox-rental]}"
fi
if [[ -n "${project_tokens[identity-service]:-}" ]]; then
  write_export IDENTITY_SERVICE_AUTH_AUDIENCE "${project_ids[identity-service]}"
  write_export IDENTITY_SERVICE_ACCESS_TOKEN "${project_tokens[identity-service]}"
  write_export IDENTITY_SERVICE_TOKEN "${project_tokens[identity-service]}"
fi
if [[ -n "${project_tokens[secrets-service]:-}" ]]; then
  write_export SECRETS_SERVICE_AUTH_AUDIENCE "${project_ids[secrets-service]}"
  write_export SECRETS_SERVICE_ACCESS_TOKEN "${project_tokens[secrets-service]}"
  write_export SECRETS_SERVICE_TOKEN "${project_tokens[secrets-service]}"
fi
if [[ -n "${project_tokens[mailbox-service]:-}" ]]; then
  write_export MAILBOX_SERVICE_AUTH_AUDIENCE "${project_ids[mailbox-service]}"
  write_export MAILBOX_SERVICE_ACCESS_TOKEN "${project_tokens[mailbox-service]}"
  write_export MAILBOX_SERVICE_TOKEN "${project_tokens[mailbox-service]}"
fi
if [[ -n "${project_tokens[forgejo]:-}" ]]; then
  write_export FORGEJO_AUTH_AUDIENCE "${project_ids[forgejo]}"
  write_export FORGEJO_OIDC_ACCESS_TOKEN "${project_tokens[forgejo]}"
  write_export FORGEJO_OIDC_TOKEN "${project_tokens[forgejo]}"
fi

if [[ ${include_platform_ops} -eq 1 ]]; then
  write_export MAILBOX_ACCOUNT "${mailbox_account}"
  write_export MAIL_OPERATOR_COMMAND "make mail MAILBOX=${mailbox_account}"
  write_export CLICKHOUSE_OPERATOR_COMMAND "make clickhouse-query QUERY='SELECT 1'"
  write_export FORGEJO_OPERATOR_CREDENTIAL "provider-native forgejo-automation token in /etc/credstore/forgejo/automation-token"
fi

if [[ ${print_env} -eq 1 ]]; then
  cat "${tmp_path}"
else
  mv -f "${tmp_path}" "${output_path}"
  tmp_path_finalized=1
  printf 'persona env written: %s\n' "${output_path}" >&2
  printf 'source %q\n' "${output_path}" >&2
  verification_wait_for_loopback_api "identity-service" "http://127.0.0.1:4248/readyz" 200
  identity_access_json="$(identity_api_get "/api/v1/organization" "${project_tokens[identity-service]}")"
  if ! identity_operations_json="$(
    identity_api_get "/api/v1/organization/operations" "${project_tokens[identity-service]}" 2>/dev/null
  )"; then
    printf 'WARNING: identity-service operation catalog endpoint unavailable; continuing with empty operations metadata\n' >&2
    identity_operations_json='{"services":[]}'
  fi
  identity_metadata_json "${persona}" "${identity_access_json}" "${identity_operations_json}"
fi
